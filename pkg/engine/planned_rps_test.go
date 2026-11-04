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
	"math"
	"testing"
	"time"
)

// plannedSeconds is the seconds of a method whose plan is stages, with one
// call in each of the given moments, and the schedule stopped at end.
func plannedSeconds(t *testing.T, stages []Stage, end time.Duration, at ...time.Duration) []Second {
	t.Helper()

	start := time.Now()
	stats := reserved(start, 0)
	stats.setMethodStages("a", stages)

	for _, moment := range at {
		stats.Record(call(start, moment, moment, CategorySuccess))
	}

	stats.EndSending(start.Add(end))

	return seconds(t, stats)
}

// everySecond is a moment inside each of the first n seconds.
func everySecond(n int) []time.Duration {
	out := make([]time.Duration, n)
	for i := range out {
		out[i] = time.Duration(i)*time.Second + 100*time.Millisecond
	}

	return out
}

func rampThenHold() []Stage {
	return []Stage{
		{StartRPS: 0, TargetRPS: 2000, Duration: 20 * time.Second},
		{StartRPS: 2000, TargetRPS: 2000, Duration: 10 * time.Second},
	}
}

// numericMean is the mean of rateAt over [from, to) by the midpoint rule with
// 1 µs steps: the reference the closed form is held to.
func numericMean(stages []Stage, from, to time.Duration) float64 {
	const step = time.Microsecond

	if to <= from {
		return 0
	}

	var sum float64

	n := 0
	for at := from; at < to; at += step {
		sum += rateAt(stages, at+step/2)
		n++
	}

	return sum / float64(n)
}

// Ground: the planned rate of a second is the area under the plan, so the
// closed form must agree with a numeric integral on every shape of plan: a
// ramp up, a ramp down, a step between stages, a stage of zero length, an
// interval inside one stage, across a boundary, and across the plan's end.
// Mutation "the rate at the interval's start" (and "at its end") turns it red.
func TestMeanRate_AgreesWithANumericReference(t *testing.T) {
	plans := map[string][]Stage{
		"ramp up then hold": rampThenHold(),
		"ramp down":         {{StartRPS: 100, TargetRPS: 50, Duration: 10 * time.Second}},
		"two ramps and a constant": {
			{StartRPS: 0, TargetRPS: 500, Duration: 5 * time.Second},
			{StartRPS: 500, TargetRPS: 1500, Duration: 5 * time.Second},
			{StartRPS: 1500, TargetRPS: 1500, Duration: 5 * time.Second},
		},
		"a step and a stage of zero length": {
			{StartRPS: 100, TargetRPS: 100, Duration: 3 * time.Second},
			{StartRPS: 7, TargetRPS: 9, Duration: 0},
			{StartRPS: 400, TargetRPS: 400, Duration: 3 * time.Second},
		},
	}

	spans := [][2]time.Duration{
		{0, time.Second},
		{time.Second, 2 * time.Second},
		{2500 * time.Millisecond, 3500 * time.Millisecond},
		{2900 * time.Millisecond, 3300 * time.Millisecond},
		{4 * time.Second, 5 * time.Second},
		{12 * time.Second, 12300 * time.Millisecond},
		{14500 * time.Millisecond, 15500 * time.Millisecond},
		{17 * time.Second, 19 * time.Second},
	}

	for name, stages := range plans {
		for _, span := range spans {
			got := meanRate(stages, span[0], span[1])
			want := numericMean(stages, span[0], span[1])

			if !within(got, want, 1e-3) {
				t.Errorf("%s: meanRate over [%v, %v) = %v, numeric reference %v", name, span[0], span[1], got, want)
			}
		}
	}
}

// Ground: boundary — nothing is offered past the plan, and an empty or
// backwards interval has no mean.
func TestMeanRate_ZeroPastThePlanAndForNoInterval(t *testing.T) {
	stages := []Stage{{StartRPS: 80, TargetRPS: 80, Duration: 4 * time.Second}}

	if got := meanRate(stages, 4*time.Second, 5*time.Second); got != 0 {
		t.Errorf("meanRate past the plan = %v, want 0", got)
	}
	if got := meanRate(stages, time.Second, time.Second); got != 0 {
		t.Errorf("meanRate over an empty interval = %v, want 0", got)
	}
	if got := meanRate(stages, 2*time.Second, time.Second); got != 0 {
		t.Errorf("meanRate over a backwards interval = %v, want 0", got)
	}
	if got := meanRate(nil, 0, time.Second); got != 0 {
		t.Errorf("meanRate of no plan = %v, want 0", got)
	}
	// Half of the interval is inside the plan: the mean is over the whole width.
	if got := meanRate(stages, 3500*time.Millisecond, 4500*time.Millisecond); !within(got, 40, 1e-9) {
		t.Errorf("meanRate across the plan's end = %v, want 40 (half the width is past the plan)", got)
	}
}

// Ground: contract — Second.PlannedRPS is public. The ramp 0 to 2000 over 20 s
// offers 100·t, so second i has the mean 100·(i+0.5) = 50·(2i+1); the constant
// stage after it offers 2000. Mutation "the rate at the second's start" (100·i)
// turns it red.
func TestTimeline_ThePlannedRateOfASecondFollowsTheRamp(t *testing.T) {
	secs := plannedSeconds(t, rampThenHold(), 30*time.Second, everySecond(30)...)

	if len(secs) != 30 {
		t.Fatalf("seconds = %d, want 30", len(secs))
	}

	for i, s := range secs {
		want := 2000.0
		if i < 20 {
			want = 50 * float64(2*i+1)
		}

		if !within(s.PlannedRPS, want, 1e-9) {
			t.Errorf("second %d planned %v, want %v", i, s.PlannedRPS, want)
		}
	}
}

// Ground: V5 — the last, partial second's mean is over the part the schedule
// covered: a run of 20.5 s offered 2000 in its last half-second, not 1000.
func TestTimeline_ThePartialLastSecondIsAveragedOverWhatItCovered(t *testing.T) {
	secs := plannedSeconds(t, rampThenHold(), 20500*time.Millisecond, everySecond(21)...)

	if len(secs) != 21 {
		t.Fatalf("seconds = %d, want 21", len(secs))
	}
	if !within(secs[20].PlannedRPS, 2000, 1e-9) {
		t.Errorf("partial second 20 planned %v, want 2000 (the mean over [20, 20.5))", secs[20].PlannedRPS)
	}
	if !within(secs[19].PlannedRPS, 1950, 1e-9) {
		t.Errorf("second 19 planned %v, want 1950", secs[19].PlannedRPS)
	}
}

// Ground: V5 — a run stopped inside the ramp covers its last second up to the
// stop: [12, 12.3) of 100·t is 1215, not the whole second's 1250. The seconds
// after the stop, where only the drain of the calls in flight went on, have no
// plan. Mutation "the whole second's mean" (1250) turns it red.
func TestTimeline_AStoppedRunCoversItsLastSecondUpToTheStop(t *testing.T) {
	stages := []Stage{{StartRPS: 0, TargetRPS: 2000, Duration: 20 * time.Second}}
	at := append(everySecond(13), 14*time.Second+100*time.Millisecond)

	secs := plannedSeconds(t, stages, 12300*time.Millisecond, at...)

	if len(secs) != 15 {
		t.Fatalf("seconds = %d, want 15", len(secs))
	}
	if !within(secs[11].PlannedRPS, 1150, 1e-9) {
		t.Errorf("second 11 planned %v, want 1150", secs[11].PlannedRPS)
	}
	if !within(secs[12].PlannedRPS, 1215, 1e-9) {
		t.Errorf("second 12 planned %v, want 1215 (the mean over [12, 12.3))", secs[12].PlannedRPS)
	}
	for _, i := range []int{13, 14} {
		if secs[i].PlannedRPS != 0 {
			t.Errorf("second %d planned %v, want 0: the schedule had stopped", i, secs[i].PlannedRPS)
		}
	}
}

// Ground: V5 — a second past the plan's end (the drain of a finished run) has
// no planned rate, whatever the last stage was: the plan offered nothing there.
func TestTimeline_ASecondPastThePlanHasNoPlannedRate(t *testing.T) {
	stages := []Stage{{StartRPS: 50, TargetRPS: 50, Duration: 3 * time.Second}}
	at := append(everySecond(3), 4*time.Second+100*time.Millisecond, 6*time.Second+100*time.Millisecond)

	secs := plannedSeconds(t, stages, 3*time.Second, at...)

	if len(secs) != 7 {
		t.Fatalf("seconds = %d, want 7", len(secs))
	}

	for i, s := range secs {
		want := 0.0
		if i < 3 {
			want = 50
		}

		if s.PlannedRPS != want {
			t.Errorf("second %d planned %v, want %v", i, s.PlannedRPS, want)
		}
	}
}

// Ground: the planned rate is per method. Two methods with different plans
// each carry their own, and a method with no plan handed over carries none.
func TestTimeline_EachMethodCarriesItsOwnPlannedRate(t *testing.T) {
	start := time.Now()
	stats := reserved(start, 0)
	stats.setMethodStages("a", []Stage{{StartRPS: 10, TargetRPS: 10, Duration: 2 * time.Second}})
	stats.setMethodStages("b", []Stage{{StartRPS: 40, TargetRPS: 40, Duration: 2 * time.Second}})

	for _, name := range []string{"a", "b", "c"} {
		for _, at := range everySecond(2) {
			r := call(start, at, at, CategorySuccess)
			r.Method = name
			stats.Record(r)
		}
	}

	stats.EndSending(start.Add(2 * time.Second))

	want := map[string]float64{"a": 10, "b": 40, "c": 0}
	for _, m := range stats.Report().Methods {
		for i, s := range m.Seconds {
			if s.PlannedRPS != want[m.Method] {
				t.Errorf("method %s second %d planned %v, want %v", m.Method, i, s.PlannedRPS, want[m.Method])
			}
		}
	}
}

// Ground: a breakpoint step is a ramp from 0 to the step's first rate, then
// the step's rate (cmd's stepStages); its seconds carry the planned rate as
// any run's do.
func TestTimeline_ABreakpointStepCarriesItsPlannedRate(t *testing.T) {
	stages := []Stage{
		{StartRPS: 0, TargetRPS: 300, Duration: 5 * time.Second},
		{StartRPS: 300, TargetRPS: 300, Duration: 10 * time.Second},
	}

	secs := plannedSeconds(t, stages, 15*time.Second, everySecond(15)...)

	if len(secs) != 15 {
		t.Fatalf("seconds = %d, want 15", len(secs))
	}

	for i, s := range secs {
		want := 300.0
		if i < 5 {
			want = 30 * float64(2*i+1)
		}

		if !within(s.PlannedRPS, want, 1e-9) {
			t.Errorf("second %d planned %v, want %v", i, s.PlannedRPS, want)
		}
	}
}

// Ground: V3 — the planned rate and the pacer are two computations of one
// area under the rate, and the call count of a second is ceil(A(i+1)) -
// ceil(A(i)): it differs from the mean rate by less than one call. Mutation
// "the rate at the second's end instead of its mean" turns it red on the ramps.
func TestPlannedRPS_AgreesWithTheTicksThePacerPlaces(t *testing.T) {
	plans := map[string][]Stage{
		"ramp up then hold": rampThenHold(),
		"ramp down":         {{StartRPS: 100, TargetRPS: 50, Duration: 10 * time.Second}},
		"two ramps and a constant": {
			{StartRPS: 0, TargetRPS: 500, Duration: 5 * time.Second},
			{StartRPS: 500, TargetRPS: 1500, Duration: 5 * time.Second},
			{StartRPS: 1500, TargetRPS: 1500, Duration: 5 * time.Second},
		},
	}

	for name, stages := range plans {
		var total time.Duration
		for _, stage := range stages {
			total += stage.Duration
		}

		n := int(total / time.Second)
		secs := plannedSeconds(t, stages, total, everySecond(n)...)

		if len(secs) != n {
			t.Fatalf("%s: seconds = %d, want %d", name, len(secs), n)
		}

		for i, s := range secs {
			from := time.Duration(i) * time.Second
			ticks := ticksBetween(stages, from, from+time.Second)

			if diff := math.Abs(float64(ticks) - s.PlannedRPS); diff >= 1 {
				t.Errorf("%s: second %d: the pacer places %d ticks, the report plans %v (differ by %v, want < 1)",
					name, i, ticks, s.PlannedRPS, diff)
			}
		}
	}
}

// Ground: V5 — the covered part of the last second ends at the earlier of the
// schedule's end and the method's own plan: with two methods of different
// length the schedule runs to the longer plan, and the shorter one's plan ends
// inside its second. The plan {50 for 2.5 s} under a schedule that ran 10 s
// offered 50 in [2, 2.5): second 2 is 50, not the 25 of a mean over the whole
// second, and second 3 has no plan. Mutation "the covered end is the schedule's
// end, without the min with the plan's" (second 2 = 25) turns it red.
func TestTimeline_APlanThatEndsInsideASecondIsAveragedOverItsOwnEnd(t *testing.T) {
	stages := []Stage{{StartRPS: 50, TargetRPS: 50, Duration: 2500 * time.Millisecond}}

	secs := plannedSeconds(t, stages, 10*time.Second, everySecond(4)...)

	if len(secs) != 4 {
		t.Fatalf("seconds = %d, want 4", len(secs))
	}

	for i, want := range []float64{50, 50, 50, 0} {
		if !within(secs[i].PlannedRPS, want, 1e-9) {
			t.Errorf("second %d planned %v, want %v", i, secs[i].PlannedRPS, want)
		}
	}
}

// Ground: V1 — a call is counted in the second it was scheduled for, so that a
// target which freezes under load shows the rate the failing calls went out at,
// not the rate of the moment their timeout came. The outcome counters stay by
// the moment of finishing. Mutation "counted by the second it finished in"
// turns it red.
func TestTimeline_ACallIsCountedInTheSecondItWasScheduledFor(t *testing.T) {
	start := time.Now()
	stats := reserved(start, 0)

	stats.Record(call(start, 2*time.Second, 12*time.Second, CategoryTimeout))

	secs := seconds(t, stats)

	if secs[2].ScheduledSent != 1 || secs[2].ScheduledFailed != 1 {
		t.Errorf("second 2: scheduled sent %d, failed %d; want 1 and 1", secs[2].ScheduledSent, secs[2].ScheduledFailed)
	}
	if secs[12].ScheduledSent != 0 || secs[12].ScheduledFailed != 0 {
		t.Errorf("second 12: scheduled sent %d, failed %d; want 0 and 0: the call finished there, it was not scheduled there",
			secs[12].ScheduledSent, secs[12].ScheduledFailed)
	}
	if secs[12].TimedOut != 1 || secs[2].TimedOut != 0 {
		t.Errorf("timed out: second 2 has %d, second 12 has %d; want 0 and 1: the outcome counters stay by the moment of finishing",
			secs[2].TimedOut, secs[12].TimedOut)
	}
}

// Ground: the rule is Report.WarmupSent's and WarmupFailed's: a call that never
// went out is not sent, an aborted one went out and did not fail, and a call
// with a lag before its schedule (a broken clock) still counts: the counters
// sit outside the lag's branch. Mutations "counts the unsent call" and "counts
// inside the lag >= 0 branch" turn it red.
func TestTimeline_ScheduledCountsFollowTheSentRule(t *testing.T) {
	start := time.Now()
	stats := reserved(start, 0)

	unsentCall := unsent(start, time.Second, 500*time.Millisecond)
	earlyBegun := call(start, 2*time.Second, 2*time.Second+time.Millisecond, CategorySuccess)
	earlyBegun.BegunAt = earlyBegun.ScheduledAt.Add(-time.Millisecond)

	for _, r := range []Result{
		call(start, 0, time.Millisecond, CategorySuccess),
		call(start, 0, time.Millisecond, CategoryServerFault),
		call(start, 0, time.Millisecond, CategoryAborted),
		unsentCall,
		earlyBegun,
	} {
		stats.Record(r)
	}

	secs := seconds(t, stats)

	if secs[0].ScheduledSent != 3 || secs[0].ScheduledFailed != 1 {
		t.Errorf("second 0: scheduled sent %d, failed %d; want 3 and 1: success, failure, abort went out, only the failure failed",
			secs[0].ScheduledSent, secs[0].ScheduledFailed)
	}
	if secs[1].ScheduledSent != 0 || secs[1].ScheduledFailed != 0 {
		t.Errorf("second 1: scheduled sent %d, failed %d; want 0 and 0: the call never went out",
			secs[1].ScheduledSent, secs[1].ScheduledFailed)
	}
	if secs[2].ScheduledSent != 1 {
		t.Errorf("second 2: scheduled sent %d, want 1: a call with a negative lag is still counted", secs[2].ScheduledSent)
	}
}

// Ground: contract — the scheduled counters of the warm-up's seconds add up to
// Report.WarmupSent and WarmupFailed, the two numbers the line `ramp N sent (K
// failed)` shows: the block and that line are one count. The failures here end
// after the window, where a count by the finishing moment would lose them.
// Mutation "counted by the second it finished in" turns it red.
func TestTimeline_ScheduledCountsAddUpToTheWarmupTotals(t *testing.T) {
	start := time.Now()
	stats := reserved(start, 3*time.Second)

	for _, r := range []Result{
		call(start, 0, 50*time.Millisecond, CategorySuccess),
		call(start, 500*time.Millisecond, 700*time.Millisecond, CategoryOverload),
		call(start, time.Second, 9*time.Second, CategoryTimeout),
		call(start, 1500*time.Millisecond, 8*time.Second, CategoryTimeout),
		call(start, 2*time.Second, 2100*time.Millisecond, CategoryAborted),
		unsent(start, 2200*time.Millisecond, 400*time.Millisecond),
		call(start, 2900*time.Millisecond, 6*time.Second, CategoryServerFault),
		// Measured: scheduled after the window, not part of the sums.
		call(start, 3*time.Second, 3100*time.Millisecond, CategorySuccess),
		call(start, 4*time.Second, 7*time.Second, CategoryTimeout),
	} {
		stats.Record(r)
	}

	stats.EndSending(start.Add(5 * time.Second))
	stats.Finish(start.Add(10 * time.Second))

	report := stats.Report()
	secs := report.Methods[0].Seconds

	var sent, failed int
	for _, s := range secs[:3] {
		sent += s.ScheduledSent
		failed += s.ScheduledFailed
	}

	if report.WarmupSent != 6 || report.WarmupFailed != 4 {
		t.Fatalf("warm-up sent %d, failed %d; the fixture wants 6 and 4", report.WarmupSent, report.WarmupFailed)
	}
	if sent != report.WarmupSent || failed != report.WarmupFailed {
		t.Errorf("the window's seconds add up to sent %d, failed %d; Report has %d and %d",
			sent, failed, report.WarmupSent, report.WarmupFailed)
	}
}
