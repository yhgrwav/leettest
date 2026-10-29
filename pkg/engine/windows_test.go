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
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/metrics"
)

// windowed is a Stats asked for windows over a measured part of measured after
// a warm-up, set up as Run does it: windows asked for, Reserve, Start, then
// the end of sending at the end of the plan.
func windowed(start time.Time, warmup, measured time.Duration) *Stats {
	stats := NewStats()
	stats.SetWindows(measured)
	stats.Reserve(warmup+measured+time.Minute, "a")
	stats.Start(start, warmup)
	stats.EndSending(start.Add(warmup + measured))

	return stats
}

func windowsOf(t *testing.T, stats *Stats) []Window {
	t.Helper()

	report := stats.Report()
	if len(report.Methods) != 1 {
		t.Fatalf("methods = %d, want 1", len(report.Methods))
	}

	return report.Methods[0].Windows
}

// inMeasured is a call scheduled at, counted from the end of the warm-up, that
// finished took later.
func inMeasured(start time.Time, warmup, at, took time.Duration, category Category) Result {
	return call(start, warmup+at, warmup+at+took, category)
}

// withinAPercent compares two durations the way a window's two significant
// digits allow.
func withinAPercent(got, want time.Duration) bool {
	return max(got-want, want-got) <= want/100
}

// Ground: contract (A5, T1) — the window is the schedule's, the finish is not.
func TestWindows_ACallBelongsToTheWindowItWasScheduledIn(t *testing.T) {
	start := time.Now()
	stats := windowed(start, 0, 120*time.Second) // 60 windows of 2 s

	// Scheduled in window 2, begun in window 4, finished in window 5.
	r := call(start, 5*time.Second, 11*time.Second, CategorySuccess)
	r.BegunAt = start.Add(9 * time.Second)
	stats.Record(r)

	got := windowsOf(t, stats)
	if len(got) != 60 {
		t.Fatalf("windows = %d, want 60", len(got))
	}
	for i, w := range got {
		want := 0
		if i == 2 {
			want = 1
		}
		if w.Sent != want || int(w.Latency.Count()) != want {
			t.Errorf("window %d: sent %d, observations %d; want %d of each (scheduled at 5 s, window 2)", i, w.Sent, w.Latency.Count(), want)
		}
	}
	if !withinAPercent(got[2].P50.Value, 6*time.Second) {
		t.Errorf("window 2 p50 = %s, want the call's 6 s counted from its schedule", got[2].P50.Value)
	}
}

// mixedRun is a mix of every way a call can end, for a run with a warm-up, and
// the number of its calls that went out after the warm-up.
func mixedRun(t *testing.T) (stats *Stats, wantSent int) {
	t.Helper()

	const (
		warmup   = 10 * time.Second
		measured = 120 * time.Second
	)

	start := time.Now()
	stats = windowed(start, warmup, measured)
	categories := []Category{
		CategorySuccess, CategorySuccess, CategoryTimeout, CategoryOverload,
		CategoryServerFault, CategoryAborted, CategoryUnreachable,
	}

	for i := range 2000 {
		scheduled := time.Duration(i) * 60 * time.Millisecond
		r := call(start, scheduled, scheduled+time.Duration(i%13+1)*10*time.Millisecond, categories[i%len(categories)])
		if i%11 == 0 {
			// A timeout that never went out: in no count that is about the target.
			r.Category, r.NotSent = CategoryTimeout, true
		}
		if scheduled >= warmup && !r.NotSent {
			wantSent++
		}
		stats.Record(r)
	}
	stats.Finish(start.Add(warmup + measured + time.Second))

	return stats, wantSent
}

// Ground: contract (A2, T2) — the windows add up to the method's totals; the
// totals are the oracle, so a boundary that loses or doubles a call shows here.
func TestWindows_TheSumOfTheWindowsIsTheMethodsTotals(t *testing.T) {
	stats, wantSent := mixedRun(t)

	m := stats.Report().Methods[0]
	if m.Sent != wantSent {
		t.Fatalf("the method's sent = %d, but the test scheduled %d: the oracle is wrong", m.Sent, wantSent)
	}
	if len(m.Windows) != 60 {
		t.Fatalf("windows = %d, want 60", len(m.Windows))
	}

	var sent, failed, withSent int
	var folded []*metrics.Snapshot
	for _, w := range m.Windows {
		sent += w.Sent
		failed += w.Failed
		if w.Sent > 0 {
			withSent++
		}
		folded = append(folded, w.Latency)
	}

	if sent != m.Sent {
		t.Errorf("sum of window sent = %d, want the method's %d", sent, m.Sent)
	}
	if failed != m.Failed {
		t.Errorf("sum of window failed = %d, want the method's %d", failed, m.Failed)
	}
	if got := metrics.Merge(folded...); int(got.Count()) != m.Latencies || int(got.CensoredCount()) != m.Censored {
		t.Errorf("windows' observations %d (%d censored), want the method's %d (%d censored)",
			got.Count(), got.CensoredCount(), m.Latencies, m.Censored)
	}
	if m.OutsideWindows != 0 {
		t.Errorf("outside the windows = %d, want 0 in a normal run", m.OutsideWindows)
	}
	if withSent < 55 {
		t.Errorf("%d of 60 windows hold calls, want the load spread over nearly all", withSent)
	}
}

// Ground: contract (A5, T3) — the width rounds up, so 60 windows always reach
// the end; and a window is never under a second.
func TestWindows_WidthRoundsUpToANanosecondAndIsAtLeastASecond(t *testing.T) {
	cases := []struct {
		measured time.Duration
		want     time.Duration
	}{
		{500 * time.Millisecond, time.Second},
		{30 * time.Second, time.Second},
		{60 * time.Second, time.Second},
		{61 * time.Second, 1016666667},
		{100 * time.Second, 1666666667},
		{100*time.Second + 1, 1666666667},
		{time.Hour, time.Minute},
		{time.Hour + 1, 60000000001},
	}
	for _, c := range cases {
		if got := WindowWidth(c.measured); got != c.want {
			t.Errorf("WindowWidth(%s) = %d ns, want %d", c.measured, got, int64(c.want))
		}
	}
}

// Ground: boundary (A5, T3) — durations that do not divide by 60, the last
// nanosecond, and the ones the width floor reshapes.
func TestWindows_TheLastNanosecondIsInTheLastWindowAndTheCountNeverPassesSixty(t *testing.T) {
	cases := []struct {
		measured time.Duration
		windows  int
	}{
		{500 * time.Millisecond, 1},
		{1500 * time.Millisecond, 2},
		{30 * time.Second, 30},
		{60 * time.Second, 60},
		{61 * time.Second, 60},
		{100 * time.Second, 60},
		{100*time.Second + 1, 60},
		{time.Hour, 60},
		{time.Hour + 1, 60},
	}
	for _, c := range cases {
		t.Run(c.measured.String(), func(t *testing.T) {
			start := time.Now()
			stats := windowed(start, 0, c.measured)
			stats.Record(call(start, c.measured-1, c.measured, CategorySuccess))

			m := stats.Report().Methods[0]
			if len(m.Windows) != c.windows {
				t.Fatalf("windows = %d, want %d", len(m.Windows), c.windows)
			}
			if m.OutsideWindows != 0 {
				t.Errorf("outside = %d: the call one nanosecond before the end is inside the plan", m.OutsideWindows)
			}

			width := WindowWidth(c.measured)
			var covered time.Duration
			for i, w := range m.Windows {
				want := 0
				if i == len(m.Windows)-1 {
					want = 1
				}
				if w.Sent != want {
					t.Errorf("window %d sent = %d, want %d: the last nanosecond is in the last window", i, w.Sent, want)
				}
				if w.Start != time.Duration(i)*width {
					t.Errorf("window %d starts at %d ns, want %d", i, int64(w.Start), int64(time.Duration(i)*width))
				}
				covered += w.Width
			}
			// Every window is a full width but the last, which is what is left.
			if covered != c.measured {
				t.Errorf("widths add up to %d ns, want the measured part's %d", int64(covered), int64(c.measured))
			}
		})
	}
}

// Ground: boundary (A5, T3) — a moment past the plan; clamping it into the
// last window would be a silent distortion.
func TestWindows_ACallPastTheEndIsCountedAsideNotMovedIntoTheLastWindow(t *testing.T) {
	start := time.Now()
	stats := windowed(start, 0, 10*time.Second)

	stats.Record(call(start, 10*time.Second, 10*time.Second+time.Millisecond, CategorySuccess))
	stats.Record(call(start, 40*time.Second, 40*time.Second+time.Millisecond, CategorySuccess))

	m := stats.Report().Methods[0]
	if m.OutsideWindows != 2 {
		t.Errorf("outside = %d, want 2", m.OutsideWindows)
	}
	for i, w := range m.Windows {
		if w.Sent != 0 {
			t.Errorf("window %d sent = %d, want 0: a call past the end is in no window", i, w.Sent)
		}
	}
	if m.Sent != 2 {
		t.Errorf("the method's sent = %d, want 2: the windows never change the totals", m.Sent)
	}
}

// Ground: contract (A4, T4) — the warm-up is in no window and the axis starts
// after it.
func TestWindows_WarmupIsInNoWindowAndWindowZeroStartsWhereItEnds(t *testing.T) {
	start := time.Now()
	const warmup = 5 * time.Second
	stats := windowed(start, warmup, 10*time.Second) // 10 windows of 1 s

	for _, at := range []time.Duration{0, 2 * time.Second, 4999 * time.Millisecond} {
		stats.Record(call(start, at, at+10*time.Millisecond, CategoryServerFault))
	}
	for _, at := range []time.Duration{0, 500 * time.Millisecond, 9999 * time.Millisecond} {
		stats.Record(inMeasured(start, warmup, at, 10*time.Millisecond, CategorySuccess))
	}

	m := stats.Report().Methods[0]
	if len(m.Windows) != 10 {
		t.Fatalf("windows = %d, want 10 over the measured part alone", len(m.Windows))
	}
	if m.Windows[0].Start != 0 {
		t.Errorf("window 0 starts at %s, want 0: the axis starts where the warm-up ends", m.Windows[0].Start)
	}

	want := map[int]int{0: 2, 9: 1}
	var sent, failed int
	for i, w := range m.Windows {
		sent += w.Sent
		failed += w.Failed
		if w.Sent != want[i] {
			t.Errorf("window %d sent = %d, want %d", i, w.Sent, want[i])
		}
	}
	if sent != 3 || failed != 0 {
		t.Errorf("windows hold sent %d failed %d, want 3 and 0: the three warm-up failures are in none", sent, failed)
	}
}

// Ground: contract (A5, T5) — a stop leaves the windows after it out and the
// last one as wide as what was covered.
func TestWindows_AStopLeavesNoWindowsAfterItAndTheLastOneIsAsWideAsWhatWasCovered(t *testing.T) {
	start := time.Now()
	const warmup = 2 * time.Second
	stats := windowed(start, warmup, 10*time.Second) // planned 10 windows of 1 s

	// The schedule stopped 3.5 s into the measured part; the earliest end counts.
	stats.EndSending(start.Add(warmup + 3500*time.Millisecond))
	stats.Record(inMeasured(start, warmup, 3200*time.Millisecond, 800*time.Millisecond, CategorySuccess))

	got := windowsOf(t, stats)
	if len(got) != 4 {
		t.Fatalf("windows = %d, want 4: the ones after the stop are absent", len(got))
	}
	for i, w := range got[:3] {
		if w.Width != time.Second {
			t.Errorf("window %d is %s wide, want the planned 1 s", i, w.Width)
		}
	}
	if got[3].Width != 500*time.Millisecond {
		t.Errorf("last window is %s wide, want the 500 ms that were covered", got[3].Width)
	}
	if got[3].Sent != 1 {
		t.Errorf("last window sent = %d, want the call that was scheduled in it", got[3].Sent)
	}
}

// Ground: boundary (A5) — the stop comes before the measured part starts.
// Green on the stub: nothing is built yet, and nothing should be then.
func TestWindows_AStopInsideTheWarmupHasNoWindows(t *testing.T) {
	start := time.Now()
	const warmup = 2 * time.Second
	stats := windowed(start, warmup, 10*time.Second)
	stats.EndSending(start.Add(time.Second))

	m := stats.Report().Methods[0]
	if len(m.Windows) != 0 || m.OutsideWindows != 0 {
		t.Errorf("windows %d, outside %d; want neither: nothing was measured", len(m.Windows), m.OutsideWindows)
	}
}

// Ground: contract (A5, T5) — a window with no calls in the middle is a signal.
func TestWindows_AWindowWithNoCallsInTheMiddleStaysWithNoPercentiles(t *testing.T) {
	start := time.Now()
	stats := windowed(start, 0, 4*time.Second)

	stats.Record(call(start, 500*time.Millisecond, 510*time.Millisecond, CategorySuccess))
	stats.Record(call(start, 2500*time.Millisecond, 2510*time.Millisecond, CategorySuccess))

	got := windowsOf(t, stats)
	if len(got) != 4 {
		t.Fatalf("windows = %d, want 4: an empty window is not skipped", len(got))
	}
	for _, i := range []int{1, 3} {
		w := got[i]
		if w.Sent != 0 || w.Latency.Count() != 0 || w.P50.Defined || w.P99.Defined {
			t.Errorf("window %d = %+v, want no calls and no percentiles", i, w)
		}
	}
	if got[2].Start != 2*time.Second || got[2].Sent != 1 {
		t.Errorf("window 2 starts at %s with sent %d, want 2 s and 1: the empty window keeps its place", got[2].Start, got[2].Sent)
	}
}

// Ground: contract (T6) — a call cut off by its timeout is a lower bound in
// its window, as in the totals (#73).
func TestWindows_ACensoredCallMakesOnlyItsWindowsP99ALowerBound(t *testing.T) {
	start := time.Now()
	stats := windowed(start, 0, 4*time.Second)

	for _, at := range []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond} {
		stats.Record(call(start, at, at+10*time.Millisecond, CategorySuccess))
	}
	stats.Record(call(start, 1100*time.Millisecond, 1110*time.Millisecond, CategorySuccess))
	stats.Record(call(start, 1200*time.Millisecond, 1250*time.Millisecond, CategoryTimeout))

	got := windowsOf(t, stats)
	if len(got) != 4 {
		t.Fatalf("windows = %d, want 4", len(got))
	}
	if !got[0].P99.Defined || !got[0].P99.Exact {
		t.Errorf("window 0 p99 = %+v, want exact: nothing in it was cut off", got[0].P99)
	}
	if !got[1].P99.Defined || got[1].P99.Exact || got[1].Latency.CensoredCount() != 1 {
		t.Errorf("window 1 p99 = %+v with %d censored, want a lower bound from the one cut-off call", got[1].P99, got[1].Latency.CensoredCount())
	}
	if got[1].P99.Value < 49*time.Millisecond {
		t.Errorf("window 1 p99 bound = %s, want the cut-off call's 50 ms", got[1].P99.Value)
	}
}

// Ground: contract (A1, T8) — the windows are display only: with and without
// them every number but the windows' own is the same, though a call fell
// outside them. Green on the stub; it holds the line for the code.
func TestWindows_TheTotalsAreTheSameWithAndWithoutWindows(t *testing.T) {
	start := time.Now()
	const warmup, measured = 2 * time.Second, 10 * time.Second

	fill := func(withWindows bool) Report {
		stats := NewStats()
		if withWindows {
			stats.SetWindows(measured)
		}
		stats.Reserve(warmup+measured+time.Minute, "a")
		stats.Start(start, warmup)
		stats.EndSending(start.Add(warmup + measured))

		for i := range 400 {
			at := time.Duration(i) * 35 * time.Millisecond
			category := []Category{CategorySuccess, CategoryTimeout, CategoryOverload}[i%3]
			stats.Record(call(start, at, at+time.Duration(i%7+1)*time.Millisecond, category))
		}
		// Past the end of the plan: in the totals, in no window.
		stats.Record(call(start, warmup+measured, warmup+measured+time.Millisecond, CategorySuccess))
		stats.Finish(start.Add(warmup + measured + time.Second))

		return stats.Report()
	}

	with, without := fill(true), fill(false)
	for i := range with.Methods {
		with.Methods[i].Windows, with.Methods[i].OutsideWindows = nil, 0
	}
	if !reflect.DeepEqual(with, without) {
		t.Errorf("the report with windows differs from the one without in more than the windows:\nwith:    %+v\nwithout: %+v", with, without)
	}
}

// Ground: contract (A6, T9) — nothing asked, nothing held.
func TestWindows_AStatsNobodyAskedForHoldsNoneAndRecordsNoneOnTheHotPath(t *testing.T) {
	start := time.Now()
	stats := NewStats()
	stats.Reserve(time.Hour, "a")
	stats.Start(start, 0)

	if stats.WindowBytes() != 0 {
		t.Errorf("window bytes = %d before any call, want 0", stats.WindowBytes())
	}
	stats.Record(call(start, 500*time.Millisecond, 600*time.Millisecond, CategorySuccess))

	m := stats.Report().Methods[0]
	if stats.WindowBytes() != 0 || len(m.Windows) != 0 || m.OutsideWindows != 0 {
		t.Errorf("bytes %d, windows %d, outside %d; want none of them", stats.WindowBytes(), len(m.Windows), m.OutsideWindows)
	}
}

// Ground: contract (A6, T9) — Options.Windows is the switch Run reads; a step
// of a breakpoint search leaves it off.
func TestEngine_WindowsAreInTheReportOnlyWhenTheOptionAsksForThem(t *testing.T) {
	run := func(windows bool, stage time.Duration) Report {
		eng, err := New(Options{
			Calls: []Call{{Method: "a", Timeout: 100 * time.Millisecond, Stages: []Stage{
				{StartRPS: 20, TargetRPS: 20, Duration: stage},
			}}},
			Sender:      FakeSender{Delay: time.Millisecond},
			MaxInFlight: 100,
			Warmup:      500 * time.Millisecond,
			Windows:     windows,
		})
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		if err := eng.Run(t.Context()); err != nil {
			t.Fatalf("run: %v", err)
		}

		return eng.Report()
	}

	if got := run(false, 600*time.Millisecond).Methods[0].Windows; len(got) != 0 {
		t.Errorf("windows without the option = %d, want 0", len(got))
	}

	m := run(true, 2*time.Second).Methods[0]
	// 1.5 s measured: a window of 1 s and the 0.5 s that is left.
	if len(m.Windows) != 2 {
		t.Fatalf("windows with the option = %d, want 2 over the 1.5 s after the warm-up", len(m.Windows))
	}
	if m.Windows[0].Start != 0 || m.Windows[1].Start != time.Second || m.Windows[1].Width != 500*time.Millisecond {
		t.Errorf("windows %+v and %+v, want one at 0 and one at 1 s that is 500 ms wide", m.Windows[0], m.Windows[1])
	}
	if sum := m.Windows[0].Sent + m.Windows[1].Sent; sum != m.Sent || m.Sent == 0 {
		t.Errorf("windows hold %d sent, want the method's %d", sum, m.Sent)
	}
}

// Ground: contract (K1, A4, A5) — end to end through Run: two methods with
// plans of different length, one of them a ramp and a constant stage. The axis
// and the width are the run's, shared by both; a method's windows stop where
// its own plan stops, so a method that merely ended early has no invented
// "no calls" tail. Mutation: the short method's windows run to the end of the
// longest plan (three windows, the last one empty).
func TestEngine_AShorterMethodsWindowsEndWhereItsOwnPlanEnds(t *testing.T) {
	const warmup = 500 * time.Millisecond

	eng, err := New(Options{
		Calls: []Call{
			{Method: "long", Timeout: 100 * time.Millisecond, Stages: []Stage{
				{StartRPS: 10, TargetRPS: 30, Duration: time.Second},
				{StartRPS: 30, TargetRPS: 30, Duration: 2 * time.Second},
			}},
			{Method: "short", Timeout: 100 * time.Millisecond, Stages: []Stage{
				{StartRPS: 20, TargetRPS: 20, Duration: 1700 * time.Millisecond},
			}},
		},
		Sender:      FakeSender{Delay: time.Millisecond, FailRatio: 0.3},
		MaxInFlight: 200,
		Warmup:      warmup,
		Windows:     true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	byName := map[string]MethodReport{}
	for _, m := range eng.Report().Methods {
		byName[m.Method] = m
	}

	// The longest plan is 3 s; 2.5 s of it is measured, so a window is 1 s.
	// long covers all of it: 1 s, 1 s and the 0.5 s that are left. short plans
	// 1.7 s, 1.2 s of it measured: 1 s and the 0.2 s that are left.
	for name, want := range map[string][]time.Duration{
		"long":  {time.Second, time.Second, 500 * time.Millisecond},
		"short": {time.Second, 200 * time.Millisecond},
	} {
		m := byName[name]
		if len(m.Windows) != len(want) {
			t.Fatalf("%s: windows = %d, want %d", name, len(m.Windows), len(want))
		}

		var sent, failed int
		var folded []*metrics.Snapshot
		for i, w := range m.Windows {
			if w.Start != time.Duration(i)*time.Second || w.Width != want[i] {
				t.Errorf("%s window %d: start %s width %s, want %s and %s: one axis and one width for the run, the last window as wide as what was covered",
					name, i, w.Start, w.Width, time.Duration(i)*time.Second, want[i])
			}
			sent += w.Sent
			failed += w.Failed
			folded = append(folded, w.Latency)
		}

		if sent != m.Sent || failed != m.Failed {
			t.Errorf("%s: windows hold sent %d failed %d, want the method's %d and %d", name, sent, failed, m.Sent, m.Failed)
		}
		if got := metrics.Merge(folded...); int(got.Count()) != m.Latencies || int(got.CensoredCount()) != m.Censored {
			t.Errorf("%s: windows' observations %d (%d censored), want the method's %d (%d censored)",
				name, got.Count(), got.CensoredCount(), m.Latencies, m.Censored)
		}
		if m.OutsideWindows != 0 {
			t.Errorf("%s: outside the windows = %d, want 0", name, m.OutsideWindows)
		}
	}

	// The plan puts the calls in the windows: long ramps 10 to 30 rps in its
	// first second, 0.5 s of which is warm-up, so its first window is thinner
	// than its second; short's last window holds 0.2 s of 20 rps. The schedule
	// is the plan's, so these counts do not wait on the machine.
	long, short := byName["long"].Windows, byName["short"].Windows
	if long[0].Sent < 25 || long[0].Sent > 29 || long[1].Sent < 28 || long[1].Sent > 32 || long[0].Sent >= long[1].Sent || long[2].Sent < 13 || long[2].Sent > 17 {
		t.Errorf("long sent per window = %d, %d, %d; want about 27, 30, 15, the first thinner than the second", long[0].Sent, long[1].Sent, long[2].Sent)
	}
	if short[0].Sent < 19 || short[0].Sent > 21 || short[1].Sent < 3 || short[1].Sent > 5 {
		t.Errorf("short sent per window = %d, %d; want about 20 and 4", short[0].Sent, short[1].Sent)
	}
	if byName["long"].Failed == 0 {
		t.Error("long failed nothing: the test's own 30% failures did not reach the totals")
	}
}

// Ground: contract (A6, T10) — the memory is taken once, at Reserve, and it
// is fixed by the number of methods alone. The number is pinned, not described.
func TestWindows_MemoryIsTakenAtReserveAndBoundedByWindowsAndMethods(t *testing.T) {
	const perHistogram = 37_000 // the bound of a window's one histogram, bytes

	for _, methods := range []int{1, 10} {
		t.Run(fmt.Sprintf("%d methods", methods), func(t *testing.T) {
			names := make([]string, methods)
			for i := range names {
				names[i] = fmt.Sprintf("m%d", i)
			}

			start := time.Now()
			stats := NewStats()
			stats.SetWindows(10 * time.Minute)
			stats.Reserve(11*time.Minute, names...)
			stats.Start(start, 0)
			stats.EndSending(start.Add(10 * time.Minute))

			before := stats.WindowBytes()
			if bound := MaxWindows * methods * 2 * perHistogram; before <= 0 || before > bound {
				t.Fatalf("window bytes after Reserve = %d, want in (0, %d]", before, bound)
			}

			for i := range 600 {
				at := time.Duration(i) * time.Second
				stats.Record(Result{
					Method: names[i%methods], ScheduledAt: start.Add(at), BegunAt: start.Add(at),
					Outcome: Outcome{SentAt: start.Add(at), DoneAt: start.Add(at + time.Millisecond), Category: CategorySuccess},
				})
			}
			if after := stats.WindowBytes(); after != before {
				t.Errorf("window bytes %d after the calls, %d before: nothing may be allocated on the hot path", after, before)
			}

			for _, m := range stats.Report().Methods {
				if len(m.Windows) != MaxWindows {
					t.Errorf("%s: windows = %d, want %d", m.Method, len(m.Windows), MaxWindows)
				}
			}
		})
	}
}

// CI runs no benchmarks, so the zero of the benchmark is held here.
// Ground: hot path (A8, T12) — the control is that the call reached a window;
// zero allocations with no window written would prove nothing.
func TestWindows_RecordDoesNotAllocateAndReachesTheWindow(t *testing.T) {
	start := time.Now()
	stats := NewStats()
	stats.SetWindows(time.Minute)
	stats.Reserve(2*time.Minute, "a")
	stats.Start(start, 0)
	stats.EndSending(start.Add(time.Minute))

	ok := call(start, 500*time.Millisecond, 600*time.Millisecond, CategorySuccess)
	cut := call(start, 700*time.Millisecond, 900*time.Millisecond, CategoryTimeout)

	const runs = 1000
	if allocs := testing.AllocsPerRun(runs, func() {
		stats.Record(ok)
		stats.Record(cut)
	}); allocs != 0 {
		t.Errorf("Record allocates %v times per call, want 0", allocs)
	}

	got := windowsOf(t, stats)
	if len(got) == 0 {
		t.Fatal("no windows: the calls had nowhere to be counted")
	}
	if got[0].Sent != 2*(runs+1) || got[0].Latency.CensoredCount() != runs+1 {
		t.Errorf("window 0 = %+v, want the %d calls recorded under test in it, %d of them cut off", got[0], 2*(runs+1), runs+1)
	}
}
