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
	"context"
	"runtime"
	"slices"
	"testing"
	"time"
)

// Ground: contract — the report names why the schedule is not exact: the names
// are the report's wording, the order is fixed, and the list is empty exactly
// when exactScheduleFor is true. The inputs are injected, so the table is the
// same on every host.
func TestScheduleReasonsFor_NamesEveryConditionTheScheduleFails(t *testing.T) {
	// The names are the report's wording, written out here and not through
	// the constants: renaming one is a change of the JSON.
	const (
		fine     = 40 * time.Nanosecond
		edge     = 999 * time.Nanosecond
		coarse   = 500 * time.Microsecond
		noSleep  = "no_precise_sleep"
		stepName = "clock_step"
		procName = "gomaxprocs"
	)

	for _, tc := range []struct {
		precise bool
		step    time.Duration
		procs   int
		want    []string
	}{
		{true, fine, 2, nil},
		{true, edge, 2, nil},
		{true, fine, 8, nil},

		{false, fine, 2, []string{noSleep}},
		{false, edge, 8, []string{noSleep}},
		{true, time.Microsecond, 2, []string{stepName}},
		{true, coarse, 8, []string{stepName}},
		{true, fine, 1, []string{procName}},
		{true, edge, 1, []string{procName}},
		{true, fine, 0, []string{procName}},

		{false, time.Microsecond, 2, []string{noSleep, stepName}},
		{false, coarse, 8, []string{noSleep, stepName}},
		{false, fine, 1, []string{noSleep, procName}},
		{true, time.Microsecond, 1, []string{stepName, procName}},
		{true, coarse, 1, []string{stepName, procName}},
		{false, coarse, 1, []string{noSleep, stepName, procName}},
		{false, time.Microsecond, 1, []string{noSleep, stepName, procName}},
	} {
		got := scheduleReasonsFor(tc.precise, tc.step, tc.procs)
		if !slices.Equal(got, tc.want) {
			t.Errorf("precise sleep %v, step %v, %d Ps: reasons %q, want %q", tc.precise, tc.step, tc.procs, got, tc.want)
		}

		// One computation, not two: no reasons is the exact schedule.
		if exact := exactScheduleFor(tc.precise, tc.step, tc.procs); exact != (len(got) == 0) {
			t.Errorf("precise sleep %v, step %v, %d Ps: exactScheduleFor %v with reasons %q", tc.precise, tc.step, tc.procs, exact, got)
		}
	}
}

// Ground: boundary — the P count is the runtime's, as for the verdict itself:
// GOMAXPROCS 1 names gomaxprocs on every host, whatever else the host adds.
// Not parallel: it changes the process's GOMAXPROCS.
func TestNewDispatcher_OnePNamesTheReason(t *testing.T) {
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })

	d := NewDispatcher(nil)
	if d.exact {
		t.Fatal("exact wait with GOMAXPROCS 1")
	}
	if !slices.Contains(d.notExact, "gomaxprocs") {
		t.Errorf("reasons %q with GOMAXPROCS 1, want gomaxprocs among them", d.notExact)
	}
}

// reasonsOfARun is the reasons the report of a short run names when the caller
// measured the clock step as step (Options.ClockStep).
func reasonsOfARun(t *testing.T, step time.Duration) []string {
	t.Helper()

	eng, err := New(Options{
		Calls: []Call{
			{Method: "a.B/One", Timeout: 100 * time.Millisecond, Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 20 * time.Millisecond}}},
		},
		Sender:      FakeSender{Delay: time.Millisecond},
		MaxInFlight: 64,
		ClockStep:   step,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := eng.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	return eng.Report().ScheduleNotExact
}

// The step the caller measured is the step the verdict uses, the one the
// report prints as the clock step before the run: a second measurement of the
// engine's own could disagree with the line that prints the step. The other
// reasons are the host's, so only clock_step is asserted, present or absent.
//
// Ground: contract — Options.ClockStep is the caller's measure; 0 means the
// engine measures. The given step decides on every host: a coarse one names
// clock_step on Linux, a fine one leaves it out on Windows, whatever the
// engine would measure itself.
func TestEngine_TheCallersClockStepDecidesClockStep(t *testing.T) {
	for _, tc := range []struct {
		name string
		step time.Duration
		want bool
	}{
		{"a coarse step", 2 * time.Microsecond, true},
		{"exactly 1us", time.Microsecond, true},
		{"a very coarse step", 15625 * time.Microsecond, true},
		{"one nanosecond under 1us", 999 * time.Nanosecond, false},
		{"a fine step", 40 * time.Nanosecond, false},
	} {
		if got := slices.Contains(reasonsOfARun(t, tc.step), "clock_step"); got != tc.want {
			t.Errorf("%s: clock_step in the reasons %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Ground: contract — Engine.Report hands the dispatcher's reasons on: the
// report is where the CLI reads them. A run that did not wait exactly names
// why; the dispatcher is built by Run, so the reasons are those of its moment.
// Not parallel: it changes the process's GOMAXPROCS.
func TestEngine_ReportSaysWhyTheScheduleWasNotExact(t *testing.T) {
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })

	eng, err := New(Options{
		Calls: []Call{
			{Method: "a.B/One", Timeout: 100 * time.Millisecond, Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 50 * time.Millisecond}}},
		},
		Sender:      FakeSender{Delay: time.Millisecond},
		MaxInFlight: 64,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := eng.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := eng.Report().ScheduleNotExact; !slices.Contains(got, "gomaxprocs") {
		t.Errorf("report reasons %q with GOMAXPROCS 1, want gomaxprocs among them", got)
	}
}
