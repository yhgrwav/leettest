// Copyright 2026 yhgrwav
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/export/prometheus"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// exportSettings is the export resolved before the run: whether it is on, where
// it listens, how long it lingers and the constant labels of every series.
type exportSettings struct {
	on     bool
	listen string
	linger time.Duration
	labels []prometheus.Label
}

// resolveExportSettings combines export.prometheus with -metrics-listen and
// refuses what cannot run before the listener opens.
func resolveExportSettings(cfg *config.MasterConfig, flags runFlags) (exportSettings, error) {
	var prom *config.Prometheus
	if cfg.Export != nil {
		prom = cfg.Export.Prometheus
	}
	if prom == nil && flags.metricsListen == "" {
		return exportSettings{}, nil
	}

	if cfg.Load.Breakpoint != nil {
		return exportSettings{}, fmt.Errorf("metrics export cannot run beside a breakpoint search")
	}
	if flags.dryRun {
		return exportSettings{}, fmt.Errorf("metrics export cannot run with -dry-run")
	}

	listen := flags.metricsListen
	linger := config.DefaultLinger
	if prom != nil {
		linger = prom.Linger
		if listen == "" {
			listen = prom.Listen
		}
	}
	if linger < 0 {
		return exportSettings{}, fmt.Errorf("metrics export: linger %s must not be negative", linger)
	}
	if listen == "" {
		return exportSettings{}, fmt.Errorf("metrics export needs an address: set export.prometheus.listen or -metrics-listen")
	}

	merged := mergeLabels(cfg.Labels, flags.flagLabels)
	labels := make([]prometheus.Label, 0, len(merged))
	for _, key := range slices.Sorted(maps.Keys(merged)) {
		if err := prometheus.CheckLabelName(key); err != nil {
			return exportSettings{}, fmt.Errorf("metrics export: %w", err)
		}
		labels = append(labels, prometheus.Label{Name: key, Value: merged[key]})
	}

	return exportSettings{on: true, listen: listen, linger: linger, labels: labels}, nil
}

// metricsExport is the running /metrics endpoint: the listener, the handler's
// cache and the one view it refills.
type metricsExport struct {
	srv  *http.Server
	lis  net.Listener
	addr string

	labels []prometheus.Label

	done       chan struct{}
	serveDone  chan struct{}
	pressCh    chan struct{}
	closeOnce  sync.Once
	finishOnce sync.Once

	eng atomic.Pointer[engine.Engine]

	view   engine.ExportView
	pview  prometheus.View
	buf    *metrics.Buffer
	bounds []time.Duration
}

// openMetricsExport binds the address and starts serving it. The address is
// taken before the target is asked, so a taken port is refused there.
func openMetricsExport(settings exportSettings, stderr io.Writer) (*metricsExport, error) {
	lis, err := new(net.ListenConfig).Listen(context.Background(), "tcp", settings.listen)
	if err != nil {
		return nil, fmt.Errorf("metrics export: listen on %s: %w", settings.listen, err)
	}

	e := &metricsExport{
		lis:       lis,
		addr:      lis.Addr().String(),
		labels:    settings.labels,
		done:      make(chan struct{}),
		serveDone: make(chan struct{}),
		pressCh:   make(chan struct{}, 1),
		buf:       metrics.NewBuffer(),
	}
	e.pview.Labels = settings.labels
	e.bounds = make([]time.Duration, len(prometheus.Bounds))
	for i, bound := range prometheus.Bounds {
		e.bounds[i] = time.Duration(math.Round(bound * float64(time.Second)))
	}

	e.srv = prometheus.NewServer(prometheus.NewHandler(
		prometheus.Source{Read: e.read, Done: e.done},
		prometheus.MinRenderInterval, time.Now,
	))
	go func() {
		defer close(e.serveDone)
		_ = e.srv.Serve(lis)
	}()

	fmt.Fprintf(stderr, "metrics: listening on http://%s/metrics\n", e.addr)

	return e, nil
}

func (e *metricsExport) setEngine(eng *engine.Engine) {
	e.eng.Store(eng)
}

// finish says the run is over: the next scrape renders the final values.
func (e *metricsExport) finish() {
	e.finishOnce.Do(func() { close(e.done) })
}

// press ends the linger; it is what a stop or an abort means after the report.
func (e *metricsExport) press() {
	select {
	case e.pressCh <- struct{}{}:
	default:
	}
}

// serveFinal keeps the endpoint up for d after the report, then returns. A
// press or the caller's context ends it earlier.
func (e *metricsExport) serveFinal(ctx context.Context, stderr io.Writer, d time.Duration) {
	fmt.Fprintf(stderr, "metrics: serving final values for %s at %s\n", d, e.addr)

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-e.pressCh:
	case <-ctx.Done():
	}
}

// close shuts the endpoint down and waits for its serving goroutine.
func (e *metricsExport) close() {
	e.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_ = e.srv.Shutdown(ctx)
		_ = e.lis.Close()
		<-e.serveDone
	})
}

func (e *metricsExport) read() prometheus.View {
	e.pview.Started = false
	e.pview.Warmup = false
	e.pview.InFlight = 0
	e.pview.Methods = e.pview.Methods[:0]

	eng := e.eng.Load()
	if eng == nil {
		return e.pview
	}

	eng.ExportInto(&e.view, e.buf, e.bounds)

	e.pview.Started = e.view.Started
	e.pview.Warmup = e.view.Warmup
	e.pview.InFlight = e.view.InFlight
	if cap(e.pview.Methods) < len(e.view.Methods) {
		e.pview.Methods = make([]prometheus.Method, len(e.view.Methods))
	} else {
		e.pview.Methods = e.pview.Methods[:len(e.view.Methods)]
	}

	for i := range e.view.Methods {
		src := &e.view.Methods[i]
		dst := &e.pview.Methods[i]
		*dst = prometheus.Method{
			Name:       strings.TrimPrefix(src.Name, "/"),
			PlannedRPS: src.PlannedRPS,
			Calls:      src.Calls,
			NotSent:    src.NotSent,
			WarmupSent: src.WarmupSent,
		}
		dst.Latency.Count = src.Count
		dst.Latency.SumSeconds = src.Sum.Seconds()
		copy(dst.Latency.Cumulative[:], src.Cumulative)
	}

	return e.pview
}
