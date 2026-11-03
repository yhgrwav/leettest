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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// A report says when the generator did not wait to each call's moment
// exactly: the engine hands over the reasons (no_precise_sleep, clock_step,
// gomaxprocs), the text puts one line under the machine's, the JSON always has
// the field. The reasons are injected, so none of this depends on the host the
// test runs on.

// schedBody is what the line says after the reasons: the delay is inside the
// latencies, and not inside the p99 without the client-side waits (which takes
// the whole start lag off). schedTail adds the run's own start lag, p99 and
// max, the numbers the start lag note prints too; a run with no start lag
// ends on schedBody.
const (
	schedBody = ": calls may start late, and latencies include that delay (p99 without client-side waits does not)."
	schedTail = schedBody + " Start lag in this run: p99 1.30ms, max 2.10ms."
)

// The reasons in words, as the line spells them: the OS by the name its
// passport gives (or "this host" without a passport), the clock step by the 1us
// the verdict draws the line at, the Ps by the 2 it needs.
const (
	wordsStep  = "clock step 1us or coarser"
	wordsProcs = "GOMAXPROCS below 2"
)

const (
	schedNoSleep = "no_precise_sleep"
	schedStep    = "clock_step"
	schedProcs   = "gomaxprocs"
)

func schedPassport(goos string) *Passport {
	p := crPassport()
	p.OS = goos

	return p
}

// schedRun is a run on os whose schedule was not exact for reasons, with a
// start lag of 1.3ms at p99 and 2.1ms at most.
func schedRun(goos string, reasons ...string) RunReport {
	return RunReport{
		Report: engine.Report{
			Sent: 1000, Duration: 10 * time.Second, Planned: 10 * time.Second,
			StartLagP99: lagQuantile(1300 * time.Microsecond), StartLagMax: 2100 * time.Microsecond,
			ScheduleNotExact: reasons,
			Methods:          []engine.MethodReport{lateMethod(lateA, 5*time.Millisecond, 5*time.Millisecond)},
		},
		Passport: schedPassport(goos),
	}
}

func schedSearch(goos string, reasons ...string) BreakpointRun {
	return schedSearchBy(goos, reasons, reasons, reasons)
}

// schedSearchBy is a search with a step for each list of reasons, the engine's
// own for that step.
func schedSearchBy(goos string, perStep ...[]string) BreakpointRun {
	rates := []int{100, 125, 156, 195, 244}
	steps := make([]breakpoint.Step, len(perStep))

	for i, reasons := range perStep {
		steps[i] = held(rates[i])
		steps[i].Report.ScheduleNotExact = reasons
	}

	return BreakpointRun{
		Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, Passport: schedPassport(goos),
		Result: breakpoint.Result{Outcome: breakpoint.HeldThroughout, Held: rates[len(perStep)-1], Steps: steps},
	}
}

// scheduleLines are the lines of out that say something of the schedule.
func scheduleLines(out string) []string {
	var lines []string

	for line := range strings.Lines(out) {
		if strings.HasPrefix(line, "schedule:") {
			lines = append(lines, strings.TrimSuffix(line, "\n"))
		}
	}

	return lines
}

func schedStrings(t *testing.T, v any) []string {
	t.Helper()

	list, ok := v.([]any)
	if !ok {
		t.Fatalf("%v (%T) is not a list", v, v)
	}

	out := make([]string, 0, len(list))
	for _, e := range list {
		s, ok := e.(string)
		if !ok {
			t.Fatalf("%v (%T) is not a string", e, e)
		}

		out = append(out, s)
	}

	return out
}

// One line, a reason after the other in the engine's order, each in words: the
// OS by its name (a macOS host says darwin by what the code knows of it, not
// by a measurement), the clock step by the 1us the verdict draws the line at,
// GOMAXPROCS by the 2 it needs. The numbers of the run follow.
//
// Ground: contract — the line is the report's wording, copied verbatim into
// the reply to a developer.
func TestSchedule_TextSaysWhyInOneLine(t *testing.T) {
	for _, tc := range []struct {
		name    string
		os      string
		reasons []string
		want    string
	}{
		{"no microsecond sleep, windows", "windows", []string{schedNoSleep},
			"schedule: not exact (no microsecond sleep on windows)" + schedTail},
		{"no microsecond sleep, darwin", "darwin", []string{schedNoSleep},
			"schedule: not exact (no microsecond sleep on darwin)" + schedTail},
		{"a coarse clock", "linux", []string{schedStep},
			"schedule: not exact (" + wordsStep + ")" + schedTail},
		{"one P", "linux", []string{schedProcs},
			"schedule: not exact (" + wordsProcs + ")" + schedTail},
		{"two reasons", "windows", []string{schedNoSleep, schedProcs},
			"schedule: not exact (no microsecond sleep on windows, " + wordsProcs + ")" + schedTail},
		{"three reasons, in the engine's order", "windows", []string{schedNoSleep, schedStep, schedProcs},
			"schedule: not exact (no microsecond sleep on windows, " + wordsStep + ", " + wordsProcs + ")" + schedTail},
	} {
		out := printedRun(schedRun(tc.os, tc.reasons...))

		if got := scheduleLines(out); !slices.Equal(got, []string{tc.want}) {
			t.Errorf("%s: schedule lines %q, want %q\n%s", tc.name, got, []string{tc.want}, out)
		}
	}
}

// A run with no start lag to show (the condition of the start lag note: no
// p99 and no max) keeps the line, which then ends on the claim and has no
// sentence of numbers: "Start lag in this run: p99 -, max 0" would say a lag
// was measured. A max alone is a lag to show.
//
// Ground: boundary — the numbers sentence stands exactly where startLagNote
// prints its numbers.
func TestSchedule_TextWithoutAStartLagHasNoNumbers(t *testing.T) {
	for _, tc := range []struct {
		name string
		p99  metrics.Quantile
		max  time.Duration
		want string
	}{
		{"no lag at all", metrics.Quantile{}, 0, "schedule: not exact (no microsecond sleep on windows)" + schedBody},
		{"a negative max and no p99", metrics.Quantile{}, -time.Microsecond, "schedule: not exact (no microsecond sleep on windows)" + schedBody},
		{"a max and no p99", metrics.Quantile{}, 2100 * time.Microsecond,
			"schedule: not exact (no microsecond sleep on windows)" + schedBody + " Start lag in this run: p99 -, max 2.10ms."},
		{"a p99 and no max", lagQuantile(1300 * time.Microsecond), 0,
			"schedule: not exact (no microsecond sleep on windows)" + schedBody + " Start lag in this run: p99 1.30ms, max 0."},
	} {
		run := schedRun("windows", schedNoSleep)
		run.StartLagP99, run.StartLagMax = tc.p99, tc.max

		out := printedRun(run)
		if got := scheduleLines(out); !slices.Equal(got, []string{tc.want}) {
			t.Errorf("%s: schedule lines %q, want %q\n%s", tc.name, got, []string{tc.want}, out)
		}
	}
}

// A report with no passport (a library caller, a step of a search) has no OS
// to name, and the line is still printed: the reasons are the engine's, not
// the passport's, and saying nothing would hide the cause. Only the OS reason
// reads the passport.
//
// Ground: contract — the line does not depend on the passport; "this host"
// stands where the OS name would.
func TestSchedule_TextWithoutAPassportSaysThisHost(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reasons []string
		want    string
	}{
		{"no microsecond sleep", []string{schedNoSleep},
			"schedule: not exact (no microsecond sleep on this host)" + schedTail},
		{"all three", []string{schedNoSleep, schedStep, schedProcs},
			"schedule: not exact (no microsecond sleep on this host, " + wordsStep + ", " + wordsProcs + ")" + schedTail},
		{"a reason that names no OS", []string{schedStep},
			"schedule: not exact (" + wordsStep + ")" + schedTail},
	} {
		run := schedRun("windows", tc.reasons...)
		run.Passport = nil

		out := printedRun(run)
		if got := scheduleLines(out); !slices.Equal(got, []string{tc.want}) {
			t.Errorf("%s: schedule lines %q, want %q\n%s", tc.name, got, []string{tc.want}, out)
		}
	}
}

// The line is for a schedule that was not exact: an exact one prints nothing,
// as the other notes do. Empty and nil reasons are the same exact schedule.
//
// Ground: contract — silence is the exact schedule; a line there would be
// noise that teaches the reader to skip it.
func TestSchedule_TextSaysNothingWhenExact(t *testing.T) {
	// The inexact run is first: a report that never prints the line passes
	// the rest of this test, so it must fail here.
	if got := scheduleLines(printedRun(schedRun("windows", schedNoSleep))); len(got) != 1 {
		t.Fatalf("an inexact schedule: schedule lines %q, want one", got)
	}

	for name, reasons := range map[string][]string{"nil": nil, "empty": {}} {
		out := printedRun(schedRun("linux", reasons...))
		if got := scheduleLines(out); len(got) != 0 {
			t.Errorf("%s reasons: schedule lines %q in an exact run:\n%s", name, got, out)
		}
	}
}

// The line stands in the header with the machine's: right under the machine
// line, or under the labels line when the run has labels (that one stays under
// the machine line).
//
// Ground: contract — the header is read as one block.
func TestSchedule_TextStandsUnderTheMachine(t *testing.T) {
	for _, withLabels := range []bool{false, true} {
		run := schedRun("windows", schedNoSleep)
		if withLabels {
			run.Passport.Labels = map[string]string{"env": "ci"}
		}

		lines := strings.Split(printedRun(run), "\n")

		at := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "machine: ") })
		if at < 0 {
			t.Fatalf("labels %v: no machine line:\n%s", withLabels, strings.Join(lines, "\n"))
		}

		next := at + 1
		if withLabels {
			if next >= len(lines) || !strings.HasPrefix(lines[next], "labels: ") {
				t.Fatalf("labels %v: line after machine is not the labels:\n%s", withLabels, strings.Join(lines, "\n"))
			}

			next++
		}

		if next >= len(lines) || !strings.HasPrefix(lines[next], "schedule: not exact") {
			t.Errorf("labels %v: the schedule line is not right under the passport lines:\n%s", withLabels, strings.Join(lines, "\n"))
		}
	}
}

// A search says it once, in its header: the schedule is the machine's, not a
// step's, and it is the same for every step. Exact steps print nothing.
//
// Ground: contract — one line in a search report, not one for each step.
func TestSchedule_ASearchSaysItOnce(t *testing.T) {
	out := printedSearchRun(schedSearch("windows", schedNoSleep))

	// The steps' start lags differ and the worst step is past the breaking
	// point, where the lag is the generator's overload and not the timer's:
	// the line carries no numbers.
	want := []string{"schedule: not exact (no microsecond sleep on windows)" + schedBody}
	if lines := scheduleLines(out); !slices.Equal(lines, want) {
		t.Fatalf("search with three inexact steps: schedule lines %q, want %q\n%s", lines, want, out)
	}

	rows := strings.Split(out, "\n")
	at := slices.IndexFunc(rows, func(l string) bool { return strings.HasPrefix(l, "machine: ") })
	if at < 0 || at+1 >= len(rows) || !strings.HasPrefix(rows[at+1], "schedule: ") {
		t.Errorf("the schedule line is not right under the machine line:\n%s", out)
	}

	if got := scheduleLines(printedSearchRun(schedSearch("linux"))); len(got) != 0 {
		t.Errorf("an exact search: schedule lines %q, want none", got)
	}
}

// The line stands under the labels when the search has them, as a run's does.
//
// Ground: contract — the header is read as one block.
func TestSchedule_ASearchLineStandsUnderTheLabels(t *testing.T) {
	run := schedSearch("windows", schedNoSleep)
	run.Passport.Labels = map[string]string{"env": "ci"}

	lines := strings.Split(printedSearchRun(run), "\n")

	at := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "machine: ") })
	if at < 0 || at+2 >= len(lines) || !strings.HasPrefix(lines[at+1], "labels: ") ||
		!strings.HasPrefix(lines[at+2], "schedule: not exact") {
		t.Errorf("the schedule line is not right under the labels line:\n%s", strings.Join(lines, "\n"))
	}
}

// The steps of a search have a clock of their own and may not agree on the
// reasons. The line is the union, in the engine's order (no_precise_sleep,
// clock_step, gomaxprocs); a reason that was not in every step says in how many
// it was. The reasons of the steps never print once for each step.
//
// Ground: contract — one line for a search whatever its steps said; a reason
// shown without its count would claim it held for the whole search.
func TestSchedule_ASearchNamesTheUnionOfItsSteps(t *testing.T) {
	for _, tc := range []struct {
		name    string
		os      string
		perStep [][]string
		want    string
	}{
		{"every reason in some steps", "windows",
			[][]string{{schedNoSleep, schedStep}, {schedNoSleep}, {schedNoSleep, schedProcs}},
			"schedule: not exact (no microsecond sleep on windows, " + wordsStep + " in 1 of 3 steps, " +
				wordsProcs + " in 1 of 3 steps)" + schedBody},
		{"the engine's order, not the order of the steps", "windows",
			[][]string{{schedProcs}, {schedProcs}, {schedNoSleep}},
			"schedule: not exact (no microsecond sleep on windows in 1 of 3 steps, " + wordsProcs + " in 2 of 3 steps)" + schedBody},
		{"one step of four", "linux",
			[][]string{nil, {schedStep}, nil, nil},
			"schedule: not exact (" + wordsStep + " in 1 of 4 steps)" + schedBody},
		{"the last step only", "linux",
			[][]string{{}, {}, {schedProcs}},
			"schedule: not exact (" + wordsProcs + " in 1 of 3 steps)" + schedBody},
		{"every step has the reason, one has another too", "linux",
			[][]string{{schedProcs}, {schedProcs, schedStep}, {schedProcs}},
			"schedule: not exact (" + wordsStep + " in 1 of 3 steps, " + wordsProcs + ")" + schedBody},
		{"a single step", "windows",
			[][]string{{schedNoSleep}},
			"schedule: not exact (no microsecond sleep on windows)" + schedBody},
	} {
		out := printedSearchRun(schedSearchBy(tc.os, tc.perStep...))

		if got := scheduleLines(out); !slices.Equal(got, []string{tc.want}) {
			t.Errorf("%s: schedule lines %q, want %q\n%s", tc.name, got, []string{tc.want}, out)
		}
	}
}

// Without a passport the OS is "this host" in a search too, and the line is
// still there.
//
// Ground: contract — the line does not depend on the passport.
func TestSchedule_ASearchWithoutAPassportSaysThisHost(t *testing.T) {
	run := schedSearch("windows", schedNoSleep)
	run.Passport = nil

	want := []string{"schedule: not exact (no microsecond sleep on this host)" + schedBody}
	if got := scheduleLines(printedSearchRun(run)); !slices.Equal(got, want) {
		t.Errorf("schedule lines %q, want %q", got, want)
	}
}

// A search's JSON has no schedule of its own: each step's report has it (a
// step measures its own clock, so the answer is not the search's), and the
// passport of a search is the machine and the config, not the schedule.
//
// Ground: contract — the search schema takes no new top-level key, and the
// passport no new field.
func TestSchedule_ASearchJSONHasNoScheduleOfItsOwn(t *testing.T) {
	out := searchJSONOf(t, schedSearch("windows", schedNoSleep))

	if _, ok := out["schedule"]; ok {
		t.Errorf("a search report has a top-level schedule: %v", out["schedule"])
	}

	passport, ok := out["passport"].(map[string]any)
	if !ok {
		t.Fatalf("passport %v (%T), want an object", out["passport"], out["passport"])
	}

	if _, ok := passport["schedule"]; ok {
		t.Errorf("the passport of a search has a schedule: %v", passport["schedule"])
	}
}

// The field is always there, exact or not, and the list is a list, never null:
// a consumer reads it without a nil check.
//
// Ground: contract — schedule is part of schema_version 1.
func TestSchedule_JSONHasTheFieldAlways(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reasons []string
		exact   bool
		want    []string
	}{
		{"exact, nil", nil, true, []string{}},
		{"exact, empty", []string{}, true, []string{}},
		{"one reason", []string{schedProcs}, false, []string{schedProcs}},
		{"three reasons in order", []string{schedNoSleep, schedStep, schedProcs}, false, []string{schedNoSleep, schedStep, schedProcs}},
	} {
		out := writeJSON(t, jsonRun(engine.Report{
			ScheduleNotExact: tc.reasons,
			Methods:          []engine.MethodReport{{Method: "/pkg.S/M"}},
		}))

		if got := field(t, out, "schedule", "exact"); got != tc.exact {
			t.Errorf("%s: schedule.exact %v, want %v", tc.name, got, tc.exact)
		}

		raw := field(t, out, "schedule", "not_exact_reasons")
		if raw == nil {
			t.Errorf("%s: schedule.not_exact_reasons is null, want a list", tc.name)

			continue
		}

		if got := schedStrings(t, raw); !slices.Equal(got, tc.want) {
			t.Errorf("%s: schedule.not_exact_reasons %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Each step of a search is a run report, and says it as a run does.
//
// Ground: contract — breakpoint.runs[].report is the run schema.
func TestSchedule_JSONOfASearchStepHasIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reasons []string
		exact   bool
	}{
		{"inexact", []string{schedNoSleep, schedProcs}, false},
		{"exact", nil, true},
	} {
		out := searchJSONOf(t, schedSearch("windows", tc.reasons...))

		runs, ok := field(t, out, "breakpoint", "runs").([]any)
		if !ok || len(runs) != 3 {
			t.Fatalf("%s: breakpoint.runs %v, want three", tc.name, field(t, out, "breakpoint", "runs"))
		}

		for i, r := range runs {
			if got := field(t, r, "report", "schedule", "exact"); got != tc.exact {
				t.Errorf("%s: run %d: schedule.exact %v, want %v", tc.name, i, got, tc.exact)
			}

			raw := field(t, r, "report", "schedule", "not_exact_reasons")
			if raw == nil {
				t.Errorf("%s: run %d: schedule.not_exact_reasons is null", tc.name, i)

				continue
			}

			want := tc.reasons
			if want == nil {
				want = []string{}
			}

			if got := schedStrings(t, raw); !slices.Equal(got, want) {
				t.Errorf("%s: run %d: schedule.not_exact_reasons %q, want %q", tc.name, i, got, want)
			}
		}
	}
}

// schedWithoutKey is f as a report written before the schedule field would
// be: the file has no "schedule" key, and it is read back by ReadReport, the
// way compare reads it.
func schedWithoutKey(t *testing.T, f RunFile) RunFile {
	t.Helper()

	raw, err := json.Marshal(f.Report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := doc["schedule"]; !ok {
		t.Fatal("fixture: the report has no schedule key to drop")
	}

	delete(doc, "schedule")

	if raw, err = json.Marshal(doc); err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if bytes.Contains(raw, []byte(`"schedule"`)) {
		t.Fatalf("fixture: a schedule key is left in %s", raw)
	}

	path := filepath.Join(t.TempDir(), "old.json")
	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	read, err := ReadReport(path)
	if err != nil || read.Mode != ModeRun {
		t.Fatalf("read the report without a schedule: mode %q, %v", read.Mode, err)
	}

	return RunFile{Path: f.Path, Report: read.Run}
}

// schedDiff is a difference as the compare report names it, without the text
// lines it keeps for itself.
func schedDiff(d CompareDiff) [3]string { return [3]string{d.Field, d.Base, d.New} }

// A report written before the schedule field has no key, and a missing key is
// not a schedule that was not exact: it is `none`, the way a missing passport
// is. Read as "not exact with no reasons" it would put a claim into a run that
// never made it. A difference of `none` against a report that has one makes
// the reports not comparable, as any other difference does.
//
// Ground: contract — an old report is read as it was, not as a failure.
func TestCompareRuns_AReportWithoutAScheduleIsNone(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		oldBase, oldNew      bool
		base, nw             []string
		wantLine             string
		wantField, wantBase  string
		wantNew              string
		comparableAfterwards bool
	}{
		{name: "old base, exact new", oldBase: true, wantLine: "differs: schedule: none -> exact",
			wantField: "schedule", wantBase: "none", wantNew: "exact"},
		{name: "exact base, old new", oldNew: true, wantLine: "differs: schedule: exact -> none",
			wantField: "schedule", wantBase: "exact", wantNew: "none"},
		{name: "old base, not exact new", oldBase: true, nw: []string{schedNoSleep, schedStep},
			wantLine:  "differs: schedule: none -> not exact (no_precise_sleep, clock_step)",
			wantField: "schedule", wantBase: "none", wantNew: "not exact (no_precise_sleep, clock_step)"},
		{name: "not exact base, old new", oldNew: true, base: []string{schedProcs},
			wantLine:  "differs: schedule: not exact (gomaxprocs) -> none",
			wantField: "schedule", wantBase: "not exact (gomaxprocs)", wantNew: "none"},
		{name: "both old", oldBase: true, oldNew: true, comparableAfterwards: true},
		{name: "both old, whatever the engine said", oldBase: true, oldNew: true,
			base: []string{schedProcs}, comparableAfterwards: true},
	} {
		for _, limits := range [][2]string{{"", ""}, {"20%", "1%"}} {
			b, n := crBase(), crNew()
			b.notExact, n.notExact = tc.base, tc.nw

			base, nw := b.file(t, "base.json"), n.file(t, "new.json")
			if tc.oldBase {
				base = schedWithoutKey(t, base)
			}

			if tc.oldNew {
				nw = schedWithoutKey(t, nw)
			}

			var out bytes.Buffer

			got, rep := CompareRunsReport(&out, base, nw, crLimit(t, "-max-p99-rise", limits[0]), crLimit(t, "-max-errors-rise", limits[1]))

			var wantLines []string
			if tc.wantLine != "" {
				wantLines = []string{tc.wantLine}
			}

			if diffs := crDiffs(out.String()); !slices.Equal(diffs, wantLines) {
				t.Errorf("%s %v: difference lines %q, want %q\n%s", tc.name, limits, diffs, wantLines, out.String())
			}

			var wantDiffs [][3]string
			if tc.wantField != "" {
				wantDiffs = [][3]string{{tc.wantField, tc.wantBase, tc.wantNew}}
			}

			var gotDiffs [][3]string
			for _, d := range rep.Differences {
				gotDiffs = append(gotDiffs, schedDiff(d))
			}

			if !slices.Equal(gotDiffs, wantDiffs) {
				t.Errorf("%s %v: differences in the JSON %q, want %q", tc.name, limits, gotDiffs, wantDiffs)
			}

			if got.Comparable != tc.comparableAfterwards {
				t.Errorf("%s %v: comparable %v, want %v", tc.name, limits, got.Comparable, tc.comparableAfterwards)
			}
		}
	}
}

// The compare report's JSON names a difference of schedules the way the text
// line does: field `schedule`, and for each side `exact` or `not exact` with
// the reasons in parentheses, in the engine's order. It stands in the order of
// the text: the passport, the schedule, the methods.
//
// Ground: contract — differences[] is part of the compare report's schema;
// the text and the JSON are built from one value.
func TestCompareRuns_TheScheduleDifferenceIsInTheJSONReport(t *testing.T) {
	for _, tc := range []struct {
		name       string
		base, nw   []string
		want       [][3]string
		withOthers bool
	}{
		{"exact to not exact", nil, []string{schedNoSleep},
			[][3]string{{"schedule", "exact", "not exact (no_precise_sleep)"}}, false},
		{"not exact to exact", []string{schedProcs}, nil,
			[][3]string{{"schedule", "not exact (gomaxprocs)", "exact"}}, false},
		{"reasons in order", []string{schedStep}, []string{schedNoSleep, schedStep, schedProcs},
			[][3]string{{"schedule", "not exact (clock_step)", "not exact (no_precise_sleep, clock_step, gomaxprocs)"}}, false},
		{"the same reasons", []string{schedProcs}, []string{schedProcs}, nil, false},
		{"between the passport and the methods", nil, []string{schedStep},
			[][3]string{
				{"passport.os", "linux", "darwin"},
				{"schedule", "exact", "not exact (clock_step)"},
				{"methods", "pkg.Svc/Do", "pkg.Svc/Do, pkg.Svc/Other"},
			}, true},
	} {
		b, n := crBase(), crNew()
		b.notExact, n.notExact = tc.base, tc.nw

		if tc.withOthers {
			n.passport.OS = "darwin"
			n.methods = append(n.methods, crCall("pkg.Svc/Other", crExact(crMillis(10)), 0))
		}

		var out bytes.Buffer

		_, rep := CompareRunsReport(&out, b.file(t, "base.json"), n.file(t, "new.json"), nil, nil)

		var got [][3]string
		for _, d := range rep.Differences {
			got = append(got, schedDiff(d))
		}

		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: differences in the JSON %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Two runs whose schedules were not alike are not comparable, and the line
// names both, as it does for the passport's gomaxprocs: the exact one as
// "exact", the other as "not exact" with its reasons in the engine's order.
// The same reasons on both sides is no difference.
//
// Ground: contract — a wait that was up to a millisecond late shifts every
// latency; a comparison that hides it would lie.
func TestCompareRuns_TheScheduleDifferenceIsNamed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		base, nw   []string
		want       []string
		withOthers bool
	}{
		{"exact to not exact", nil, []string{schedNoSleep},
			[]string{"differs: schedule: exact -> not exact (no_precise_sleep)"}, false},
		{"not exact to exact", []string{schedProcs}, nil,
			[]string{"differs: schedule: not exact (gomaxprocs) -> exact"}, false},
		{"another reason", []string{schedNoSleep}, []string{schedProcs},
			[]string{"differs: schedule: not exact (no_precise_sleep) -> not exact (gomaxprocs)"}, false},
		{"one reason more", []string{schedNoSleep}, []string{schedNoSleep, schedStep},
			[]string{"differs: schedule: not exact (no_precise_sleep) -> not exact (no_precise_sleep, clock_step)"}, false},
		{"the same reasons", []string{schedNoSleep, schedProcs}, []string{schedNoSleep, schedProcs}, nil, false},
		{"both exact", nil, nil, nil, false},
		{"after the passport, before the methods", nil, []string{schedStep},
			[]string{
				"differs: passport.os: linux -> darwin",
				"differs: schedule: exact -> not exact (clock_step)",
				"differs: methods: pkg.Svc/Do -> pkg.Svc/Do, pkg.Svc/Other",
			}, true},
	} {
		for _, limits := range [][2]string{{"", ""}, {"20%", "1%"}} {
			base, nw := crBase(), crNew()
			base.notExact, nw.notExact = tc.base, tc.nw

			if tc.withOthers {
				nw.passport.OS = "darwin"
				nw.methods = append(nw.methods, crCall("pkg.Svc/Other", crExact(crMillis(10)), 0))
			}

			out, got := crCompare(t, base, nw, limits[0], limits[1])

			if diffs := crDiffs(out); !slices.Equal(diffs, tc.want) {
				t.Errorf("%s %v: difference lines %q, want %q\n%s", tc.name, limits, diffs, tc.want, out)
			}

			if alike := len(tc.want) == 0; got.Comparable != alike {
				t.Errorf("%s %v: comparable %v, want %v", tc.name, limits, got.Comparable, alike)
			}
		}
	}
}
