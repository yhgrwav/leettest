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
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// bpPlan measures 4s of each step: planned calls = rps × 4.
var bpPlan = breakpoint.Plan{From: 100, To: 400, Settle: time.Second, Hold: 5 * time.Second, Timeout: 500 * time.Millisecond}

// bpRun is a run at rps of kind that sent sent calls with p99, judged as
// broken/cause/why.
func bpRun(kind breakpoint.Kind, rps, sent int, p99 time.Duration, broken bool, cause breakpoint.Cause, why string) breakpoint.Step {
	q := metrics.Quantile{Value: p99, Exact: true, Defined: true}

	return breakpoint.Step{
		RPS: rps, Kind: kind, Broken: broken, Cause: cause, Why: why,
		Report: engine.Report{Sent: sent, Methods: []engine.MethodReport{{
			Method: "pkg.Svc/Do", Sent: sent, Latencies: sent, P99: q, P99WithoutClientWaits: q,
		}}},
	}
}

func recovered(s breakpoint.Step) breakpoint.Step {
	s.Recovered = true

	return s
}

func held(rps int) breakpoint.Step {
	return bpRun(breakpoint.RateStep, rps, rps*4, 21*time.Millisecond, false, breakpoint.NoCause, "")
}

const kneeWhy = "p99 341ms = 16.2x baseline 21ms (no p99_limit set)"

// outcomes is one result per outcome, with what the headline of the text
// report must say and must not ("|" separates the claims).
var outcomes = map[string]struct {
	res          breakpoint.Result
	says, denies string
}{
	"broke": {breakpoint.Result{
		Outcome: breakpoint.BrokeBetween, Held: 244, Broke: 305, Cause: breakpoint.CauseP99VsBase,
		Steps: []breakpoint.Step{
			held(100), held(125), held(156), held(195), held(244),
			bpRun(breakpoint.RateStep, 305, 1220, 341*time.Millisecond, true, breakpoint.CauseP99VsBase, kneeWhy),
			recovered(bpRun(breakpoint.Probe, 100, 400, 22*time.Millisecond, false, breakpoint.NoCause, "")), // 5s probe, 4s measured
			bpRun(breakpoint.Repeat, 305, 1220, 338*time.Millisecond, true, breakpoint.CauseP99VsBase, kneeWhy),
		},
	}, "held 244|broke at 305", "stopped|invalid|gave out"},
	"broke_at_first": {breakpoint.Result{
		Outcome: breakpoint.BrokeAtFirst, Broke: 100, Cause: breakpoint.CauseErrors,
		Steps: []breakpoint.Step{
			bpRun(breakpoint.RateStep, 100, 400, 30*time.Millisecond, true, breakpoint.CauseErrors, "failed 200 of 400 calls (50.0%)"),
			bpRun(breakpoint.Repeat, 100, 400, 30*time.Millisecond, true, breakpoint.CauseErrors, "failed 200 of 400 calls (50.0%)"),
		},
		Notes: []string{"no lower step to check recovery against; start lower (from) for a reliable result"},
	}, "broke at|first|100|at or below", "held|stopped|gave out"},
	"held_all": {breakpoint.Result{
		Outcome: breakpoint.HeldThroughout, Held: 381,
		Steps: []breakpoint.Step{held(100), held(125), held(156), held(195), held(244), held(305), held(381)},
	}, "held|every step|381|above", "broke|stopped|gave out|invalid"},
	"run_limit": {breakpoint.Result{
		Outcome: breakpoint.RunLimit, Held: 100, Broke: 125, Cause: breakpoint.CauseGenerator,
		Steps: []breakpoint.Step{held(100),
			bpRun(breakpoint.RateStep, 125, 450, 21*time.Millisecond, false, breakpoint.CauseGenerator,
				"the generator sent 112 of 125 rps; the target above that is untested")},
	}, "gave out at 125|not the target|generator sent 112 of 125|untested|held 100", "broke at"},
	"run_limit_first": {breakpoint.Result{
		Outcome: breakpoint.RunLimit, Broke: 100, Cause: breakpoint.CauseGenerator,
		Steps: []breakpoint.Step{bpRun(breakpoint.RateStep, 100, 300, 21*time.Millisecond, false, breakpoint.CauseGenerator,
			"the generator sent 75 of 100 rps; the target above that is untested")},
	}, "gave out|first step|100|nothing was learned", "broke at|held"},
	"stopped": {breakpoint.Result{
		Outcome: breakpoint.Stopped, Held: 244,
		Steps: []breakpoint.Step{held(100), held(125), held(156), held(195), held(244),
			bpRun(breakpoint.RateStep, 305, 1220, 341*time.Millisecond, true, breakpoint.CauseP99VsBase, kneeWhy),
			bpRun(breakpoint.Probe, 100, 200, 22*time.Millisecond, false, breakpoint.NoCause, "")},
	}, "stopped at 100|probe|held 244", "broke at|gave out|invalid"},
	"stopped_first": {breakpoint.Result{
		Outcome: breakpoint.Stopped,
		Steps:   []breakpoint.Step{bpRun(breakpoint.RateStep, 100, 200, 21*time.Millisecond, false, breakpoint.NoCause, "")},
	}, "stopped|first step|nothing was learned", "broke at|held|gave out|invalid"},
	"invalid": {breakpoint.Result{
		Outcome: breakpoint.Invalid, Held: 244, Cause: breakpoint.CauseClockStep,
		Steps: []breakpoint.Step{held(100), held(125), held(156), held(195), held(244),
			bpRun(breakpoint.RateStep, 305, 1220, 341*time.Millisecond, false, breakpoint.CauseClockStep, "invalid run: clock step")},
	}, "invalid|clock step|305|no breaking point", "held|broke at"},
}

func printedSearch(res breakpoint.Result) string {
	var buf bytes.Buffer
	PrintBreakpoint(&buf, BreakpointRun{Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, Result: res})

	return buf.String()
}

func searchJSON(t *testing.T, res breakpoint.Result) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteBreakpointJSON(&buf, BreakpointRun{Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, Result: res}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}

	return out
}

// Each outcome opens with a headline under the method's name that makes its
// claims, with the rates they are about, and none of another outcome's.
func TestBreakpoint_EachOutcomeHasItsHeadline(t *testing.T) {
	for name, tc := range outcomes {
		out := printedSearch(tc.res)
		_, rest, ok := strings.Cut(out, "breaking point: pkg.Svc/Do\n  ")
		if !ok {
			t.Errorf("%s: no headline under the method, got\n%s", name, out)

			continue
		}
		head, _, _ := strings.Cut(rest, "\n")
		for _, claim := range strings.Split(tc.says, "|") {
			if !strings.Contains(head, claim) {
				t.Errorf("%s: the headline %q does not say %q", name, head, claim)
			}
		}
		for _, claim := range strings.Split(tc.denies, "|") {
			if strings.Contains(head, claim) {
				t.Errorf("%s: the headline %q says %q, another outcome's claim", name, head, claim)
			}
		}
	}
}

// Every run is a row with its kind, the planned rps and the rps the generator
// sent (calls over the measured window, rounded down: a short run never
// prints as full). Probes are rows of kind probe.
func TestBreakpoint_EveryRunIsARowWithPlannedAndSent(t *testing.T) {
	out := printedSearch(outcomes["broke"].res)
	for _, row := range []string{
		`(?m)^\s+kind\s+planned\s+sent\s`,
		`(?m)^\s+step\s+305\s+305\s.*broke: ` + regexp.QuoteMeta(kneeWhy),
		`(?m)^\s+probe\s+100\s+100\s`,
		`(?m)^\s+repeat\s+305\s+305\s`,
	} {
		if !regexp.MustCompile(row).MatchString(out) {
			t.Errorf("no row %q in\n%s", row, out)
		}
	}
	if n := strings.Count(out, "\n  step "); n != 6 {
		t.Errorf("%d step rows, want 6: the probe and the repeat are not steps", n)
	}

	out = printedSearch(outcomes["run_limit"].res)
	if !regexp.MustCompile(`(?m)^\s+step\s+125\s+112\s`).MatchString(out) {
		t.Errorf("the short step must show planned 125, sent 112 (450 calls over 4s):\n%s", out)
	}
}

// A probe's row says what the search decided of it: one that did not break
// but stayed over 1.5× the baseline did not recover, and a search that ended
// so says it before the table.
func TestBreakpoint_AProbeRowSaysWhetherItRecovered(t *testing.T) {
	slow := bpRun(breakpoint.Probe, 100, 400, 45*time.Millisecond, false, breakpoint.NoCause, "")
	ok := slow
	ok.Recovered = true
	steps := []breakpoint.Step{held(100), held(125), held(156), held(195), held(244),
		bpRun(breakpoint.RateStep, 305, 1220, 341*time.Millisecond, true, breakpoint.CauseP99VsBase, kneeWhy)}
	for range breakpoint.MaxProbes {
		steps = append(steps, slow)
	}
	out := printedSearch(breakpoint.Result{
		Outcome: breakpoint.BrokeBetween, Held: 244, Broke: 305, Cause: breakpoint.CauseNoRecovery, Steps: steps,
		Notes: []string{"305: broke and did not recover within 5 probes of 1s at 100 rps"},
	})
	if n := len(regexp.MustCompile(`(?m)^\s+probe\s.*not recovered`).FindAllString(out, -1)); n != breakpoint.MaxProbes {
		t.Errorf("%d probe rows say not recovered, want %d:\n%s", n, breakpoint.MaxProbes, out)
	}
	if regexp.MustCompile(`(?m)^\s+probe\s.*\sheld$`).MatchString(out) {
		t.Errorf("a probe that did not recover prints as held:\n%s", out)
	}
	head, _, _ := strings.Cut(out, "kind")
	if !strings.Contains(head, "did not recover") {
		t.Errorf("the lines before the table do not say the target did not recover:\n%s", head)
	}

	out = printedSearch(breakpoint.Result{Outcome: breakpoint.HeldThroughout, Held: 305, Steps: append(steps[:6:6], ok)})
	if !regexp.MustCompile(`(?m)^\s+probe\s.*\srecovered$`).MatchString(out) {
		t.Errorf("a recovered probe's row must say recovered:\n%s", out)
	}
}

// The plan line says before the first step how long the search can take.
func TestBreakpoint_ThePlanLine(t *testing.T) {
	p := breakpoint.Plan{From: 100, To: 156, Settle: 100 * time.Millisecond, Hold: time.Second, Timeout: 500 * time.Millisecond}
	// Plan.Worst 67.1s + connect 5s, rounded up.
	if got, want := PlanLine(p, 5*time.Second), "breakpoint: up to 3 steps, at most 1m13s"; got != want {
		t.Errorf("plan line %q, want %q", got, want)
	}
}

// Ground: contract — "at most" is a promise of an upper bound. The ramp before
// the first step is run time the plan has to count: once (a repeat of the first
// step starts at its rate, no second ramp), before the rounding up to a second
// (the same rounding as without the ramp, the second not added when the sum is
// whole), and a ramp of 0 is the release's line byte for byte.
func TestBreakpoint_ThePlanLineCountsTheRamp(t *testing.T) {
	p := breakpoint.Plan{From: 100, To: 156, Settle: 100 * time.Millisecond, Hold: time.Second, Timeout: 500 * time.Millisecond}
	// Plan.Worst 67.1s + connect 5s = 72.1s, plus the ramp, rounded up.
	for _, tc := range []struct {
		ramp time.Duration
		want string
	}{
		{0, PlanLine(p, 5*time.Second)},
		{10 * time.Second, "breakpoint: up to 3 steps, at most 1m23s"},
		{1500 * time.Millisecond, "breakpoint: up to 3 steps, at most 1m14s"},
		{2900 * time.Millisecond, "breakpoint: up to 3 steps, at most 1m15s"},
	} {
		if got := PlanLineRamp(p, 5*time.Second, tc.ramp); got != tc.want {
			t.Errorf("ramp %v: plan line %q, want %q", tc.ramp, got, tc.want)
		}
	}
	if got, want := PlanLine(p, 5*time.Second), "breakpoint: up to 3 steps, at most 1m13s"; got != want {
		t.Errorf("plan line without a ramp %q, want the release's %q", got, want)
	}
}

// The search's JSON: mode breakpoint, outcome and why from closed lists,
// rates null where the outcome has none, every run with its kind, planned and
// sent rps and its full report.
func TestBreakpoint_JSONIsAContract(t *testing.T) {
	want := map[string]struct {
		outcome, why string
		held, broke  any
	}{
		"broke":           {"broke", "p99_vs_base", 244.0, 305.0},
		"broke_at_first":  {"broke_at_first", "errors", nil, 100.0},
		"held_all":        {"held_all", "", 381.0, nil},
		"run_limit":       {"run_limit", "generator", 100.0, 125.0},
		"run_limit_first": {"run_limit", "generator", nil, 100.0},
		"stopped":         {"stopped", "", 244.0, nil},
		"stopped_first":   {"stopped", "", nil, nil},
		"invalid":         {"invalid", "clock_step", nil, nil},
	}
	for name, tc := range outcomes {
		out := searchJSON(t, tc.res)
		if out["mode"] != "breakpoint" || out["schema_version"] != 1.0 {
			t.Errorf("%s: mode %v schema %v, want breakpoint 1", name, out["mode"], out["schema_version"])
		}
		bp, _ := out["breakpoint"].(map[string]any)
		if bp == nil {
			t.Errorf("%s: no breakpoint object", name)

			continue
		}
		w := want[name]
		var why any
		if w.why != "" {
			why = w.why
		}
		for key, v := range map[string]any{"outcome": w.outcome, "why": why, "held_rps": w.held, "broke_rps": w.broke} {
			got, present := bp[key]
			if !present || got != v {
				t.Errorf("%s: %s = %v (present %v), want %v", name, key, got, present, v)
			}
		}
		runs, _ := bp["runs"].([]any)
		if len(runs) != len(tc.res.Steps) {
			t.Errorf("%s: %d runs, want %d", name, len(runs), len(tc.res.Steps))

			continue
		}
		for i, r := range runs {
			run, _ := r.(map[string]any)
			step := tc.res.Steps[i]
			if run["kind"] != step.Kind.String() || run["planned_rps"] != float64(step.RPS) {
				t.Errorf("%s run %d: kind %v planned %v, want %v %d", name, i, run["kind"], run["planned_rps"], step.Kind, step.RPS)
			}
			if why, ok := run["why"]; !ok || (why != nil && !slices.Contains(whyNames, why)) {
				t.Errorf("%s run %d: why %v (present %v), want null or one of %v", name, i, why, ok, whyNames)
			}
			if rec, ok := run["recovered"]; !ok || (step.Kind == breakpoint.Probe) != (rec != nil) ||
				(rec != nil && rec != step.Recovered) {
				t.Errorf("%s run %d (%v): recovered %v, want the probe's verdict, null for others", name, i, step.Kind, rec)
			}
			if _, ok := run["sent_rps"].(float64); !ok {
				t.Errorf("%s run %d: sent_rps %v, want a number", name, i, run["sent_rps"])
			}
			if rep, _ := run["report"].(map[string]any); rep == nil || rep["methods"] == nil {
				t.Errorf("%s run %d: no full report", name, i)
			}
		}
	}

	bp, _ := searchJSON(t, outcomes["run_limit"].res)["breakpoint"].(map[string]any)
	runs, _ := bp["runs"].([]any)
	if len(runs) != 2 {
		t.Errorf("run_limit: %d runs, want 2", len(runs))
	} else {
		if run, _ := runs[1].(map[string]any); run["sent_rps"] != 112.0 || run["why"] != "generator" {
			t.Errorf("short run: sent %v why %v, want 112 generator", run["sent_rps"], run["why"])
		}
	}
}

// whyNames is the closed list of why, in the spec and the README.
var whyNames = []any{"errors", "p99_limit", "p99_vs_base", "connection", "no_recovery",
	"generator", "in_flight_cap", "stream_limit", "stream_wait", "client_errors", "clock_step", "request_errors"}

// Every cause has its JSON name from the closed list, and the list has no
// name no cause maps to.
func TestBreakpoint_EveryCauseHasAListedName(t *testing.T) {
	var got []any
	for c := breakpoint.CauseErrors; c <= breakpoint.CauseRequestErrors; c++ {
		res := breakpoint.Result{Outcome: breakpoint.RunLimit, Broke: 100, Cause: c,
			Steps: []breakpoint.Step{bpRun(breakpoint.RateStep, 100, 400, 21*time.Millisecond, false, c, "x")}}
		bp, _ := searchJSON(t, res)["breakpoint"].(map[string]any)
		got = append(got, bp["why"])
	}
	if !slices.Equal(got, whyNames) {
		t.Errorf("why of each cause %v, want %v", got, whyNames)
	}
}

// In breakpoint mode the plain run's fields are absent, not taken from some
// step: an old consumer cannot read one step's numbers as the result.
func TestBreakpoint_JSONHasNoPlainRunFields(t *testing.T) {
	out := searchJSON(t, outcomes["broke"].res)
	if out["mode"] != "breakpoint" || out["breakpoint"] == nil {
		t.Fatalf("not a breakpoint report: %v", out)
	}
	for _, key := range []string{"outcome", "sent", "failed", "methods", "tail_wait_cause", "client_waits",
		"start_lag", "cap_hit", "connections", "invalid_reasons", "duration_us", "warmup_us"} {
		if _, ok := out[key]; ok {
			t.Errorf("top-level %q present in breakpoint mode", key)
		}
	}
}

// searchOfRuns is a search of three held steps (100, 125 and 156 rps) whose
// runs are the given reports, one per step, as the CLI aligns them.
func searchOfRuns(runs ...RunReport) BreakpointRun {
	res := breakpoint.Result{
		Outcome: breakpoint.HeldThroughout, Held: 156,
		Steps: []breakpoint.Step{held(100), held(125), held(156)},
	}

	return BreakpointRun{Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, Result: res, Runs: runs}
}

// Ground: contract — a search whose first step ramps (load.ramp) prints the
// ramp block a plain run prints, so that a failure on the way to `from` is in
// the text and not only in the JSON. The block is the plain run's byte for
// byte (printRampBlocks of the first run's report, no new wording), it comes
// directly after the runs table and before the notes, and once: the steps
// after the first carry no ramp and print nothing. The first run ramps 4s with
// a failing second; the rates and counts are not round on purpose.
// Mutations "no call", "only the last run" and "before the table" turn it red.
func TestBreakpoint_TheFirstStepShowsItsRampBySecond(t *testing.T) {
	first := blockRun(4*time.Second, time.Second, secondsOf(5, func(i int) engine.Second {
		return blockSecond(float64(20+i*20), 17+i*21, i*i)
	})...)
	run := searchOfRuns(first, blockRun(0, time.Second), blockRun(0, time.Second))
	run.Ramp = 4 * time.Second
	run.Result.Notes = []string{"a note of the search"}

	var want bytes.Buffer
	printRampBlocks(&want, first)
	if !strings.Contains(want.String(), "by second") {
		t.Fatalf("the fixture's ramp block is empty, the test proves nothing:\n%s", want.String())
	}

	var out bytes.Buffer
	PrintBreakpoint(&out, run)
	got := out.String()

	if n := strings.Count(got, "by second"); n != 1 {
		t.Fatalf("%d ramp blocks in the search's text, want 1:\n%s", n, got)
	}

	verdicts := make([]string, len(run.Result.Steps))
	for i := range run.Result.Steps {
		verdicts[i] = verdict(&run.Result, i)
	}

	table := runsTable(run.Plan, run.Result.Steps, verdicts, "  ")
	at := strings.Index(got, table)
	if at < 0 {
		t.Fatalf("no runs table in the search's text:\n%s", got)
	}

	after := got[at+len(table):]
	if !strings.HasPrefix(after, want.String()) {
		t.Errorf("the text after the runs table does not begin with the plain run's ramp block\nwant prefix:\n%s\ngot:\n%s", want.String(), after)
	}

	rest := strings.TrimPrefix(after, want.String())
	if !strings.HasPrefix(rest, "\nnotes:\n") {
		t.Errorf("the notes do not follow the block directly:\n%s", rest)
	}
}

// Ground: boundary — a search with no ramp (load.ramp absent or 0, so no run
// has a ramp in its settings) has no ramp block: the text is the release's.
// Green from the start; it guards the rule that only a run with a ramp prints
// the block. Mutation "ramp() < 0 for ramp() <= 0" turns it red.
func TestBreakpoint_ASearchWithoutARampHasNoRampBlock(t *testing.T) {
	plain := func() RunReport {
		// A warm-up of 3s: a ramp of 0 would print a block if the guard let it.
		return blockRun(0, 3*time.Second, secondsOf(5, func(i int) engine.Second {
			return blockSecond(float64(100+i*10), 100, 0)
		})...)
	}

	var out bytes.Buffer
	PrintBreakpoint(&out, searchOfRuns(plain(), plain(), plain()))

	if strings.Contains(out.String(), "by second") {
		t.Errorf("a search without a ramp prints a block by second:\n%s", out.String())
	}
}

// A plain run says its mode too, so a consumer can tell the two apart.
func TestJSON_APlainRunSaysItsMode(t *testing.T) {
	out := writeJSON(t, JSONRun{Target: "t", Outcome: OutcomeComplete, Run: clockRun(0, time.Millisecond)})
	if out["mode"] != "run" {
		t.Errorf("mode %v, want run", out["mode"])
	}
}
