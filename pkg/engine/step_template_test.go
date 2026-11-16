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
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Step templates (v0.9 unit 3b). Compile stubs: engine.Step has Build and
// engine.Request has Seq, and nothing reads or sets them yet, so every test that
// expects a step body or a seq on a request is red until the code. The guard
// tests (plain steps, plain calls) are green now and stay so.

// stepTimeout is the timeout of every call and step here: short, so that the
// in-flight budget of a few hundred iterations a second fits MaxInFlight.
const stepTimeout = 100 * time.Millisecond

// tagged is the Build of a step: the tag and the seq it was asked for, as text,
// so a test reads both from the body the sender got.
func tagged(tag string) func(uint64) []byte {
	return func(seq uint64) []byte { return []byte(tag + strconv.FormatUint(seq, 10)) }
}

// taggedTop is tagged that also keeps the largest seq it was asked for.
func taggedTop(tag string, top *atomic.Uint64) func(uint64) []byte {
	build := tagged(tag)

	return func(seq uint64) []byte {
		for {
			old := top.Load()
			if seq <= old || top.CompareAndSwap(old, seq) {
				break
			}
		}

		return build(seq)
	}
}

// templatedScenario is a call of the scenario "flow" with a template on its own
// body and the steps after it.
func templatedScenario(tpl *Template, next []Step, stages ...Stage) Call {
	return Call{Method: "a/One", Scenario: "flow", Template: tpl, Timeout: stepTimeout, Next: next, Stages: stages}
}

// stepCall is a call the iteration sender saw: iteration says which iteration
// it belongs to, so that the steps of one iteration can be read together.
type stepCall struct {
	method    string
	payload   string
	seq       uint64
	iteration int64
}

// iterationSender answers every call and keeps what it was given. Each call
// ends at a moment of its own, and the step after it is scheduled at exactly
// that moment, so a step is matched to its iteration without the engine
// helping: the iteration of a step is that of the call whose end it starts at.
type iterationSender struct {
	first string
	hold  func(context.Context) error

	mu    sync.Mutex
	last  time.Time
	owner map[int64]int64
	calls []stepCall
}

func newIterationSender(first string) *iterationSender {
	return &iterationSender{first: first, owner: map[int64]int64{}}
}

func (s *iterationSender) Send(ctx context.Context, req Request) (Outcome, error) {
	if req.Method == s.first && s.hold != nil {
		if err := s.hold(ctx); err != nil {
			return Outcome{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	end := time.Now()
	if !end.After(s.last) {
		end = s.last.Add(time.Nanosecond)
	}

	s.last = end
	id := end.UnixNano()

	iteration := id
	if req.Method != s.first {
		iteration = s.owner[req.ScheduledAt.UnixNano()]
	}

	s.owner[id] = iteration
	s.calls = append(s.calls, stepCall{method: req.Method, payload: string(req.Payload), seq: req.Seq, iteration: iteration})

	return Outcome{Category: CategorySuccess, SentAt: end, DoneAt: end}, nil
}

// iterations are the calls seen, grouped by iteration, each group in the order
// its calls were made; the groups follow the order their step 1 ended in.
func (s *iterationSender) iterations() [][]stepCall {
	s.mu.Lock()
	defer s.mu.Unlock()

	var order []int64

	groups := map[int64][]stepCall{}

	for _, c := range s.calls {
		if _, ok := groups[c.iteration]; !ok {
			order = append(order, c.iteration)
		}

		groups[c.iteration] = append(groups[c.iteration], c)
	}

	out := make([][]stepCall, 0, len(order))
	for _, id := range order {
		out = append(out, groups[id])
	}

	return out
}

// payloadsOf are the bodies of a group, in the order of its calls.
func payloadsOf(group []stepCall) []string {
	out := make([]string, 0, len(group))
	for _, c := range group {
		out = append(out, c.payload)
	}

	return out
}

// Ground: contract — one seq per iteration: iteration n sends n in step 1 and n
// in every templated step, the next iteration n+1, and the counter is the
// call's: it moves once per iteration, never per step. Step 1's request says
// which seq it was built with.
// Mutations: iterate builds with first.Seq+1; a fresh seq per step; newRequest
// leaves req.Seq zero.
func TestEngine_ScenarioStepsGetTheIterationsSeq(t *testing.T) {
	const iterations = 6

	tpl := newCountingTemplate(0)
	sender := newIterationSender("a/One")

	eng, err := New(Options{
		Calls: []Call{templatedScenario(tpl.Template, []Step{
			{Method: "a/Two", Timeout: stepTimeout, Build: tagged("two-")},
			{Method: "a/Three", Timeout: stepTimeout, Build: tagged("three-")},
		}, atRate(200, iterations))},
		Sender:      sender,
		MaxInFlight: 256,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	groups := sender.iterations()
	if len(groups) != iterations {
		t.Fatalf("the sender saw %d iterations, want %d", len(groups), iterations)
	}

	seen := map[uint64]int{}

	for _, group := range groups {
		n, err := strconv.ParseUint(group[0].payload, 10, 64)
		if err != nil {
			t.Fatalf("step 1 body = %q, want its seq", group[0].payload)
		}

		seen[n]++

		sn := strconv.FormatUint(n, 10)
		if got, want := payloadsOf(group), []string{sn, "two-" + sn, "three-" + sn}; !slices.Equal(got, want) {
			t.Errorf("the iteration with seq %d sent %q, want %q: every step takes the iteration's seq", n, got, want)
		}
		if group[0].seq != n {
			t.Errorf("the request of step 1 carried Seq %d, want %d: the steps are built from it", group[0].seq, n)
		}
	}

	for n := uint64(1); n <= iterations; n++ {
		if seen[n] != 1 {
			t.Errorf("seq %d went to %d iterations, want 1", n, seen[n])
		}
	}
	if got := tpl.Counter.Handed(); got != iterations {
		t.Errorf("counter = %d, want %d: it moves once per iteration, the steps never move it", got, iterations)
	}
	if m, ok := methodRow(eng.Report(), "a/One"); !ok || m.Templates == nil || m.Templates.LastSeq != iterations {
		t.Errorf("a/One templates = %+v (found %v), want LastSeq %d", m.Templates, ok, iterations)
	}
}

// Ground: contract — a plain step keeps the payload it has, between two
// templated ones and after a templated call; Build is the step's own.
// Mutation: a plain step handed the Build of a template.
func TestEngine_ScenarioPlainStepKeepsItsPayloadNextToATemplatedStep(t *testing.T) {
	const iterations = 4

	tpl := newCountingTemplate(0)
	sender := newIterationSender("a/One")

	eng, err := New(Options{
		Calls: []Call{templatedScenario(tpl.Template, []Step{
			{Method: "a/Two", Timeout: stepTimeout, Payload: []byte("plain-two")},
			{Method: "a/Three", Timeout: stepTimeout, Build: tagged("three-")},
			{Method: "a/Four", Timeout: stepTimeout, Payload: []byte("plain-four")},
		}, atRate(200, iterations))},
		Sender:      sender,
		MaxInFlight: 256,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	groups := sender.iterations()
	if len(groups) != iterations {
		t.Fatalf("the sender saw %d iterations, want %d", len(groups), iterations)
	}

	for _, group := range groups {
		sn := group[0].payload
		if got, want := payloadsOf(group), []string{sn, "plain-two", "three-" + sn, "plain-four"}; !slices.Equal(got, want) {
			t.Errorf("an iteration sent %q, want %q: the plain steps keep their payload", got, want)
		}
	}
}

// Ground: contract — a templated call whose steps are all plain sends them as
// before: nothing of the template reaches a step. Guard, green before and after.
func TestEngine_ScenarioPlainStepsOfATemplatedCallAreUntouched(t *testing.T) {
	tpl := newCountingTemplate(0)
	sender := newIterationSender("a/One")

	eng, err := New(Options{
		Calls: []Call{templatedScenario(tpl.Template, []Step{
			{Method: "a/Two", Timeout: stepTimeout, Payload: []byte("plain-two")},
		}, atRate(200, 3))},
		Sender:      sender,
		MaxInFlight: 256,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, group := range sender.iterations() {
		if got := payloadsOf(group); len(got) != 2 || got[1] != "plain-two" {
			t.Errorf("an iteration sent %q, want its step to carry plain-two", got)
		}
	}
}

// Ground: contract — an iteration the dispatcher built and could not hand out
// spends no seq: the run after the stop starts at the number it left. The step
// bodies are made by the worker when the iteration runs, so an unsent iteration
// builds none, and each request that did go out says its seq.
// Mutation: a number is spent on a built but unsent iteration; step bodies are
// built in newRequest.
func TestEngine_ScenarioTemplatedStepUnsentIterationSpendsNoSeq(t *testing.T) {
	var stepBuilds atomic.Int64

	built := make(chan struct{}, 16)
	tpl := NewTemplate([]string{"seq"}, 0, func(seq uint64) []byte {
		built <- struct{}{}

		return seqBody(seq)
	})
	call := templatedScenario(tpl, []Step{{Method: "a/Two", Timeout: stepTimeout, Build: func(seq uint64) []byte {
		stepBuilds.Add(1)

		return seqBody(seq)
	}}}, atRate(100, 50))

	// Nobody reads the third request: the dispatcher builds it and waits.
	out := make(chan Request)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- NewDispatcher([]Call{call}).Run(ctx, out) }()

	for i := 1; i <= 2; i++ {
		select {
		case req := <-out:
			if got, want := string(req.Payload), strconv.Itoa(i); got != want {
				t.Fatalf("request %d body = %q, want %q", i, got, want)
			}
			if req.Seq != uint64(i) {
				t.Fatalf("request %d carries Seq %d, want %d", i, req.Seq, i)
			}
			if req.scenario == nil || len(req.scenario.Next) != 1 {
				t.Fatalf("request %d carries the call %+v, want one step", i, req.scenario)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("request %d did not arrive", i)
		}
	}

	for i := 0; i < 3; i++ {
		select {
		case <-built:
		case <-time.After(2 * time.Second):
			t.Fatalf("the dispatcher built %d requests, want the third built and waiting", i)
		}
	}

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the dispatcher did not stop")
	}

	if n := tpl.Counter.Handed(); n != 2 {
		t.Fatalf("counter = %d after 2 iterations were handed out and a third was built, want 2", n)
	}
	if n := stepBuilds.Load(); n != 0 {
		t.Fatalf("the dispatcher built %d step bodies, want 0: the worker builds them when the iteration runs", n)
	}

	next := make(chan Request, 4)
	if err := NewDispatcher([]Call{templatedScenario(tpl, nil, atRate(200, 1))}).Run(t.Context(), next); err != nil {
		t.Fatalf("second run: %v", err)
	}
	close(next)

	req, ok := <-next
	if !ok || string(req.Payload) != "3" || req.Seq != 3 {
		t.Fatalf("the run after the stop starts with %q (Seq %d), want 3: a seq was spent on the unsent iteration", req.Payload, req.Seq)
	}
}

// Ground: contract — a run stops before the first iteration whose seq does not
// fit the narrowest field of any templated body (the limit is the call's
// Template.Max, the smallest over the call and its steps, set by the CLI): the
// iterations that fit are whole, no step is built with a seq above the limit,
// and the error names the call.
// Mutation: Max taken from the call's body only (the CLI side, see the CLI test).
func TestEngine_ScenarioSeqOverflowUsesTheNarrowestStepLimit(t *testing.T) {
	var stepTop atomic.Uint64

	tpl := newCountingTemplate(3)
	sender := newIterationSender("a/One")

	eng, err := New(Options{
		Calls: []Call{templatedScenario(tpl.Template, []Step{
			{Method: "a/Two", Timeout: stepTimeout, Build: taggedTop("two-", &stepTop)},
			{Method: "a/Three", Timeout: stepTimeout, Build: taggedTop("three-", &stepTop)},
		}, atRate(200, 6))},
		Sender:      sender,
		MaxInFlight: 256,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	runErr := eng.Run(t.Context())
	if !errors.Is(runErr, ErrSeqOverflow) {
		t.Fatalf("run error = %v, want ErrSeqOverflow", runErr)
	}
	if !strings.Contains(runErr.Error(), "a/One") || !strings.Contains(runErr.Error(), "3") {
		t.Errorf("error %q names neither the call nor the limit 3", runErr)
	}

	groups := sender.iterations()
	if len(groups) != 3 {
		t.Fatalf("the sender saw %d iterations, want the 3 that fit", len(groups))
	}

	for _, group := range groups {
		sn := group[0].payload
		if got, want := payloadsOf(group), []string{sn, "two-" + sn, "three-" + sn}; !slices.Equal(got, want) {
			t.Errorf("an iteration sent %q, want %q: an iteration that fits is whole", got, want)
		}
	}
	if top := stepTop.Load(); top > 3 || top == 0 {
		t.Errorf("the steps were built up to seq %d, want 3: none above the limit, and built", top)
	}

	if got := eng.Report().SeqOverflow; got == nil || got.Method != "a/One" || got.Limit != 3 {
		t.Errorf("SeqOverflow = %+v, want the call a/One and the limit 3", got)
	}
}

// Ground: contract — a request handed out by a templated call says the seq of
// its body, also for a call run alone; a plain call's requests say nothing (the
// zero), and the plain path has no seq to take.
// Mutation: newRequest leaves req.Seq zero.
func TestScheduler_TemplatedRequestCarriesItsSeq(t *testing.T) {
	tpl := newCountingTemplate(0)
	out := make(chan Request, 16)

	if err := NewScheduler(templateCall("a.B/One", tpl.Template, atRate(200, 4))).Run(t.Context(), out); err != nil {
		t.Fatalf("run: %v", err)
	}
	close(out)

	var got []uint64
	for req := range out {
		got = append(got, req.Seq)
		if want := strconv.FormatUint(req.Seq, 10); string(req.Payload) != want {
			t.Errorf("body = %q with Seq %d, want the body built with that seq", req.Payload, req.Seq)
		}
	}

	if want := []uint64{1, 2, 3, 4}; !slices.Equal(got, want) {
		t.Errorf("Seq of the requests = %v, want %v", got, want)
	}
}

// Ground: hot path — a call without templates builds a request that says no
// seq: the plain branch does not take one. Guard, green before and after.
func TestScheduler_PlainRequestCarriesNoSeq(t *testing.T) {
	plain := NewScheduler(Call{Method: "a.B/One", Payload: []byte("x"), Timeout: time.Second})
	data := NewScheduler(datasetCall(3, NewRecordCounter(), atRate(10, 3)))
	at := time.Now()

	if req := plain.newRequest(at); req.Seq != 0 {
		t.Errorf("a plain request carries Seq %d, want 0", req.Seq)
	}
	if req := data.newRequest(at); req.Seq != 0 {
		t.Errorf("a dataset request carries Seq %d, want 0", req.Seq)
	}
}

// Ground: concurrency — many iterations of one templated call in flight at once
// (the barrier holds all of them inside step 1 until every one has begun), each
// step body made from its own iteration's seq: no body is built with another
// iteration's seq, none is lost or shared. Seqs are not 1..n, so a counter the
// pool kept of its own would not look right. Run under -race.
// Mutation: iterate builds with a shared, not per-iteration, seq.
func TestWorkerPool_TemplatedStepBodiesAreNotMixedAcrossIterations(t *testing.T) {
	const iterations = 128

	var entered atomic.Int64

	gate := make(chan struct{})
	sender := newIterationSender("a/One")
	sender.hold = func(ctx context.Context) error {
		if entered.Add(1) == iterations {
			close(gate)
		}

		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("hold: %w", ctx.Err())
		case <-time.After(10 * time.Second):
			return nil
		}
	}

	steps := []Step{
		{Method: "a/Two", Timeout: time.Minute, Build: tagged("two-")},
		{Method: "a/Three", Timeout: time.Minute, Build: tagged("three-")},
	}

	_, err := runPool(t.Context(), sender, 2*iterations, func(in chan<- Request) {
		tick := time.Now()

		for i := 1; i <= iterations; i++ {
			req := scenarioReq(tick, "flow", Step{Method: "a/One", Timeout: time.Minute}, steps...)
			req.Seq = uint64(10 * i)
			req.Payload = []byte("first-" + strconv.FormatUint(req.Seq, 10))
			in <- req
		}
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	select {
	case <-gate:
	default:
		t.Fatalf("only %d iterations were in flight together, want all %d", entered.Load(), iterations)
	}

	groups := sender.iterations()
	if len(groups) != iterations {
		t.Fatalf("the sender saw %d iterations, want %d", len(groups), iterations)
	}

	seen := map[string]bool{}
	bad := 0

	for _, group := range groups {
		sn, ok := strings.CutPrefix(group[0].payload, "first-")
		if !ok {
			t.Fatalf("step 1 body = %q, want first-<seq>", group[0].payload)
		}

		if got, want := payloadsOf(group), []string{group[0].payload, "two-" + sn, "three-" + sn}; !slices.Equal(got, want) {
			bad++
			if bad <= 3 {
				t.Errorf("the iteration with seq %s sent %q, want %q: a step built with another iteration's seq", sn, got, want)
			}
		}

		if seen[sn] {
			t.Errorf("seq %s went to two iterations", sn)
		}

		seen[sn] = true
	}

	if bad > 0 {
		t.Errorf("%d of %d iterations sent a step body that is not its own", bad, iterations)
	}
}
