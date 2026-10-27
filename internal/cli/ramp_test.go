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

package cli

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// rampRun is a finished run of one call at 800 rps for 20s after a ramp of
// ramp: the engine's excluded window is the ramp and the warm-up (warmup), its
// planned duration the stages' sum, and the settings are the config's.
func rampRun(ramp, warmup time.Duration, sent, failed int) RunReport {
	return RunReport{
		Report: engine.Report{
			Duration: ramp + 20*time.Second, Planned: ramp + 20*time.Second,
			Warmup: ramp + warmup, WarmupSent: sent, WarmupFailed: failed,
			Sent: 16000,
			Methods: []engine.MethodReport{{
				Method: "a.B/One", Sent: 16000, RPS: 800, RPSLow: 800, RPSHigh: 800,
				PlannedDuration: ramp + 20*time.Second,
			}},
		},
		Settings: &RunSettings{MaxInFlight: 5000, Connections: 1, Warmup: warmup, Ramp: ramp},
	}
}

// Ground: contract — a run with a ramp says so where the warm-up is said: the
// settings line puts the ramp after the warm-up, and the note that holds the
// excluded calls is named for what they were. The ramp comes from the
// settings, which carry the config's value (Spec 7); the engine's Warmup is
// ramp and warm-up together and cannot tell them apart. Without a ramp, or
// with no settings, both lines are those of the release.
func TestReport_ARampIsNamedInSettingsAndTheWarmUpNote(t *testing.T) {
	for _, tc := range []struct {
		name     string
		run      RunReport
		settings string
		note     string
	}{
		{
			name:     "a ramp and a warm-up",
			run:      rampRun(30*time.Second, 2*time.Second, 1200, 0),
			settings: "settings: warm-up 2s, ramp 30s, max in flight 5000, connections 1",
			note:     "ramp and warm-up 1200 sent, excluded from stats",
		},
		{
			name:     "a ramp without a warm-up",
			run:      rampRun(30*time.Second, 0, 1200, 0),
			settings: "settings: warm-up 0s, ramp 30s, max in flight 5000, connections 1",
			note:     "ramp 1200 sent, excluded from stats",
		},
		{
			name:     "failures in the excluded calls are shown",
			run:      rampRun(30*time.Second, 2*time.Second, 1200, 12),
			settings: "settings: warm-up 2s, ramp 30s, max in flight 5000, connections 1",
			note:     "ramp and warm-up 1200 sent (12 failed), excluded from stats",
		},
		{
			name:     "a ramp that sent nothing still has its line",
			run:      rampRun(30*time.Second, 0, 0, 0),
			settings: "settings: warm-up 0s, ramp 30s, max in flight 5000, connections 1",
			note:     "ramp 0 sent, excluded from stats",
		},
		{
			name:     "no ramp",
			run:      rampRun(0, 2*time.Second, 1200, 0),
			settings: "settings: warm-up 2s, max in flight 5000, connections 1",
			note:     "warm-up 1200 sent, excluded from stats",
		},
	} {
		lines := echoLines(tc.run)
		if !slices.Contains(lines, tc.settings) {
			t.Errorf("%s: no line %q in:\n%s", tc.name, tc.settings, strings.Join(lines, "\n"))
		}
		if !slices.Contains(lines, tc.note) {
			t.Errorf("%s: no line %q in:\n%s", tc.name, tc.note, strings.Join(lines, "\n"))
		}
		if tc.run.Settings.Ramp == 0 && strings.Contains(strings.Join(lines, "\n"), "ramp") {
			t.Errorf("%s: the report says ramp with none configured:\n%s", tc.name, strings.Join(lines, "\n"))
		}
	}
}

// Ground: contract — the ramp is told after the warm-up with compression too,
// and a run told nothing of its settings (nil) has no ramp to name: the
// engine's window is a warm-up there, as before.
func TestReport_TheRampSitsBeforeCompressionAndNilSettingsHaveNone(t *testing.T) {
	run := rampRun(30*time.Second, 2*time.Second, 1200, 0)
	run.Settings.Compression = "gzip"
	want := "settings: warm-up 2s, ramp 30s, max in flight 5000, connections 1, compression gzip"
	if lines := echoLines(run); !slices.Contains(lines, want) {
		t.Errorf("no line %q in:\n%s", want, strings.Join(lines, "\n"))
	}

	run.Settings = nil
	lines := echoLines(run)
	if !slices.Contains(lines, "warm-up 1200 sent, excluded from stats") || strings.Contains(strings.Join(lines, "\n"), "ramp") {
		t.Errorf("without settings the window is a warm-up and nothing is a ramp:\n%s", strings.Join(lines, "\n"))
	}
}

// Ground: contract — the plan line gives the rate and length of the measured
// part, not a number the user did not set: PlannedDuration is the stages' sum,
// ramp included (50s), and 800 rps for 50s is no one's config. With a ramp
// the line is the user's duration and then the ramp; without one it is as it
// was, byte for byte.
func TestReport_TheRampPlanLineSaysWhatTheUserSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  RunReport
		want string
	}{
		{"a ramp of 30s", rampRun(30*time.Second, 2*time.Second, 1200, 0), "  plan: 800 rps for 20s, after a 30s ramp"},
		{"a ramp of 90s", rampRun(90*time.Second, 0, 1200, 0), "  plan: 800 rps for 20s, after a 1m30s ramp"},
		{"no ramp", rampRun(0, 2*time.Second, 1200, 0), "  plan: 800 rps for 20s"},
	} {
		lines := echoLines(tc.run)
		if i := indexOfPrefix(lines, "  plan:"); i < 0 || lines[i] != tc.want {
			t.Errorf("%s: no line %q in:\n%s", tc.name, tc.want, strings.Join(lines, "\n"))
		}
	}

	nilSettings := rampRun(30*time.Second, 2*time.Second, 1200, 0)
	nilSettings.Settings = nil
	if lines := echoLines(nilSettings); indexOfPrefix(lines, "  plan: 800 rps for 50s") < 0 {
		t.Errorf("without settings the plan is the stages' sum, as before:\n%s", strings.Join(lines, "\n"))
	}
}

// Ground: contract — JSON (schema_version 1): settings.ramp_us is the config's
// ramp in whole microseconds, always present, 0 without one; settings.warmup_us
// stays the configured warm-up; the top-level warmup_us is the window the
// statistics leave out, ramp and warm-up (32s), so a reader who wants the
// warm-up alone takes warmup_us minus settings.ramp_us; planned_duration_us
// stays the stages' sum. The notes carry the ramp line.
func TestJSON_TheRampIsInTheSettings(t *testing.T) {
	out := writeJSON(t, rampJSON(rampRun(30*time.Second, 2*time.Second, 1200, 0)))

	for _, tc := range []struct {
		path []string
		want float64
	}{
		{[]string{"settings", "ramp_us"}, 30_000_000},
		{[]string{"settings", "warmup_us"}, 2_000_000},
		{[]string{"warmup_us"}, 32_000_000},
		{[]string{"warmup_sent"}, 1200},
		{[]string{"planned_us"}, 50_000_000},
	} {
		if got := field(t, out, tc.path...); got != tc.want {
			t.Errorf("%s = %v, want %v", strings.Join(tc.path, "."), got, tc.want)
		}
	}

	method := field(t, out, "methods").([]any)[0]
	if got := field(t, method, "planned_duration_us"); got != 50_000_000.0 {
		t.Errorf("methods[0].planned_duration_us = %v, want the stages' sum, 50000000", got)
	}

	notes, _ := field(t, out, "notes").([]any)
	if !slices.Contains(notes, any("ramp and warm-up 1200 sent, excluded from stats")) {
		t.Errorf("notes = %v, want the ramp and warm-up line among them", notes)
	}

	none := writeJSON(t, rampJSON(rampRun(0, 2*time.Second, 1200, 0)))
	if got := field(t, none, "settings", "ramp_us"); got != 0.0 {
		t.Errorf("settings.ramp_us without a ramp = %v, want 0 (always present)", got)
	}

	nilSettings := rampRun(30*time.Second, 2*time.Second, 1200, 0)
	nilSettings.Settings = nil
	if got := field(t, writeJSON(t, rampJSON(nilSettings)), "settings", "ramp_us"); got != 0.0 {
		t.Errorf("settings.ramp_us without settings = %v, want 0, never filled from the measurement", got)
	}
}

// rampJSON is the JSON input of a finished ramp run.
func rampJSON(run RunReport) JSONRun {
	out := jsonRun(run.Report)
	out.Run = run

	return out
}

// Ground: contract — the once-dataset check counts the ramp's calls too (the
// engine's own count of the stages, 12000 on the ramp and 16000 on the hold),
// and a message about a run with a ramp says the ramp is in it. A plan with no
// ramp keeps the release's words; the boundary is the record count itself.
func TestCheckDatasetsFit_CountsAndNamesTheRamp(t *testing.T) {
	call := func(records int, stages ...engine.Stage) engine.Call {
		return engine.Call{
			Method: "/a.B/One", Stages: stages, Payloads: make([][]byte, records),
			Dataset: &engine.DatasetRef{File: "data/users.jsonl", Once: true},
		}
	}
	ramped := []engine.Stage{
		{StartRPS: 0, TargetRPS: 800, Duration: 30 * time.Second},
		{StartRPS: 800, TargetRPS: 800, Duration: 20 * time.Second},
	}
	flat := []engine.Stage{{StartRPS: 800, TargetRPS: 800, Duration: 20 * time.Second}}

	if err := CheckDatasetsFit([]engine.Call{call(28000, ramped...)}); err != nil {
		t.Errorf("28000 records for 28000 calls: %v", err)
	}

	err := CheckDatasetsFit([]engine.Call{call(27999, ramped...)})
	if err == nil {
		t.Fatal("27999 records for 28000 calls: no error")
	}
	for _, want := range []string{"has 27999 records", "hands out 28000 (ramp and warm-up included)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}

	// The verdict's own case: ramp 1s, 100 rps for 1s, a file of 120.
	small := []engine.Stage{
		{StartRPS: 0, TargetRPS: 100, Duration: time.Second},
		{StartRPS: 100, TargetRPS: 100, Duration: time.Second},
	}
	err = CheckDatasetsFit([]engine.Call{call(120, small...)})
	if err == nil || !strings.Contains(err.Error(), "has 120 records, the run hands out 150 (ramp and warm-up included);") {
		t.Errorf("a file of 120 for 150 calls: %v", err)
	}

	err = CheckDatasetsFit([]engine.Call{call(15999, flat...)})
	if err == nil || !strings.Contains(err.Error(), "hands out 16000 (warm-up included);") || strings.Contains(err.Error(), "ramp") {
		t.Errorf("a plan without a ramp: %v, want the release's words", err)
	}
}
