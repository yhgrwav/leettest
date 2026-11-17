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

package engine

import (
	"context"
	"math"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// One method more than once in a run (reviews/scenario-same-method.md, U2).
// Compile stubs: RowKeys returns bare methods and MethodReport has the three
// fields, so a repeat is refused by New and every run test below is red on
// that assert until the code; RowKeys and the namespace tests are in
// rowkeys_test.go.

const (
	smGet    = "/pkg.Svc/Get"
	smUpdate = "/pkg.Svc/Update"
	smOther  = "/pkg.Svc/Other"
)

// smRecords are the n records of a file tagged tag, with a ref that says so.
func smRecords(tag string, n int) ([][]byte, *DatasetRef) {
	out := make([][]byte, n)
	for i := range out {
		out[i] = []byte(tag + strconv.Itoa(i))
	}

	return out, &DatasetRef{File: "data/" + tag + ".jsonl", SHA256: "sha-" + tag}
}

func smRun(t *testing.T, opts Options) (*Engine, Report) {
	t.Helper()

	eng, err := New(opts)
	if err != nil {
		t.Fatalf("new: %v (a method repeated in a run is allowed)", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	report := eng.Report()

	return eng, report
}

// Ground: contract — verdict §2, §5, §6: the scenario "s1: Get, Update, Get" has a
// row per step, each with its own numbers: Sent (the step-3 Get is reached
// only by the iterations whose Update went well), Timeout, Dataset; the bare
// method has no row; the row says its Scenario, Step (from 1) and GRPCMethod;
// the sender still gets the real method. Mutations: no key set in iterate
// (M1, steps land in one bare row), Report() matching a step by its method
// (M6, both Gets take one Timeout and Dataset), the rest of the key sites
// (M7, M8) in the next test.
func TestRun_ARepeatedMethodInAScenarioHasARowPerStep(t *testing.T) {
	var updates atomic.Int64

	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		// The first four Updates fail: those iterations end there.
		smUpdate: func(context.Context, Request) (Outcome, error) {
			if updates.Add(1) <= 4 {
				return Outcome{Category: CategoryServerFault, Err: ErrFakeFailure}, nil
			}

			return Outcome{Category: CategorySuccess}, nil
		},
	}}

	headBodies, headRef := smRecords("head", 5)
	tailBodies, tailRef := smRecords("tail", 7)

	_, report := smRun(t, Options{
		Calls: []Call{{
			Method: smGet, Scenario: "s1", Timeout: time.Second, Payloads: headBodies, Dataset: headRef,
			Next: []Step{
				{Method: smUpdate, Timeout: 2 * time.Second},
				{Method: smGet, Timeout: 3 * time.Second, Payloads: tailBodies, Dataset: tailRef},
			},
			Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 100 * time.Millisecond}},
		}},
		Sender:      sender,
		MaxInFlight: 256,
	})

	headKey, tailKey := smGet+" (s1, step 1)", smGet+" (s1, step 3)"

	if _, bare := methodRow(report, smGet); bare {
		t.Errorf("a row for the bare %s: the two Gets are one row", smGet)
	}
	if len(report.Methods) != 3 {
		t.Errorf("%d rows, want 3 (the head, Update, the step-3 Get)", len(report.Methods))
	}

	type want struct {
		key        string
		scenario   string
		step       int
		grpc       string
		sent       int
		timeout    time.Duration
		datasetOf  string
		datasetLen int
	}

	for _, w := range []want{
		{headKey, "s1", 1, smGet, 10, time.Second, "data/head.jsonl", 5},
		{smUpdate, "s1", 2, smUpdate, 10, 2 * time.Second, "", 0},
		{tailKey, "s1", 3, smGet, 6, 3 * time.Second, "data/tail.jsonl", 7},
	} {
		m, ok := methodRow(report, w.key)
		if !ok {
			t.Errorf("no row %q", w.key)

			continue
		}
		if m.Scenario != w.scenario || m.Step != w.step || m.GRPCMethod != w.grpc {
			t.Errorf("%s: scenario %q step %d grpc %q, want %q %d %q", w.key, m.Scenario, m.Step, m.GRPCMethod, w.scenario, w.step, w.grpc)
		}
		if m.Sent != w.sent {
			t.Errorf("%s: sent %d, want %d", w.key, m.Sent, w.sent)
		}
		if m.Timeout != w.timeout {
			t.Errorf("%s: timeout %v, want %v", w.key, m.Timeout, w.timeout)
		}
		switch {
		case w.datasetOf == "" && m.Dataset != nil:
			t.Errorf("%s: dataset %+v, want none", w.key, *m.Dataset)
		case w.datasetOf != "" && m.Dataset == nil:
			t.Errorf("%s: no dataset, want %s", w.key, w.datasetOf)
		case w.datasetOf != "" && (m.Dataset.File != w.datasetOf || m.Dataset.Records != w.datasetLen):
			t.Errorf("%s: dataset %s of %d records, want %s of %d", w.key, m.Dataset.File, m.Dataset.Records, w.datasetOf, w.datasetLen)
		}
	}

	sc := scenarioOf(t, report, "s1")
	var steps []string
	for _, s := range sc.Steps {
		steps = append(steps, s.Method)
	}
	if want := []string{headKey, smUpdate, tailKey}; !slices.Equal(steps, want) {
		t.Errorf("scenario steps %q, want %q", steps, want)
	}

	// The key stays out of the request: Invoke needs the real method.
	sent := sender.methods()
	if got := slices.Compact(slices.Sorted(slices.Values(sent))); !slices.Equal(got, []string{smGet, smUpdate}) {
		t.Errorf("the sender got methods %q, want only %q and %q", got, smGet, smUpdate)
	}
}

// Ground: contract — verdict §2, §5: a plain Get and a scenario holding Get as
// a step are two rows with no state in common: the plain one bare, the step
// with the suffix; the step row's rate divides by its head's plan (a quarter
// of the plain call's), its dataset counts iterations, the plain call's its
// own requests; the head's plan reaches the step (setScenario takes keys,
// M8), the handed-out counts are told apart (noteHanded, M7); and a call
// numbered on the bare row (M3) or the step merged into the plain row (M1)
// turns it red. A `pass` threshold judges the plain row by its method, so the
// plain row must be the bare one with the plain call's own numbers.
func TestRun_APlainCallAndAStepOfOneMethodAreTwoRows(t *testing.T) {
	plainBodies, plainRef := smRecords("plain", 100)
	stepBodies, stepRef := smRecords("step", 100)

	eng, report := smRun(t, Options{
		Calls: []Call{
			{
				Method: smGet, Timeout: time.Second, Payloads: plainBodies, Dataset: plainRef,
				Stages: []Stage{{StartRPS: 10, TargetRPS: 10, Duration: time.Second}},
			},
			{
				Method: smOther, Scenario: "s1", Timeout: time.Second,
				Next:   []Step{{Method: smGet, Timeout: time.Second, Payloads: stepBodies, Dataset: stepRef}},
				Stages: []Stage{{StartRPS: 20, TargetRPS: 20, Duration: 250 * time.Millisecond}},
			},
		},
		Sender:      &scripted{},
		MaxInFlight: 256,
	})

	stepKey := smGet + " (s1, step 2)"

	plain, ok := methodRow(report, smGet)
	if !ok {
		t.Fatalf("no row for the plain %s: it must stay bare", smGet)
	}
	step, ok := methodRow(report, stepKey)
	if !ok {
		t.Fatalf("no row %q", stepKey)
	}
	if plain.Scenario != "" || plain.Step != 0 || plain.GRPCMethod != smGet {
		t.Errorf("plain row: scenario %q step %d grpc %q, want none, 0 and %q", plain.Scenario, plain.Step, plain.GRPCMethod, smGet)
	}
	if step.Scenario != "s1" || step.Step != 2 || step.GRPCMethod != smGet {
		t.Errorf("step row: scenario %q step %d grpc %q, want s1, 2 and %q", step.Scenario, step.Step, step.GRPCMethod, smGet)
	}

	if plain.Sent != 10 || step.Sent != 5 {
		t.Errorf("sent: plain %d, step %d; want 10 and 5", plain.Sent, step.Sent)
	}
	if math.Abs(plain.RPS-10) > 0.5 || math.Abs(step.RPS-20) > 0.5 {
		t.Errorf("sent/s: plain %.2f over its 1 s, step %.2f over its head's 0.25 s; want 10 and 20", plain.RPS, step.RPS)
	}

	if plain.Dataset == nil || step.Dataset == nil {
		t.Fatalf("datasets: plain %v, step %v; want both", plain.Dataset, step.Dataset)
	}
	if plain.Dataset.File != "data/plain.jsonl" || step.Dataset.File != "data/step.jsonl" {
		t.Errorf("dataset files %q and %q, want the plain call's and the step's own", plain.Dataset.File, step.Dataset.File)
	}
	if plain.Dataset.Used != 10 || step.Dataset.Used != 5 {
		t.Errorf("dataset used: plain %d (requests), step %d (iterations); want 10 and 5", plain.Dataset.Used, step.Dataset.Used)
	}

	// The live view's target rate is the head's, by key, and the plain call's.
	snapshot := eng.Snapshot()
	for _, m := range snapshot.Methods {
		if m.Method == smGet && m.TargetRPS != 10 {
			t.Errorf("target of the plain row %d, want 10", m.TargetRPS)
		}
		if m.Method == smOther && m.TargetRPS != 20 {
			t.Errorf("target of the head row %d, want 20", m.TargetRPS)
		}
	}
}

// Ground: contract — verdict §5: DatasetExhausted.Method is the key of the row
// of the call whose file ran out, not its method, so a repeated head is told
// from its twin.
func TestRun_DatasetExhaustedNamesTheRowKeyOfARepeatedHead(t *testing.T) {
	bodies, ref := smRecords("once", 1)
	ref.Once = true

	_, report := smRun(t, Options{
		Calls: []Call{{
			Method: smGet, Scenario: "s1", Timeout: time.Second, Payloads: bodies, Dataset: ref,
			Next:   []Step{{Method: smUpdate, Timeout: time.Second}, {Method: smGet, Timeout: time.Second}},
			Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 100 * time.Millisecond}},
		}},
		Sender:      &scripted{},
		MaxInFlight: 256,
	})

	if report.DatasetExhausted == nil {
		t.Fatal("the one-record file did not run out")
	}
	if want := smGet + " (s1, step 1)"; report.DatasetExhausted.Method != want {
		t.Errorf("exhausted method %q, want %q", report.DatasetExhausted.Method, want)
	}
}

// Ground: contract — verdict §5, §9: the planned rate of the head of a scenario
// is looked up by its row key (callOf), so the repeated head "Get (s1, step 1)"
// has its plan's rate and a row keyed by the bare method none. M9 (by the
// method) makes it 0.
func TestStatsExportInto_PlannedRPSOfARepeatedHeadIsItsPlans(t *testing.T) {
	head := smGet + " (s1, step 1)"
	stats, _ := startedStats(0, head, smUpdate, smGet+" (s1, step 3)")
	calls := []Call{rkFlow("s1", smGet, smUpdate, smGet)}
	calls[0].Stages = []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Hour}}

	view := exportOf(stats, calls...)

	rates := map[string]float64{}
	for _, m := range view.Methods {
		rates[m.Name] = m.PlannedRPS
	}
	if rates[head] != 100 {
		t.Errorf("planned rps of %q = %v, want 100 (the head's plan)", head, rates[head])
	}
}
