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
	"strings"
	"testing"
	"time"
)

const modeSettingsLine = "settings: warm-up 0s, max in flight 5000, connections 1"

// Ground: contract — a run's text header tells the mode on a line of its own
// between the name and the settings, for local as for real: a reader of a saved
// report sees which kind of run it was without the config. The old lines stay
// as they were.
func TestMode_ARunTellsItAboveTheSettings(t *testing.T) {
	for _, mode := range []string{"local", "real"} {
		lines := echoLines(RunReport{
			Report: echoReport(), Name: "wallet",
			Settings: &RunSettings{MaxInFlight: 5000, Connections: 1, Mode: mode},
		})

		want := []string{"run finished: localhost:50051 in 1.0s", "name: wallet", "mode: " + mode, modeSettingsLine}
		for i, w := range want {
			if lines[i] != w {
				t.Fatalf("%s: line %d = %q, want %q:\n%s", mode, i, lines[i], w, strings.Join(lines, "\n"))
			}
		}
	}
}

// Ground: contract — with no name the mode line still sits right before the
// settings, after the methods source.
func TestMode_ARunWithoutANameStillTellsIt(t *testing.T) {
	lines := echoLines(RunReport{
		Report: echoReport(), MethodsSource: ReflectionSource(),
		Settings: &RunSettings{MaxInFlight: 5000, Connections: 1, Mode: "real"},
	})

	want := []string{"run finished: localhost:50051 in 1.0s", "methods: server reflection", "mode: real", modeSettingsLine}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("line %d = %q, want %q:\n%s", i, lines[i], w, strings.Join(lines, "\n"))
		}
	}
}

// Ground: contract — a mode nobody told prints no line, not "mode: ": the
// library does not make a mode up, the CLI always tells one. Settings that were
// not given print none of their lines, as before.
func TestMode_NoModeNoLine(t *testing.T) {
	withSettings := echoLines(RunReport{Report: echoReport(), Name: "x", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1}})
	if i := indexOfPrefix(withSettings, "mode:"); i >= 0 {
		t.Errorf("a mode line %q for a mode nobody told:\n%s", withSettings[i], strings.Join(withSettings, "\n"))
	}

	without := echoLines(RunReport{Report: echoReport(), Name: "x"})
	if i := indexOfPrefix(without, "mode:"); i >= 0 {
		t.Errorf("a mode line %q for a run with no settings:\n%s", without[i], strings.Join(without, "\n"))
	}
}

// Ground: contract — the search tells the mode in the same place: after the
// name, before the settings, below the answer block and above the table of
// runs. The answer block itself is untouched.
func TestMode_ASearchTellsItAboveTheSettings(t *testing.T) {
	for _, mode := range []string{"local", "real"} {
		run := echoSearch(bpPlan, 2)
		run.Mode = mode
		out := echoSearchText(run)

		block, rest, ok := strings.Cut(out, "\n\n")
		if !ok {
			t.Fatalf("%s: no blank line after the answer:\n%s", mode, out)
		}
		if strings.Contains(block, "mode:") {
			t.Errorf("%s: the answer block holds the mode line:\n%s", mode, block)
		}
		table := strings.Index(rest, "  kind")
		if table < 0 {
			t.Fatalf("%s: no table of runs:\n%s", mode, out)
		}
		want := "name: wallet\nmode: " + mode + "\nsettings: settle 1s, hold 5s, max in flight per step, connections 2\n\n"
		if got := rest[:table]; got != want {
			t.Errorf("%s: between the answer and the table:\n%q\nwant\n%q", mode, got, want)
		}
	}
}

// Ground: contract — a search with no mode told prints no mode line.
func TestMode_ASearchWithNoModeNoLine(t *testing.T) {
	if out := echoSearchText(echoSearch(bpPlan, 2)); strings.Contains(out, "mode:") {
		t.Errorf("a mode line for a mode nobody told:\n%s", out)
	}
}

// Ground: contract — settings.mode in JSON is the app.mode as set, additive in
// schema_version 1; the top-level mode, "run", is the kind of report and does
// not change. A mode nobody told is "", like the other zeros of an absent echo.
func TestMode_JSONSettings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings *RunSettings
		want     string
	}{
		{"local", &RunSettings{MaxInFlight: 5000, Connections: 1, Mode: "local"}, "local"},
		{"real", &RunSettings{MaxInFlight: 5000, Connections: 1, Mode: "real"}, "real"},
		{"not told", &RunSettings{MaxInFlight: 5000, Connections: 1}, ""},
		{"no settings", nil, ""},
	} {
		run := jsonRun(echoReport())
		run.Run.Settings = tc.settings
		out := writeJSON(t, run)

		if got := field(t, out, "settings", "mode"); got != tc.want {
			t.Errorf("%s: settings.mode = %v, want %q", tc.name, got, tc.want)
		}
		if got := field(t, out, "mode"); got != "run" {
			t.Errorf("%s: mode = %v, want run: the kind of report", tc.name, got)
		}
	}
}

// Ground: contract — the search's top-level settings.mode is the config's; the
// top-level mode stays "breakpoint".
func TestMode_ASearchJSONSettings(t *testing.T) {
	for _, tc := range []struct{ mode, want string }{{"local", "local"}, {"real", "real"}, {"", ""}} {
		run := echoSearch(bpPlan, 2)
		run.Mode = tc.mode
		out := searchJSONOf(t, run)

		if got := field(t, out, "settings", "mode"); got != tc.want {
			t.Errorf("mode %q: settings.mode = %v, want %q", tc.mode, got, tc.want)
		}
		if got := field(t, out, "mode"); got != "breakpoint" {
			t.Errorf("mode %q: mode = %v, want breakpoint: the kind of report", tc.mode, got)
		}
	}
}

// Ground: contract — each run of the search carries the settings it was given,
// mode included, as its own report does for a plain run.
func TestMode_EachSearchRunKeepsTheModeItWasGiven(t *testing.T) {
	run := echoSearch(bpPlan, 2)
	run.Mode = "real"
	for i := range run.Result.Steps {
		run.Runs = append(run.Runs, RunReport{
			Report:   run.Result.Steps[i].Report,
			Settings: &RunSettings{MaxInFlight: 10 + i, Connections: 2, Warmup: time.Second, Mode: "real"},
		})
	}
	out := searchJSONOf(t, run)

	runs := field(t, out, "breakpoint", "runs").([]any)
	if len(runs) == 0 {
		t.Fatal("no runs in the report")
	}
	for i, r := range runs {
		if got := field(t, field(t, r, "report"), "settings", "mode"); got != "real" {
			t.Errorf("runs[%d].report.settings.mode = %v, want real", i, got)
		}
	}
}
