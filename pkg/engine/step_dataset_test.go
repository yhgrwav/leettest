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
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Step datasets (v0.9 unit 3c). Compile stubs: engine.Step has Payloads and
// Dataset, engine.Request has StepRows and ErrStepDatasetOnce exists, and
// nothing reads, sets, moves or reports them yet, so every test that expects a
// row, a moved counter, a report row or an error is red until the code. The
// guard tests (a call without dataset steps) are green now and stay so.

// stepBodies are the records of a step's file: "<tag><row>".
func stepBodies(tag string, n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = []byte(tag + strconv.Itoa(i))
	}

	return out
}

// datasetStep is a step with n records tagged tag, and counter as its own
// position (nil: the run's).
func datasetStep(method, tag string, n int, counter *RecordCounter) Step {
	return Step{
		Method: method, Timeout: stepTimeout, Payloads: stepBodies(tag, n),
		Dataset: &DatasetRef{File: "data/" + tag + ".jsonl", SHA256: "sha-" + tag, Counter: counter},
	}
}

// stepDatasetCall is a call of the scenario "flow" with a plain body and next.
func stepDatasetCall(next []Step, stages ...Stage) Call {
	return Call{Method: "a/One", Scenario: "flow", Payload: []byte("first"), Timeout: stepTimeout, Next: next, Stages: stages}
}

// mover is one of the two goroutines that hand requests out: the dispatcher of
// the engine and the library's Scheduler.Run.
type mover struct {
	name string
	run  func(ctx context.Context, call Call, out chan<- Request) error
}

var movers = []mover{
	{"Dispatcher", func(ctx context.Context, call Call, out chan<- Request) error {
		return NewDispatcher([]Call{call}).Run(ctx, out)
	}},
	{"Scheduler", func(ctx context.Context, call Call, out chan<- Request) error {
		return NewScheduler(call).Run(ctx, out)
	}},
}

// handedBy is every request the mover hands out for call, to the end of its plan.
func handedBy(t *testing.T, m mover, call Call) []Request {
	t.Helper()

	out := make(chan Request, 256)
	if err := m.run(t.Context(), call, out); err != nil {
		t.Fatalf("%s run: %v", m.name, err)
	}
	close(out)

	var got []Request
	for req := range out {
		got = append(got, req)
	}

	return got
}

// rowsOf is the row step k (an index into Next) takes in each request; -1 where
// the request carries none.
func rowsOf(reqs []Request, k int) []int {
	rows := make([]int, len(reqs))
	for i := range reqs {
		rows[i] = -1
		if k < len(reqs[i].StepRows) {
			rows[i] = int(reqs[i].StepRows[k])
		}
	}

	return rows
}

// Ground: contract — the order of a step's records is the dataset's whole
// promise, as for a call: iteration i takes row i of its step's file, wrapping,
// and the step's counter is what moved. The row is read before the counter
// moves, so a row read after handedOut (i+1) is red.
// Mutations: the row is read after handedOut; the row is taken in the worker.
func stepRowIsTheIterationsRow(t *testing.T, m mover) {
	t.Helper()

	counter := NewRecordCounter()
	call := stepDatasetCall([]Step{datasetStep("a/Two", "two-", 3, counter)}, atRate(1000, 7))

	got := rowsOf(handedBy(t, m, call), 0)
	if want := []int{0, 1, 2, 0, 1, 2, 0}; !slices.Equal(got, want) {
		t.Errorf("%s: rows of the step = %v, want %v: iteration i takes row i, wrapping", m.name, got, want)
	}
	if n := counter.Handed(); n != 7 {
		t.Errorf("%s: step counter = %d, want 7", m.name, n)
	}
}

func TestDispatcher_StepRowIsTheIterationsRow(t *testing.T) {
	stepRowIsTheIterationsRow(t, movers[0])
}

func TestScheduler_StepRowIsTheIterationsRow(t *testing.T) {
	stepRowIsTheIterationsRow(t, movers[1])
}

// Ground: contract — random order of a step is the call's: the same seed and n
// give the call's pinned lists (a seed written in a report must give the same
// order in every version), each pass a fresh permutation of the rows.
// Mutation: random order without a fresh permutation per pass.
func stepRandomOrderFollowsTheGoldens(t *testing.T, m mover) {
	t.Helper()

	for _, tc := range []struct {
		seed uint64
		want []int
	}{{goldenSeed, golden42}, {7, golden7}} {
		call := stepDatasetCall([]Step{datasetStep("a/Two", "two-", 5, NewShuffledCounter(5, tc.seed))}, atRate(1000, 15))

		got := rowsOf(handedBy(t, m, call), 0)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: seed %d: rows of the step = %v, want the golden %v", m.name, tc.seed, got, tc.want)
		}

		for pass := 0; pass+5 <= len(got); pass += 5 {
			seen := slices.Clone(got[pass : pass+5])
			slices.Sort(seen)

			if want := []int{0, 1, 2, 3, 4}; !slices.Equal(seen, want) {
				t.Errorf("%s: seed %d: pass %d = %v, want each of 0..4 once", m.name, tc.seed, pass/5, got[pass:pass+5])
			}
		}
	}
}

func TestDispatcher_StepRandomOrderFollowsTheGoldens(t *testing.T) {
	stepRandomOrderFollowsTheGoldens(t, movers[0])
}

func TestScheduler_StepRandomOrderFollowsTheGoldens(t *testing.T) {
	stepRandomOrderFollowsTheGoldens(t, movers[1])
}

// Ground: contract — every dataset step has a counter of its own, and the call's
// is another one: three counters over the same iterations move by one each per
// iteration, the rows follow each file's length, and the call's own body is
// still its own row.
// Mutation: per-step counters shared with the call's counter.
func stepCountersAreIndependent(t *testing.T, m mover) {
	t.Helper()

	var (
		callCounter = NewRecordCounter()
		three       = NewRecordCounter()
		two         = NewRecordCounter()
	)

	call := stepDatasetCall([]Step{
		{Method: "a/Plain", Timeout: stepTimeout, Payload: []byte("plain")},
		datasetStep("a/Three", "three-", 3, three),
		datasetStep("a/Two", "two-", 2, two),
	}, atRate(1000, 7))
	call.Payload = nil
	call.Payloads = recordsOf(5)
	call.Dataset = &DatasetRef{File: "data/call.jsonl", Counter: callCounter}

	reqs := handedBy(t, m, call)

	if got, want := rowsOf(reqs, 1), []int{0, 1, 2, 0, 1, 2, 0}; !slices.Equal(got, want) {
		t.Errorf("%s: rows of the 3-record step = %v, want %v", m.name, got, want)
	}
	if got, want := rowsOf(reqs, 2), []int{0, 1, 0, 1, 0, 1, 0}; !slices.Equal(got, want) {
		t.Errorf("%s: rows of the 2-record step = %v, want %v", m.name, got, want)
	}

	own := make([]int, len(reqs))
	for i := range reqs {
		own[i] = recordNumber(reqs[i])
	}

	if want := []int{0, 1, 2, 3, 4, 0, 1}; !slices.Equal(own, want) {
		t.Errorf("%s: records of the call itself = %v, want %v", m.name, own, want)
	}

	for name, c := range map[string]*RecordCounter{"call": callCounter, "3-record step": three, "2-record step": two} {
		if n := c.Handed(); n != 7 {
			t.Errorf("%s: %s counter = %d, want 7: one move per handed iteration, none for another counter", m.name, name, n)
		}
	}
}

func TestDispatcher_StepCountersAreIndependent(t *testing.T) {
	stepCountersAreIndependent(t, movers[0])
}

func TestScheduler_StepCountersAreIndependent(t *testing.T) {
	stepCountersAreIndependent(t, movers[1])
}

// stepCounterMovesOncePerHandedIteration hands two iterations out on an
// unbuffered channel, lets the mover build a third that nobody takes, and stops
// it there. The template's Build says when each request has been built, so the
// third is known to be built and waiting.
func stepCounterMovesOncePerHandedIteration(t *testing.T, m mover) {
	t.Helper()

	const handed = 2

	built := make(chan struct{}, 16)
	tpl := NewTemplate([]string{"seq"}, 0, func(seq uint64) []byte {
		built <- struct{}{}

		return seqBody(seq)
	})

	two, three := NewRecordCounter(), NewRecordCounter()
	call := stepDatasetCall([]Step{datasetStep("a/Two", "two-", 3, two), datasetStep("a/Three", "three-", 3, three)}, atRate(100, 50))
	call.Payload, call.Template = nil, tpl

	out := make(chan Request)
	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	done := make(chan error, 1)
	go func() { done <- m.run(ctx, call, out) }()

	for i := 0; i < handed; i++ {
		select {
		case req := <-out:
			if got := rowsOf([]Request{req}, 0)[0]; got != i {
				t.Fatalf("%s: request %d takes row %d of step 1, want %d", m.name, i, got, i)
			}
			if got := rowsOf([]Request{req}, 1)[0]; got != i {
				t.Fatalf("%s: request %d takes row %d of step 2, want %d", m.name, i, got, i)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: request %d did not arrive", m.name, i)
		}
	}

	for i := 0; i < handed+1; i++ {
		select {
		case <-built:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: %d requests built, want %d: the last built and waiting", m.name, i, handed+1)
		}
	}

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not stop", m.name)
	}

	if n := two.Handed(); n != handed {
		t.Errorf("%s: counter of step 1 = %d after %d iterations were handed out and a third was built, want %d", m.name, n, handed, handed)
	}
	if n := three.Handed(); n != handed {
		t.Errorf("%s: counter of step 2 = %d, want %d: one move per iteration, not one per step", m.name, n, handed)
	}
	if n := tpl.Counter.Handed(); n != handed {
		t.Errorf("%s: seq counter = %d, want %d", m.name, n, handed)
	}

	// The run after the stop goes on from the rows the first one left: the
	// built, unsent iteration spent none.
	next := make(chan Request, 4)
	if err := m.run(t.Context(), stepDatasetCallWith(call, atRate(200, 1)), next); err != nil {
		t.Fatalf("%s second run: %v", m.name, err)
	}
	close(next)

	req, ok := <-next
	if !ok {
		t.Fatalf("%s: the second run handed nothing out", m.name)
	}
	if got := rowsOf([]Request{req}, 0)[0]; got != handed%3 {
		t.Errorf("%s: the run after the stop starts at row %d of step 1, want %d: a row was spent on the unsent iteration", m.name, got, handed%3)
	}
	if got := rowsOf([]Request{req}, 1)[0]; got != handed%3 {
		t.Errorf("%s: the run after the stop starts at row %d of step 2, want %d", m.name, got, handed%3)
	}
}

// stepDatasetCallWith is call over other stages.
func stepDatasetCallWith(call Call, stages ...Stage) Call {
	call.Stages = stages

	return call
}

// Ground: contract — a step's counter moves once per iteration the dispatcher
// has handed out, after it has left, and stands on one that was built and not
// handed out: the report's used is what went out, and a row built and never
// sent is not spent.
// Mutations: the counter moves per step sent; the counter moves only in
// Dispatcher.Run (the Scheduler test); the counter moves before the send.
func TestDispatcher_StepCounterMovesOncePerHandedIteration(t *testing.T) {
	stepCounterMovesOncePerHandedIteration(t, movers[0])
}

// Ground: contract — the same as for the dispatcher: Scheduler.Run is the
// library's mover and has its own handedOut.
// Mutation: the counter moves only in Dispatcher.Run.
func TestScheduler_StepCounterMovesOncePerHandedIteration(t *testing.T) {
	stepCounterMovesOncePerHandedIteration(t, movers[1])
}

// Ground: hot path — a call without dataset steps hands out requests that carry
// no rows: the plain branch builds none, whatever the call's own dataset is.
// Guard, green before and after.
func TestMovers_CallWithoutDatasetStepsCarriesNoStepRows(t *testing.T) {
	for _, m := range movers {
		call := stepDatasetCall([]Step{{Method: "a/Two", Timeout: stepTimeout, Payload: []byte("plain")}}, atRate(1000, 5))
		call.Payload = nil
		call.Payloads = recordsOf(3)
		call.Dataset = &DatasetRef{File: "data/call.jsonl", Counter: NewRecordCounter()}

		reqs := handedBy(t, m, call)
		if len(reqs) != 5 {
			t.Fatalf("%s: handed out %d, want 5", m.name, len(reqs))
		}

		for i := range reqs {
			if reqs[i].StepRows != nil {
				t.Errorf("%s: request %d carries StepRows %v, want nil", m.name, i, reqs[i].StepRows)
			}
		}
	}
}

// Ground: hot path — a mover hands out a scenario request with one allocation,
// the row slice, and a plain request with none: the call is carried by a pointer
// to the caller's own Call, not to a copy built per request. Counted over the
// whole hand-out because Dispatcher.Run is a loop, not a function to call once.
// Mutation: Dispatcher.Run builds a temporary Scheduler for each request.
func TestMovers_HandOutAllocatesOneForAScenarioRequestAndNoneForAPlainOne(t *testing.T) {
	const n = 1000

	mallocsPerRequest := func(m mover, call Call) float64 {
		out := make(chan Request, n+24)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		if err := m.run(t.Context(), call, out); err != nil {
			t.Fatalf("%s run: %v", m.name, err)
		}
		runtime.ReadMemStats(&after)

		if len(out) != n {
			t.Fatalf("%s: handed out %d, want %d", m.name, len(out), n)
		}

		return float64(after.Mallocs-before.Mallocs) / n
	}

	for _, m := range movers {
		scenario := stepDatasetCall([]Step{datasetStep("a/Two", "two-", 3, NewRecordCounter())}, atRate(1000000, n))
		plain := stepDatasetCall([]Step{{Method: "a/Two", Timeout: stepTimeout, Payload: []byte("plain")}}, atRate(1000000, n))

		// A constant few allocations of the run itself, spread over n, stay under 0.1.
		if got := mallocsPerRequest(m, scenario); got > 1.1 {
			t.Errorf("%s: %.2f allocations per scenario request, want 1 (the row slice)", m.name, got)
		}
		if got := mallocsPerRequest(m, plain); got > 0.1 {
			t.Errorf("%s: %.2f allocations per plain request, want 0", m.name, got)
		}
	}
}

// stepDatasetInFlight fits rps × Σ step timeouts of the fastest plans here.
const stepDatasetInFlight = 1024

// runScenarioEngine runs one engine over call to the end and returns its report.
func runScenarioEngine(t *testing.T, call Call, sender Sender) Report {
	t.Helper()

	eng, err := New(Options{Calls: []Call{call}, Sender: sender, MaxInFlight: stepDatasetInFlight})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	return eng.Report()
}

// scenarioSender fails step 1 for the records its failing function names and
// keeps the bodies of the steps after it.
type scenarioSender struct {
	fails func(record int) bool

	mu     sync.Mutex
	bodies map[string][]string
}

func (s *scenarioSender) Send(_ context.Context, req Request) (Outcome, error) {
	now := time.Now()
	out := Outcome{Category: CategorySuccess, SentAt: now, DoneAt: now}

	if req.Method == "a/One" {
		if s.fails != nil && s.fails(recordNumber(req)) {
			out.Category = CategoryServerFault
		}

		return out, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bodies == nil {
		s.bodies = map[string][]string{}
	}

	s.bodies[req.Method] = append(s.bodies[req.Method], string(req.Payload))

	return out, nil
}

// bodiesOf are the bodies a method received, sorted: the order in which
// iterations reach a step is the sender's business, the set is the engine's.
func (s *scenarioSender) bodiesOf(method string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := slices.Clone(s.bodies[method])
	sort.Strings(out)

	return out
}

// Ground: contract — report: used is what was handed out with the step, not
// what reached it: step 1 always fails, 100 iterations, 10 records at step 2 give
// used 10 and used_max 10 on the row of step 2's method, which sent nothing and
// was reached by no iteration. The counter moved once per iteration, never per
// step sent.
// Mutations: used counted from steps reached; the step method's dataset left nil
// (report not fed); the counter moves per step sent.
func TestEngine_StepDatasetUsedIsIterationsHandedOutNotStepsReached(t *testing.T) {
	const iterations, records = 100, 10

	counter := NewRecordCounter()
	sender := &scenarioSender{fails: func(int) bool { return true }}
	call := stepDatasetCall([]Step{datasetStep("a/Two", "two-", records, counter)}, atRate(1000, iterations))

	report := runScenarioEngine(t, call, sender)

	if report.Scheduled != iterations {
		t.Fatalf("scheduled %d, want %d", report.Scheduled, iterations)
	}
	if n := counter.Handed(); n != iterations {
		t.Errorf("step counter = %d, want %d: it moves for every iteration handed out, reached or not", n, iterations)
	}

	row, ok := methodRow(report, "a/Two")
	if !ok {
		t.Fatalf("no row for a/Two among %d method rows", len(report.Methods))
	}
	if row.Sent != 0 {
		t.Errorf("a/Two sent %d, want 0: step 1 failed every time", row.Sent)
	}
	if ds := row.Dataset; ds == nil {
		t.Errorf("a/Two has no dataset in the report, want the step's file")
	} else {
		if ds.File != "data/two-.jsonl" || ds.SHA256 != "sha-two-" || ds.Records != records {
			t.Errorf("a/Two dataset = %q %q with %d records, want data/two-.jsonl sha-two- with %d", ds.File, ds.SHA256, ds.Records, records)
		}
		if ds.Used != records || ds.UsedMax != records {
			t.Errorf("a/Two used %d, used_max %d, want %d and %d: 100 iterations handed out over 10 records", ds.Used, ds.UsedMax, records, records)
		}
	}

	sc := scenarioOf(t, report, "flow")
	if len(sc.Steps) != 2 {
		t.Fatalf("the scenario has %d steps, want 2", len(sc.Steps))
	}
	if got := sc.Steps[1].Reached; got != 0 {
		t.Errorf("step 2 reached by %d iterations, want 0: used %d and reached 0 are both true of one report", got, records)
	}
	if m, ok := methodRow(report, "a/One"); !ok || m.Dataset != nil {
		t.Errorf("a/One dataset = %+v (found %v), want none: the call has no file", m.Dataset, ok)
	}
}

// Ground: contract — an iteration whose step 1 failed still spent the row of
// step 2: iterations 0, 2 and 4 fail, 1, 3 and 5 reach step 2 and take rows 1, 3
// and 5 of it, not 0, 1 and 2.
// Mutation: the counter moves per step sent.
func TestEngine_StepRowIsSpentByAnIterationThatFailedBefore(t *testing.T) {
	const iterations = 6

	sender := &scenarioSender{fails: func(record int) bool { return record%2 == 0 }}
	call := stepDatasetCall([]Step{datasetStep("a/Two", "two-", iterations, NewRecordCounter())}, atRate(200, iterations))
	call.Payload = nil
	call.Payloads = recordsOf(iterations)
	call.Dataset = &DatasetRef{File: "data/call.jsonl", Counter: NewRecordCounter()}

	runScenarioEngine(t, call, sender)

	if got, want := sender.bodiesOf("a/Two"), []string{"two-1", "two-3", "two-5"}; !slices.Equal(got, want) {
		t.Errorf("step 2 got %q, want %q: the iterations that failed in step 1 spent rows 0, 2 and 4", got, want)
	}
}

// Ground: contract — a step with records and no counter counts this run from
// its first row, as a call does (counted), through the engine; the caller's
// call is not touched, and a second run over the same call starts again at row 0.
// Mutations: needsCounter does not see a step's nil counter; counted writes into
// the caller's Next.
func TestEngine_StepDatasetWithNoCounterCountsTheRun(t *testing.T) {
	const iterations = 4

	call := stepDatasetCall([]Step{datasetStep("a/Two", "two-", 3, nil)}, atRate(200, iterations))
	want := []string{"two-0", "two-0", "two-1", "two-2"}

	for run := 1; run <= 2; run++ {
		sender := &scenarioSender{}
		report := runScenarioEngine(t, call, sender)

		if got := sender.bodiesOf("a/Two"); !slices.Equal(got, want) {
			t.Errorf("run %d: step 2 got %q, want %q: rows 0, 1, 2, 0 from the run's own counter", run, got, want)
		}

		row, ok := methodRow(report, "a/Two")
		if !ok || row.Dataset == nil {
			t.Fatalf("run %d: a/Two has no dataset in the report (found %v)", run, ok)
		}
		if row.Dataset.Used != 3 || row.Dataset.UsedMax != 2 {
			t.Errorf("run %d: used %d, used_max %d, want 3 and 2", run, row.Dataset.Used, row.Dataset.UsedMax)
		}
	}

	if ref := call.Next[0].Dataset; ref.Counter != nil {
		t.Errorf("the caller's step has a counter after the runs: the run's own went into the caller's Next")
	}
}

// Ground: contract — counted gives a step with records and no counter one of the
// run's own, on a copy of Next: the caller's slice and its step stay as they
// were, and needsCounter sees the step, or countedCalls would skip the call.
// Mutations: needsCounter looks at the call only; counted writes into Next.
func TestCounted_AStepWithNoCounterGetsOneAndTheCallersStepsAreUntouched(t *testing.T) {
	call := stepDatasetCall([]Step{
		datasetStep("a/Two", "two-", 3, nil),
		{Method: "a/Three", Timeout: stepTimeout, Payload: []byte("plain")},
	}, atRate(10, 1))
	ref := call.Next[0].Dataset

	if !call.needsCounter() {
		t.Errorf("needsCounter = false for a call whose step has records and no counter")
	}

	got := counted(call)
	if len(got.Next) != 2 || got.Next[0].Dataset == nil || got.Next[0].Dataset.Counter == nil {
		t.Fatalf("counted left the step without a counter: %+v", got.Next)
	}
	if &got.Next[0] == &call.Next[0] {
		t.Errorf("counted returned the caller's Next, want a copy")
	}
	if call.Next[0].Dataset != ref || ref.Counter != nil {
		t.Errorf("the caller's step has a counter after counted: its DatasetRef was written into")
	}
	if got.Next[1].Dataset != nil || string(got.Next[1].Payload) != "plain" {
		t.Errorf("a plain step changed: %+v", got.Next[1])
	}
}

// Ground: contract — countedCalls counts a call whose only missing counter is a
// step's, on a copy: the caller's slice, call and step stay as they were.
// Mutation: needsCounter looks at the call only.
func TestCountedCalls_ACallWhoseOnlyMissingCounterIsAStepsIsCounted(t *testing.T) {
	calls := []Call{stepDatasetCall([]Step{datasetStep("a/Two", "two-", 3, nil)}, atRate(10, 1))}

	got := countedCalls(calls)
	if got[0].Next[0].Dataset == nil || got[0].Next[0].Dataset.Counter == nil {
		t.Fatalf("countedCalls left the step without a counter")
	}
	if &got[0] == &calls[0] {
		t.Errorf("countedCalls returned the caller's slice, want a copy")
	}
	if calls[0].Next[0].Dataset.Counter != nil {
		t.Errorf("the caller's step has a counter after countedCalls")
	}
}

func onceStepCall() Call {
	step := datasetStep("a/Two", "two-", 3, NewRecordCounter())
	step.Dataset.Once = true

	return stepDatasetCall([]Step{step}, atRate(100, 3))
}

// Ground: contract — Once on a step is an error from the engine, not a flag
// ignored: a step's rows are wrapped, never spent. Either New or Run says it.
// Mutation: Once on a step ignored.
func TestEngine_StepDatasetOnceIsAnError(t *testing.T) {
	eng, err := New(Options{Calls: []Call{onceStepCall()}, Sender: answering(), MaxInFlight: stepDatasetInFlight})
	if err == nil {
		err = eng.Run(t.Context())
	}
	if !errors.Is(err, ErrStepDatasetOnce) {
		t.Errorf("err = %v, want ErrStepDatasetOnce", err)
	}
}

// Ground: contract — the library's Scheduler.Run refuses it too, before it hands
// anything out.
// Mutation: Once on a step ignored.
func TestScheduler_RunStepDatasetOnceIsAnError(t *testing.T) {
	out := make(chan Request, 8)

	err := NewScheduler(onceStepCall()).Run(t.Context(), out)
	if !errors.Is(err, ErrStepDatasetOnce) {
		t.Errorf("err = %v, want ErrStepDatasetOnce", err)
	}
	if n := len(out); n != 0 {
		t.Errorf("%d requests handed out before the refusal, want 0", n)
	}
}

// Ground: concurrency — many iterations of one call in flight at once (the
// barrier holds all of them inside step 1 until every one has begun), each step
// body taken from its own iteration's row of its own file: no body is read with
// another iteration's row, none is lost. The rows are not the order of arrival,
// so a position the pool kept of its own would not look right. Run under -race.
// Mutation: the worker picks the row from a shared position.
func TestWorkerPool_StepDatasetBodiesAreNotMixedAcrossIterations(t *testing.T) {
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

	steps := []Step{datasetStep("a/Two", "two-", 7, nil), datasetStep("a/Three", "three-", 5, nil)}
	for i := range steps {
		steps[i].Timeout = time.Minute
	}

	_, err := runPool(t.Context(), sender, 2*iterations, func(in chan<- Request) {
		tick := time.Now()

		for i := 1; i <= iterations; i++ {
			req := scenarioReq(tick, "flow", Step{Method: "a/One", Timeout: time.Minute}, steps...)
			req.Payload = []byte("first-" + strconv.Itoa(i))
			req.StepRows = []int32{int32(i % 7), int32(3 * i % 5)} //nolint:gosec // small test values
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

	bad := 0

	for _, group := range groups {
		id, ok := strings.CutPrefix(group[0].payload, "first-")
		if !ok {
			t.Fatalf("step 1 body = %q, want first-<n>", group[0].payload)
		}

		i, err := strconv.Atoi(id)
		if err != nil {
			t.Fatalf("step 1 body = %q, want first-<n>", group[0].payload)
		}

		want := []string{group[0].payload, "two-" + strconv.Itoa(i%7), "three-" + strconv.Itoa(3*i%5)}
		if got := payloadsOf(group); !slices.Equal(got, want) {
			bad++
			if bad <= 3 {
				t.Errorf("iteration %d sent %q, want %q: a step took another iteration's row", i, got, want)
			}
		}
	}

	if bad > 0 {
		t.Errorf("%d of %d iterations sent a step body that is not its own row", bad, iterations)
	}
}
