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
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// JSON of a scenario (v0.9 unit 2): a top-level scenarios array next to
// methods, schema_version stays 1. The reports are the hand-built ones of
// scenario_report_test.go.

// scenarioEntry is scenarios[i] of the written report, decoded.
func scenarioEntry(t *testing.T, run RunReport, i int) map[string]any {
	t.Helper()

	out := writeJSON(t, jsonRun(run.Report))
	list, ok := field(t, out, "scenarios").([]any)
	if !ok || len(list) <= i {
		t.Fatalf("scenarios = %v, want at least %d entries", field(t, out, "scenarios"), i+1)
	}
	entry, ok := list[i].(map[string]any)
	if !ok {
		t.Fatalf("scenarios[%d] = %v, want an object", i, list[i])
	}

	return entry
}

func decodeExpected(t *testing.T, doc string) map[string]any {
	t.Helper()

	var want map[string]any
	if err := json.Unmarshal([]byte(doc), &want); err != nil {
		t.Fatalf("the expected document is not JSON: %v", err)
	}

	return want
}

// Ground: contract — the entry of a stopped three-step scenario, key by key:
// iterations (not scheduled), all 11 categories as int, the latency as the
// methods' quantile objects (p99 null under the minimum count), the
// observations behind them, the censored ones and one object per step. The
// step method is spelled as methods[].method spells it. Mutation "scheduled
// for iterations" dies here.
func TestJSON_AScenarioEntryOfThreeSteps(t *testing.T) {
	run := scenarioRun()
	methods := field(t, writeJSON(t, jsonRun(run.Report)), "methods").([]any)
	name := func(k int) string { return methods[k].(map[string]any)["method"].(string) }

	want := decodeExpected(t, `{
		"name": "pay-and-check", "iterations": 100,
		"categories": {"success": 84, "request_error": 3, "overload": 2, "failure": 4, "timed_out": 4,
			"cut_off": 0, "unreachable": 0, "client_error": 0, "bad_response": 0, "aborted": 3, "unclassified": 0},
		"latency": {"p50": {"us": 31200, "ns": 31200000, "lower_bound": false}, "p99": null},
		"observations": 91, "censored": 7,
		"steps": [
			{"method": "`+name(0)+`", "reached": 100, "failed_here": 3, "aborted_here": 2, "not_sent_here": 1, "not_reached": 0},
			{"method": "`+name(1)+`", "reached": 94, "failed_here": 5, "aborted_here": 1, "not_sent_here": 0, "not_reached": 6},
			{"method": "`+name(2)+`", "reached": 88, "failed_here": 4, "aborted_here": 0, "not_sent_here": 0, "not_reached": 12}
		]
	}`)

	if got := scenarioEntry(t, run, 0); !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("scenarios[0]:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}
}

// A timed-out or aborted iteration is only known to have lasted so long, so
// the percentile it sets is a lower bound, as in a method's quantile.
// Ground: contract — lower_bound true where the report's text marks it with
// ">". Mutation 4 (the flag dropped for timeout-censored iterations) dies here.
func TestJSON_AScenarioLatencyKeepsItsLowerBound(t *testing.T) {
	run := scenarioTwoStepRun()

	got := scenarioEntry(t, run, 0)["latency"]

	want := decodeExpected(t, `{
		"p50": {"us": 12300, "ns": 12300000, "lower_bound": false},
		"p99": {"us": 456000, "ns": 456000000, "lower_bound": true}
	}`)
	if !reflect.DeepEqual(got, any(want)) {
		t.Errorf("latency = %v, want %v", got, want)
	}
}

// Ground: contract — categories are a fixed set of 11 names, each an int, and
// they add up to the iterations: a scenario's outcome has no remainder.
func TestJSON_AScenarioCategoriesAddUpToIterations(t *testing.T) {
	for name, run := range map[string]RunReport{"stopped": scenarioRun(), "finished": scenarioTwoStepRun()} {
		t.Run(name, func(t *testing.T) {
			entry := scenarioEntry(t, run, 0)
			cats, ok := entry["categories"].(map[string]any)
			if !ok || len(cats) != 11 {
				t.Fatalf("categories = %v, want an object of the 11 contract names", entry["categories"])
			}

			sum := 0.0
			for _, n := range cats {
				sum += n.(float64)
			}
			if sum != entry["iterations"].(float64) {
				t.Errorf("categories add up to %v, iterations %v", sum, entry["iterations"])
			}
		})
	}
}

// Ground: contract — the counter invariant on the written numbers, not only
// the engine's: reached(k+1) = reached(k) - failed_here - aborted_here -
// not_sent_here, and reached + not_reached = iterations.
func TestJSON_AScenarioStepsKeepTheCounterInvariant(t *testing.T) {
	for name, run := range map[string]RunReport{"stopped": scenarioRun(), "finished": scenarioTwoStepRun()} {
		t.Run(name, func(t *testing.T) {
			entry := scenarioEntry(t, run, 0)
			iterations := entry["iterations"].(float64)
			steps := entry["steps"].([]any)

			for k, s := range steps {
				row := s.(map[string]any)
				reached := row["reached"].(float64)
				if reached+row["not_reached"].(float64) != iterations {
					t.Errorf("step %d: reached %v + not_reached %v != iterations %v", k+1, reached, row["not_reached"], iterations)
				}
				if k+1 < len(steps) {
					left := reached - row["failed_here"].(float64) - row["aborted_here"].(float64) - row["not_sent_here"].(float64)
					if next := steps[k+1].(map[string]any)["reached"].(float64); next != left {
						t.Errorf("step %d lets %v through, step %d reached %v", k+1, left, k+2, next)
					}
				}
			}
		})
	}
}

// A plain run has the key, empty: a reader that loops over scenarios must not
// meet null. The schema guard cannot say this: its line for the key is
// "scenarios []object" whether the slice is nil or empty.
// Ground: contract — scenarios is always an array. Mutation 5 (null, or the
// key left out) dies here.
func TestJSON_APlainRunHasAnEmptyScenariosArray(t *testing.T) {
	for name, report := range map[string]engine.Report{
		"no calls": {},
		"one call": {Methods: []engine.MethodReport{{Method: "/pkg.S/M"}}},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteJSON(&buf, jsonRun(report)); err != nil {
				t.Fatal(err)
			}

			if !bytes.Contains(buf.Bytes(), []byte(`"scenarios":[]`)) {
				t.Errorf(`the output has no "scenarios":[]:`+"\n%s", buf.String())
			}
			if list, ok := field(t, writeJSON(t, jsonRun(report)), "scenarios").([]any); !ok || len(list) != 0 {
				t.Errorf("scenarios = %v, want []", field(t, writeJSON(t, jsonRun(report)), "scenarios"))
			}
		})
	}
}

// GUARD (green at once): a step method has no plan of its own and its row
// says so with 0, in the types the schema already has; the head call keeps
// its plan. The reference tells the reader what 0 means there.
// Ground: contract — A6, planned_* of a step method are 0 in schema 1.
func TestJSON_AStepMethodKeepsPlannedZero(t *testing.T) {
	methods := field(t, writeJSON(t, jsonRun(scenarioRun().Report)), "methods").([]any)

	for k, want := range []struct{ low, high, durationUS float64 }{{12, 12, 10_000_000}, {0, 0, 0}, {0, 0, 0}} {
		m := methods[k].(map[string]any)
		got := [3]float64{m["planned_rps_low"].(float64), m["planned_rps_high"].(float64), m["planned_duration_us"].(float64)}
		if got != [3]float64{want.low, want.high, want.durationUS} {
			t.Errorf("methods[%d] plan = %v, want %v", k, got, want)
		}
	}
}

// GUARD (green at once): compare does not break on a report with scenarios.
// The key is ignored on reading, and step methods are ordinary methods to it:
// the same Comparison with the key as without it, and no error reading it.
// Ground: contract — A6, additive key, schema_version stays 1.
func TestCompare_ScenariosKeyDoesNotChangeTheComparison(t *testing.T) {
	read := func(t *testing.T, name string, report engine.Report) RunFile {
		t.Helper()

		path := filepath.Join(t.TempDir(), name)
		run := jsonRun(report)
		run.Run.Passport = limitPassport("linux", 8, 8, nil)

		var buf bytes.Buffer
		if err := WriteJSON(&buf, run); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}

		got, err := ReadReport(path)
		if err != nil {
			t.Fatalf("ReadReport(%s) = %v", name, err)
		}

		return RunFile{Path: path, Report: got.Run}
	}

	with := scenarioRun().Report
	without := scenarioRun().Report
	without.Scenarios = nil

	baseWith, baseWithout := read(t, "base-with.json", with), read(t, "base-without.json", without)
	newWithout := read(t, "new-without.json", without)
	newWith := read(t, "new-with.json", with)

	want := CompareRuns(io.Discard, baseWithout, newWithout, nil, nil)
	if !want.Comparable {
		t.Fatal("the reports of the guard are not comparable: it is not set up")
	}

	for name, pair := range map[string][2]RunFile{
		"scenarios on the base": {baseWith, newWithout},
		"scenarios on the new":  {baseWithout, newWith},
		"scenarios on both":     {baseWith, newWith},
	} {
		if got := CompareRuns(io.Discard, pair[0], pair[1], nil, nil); got != want {
			t.Errorf("%s: Comparison = %+v, want %+v as without the key", name, got, want)
		}
	}
}
