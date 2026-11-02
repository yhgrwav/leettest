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

// lagP is the percentile the early warning judges the start lag by, the p99 of
// the report.
const lagP = 0.99

// lagSecond is the counters of one second of the run, by the second a call was
// done in: the moment the generator finished with it or gave up on it.
type lagSecond struct {
	calls, starts, late, unsent int
}

// countLag files r in the second it was done in. The caller holds s.mu. A call
// done before the start or past the reserved span is left out whole, as the
// timeline leaves it.
func (s *Stats) countLag(r Result) {
	if s.startedAt.IsZero() {
		return
	}

	done := r.DoneAt.Sub(s.startedAt)
	if done < 0 || done/time.Second >= time.Duration(len(s.lag)) {
		return
	}

	sec := &s.lag[done/time.Second]
	sec.calls++

	if lag := r.QueueTime(); lag >= 0 {
		sec.starts++

		if lag > s.floor {
			sec.late++
		}
	}

	if r.NotSent && notSentCause(r) == BlockedOnGenerator {
		sec.unsent++
	}
}

// LagWindow is the last whole seconds of the run as the generator lived them:
// how many calls started, how many of those started over the wait floor late,
// and how many the generator itself left unsent. Warm-up calls are in it.
type LagWindow struct {
	// Seconds is the whole seconds the window covers; fewer than asked for
	// early in the run.
	Seconds int
	// End is the time from the start of the run to the end of the window.
	End time.Duration
	// Warmup is set when the window ends within the warm-up.
	Warmup bool
	// Floor is the wait floor of the run, the one "late" is measured over.
	Floor time.Duration
	// Calls is every call recorded for the window's seconds, a start on record
	// or not.
	Calls int
	// Starts is the calls with a start on record, unsent ones included.
	Starts int
	// Late is the starts whose lag was over Floor.
	Late int
	// NotSentGenerator is the calls left unsent with the generator to blame.
	NotSentGenerator int
	// Scheduled is the rate the plan asked for in the window, the highest of
	// its stages summed over the methods. Only the engine knows it.
	Scheduled int
}

// Behind reports whether the generator was behind its schedule in the window:
// p99 of the start lag over the floor (rule #73, at least metrics.MinCount
// starts), or any call left unsent by the generator.
func (w LagWindow) Behind() bool {
	if w.NotSentGenerator > 0 {
		return true
	}

	starts := int64(w.Starts)

	return starts >= metrics.MinCount(lagP) && int64(w.Late) >= metrics.MinAbove(lagP, starts)
}

// LagWindow reads the window of the last seconds whole seconds before now, by
// the second a call was done in. The second now is in is not whole and is left
// out; fewer than seconds seconds into the run, the window is those there are.
// Warm-up seconds are in it. A run that has not started, seconds of zero or
// less, and a now before the first whole second give an empty window. Scheduled
// stays 0: only Engine.LagWindow knows the plan.
func (s *Stats) LagWindow(now time.Time, seconds int) LagWindow {
	if seconds <= 0 {
		return LagWindow{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.startedAt.IsZero() {
		return LagWindow{}
	}

	// The whole seconds before now are [to-seconds, to); seconds can be as
	// large as an int, so it is subtracted from to and not added to from.
	to := int(now.Sub(s.startedAt) / time.Second)
	if to <= 0 {
		return LagWindow{}
	}

	from := max(to-seconds, 0)
	end := time.Duration(to) * time.Second
	w := LagWindow{Seconds: to - from, End: end, Warmup: end <= s.warmup, Floor: s.floor}

	for i := from; i < min(to, len(s.lag)); i++ {
		sec := s.lag[i]
		w.Calls += sec.calls
		w.Starts += sec.starts
		w.Late += sec.late
		w.NotSentGenerator += sec.unsent
	}

	return w
}

// LagWindow is Stats.LagWindow of the run with Scheduled filled in: the rate
// the plan asked for over the window's seconds, the highest of each method's
// stages that overlap it (ratesOver), summed over the methods. On a ramp that is
// the rate at the window's far end, not the stage's target.
func (e *Engine) LagWindow(now time.Time, seconds int) LagWindow {
	w := e.stats.LagWindow(now, seconds)
	if w.Seconds == 0 {
		return w
	}

	from := w.End - time.Duration(w.Seconds)*time.Second
	for _, call := range e.opts.Calls {
		_, high := ratesOver(call.Stages, from, w.End)
		w.Scheduled += high
	}

	return w
}
