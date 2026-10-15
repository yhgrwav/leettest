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
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// echoReport is a one-method run of 1 s with the method's plan and measured
// rate told apart: it plans 300 rps for 40 s and was measured at 123 sent/s.
func echoReport() engine.Report {
	return engine.Report{
		Duration: time.Second, Sent: 123, Warmup: 1250 * time.Millisecond,
		Methods: []engine.MethodReport{{
			Method: "a.B/One", Sent: 123, RPS: 123.4,
			RPSLow: 300, RPSHigh: 300, PlannedDuration: 40 * time.Second,
		}},
	}
}

func echoLines(run RunReport) []string {
	var out bytes.Buffer
	PrintReport(&out, "localhost:50051", run)

	return strings.Split(out.String(), "\n")
}

// indexOfPrefix is the first line starting with prefix, -1 without one.
func indexOfPrefix(lines []string, prefix string) int {
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return i
		}
	}

	return -1
}

// Ground: contract — the run's name and settings are the lines right before
// "sent N, failed M": after the methods-source line when there is one (the
// line under "run finished:" is a contract of its own), else under the first
// line.
func TestEcho_TheLinesSitBeforeSentAndAfterTheMethodsSource(t *testing.T) {
	run := RunReport{
		Report: echoReport(), Name: "wallet",
		Settings: &RunSettings{MaxInFlight: 1234, Connections: 4, Warmup: 1250 * time.Millisecond},
	}
	const settings = "settings: warm-up 1.25s, max in flight 1234, connections 4"

	lines := echoLines(run)
	want := []string{"run finished: localhost:50051 in 1.0s", "name: wallet", settings}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("line %d = %q, want %q:\n%s", i, lines[i], w, strings.Join(lines, "\n"))
		}
	}
	if !strings.HasPrefix(lines[3], "sent 123, failed 0") {
		t.Errorf("line 3 = %q, want the sent line right after settings", lines[3])
	}

	run.MethodsSource = ReflectionSource()
	lines = echoLines(run)
	want = []string{"run finished: localhost:50051 in 1.0s", "methods: server reflection", "name: wallet", settings}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("with a methods source: line %d = %q, want %q:\n%s", i, lines[i], w, strings.Join(lines, "\n"))
		}
	}
	if !strings.HasPrefix(lines[4], "sent 123, failed 0") {
		t.Errorf("with a methods source: line 4 = %q, want the sent line", lines[4])
	}
}

// Ground: contract — the defaults are echoed too, so a reader sees what a run
// without flags used.
func TestEcho_TheDefaultsAreEchoed(t *testing.T) {
	lines := echoLines(RunReport{
		Report: echoReport(), Name: "tour", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1},
	})

	if i := indexOfPrefix(lines, "settings:"); i < 0 || lines[i] != "settings: warm-up 0s, max in flight 5000, connections 1" {
		t.Errorf("no line %q in:\n%s", "settings: warm-up 0s, max in flight 5000, connections 1", strings.Join(lines, "\n"))
	}
}

// Ground: contract — a configured duration is echoed as the config parses
// it (time.Duration.String), never rounded: formatDuration would turn 1.25s
// into 1.3s and 30s into 30.0s, and the echo would lie about the config.
func TestEcho_DurationsAreAsConfigured(t *testing.T) {
	tests := []struct {
		warmup time.Duration
		want   string
	}{
		{1250 * time.Millisecond, "warm-up 1.25s,"},
		{30 * time.Second, "warm-up 30s,"},
		{90 * time.Second, "warm-up 1m30s,"},
		{0, "warm-up 0s,"},
	}

	for _, tt := range tests {
		lines := echoLines(RunReport{
			Report: echoReport(), Name: "x", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1, Warmup: tt.warmup},
		})
		i := indexOfPrefix(lines, "settings:")
		if i < 0 || !strings.Contains(lines[i], tt.want) {
			t.Errorf("warmup %v: no settings line containing %q in:\n%s", tt.warmup, tt.want, strings.Join(lines, "\n"))
		}
	}
}

// Ground: contract — under a method row the configured plan: its rate and
// length. The row itself stays the table's fixed width, so a tail does not
// push the columns (80/120), and the plan comes before the dataset line.
func TestEcho_AMethodsPlanIsASubline(t *testing.T) {
	report := echoReport()
	report.Methods[0].Dataset = &engine.DatasetReport{File: "w.jsonl", Records: 4, Used: 4, UsedMax: 1}
	lines := echoLines(RunReport{Report: report, Name: "x", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1}})

	row := indexOfPrefix(lines, "a.B/One")
	if row < 0 || row+3 >= len(lines) {
		t.Fatalf("no method row:\n%s", strings.Join(lines, "\n"))
	}
	if lines[row+1] != "  plan: 300 rps for 40s" {
		t.Errorf("the line under the method = %q, want %q", lines[row+1], "  plan: 300 rps for 40s")
	}
	if !strings.HasPrefix(lines[row+2], "  p99.9:") || !strings.HasPrefix(lines[row+3], "  data:") {
		t.Errorf("the lines after the plan = %q, %q, want p99.9 then the dataset line", lines[row+2], lines[row+3])
	}
	if header := indexOfPrefix(lines, "method "); header < 0 || len(lines[header]) != len(lines[row]) {
		t.Errorf("the method row is %d wide, the header %d: the plan went into the table", len(lines[row]), len(lines[header]))
	}
}

// Ground: contract — a ramp is told as a range, as the silence note tells it,
// and the length of a multi-stage plan is the sum, not rounded.
func TestEcho_ARampPlanIsARange(t *testing.T) {
	report := echoReport()
	report.Methods[0].RPSLow, report.Methods[0].RPSHigh = 100, 300
	report.Methods[0].PlannedDuration = 90 * time.Second
	lines := echoLines(RunReport{Report: report, Name: "x", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1}})

	if i := indexOfPrefix(lines, "  plan:"); i < 0 || lines[i] != "  plan: 100-300 rps for 1m30s" {
		t.Errorf("no line %q in:\n%s", "  plan: 100-300 rps for 1m30s", strings.Join(lines, "\n"))
	}
}

// Ground: contract — the text report stays ASCII: a name from outside comes
// out escaped, like every other line. (Equality with the JSON name holds for
// an ASCII name only, checked end to end.)
func TestEcho_ANonASCIINameStaysASCII(t *testing.T) {
	var out bytes.Buffer
	PrintReport(&out, "localhost:50051", RunReport{
		Report: echoReport(), Name: "кошелёк", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1},
	})

	text := out.String()
	for i := range len(text) {
		if text[i] >= 0x80 {
			t.Fatalf("the report holds a non-ASCII byte at %d:\n%q", i, text)
		}
	}
	if !strings.Contains(text, "\nname: ") {
		t.Errorf("no name line:\n%s", text)
	}
}

// echoSearch is a search held through every step with plan, over connections.
func echoSearch(plan breakpoint.Plan, connections int) BreakpointRun {
	return BreakpointRun{
		Target: "t:1", Name: "wallet", Method: "pkg.Svc/Do", Plan: plan, Connections: connections,
		Result: outcomes["held_all"].res,
	}
}

func echoSearchText(run BreakpointRun) string {
	var buf bytes.Buffer
	PrintBreakpoint(&buf, run)

	return buf.String()
}

// Ground: contract — the search's name and settings come after the blank line
// that closes the answer block, before the table of runs, and a blank line
// follows them: the answer block (checked as a block elsewhere) is untouched.
// The cap is the flag's when it was set, else "per step".
func TestEcho_ASearchTellsItsNameAndSettingsAboveTheRuns(t *testing.T) {
	tests := []struct {
		name        string
		maxInFlight int
		want        string
	}{
		{"no flag", 0, "settings: settle 1s, hold 5s, max in flight per step, connections 2"},
		{"flag", 777, "settings: settle 1s, hold 5s, max in flight 777, connections 2"},
	}

	for _, tt := range tests {
		plan := bpPlan
		plan.MaxInFlight = tt.maxInFlight
		out := echoSearchText(echoSearch(plan, 2))

		block, rest, ok := strings.Cut(out, "\n\n")
		if !ok {
			t.Fatalf("%s: no blank line after the answer:\n%s", tt.name, out)
		}
		if strings.Contains(block, "name:") || strings.Contains(block, "settings:") {
			t.Errorf("%s: the answer block holds the echo, which belongs below it:\n%s", tt.name, block)
		}
		table := strings.Index(rest, "  kind")
		if table < 0 {
			t.Fatalf("%s: no table of runs:\n%s", tt.name, out)
		}
		if got, want := rest[:table], "name: wallet\n"+tt.want+"\n\n"; got != want {
			t.Errorf("%s: between the answer and the table:\n%q\nwant\n%q", tt.name, got, want)
		}
	}
}

// Ground: contract — configured durations of the search are as written too.
func TestEcho_ASearchDurationsAreNotRounded(t *testing.T) {
	plan := bpPlan
	plan.Settle, plan.Hold = 1250*time.Millisecond, 90*time.Second

	if out := echoSearchText(echoSearch(plan, 1)); !strings.Contains(out, "settings: settle 1.25s, hold 1m30s,") {
		t.Errorf("settle 1.25s and hold 1m30s are not echoed as written:\n%s", out)
	}
}

// Ground: contract — the search's text and JSON name agree for an ASCII name.
func TestEcho_ASearchTextNameIsTheJSONName(t *testing.T) {
	run := echoSearch(bpPlan, 1)
	out := searchJSONOf(t, run)

	if got := field(t, out, "name"); got != "wallet" || !strings.Contains(echoSearchText(run), "\nname: "+got.(string)+"\n") {
		t.Errorf("JSON name %v, text:\n%s", got, echoSearchText(run))
	}
}

func searchJSONOf(t *testing.T, run BreakpointRun) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	if err := WriteBreakpointJSON(&buf, run); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}

	return out
}

// Ground: contract — settings is the echo of the config: the cap, the
// connections as configured (not what opened), the warm-up in microseconds.
// warmup_us stays where it was and holds the engine's warm-up, which is a
// different number here to tell them apart.
func TestEcho_JSONSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings RunSettings
	}{
		{"set", RunSettings{MaxInFlight: 1234, Connections: 4, Warmup: 1250 * time.Millisecond}},
		{"defaults", RunSettings{MaxInFlight: 5000, Connections: 1}},
	}

	for _, tt := range tests {
		run := jsonRun(echoReport())
		run.Run.Settings = &tt.settings
		out := writeJSON(t, run)

		want := map[string]float64{
			"max_in_flight": float64(tt.settings.MaxInFlight),
			"connections":   float64(tt.settings.Connections),
			"warmup_us":     float64(tt.settings.Warmup.Microseconds()),
		}
		for key, w := range want {
			if got := field(t, out, "settings", key); got != w {
				t.Errorf("%s: settings.%s = %v, want %v", tt.name, key, got, w)
			}
		}
		if got := field(t, out, "warmup_us"); got != 1250000.0 {
			t.Errorf("%s: warmup_us = %v, want 1250000: it stays the engine's", tt.name, got)
		}
	}
}

// Ground: contract — a method carries its configured length as
// planned_duration_us; rps stays the measured rate and the planned rate stays
// planned_rps_low/high, not one name for two meanings.
func TestEcho_JSONMethodPlan(t *testing.T) {
	run := jsonRun(echoReport())
	run.Run.Settings = &RunSettings{MaxInFlight: 5000, Connections: 1}
	method := field(t, writeJSON(t, run), "methods").([]any)[0].(map[string]any)

	if got := method["planned_duration_us"]; got != 40000000.0 {
		t.Errorf("planned_duration_us = %v, want 40000000", got)
	}
	if got := method["rps"]; got != 123.4 {
		t.Errorf("rps = %v, want 123.4: the measured rate", got)
	}
	if method["planned_rps_low"] != 300.0 || method["planned_rps_high"] != 300.0 {
		t.Errorf("planned_rps %v-%v, want 300-300", method["planned_rps_low"], method["planned_rps_high"])
	}
}

// Ground: contract — the search's top-level settings: the cap is the flag's, or
// null when each step takes its own (never the default of a plain run); the
// warm-up is the settle; the connections as configured.
func TestEcho_ASearchJSONSettings(t *testing.T) {
	tests := []struct {
		name        string
		maxInFlight int
		want        any
	}{
		{"no flag", 0, nil},
		{"flag", 777, 777.0},
	}

	for _, tt := range tests {
		plan := bpPlan
		plan.MaxInFlight = tt.maxInFlight
		out := searchJSONOf(t, echoSearch(plan, 3))

		if got := field(t, out, "settings", "max_in_flight"); got != tt.want {
			t.Errorf("%s: settings.max_in_flight = %v, want %v", tt.name, got, tt.want)
		}
		if got := field(t, out, "settings", "connections"); got != 3.0 {
			t.Errorf("%s: settings.connections = %v, want 3", tt.name, got)
		}
		if got := field(t, out, "settings", "warmup_us"); got != 1000000.0 {
			t.Errorf("%s: settings.warmup_us = %v, want 1000000: the settle", tt.name, got)
		}
	}
}

// Ground: contract — each run of the search has its own settings in its
// report: its own cap, not the search's, and the settle as its warm-up.
func TestEcho_EachSearchRunHasItsOwnSettings(t *testing.T) {
	run := echoSearch(bpPlan, 2)
	for i := range run.Result.Steps {
		run.Runs = append(run.Runs, RunReport{
			Report:   run.Result.Steps[i].Report,
			Settings: &RunSettings{MaxInFlight: 10 + i, Connections: 2, Warmup: time.Second},
		})
	}
	out := searchJSONOf(t, run)

	runs := field(t, out, "breakpoint", "runs").([]any)
	if len(runs) != len(run.Runs) {
		t.Fatalf("%d runs in the report, want %d", len(runs), len(run.Runs))
	}
	for i, r := range runs {
		report := field(t, r, "report")
		if got := field(t, report, "settings", "max_in_flight"); got != float64(10+i) {
			t.Errorf("runs[%d].report.settings.max_in_flight = %v, want %d: the cap of that run", i, got, 10+i)
		}
		if got := field(t, report, "settings", "warmup_us"); got != 1000000.0 {
			t.Errorf("runs[%d].report.settings.warmup_us = %v, want 1000000", i, got)
		}
		if got := field(t, report, "settings", "connections"); got != 2.0 {
			t.Errorf("runs[%d].report.settings.connections = %v, want 2", i, got)
		}
	}
}
