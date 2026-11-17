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

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// A method that appears more than once in a run (v0.10 unit 3): the report
// tells its rows apart by the key `<method> (<scenario>, step <n>)`, which the
// engine hands out as MethodReport.Method. The reports here are built by hand,
// as in scenario_report_test.go; what the engine counts is the engine's tests.

const (
	rkGet    = "/pay.v1.Wallet/GetBalance"
	rkMove   = "/pay.v1.Wallet/Transfer"
	rkGetKey = rkGet + " (pay, step 1)"
	rkGetEnd = rkGet + " (pay, step 3)"
)

// rkRepeatedRun is a run of one plain call and one scenario "pay" that calls
// the plain call's method at step 1 and step 3, with Transfer between: the
// rows are the bare plain call, two keyed rows, and Transfer, which appears
// once, so it is bare.
func rkRepeatedRun() RunReport {
	plan := 10 * time.Second

	return RunReport{Report: engine.Report{
		Duration: plan, Planned: plan, Sent: 640, Scheduled: 100,
		Methods: []engine.MethodReport{
			{Method: rkGet, GRPCMethod: rkGet, Sent: 400, RPS: 40, RPSLow: 40, RPSHigh: 40, PlannedDuration: plan},
			{Method: rkGetKey, GRPCMethod: rkGet, Scenario: "pay", Step: 1, Sent: 100, RPS: 10, RPSLow: 10, RPSHigh: 10, PlannedDuration: plan},
			{Method: rkMove, GRPCMethod: rkMove, Scenario: "pay", Step: 2, Sent: 100, RPS: 10},
			{Method: rkGetEnd, GRPCMethod: rkGet, Scenario: "pay", Step: 3, Sent: 100, RPS: 10},
		},
		Scenarios: []engine.ScenarioReport{{
			Name: "pay", Iterations: 100,
			ByCategory: map[engine.Category]int{engine.CategorySuccess: 100},
			Steps: []engine.ScenarioStepReport{
				{Method: rkGetKey, Reached: 100},
				{Method: rkMove, Reached: 100},
				{Method: rkGetEnd, Reached: 100},
			},
		}},
	}}
}

func rkMethodRows(t *testing.T, run RunReport) []map[string]any {
	t.Helper()

	var rows []map[string]any
	for _, m := range field(t, writeJSON(t, jsonRun(run.Report)), "methods").([]any) {
		rows = append(rows, m.(map[string]any))
	}

	return rows
}

// Ground: contract — spec 6: methods[].method is the row key, grpc_method the
// path the row called, scenario and step say where the row is; a plain call has
// null for both. Mutation M12 ("scenario and step swapped", "step counted from
// 0") dies here.
func TestJSON_MethodRowsCarryScenarioStepAndGRPCMethod(t *testing.T) {
	type want struct {
		method, grpc, scenario string
		step                   float64
	}

	rows := rkMethodRows(t, rkRepeatedRun())
	if len(rows) != 4 {
		t.Fatalf("len(methods) = %d, want 4", len(rows))
	}

	for i, w := range []want{
		{rkGet, rkGet, "", 0},
		{rkGetKey, rkGet, "pay", 1},
		{rkMove, rkMove, "pay", 2},
		{rkGetEnd, rkGet, "pay", 3},
	} {
		row := rows[i]
		if row["method"] != w.method {
			t.Errorf("methods[%d].method = %v, want the key %q", i, row["method"], w.method)
		}
		if row["grpc_method"] != w.grpc {
			t.Errorf("methods[%d].grpc_method = %v, want %q", i, row["grpc_method"], w.grpc)
		}

		scenario, hasScenario := row["scenario"]
		step, hasStep := row["step"]
		if !hasScenario || !hasStep {
			t.Errorf("methods[%d] has scenario %v, step %v: both fields are always written", i, hasScenario, hasStep)
		}
		if w.scenario == "" {
			if scenario != nil || step != nil {
				t.Errorf("methods[%d] (a plain call): scenario %v, step %v, want null and null", i, scenario, step)
			}

			continue
		}
		if scenario != w.scenario || step != w.step {
			t.Errorf("methods[%d]: scenario %v, step %v, want %q, %v", i, scenario, step, w.scenario, w.step)
		}
	}
}

// Without a repeat the rows keep their bare names: the key is the method and
// grpc_method says the same; a scenario step still tells where it is.
// Ground: contract — spec 6, the key equals the method when nothing repeats.
func TestJSON_WithoutARepeatTheKeyIsTheGRPCMethod(t *testing.T) {
	run := scenarioRun()

	rows := rkMethodRows(t, run)

	for i, want := range []string{scStep1, scStep2, scStep3} {
		if rows[i]["method"] != want || rows[i]["grpc_method"] != want {
			t.Errorf("methods[%d]: method %v, grpc_method %v, want both %q", i, rows[i]["method"], rows[i]["grpc_method"], want)
		}
	}
}

// Ground: contract — spec 6/7: the step of a scenario with no repeat has its
// scenario and its number all the same (head is step 1).
func TestJSON_AStepWithoutARepeatStillNamesItsScenarioAndNumber(t *testing.T) {
	run := scenarioRun()
	for i := range run.Methods {
		run.Methods[i].Scenario, run.Methods[i].Step = "pay-and-check", i+1
	}

	rows := rkMethodRows(t, run)

	for i := range rows {
		if rows[i]["scenario"] != "pay-and-check" || rows[i]["step"] != float64(i+1) {
			t.Errorf("methods[%d]: scenario %v, step %v, want pay-and-check, %d", i, rows[i]["scenario"], rows[i]["step"], i+1)
		}
	}
}

// Ground: contract — spec 7: scenarios[].steps[].method is the same key as
// methods[].method, so the two join. A step of a repeated method is the case.
func TestJSON_ScenarioStepsJoinTheMethodRowsByKey(t *testing.T) {
	out := writeJSON(t, jsonRun(rkRepeatedRun().Report))

	var keys []string
	for _, m := range field(t, out, "methods").([]any) {
		keys = append(keys, m.(map[string]any)["method"].(string))
	}

	steps := field(t, out, "scenarios").([]any)[0].(map[string]any)["steps"].([]any)
	for i, want := range []string{rkGetKey, rkMove, rkGetEnd} {
		got := steps[i].(map[string]any)["method"]
		if got != want {
			t.Errorf("scenarios[0].steps[%d].method = %v, want the key %q", i, got, want)
		}
		if !slices.Contains(keys, want) {
			t.Errorf("no methods[].method equals the step key %q: %q", want, keys)
		}
	}
}

// GUARD (green at once): the text report prints the key in the method table and
// in the scenario block; the keyed step rows have no plan line, so no "0 rps";
// the block's start rate is read from the head row by its key.
// Ground: contract — spec 8, the text names the row by its key.
func TestPrintReport_RepeatedMethodRowsPrintTheirKey(t *testing.T) {
	lines := echoLines(rkRepeatedRun())
	text := strings.Join(lines, "\n")

	for _, key := range []string{rkGetKey, rkGetEnd} {
		want := strings.TrimPrefix(key, "/")
		if n := strings.Count(text, want); n != 2 {
			t.Errorf("the key %q is printed %d times, want 2 (method table, scenario block):\n%s", want, n, text)
		}
	}

	if got := strings.Count(text, "plan: "); got != 2 {
		t.Errorf("%d plan lines, want 2 (the plain call and the scenario head, not the steps):\n%s", got, text)
	}
	if strings.Contains(text, " 0 rps") {
		t.Errorf("a step row is shown with a rate of its own:\n%s", text)
	}

	if !strings.Contains(text, "100 iterations, started at 10/s") {
		t.Errorf("the block has no start rate of the head row, read by its key:\n%s", text)
	}
}

// Without reflection the check of the methods only costs a warning, one per
// gRPC method: a method called by the head, by a step twice and by a plain call
// is named once.
// Ground: contract — spec 9, unchecked[].method is the gRPC method, one entry
// per method. Mutation M12 ("the unchecked dedup removed") dies here.
func TestAttachMethods_UncheckedNamesEachMethodOnce(t *testing.T) {
	cfg := scenarioConfig()
	cfg.Load.Calls[0].Steps = []config.Step{
		{Method: wsSecond, Timeout: time.Second}, {Method: wsFirst, Timeout: time.Second}, {Method: wsSecond, Timeout: time.Second},
	}
	cfg.Load.Calls[1].Method = wsSecond
	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	unchecked, _, err := AttachMethods(t.Context(), &fakeResolver{err: descriptor.ErrReflectionUnsupported}, cfg, calls)
	if err != nil {
		t.Fatalf("AttachMethods() = %v, want no error", err)
	}

	count := map[string]int{}
	for _, u := range unchecked {
		count[u.Method]++
	}
	for _, method := range []string{wsFirst, wsSecond} {
		if count[method] != 1 {
			t.Errorf("unchecked names %s %d times, want once: %v", method, count[method], count)
		}
	}
	if len(count) != 2 {
		t.Errorf("unchecked names %v, want the two gRPC methods only", count)
	}
}

// GUARD (green at once): compare pairs the rows by the unique key, so a plain
// run and a run with the same method repeated in a scenario are told apart by a
// methods line, not compared row to row and not an error. Spec 8: compare needs
// no code change.
// Ground: contract — spec 8.
func TestCompareRuns_AKeyedRepeatChangesTheMethodSet(t *testing.T) {
	base, nw := crBase(), crNew()
	base.methods = []crMethod{
		crCall("pkg.Svc/Get", crExact(crMillis(100)), 10),
		crCall("pkg.Svc/Update", crExact(crMillis(100)), 10),
	}
	nw.methods = []crMethod{
		crCall("pkg.Svc/Get (s1, step 1)", crExact(crMillis(100)), 10),
		crCall("pkg.Svc/Update", crExact(crMillis(100)), 10),
		crCall("pkg.Svc/Get (s1, step 3)", crExact(crMillis(100)), 10),
	}

	out, got := crCompare(t, base, nw, "", "")

	want := []string{
		"differs: methods: pkg.Svc/Get, pkg.Svc/Update -> pkg.Svc/Get (s1, step 1), pkg.Svc/Get (s1, step 3), pkg.Svc/Update",
	}
	if diffs := crDiffs(out); !slices.Equal(diffs, want) {
		t.Errorf("difference lines %q, want %q\n%s", diffs, want, out)
	}
	crWantResult(t, got, Comparison{})
}
