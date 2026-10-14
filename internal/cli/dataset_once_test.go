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
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
)

func onceOf(file string, records, used int) *engine.DatasetReport {
	d := datasetOf(file, records, used, 1)
	d.Once = true

	return d
}

// The line of a call that sends each record at most once ends with the words
// saying so, after the order when there is one; a call that reuses prints as
// it did.
// Ground: contract — the one place the text says the file was not wrapped.
func TestReport_DatasetLineEachAtMostOnce(t *testing.T) {
	random := onceOf("data/users.jsonl", 3, 3)
	random.Random, random.Seed = true, 42

	for _, tc := range []struct {
		name string
		d    *engine.DatasetReport
		want string
	}{
		{"file order", onceOf("data/users.jsonl", 3, 3),
			"  data: 3 of 3 requests from users.jsonl, each used up to 1 time, each at most once"},
		{"part of the file", onceOf("data/users.jsonl", 5, 2),
			"  data: 2 of 5 requests from users.jsonl, each used up to 1 time, each at most once"},
		{"nothing used", onceOf("data/users.jsonl", 3, 0),
			"  data: none of 3 requests from users.jsonl used, each at most once"},
		{"random order", random,
			"  data: 3 of 3 requests from users.jsonl, each used up to 1 time, random order, seed 42, each at most once"},
		{"reuse says nothing of it", datasetOf("data/users.jsonl", 3, 3, 2),
			"  data: 3 of 3 requests from users.jsonl, each used up to 2 times"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			PrintReport(&out, "localhost:50051", reportWith(engine.MethodReport{Method: "/pkg.S/M", Dataset: tc.d}))

			if got := lineAfter(t, out.String(), "pkg.S/M "); got != tc.want {
				t.Errorf("line under the method = %q, want %q", got, tc.want)
			}
		})
	}
}

// Ground: contract — the JSON names are an interface (schema_v1.txt): reuse is
// a bool, false exactly when the call sends each record at most once.
func TestJSON_DatasetReuse(t *testing.T) {
	out := writeJSON(t, jsonRun(engine.Report{Methods: []engine.MethodReport{
		{Method: "/pkg.S/Once", Sent: 1, Dataset: onceOf("data/users.jsonl", 3, 3)},
		{Method: "/pkg.S/Wrap", Sent: 1, Dataset: datasetOf("data/users.jsonl", 3, 3, 5)},
	}}))

	methods := field(t, out, "methods").([]any)
	if got := field(t, methods[0], "dataset", "reuse"); got != false {
		t.Errorf("each at most once: dataset.reuse = %v, want false", got)
	}
	if got := field(t, methods[1], "dataset", "reuse"); got != true {
		t.Errorf("wrapping: dataset.reuse = %v, want true", got)
	}
}

// exhaustedSearch is a search that held up to 244 rps and was stopped at 305
// by reason, the run at 305 carrying the exhaustion when it is set.
func exhaustedSearch(reason string, exhausted bool) BreakpointRun {
	last := bpRun(breakpoint.RateStep, 305, 0, 0, false, breakpoint.NoCause, "")
	if exhausted {
		last.Report.DatasetExhausted = &engine.DatasetExhausted{Method: "/pkg.Svc/Do", File: "data/users.jsonl"}
	}

	return BreakpointRun{
		Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, StopReason: reason,
		Result: breakpoint.Result{
			Outcome: breakpoint.Stopped, Held: 244,
			Steps: []breakpoint.Step{held(100), held(125), held(156), held(195), held(244), last},
		},
	}
}

func printedSearchRun(run BreakpointRun) string {
	var buf bytes.Buffer
	PrintBreakpoint(&buf, run)

	return buf.String()
}

// A search ended because a file ran out says which file, which call and at
// which rate, under the headline; ended any other way it says nothing of one.
// Ground: contract — exhaustion is never silent, and never claimed when
// someone else stopped the search first.
func TestBreakpoint_TextNamesTheDatasetThatRanOut(t *testing.T) {
	const want = "dataset users.jsonl (call pkg.Svc/Do) ran out at 305 rps"

	if out := printedSearchRun(exhaustedSearch(StopDatasetExhausted, true)); !strings.Contains(out, want) {
		t.Errorf("no %q in:\n%s", want, out)
	}

	for name, run := range map[string]BreakpointRun{
		"stopped on request": exhaustedSearch(StopRequested, false),
		"stopped on request, the file ran out in the drain": exhaustedSearch(StopRequested, true),
		"no reason": exhaustedSearch("", false),
	} {
		if out := printedSearchRun(run); strings.Contains(out, "ran out") {
			t.Errorf("%s: the text claims a file ran out:\n%s", name, out)
		}
	}
}

// Ground: contract — stop_reason is a closed list, "dataset_exhausted" |
// "requested" | null, the key always present (schema_breakpoint_v1.txt).
func TestBreakpoint_JSONStopReason(t *testing.T) {
	notStopped := BreakpointRun{
		Target: "t:1", Method: "pkg.Svc/Do", Plan: bpPlan, Result: outcomes["held_all"].res,
	}

	for name, tc := range map[string]struct {
		run  BreakpointRun
		want any
	}{
		"the file ran out":        {exhaustedSearch(StopDatasetExhausted, true), "dataset_exhausted"},
		"stopped on request":      {exhaustedSearch(StopRequested, false), "requested"},
		"the search ended itself": {notStopped, nil},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteBreakpointJSON(&buf, tc.run); err != nil {
				t.Fatalf("write: %v", err)
			}
			var out map[string]any
			if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
				t.Fatalf("json: %v", err)
			}

			if got := field(t, out, "breakpoint", "stop_reason"); got != tc.want {
				t.Errorf("breakpoint.stop_reason = %v, want %v", got, tc.want)
			}
			if got := field(t, out, "schema_version"); got != 1.0 {
				t.Errorf("schema_version = %v, want 1: the field is added", got)
			}
		})
	}
}

// dataset_reuse: false is carried to the engine as Once, per call; left out or
// true is not. Two calls on the same file are two calls: each has its counter,
// so the file is used once in each.
// Ground: contract — what the engine is handed (decisions «Dataset per call»).
func TestCalls_DatasetReuseGivesOnce(t *testing.T) {
	no, yes := false, true
	records := []config.DatasetRecord{rec(1, `{}`), rec(2, `{}`)}
	cfg := &config.MasterConfig{Load: config.Load{Calls: []config.Call{
		{Method: "a.B/Once", RPS: 5, Duration: time.Second, Dataset: "data/u.jsonl", DatasetReuse: &no, Records: records},
		{Method: "a.B/Again", RPS: 5, Duration: time.Second, Dataset: "data/u.jsonl", DatasetReuse: &no, Records: records},
		{Method: "a.B/Wrap", RPS: 5, Duration: time.Second, Dataset: "data/u.jsonl", DatasetReuse: &yes, Records: records},
		{Method: "a.B/Default", RPS: 5, Duration: time.Second, Dataset: "data/u.jsonl", Records: records},
	}}}

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}

	for i, want := range []bool{true, true, false, false} {
		if got := calls[i].Dataset.Once; got != want {
			t.Errorf("%s: Once = %v, want %v", calls[i].Method, got, want)
		}
	}
	if calls[0].Dataset.Counter == calls[1].Dataset.Counter {
		t.Error("two calls on one file share a counter: the file would be used once between them")
	}
}
