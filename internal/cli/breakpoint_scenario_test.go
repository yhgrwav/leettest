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
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// The report of a breaking-point search of a scenario (A3, U3): the rate is
// iterations per second, the table and the dataset lines read the head row,
// not the first row by name. The search itself is pkg/breakpoint's tests; the
// reports here are built by hand.

// The head sorts after both steps by name: a report that takes Methods[0] for
// the head reads GetBalance, a step.
const (
	sbpHead    = "/pay.v1.Wallet/Transfer"
	sbpBalance = "/pay.v1.Wallet/GetBalance"
	sbpHistory = "/pay.v1.Wallet/GetHistory"
)

const sbpHeader = "breaking point: scenario pay, 3 steps; rates are iterations per second"

const sbpWhy = "first to break: step 3 of 3, pay.v1.Wallet/GetHistory: failed 60 of 480 calls (12.5%)"

func sbpPlanOf() breakpoint.Plan {
	p := bpPlan
	p.StepTimeouts = []time.Duration{300 * time.Millisecond, 300 * time.Millisecond}

	return p
}

func sbpMethod(key string, step, sent, rps int, p99 time.Duration) engine.MethodReport {
	return engine.MethodReport{
		Method: key, Scenario: "pay", Step: step, GRPCMethod: key, Sent: sent, Latencies: sent,
		P99: scExact(p99), P99WithoutClientWaits: scExact(p99), RPSLow: rps, RPSHigh: rps,
	}
}

// sbpRun is a run of the scenario at rps iterations per second over the 4s
// window of bpPlan: the head sent rps×4 calls, GetBalance 20 fewer, GetHistory
// 40 fewer. Report.Sent is all three, Methods[0] is GetBalance. The head's p99
// is 31ms, GetBalance's 7ms, the iteration's 142ms: no two alike.
func sbpRun(kind breakpoint.Kind, rps int, broken bool, cause breakpoint.Cause, why string, at *breakpoint.Row) breakpoint.Step {
	iters := rps * 4
	head := sbpMethod(sbpHead, 1, iters, rps, 31*time.Millisecond)
	balance := sbpMethod(sbpBalance, 2, iters-20, 0, 7*time.Millisecond)
	history := sbpMethod(sbpHistory, 3, iters-40, 0, 9*time.Millisecond)

	return breakpoint.Step{
		RPS: rps, Kind: kind, Broken: broken, Cause: cause, Why: why, BrokeStep: at,
		Report: engine.Report{
			Sent:    head.Sent + balance.Sent + history.Sent,
			Methods: []engine.MethodReport{balance, history, head},
			Scenarios: []engine.ScenarioReport{{
				Name: "pay", Iterations: iters, P50: scExact(60 * time.Millisecond), P99: scExact(142 * time.Millisecond),
				Latencies: iters, ByCategory: map[engine.Category]int{engine.CategorySuccess: iters},
				Steps: []engine.ScenarioStepReport{
					{Method: sbpHead, Reached: iters},
					{Method: sbpBalance, Reached: iters},
					{Method: sbpHistory, Reached: iters},
				},
			}},
		},
	}
}

func sbpHeld(rps int) breakpoint.Step {
	return sbpRun(breakpoint.RateStep, rps, false, breakpoint.NoCause, "", nil)
}

var sbpRowHistory = &breakpoint.Row{Step: 3, Method: sbpHistory}

// sbpSearch is a scenario search that broke at 156 on its third step, with a
// probe between the step and its repeat; the repeat has the most iterations.
func sbpSearch() BreakpointRun {
	steps := []breakpoint.Step{
		sbpHeld(100), sbpHeld(125),
		sbpRun(breakpoint.RateStep, 156, true, breakpoint.CauseErrors, sbpWhy, sbpRowHistory),
		sbpRun(breakpoint.Probe, 100, false, breakpoint.NoCause, "", nil),
		sbpRun(breakpoint.Repeat, 156, true, breakpoint.CauseErrors, sbpWhy, sbpRowHistory),
	}
	steps[3].Recovered = true
	steps[4].Report.Scenarios[0].Iterations = 624

	return BreakpointRun{
		Target: "t:1", Method: "pay.v1.Wallet/Transfer", Scenario: "pay", Plan: sbpPlanOf(),
		Result: breakpoint.Result{
			Outcome: breakpoint.BrokeBetween, Held: 125, Broke: 156, Cause: breakpoint.CauseErrors,
			BrokeStep: sbpRowHistory, Steps: steps,
		},
	}
}

func sbpText(run BreakpointRun) string {
	var buf bytes.Buffer
	PrintBreakpoint(&buf, run)

	return buf.String()
}

func sbpJSON(t *testing.T, run BreakpointRun) map[string]any {
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

// Ground: contract (spec 4.1) — the first line names the scenario, the number
// of its steps (the head is the first) and the unit, word for word.
func TestScenarioSearch_TheHeaderNamesTheScenarioAndTheUnit(t *testing.T) {
	out := sbpText(sbpSearch())

	if first, _, _ := strings.Cut(out, "\n"); first != sbpHeader {
		t.Errorf("first line = %q, want %q", first, sbpHeader)
	}
}

// Ground: contract (spec 4.2) — the unit of the scenario's search is the
// iteration; no outcome says "rps" for it, the headline and the criterion
// included, whatever ends the search. The Why of a run is the search's own
// text, already in iterations/s; what the report adds must follow.
func TestScenarioSearch_NoRpsInTheTextOfAnyOutcome(t *testing.T) {
	asIterations := func(s string) string { return strings.ReplaceAll(s, " rps", " iterations/s") }

	for name, tc := range outcomes {
		res := tc.res
		res.Steps = slices.Clone(res.Steps)
		for i := range res.Steps {
			res.Steps[i].Why = asIterations(res.Steps[i].Why)
		}
		res.Notes = nil

		run := BreakpointRun{Target: "t:1", Method: "pkg.Svc/Do", Scenario: "pay", Plan: sbpPlanOf(), Result: res}
		if out := sbpText(run); strings.Contains(out, " rps") {
			t.Errorf("%s: \" rps\" in the report of a scenario search:\n%s", name, out)
		}
	}

	// The plain search is not touched by it.
	if out := printedSearch(outcomes["broke"].res); !strings.Contains(out, "held 244 rps, broke at 305 rps") {
		t.Errorf("a plain search lost its rps:\n%s", out)
	}
}

// Ground: contract (spec 4.1, 4.2) — the headline and the criterion line name
// the rates in iterations/s.
func TestScenarioSearch_TheHeadlineIsInIterations(t *testing.T) {
	out := sbpText(sbpSearch())

	for _, want := range []string{
		"  held 125 iterations/s, broke at 156 iterations/s\n",
		"  156 iterations/s: " + sbpWhy + "\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}

// Ground: contract (spec 4.2) — what ends a search by a dataset or a {{seq}}
// says the rate in iterations/s too.
func TestScenarioSearch_EndedByAFileOrASeqSaysIterations(t *testing.T) {
	file := exhaustedSearch(StopDatasetExhausted, true)
	file.Scenario, file.Plan = "pay", sbpPlanOf()

	overflow := exhaustedSearch(StopSeqOverflow, false)
	overflow.Scenario, overflow.Plan = "pay", sbpPlanOf()
	last := &overflow.Result.Steps[len(overflow.Result.Steps)-1]
	last.Report.SeqOverflow = &engine.SeqOverflow{Method: sbpHead, Limit: 999}

	for name, tc := range map[string]struct {
		run  BreakpointRun
		want string
	}{
		"dataset": {file, "ran out at 305 iterations/s"},
		"seq":     {overflow, "outgrew its field (limit 999) at 305 iterations/s"},
	} {
		out := sbpText(tc.run)
		if !strings.Contains(out, tc.want) {
			t.Errorf("%s: no %q in\n%s", name, tc.want, out)
		}
		if strings.Contains(out, " rps") {
			t.Errorf("%s: \" rps\" in\n%s", name, out)
		}
	}
}

// Ground: contract (spec 4.2) — the ramp block of the search's first step is
// about the head's iterations: its planned column says so.
func TestScenarioSearch_TheRampBlockSaysIterations(t *testing.T) {
	run := sbpSearch()
	run.Ramp = 3 * time.Second

	first := RunReport{Report: run.Result.Steps[0].Report, Settings: &RunSettings{Ramp: 3 * time.Second, Warmup: 3 * time.Second}}
	first.SendingEnded = 3 * time.Second
	first.Warmup = 3 * time.Second
	secs := []engine.Second{blockSecond(33, 33, 0), blockSecond(66, 66, 0), blockSecond(100, 100, 0)}
	first.Methods = slices.Clone(first.Methods)
	for i := range first.Methods {
		if first.Methods[i].Method == sbpHead {
			first.Methods[i].Seconds = secs
		}
	}
	run.Runs = []RunReport{first}
	run.Runs = append(run.Runs, make([]RunReport, len(run.Result.Steps)-1)...)
	for i := 1; i < len(run.Runs); i++ {
		run.Runs[i] = RunReport{Report: run.Result.Steps[i].Report}
	}

	out := sbpText(run)
	if !strings.Contains(out, "planned iterations/s") {
		t.Errorf("the ramp block does not say iterations:\n%s", out)
	}
	if strings.Contains(out, " rps") {
		t.Errorf("\" rps\" in a scenario search with a ramp:\n%s", out)
	}
}

// Ground: contract (spec 4.3, 1.1) — sent is the head row's calls over the
// window, not Report.Sent (all rows) and not Methods[0] (the first by name,
// here a step); p99 is the iteration's, under its own heading. Mutation
// "Methods[0] instead of the head" dies here.
func TestScenarioSearch_SentIsTheHeadsAndP99IsTheIterations(t *testing.T) {
	out := sbpText(sbpSearch())

	if !regexp.MustCompile(`(?m)^\s+kind\s+planned\s+sent\s+failed\s+iteration p99\s+verdict`).MatchString(out) {
		t.Errorf("no heading with \"iteration p99\" in\n%s", out)
	}
	// 100 iterations/s planned, 400 head calls in 4s: sent 100 (GetBalance's
	// 380 would be 95, all rows 285); p99 142ms (the head's is 31ms).
	if !regexp.MustCompile(`(?m)^\s+step\s+100\s+100\s+0\s+142ms\s+held$`).MatchString(out) {
		t.Errorf("the row of the first step is not planned 100, sent 100, p99 142ms, held:\n%s", out)
	}
	for _, not := range []string{" 95 ", " 285 ", " 31ms", " 7ms"} {
		if strings.Contains(out, not) {
			t.Errorf("%q in the table: it reads a row that is not the head, or Report.Sent:\n%s", not, out)
		}
	}
}

// Ground: contract (spec 4.4) — what the dataset and the templates did is
// said for every row of the scenario in the order of its steps, each line
// naming the step, not for the first row by name only. Mutation "Methods[0]
// instead of the head" dies here.
func TestScenarioSearch_DataLinesAreOnePerRowInStepOrder(t *testing.T) {
	run := sbpSearch()
	last := &run.Result.Steps[len(run.Result.Steps)-1].Report
	for i := range last.Methods {
		switch last.Methods[i].Method {
		case sbpHead:
			last.Methods[i].Dataset = &engine.DatasetReport{File: "data/users.jsonl", Records: 20, Used: 20, UsedMax: 5}
		case sbpBalance:
			last.Methods[i].Dataset = &engine.DatasetReport{File: "data/accounts.jsonl", Records: 50, Used: 50, UsedMax: 2}
		case sbpHistory:
			last.Methods[i].Templates = &engine.TemplatesReport{Placeholders: []string{"{{seq}}"}, LastSeq: 900}
		}
	}

	out := sbpText(run)
	want := []string{
		"  step 1, pay.v1.Wallet/Transfer: data: 20 of 20 requests from users.jsonl, each used up to 5 times",
		"  step 2, pay.v1.Wallet/GetBalance: data: 50 of 50 requests from accounts.jsonl, each used up to 2 times",
		"  step 3, pay.v1.Wallet/GetHistory: data: templated, {{seq}} (last seq 900, ramp and warm-up counted)",
	}
	at := -1
	for _, line := range want {
		i := strings.Index(out, line+"\n")
		if i < 0 {
			t.Fatalf("no line %q in\n%s", line, out)
		}
		if i < at {
			t.Errorf("line %q is out of the order of the steps in\n%s", line, out)
		}
		at = i
	}
	if strings.Contains(out, "\n  data: ") {
		t.Errorf("a data line with no row named in\n%s", out)
	}
}

// Ground: contract (spec 4.5) — the scenario block is printed once, for the
// run that confirmed the break (the repeat, not the probe that follows the
// step), after the table of runs: the numbers of every step are in the table.
func TestScenarioSearch_TheScenarioBlockIsPrintedOnceForTheConfirmingRun(t *testing.T) {
	out := sbpText(sbpSearch())

	if n := strings.Count(out, "\nscenario pay\n"); n != 1 {
		t.Fatalf("%d scenario blocks, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "  624 iterations, started at 156/s") {
		t.Errorf("the block is not the repeat's (624 iterations, started at 156/s):\n%s", out)
	}
	if strings.Index(out, "\nscenario pay\n") < strings.Index(out, "iteration p99") {
		t.Errorf("the block is before the table of runs:\n%s", out)
	}
}

// The probe is never the run the block is about, also when the search was
// stopped on it.
func TestScenarioSearch_TheBlockOfAStoppedSearchSkipsTheProbe(t *testing.T) {
	steps := []breakpoint.Step{sbpHeld(100), sbpHeld(125), sbpRun(breakpoint.Probe, 100, false, breakpoint.NoCause, "", nil)}
	steps[1].Report.Scenarios[0].Iterations = 500
	steps[2].Report.Scenarios[0].Iterations = 7

	out := sbpText(BreakpointRun{
		Target: "t:1", Method: "pay.v1.Wallet/Transfer", Scenario: "pay", Plan: sbpPlanOf(),
		Result: breakpoint.Result{Outcome: breakpoint.Stopped, Held: 125, Steps: steps},
	})

	if !strings.Contains(out, "  500 iterations, started at 125/s") || strings.Contains(out, "  7 iterations") {
		t.Errorf("the block is not the last step's, or it is the probe's:\n%s", out)
	}
}

// A plain search has no scenario block and no scenario in its header.
func TestScenarioSearch_APlainSearchPrintsNoScenario(t *testing.T) {
	out := printedSearch(outcomes["broke"].res)

	if strings.Contains(out, "scenario") || strings.Contains(out, "iteration") {
		t.Errorf("a plain search mentions a scenario:\n%s", out)
	}
	if first, _, _ := strings.Cut(out, "\n"); first != "breaking point: pkg.Svc/Do" {
		t.Errorf("first line = %q", first)
	}
}

// Ground: contract (spec 2.2, 4.6) — JSON adds, never renames: the unit, the
// scenario's name, and the first row to break, in the search and in the run
// that found it; held_rps and the other names stay. sent_rps is the head's.
func TestScenarioSearch_JSONNamesTheUnitTheScenarioAndTheRow(t *testing.T) {
	out := sbpJSON(t, sbpSearch())

	for path, want := range map[string]any{
		"schema_version":        1.0,
		"method":                "pay.v1.Wallet/Transfer",
		"breakpoint.rate_unit":  "iterations",
		"breakpoint.scenario":   "pay",
		"breakpoint.held_rps":   125.0,
		"breakpoint.broke_rps":  156.0,
		"breakpoint.broke_step": map[string]any{"step": 3.0, "method": sbpHistory},
		"breakpoint.why":        "errors",
	} {
		if got := field(t, out, strings.Split(path, ".")...); !jsonEqual(got, want) {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}

	runs, _ := field(t, out, "breakpoint", "runs").([]any)
	if len(runs) != 5 {
		t.Fatalf("%d runs, want 5", len(runs))
	}
	for i, wantStep := range []any{nil, nil, map[string]any{"step": 3.0, "method": sbpHistory}, nil, map[string]any{"step": 3.0, "method": sbpHistory}} {
		run := runs[i].(map[string]any)
		if !jsonEqual(run["broke_step"], wantStep) {
			t.Errorf("runs[%d].broke_step = %v, want %v", i, run["broke_step"], wantStep)
		}
		if run["sent_rps"] != float64(sbpRunsPlanned[i]) {
			t.Errorf("runs[%d].sent_rps = %v, want the head's %d", i, run["sent_rps"], sbpRunsPlanned[i])
		}
	}
}

// planned rate of each run of sbpSearch: sent by the head equals it.
var sbpRunsPlanned = []int{100, 125, 156, 100, 156}

// Ground: contract — a plain search says its unit is calls, has no scenario
// and no broken row; the keys are always there (schema_breakpoint_v1.txt).
func TestScenarioSearch_APlainSearchSaysCallsAndNoRow(t *testing.T) {
	out := searchJSON(t, outcomes["broke"].res)

	if got := field(t, out, "breakpoint", "rate_unit"); got != "calls" {
		t.Errorf("breakpoint.rate_unit = %v, want calls", got)
	}
	for _, path := range [][]string{{"breakpoint", "scenario"}, {"breakpoint", "broke_step"}} {
		if got := field(t, out, path...); got != nil {
			t.Errorf("%v = %v, want null", path, got)
		}
	}
	runs := field(t, out, "breakpoint", "runs").([]any)
	for i, r := range runs {
		m := r.(map[string]any)
		if v, ok := m["broke_step"]; !ok || v != nil {
			t.Errorf("runs[%d].broke_step = %v (present %v), want null", i, v, ok)
		}
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)

	return bytes.Equal(x, y)
}

// The live screen is the same search: its header, its table and its cooldown
// speak of iterations too.
func TestScenarioSearch_TheScreenSaysIterations(t *testing.T) {
	m := testSearchModelOf(t, sbpPlanOf())
	send(m, searchRunMsg{eng: stepEngine(t, 156), run: breakpoint.Run{RPS: 156, Kind: breakpoint.RateStep, Step: 3, Steps: 6}})

	if got := m.place(); !strings.Contains(got, "156 iterations/s") || strings.Contains(got, " rps") {
		t.Errorf("place = %q", got)
	}

	m.rows = []breakpoint.Step{sbpHeld(100)}
	table := m.table(-1)
	if !strings.Contains(table, "iteration p99") || !strings.Contains(table, "142ms") {
		t.Errorf("the table is not the iteration's:\n%s", table)
	}

	m.coolUntil, m.coolRPS = m.now().Add(10*time.Second), 305
	if screen := m.View(); !strings.Contains(screen, "before the repeat of 305 iterations/s") {
		t.Errorf("the cooldown line does not say iterations:\n%s", screen)
	}

	plain := testSearchModel(t)
	plain.coolUntil, plain.coolRPS = plain.now().Add(10*time.Second), 305
	if screen := plain.View(); !strings.Contains(screen, "before the repeat of 305 rps") {
		t.Errorf("a plain search's cooldown lost its rps:\n%s", screen)
	}
}

// sbpFile is a scenario search as the file compare reads it, with the sha of
// each row's dataset (empty: no dataset on the row).
func sbpFile(t *testing.T, head, balance, history string) SearchFile {
	t.Helper()

	run := sbpSearch()
	last := &run.Result.Steps[0].Report
	for i := range last.Methods {
		var sha string
		switch last.Methods[i].Method {
		case sbpHead:
			sha = head
		case sbpBalance:
			sha = balance
		case sbpHistory:
			sha = history
		}
		if sha != "" {
			last.Methods[i].Dataset = &engine.DatasetReport{File: "d.jsonl", SHA256: sha, Records: 1, Used: 1, UsedMax: 1}
		}
	}

	return SearchFile{Path: "s.json", Report: NewBreakpointReport(run)}
}

func hasDatasetDiff(diffs []CompareDiff) bool {
	return slices.ContainsFunc(diffs, func(d CompareDiff) bool { return d.Field == "dataset" })
}

// Ground: contract (spec 4.7) — a changed dataset of a step is a difference:
// the head's file is the same, and "the data is the same" would be false.
// Mutation "datasetHash only by r.Method" dies here.
func TestScenarioSearch_CompareSeesAChangedDatasetOfAStep(t *testing.T) {
	base := sbpFile(t, "aa", "bb", "")

	for name, tc := range map[string]struct {
		nw   SearchFile
		diff bool
	}{
		"the same files":           {sbpFile(t, "aa", "bb", ""), false},
		"a step's file changed":    {sbpFile(t, "aa", "cc", ""), true},
		"the last step got a file": {sbpFile(t, "aa", "bb", "dd"), true},
		"the head's file changed":  {sbpFile(t, "ee", "bb", ""), true},
		"a step's file went away":  {sbpFile(t, "aa", "", ""), true},
	} {
		if got := hasDatasetDiff(differences(base, tc.nw)); got != tc.diff {
			t.Errorf("%s: a dataset difference = %v, want %v", name, got, tc.diff)
		}
	}

	if got := hasDatasetDiff(differences(sbpFile(t, "", "", ""), sbpFile(t, "", "", ""))); got {
		t.Errorf("two scenarios with no data differ in a dataset")
	}
}

// Ground: contract (spec 4.7) — a plain search's dataset is the sha of the
// searched method's file, as before: the value does not move.
func TestScenarioSearch_ThePlainDatasetHashIsTheFilesSha(t *testing.T) {
	run := BreakpointRun{Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, Result: outcomes["held_all"].res}
	steps := slices.Clone(run.Result.Steps)
	other := engine.MethodReport{Method: "/pkg.Svc/Other", Dataset: &engine.DatasetReport{SHA256: "other"}}
	steps[0].Report.Methods = []engine.MethodReport{
		other, {Method: "/pkg.Svc/Do", Dataset: &engine.DatasetReport{File: "d.jsonl", SHA256: "abc123"}},
	}
	run.Result.Steps = steps

	if got := datasetHash(NewBreakpointReport(run)); got != "abc123" {
		t.Errorf("datasetHash = %q, want the method's own sha abc123", got)
	}

	steps[0].Report.Methods = []engine.MethodReport{other}
	run.Result.Steps = steps
	if got := datasetHash(NewBreakpointReport(run)); got != "none" {
		t.Errorf("datasetHash with no dataset on the method = %q, want none", got)
	}
}
