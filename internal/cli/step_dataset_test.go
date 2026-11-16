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
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// Step datasets (v0.9 unit 3c): config.Step.Records to engine.Step.Payloads,
// the DatasetRef and the counter of the step, the check against the step
// method's input, and the step method's row in the report. The config is built
// by hand; the red tests of Parse are in pkg/config, those of the rows and the
// counters in pkg/engine. Every method takes the order message (stepResolver),
// so a record that fits one fits all, and the tests tell the steps apart by
// name.

// sdStep is a step of n records, record i being {"id":"<tag>-<i>"} on line i+1
// of the file.
func sdStep(method, file, tag string, n int) config.Step {
	records := make([]config.DatasetRecord, n)
	for i := range records {
		records[i] = rec(i+1, fmt.Sprintf(`{"id":"%s-%d"}`, tag, i))
	}

	return config.Step{
		Method: method, Timeout: 100 * time.Millisecond,
		Dataset: file, Records: records, DatasetSHA256: "sha-" + tag,
	}
}

// sdConfig is a scenario of a call without data (the order method) and the
// given steps after it: 100 rps for 150 ms, 15 iterations.
func sdConfig(steps ...config.Step) *config.MasterConfig {
	return &config.MasterConfig{Load: config.Load{Calls: []config.Call{{
		Method: orderMethod, RPS: 100, Duration: 150 * time.Millisecond, Timeout: 100 * time.Millisecond,
		Scenario: "order-flow", Steps: steps,
	}}}}
}

// sdAttach builds the calls of cfg and attaches the data through resolver, as
// the run does.
func sdAttach(t *testing.T, resolver descriptor.Resolver, cfg *config.MasterConfig) ([]engine.Call, error) {
	t.Helper()

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}

	_, _, err = AttachMethods(t.Context(), resolver, cfg, calls)

	return calls, err
}

// Each step with records gets a slot per record, the file and the sha256 as the
// config has them, and a counter of its own: not the call's, not another
// step's. A step with no file stays plain.
// Ground: contract — "every dataset counts for itself": a counter shared with the
// call would give the step the call's rows.
// Mutation: the step takes the counter of the call, or one counter serves all steps.
func TestCallsFromConfig_StepDatasetBuildsItsSlotsRefAndCounter(t *testing.T) {
	cfg := sdConfig(
		sdStep(payOrder, "data/pay.jsonl", "pay", 3),
		sdStep(shipOrder, "data/ship.jsonl", "ship", 2),
		config.Step{Method: closeOrder, Timeout: 100 * time.Millisecond},
	)
	cfg.Load.Calls[0].Dataset = "data/orders.jsonl"
	cfg.Load.Calls[0].Records = []config.DatasetRecord{rec(1, `{}`), rec(2, `{}`)}

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}
	call := calls[0]
	if len(call.Next) != 3 || call.Dataset == nil || call.Dataset.Counter == nil {
		t.Fatalf("test setup: %d steps, call dataset %+v, want 3 steps and a counted call dataset", len(call.Next), call.Dataset)
	}

	var counters []*engine.RecordCounter
	for k, want := range []struct {
		file, sha string
		slots     int
	}{{"data/pay.jsonl", "sha-pay", 3}, {"data/ship.jsonl", "sha-ship", 2}} {
		step := call.Next[k]
		if len(step.Payloads) != want.slots {
			t.Errorf("step %d: %d payload slots, want %d", k+1, len(step.Payloads), want.slots)
		}
		ref := step.Dataset
		if ref == nil {
			t.Errorf("step %d: no Dataset, want the file %s", k+1, want.file)

			continue
		}
		if ref.File != want.file || ref.SHA256 != want.sha {
			t.Errorf("step %d: dataset %q sha %q, want %q sha %q", k+1, ref.File, ref.SHA256, want.file, want.sha)
		}
		if ref.Once {
			t.Errorf("step %d: Once is set; a step does not support it", k+1)
		}
		if ref.Counter == nil {
			t.Errorf("step %d: no Counter, want one of its own", k+1)
		}
		counters = append(counters, ref.Counter)
	}
	counters = append(counters, call.Dataset.Counter)
	for i := range counters {
		for j := i + 1; j < len(counters); j++ {
			if counters[i] != nil && counters[i] == counters[j] {
				t.Errorf("counters %d and %d are one: the call and its steps count apart", i, j)
			}
		}
	}

	plain := call.Next[2]
	if plain.Dataset != nil || len(plain.Payloads) != 0 {
		t.Errorf("a step with no file: dataset %+v, %d slots, want a plain step", plain.Dataset, len(plain.Payloads))
	}
}

// The records of a step are encoded by the step's method, not by the call's:
// {"id":"..."} is a string in one message and a number in the other.
// Ground: contract — "checked against the step method's Input", the body is what
// that method takes off the wire.
// Mutation: the step's records are encoded by the call's message.
func TestAttachMethods_StepDatasetRecordsAreEncodedByTheStepsMethod(t *testing.T) {
	resolver := &fakeResolver{known: map[string]protoreflect.MessageDescriptor{
		orderMethod: recMessage(t), payOrder: orderMessage(t),
	}}

	calls, err := sdAttach(t, resolver, sdConfig(sdStep(payOrder, "data/pay.jsonl", "pay", 3)))
	if err != nil {
		t.Fatalf("attach: %v, want the records accepted: they fit the step's method", err)
	}

	payloads := calls[0].Next[0].Payloads
	if len(payloads) != 3 {
		t.Fatalf("%d payloads, want 3", len(payloads))
	}
	desc := orderMessage(t)
	for i, p := range payloads {
		msg := dynamicpb.NewMessage(desc)
		if err := proto.Unmarshal(p, msg); err != nil {
			t.Errorf("record %d: payload %x is not an Order: %v", i, p, err)

			continue
		}
		if got, want := msg.Get(desc.Fields().ByName("id")).String(), fmt.Sprintf("pay-%d", i); got != want {
			t.Errorf("record %d: id = %q, want %q", i, got, want)
		}
	}
}

// A record that does not fit the step's method stops the run before it: the
// error names the step's path, the file and the line (blank lines counted), the
// message; never the record, a secret may stand in it. The same file with every
// record fitting is accepted.
// Ground: contract — the text is ours, the sentinel is data's (ErrRequestData).
// Mutations: the error names the call, or the step's data; it quotes the record.
func TestAttachMethods_StepDatasetThatDoesNotFitNamesTheLineNotTheRecord(t *testing.T) {
	const secret = "S3CR3T-4821"

	build := func(bad string) *config.MasterConfig {
		ship := sdStep(shipOrder, "data/ship.jsonl", "ship", 2)
		ship.Records = append(ship.Records, rec(5, bad), rec(7, `{"id":"later-7"}`))

		return sdConfig(sdStep(payOrder, "data/pay.jsonl", "pay", 2), ship)
	}

	if _, err := sdAttach(t, stepResolver(t), build(`{"id":"fine"}`)); err != nil {
		t.Fatalf("baseline: %v, want the records accepted", err)
	}

	want := "load.calls[0].steps[1].dataset: data/ship.jsonl:5: does not fit " + string(orderMessage(t).FullName())

	for name, bad := range map[string]string{
		"an unknown field": `{"nope":"` + secret + `"}`,
		"a wrong type":     `{"id":["` + secret + `"]}`,
		"not an object":    `"` + secret + `"`,
		"a duplicate":      `{"id":"a","id":"` + secret + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := sdAttach(t, stepResolver(t), build(bad))

			if !errors.Is(err, ErrRequestData) {
				t.Fatalf("error = %v, want %v", err, ErrRequestData)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q lacks %q", err, want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error %q quotes the record", err)
			}
		})
	}
}

// With no way to get the step method's schema the records cannot be checked and
// the run would send empty messages: an error in the words the call's dataset
// has, naming the step. A step with no dataset and no data only costs the check.
// Ground: contract — "same strictness as data of a step".
// Mutation: the step with a dataset is treated as a step with no data (unchecked).
func TestAttachMethods_StepDatasetNeedsASchemaFromReflectionOrAProto(t *testing.T) {
	resolver := &fakeResolver{err: descriptor.ErrReflectionUnsupported}

	plain := sdConfig(config.Step{Method: payOrder, Timeout: 100 * time.Millisecond})
	if _, err := sdAttach(t, resolver, plain); err != nil {
		t.Fatalf("baseline: a step with no data and no dataset got %v, want only an unchecked method", err)
	}

	_, err := sdAttach(t, resolver, sdConfig(sdStep(payOrder, "data/pay.jsonl", "pay", 2)))

	if !errors.Is(err, descriptor.ErrReflectionUnsupported) {
		t.Fatalf("error = %v, want %v", err, descriptor.ErrReflectionUnsupported)
	}
	for _, want := range []string{
		"load.calls[0].steps[0]",
		"the schema for its dataset comes from reflection; set app.proto or app.protoset to give it another way, or drop the dataset:",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "drop the data:") {
		t.Errorf("error %q has the wording of data, want the dataset's", err)
	}
}

// stepSender answers every request, keeps for each method the number of the
// record it carried (the first byte; -1 for an empty body) in the order the
// requests came, and fails the calls of failMethod. An empty body never panics
// it: the stubs of the red phase send empty ones.
type stepSender struct {
	failMethod string

	mu   sync.Mutex
	seen map[string][]int
}

func (s *stepSender) Send(_ context.Context, req engine.Request) (engine.Outcome, error) {
	record := -1
	if len(req.Payload) > 0 {
		record = int(req.Payload[0])
	}

	s.mu.Lock()
	if s.seen == nil {
		s.seen = map[string][]int{}
	}
	s.seen[req.Method] = append(s.seen[req.Method], record)
	s.mu.Unlock()

	now := time.Now()
	out := engine.Outcome{Category: engine.CategorySuccess, SentAt: now, DoneAt: now}
	if req.Method == s.failMethod {
		out.Category = engine.CategoryServerFault
	}

	return out, nil
}

func (s *stepSender) recordsOf(method string) []int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.seen[method])
}

// sdRun runs the calls CallsFromConfig builds from cfg, each record of a step
// carrying its own number as the body, and returns the report.
func sdRun(t *testing.T, cfg *config.MasterConfig, sender engine.Sender) engine.Report {
	t.Helper()

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}
	for _, c := range calls {
		for k := range c.Next {
			for i := range c.Next[k].Payloads {
				c.Next[k].Payloads[i] = []byte{byte(i)}
			}
		}
	}

	// rps x the timeouts of all steps must fit the cap, or New refuses the plan.
	eng, err := engine.New(engine.Options{Calls: calls, Sender: sender, MaxInFlight: 1024})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	return eng.Report()
}

func sdMethodRow(t *testing.T, report engine.Report, method string) engine.MethodReport {
	t.Helper()

	for i := range report.Methods {
		if displayMethod(report.Methods[i].Method) == method {
			return report.Methods[i]
		}
	}
	t.Fatalf("no row for %s among %d method rows", method, len(report.Methods))

	return engine.MethodReport{}
}

// sdRandom is cfg's step k made random with the seed (nil: none in the config).
func sdRandom(cfg *config.MasterConfig, k int, seed *int64) {
	random := "random"
	cfg.Load.Calls[0].Steps[k].DatasetOrder = &random
	cfg.Load.Calls[0].Steps[k].DatasetSeed = seed
}

// No seed in the config: the run draws one per step, reports it, and the same
// config with that seed hands out the same sequence. Two steps draw two seeds,
// and a seed in the config is the one reported.
// Ground: contract — "any run can be repeated" from its report (P28), for a step
// as for a call.
// Mutations: the seed is drawn once for the call and its steps; it is not
// reported; it is above 2^53-1.
func TestRun_StepDatasetDrawnSeedIsReportedAndRepeatsTheRun(t *testing.T) {
	methods := []string{payOrder, shipOrder}
	build := func(seeds ...*int64) *config.MasterConfig {
		cfg := sdConfig(sdStep(payOrder, "data/pay.jsonl", "pay", 5), sdStep(shipOrder, "data/ship.jsonl", "ship", 5))
		for k, seed := range seeds {
			sdRandom(cfg, k, seed)
		}

		return cfg
	}

	sender := &stepSender{}
	report := sdRun(t, build(nil, nil), sender)

	seeds := make([]int64, len(methods))
	first := make([][]int, len(methods))
	for k, m := range methods {
		ds := sdMethodRow(t, report, m).Dataset
		if ds == nil || !ds.Random {
			t.Fatalf("%s: dataset %+v, want a random order", m, ds)
		}
		if ds.Seed > maxSeed {
			t.Errorf("%s: drawn seed %d is above 2^53-1: a JSON reader on float64 would repeat another run", m, ds.Seed)
		}
		seeds[k] = int64(ds.Seed)

		first[k] = sender.recordsOf("/" + m)
		if len(first[k]) != 15 {
			t.Fatalf("%s: %d requests, want 15", m, len(first[k]))
		}
		for pass := 0; pass < 3; pass++ {
			seen := slices.Clone(first[k][pass*5 : pass*5+5])
			slices.Sort(seen)
			if !slices.Equal(seen, []int{0, 1, 2, 3, 4}) {
				t.Errorf("%s: pass %d = %v, want each record once", m, pass, first[k][pass*5:pass*5+5])
			}
		}
	}
	if seeds[0] == seeds[1] {
		t.Errorf("both steps drew seed %d: each dataset draws its own", seeds[0])
	}

	again := &stepSender{}
	sdRun(t, build(&seeds[0], &seeds[1]), again)
	for k, m := range methods {
		if got := again.recordsOf("/" + m); !slices.Equal(got, first[k]) {
			t.Errorf("%s: with seed %d the run sent %v, want %v as before", m, seeds[k], got, first[k])
		}
	}

	forty, seven := int64(42), int64(7)
	for k, m := range methods {
		got := sdMethodRow(t, sdRun(t, build(&forty, &seven), &stepSender{}), m).Dataset
		want := []uint64{42, 7}[k]
		if got == nil || !got.Random || got.Seed != want {
			t.Errorf("%s: dataset %+v, want random with seed %d, the config's", m, got, want)
		}
	}
}

// The scenario the whole unit is for: the call's answer is always a failure, so
// no iteration reaches the step, and the step's dataset still reports every
// iteration as using a row: 100 iterations over 10 records, each used 10 times,
// while the step method sent nothing. The text puts the line under the step
// method's row, the JSON puts the same object at methods[].dataset of that
// method, with the call's keys and no path of its own.
// Ground: contract — "used is iterations handed out with that step" (decisions,
// «Датасет на шаге»); the report says what the file was used for, not what the
// step reached.
// Mutations: used counts the step's sends; no dataset on a row that sent nothing;
// the line goes under the call's row.
func TestReport_StepDatasetUsedCountsIterationsNotStepsReached(t *testing.T) {
	const iterations, records = 100, 10

	cfg := sdConfig(sdStep(payOrder, "data/pay.jsonl", "pay", records))
	cfg.Load.Calls[0].RPS, cfg.Load.Calls[0].Duration = 500, 200*time.Millisecond

	report := sdRun(t, cfg, &stepSender{failMethod: "/" + orderMethod})
	if report.Scheduled != iterations {
		t.Fatalf("test setup: scheduled %d, want %d", report.Scheduled, iterations)
	}
	if row := sdMethodRow(t, report, payOrder); row.Sent != 0 {
		t.Fatalf("test setup: %s sent %d, want 0: the call failed every time", payOrder, row.Sent)
	}

	var out strings.Builder
	PrintReport(&out, "localhost:50051", RunReport{Report: report})

	const want = "  data: 10 of 10 requests from pay.jsonl, each used up to 10 times"
	if got := lineAfterRow(t, out.String(), payOrder); !strings.Contains(got, want) {
		t.Errorf("block of %s:\n%s\nwant the line %q", payOrder, got, want)
	}
	if n := strings.Count(out.String(), "  data: "); n != 1 {
		t.Errorf("%d data lines, want 1: the call has no file:\n%s", n, out.String())
	}
	if got := lineAfterRow(t, out.String(), orderMethod); strings.Contains(got, "  data: ") {
		t.Errorf("block of %s has a data line:\n%s", orderMethod, got)
	}

	doc := writeJSON(t, JSONRun{Target: "localhost:50051", Outcome: OutcomeComplete, Run: RunReport{Report: report}})
	if v := field(t, doc, "schema_version"); v != 1.0 {
		t.Errorf("schema_version = %v, want 1", v)
	}

	byMethod := map[string]any{}
	for _, m := range field(t, doc, "methods").([]any) {
		byMethod[displayMethod(field(t, m, "method").(string))] = m
	}

	obj, ok := field(t, byMethod[payOrder], "dataset").(map[string]any)
	if !ok {
		t.Fatalf("methods[%s].dataset = %v, want an object", payOrder, field(t, byMethod[payOrder], "dataset"))
	}
	wantObj := map[string]any{
		"file": "data/pay.jsonl", "sha256": "sha-pay", "records": 10.0, "used": 10.0, "used_max": 10.0,
		"order": "file", "seed": nil, "reuse": true,
	}
	if len(obj) != len(wantObj) {
		t.Errorf("dataset = %v, want exactly the keys of %v", obj, wantObj)
	}
	for k, v := range wantObj {
		if obj[k] != v {
			t.Errorf("dataset.%s = %v, want %v", k, obj[k], v)
		}
	}
	if v := field(t, byMethod[orderMethod], "dataset"); v != nil {
		t.Errorf("methods[%s].dataset = %v, want null: the call has no file", orderMethod, v)
	}

	// The same report says the step was reached by no iteration: both numbers
	// are true, and the report must not make one of the other.
	scenarios := field(t, doc, "scenarios").([]any)
	if len(scenarios) != 1 {
		t.Fatalf("%d scenarios, want 1", len(scenarios))
	}
	steps := field(t, scenarios[0], "steps").([]any)
	if len(steps) != 2 {
		t.Fatalf("%d scenario steps, want 2", len(steps))
	}
	if got := field(t, steps[1], "reached"); got != 0.0 {
		t.Errorf("scenarios[0].steps[1].reached = %v, want 0 beside used 10", got)
	}

	for _, s := range scenarios {
		for _, st := range field(t, s, "steps").([]any) {
			if _, has := st.(map[string]any)["dataset"]; has {
				t.Errorf("a scenario step has a dataset key: %v; the dataset is on methods[], schema 1 has no other path", st)
			}
		}
	}
}

// lineAfterRow is the block of the method's row: the row and the indented lines
// under it, up to the next row.
func lineAfterRow(t *testing.T, out, method string) string {
	t.Helper()

	var block []string
	in := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, method+" "):
			in = true
			block = append(block, line)
		case in && strings.HasPrefix(line, "  "):
			block = append(block, line)
		case in:
			return strings.Join(block, "\n")
		}
	}
	if !in {
		t.Fatalf("no row of %s in:\n%s", method, out)
	}

	return strings.Join(block, "\n")
}

// A changed sha256 of a step's file shows in compare exactly as a call's does:
// the line names the method that sends the step, and the code of compare is
// not touched.
// Ground: contract — "compare of a changed step dataset sha256 shows the same
// difference as for a call"; green on the stub, it stays so (a guard).
func TestCompareRuns_StepDatasetShaDiffersLikeACalls(t *testing.T) {
	c, d := strings.Repeat("c", 64), strings.Repeat("d", 64)

	base, nw := crBase(), crNew()
	for _, r := range []*crRun{&base, &nw} {
		r.methods = append(r.methods, crCall("shop.v1.Shop/PayOrder", crExact(crMillis(100)), 10))
	}
	base.methods[1].datasetSHA, nw.methods[1].datasetSHA = c, d

	out, got := crCompare(t, base, nw, "", "")

	if want := "differs: dataset shop.v1.Shop/PayOrder: " + c + " -> " + d; !strings.Contains(out, want) {
		t.Errorf("no %q in:\n%s", want, out)
	}
	if got.Comparable {
		t.Errorf("the reports are comparable with a changed dataset")
	}
}
