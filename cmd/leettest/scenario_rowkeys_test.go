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

package main

import (
	"strings"
	"testing"
)

// A method repeated in a run, end to end on the fake target (v0.10 unit 3): a
// plain UnaryCall with a pass: limit next to a scenario that calls Check,
// UnaryCall, Check. UnaryCall is then in the run twice, so the scenario's row
// gets a key; the plain call stays bare, and its limit is judged on its own
// calls only.
// Ground: contract — spec 6, 7 and 9: the keys, the new row fields, the
// scenario steps by key, and the limit on the plain row alone.
func TestRun_ARepeatedMethodHasKeyedRowsAndThePlainCallStaysBare(t *testing.T) {
	const plain = "    - method: " + unaryCallMethod + "\n      rps: 40\n      duration: 1s\n      timeout: 2s\n" +
		"      pass:\n        p99: 1s\n        errors: 50%\n"
	steps := stepUnary + "        - method: " + checkMethod + "\n"
	cfg := dryConfig(t, closedPort(t), "", plain+scenarioCalls("pay", "10", "1s", steps))

	res, out := runFake(t, cfg)

	skipIfTheGeneratorDecided(t, res, out)
	wantExit(t, res, 0)

	type row struct {
		method, grpc, scenario string
		step                   any
	}
	rows := jsonAt(t, out, "methods").([]any)
	want := []row{
		{"/" + unaryCallMethod, "/" + unaryCallMethod, "", nil},
		{"/" + checkMethod + " (pay, step 1)", "/" + checkMethod, "pay", float64(1)},
		{"/" + unaryCallMethod + " (pay, step 2)", "/" + unaryCallMethod, "pay", float64(2)},
		{"/" + checkMethod + " (pay, step 3)", "/" + checkMethod, "pay", float64(3)},
	}
	if len(rows) != len(want) {
		t.Fatalf("%d rows in methods, want %d:\n%s", len(rows), len(want), res.stdout)
	}
	byKey := map[string]map[string]any{}
	for _, r := range rows {
		got := r.(map[string]any)
		byKey[got["method"].(string)] = got
	}
	for _, w := range want {
		got, ok := byKey[w.method]
		if !ok {
			t.Errorf("no row with the key %q among %d rows", w.method, len(byKey))

			continue
		}

		var scenario string
		if s, ok := got["scenario"].(string); ok {
			scenario = s
		}
		if got["grpc_method"] != w.grpc || scenario != w.scenario || got["step"] != w.step {
			t.Errorf("row %q = grpc_method %v, scenario %v, step %v; want %q, %q, %v",
				w.method, got["grpc_method"], got["scenario"], got["step"], w.grpc, w.scenario, w.step)
		}
	}

	// The scenario's steps are the same keys, in the scenario's order.
	steps0 := jsonAt(t, out, "scenarios").([]any)[0].(map[string]any)["steps"].([]any)
	for k, w := range []string{want[1].method, want[2].method, want[3].method} {
		if got := steps0[k].(map[string]any)["method"]; got != w {
			t.Errorf("scenarios[0].steps[%d].method = %v, want the key %q", k, got, w)
		}
	}

	// The limit is the plain call's: its method is bare, and what it judged is
	// the plain row's calls, not the plain row's and the step's together.
	plainRow := byKey["/"+unaryCallMethod]
	stepRow := byKey[want[2].method]
	if plainRow["sent"].(float64) <= stepRow["sent"].(float64) {
		t.Fatalf("plain sent %v, step sent %v: the plain call plans 40 rps, the scenario 10", plainRow["sent"], stepRow["sent"])
	}
	rowsOfLimits := verdicts(t, out)
	wantVerdict(t, rowsOfLimits, "p99", "pass")
	wantVerdict(t, rowsOfLimits, "errors", "pass")
	for _, key := range []string{"p99", "errors"} {
		if got, _ := rowsOfLimits[key]["method"].(string); strings.TrimPrefix(got, "/") != unaryCallMethod {
			t.Errorf("thresholds %s: method %v, want %s", key, rowsOfLimits[key]["method"], unaryCallMethod)
		}
	}
	if total, _ := rowsOfLimits["errors"]["total"].(float64); total != plainRow["sent"].(float64) {
		t.Errorf("errors judged %v calls, the plain row sent %v: the step's calls are not the plain call's", total, plainRow["sent"])
	}
}
