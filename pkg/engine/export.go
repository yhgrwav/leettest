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

package engine

import (
	"time"

	"github.com/yhgrwav/leettest/pkg/metrics"
)

// Indices of ExportMethod.Calls: the eleven categories in the order of the
// contract (CLAUDE.md, categories), the order Prometheus renders them in.
const (
	ExportSuccess = iota
	ExportRequestError
	ExportOverload
	ExportFailure
	ExportTimedOut
	ExportCutOff
	ExportUnreachable
	ExportClientError
	ExportBadResponse
	ExportAborted
	ExportUnclassified
	ExportCategories
)

// Indices of ExportMethod.NotSent: the client-side cause that kept a call
// from leaving the generator.
const (
	ExportCauseGenerator = iota
	ExportCauseConnection
	ExportCauseStream
	ExportCauses
)

// ExportView is the run as a metrics scrape reads it: counters at one moment,
// copied under the locks the recording takes. It is plain data, so a caller
// keeps one and refills it. It knows nothing of Prometheus.
type ExportView struct {
	// Started is false until the run has begun: no clock yet, nothing counted.
	Started bool
	// Warmup is true while the run is inside its warm-up.
	Warmup   bool
	InFlight int64
	// Methods are the run's methods, by name.
	Methods []ExportMethod
}

// ExportMethod is one method's counters. Warm-up is in WarmupSent only.
type ExportMethod struct {
	Name string
	// PlannedRPS is the rate the plan offers now; 0 once the plan is over.
	PlannedRPS float64
	// Calls counts measured calls by category, indexed by the Export*
	// constants. Calls[ExportSuccess] is Count: one copy of the distribution
	// gives both.
	Calls [ExportCategories]int64
	// Cumulative[i] is the measured successes at or under the i-th bound the
	// caller gave; Count is the total (+Inf), Sum the sum of their latencies.
	Cumulative []int64
	Count      int64
	Sum        time.Duration
	// NotSent counts calls that never left the generator, by cause, indexed by
	// the ExportCause* constants; unknown cause is in none of them.
	NotSent    [ExportCauses]int64
	WarmupSent int64
}

// ExportInto fills dst with the run now, reusing its memory and buf's: the
// bounds are the histogram edges, ascending. The counters are copied under
// the lock Record takes, each method's distribution under its own lock, never
// nested; the plan's rate is read at the elapsed time taken under the same
// lock as the counters. Methods come by name. Before Start, Started is false
// and nothing else is filled.
//
// STUB (prometheus-export, layer 4): leaves dst empty.
func (s *Stats) ExportInto(dst *ExportView, _ *metrics.Buffer, _ []time.Duration, _ []Call) {
	*dst = ExportView{Methods: dst.Methods[:0]}
}

// ExportInto is Stats.ExportInto with the plan of this engine's calls and the
// number of calls in flight.
//
// STUB (prometheus-export, layer 4): leaves dst empty.
func (e *Engine) ExportInto(dst *ExportView, buf *metrics.Buffer, bounds []time.Duration) {
	e.stats.ExportInto(dst, buf, bounds, e.opts.Calls)
}
