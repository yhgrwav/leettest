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

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// A report says when the generator did not wait to each call's moment
// exactly: the engine hands over the reasons (no_precise_sleep, clock_step,
// gomaxprocs), the text puts one line under the machine's, the JSON always has
// the field. The reasons are injected, so none of this depends on the host the
// test runs on.

// schedTail is what the line says after the reasons: the start lag is the
// run's own, p99 and max, the numbers the start lag note prints too.
const schedTail = ": starts may be late; that delay is inside the latencies. Start lag in this run: p99 1.30ms, max 2.10ms."

const (
	schedNoSleep = "no_precise_sleep"
	schedStep    = "clock_step"
	schedProcs   = "gomaxprocs"
)

func schedPassport(os string) *Passport {
	p := crPassport()
	p.OS = os

	return p
}

// schedRun is a run on os whose schedule was not exact for reasons, with a
// start lag of 1.3ms at p99 and 2.1ms at most.
func schedRun(os string, reasons ...string) RunReport {
	return RunReport{
		Report: engine.Report{
			Sent: 1000, Duration: 10 * time.Second, Planned: 10 * time.Second,
			StartLagP99: lagQuantile(1300 * time.Microsecond), StartLagMax: 2100 * time.Microsecond,
			ScheduleNotExact: reasons,
			Methods:          []engine.MethodReport{lateMethod(lateA, 5*time.Millisecond, 5*time.Millisecond)},
		},
		Passport: schedPassport(os),
	}
}

func schedSearch(os string, reasons ...string) BreakpointRun {
	steps := []breakpoint.Step{held(100), held(125), held(156)}
	for i := range steps {
		steps[i].Report.ScheduleNotExact = reasons
	}

	return BreakpointRun{
		Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, Passport: schedPassport(os),
		Result: breakpoint.Result{Outcome: breakpoint.HeldThroughout, Held: 156, Steps: steps},
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
			"schedule: not exact (clock step of 1 us or coarser)" + schedTail},
		{"one P", "linux", []string{schedProcs},
			"schedule: not exact (GOMAXPROCS below 2)" + schedTail},
		{"two reasons", "windows", []string{schedNoSleep, schedProcs},
			"schedule: not exact (no microsecond sleep on windows, GOMAXPROCS below 2)" + schedTail},
		{"three reasons, in the engine's order", "windows", []string{schedNoSleep, schedStep, schedProcs},
			"schedule: not exact (no microsecond sleep on windows, clock step of 1 us or coarser, GOMAXPROCS below 2)" + schedTail},
	} {
		out := printedRun(schedRun(tc.os, tc.reasons...))

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

	lines := scheduleLines(out)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "schedule: not exact (no microsecond sleep on windows)") {
		t.Fatalf("search with three inexact steps: schedule lines %q, want one naming the OS\n%s", lines, out)
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
