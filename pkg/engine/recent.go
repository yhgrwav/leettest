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
)

// Recent is one method's calls that finished in a window of whole seconds
// near the end of the run: Total is the calls that count, Failed those of
// them that failed. A call counts when it went out and was not aborted by us;
// it failed when its category is not success. A timeout whose request never
// went out and an abort are in neither: they say nothing of the target.
type Recent struct {
	Method string
	Failed int
	Total  int
}

// Recent reads the last seconds whole seconds of the run before now, by the
// second a call finished in, for every method with a call in that window, by
// method name. The second now is in is not whole and is left out; fewer than
// seconds seconds into the run, the window is those there are. Ramp and warm-up
// seconds are in it. The window is cut to the reserved span the way Record
// cuts a call: a second past it holds nothing, so a window wholly past it, a
// now before the start, a run that has not started and seconds of zero or
// less all give nothing. It appends to dst[:0], so a caller that keeps dst
// reads without allocating, and it reads only the window under the lock, not
// the run.
func (s *Stats) Recent(dst []Recent, now time.Time, seconds int) []Recent {
	dst = dst[:0]
	if seconds <= 0 {
		return dst
	}

	s.mu.Lock()

	if s.startedAt.IsZero() {
		s.mu.Unlock()
		return dst
	}

	// The whole seconds before now are [to-seconds, to); seconds can be as
	// large as an int, so it is subtracted from to and not added to from.
	to := int(now.Sub(s.startedAt) / time.Second)
	from := to - seconds

	for name, m := range s.byMethod {
		if failed, total := m.timeline.recent(from, to); total > 0 {
			dst = append(dst, Recent{Method: name, Failed: failed, Total: total})
		}
	}

	s.mu.Unlock()

	slices.SortFunc(dst, func(a, b Recent) int { return strings.Compare(a.Method, b.Method) })

	return dst
}

// since is the time from the start of the run to now, zero before it.
func (s *Stats) since(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.startedAt.IsZero() {
		return 0
	}

	return now.Sub(s.startedAt)
}

// Recent is Stats.Recent of the run.
func (e *Engine) Recent(dst []Recent, now time.Time, seconds int) []Recent {
	return e.stats.Recent(dst, now, seconds)
}

// Progress is what the run knows of itself that a stop line prints.
type Progress struct {
	// Elapsed is the time since the run started.
	Elapsed time.Duration
	// InFlight is the calls begun and not finished. There is no count of calls
	// begun: the engine keeps none and the send loop gains none for a line.
	InFlight int
}

// Progress reads the run at now.
func (e *Engine) Progress(now time.Time) Progress {
	return Progress{Elapsed: e.stats.since(now), InFlight: e.pool.inFlightCount()}
}
