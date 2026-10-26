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

package cli

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// A second whose rounded share reaches this many hundredths is busy.
const busyHundredths = 90

// A counted second shorter than this, the plan's last, joins the one before:
// a Windows clock steps 15.6 ms, so 10 ms of wall would read 0 or 1.56.
const minLastSecond = 500 * time.Millisecond

// CPUSample is one reading of the process's CPU time (user plus system) at
// the moment At.
type CPUSample struct {
	At  time.Time
	CPU time.Duration
}

// GeneratorCPU is how much of the CPU available to the process the generator
// used, laid on the run's second grid.
type GeneratorCPU struct {
	// AvailableCores is the smaller of GOMAXPROCS and the cgroup quota.
	AvailableCores float64
	// Share is the CPU time over the counted seconds divided by their wall
	// time and the available cores, rounded half-up to 2 decimals.
	Share float64
	// BusySeconds counts the counted seconds whose rounded share is 0.90 or
	// more; FirstBusy is the first of them, from the start of the run.
	BusySeconds int
	FirstBusy   *int
	// Seconds[i] is the share of second i from the start of the run, warm-up
	// included; nil where no reading covers the second.
	Seconds []*float64
	// Counted is how many seconds Share, BusySeconds and the report line
	// speak of; 0 when the counted part of the run is under a second.
	Counted int
}

// cpuSpan is the CPU time used while the clock went from start to end, both
// from the start of the run.
type cpuSpan struct {
	start, end time.Duration
	cpu        time.Duration
}

// cpuSpans are the CPU the process used between each two readings.
type cpuSpans []cpuSpan

// cpuWindow is a counted second of the run; the last may be a piece.
type cpuWindow struct {
	second   int
	from, to time.Duration
}

// newCPUSpans turns the readings into spans from the start of the run. Two
// readings at one instant make one, the later; a reading that goes back in
// time is dropped.
func newCPUSpans(samples []CPUSample, startedAt time.Time) cpuSpans {
	type point struct{ at, cpu time.Duration }

	points := make([]point, 0, len(samples))
	for _, s := range samples {
		p := point{s.At.Sub(startedAt), s.CPU}
		switch last := len(points) - 1; {
		case last >= 0 && p.at == points[last].at:
			points[last] = p
		case last >= 0 && p.at < points[last].at:
		default:
			points = append(points, p)
		}
	}

	spans := make(cpuSpans, 0, len(points))
	for i := 1; i < len(points); i++ {
		spans = append(spans, cpuSpan{
			start: points[i-1].at, end: points[i].at, cpu: max(0, points[i].cpu-points[i-1].cpu),
		})
	}

	return spans
}

// in is the CPU time used in [from, to) and the wall time of it that the
// readings cover. A span that straddles an edge gives its CPU pro rata by
// wall time, so a reading that came late moves none of it to the wrong second.
// The CPU time is exact: a rational number of nanoseconds.
func (s cpuSpans) in(from, to time.Duration) (*big.Rat, time.Duration) {
	used, covered := new(big.Rat), time.Duration(0)

	for i := sort.Search(len(s), func(i int) bool { return s[i].end > from }); i < len(s) && s[i].start < to; i++ {
		part := min(to, s[i].end) - max(from, s[i].start)
		if part <= 0 {
			continue
		}
		covered += part
		used.Add(used, new(big.Rat).SetFrac(
			new(big.Int).Mul(big.NewInt(int64(s[i].cpu)), big.NewInt(int64(part))),
			big.NewInt(int64(s[i].end-s[i].start))))
	}

	return used, covered
}

// hundredths is the share cpu / (wall x cores) in hundredths, rounded half up,
// worked out on the exact numbers: 0.285 is 29, as it reads.
func hundredths(cpu *big.Rat, wall time.Duration, cores *big.Rat) int64 {
	share := new(big.Rat).Quo(cpu, new(big.Rat).Mul(new(big.Rat).SetInt64(int64(wall)), cores))
	share.Mul(share, big.NewRat(100, 1))
	share.Add(share, big.NewRat(1, 2))

	return new(big.Int).Quo(share.Num(), share.Denom()).Int64()
}

// fromHundredths is the share as the report prints it: 29 is 0.29.
func fromHundredths(h int64) float64 {
	return float64(h) / 100
}

// LayGeneratorCPU lays the readings on the run's second grid, or returns nil
// when there is nothing to lay them on: fewer than two readings, no cores, or
// no counted part of the run.
//
// Second i is [StartedAt+i s, StartedAt+(i+1) s), warm-up included, the way
// methods[].seconds and silent_from_s count; the grid is as long as the
// longest of those. The seconds that count for Share and busy are the
// planned ones after the warm-up: from the first second wholly after it to
// the last that starts before the schedule stopped handing out calls
// (Report.SendingEnded: the plan's end, or a stop). The wait for calls still
// in flight after it, the timeout tail, is on the grid and not counted.
func LayGeneratorCPU(report engine.Report, samples []CPUSample, cores float64) *GeneratorCPU {
	if len(samples) < 2 || !(cores > 0) || math.IsInf(cores, 0) {
		return nil
	}

	grid := cpuGrid(report)
	spans := newCPUSpans(samples, report.StartedAt)
	rate := new(big.Rat).SetFloat64(cores)
	end := report.SendingEnded

	windows, merged := cpuWindows(report.Warmup, end, grid)
	counted, total, wall := countWindows(spans, windows, rate)
	// A counted part under a second, or none, has a share from the run after
	// its warm-up as a whole, and no second to call busy.
	short := wall < time.Second
	if short {
		from := time.Duration(0)
		if report.Warmup < end {
			from = max(0, report.Warmup)
		}
		total, wall = spans.in(from, end)
	}
	if wall <= 0 {
		return nil
	}

	out := &GeneratorCPU{
		AvailableCores: cores, Share: fromHundredths(hundredths(total, wall, rate)),
		Seconds: cpuSecondShares(spans, rate, grid, counted, merged),
	}

	if short {
		return out
	}
	countBusySeconds(out, counted, grid)

	return out
}

// cpuGrid is the run's length in seconds: the longest per-method seconds list.
func cpuGrid(report engine.Report) int {
	grid := 0
	for i := range report.Methods {
		grid = max(grid, len(report.Methods[i].Seconds))
	}

	return grid
}

// cpuWindows are the counted seconds: each second after the warm-up that
// starts before end, cut at end. A last one shorter than minLastSecond joins
// the one before it, if there is one; merged is that second, or -1.
func cpuWindows(warmup, end time.Duration, grid int) (windows []cpuWindow, merged int) {
	first := int((max(0, warmup) + time.Second - 1) / time.Second)
	for i := first; i < grid && time.Duration(i)*time.Second < end; i++ {
		from := time.Duration(i) * time.Second
		windows = append(windows, cpuWindow{i, from, min(from+time.Second, end)})
	}
	merged = -1
	if n := len(windows); n >= 2 && windows[n-1].to-windows[n-1].from < minLastSecond {
		windows[n-2].to = windows[n-1].to
		merged = windows[n-1].second
		windows = windows[:n-1]
	}

	return windows, merged
}

// countWindows gives each counted second its share in hundredths, and returns
// the CPU time and the wall time the readings cover in them all. A second no
// reading covers is left out of all three.
func countWindows(spans cpuSpans, windows []cpuWindow, rate *big.Rat) (map[int]int64, *big.Rat, time.Duration) {
	counted := make(map[int]int64, len(windows))
	total, wall := new(big.Rat), time.Duration(0)
	for _, w := range windows {
		used, covered := spans.in(w.from, w.to)
		if covered <= 0 {
			continue
		}
		counted[w.second] = hundredths(used, covered, rate)
		total.Add(total, used)
		wall += covered
	}

	return counted, total, wall
}

// cpuSecondShares is the share of every second on the grid: the counted one
// where there is one, the second's own readings elsewhere, nil for the merged
// second and for a second no reading covers.
func cpuSecondShares(spans cpuSpans, rate *big.Rat, grid int, counted map[int]int64, merged int) []*float64 {
	seconds := make([]*float64, grid)
	for i := range grid {
		h, isCounted := counted[i]
		switch {
		case isCounted:
		case i == merged:
			continue
		default:
			from := time.Duration(i) * time.Second
			used, covered := spans.in(from, from+time.Second)
			if covered <= 0 {
				continue
			}
			h = hundredths(used, covered, rate)
		}
		s := fromHundredths(h)
		seconds[i] = &s
	}

	return seconds
}

// countBusySeconds sets Counted, BusySeconds (at least busyHundredths) and
// FirstBusy from the counted seconds.
func countBusySeconds(out *GeneratorCPU, counted map[int]int64, grid int) {
	for i := range grid {
		h, ok := counted[i]
		if !ok {
			continue
		}
		out.Counted++
		if h >= busyHundredths {
			out.BusySeconds++
			if out.FirstBusy == nil {
				at := i
				out.FirstBusy = &at
			}
		}
	}
}

// CPUMeter records the process's CPU time beside a run.
type CPUMeter struct {
	read     func() (time.Duration, error)
	now      func() time.Time
	interval time.Duration

	mu      sync.Mutex
	samples []CPUSample
	failed  bool
}

// NewCPUMeter returns a meter that reads the CPU time with read and the
// moment with now, and ticks every interval once started.
func NewCPUMeter(read func() (time.Duration, error), now func() time.Time, interval time.Duration) *CPUMeter {
	return &CPUMeter{read: read, now: now, interval: interval}
}

// Tick takes one sample. A failed read ends the measurement for good.
func (m *CPUMeter) Tick() {
	m.mu.Lock()
	failed := m.failed
	m.mu.Unlock()

	if failed {
		return
	}
	// Outside the lock: the lock guards the list, and a reading in progress
	// must not hide, by holding it, that stop returned without waiting.
	at := m.now()
	cpu, err := m.read()

	m.mu.Lock()
	defer m.mu.Unlock()

	if err != nil {
		m.failed = true

		return
	}
	m.samples = append(m.samples, CPUSample{At: at, CPU: cpu})
}

// Start takes a sample and then one every interval until the returned stop,
// which waits for the ticker to finish and takes the last one.
func (m *CPUMeter) Start() (stop func()) {
	m.Tick()

	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				m.Tick()
			case <-quit:
				return
			}
		}
	}()

	var once sync.Once

	return func() {
		once.Do(func() {
			close(quit)
			<-done
			m.Tick()
		})
	}
}

// Result lays the samples on the grid of the report, or nil when a read
// failed: a partial series would give a share over other seconds than the
// line names.
func (m *CPUMeter) Result(report engine.Report, cores float64) *GeneratorCPU {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failed {
		return nil
	}

	return LayGeneratorCPU(report, m.samples, cores)
}

var errCPUNotRead = errors.New("process CPU time not read")

// cpuNote is the line about the generator's CPU for the report, or "". It
// states what was measured and judges nothing: no verdict, no exit code.
func cpuNote(g *GeneratorCPU) string {
	if g == nil || g.BusySeconds == 0 || g.FirstBusy == nil {
		return ""
	}

	return fmt.Sprintf("generator CPU: >= %d%% of the %s available in %d of %d s (from %d s);\n"+
		"its own delays may be in the latencies.",
		busyHundredths, coresText(g.AvailableCores), g.BusySeconds, g.Counted, *g.FirstBusy)
}

// coresText is "1 core", "1.5 cores": up to two decimals, so a quota of 1.25
// is not read as 1.3.
func coresText(cores float64) string {
	text := strconv.FormatFloat(math.Round(cores*100)/100, 'f', -1, 64)
	if text == "1" {
		return text + " core"
	}

	return text + " cores"
}
