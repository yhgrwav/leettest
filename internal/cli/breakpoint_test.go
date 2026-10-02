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
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// bpPlan measures 4s of each step: planned calls = rps Г— 4.
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

func held(rps int) breakpoint.Step {
	return bpRun(breakpoint.RateStep, rps, rps*4, 21*time.Millisecond, false, breakpoint.NoCause, "")
}

const kneeWhy = "p99 341ms = 16.2Г— baseline 21ms (no p99_limit set)"

// outcomes is one result per outcome, with the headline the text report
// opens with.
var outcomes = map[string]struct {
	res      breakpoint.Result
	headline string
}{
	"broke": {breakpoint.Result{
		Outcome: breakpoint.BrokeBetween, Held: 244, Broke: 305, Cause: breakpoint.CauseP99VsBase,
		Steps: []breakpoint.Step{
			held(100), held(125), held(156), held(195), held(244),
			bpRun(breakpoint.RateStep, 305, 1220, 341*time.Millisecond, true, breakpoint.CauseP99VsBase, kneeWhy),
			bpRun(breakpoint.Probe, 100, 500, 22*time.Millisecond, false, breakpoint.NoCause, ""),
			bpRun(breakpoint.Repeat, 305, 1220, 338*time.Millisecond, true, breakpoint.CauseP99VsBase, kneeWhy),
		},
	}, "held 244 rps, broke at 305 rps"},
	"broke_at_first": {breakpoint.Result{
		Outcome: breakpoint.BrokeAtFirst, Broke: 100, Cause: breakpoint.CauseErrors,
		Steps: []breakpoint.Step{
			bpRun(breakpoint.RateStep, 100, 400, 30*time.Millisecond, true, breakpoint.CauseErrors, "failed 200 of 400 calls (50.0%)"),
			bpRun(breakpoint.Repeat, 100, 400, 30*time.Millisecond, true, breakpoint.CauseErrors, "failed 200 of 400 calls (50.0%)"),
		},
		Notes: []string{"no lower step to check recovery against; start lower (from) for a reliable result"},
	}, "broke at the first step, 100 rps: the limit is at or below it"},
	"held_all": {breakpoint.Result{
		Outcome: breakpoint.HeldThroughout, Held: 381,
		Steps: []breakpoint.Step{held(100), held(125), held(156), held(195), held(244), held(305), held(381)},
	}, "held every step up to 381 rps: the limit is above it"},
	"run_limit": {breakpoint.Result{
		Outcome: breakpoint.RunLimit, Held: 100, Broke: 125, Cause: breakpoint.CauseGenerator,
		Steps: []breakpoint.Step{held(100),
			bpRun(breakpoint.RateStep, 125, 450, 21*time.Millisecond, false, breakpoint.CauseGenerator,
				"the generator sent 112 of 125 rps; the target above that is untested")},
	}, "the run gave out at 125 rps, not the target: the generator sent 112 of 125 rps; the target above that is untested; the target held 100 rps"},
	"run_limit_first": {breakpoint.Result{
		Outcome: breakpoint.RunLimit, Broke: 100, Cause: breakpoint.CauseGenerator,
		Steps: []breakpoint.Step{bpRun(breakpoint.RateStep, 100, 300, 21*time.Millisecond, false, breakpoint.CauseGenerator,
			"the generator sent 75 of 100 rps; the target above that is untested")},
	}, "the run gave out at the first step (100 rps); nothing was learned about the target"},
	"stopped": {breakpoint.Result{
		Outcome: breakpoint.Stopped, Held: 244,
		Steps: []breakpoint.Step{held(100), held(125), held(156), held(195), held(244),
			bpRun(breakpoint.RateStep, 305, 1220, 341*time.Millisecond, true, breakpoint.CauseP99VsBase, kneeWhy),
			bpRun(breakpoint.Probe, 100, 200, 22*time.Millisecond, false, breakpoint.NoCause, "")},
	}, "stopped at 100 rps (probe); held 244 rps so far"},
	"stopped_first": {breakpoint.Result{
		Outcome: breakpoint.Stopped,
		Steps:   []breakpoint.Step{bpRun(breakpoint.RateStep, 100, 200, 21*time.Millisecond, false, breakpoint.NoCause, "")},
	}, "stopped at the first step; nothing was learned about the target"},
	"invalid": {breakpoint.Result{
		Outcome: breakpoint.Invalid, Held: 244, Cause: breakpoint.CauseClockStep,
		Steps: []breakpoint.Step{held(100), held(125), held(156), held(195), held(244),
			bpRun(breakpoint.RateStep, 305, 1220, 341*time.Millisecond, false, breakpoint.CauseClockStep, "invalid run: clock step")},
	}, "invalid search: clock step at 305 rps; no breaking point is reported"},
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

// Each outcome opens with its own headline, under the method's name.
func TestBreakpoint_EachOutcomeHasItsHeadline(t *testing.T) {
	for name, tc := range outcomes {
		out := printedSearch(tc.res)
		if !strings.Contains(out, "breaking point: pkg.Svc/Do\n  "+tc.headline+"\n") {
			t.Errorf("%s: want the headline %q under the method, got\n%s", name, tc.headline, out)
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

// The plan line says before the first step how long the search can take.
func TestBreakpoint_ThePlanLine(t *testing.T) {
	p := breakpoint.Plan{From: 100, To: 156, Settle: 100 * time.Millisecond, Hold: time.Second, Timeout: 500 * time.Millisecond}
	if got, want := PlanLine(p), "breakpoint: up to 3 steps, at most 57.5s"; got != want {
		t.Errorf("plan line %q, want %q", got, want)
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

// A plain run says its mode too, so a consumer can tell the two apart.
func TestJSON_APlainRunSaysItsMode(t *testing.T) {
	out := writeJSON(t, JSONRun{Target: "t", Outcome: OutcomeComplete, Run: clockRun(0, time.Millisecond)})
	if out["mode"] != "run" {
		t.Errorf("mode %v, want run", out["mode"])
	}
}
