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
	"net/http"
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
//
// STUB (prometheus-export, layer 3): 404 to everything.
func NewHandler(_ Source, _ time.Duration, _ func() time.Time) http.Handler {
	return http.NotFoundHandler()
}

// NewServer is an http.Server around h with a ReadHeaderTimeout: a port open
// to anyone must not hold a goroutine for a client that never finishes its
// request line.
//
// STUB (prometheus-export, layer 3): no timeout.
func NewServer(h http.Handler) *http.Server {
	return &http.Server{Handler: h}
}
