package main

import (
	"os"
	"slices"
	"testing"
	"time"
)

// The end-to-end runs here check what the report says about the target on a
// loopback target answering in microseconds; on a 0.5 ms Windows clock every
// one would be invalid. The clock rule has its own tests.
func TestMain(m *testing.M) {
	clockStep = func() time.Duration { return 0 }
	os.Exit(m.Run())
}

// A search measures the clock before each step and hands that step to the
// engine: the step the command measured decides clock_step in the reasons of
// every step's schedule, whatever the host's own clock is. Both ways, so the
// test tells the command's step from the engine's on a coarse host and on a
// fine one.
func TestRun_ASearchStepsTakeTheClockStepTheCommandMeasured(t *testing.T) {
	saved := clockStep
	t.Cleanup(func() { clockStep = saved })

	for _, tc := range []struct {
		name string
		step time.Duration
		want bool
	}{
		{"a coarse clock", 15625 * time.Microsecond, true},
		{"a fine clock", 999 * time.Nanosecond, false},
	} {
		clockStep = func() time.Duration { return tc.step }

		res := runCLI(t.Context(), t, 60*time.Second,
			"-fake", "-output", "json", "-c", writeSearch(t, closedPort(t), checkMethod))

		bp, _ := decodeOnly(t, res.stdout)["breakpoint"].(map[string]any)

		runs, _ := bp["runs"].([]any)
		if len(runs) == 0 {
			t.Fatalf("%s: the report lists no runs:\n%s", tc.name, res.stdout)
		}

		for i, r := range runs {
			reasons, _ := jsonAt(t, r, "report", "schedule", "not_exact_reasons").([]any)
			if got := slices.Contains(reasons, any("clock_step")); got != tc.want {
				t.Errorf("%s: run %d: clock_step in %v is %v, want %v", tc.name, i, reasons, got, tc.want)
			}
		}
	}
}

// The whole path: the measured step reaches the report, not only runResult.
// A 15.625 ms step against the fake target's 25 ms p50 is over a quarter.
func TestRun_ACoarseClockIsExit2WithClockStepInJSON(t *testing.T) {
	saved := clockStep
	clockStep = func() time.Duration { return 15625 * time.Microsecond }
	t.Cleanup(func() { clockStep = saved })

	res := runCLI(t.Context(), t, 10*time.Second,
		"-fake", "-output", "json", "-c", writeConfig(t, closedPort(t), checkMethod, plaintext))
	if code := exitCode(res.err); code != 2 {
		t.Errorf("exit code %d (%v), want 2", code, res.err)
	}

	out := decodeOnly(t, res.stdout)
	reasons, _ := out["invalid_reasons"].([]any)
	if !slices.Contains(reasons, any("clock_step")) {
		t.Errorf("invalid_reasons = %v, want clock_step", reasons)
	}
}
