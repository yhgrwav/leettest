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

package prometheus

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"
)

// Source is where the handler reads the run from.
type Source struct {
	// Read returns the counters now.
	Read func() View
	// Done is closed once the run is over and nothing changes any more; Read
	// then returns the final values. A nil Done is a run that never ends.
	Done <-chan struct{}
}

// NewHandler serves GET /metrics and answers 404 to everything else, any other
// method included, without reading src. The text is rendered from src.Read at
// most once per minInterval while the run goes (now is the clock), however
// many scrapes come: a scrape at least minInterval after the last render
// renders again. Once Done is closed the next scrape renders one last time,
// whatever the interval, and from then on that text is served and Read is never
// called again. Concurrent scrapes share one render. The reply is 200 with
// Content-Type "text/plain; version=0.0.4; charset=utf-8".
func NewHandler(src Source, minInterval time.Duration, now func() time.Time) http.Handler {
	return &handler{src: src, minInterval: minInterval, now: now}
}

// NewServer is an http.Server around h with a ReadHeaderTimeout: a port open
// to anyone must not hold a goroutine for a client that never finishes its
// request line.
func NewServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

type handler struct {
	src         Source
	minInterval time.Duration
	now         func() time.Time

	mu       sync.Mutex
	text     string
	last     time.Time
	rendered bool
	final    bool
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/metrics" {
		http.NotFound(w, r)

		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	done := false
	if h.src.Done != nil {
		select {
		case <-h.src.Done:
			done = true
		default:
		}
	}

	switch {
	case h.final:
	case done:
		h.render()
		h.final = true
	case !h.rendered:
		h.render()
	case !h.now().Before(h.last.Add(h.minInterval)):
		h.render()
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, h.text)
}

func (h *handler) render() {
	var b bytes.Buffer
	_ = Render(&b, h.src.Read())
	h.text = b.String()
	h.last = h.now()
	h.rendered = true
}
