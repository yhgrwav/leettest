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

func within(got, want, tolerance float64) bool {
	return math.Abs(got-want) <= tolerance
}

// Ground: review verdict V1 — planned_rps is the rate the plan offers now; a
// constant stage offers its own rate at every moment of it.
func TestRateAt_AConstantStageHoldsItsRate(t *testing.T) {
	stages := []Stage{{StartRPS: 50, TargetRPS: 50, Duration: 10 * time.Second}}

	for _, at := range []time.Duration{0, time.Nanosecond, 3 * time.Second, 10*time.Second - time.Millisecond} {
		if got := rateAt(stages, at); got != 50 {
			t.Errorf("rateAt(50 rps for 10s, %v) = %v, want 50", at, got)
		}
	}
}

// Ground: review verdict V1 — a ramp offers the line between its two rates
// (the same line Stage.offset places its ticks on), not its target from the
// first second and not its mean.
func TestRateAt_ARampMovesLinearly(t *testing.T) {
	cases := []struct {
		name  string
		stage Stage
		at    time.Duration
		want  float64
	}{
		{"up, start", Stage{0, 100, 10 * time.Second}, 0, 0},
		{"up, quarter", Stage{0, 100, 10 * time.Second}, 2500 * time.Millisecond, 25},
		{"up, middle", Stage{0, 100, 10 * time.Second}, 5 * time.Second, 50},
		{"up, three quarters", Stage{0, 100, 10 * time.Second}, 7500 * time.Millisecond, 75},
		{"down, quarter", Stage{100, 0, 10 * time.Second}, 2500 * time.Millisecond, 75},
		{"down, three quarters", Stage{100, 0, 10 * time.Second}, 7500 * time.Millisecond, 25},
		{"between two nonzero rates", Stage{1, 2, 3 * time.Second}, 1500 * time.Millisecond, 1.5},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rateAt([]Stage{c.stage}, c.at); !within(got, c.want, 1e-9) {
				t.Errorf("rateAt(%+v, %v) = %v, want %v", c.stage, c.at, got, c.want)
			}
		})
	}
}

// Ground: edge — the boundary between two stages. A stage is [start, end): at
// its end the next stage's rate is offered, a nanosecond before it still the
// previous stage's.
func TestRateAt_AtAStageBoundaryTheNextStageTakesOver(t *testing.T) {
	stages := []Stage{
		{StartRPS: 50, TargetRPS: 50, Duration: 5 * time.Second},
		{StartRPS: 80, TargetRPS: 80, Duration: 5 * time.Second},
	}

	if got := rateAt(stages, 5*time.Second); got != 80 {
		t.Errorf("rateAt at exactly 5s = %v, want 80 (the second stage's)", got)
	}
	if got := rateAt(stages, 5*time.Second-time.Nanosecond); got != 50 {
		t.Errorf("rateAt a nanosecond before 5s = %v, want 50 (the first stage's)", got)
	}
}

// Ground: edge — a ramp followed by a stage: the ramp's last moment is next to
// its target, then the next stage's own rate (not the ramp's target carried on).
func TestRateAt_ARampHandsOverToTheNextStageAtItsEnd(t *testing.T) {
	stages := []Stage{
		{StartRPS: 0, TargetRPS: 100, Duration: 10 * time.Second},
		{StartRPS: 20, TargetRPS: 20, Duration: 10 * time.Second},
	}

	if got := rateAt(stages, 10*time.Second); got != 20 {
		t.Errorf("rateAt at the end of the ramp = %v, want 20 (the next stage's)", got)
	}
	if got := rateAt(stages, 10*time.Second-time.Millisecond); !within(got, 99.99, 0.001) {
		t.Errorf("rateAt a millisecond before the end of the ramp = %v, want 99.99", got)
	}
}

// Ground: review verdict V1 — "0 after the plan ends": the plan owns [0, total);
// at the end and long past it nothing is offered.
func TestRateAt_AfterThePlanItIsZero(t *testing.T) {
	stages := []Stage{{StartRPS: 50, TargetRPS: 50, Duration: 10 * time.Second}}

	for _, at := range []time.Duration{10 * time.Second, 10*time.Second + time.Nanosecond, time.Hour} {
		if got := rateAt(stages, at); got != 0 {
			t.Errorf("rateAt(10s plan, %v) = %v, want 0", at, got)
		}
	}
}

// Ground: edge — the last moment before a ramp ends: close to its target, not
// zero ("the plan is over" must not creep in early) and not past it.
func TestRateAt_JustBeforeTheLastRampEnds(t *testing.T) {
	stages := []Stage{{StartRPS: 0, TargetRPS: 50, Duration: 10 * time.Second}}

	if got := rateAt(stages, 10*time.Second-time.Millisecond); !within(got, 49.995, 0.001) {
		t.Errorf("rateAt a millisecond before the end of 0→50 = %v, want 49.995", got)
	}
}

// Ground: edge — a ramp down never offers a negative rate, even a nanosecond
// before the end where the line is about to touch zero.
func TestRateAt_ARampDownIsNeverNegative(t *testing.T) {
	stages := []Stage{{StartRPS: 100, TargetRPS: 0, Duration: 10 * time.Second}}

	if got := rateAt(stages, 10*time.Second-time.Nanosecond); math.IsNaN(got) || got < 0 {
		t.Errorf("rateAt a nanosecond before the end of 100→0 = %v, want ≥ 0", got)
	}
}

// Ground: edge — empty. No stages, no plan, no rate.
func TestRateAt_NoStagesIsZero(t *testing.T) {
	for _, stages := range [][]Stage{nil, {}} {
		if got := rateAt(stages, time.Second); got != 0 {
			t.Errorf("rateAt(%v, 1s) = %v, want 0", stages, got)
		}
	}
}

// Ground: edge — a pause in the plan (a stage of rate 0) offers 0, and the
// stage after it offers its own rate.
func TestRateAt_AZeroRateStageIsAPause(t *testing.T) {
	stages := []Stage{
		{StartRPS: 0, TargetRPS: 0, Duration: 5 * time.Second},
		{StartRPS: 30, TargetRPS: 30, Duration: 5 * time.Second},
	}

	if got := rateAt(stages, 2*time.Second); got != 0 {
		t.Errorf("rateAt inside the pause = %v, want 0", got)
	}
	if got := rateAt(stages, 6*time.Second); got != 30 {
		t.Errorf("rateAt after the pause = %v, want 30", got)
	}
}

// Ground: edge — a stage of zero length owns no moment: it is skipped, at the
// start of the plan and in the middle of it.
func TestRateAt_AStageOfZeroLengthIsSkipped(t *testing.T) {
	first := []Stage{
		{StartRPS: 10, TargetRPS: 10, Duration: 0},
		{StartRPS: 20, TargetRPS: 20, Duration: 5 * time.Second},
	}
	if got := rateAt(first, 0); got != 20 {
		t.Errorf("rateAt(0) with a zero-length first stage = %v, want 20", got)
	}

	middle := []Stage{
		{StartRPS: 10, TargetRPS: 10, Duration: 5 * time.Second},
		{StartRPS: 99, TargetRPS: 99, Duration: 0},
		{StartRPS: 20, TargetRPS: 20, Duration: 5 * time.Second},
	}
	if got := rateAt(middle, 5*time.Second); got != 20 {
		t.Errorf("rateAt at 5s with a zero-length stage in the middle = %v, want 20", got)
	}
}

// Ground: edge — impossible input. A clock read can come out negative (the
// view is taken before the first tick of a coarse clock): the plan has not
// started, its first rate is the honest answer, not 0 and not a panic.
func TestRateAt_ANegativeElapsedIsTheFirstRate(t *testing.T) {
	if got := rateAt([]Stage{{StartRPS: 40, TargetRPS: 40, Duration: 5 * time.Second}}, -time.Second); got != 40 {
		t.Errorf("rateAt(-1s) of a constant stage = %v, want 40", got)
	}
	if got := rateAt([]Stage{{StartRPS: 10, TargetRPS: 20, Duration: 5 * time.Second}}, -time.Second); got != 10 {
		t.Errorf("rateAt(-1s) of a ramp 10→20 = %v, want 10", got)
	}
}

// ticksBetween counts the ticks the schedule places in [from, to), the way the
// dispatcher does: each stage's offsets (Stage.offset) after the stages before it.
func ticksBetween(stages []Stage, from, to time.Duration) int {
	n := 0

	var base time.Duration
	for _, stage := range stages {
		for i := 0; ; i++ {
			at, ok := stage.offset(i)
			if !ok || base+at >= to {
				break
			}
			if base+at >= from {
				n++
			}
		}
		base += stage.Duration
	}

	return n
}

// Ground: review verdict V1 — planned_rps must agree with the schedule it
// describes. A one-second window centred on t, inside one stage, holds the area
// under the rate over that second: the rate at t, whole calls, so within 1. The
// centred window, not [t, t+1s): that one holds the rate at t+0.5s, and on a
// ramp it is off by half a second of slope (the reviewer's window is amended in
// the report).
func TestRateAt_AgreesWithTheTicksTheScheduleLaysOut(t *testing.T) {
	plans := []struct {
		name   string
		stages []Stage
		at     []time.Duration
	}{
		{"constant", []Stage{{50, 50, 10 * time.Second}}, []time.Duration{time.Second, 4500 * time.Millisecond, 9500 * time.Millisecond}},
		{"ramp up", []Stage{{0, 100, 10 * time.Second}}, []time.Duration{time.Second, 3 * time.Second, 5 * time.Second, 9 * time.Second}},
		{"ramp down", []Stage{{100, 0, 10 * time.Second}}, []time.Duration{time.Second, 3 * time.Second, 5 * time.Second, 9 * time.Second}},
		{
			"hold, ramp, hold",
			[]Stage{{10, 10, 5 * time.Second}, {10, 60, 5 * time.Second}, {60, 60, 5 * time.Second}},
			[]time.Duration{time.Second, 4500 * time.Millisecond, 6 * time.Second, 7500 * time.Millisecond, 9500 * time.Millisecond, 11 * time.Second, 14500 * time.Millisecond},
		},
	}

	for _, plan := range plans {
		for _, at := range plan.at {
			t.Run(plan.name+" at "+at.String(), func(t *testing.T) {
				ticks := ticksBetween(plan.stages, at-500*time.Millisecond, at+500*time.Millisecond)
				if got := rateAt(plan.stages, at); !within(got, float64(ticks), 1) {
					t.Errorf("rateAt = %v, but the schedule places %d ticks in the second around %v", got, ticks, at)
				}
			})
		}
	}
}
