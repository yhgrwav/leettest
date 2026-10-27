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
// second a call finished in, for every method that has recorded anything, by
// method name. The second now is in is not whole and is left out; fewer than
// seconds seconds into the run, the window is those there are. Ramp and warm-up
// seconds are in it. It appends to dst[:0], so a caller that keeps dst reads
// without allocating, and it reads only the window under the lock, not the
// run.
func (s *Stats) Recent(dst []Recent, _ time.Time, _ int) []Recent {
	return dst[:0]
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
func (e *Engine) Progress(_ time.Time) Progress {
	return Progress{}
}
