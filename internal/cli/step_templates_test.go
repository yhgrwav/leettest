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
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// Step templates (v0.9 unit 3b): config.Step.Templates to engine.Step.Build,
// checked against the step method's input before the load, with the limit and
// the placeholders of the steps folded into the call's template. The config is
// built by hand; the red tests of Parse are in pkg/config. Every method here
// takes the order message, so a body that fits one fits all, and the tests
// tell the steps apart by name.

const (
	payOrder   = "shop.v1.Shop/PayOrder"
	shipOrder  = "shop.v1.Shop/ShipOrder"
	closeOrder = "shop.v1.Shop/CloseOrder"

	maxInt32 = 2147483647
)

var stepMethods = []string{payOrder, shipOrder, closeOrder}

// stepSpec is one step: its data and its templates key (nil is a key left out).
type stepSpec struct {
	data      map[string]any
	templates *bool
}

func tplStep(data map[string]any) stepSpec { return stepSpec{data: data, templates: templatesOn()} }

// steppedConfig is a scenario of a templated call (the order method) and the
// steps after it, over stepMethods in order. rps and duration are small: a
// test that runs it is over in a moment.
func steppedConfig(callData map[string]any, steps ...stepSpec) *config.MasterConfig {
	out := make([]config.Step, len(steps))
	for k, s := range steps {
		out[k] = config.Step{Method: stepMethods[k], Timeout: 100 * time.Millisecond, Data: s.data, Templates: s.templates}
	}

	call := config.Call{
		Method: orderMethod, RPS: 50, Duration: 300 * time.Millisecond, Timeout: 100 * time.Millisecond,
		Scenario: "order-flow", Steps: out, Templates: templatesOn(),
	}
	// No data is a nil Data, as a config without the key has it; a nil map in
	// the interface would be data, taken the other way.
	if callData != nil {
		call.Data = callData
	}

	return &config.MasterConfig{Load: config.Load{Calls: []config.Call{call}}}
}

func stepResolver(t testing.TB) *fakeResolver {
	t.Helper()

	known := map[string]protoreflect.MessageDescriptor{orderMethod: orderMessage(t)}
	for _, m := range stepMethods {
		known[m] = orderMessage(t)
	}

	return &fakeResolver{known: known}
}

// attachStepped builds the engine calls of cfg and attaches the data as the run
// does.
func attachStepped(t testing.TB, cfg *config.MasterConfig) ([]engine.Call, error) {
	t.Helper()

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}

	_, _, err = AttachMethods(t.Context(), stepResolver(t), cfg, calls)

	return calls, err
}

// attachedStepped is attachStepped for a config that must be accepted.
func attachedStepped(t testing.TB, cfg *config.MasterConfig) engine.Call {
	t.Helper()

	calls, err := attachStepped(t, cfg)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if calls[0].Template == nil {
		t.Fatal("the call has no Template")
	}

	return calls[0]
}

// okSender answers every request at once.
type okSender struct{}

func (okSender) Send(_ context.Context, _ engine.Request) (engine.Outcome, error) {
	now := time.Now()

	return engine.Outcome{Category: engine.CategorySuccess, SentAt: now, DoneAt: now}, nil
}

// runStepped runs the calls and returns the report; a run that the seq guard
// ended is a run with a report, not a failure.
func runStepped(t *testing.T, calls []engine.Call) engine.Report {
	t.Helper()

	eng, err := engine.New(engine.Options{Calls: calls, Sender: okSender{}, MaxInFlight: 64})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil && !errors.Is(err, engine.ErrSeqOverflow) {
		t.Fatalf("run: %v", err)
	}

	return eng.Report()
}

// Ground: contract — a templated step's body is made per iteration from its own
// data and its own method's message: {{seq}} whole in a number field is the
// number, in a string field the text, {{uuid}} a fresh uuid v4; the step holds
// no ready body beside the Build.
func TestAttachMethods_StepTemplateBuildsSeqAndUuid(t *testing.T) {
	call := attachedStepped(t, steppedConfig(
		map[string]any{"id": "{{uuid}}"},
		tplStep(map[string]any{"id": "{{uuid}}", "note": "pay-{{seq}}", "count": "{{seq}}", "ref": "{{seq}}"}),
	))

	step := call.Next[0]
	if step.Build == nil {
		t.Fatal("the templated step has no Build")
	}
	if step.Payload != nil {
		t.Errorf("the step holds a ready body (%x) beside the Build", step.Payload)
	}

	first := decodeOrder(t, step.Build(7, nil))
	if got := fieldOf(first, "note").String(); got != "pay-7" {
		t.Errorf("note = %q, want pay-7", got)
	}
	if got := fieldOf(first, "count").Int(); got != 7 {
		t.Errorf("count = %d, want 7: a whole {{seq}} in a number field is the number", got)
	}
	if got := fieldOf(first, "ref").String(); got != "7" {
		t.Errorf("ref = %q, want 7: a whole {{seq}} in a string field is the text", got)
	}
	id := fieldOf(first, "id").String()
	if !uuidV4.MatchString(id) {
		t.Errorf("id = %q, not a uuid v4", id)
	}

	if again := fieldOf(decodeOrder(t, step.Build(7, nil)), "id").String(); again == id {
		t.Errorf("two builds gave the same uuid %q: {{uuid}} is new at every build", id)
	}
}

// Ground: contract — a placeholder in a field it does not fit is a config
// error before the start, by the step method's descriptor, naming the step's
// method and the path of the step and of the field.
func TestAttachMethods_StepTemplateInAFieldItDoesNotFitIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps []stepSpec
		path  string
		step  string
	}{
		{"bytes", []stepSpec{tplStep(map[string]any{"payload": "{{seq}}"})}, "data.payload", "steps[0]"},
		{"enum", []stepSpec{tplStep(map[string]any{"kind": "{{seq}}"})}, "data.kind", "steps[0]"},
		{"bool", []stepSpec{tplStep(map[string]any{"flag": "{{seq}}"})}, "data.flag", "steps[0]"},
		{"uuid in an int32", []stepSpec{tplStep(map[string]any{"count": "{{uuid}}"})}, "data.count", "steps[0]"},
		{"a digit before seq in a number", []stepSpec{tplStep(map[string]any{"count": "1{{seq}}"})}, "data.count", "steps[0]"},
		{"a string for a message field", []stepSpec{tplStep(map[string]any{"child": "{{seq}}"})}, "data.child", "steps[0]"},
		{
			"text in a number of a nested message",
			[]stepSpec{tplStep(map[string]any{"child": map[string]any{"n": "a-{{seq}}"}})}, "data.child.n", "steps[0]",
		},
		{
			"uuid in a number of a repeated message",
			[]stepSpec{tplStep(map[string]any{"items": []any{map[string]any{"name": "ok"}, map[string]any{"n": "{{uuid}}"}}})},
			"data.items[1].n", "steps[0]",
		},
		{
			"the second step",
			[]stepSpec{tplStep(map[string]any{"id": "{{uuid}}"}), tplStep(map[string]any{"flag": "{{seq}}"})},
			"data.flag", "steps[1]",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A good placeholder in the step beside it, so that the error cannot be "no placeholders".
			steps := make([]stepSpec, len(tc.steps))
			for k, s := range tc.steps {
				data := map[string]any{"id": "{{uuid}}"}
				for key, v := range s.data {
					data[key] = v
				}
				steps[k] = stepSpec{data: data, templates: s.templates}
			}

			_, err := attachStepped(t, steppedConfig(map[string]any{"id": "{{uuid}}"}, steps...))

			if !errors.Is(err, ErrTemplateField) {
				t.Fatalf("error = %v, want ErrTemplateField", err)
			}
			for _, want := range []string{stepMethods[len(tc.steps)-1], tc.step, tc.path} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

// Ground: contract (amendment 1) — Template.Max is the smallest NON-ZERO limit
// over the call body and every templated step body, 0 only when no body has a
// limit: min with a plain 0 would switch the guard off, and seq 2147483648
// would go into an int32 wrapped.
// Mutation: plain min (0 wins).
func TestAttachMethods_StepTemplateMaxIsTheNarrowestOfCallAndSteps(t *testing.T) {
	const maxUint32 = 4294967295

	str := map[string]any{"note": "{{seq}}"}
	i32 := map[string]any{"count": "{{seq}}"}
	u32 := map[string]any{"u32": "{{seq}}"}
	i64 := map[string]any{"big": "{{seq}}"}
	uuid := map[string]any{"id": "{{uuid}}"}

	for _, tc := range []struct {
		name  string
		call  map[string]any
		steps []stepSpec
		want  uint64
	}{
		{"call int32, step string", i32, []stepSpec{tplStep(str)}, maxInt32},
		{"call string, step int32", str, []stepSpec{tplStep(i32)}, maxInt32},
		{"call int32, step int64", i32, []stepSpec{tplStep(i64)}, maxInt32},
		{"call int64, step int32", i64, []stepSpec{tplStep(i32)}, maxInt32},
		{"call int32, step uint32: the narrower", i32, []stepSpec{tplStep(u32)}, maxInt32},
		{"call uint32, step int32: the narrower", u32, []stepSpec{tplStep(i32)}, maxInt32},
		{"call uint32, step string", u32, []stepSpec{tplStep(str)}, maxUint32},
		{"call string, step uint32", str, []stepSpec{tplStep(u32)}, maxUint32},
		{"call string, step string", str, []stepSpec{tplStep(str)}, 0},
		{"call string, step int64", str, []stepSpec{tplStep(i64)}, 0},
		{"call uuid only, step string", uuid, []stepSpec{tplStep(str)}, 0},
		{"call string, step uuid only", str, []stepSpec{tplStep(uuid)}, 0},
		{"call string, first step string, second int32", str, []stepSpec{tplStep(str), tplStep(i32)}, maxInt32},
		{"call string, first step int32, second string", str, []stepSpec{tplStep(i32), tplStep(str)}, maxInt32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := attachedStepped(t, steppedConfig(tc.call, tc.steps...))

			if got := call.Template.Max; got != tc.want {
				t.Errorf("Max = %d, want %d", got, tc.want)
			}
		})
	}
}

// Ground: contract (amendment 4b) — when the narrowest {{seq}} field is in a
// step, the refusal before the run names that step's method and its place, not
// the call's: the person who reads "CreateOrder holds 2147483647" edits the
// wrong method.
// Mutation: the text names the call's method.
func TestCheckTemplatesFit_NamesTheStepWithTheNarrowestField(t *testing.T) {
	cfg := steppedConfig(
		map[string]any{"note": "{{seq}}"},
		tplStep(map[string]any{"count": "{{seq}}"}),
		tplStep(map[string]any{"id": "{{uuid}}"}),
	)
	// 1e6 rps for 3 hours is 1.08e10 requests, past the int32 of the first step.
	cfg.Load.Calls[0].RPS = 1_000_000
	cfg.Load.Calls[0].Duration = 3 * time.Hour
	call := attachedStepped(t, cfg)

	err := checkedWithin(t, []engine.Call{call})

	if !errors.Is(err, engine.ErrSeqOverflow) {
		t.Fatalf("error = %v, want engine.ErrSeqOverflow", err)
	}
	for _, want := range []string{"PayOrder", "steps[0]", "2147483647"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q: the step with the narrow field, and its limit", err, want)
		}
	}
	if strings.Contains(err.Error(), "ShipOrder") || strings.Contains(err.Error(), "steps[1]") {
		t.Errorf("error %q names a step whose field is not the narrow one", err)
	}
}

// Ground: contract — a narrow field in the call itself still names the call,
// and no step.
func TestCheckTemplatesFit_ANarrowFieldInTheCallNamesNoStep(t *testing.T) {
	cfg := steppedConfig(
		map[string]any{"count": "{{seq}}"},
		tplStep(map[string]any{"note": "{{seq}}"}),
	)
	cfg.Load.Calls[0].RPS = 1_000_000
	cfg.Load.Calls[0].Duration = 3 * time.Hour
	call := attachedStepped(t, cfg)

	err := checkedWithin(t, []engine.Call{call})

	if !errors.Is(err, engine.ErrSeqOverflow) {
		t.Fatalf("error = %v, want engine.ErrSeqOverflow", err)
	}
	if !strings.Contains(err.Error(), "shop.v1.Shop/Order") || strings.Contains(err.Error(), "steps[") {
		t.Errorf("error %q must name the call's method and no step", err)
	}
}

// Ground: contract (amendment 4b) — the same for the line of a run the guard
// ended: it names the step with the narrow field; the JSON seq_overflow.method
// stays the call's method, the one the engine stopped.
// Mutation: the note names the call's method.
func TestReport_SeqOverflowNamesTheStepWithTheNarrowestField(t *testing.T) {
	cfg := steppedConfig(
		map[string]any{"note": "{{seq}}"},
		tplStep(map[string]any{"count": "{{seq}}"}),
	)
	cfg.Load.Calls[0].Duration = time.Second
	call := attachedStepped(t, cfg)
	// The guard is the second layer; a limit of 3 stands in for the 2^31 a test cannot reach.
	call.Template.Max = 3

	report := runStepped(t, []engine.Call{call})

	if report.SeqOverflow == nil {
		t.Fatalf("the guard did not end the run: %+v", report)
	}
	note := strings.Join(seqOverflowNote(report), "\n")
	for _, want := range []string{"PayOrder", "steps[0]", "{{seq}}"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note lacks %q:\n%s", want, note)
		}
	}

	out := writeJSON(t, jsonRunOf(RunReport{Report: report}))
	if got := field(t, out, "seq_overflow", "method"); got != "/"+orderMethod {
		t.Errorf("seq_overflow.method = %v, want /%s: the call's, not the step's", got, orderMethod)
	}
}

// Ground: reference — a fast build of a step is allowed only if it gives the
// bytes of the naive path: substitute, build with RequestBody, Marshal. 1100
// values of seq over a nested, repeated and map body, as for a call.
func TestAttachMethods_StepTemplateBuildEqualsTheNaivePath(t *testing.T) {
	data := map[string]any{
		"id":    "{{uuid}}",
		"note":  "n-{{seq}}-{{seq}}",
		"count": "{{seq}}",
		"big":   "{{seq}}",
		"child": map[string]any{"name": "c-{{seq}}", "n": "{{seq}}"},
		"tags":  []any{"t-{{seq}}", "plain", "{{seq}}"},
		"items": []any{map[string]any{"name": "i-{{seq}}"}, map[string]any{"name": "j", "n": 5}},
		"attrs": map[string]any{"k": "v-{{seq}}", "k2": "x", "k3": "{{seq}}"},
		"ref":   "{{seq}}",
	}
	call := attachedStepped(t, steppedConfig(map[string]any{"id": "{{uuid}}"}, tplStep(data)))
	build := call.Next[0].Build
	if build == nil {
		t.Fatal("the templated step has no Build")
	}
	desc := orderMessage(t)

	seqs := []uint64{maxInt32}
	for seq := uint64(1); seq <= 1100; seq++ {
		seqs = append(seqs, seq)
	}

	for _, seq := range seqs {
		got := build(seq, nil)

		uuid := fieldOf(decodeOrder(t, got), "id").String()
		if !uuidV4.MatchString(uuid) {
			t.Fatalf("seq %d: id = %q, not a uuid", seq, uuid)
		}

		want, err := RequestBody(desc, substituted(data, seq, uuid))
		if err != nil {
			t.Fatalf("seq %d: the naive path: %v", seq, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("seq %d: built %x, the naive path gives %x", seq, got, want)
		}
	}
}

// Ground: contract — a step that is not templated keeps its body: the encoded
// data as in 3a (braces of an explicit templates: false are text), no Build,
// even next to a templated step of the same call.
// Mutation: a plain step is handed the Build of a template.
func TestAttachMethods_AStepWithoutTemplatesKeepsItsPayload(t *testing.T) {
	braces := map[string]any{"note": "{{seq}}"}
	off := stepSpec{data: braces, templates: templatesOff()}
	plain := stepSpec{data: map[string]any{"id": "a-1", "count": 7}}

	call := attachedStepped(t, steppedConfig(
		map[string]any{"id": "{{uuid}}"},
		plain, off, tplStep(map[string]any{"note": "t-{{seq}}"}),
	))

	wantBraces, err := RequestBody(orderMessage(t), braces)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range [][]byte{wantOrderBody(t, "a-1", 7), wantBraces} {
		if call.Next[k].Build != nil {
			t.Errorf("Next[%d].Build is set: a step without templates is not built per iteration", k)
		}
		if !bytes.Equal(call.Next[k].Payload, want) {
			t.Errorf("Next[%d].Payload = %x, want %x", k, call.Next[k].Payload, want)
		}
	}
	if call.Next[2].Build == nil {
		t.Error("Next[2].Build is nil: the templated step beside the plain ones has none")
	}
}

var dataLine = regexp.MustCompile(`(?m)^  data: templated, (.*) \(last seq (\d+), ramp and warm-up counted\)$`)

// Ground: contract — the templates line and JSON of a method list every
// placeholder its call and its templated steps use, once each, sorted; a plain
// step's braces are text and add nothing. Through the whole chain: config,
// attach, a short run, the report.
// Mutation: the union is the call's placeholders only.
func TestReport_TemplatesLineIsTheUnionOfCallAndSteps(t *testing.T) {
	seqData := map[string]any{"note": "{{seq}}"}
	uuidData := map[string]any{"id": "{{uuid}}"}

	for _, tc := range []struct {
		name  string
		call  map[string]any
		steps []stepSpec
		want  string
	}{
		{"call uuid, step seq", uuidData, []stepSpec{tplStep(seqData)}, "seq uuid"},
		{"call seq, step uuid", seqData, []stepSpec{tplStep(uuidData)}, "seq uuid"},
		{"call with none of its own, step seq", map[string]any{"note": "x"}, []stepSpec{tplStep(seqData)}, "seq"},
		{"call seq, step seq", seqData, []stepSpec{tplStep(seqData)}, "seq"},
		{"call uuid, first step seq, second uuid", uuidData, []stepSpec{tplStep(seqData), tplStep(uuidData)}, "seq uuid"},
		{
			"a plain step's braces are text", uuidData,
			[]stepSpec{{data: seqData, templates: templatesOff()}}, "uuid",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := attachedStepped(t, steppedConfig(tc.call, tc.steps...))
			report := runStepped(t, []engine.Call{call})

			var out strings.Builder
			PrintReport(&out, "localhost:50051", RunReport{Report: report})

			lines := dataLine.FindAllStringSubmatch(out.String(), -1)
			if len(lines) != 1 {
				t.Fatalf("%d templates lines, want 1:\n%s", len(lines), out.String())
			}
			if got := lines[0][1]; got != tc.want {
				t.Errorf("text: placeholders = %q, want %q", got, tc.want)
			}

			js := writeJSON(t, jsonRunOf(RunReport{Report: report}))
			for _, m := range field(t, js, "methods").([]any) {
				if field(t, m, "method") != "/"+orderMethod {
					continue
				}

				got, _ := field(t, m, "templates", "placeholders").([]any)
				parts := make([]string, len(got))
				for i, p := range got {
					parts[i], _ = p.(string)
				}
				if joined := strings.Join(parts, " "); joined != tc.want {
					t.Errorf("JSON: placeholders = %q, want %q", joined, tc.want)
				}
			}
		})
	}
}

// Ground: concurrency — the workers of every iteration in flight build their
// step's body at once, from one builder: each body carries the seq it was asked
// for and a uuid of its own, none is mixed with another's (a shared message or
// buffer written without the lock). Meaningful under -race.
// Mutation: the builder's lock is dropped.
func TestAttachMethods_StepTemplateBuildIsSafeInParallel(t *testing.T) {
	call := attachedStepped(t, steppedConfig(
		map[string]any{"id": "{{uuid}}"},
		tplStep(map[string]any{"id": "{{uuid}}", "note": "n-{{seq}}", "count": "{{seq}}"}),
	))
	build := call.Next[0].Build
	if build == nil {
		t.Fatal("the templated step has no Build")
	}

	const workers, perWorker = 64, 50

	type built struct {
		seq  uint64
		body []byte
	}

	results := make(chan built, workers*perWorker)

	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range perWorker {
				seq := uint64(w*perWorker + i + 1)
				results <- built{seq: seq, body: build(seq, nil)}
			}
		})
	}
	wg.Wait()
	close(results)

	ids := make(map[string]bool, workers*perWorker)
	bad := 0

	for r := range results {
		msg := decodeOrder(t, r.body)
		id := fieldOf(msg, "id").String()

		if note, want := fieldOf(msg, "note").String(), "n-"+strconv.FormatUint(r.seq, 10); note != want || uint64(fieldOf(msg, "count").Int()) != r.seq || !uuidV4.MatchString(id) || ids[id] {
			bad++
			if bad <= 3 {
				t.Errorf("seq %d: body has note %q, count %d, id %q (repeated: %v)", r.seq, note, fieldOf(msg, "count").Int(), id, ids[id])
			}
		}
		ids[id] = true
	}

	if bad > 3 {
		t.Errorf("%d bodies in all are wrong", bad)
	}
}

// The step builder under every worker at once, as a run with many iterations
// in flight uses it (one builder per step, one lock inside). Run with -cpu 1,8:
// the per-op time at 8 against 1 is the cost of the lock.
func BenchmarkStepTemplateBuild_Parallel(b *testing.B) {
	call := attachedStepped(b, steppedConfig(
		map[string]any{"id": "{{uuid}}"},
		tplStep(benchBody),
	))
	build := call.Next[0].Build
	if build == nil {
		b.Fatal("the templated step has no Build")
	}

	b.ReportAllocs()

	var seq atomic.Uint64

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = build(seq.Add(1), nil)
		}
	})
}

// Ground: contract (spec 5) — a templated call whose own data has no
// placeholder, the placeholders being in its step, still has a Template whose
// Build is the call's data, the same bytes at every seq (the empty message for
// no data); the limit is 0 and the placeholders are the step's only. A Build
// that returned nil would send step 1 an empty request, and the rest of the
// tests would not notice.
// Mutation: the Build of the call returns nil.
func TestAttachMethods_TemplatedCallWithoutOwnPlaceholdersSendsItsData(t *testing.T) {
	fixed := map[string]any{"id": "fixed"}
	wantFixed, err := RequestBody(orderMessage(t), fixed)
	if err != nil {
		t.Fatal(err)
	}
	if len(wantFixed) == 0 {
		t.Fatal("the expected body is empty: the test is not set up")
	}

	for _, tc := range []struct {
		name string
		data map[string]any
		want []byte
	}{
		{"data without placeholders", fixed, wantFixed},
		{"no data", nil, nil},
		{"empty data", map[string]any{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := attachedStepped(t, steppedConfig(tc.data, tplStep(map[string]any{"note": "n-{{seq}}"})))

			if call.Template.Build == nil {
				t.Fatal("the call's Template has no Build: step 1 would have no body")
			}
			for _, seq := range []uint64{1, 2} {
				if got := call.Template.Build(seq); !bytes.Equal(got, tc.want) {
					t.Errorf("Build(%d) = %x, want %x: the call's own data, the same at every seq", seq, got, tc.want)
				}
			}
			if call.Template.Max != 0 {
				t.Errorf("Max = %d, want 0: the call's body has no number field a seq goes into", call.Template.Max)
			}
			if want := []string{"seq"}; !slices.Equal(call.Template.Placeholders, want) {
				t.Errorf("Placeholders = %q, want %q: the steps' only", call.Template.Placeholders, want)
			}
			if call.Payload != nil || call.Payloads != nil {
				t.Errorf("the call holds a ready body beside the Template")
			}
		})
	}
}
