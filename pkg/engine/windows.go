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

// MaxWindows is the most windows a method has, however long the run.
const MaxWindows = 60

// MinWindowWidth is the narrowest a window gets.
const MinWindowWidth = time.Second

// Window is one slice of the measured part of a run for one method: the calls
// scheduled in it, counted by their ScheduledAt whenever they finished. Its
// numbers are display only; no total, threshold, verdict or comparison reads
// them.
type Window struct {
	// Start is counted from the end of the warm-up, the zero of the windows'
	// axis; the per-second timeline counts from the start of the run.
	Start time.Duration
	// Width is the span of the run the window covers: the planned width, but
	// for the last window of a run that stopped early or does not divide into
	// whole windows, where it is what was actually covered.
	Width time.Duration
	// Sent and Failed are the calls scheduled in the window that went out, and
	// those of them the totals call failed: the same definitions as the
	// method's Sent and Failed, from the window's own counters.
	Sent   int
	Failed int
	// Latency holds the window's measured and censored observations, for
	// folding neighbouring windows into one row; never nil in a window.
	Latency *metrics.Snapshot
	// P50 and P99 are read from Latency; not Defined in a window without
	// observations.
	P50 metrics.Quantile
	P99 metrics.Quantile
}

// WindowWidth is the width of one window of a run whose measured part is
// planned to last measured: measured/MaxWindows rounded up to a whole
// nanosecond, but not under MinWindowWidth. Rounded up, so that the call on
// the last nanosecond is not given a 61st window.
//
// Stub: returns 0 until the windows are implemented.
func WindowWidth(measured time.Duration) time.Duration {
	_ = measured

	return 0
}

// SetWindows asks for windows over a measured part planned to last measured
// (the planned duration without the warm-up). Call it before Reserve: the
// histograms of every window are allocated there in one piece, never on the
// hot path. Without the call a Stats has no windows and its methods allocate
// none.
//
// Stub: does nothing until the windows are implemented.
func (s *Stats) SetWindows(measured time.Duration) {
	_ = measured
}

// WindowBytes is the memory held by the windows' histograms, in bytes; 0 when
// they are off.
//
// Stub: always 0.
func (s *Stats) WindowBytes() int {
	return 0
}
