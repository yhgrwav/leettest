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
	"testing"
	"time"
)

// planSent is the calls a method with a flat rps × planned plan hands out by
// the moment until: one every 1/rps, none after the plan ends or the stop.
func planSent(stats *Stats, start time.Time, method string, rps int, planned, until time.Duration) {
	end := min(planned, until)
	step := time.Second / time.Duration(rps)

	for at := time.Duration(0); at < end; at += step {
		moment := start.Add(at)
		stats.Record(Result{
			Method: method, ScheduledAt: moment, BegunAt: moment,
			Outcome: Outcome{Category: CategorySuccess, SentAt: moment, DoneAt: moment},
		})
	}
}

// aAndB is a run of method A (100 rps for 10 s) next to method B (50 rps for
// 20 s), as the engine hands the plans over, stopped at stop. A sending run
// ends where the longest plan does, 20 s.
func aAndB(warmup, stop time.Duration) *Stats {
	return aAndBFrom(time.Now(), []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 10 * time.Second}}, warmup, stop)
}

// aAndBFrom is aAndB with the run started at start and A's plan given as
// stagesA, which must still add up to 10 s at 100 rps.
func aAndBFrom(start time.Time, stagesA []Stage, warmup, stop time.Duration) *Stats {
	stats := NewStats()
	stats.Reserve(time.Minute, "a", "b")
	stats.Start(start, warmup)

	stagesB := []Stage{{StartRPS: 50, TargetRPS: 50, Duration: 20 * time.Second}}
	stats.setMethodStages("a", stagesA)
	stats.setMethodStages("b", stagesB)

	planSent(stats, start, "a", 100, 10*time.Second, stop)
	planSent(stats, start, "b", 50, 20*time.Second, stop)

	end := min(20*time.Second, stop)
	stats.EndSending(start.Add(20 * time.Second))
	if stop < 20*time.Second {
		stats.EndSending(start.Add(stop))
	}
	stats.Finish(start.Add(end))

	return stats
}

func rpsOf(t *testing.T, methods []MethodReport, name string) float64 {
	t.Helper()

	for i := range methods {
		if methods[i].Method == name {
			return methods[i].RPS
		}
	}
	t.Fatalf("no method %q in the report", name)

	return 0
}

// Ground: contract — a method's sent/s is its sent calls over the time that
// method was sending; a 10 s plan next to a 20 s one is not shown at half its
// rate (the run's sending window is the longest plan's).
func TestStatsMethodRPSIsOverItsOwnPlan(t *testing.T) {
	tests := []struct {
		name   string
		warmup time.Duration
		stop   time.Duration
		wantA  float64
		wantB  float64
	}{
		{"no warm-up", 0, time.Hour, 100, 50},
		// The warm-up is out of the count and out of each method's window:
		// A sent 800 of its 1000 calls over its 8 measured seconds, B 900 over 18.
		{"warm-up 2 s", 2 * time.Second, time.Hour, 100, 50},
		// A stop before either plan ends: both windows are the 5 s up to it.
		{"stop at 5 s", 0, 5 * time.Second, 100, 50},
		// A stop after the short plan ended and before the long one did: A
		// keeps its own 10 s, B has the 15 s up to the stop.
		{"stop at 15 s", 0, 15 * time.Second, 100, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := aAndB(tt.warmup, tt.stop).Report()

			if got := rpsOf(t, report.Methods, "a"); got != tt.wantA {
				t.Errorf("sent/s of A = %v, want %v", got, tt.wantA)
			}
			if got := rpsOf(t, report.Methods, "b"); got != tt.wantB {
				t.Errorf("sent/s of B = %v, want %v", got, tt.wantB)
			}
		})
	}
}

// Ground: contract — the live view reads the same number as the report, so it
// holds the same window for each method.
func TestStatsLiveMethodRPSIsOverItsOwnPlan(t *testing.T) {
	snapshot := aAndB(0, time.Hour).Snapshot()

	got := map[string]float64{}
	for _, m := range snapshot.Methods {
		got[m.Method] = m.RPS
	}
	if got["a"] != 100 || got["b"] != 50 {
		t.Errorf("live sent/s: A %v, B %v; want 100 and 50", got["a"], got["b"])
	}
}

// Ground: contract — a plan's length is the sum of all its stages, not one of
// them: A made of two 5 s stages is a 10 s plan, 1000 calls at 100 rps.
func TestStatsMethodRPSOfAPlanOfSeveralStagesIsOverTheirSum(t *testing.T) {
	stagesA := []Stage{
		{StartRPS: 100, TargetRPS: 100, Duration: 5 * time.Second},
		{StartRPS: 100, TargetRPS: 100, Duration: 5 * time.Second},
	}

	report := aAndBFrom(time.Now(), stagesA, 0, time.Hour).Report()

	if got := rpsOf(t, report.Methods, "a"); got != 100 {
		t.Errorf("sent/s of A = %v, want 100: 1000 calls over its two stages' 10 s", got)
	}
	if got := rpsOf(t, report.Methods, "b"); got != 50 {
		t.Errorf("sent/s of B = %v, want 50", got)
	}
}

// Ground: contract — the live view stopped but not finished (the drain) keeps
// each method's window: the sending time up to the stop, cut at the end of the
// method's plan. The run started 30 s ago, so the clock plays no part.
func TestStatsLiveMethodRPSAfterTheStopIsOverItsOwnWindow(t *testing.T) {
	tests := []struct {
		name   string
		warmup time.Duration
		stop   time.Duration
		wantA  float64
		wantB  float64
	}{
		// Both plans are still running at the stop: both windows are 5 s.
		{"stop at 5 s", 0, 5 * time.Second, 100, 50},
		// A's plan ended at 10 s, B's goes on to the stop at 15 s.
		{"stop at 15 s", 0, 15 * time.Second, 100, 50},
		// The warm-up is out of the plan's window too: A sent 800 calls over
		// its 8 measured seconds, B 650 over the 13 up to the stop.
		{"warm-up 2 s, stop at 15 s", 2 * time.Second, 15 * time.Second, 100, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now().Add(-30 * time.Second)
			stats := NewStats()
			stats.Reserve(time.Minute, "a", "b")
			stats.Start(start, tt.warmup)
			stats.setMethodStages("a", []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 10 * time.Second}})
			stats.setMethodStages("b", []Stage{{StartRPS: 50, TargetRPS: 50, Duration: 20 * time.Second}})
			planSent(stats, start, "a", 100, 10*time.Second, tt.stop)
			planSent(stats, start, "b", 50, 20*time.Second, tt.stop)
			stats.EndSending(start.Add(20 * time.Second))
			stats.EndSending(start.Add(tt.stop))

			got := map[string]float64{}
			for _, m := range stats.Snapshot().Methods {
				got[m.Method] = m.RPS
			}
			if got["a"] != tt.wantA || got["b"] != tt.wantB {
				t.Errorf("live sent/s: A %v, B %v; want %v and %v", got["a"], got["b"], tt.wantA, tt.wantB)
			}
		})
	}
}

// Ground: boundary — a stop inside the warm-up leaves no measured time and no
// measured call: the rate is 0, not NaN, not infinite, not negative. Same
// number in the report and in the live view.
func TestStatsMethodRPSOfAStopInsideTheWarmupIsZero(t *testing.T) {
	stats := aAndB(2*time.Second, time.Second)

	for _, m := range stats.Report().Methods {
		if m.RPS != 0 {
			t.Errorf("report: sent/s of %s = %v, want 0", m.Method, m.RPS)
		}
	}
	for _, m := range stats.Snapshot().Methods {
		if m.RPS != 0 {
			t.Errorf("live: sent/s of %s = %v, want 0", m.Method, m.RPS)
		}
	}
}

// Ground: contract — a scenario step has no plan of its own and runs at its
// head's pace, so its window is the head's: next to a plain method with a
// longer plan the step shows the head's rate, not 0 and not the plain one's.
func TestEngineMethodRPSOfAScenarioStepIsItsHeads(t *testing.T) {
	eng, err := New(Options{
		Calls: []Call{
			{
				Method: "a/Head", Scenario: "flow", Timeout: time.Second,
				Next:   []Step{{Method: "a/Step", Timeout: time.Second}},
				Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Second}},
			},
			{
				Method: "a/Plain", Timeout: time.Second,
				Stages: []Stage{{StartRPS: 50, TargetRPS: 50, Duration: 2 * time.Second}},
			},
		},
		Sender: FakeSender{}, MaxInFlight: 1000,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	methods := eng.Report().Methods
	head, step := rpsOf(t, methods, "a/Head"), rpsOf(t, methods, "a/Step")

	if head != 100 {
		t.Errorf("sent/s of the head = %v, want 100: 100 calls over its 1 s plan", head)
	}
	if step != head {
		t.Errorf("sent/s of the step = %v, want the head's %v: one call per iteration over the head's window (not 0, not the plain method's 50)", step, head)
	}
}

// Ground: boundary — runs where every plan has the same length keep the
// numbers they had: the method window is the run's window.
func TestStatsMethodRPSOfEqualPlansIsTheRunsWindow(t *testing.T) {
	start := time.Now()
	stats := NewStats()
	stats.Reserve(time.Minute, "a", "b")
	stats.Start(start, time.Second)

	for _, name := range []string{"a", "b"} {
		stats.setMethodStages(name, []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 5 * time.Second}})
		planSent(stats, start, name, 100, 5*time.Second, time.Hour)
	}
	stats.EndSending(start.Add(5 * time.Second))
	stats.Finish(start.Add(7 * time.Second))

	report := stats.Report()
	for _, name := range []string{"a", "b"} {
		if got := rpsOf(t, report.Methods, name); got != 100 {
			t.Errorf("sent/s of %s = %v, want 100: 400 calls over the 4 measured seconds", name, got)
		}
	}
}
