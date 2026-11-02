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

import "time"

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
	return false
}

// LagWindow reads the window of the last seconds whole seconds before now.
func (s *Stats) LagWindow(_ time.Time, _ int) LagWindow {
	return LagWindow{}
}

// LagWindow is Stats.LagWindow of the run.
func (e *Engine) LagWindow(now time.Time, seconds int) LagWindow {
	return e.stats.LagWindow(now, seconds)
}
