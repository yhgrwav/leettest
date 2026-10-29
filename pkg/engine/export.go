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
	"slices"
	"strings"
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

type exportMethod struct {
	name    string
	method  *methodStats
	aborted int
	// timedOut, cutOff, unanswered, unknown and clientError are the method's
	// plain counters; the other categories are read from their distributions.
	timedOut, cutOff, unanswered, unknown, clientError int
	notSentGenerator, notSentStream, notSentConnection int
	warmupSent                                         int
}

// ExportInto fills dst with the run now, reusing its memory and buf's: the
// bounds are the histogram edges, ascending. The counters are copied under
// the lock Record takes, each method's distribution under its own lock, never
// nested; the plan's rate is read at the elapsed time taken under the same
// lock as the counters. Methods come by name. Before Start, Started is false
// and nothing else is filled.
func (s *Stats) ExportInto(dst *ExportView, buf *metrics.Buffer, bounds []time.Duration, calls []Call) {
	s.mu.Lock()
	if s.startedAt.IsZero() {
		dst.Started = false
		dst.Warmup = false
		dst.InFlight = 0
		dst.Methods = dst.Methods[:0]
		s.mu.Unlock()

		return
	}

	elapsed := s.elapsed()
	warmup := s.endedAt.IsZero() && elapsed < s.warmup
	ended := !s.endedAt.IsZero()
	sendingEnded := !s.sendingEndedAt.IsZero()
	sendingEnd := time.Duration(0)
	if sendingEnded {
		sendingEnd = s.sendingEndedAt.Sub(s.startedAt)
	}

	methods := make([]exportMethod, 0, len(s.byMethod))
	for name, method := range s.byMethod {
		methods = append(methods, exportMethod{
			name: name, method: method,
			aborted:     method.aborted,
			timedOut:    method.timedOut,
			cutOff:      method.cutOff,
			unanswered:  method.unanswered,
			unknown:     method.unknown,
			clientError: method.clientError,

			notSentGenerator:  method.notSentGenerator,
			notSentStream:     method.notSentStream,
			notSentConnection: method.notSentConnection,
			warmupSent:        method.warmupSent,
		})
	}
	s.mu.Unlock()

	slices.SortFunc(methods, func(a, b exportMethod) int { return strings.Compare(a.name, b.name) })

	dst.Started = true
	dst.Warmup = warmup
	dst.InFlight = 0

	if cap(dst.Methods) < len(methods) {
		grown := make([]ExportMethod, len(methods))
		copy(grown, dst.Methods)
		dst.Methods = grown
	} else {
		dst.Methods = dst.Methods[:len(methods)]
	}

	for i := range methods {
		src := &methods[i]
		m := &dst.Methods[i]

		m.Name = src.name
		m.PlannedRPS = 0
		if !ended && (!sendingEnded || elapsed < sendingEnd) {
			if call := callOf(calls, src.name); call != nil {
				m.PlannedRPS = rateAt(call.Stages, elapsed)
			}
		}

		if cap(m.Cumulative) < len(bounds) {
			m.Cumulative = make([]int64, len(bounds))
		} else {
			m.Cumulative = m.Cumulative[:len(bounds)]
		}
		src.method.latency.CopyInto(buf)
		m.Count, m.Sum = buf.Cumulative(bounds, m.Cumulative)

		m.Calls = [ExportCategories]int64{}
		m.Calls[ExportSuccess] = m.Count
		m.Calls[ExportRequestError] = src.method.rejected.Count()
		m.Calls[ExportOverload] = src.method.overload.Count()
		m.Calls[ExportFailure] = src.method.failure.Count()
		m.Calls[ExportTimedOut] = int64(src.timedOut)
		m.Calls[ExportCutOff] = int64(src.cutOff)
		m.Calls[ExportUnreachable] = int64(src.unanswered)
		m.Calls[ExportClientError] = int64(src.clientError)
		m.Calls[ExportBadResponse] = src.method.badResponse.Count()
		m.Calls[ExportAborted] = int64(src.aborted)
		m.Calls[ExportUnclassified] = int64(src.unknown)

		m.NotSent = [ExportCauses]int64{
			ExportCauseGenerator:  int64(src.notSentGenerator),
			ExportCauseConnection: int64(src.notSentConnection),
			ExportCauseStream:     int64(src.notSentStream),
		}
		m.WarmupSent = int64(src.warmupSent)
	}
}

// callOf is the call of the plan for method, or nil when the plan has none.
func callOf(calls []Call, method string) *Call {
	for i := range calls {
		if calls[i].Method == method {
			return &calls[i]
		}
	}

	return nil
}

// ExportInto is Stats.ExportInto with the plan of this engine's calls and the
// number of calls in flight.
func (e *Engine) ExportInto(dst *ExportView, buf *metrics.Buffer, bounds []time.Duration) {
	e.stats.ExportInto(dst, buf, bounds, e.opts.Calls)
	dst.InFlight = int64(e.pool.inFlightCount())
}
