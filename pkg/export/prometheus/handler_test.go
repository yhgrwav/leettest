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
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	contentType = "text/plain; version=0.0.4; charset=utf-8"
	// ceiling bounds every wait of these tests.
	ceiling = 10 * time.Second
)

// fakeClock is a clock the test moves.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = c.t.Add(d)
}

// counting is a Source whose n-th read shows InFlight n, so the text says
// which read it came from.
type counting struct {
	reads atomic.Int64
	done  chan struct{}
}

func newCounting() *counting { return &counting{done: make(chan struct{})} }

func (c *counting) source() Source {
	return Source{
		Read: func() View {
			return View{Started: true, InFlight: c.reads.Add(1)}
		},
		Done: c.done,
	}
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), method, target, http.NoBody))

	return rec
}

func inFlightOf(body string) string {
	return samples(body)["leettest_in_flight"]
}

// Ground: review verdict R1 — GET /metrics is 200.
func TestHandler_GetMetricsIsOK(t *testing.T) {
	c := newCounting()
	h := NewHandler(c.source(), MinRenderInterval, newClock().now)

	if got := get(h, http.MethodGet, "/metrics").Code; got != http.StatusOK {
		t.Errorf("GET /metrics = %d, want 200", got)
	}
}

// Ground: review verdict R1 — the Content-Type of the text format 0.0.4.
func TestHandler_ContentTypeIsTheTextFormat(t *testing.T) {
	c := newCounting()
	h := NewHandler(c.source(), MinRenderInterval, newClock().now)

	if got := get(h, http.MethodGet, "/metrics").Header().Get("Content-Type"); got != contentType {
		t.Errorf("Content-Type = %q, want %q", got, contentType)
	}
}

// Ground: contract — the body is Render of what the source read.
func TestHandler_TheBodyIsTheRenderOfTheSourcesView(t *testing.T) {
	src := Source{Read: goldenView}
	h := NewHandler(src, MinRenderInterval, newClock().now)

	if got, want := get(h, http.MethodGet, "/metrics").Body.String(), renderOf(t, goldenView()); got != want {
		t.Errorf("body differs from Render of the view:\n%.200s", got)
	}
}

// Ground: edge — a query string is not part of the path.
func TestHandler_AQueryStringStillServesMetrics(t *testing.T) {
	c := newCounting()
	h := NewHandler(c.source(), MinRenderInterval, newClock().now)

	if got := get(h, http.MethodGet, "/metrics?debug=1").Code; got != http.StatusOK {
		t.Errorf("GET /metrics?debug=1 = %d, want 200", got)
	}
}

// Ground: review verdict R1 — only GET /metrics; everything else is 404,
// another method on /metrics included (taken literally).
func TestHandler_EverythingElseIsNotFound(t *testing.T) {
	for _, tc := range []struct{ method, target string }{
		{http.MethodPost, "/metrics"},
		{http.MethodPut, "/metrics"},
		{http.MethodHead, "/metrics"},
		{http.MethodDelete, "/metrics"},
		{http.MethodGet, "/"},
		{http.MethodGet, "/metrics/"},
		{http.MethodGet, "/Metrics"},
		{http.MethodGet, "/metrics/extra"},
		{http.MethodGet, "/healthz"},
	} {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			c := newCounting()
			h := NewHandler(c.source(), MinRenderInterval, newClock().now)

			// The stub is 404 to everything, so first prove the handler serves.
			if get(h, http.MethodGet, "/metrics").Code != http.StatusOK {
				t.Fatal("GET /metrics is not 200: the 404 below would prove nothing")
			}
			if got := get(h, tc.method, tc.target).Code; got != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", tc.method, tc.target, got)
			}
		})
	}
}

// Ground: review verdict R2.5 — a request for another path does not read the
// run nor render (a port open to anyone costs nothing off /metrics).
func TestHandler_ANotFoundDoesNotReadTheRun(t *testing.T) {
	c := newCounting()
	h := NewHandler(c.source(), MinRenderInterval, newClock().now)

	for _, target := range []string{"/", "/healthz", "/metrics/x"} {
		get(h, http.MethodGet, target)
	}
	get(h, http.MethodPost, "/metrics")
	get(h, http.MethodGet, "/metrics")

	if got := c.reads.Load(); got != 1 {
		t.Errorf("reads of the run = %d, want 1 (only the GET /metrics)", got)
	}
}

// Ground: review verdict R2.5 — a hundred scrapes in one second are one read
// and one render.
func TestHandler_AHundredScrapesInASecondReadTheRunOnce(t *testing.T) {
	c := newCounting()
	h := NewHandler(c.source(), MinRenderInterval, newClock().now)

	for range 100 {
		get(h, http.MethodGet, "/metrics")
	}

	if got := c.reads.Load(); got != 1 {
		t.Errorf("reads of the run for 100 scrapes in one second = %d, want 1", got)
	}
}

// Ground: review verdict R2.5 — a scrape inside the interval gets the text of
// the last render, not a fresh one.
func TestHandler_AScrapeInsideTheIntervalGetsTheCachedText(t *testing.T) {
	c, clk := newCounting(), newClock()
	h := NewHandler(c.source(), MinRenderInterval, clk.now)

	first := get(h, http.MethodGet, "/metrics").Body.String()
	clk.advance(MinRenderInterval - time.Millisecond)
	second := get(h, http.MethodGet, "/metrics").Body.String()

	if first != second || inFlightOf(second) != "1" {
		t.Errorf("a scrape 1ms before the interval ends: in_flight %q (first %q), want the cached 1", inFlightOf(second), inFlightOf(first))
	}
}

// Ground: edge (boundary) — exactly one interval after the last render the
// text is rendered again.
func TestHandler_AScrapeExactlyOneIntervalLaterRendersAgain(t *testing.T) {
	c, clk := newCounting(), newClock()
	h := NewHandler(c.source(), MinRenderInterval, clk.now)

	get(h, http.MethodGet, "/metrics")
	clk.advance(MinRenderInterval)

	if got := inFlightOf(get(h, http.MethodGet, "/metrics").Body.String()); got != "2" {
		t.Errorf("in_flight one interval after the first scrape = %q, want 2 (a second read)", got)
	}
}

// Ground: review verdict R2.5 — the interval counts from the last render, so
// a long quiet spell then a scrape is one render, not a catch-up.
func TestHandler_AfterAQuietSpellOneScrapeIsOneRender(t *testing.T) {
	c, clk := newCounting(), newClock()
	h := NewHandler(c.source(), MinRenderInterval, clk.now)

	get(h, http.MethodGet, "/metrics")
	clk.advance(time.Hour)
	for range 20 {
		get(h, http.MethodGet, "/metrics")
	}

	if got := c.reads.Load(); got != 2 {
		t.Errorf("reads = %d, want 2 (the first scrape and the first after the hour)", got)
	}
}

// Ground: Brief Р6 — the run is over: the next scrape renders the final
// values even inside the interval, and from then on the same text, with no
// further read, forever.
func TestHandler_AfterDoneOneFinalRenderThenNeverAgain(t *testing.T) {
	c, clk := newCounting(), newClock()
	h := NewHandler(c.source(), MinRenderInterval, clk.now)

	get(h, http.MethodGet, "/metrics")
	close(c.done)
	clk.advance(10 * time.Millisecond)
	final := get(h, http.MethodGet, "/metrics").Body.String()

	clk.advance(24 * time.Hour)
	for range 50 {
		get(h, http.MethodGet, "/metrics")
	}
	later := get(h, http.MethodGet, "/metrics").Body.String()

	if got := c.reads.Load(); got != 2 || inFlightOf(final) != "2" || later != final {
		t.Errorf("reads = %d, final in_flight %q, later body equal: %v; want 2 reads, final from the 2nd, later == final",
			got, inFlightOf(final), later == final)
	}
}

// Ground: edge — the run was over before the first scrape: one read.
func TestHandler_DoneBeforeTheFirstScrapeIsOneRenderForever(t *testing.T) {
	c, clk := newCounting(), newClock()
	h := NewHandler(c.source(), MinRenderInterval, clk.now)
	close(c.done)

	for range 30 {
		get(h, http.MethodGet, "/metrics")
		clk.advance(time.Hour)
	}

	if got := c.reads.Load(); got != 1 {
		t.Errorf("reads after Done = %d, want 1", got)
	}
}

// Ground: review verdict R2.5 — concurrent scrapes at one instant share one
// render and see one text. Run with -race.
func TestHandler_ConcurrentScrapesShareOneRender(t *testing.T) {
	const scrapers = 64

	c := newCounting()
	h := NewHandler(c.source(), MinRenderInterval, newClock().now)

	bodies := make([]string, scrapers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range scrapers {
		wg.Go(func() {
			<-start
			for range 20 {
				bodies[i] = get(h, http.MethodGet, "/metrics").Body.String()
			}
		})
	}
	close(start)

	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(ceiling):
		t.Fatal("the scrapers did not finish within the ceiling")
	}

	same := true
	for _, b := range bodies {
		same = same && b == bodies[0]
	}
	if got := c.reads.Load(); got != 1 || !same {
		t.Errorf("reads = %d, bodies all equal = %v; want 1 and true", got, same)
	}
}

// Ground: review verdict R2.5 — a scrape that arrives while a render is in
// progress waits for it and takes its text; it does not start a second read.
func TestHandler_AScrapeDuringARenderTakesItsText(t *testing.T) {
	var reads atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	src := Source{Read: func() View {
		n := reads.Add(1)
		if n == 1 {
			close(entered)
			<-release
		}

		return View{Started: true, InFlight: n}
	}}
	h := NewHandler(src, MinRenderInterval, newClock().now)

	var wg sync.WaitGroup
	bodies := make([]string, 2)
	firstReturned := make(chan struct{})
	wg.Go(func() {
		bodies[0] = get(h, http.MethodGet, "/metrics").Body.String()
		close(firstReturned)
	})

	select {
	case <-entered:
	case <-firstReturned:
		t.Fatal("the first scrape returned without reading the source")
	case <-time.After(ceiling):
		t.Fatal("the first scrape did not reach the source within the ceiling")
	}
	wg.Go(func() { bodies[1] = get(h, http.MethodGet, "/metrics").Body.String() })
	close(release)

	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(ceiling):
		t.Fatal("the scrapes did not finish within the ceiling")
	}

	if got := reads.Load(); got != 1 || bodies[0] != bodies[1] || bodies[0] == "" {
		t.Errorf("reads = %d, bodies equal = %v; want 1 read and one text", got, bodies[0] == bodies[1])
	}
}

// Ground: gosec G112 / review verdict R1 — a port open to anyone must not keep
// a goroutine for a client that never finishes its request line.
func TestNewServer_HasAReadHeaderTimeout(t *testing.T) {
	srv := NewServer(http.NotFoundHandler())

	if srv.ReadHeaderTimeout <= 0 || srv.ReadHeaderTimeout > time.Minute {
		t.Errorf("ReadHeaderTimeout = %v, want a positive timeout of at most a minute", srv.ReadHeaderTimeout)
	}
}

// Ground: contract — the server serves the handler it was given.
// Green on the stub: it holds the line for the code.
func TestNewServer_ServesTheGivenHandler(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	srv := NewServer(h)

	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody))
	if rec.Code != http.StatusTeapot {
		t.Errorf("the server's handler answered %d, want the given handler's %d", rec.Code, http.StatusTeapot)
	}
}
