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
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// The line is a measured fact beside start lag: it says, it does not judge.

const cpuLinePrefix = "generator CPU:"

// cpuLine is the note about the generator's CPU, its wrapping undone, or "".
func cpuLine(notes []string) string {
	for _, n := range notes {
		if strings.HasPrefix(n, cpuLinePrefix) {
			return strings.Join(strings.Fields(n), " ")
		}
	}

	return ""
}

// busyRun is the example of the line: 22 s of stages, the first 2 of them
// warm-up, 18 of the 20 counted seconds at 0.95 of a core.
func busyRun(t *testing.T) RunReport {
	t.Helper()

	shares := []float64{.3, .3}
	for range 18 {
		shares = append(shares, .95)
	}
	shares = append(shares, .5, .5)

	report := cpuReport(22, 2, 22, 22)

	return RunReport{Report: report, GeneratorCPU: layCPU(t, report, 1, perSecond(1, shares...))}
}

func TestGeneratorCPULine_SaysHowManyOfTheCountedSecondsWereBusyAndFrom(t *testing.T) {
	run := busyRun(t)

	for where, text := range map[string]string{
		"runNotes":    cpuLine(runNotes(run)),
		"text report": strings.Join(strings.Fields(printedRun(run)), " "),
	} {
		for _, want := range []string{
			"generator CPU: >= 90% of the 1 core available",
			"in 18 of 20 s (from 2 s)",
			"latencies",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q:\n%s", where, want, text)
			}
		}
	}
}

// The text report is ASCII, so is every note: no "≥".
func TestGeneratorCPULine_IsASCII(t *testing.T) {
	line := cpuLine(runNotes(busyRun(t)))

	if line == "" {
		t.Fatal("no generator CPU line in the notes of a run with busy seconds")
	}
	if !isASCII(line) {
		t.Errorf("not ASCII: %q", line)
	}
}

func TestGeneratorCPULine_NamesTheCoresAvailable(t *testing.T) {
	for cores, want := range map[float64]string{
		1:   "of the 1 core available",
		1.5: "of the 1.5 cores available",
		2:   "of the 2 cores available",
		2.5: "of the 2.5 cores available",
	} {
		report := cpuReport(3, 0, 3, 3)
		run := RunReport{Report: report, GeneratorCPU: layCPU(t, report, cores, perSecond(cores, .95, .95, .95))}

		line := cpuLine(runNotes(run))
		if !strings.Contains(line, want) {
			t.Errorf("%v cores: no %q in %q", cores, want, line)
		}
		if strings.Contains(line, "1 cores") {
			t.Errorf("%v cores: %q says \"1 cores\"", cores, line)
		}
	}
}

// Above one core is possible (system time outside the Ps): the line still
// says 90%, the JSON keeps what was measured.
func TestGeneratorCPULine_AShareAboveOneIsStillNinetyPercentOrMore(t *testing.T) {
	report := cpuReport(3, 0, 3, 3)
	run := RunReport{Report: report, GeneratorCPU: layCPU(t, report, 1, perSecond(1, 1.3, 1.3, 1.3))}

	line := cpuLine(runNotes(run))
	if !strings.Contains(line, ">= 90% of the 1 core available in 3 of 3 s (from 0 s)") {
		t.Errorf("line = %q", line)
	}
	if strings.Contains(line, "130") {
		t.Errorf("line = %q, want no share above 100%% printed", line)
	}
}

// No busy counted second, no line: a loaded run is normal, the line marks the
// edge. A busy second that is not counted is not one.
func TestGeneratorCPULine_IsAbsentWithoutABusyCountedSecond(t *testing.T) {
	cases := map[string]RunReport{}

	add := func(name string, report engine.Report, cores float64, samples []CPUSample) {
		cases[name] = RunReport{Report: report, GeneratorCPU: layCPU(t, report, cores, samples)}
	}
	add("no second busy", cpuReport(3, 0, 3, 3), 1, perSecond(1, .5, .5, .5))
	add("only the warm-up busy", cpuReport(5, 2, 5, 5), 1, perSecond(1, .99, .99, .5, .5, .5))
	add("only the timeout tail busy", cpuReport(3, 0, 4, 4), 1, perSecond(1, .5, .5, .5, .99))
	add("run under a second", cpuReport(0.6, 0, 0.6, 1), 1, []CPUSample{cpuAt(0, 0), cpuAt(0.6, 0.6)})
	cases["not measured"] = RunReport{Report: cpuReport(3, 0, 3, 3)}

	for name, run := range cases {
		if line := cpuLine(runNotes(run)); line != "" {
			t.Errorf("%s: %q", name, line)
		}
		if strings.Contains(printedRun(run), cpuLinePrefix) {
			t.Errorf("%s: the text report has the line", name)
		}
	}
}

// The line states; it never changes what else the report says, the exit
// code's inputs or the verdict. The same run with and without the object
// differs by the line and the object, nothing else.
func TestGeneratorCPU_ChangesNoVerdictAndNoOtherNote(t *testing.T) {
	report := reportOf(t, times(200, waitCall{lag: 20 * ms, served: 2 * ms}))
	plain := RunReport{Report: report}
	busy := plain

	cpu := cpuReport(3, 0, 3, 3)
	busy.GeneratorCPU = layCPU(t, cpu, 1, perSecond(1, .95, .95, .95))

	if cpuLine(runNotes(busy)) == "" {
		t.Fatal("no generator CPU line in the notes of the run with busy seconds")
	}
	without := func(notes []string) []string {
		return slices.DeleteFunc(slices.Clone(notes), func(n string) bool { return strings.HasPrefix(n, cpuLinePrefix) })
	}
	if got, want := without(runNotes(busy)), runNotes(plain); !slices.Equal(got, want) {
		t.Errorf("notes differ beyond the line:\ngot:\n%q\nwant:\n%q", got, want)
	}
	if ClockTooCoarse(busy) != ClockTooCoarse(plain) {
		t.Error("the CPU object changed whether the clock is too coarse")
	}

	a, b := writeJSON(t, JSONRun{Target: "t", Outcome: OutcomeComplete, Run: plain}),
		writeJSON(t, JSONRun{Target: "t", Outcome: OutcomeComplete, Run: busy})
	if got := field(t, b, "tail_wait_cause"); got != "generator" {
		t.Errorf("tail_wait_cause = %v, want generator: the run waited on the generator", got)
	}
	dropCPU(b)
	dropCPU(a)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("the JSON differs beyond generator_cpu and its note:\nwithout: %v\nwith: %v", a, b)
	}
}

// dropCPU removes generator_cpu and the CPU line from a decoded run report.
func dropCPU(report map[string]any) {
	delete(report, "generator_cpu")

	notes, _ := report["notes"].([]any)
	report["notes"] = slices.DeleteFunc(notes, func(n any) bool {
		s, _ := n.(string)

		return strings.HasPrefix(s, cpuLinePrefix)
	})
}

// JSON contract.

// Ground: contract — the JSON names are an interface (schema_v1.txt).
func TestGeneratorCPUJSON_NamesAndValues(t *testing.T) {
	// 7 s of stages, 2 of warm-up, the run ran 10 s but the readings stop at 7.
	report := cpuReport(7, 2, 10, 10)
	run := jsonRun(report)
	run.Run.GeneratorCPU = layCPU(t, report, 1, perSecond(1, .3, .3, .5, .95, .95, .5, .5))

	out := writeJSON(t, run)

	cpu, ok := field(t, out, "generator_cpu").(map[string]any)
	if !ok {
		t.Fatalf("generator_cpu = %v, want an object", field(t, out, "generator_cpu"))
	}
	var keys []string
	for k := range cpu {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if want := []string{"available_cores", "busy_seconds", "first_busy_s", "seconds", "share"}; !slices.Equal(keys, want) {
		t.Errorf("generator_cpu has %v, want exactly %v", keys, want)
	}

	if cpu["available_cores"] != 1.0 || cpu["busy_seconds"] != 2.0 || cpu["first_busy_s"] != 3.0 {
		t.Errorf("available_cores %v, busy_seconds %v, first_busy_s %v; want 1, 2, 3",
			cpu["available_cores"], cpu["busy_seconds"], cpu["first_busy_s"])
	}
	if share, _ := cpu["share"].(float64); math.Abs(share-0.68) > 1e-9 {
		t.Errorf("share = %v, want 0.68", cpu["share"])
	}

	seconds, _ := cpu["seconds"].([]any)
	want := []any{.3, .3, .5, .95, .95, .5, .5, nil, nil, nil}
	if len(seconds) != len(want) {
		t.Fatalf("seconds = %v, want %v", seconds, want)
	}
	for i, w := range want {
		got, isNum := seconds[i].(float64)
		wantNum, wantIsNum := w.(float64)
		if isNum != wantIsNum || (isNum && math.Abs(got-wantNum) > 1e-9) {
			t.Errorf("seconds[%d] = %v, want %v", i, seconds[i], w)
		}
	}
	if methods := field(t, out, "methods").([]any); len(seconds) != len(field(t, methods[0], "seconds").([]any)) {
		t.Errorf("generator_cpu.seconds has %d entries, methods[0].seconds has %d: one grid",
			len(seconds), len(field(t, methods[0], "seconds").([]any)))
	}
}

func TestGeneratorCPUJSON_FirstBusyIsNullWithoutABusySecond(t *testing.T) {
	report := cpuReport(3, 0, 3, 3)
	run := jsonRun(report)
	run.Run.GeneratorCPU = layCPU(t, report, 1, perSecond(1, .5, .5, .5))

	out := writeJSON(t, run)

	if got := field(t, out, "generator_cpu", "first_busy_s"); got != nil {
		t.Errorf("first_busy_s = %v, want null", got)
	}
	if got := field(t, out, "generator_cpu", "busy_seconds"); got != 0.0 {
		t.Errorf("busy_seconds = %v, want 0", got)
	}
}

func TestGeneratorCPUJSON_IsNullWhenItCouldNotBeMeasured(t *testing.T) {
	out := writeJSON(t, jsonRun(cpuReport(3, 0, 3, 3)))

	if got := field(t, out, "generator_cpu"); got != nil {
		t.Errorf("generator_cpu = %v, want null", got)
	}
}

func TestGeneratorCPUJSON_NotesCarryTheLine(t *testing.T) {
	run := jsonRun(engine.Report{})
	run.Run = busyRun(t)

	out := writeJSON(t, run)

	var notes []string
	for _, n := range field(t, out, "notes").([]any) {
		notes = append(notes, n.(string))
	}
	if !strings.Contains(cpuLine(notes), "in 18 of 20 s (from 2 s)") {
		t.Errorf("notes lack the line: %q", notes)
	}
}

// A search is runs of its own: each step has its grid from its own start and
// its report carries the object. The search's own words stay as they were.
func TestGeneratorCPUJSON_AStepOfASearchCarriesItInItsReport(t *testing.T) {
	res := outcomes["broke"].res
	plain := make([]RunReport, len(res.Steps))
	for i := range res.Steps {
		plain[i] = RunReport{Report: res.Steps[i].Report}
	}
	with := slices.Clone(plain)
	cpu := cpuReport(3, 0, 3, 3)
	with[5].GeneratorCPU = layCPU(t, cpu, 1, perSecond(1, .95, .95, .95))

	decode := func(runs []RunReport) map[string]any {
		var buf bytes.Buffer
		if err := WriteBreakpointJSON(&buf, BreakpointRun{Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, Result: res, Runs: runs}); err != nil {
			t.Fatalf("write: %v", err)
		}
		var out map[string]any
		if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
			t.Fatalf("json: %v", err)
		}

		return out
	}
	reports := func(out map[string]any) []any {
		return field(t, out, "breakpoint", "runs").([]any)
	}

	got, base := decode(with), decode(plain)

	step := field(t, reports(got)[5], "report", "generator_cpu")
	if cpu, ok := step.(map[string]any); !ok || cpu["busy_seconds"] != 3.0 {
		t.Errorf("runs[5].report.generator_cpu = %v, want the object with 3 busy seconds", step)
	}
	if other := field(t, reports(got)[0], "report", "generator_cpu"); other != nil {
		t.Errorf("runs[0].report.generator_cpu = %v, want null: that step had no object", other)
	}

	// Nothing else of the search changes: not the outcome, not why, not the
	// rates, not the notes.
	for _, out := range []map[string]any{got, base} {
		for _, r := range reports(out) {
			dropCPU(r.(map[string]any)["report"].(map[string]any))
		}
	}
	if !reflect.DeepEqual(got, base) {
		t.Errorf("the search report differs beyond generator_cpu:\nwith: %v\nwithout: %v", got, base)
	}
}

// The search's text has no new line: the summary names no step's CPU.
func TestGeneratorCPULine_AddsNothingToTheSearchSummary(t *testing.T) {
	res := outcomes["broke"].res
	runs := make([]RunReport, len(res.Steps))
	cpu := cpuReport(3, 0, 3, 3)
	for i := range res.Steps {
		runs[i] = RunReport{Report: res.Steps[i].Report, GeneratorCPU: layCPU(t, cpu, 1, perSecond(1, .95, .95, .95))}
	}

	var with, without bytes.Buffer
	PrintBreakpoint(&with, BreakpointRun{Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, Result: res, Runs: runs})
	PrintBreakpoint(&without, BreakpointRun{Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, Result: res})

	if with.String() != without.String() {
		t.Errorf("the summary differs with the CPU objects:\nwith:\n%s\nwithout:\n%s", with.String(), without.String())
	}
}
