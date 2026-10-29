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

// window is one slice of a method's measured part. Its counters are guarded
// by Stats.mu; its distribution has a lock of its own, so writing a histogram
// never holds Stats.mu.
type window struct {
	sent, failed int
	latency      *metrics.Latencies
}

// windowView is what Report copies of one window under Stats.mu, the
// distribution still to be snapshotted.
type windowView struct {
	start, width time.Duration
	sent, failed int
	latency      *metrics.Latencies
}

// methodWindowView is a method's windows and its count of calls that fell
// outside them.
type methodWindowView struct {
	windows []windowView
	outside int
}

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
func WindowWidth(measured time.Duration) time.Duration {
	width := (measured + MaxWindows - 1) / MaxWindows

	return max(width, MinWindowWidth)
}

// SetWindows asks for windows over a measured part planned to last measured
// (the planned duration without the warm-up). Call it before Reserve: the
// histograms of every window are allocated there in one piece, never on the
// hot path. Without the call a Stats has no windows and its methods allocate
// none.
func (s *Stats) SetWindows(measured time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if measured <= 0 {
		s.windowWidth, s.windowCount = 0, 0

		return
	}

	s.windowWidth = WindowWidth(measured)
	s.windowCount = int((measured + s.windowWidth - 1) / s.windowWidth)
	s.windowMeasured = measured
}

// planWalked says the schedule handed out the whole plan. The windows then
// cover all of the measured part, though the last tick fell short of the plan
// and the drain ended before it; the totals' sending end is not touched.
func (s *Stats) planWalked() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.planDone = true
}

// setMethodPlan fixes where a method's windows stop on the shared axis: the
// measured part of its own load plan. A method without a plan keeps the whole
// measured part.
func (s *Stats) setMethodPlan(method string, planned time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.windowPlans == nil {
		s.windowPlans = make(map[string]time.Duration)
	}
	s.windowPlans[method] = max(0, planned)
}

// allocateWindows takes the memory of every window of one method in one piece.
// Callers hold Stats.mu; without windows it allocates nothing.
func (s *Stats) allocateWindows(method *methodStats) {
	if s.windowWidth <= 0 || s.windowCount <= 0 {
		return
	}

	method.windows = make([]window, s.windowCount)
	for i := range method.windows {
		method.windows[i].latency = metrics.NewWindowLatencies()
	}
}

// WindowBytes is the memory held by the windows' histograms, in bytes; 0 when
// they are off.
func (s *Stats) WindowBytes() int {
	s.mu.Lock()
	var histograms []*metrics.Latencies

	for _, method := range s.byMethod {
		for i := range method.windows {
			histograms = append(histograms, method.windows[i].latency)
		}
	}
	s.mu.Unlock()

	total := 0
	for _, latency := range histograms {
		total += latency.ByteSize()
	}

	return total
}

// coveredMeasuredLocked is how much of the measured part the schedule covered:
// the earliest end of sending, cut at the end of the warm-up and at zero; all
// of it once the schedule walked the whole plan. Callers hold Stats.mu.
func (s *Stats) coveredMeasuredLocked() time.Duration {
	if s.startedAt.IsZero() {
		return 0
	}
	if s.planDone {
		return s.windowMeasured
	}

	end := s.elapsed()
	if !s.sendingEndedAt.IsZero() {
		end = s.sendingEndedAt.Sub(s.startedAt)
	}

	return max(0, end-s.warmup)
}

// windowViewsLocked copies every method's windows that were covered, the last
// one as wide as what was actually covered. Callers hold Stats.mu; the
// distributions are only referenced, to be snapshotted after it is released.
func (s *Stats) windowViewsLocked() map[string]methodWindowView {
	if s.windowWidth <= 0 || s.windowCount <= 0 {
		return nil
	}

	covered := s.coveredMeasuredLocked()
	out := make(map[string]methodWindowView, len(s.byMethod))

	for name, method := range s.byMethod {
		end := covered
		if planned, ok := s.windowPlans[name]; ok {
			end = min(end, planned)
		}

		view := methodWindowView{outside: method.outsideWindows}
		if end > 0 {
			n := min(int((end+s.windowWidth-1)/s.windowWidth), len(method.windows))
			view.windows = make([]windowView, n)

			for i := 0; i < n; i++ {
				start := time.Duration(i) * s.windowWidth
				width := s.windowWidth
				if i == n-1 {
					width = end - start
				}
				view.windows[i] = windowView{
					start: start, width: width,
					sent: method.windows[i].sent, failed: method.windows[i].failed,
					latency: method.windows[i].latency,
				}
			}
		}

		out[name] = view
	}

	return out
}

// windowsOfView snapshots the distributions of one method's copied windows,
// outside Stats.mu, and reads their percentiles.
func windowsOfView(view methodWindowView) []Window {
	if len(view.windows) == 0 {
		return nil
	}

	out := make([]Window, len(view.windows))
	for i, w := range view.windows {
		snapshot := w.latency.Snapshot()
		out[i] = Window{
			Start: w.start, Width: w.width, Sent: w.sent, Failed: w.failed,
			Latency: snapshot, P50: snapshot.Percentile(0.5), P99: snapshot.Percentile(0.99),
		}
	}

	return out
}
