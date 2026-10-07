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
	"slices"
	"sort"
	"sync"
	"testing"
	"time"
)

// recordsOf is n records, each one byte: its own number.
func recordsOf(n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = []byte{byte(i)}
	}

	return out
}

// atRate is a stage of count requests at rps: the dispatcher places request i
// at i/rps, so a duration of count/rps holds exactly count of them.
func atRate(rps, count int) Stage {
	d := time.Duration(count) * time.Second / time.Duration(rps)

	return Stage{StartRPS: rps, TargetRPS: rps, Duration: d}
}

func datasetCall(n int, counter *RecordCounter, stages ...Stage) Call {
	return Call{
		Method: "a.B/One", Payloads: recordsOf(n), Dataset: &DatasetRef{File: "data/users.jsonl", Counter: counter},
		Timeout: 100 * time.Millisecond, Stages: stages,
	}
}

// recordingSender answers every request with outcome and keeps what it was
// given: the record's number (-1 for an empty payload) at the request's
// scheduled moment.
type recordingSender struct {
	outcome Outcome

	mu   sync.Mutex
	seen []seenRequest
}

type seenRequest struct {
	at     time.Time
	method string
	record int
}

func (s *recordingSender) Send(_ context.Context, req Request) (Outcome, error) {
	s.mu.Lock()
	s.seen = append(s.seen, seenRequest{at: req.ScheduledAt, method: req.Method, record: recordNumber(req)})
	s.mu.Unlock()

	out := s.outcome
	out.SentAt, out.DoneAt = time.Now(), time.Now()

	return out, nil
}

// records are the numbers the sender saw, in the order the requests were
// scheduled: calls run at once, so the order they arrive in is not the plan's.
func (s *recordingSender) records() []int {
	return s.recordsOf("")
}

// recordsOf is records for the requests of one method; "" is all of them.
func (s *recordingSender) recordsOf(method string) []int {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := slices.Clone(s.seen)
	sort.SliceStable(seen, func(i, j int) bool { return seen[i].at.Before(seen[j].at) })

	out := make([]int, 0, len(seen))
	for _, r := range seen {
		if method == "" || r.method == method {
			out = append(out, r.record)
		}
	}

	return out
}

func answering() *recordingSender {
	return &recordingSender{outcome: Outcome{Category: CategorySuccess}}
}

// runEngine runs one engine over call and returns its report.
func runEngine(t *testing.T, call Call, warmup time.Duration, sender Sender) Report {
	t.Helper()

	eng, err := New(Options{Calls: []Call{call}, Sender: sender, MaxInFlight: 64, Warmup: warmup})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	return eng.Report()
}

// handOut runs the dispatcher over calls to the end and returns what it handed
// out, per method, in the order it did.
func handOut(t *testing.T, calls ...Call) map[string][]int {
	t.Helper()

	out := make(chan Request, 1024)
	if err := NewDispatcher(calls).Run(t.Context(), out); err != nil {
		t.Fatalf("run: %v", err)
	}
	close(out)

	got := map[string][]int{}
	for req := range out {
		got[req.Method] = append(got[req.Method], recordNumber(req))
	}

	return got
}

func recordNumber(req Request) int {
	if len(req.Payload) == 0 {
		return -1
	}

	return int(req.Payload[0])
}

// Ground: contract — the order of records is the dataset's whole promise: the
// engine path (the dispatcher) hands record S mod n to the S-th request, the
// file's order, wrapping.
func TestDispatcher_DatasetCyclesInOrder(t *testing.T) {
	counter := NewRecordCounter()

	got := handOut(t, datasetCall(3, counter, atRate(1000, 7)))["a.B/One"]

	if want := []int{0, 1, 2, 0, 1, 2, 0}; !slices.Equal(got, want) {
		t.Errorf("records handed out = %v, want %v", got, want)
	}
	if n := counter.Handed(); n != 7 {
		t.Errorf("handed out = %d, want 7", n)
	}
}

// Two datasets in one run do not share a position: each call counts its own.
// Ground: contract — one counter per call; a shared one would hand each call
// every other record.
func TestDispatcher_EachCallCountsItsOwn(t *testing.T) {
	one := datasetCall(2, NewRecordCounter(), atRate(1000, 6))
	two := datasetCall(3, NewRecordCounter(), atRate(1000, 6))
	two.Method = "a.B/Two"

	got := handOut(t, one, two)

	if want := []int{0, 1, 0, 1, 0, 1}; !slices.Equal(got["a.B/One"], want) {
		t.Errorf("a.B/One: records = %v, want %v", got["a.B/One"], want)
	}
	if want := []int{0, 1, 2, 0, 1, 2}; !slices.Equal(got["a.B/Two"], want) {
		t.Errorf("a.B/Two: records = %v, want %v", got["a.B/Two"], want)
	}
}

// A call run with no counter still goes through its records in order, from the
// first, each run counting for itself.
// Ground: contract — pkg/engine is a library API; a caller that builds Calls
// without a counter gets this, not a panic and not a restart at every request.
func TestDispatcher_NoCounterCountsWithinTheRun(t *testing.T) {
	call := datasetCall(3, nil, atRate(1000, 4))

	for run := range 2 {
		if got, want := handOut(t, call)["a.B/One"], []int{0, 1, 2, 0}; !slices.Equal(got, want) {
			t.Errorf("run %d: records = %v, want %v", run, got, want)
		}
	}
}

// A request the dispatcher did not hand out did not use its record: the count
// moves after the send to the pool succeeds, not when the request is built.
// Out holds two and nobody reads; the third request waits for room, and the
// run is cancelled with it still waiting.
// Ground: contract — S is "requests handed out"; counting one that never left
// would skip a record and let a report say it was used.
func TestDispatcher_ARequestNotHandedOutIsNotCounted(t *testing.T) {
	counter := NewRecordCounter()
	call := datasetCall(3, counter, Stage{StartRPS: 100, TargetRPS: 100, Duration: time.Hour})
	out := make(chan Request, 2)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- NewDispatcher([]Call{call}).Run(ctx, out) }()

	limit := time.Now().Add(2 * time.Second)
	for counter.Handed() < 2 {
		if time.Now().After(limit) {
			t.Fatalf("handed out = %d after 2s, want the 2 that fit in the channel", counter.Handed())
		}
		time.Sleep(time.Millisecond)
	}

	// The third moment (20ms) is long gone: the dispatcher is waiting in the send.
	for time.Since(start) < 150*time.Millisecond {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the dispatcher did not stop on cancel")
	}

	if n := counter.Handed(); n != 2 {
		t.Errorf("handed out = %d, want 2: the third request never left", n)
	}

	// The next run takes up at the record the third request never used.
	next := datasetCall(3, counter, atRate(1000, 1))
	if got, want := handOut(t, next)["a.B/One"], []int{2}; !slices.Equal(got, want) {
		t.Errorf("first record of the next run = %v, want %v", got, want)
	}
}

// Ground: contract — a request built is not a request used: only the dispatcher
// moves the count, once the request has left. Building the same request twice
// gives the same record.
func TestScheduler_BuildingARequestDoesNotUseUpItsRecord(t *testing.T) {
	counter := NewRecordCounter()
	s := NewScheduler(datasetCall(3, counter, atRate(100, 1)))
	at := time.Now()

	first, second := s.newRequest(at), s.newRequest(at)

	if recordNumber(first) != 0 || recordNumber(second) != 0 {
		t.Errorf("records = %d and %d, want 0 and 0: nothing was handed out", recordNumber(first), recordNumber(second))
	}
	if n := counter.Handed(); n != 0 {
		t.Errorf("handed out = %d, want 0", n)
	}
}

var sinkRequest Request

// Ground: hot path — the hot path indexes a slice: no copy of the record, no
// encoding, no allocation beyond a call without a dataset.
func TestScheduler_NewRequestWithADatasetAllocatesNoMore(t *testing.T) {
	at := time.Now()
	with := NewScheduler(datasetCall(3, NewRecordCounter(), atRate(100, 1)))
	without := NewScheduler(Call{Method: "a.B/One", Payload: []byte{9}, Timeout: time.Second, Stages: []Stage{atRate(100, 1)}})

	// The request carries the record, not an empty payload: the comparison
	// below is of a dataset call that does its work.
	if got := with.newRequest(at); !slices.Equal(got.Payload, []byte{0}) {
		t.Fatalf("payload = %v, want record 0", got.Payload)
	}

	withAllocs := testing.AllocsPerRun(1000, func() { sinkRequest = with.newRequest(at) })
	withoutAllocs := testing.AllocsPerRun(1000, func() { sinkRequest = without.newRequest(at) })

	if withAllocs > withoutAllocs {
		t.Errorf("allocations per request: %v with a dataset, %v without", withAllocs, withoutAllocs)
	}
}

// Ground: contract — D3: warm-up requests are requests of the call; the first
// measured one is not record 1 again. The report counts them in S.
func TestEngine_DatasetCounterSurvivesWarmup(t *testing.T) {
	sender := answering()
	report := runEngine(t, datasetCall(3, NewRecordCounter(), atRate(100, 10)), 50*time.Millisecond, sender)

	if want := []int{0, 1, 2, 0, 1, 2, 0, 1, 2, 0}; !slices.Equal(sender.records(), want) {
		t.Errorf("records sent = %v, want %v: warm-up included", sender.records(), want)
	}
	// 10 handed out, of which 5 are measured: counting only the measured ones
	// would say 2.
	want := &DatasetReport{File: "data/users.jsonl", Records: 3, Used: 3, UsedMax: 4}
	if got := report.Methods[0].Dataset; got == nil || *got != *want {
		t.Errorf("dataset report = %+v, want %+v", got, want)
	}
}

// Ground: contract — D3: a stage does not restart the dataset; the index of a
// request within its stage is not the count of the call.
func TestEngine_DatasetCounterSurvivesStages(t *testing.T) {
	sender := answering()
	report := runEngine(t, datasetCall(3, NewRecordCounter(), atRate(100, 5), atRate(100, 5)), 0, sender)

	if want := []int{0, 1, 2, 0, 1, 2, 0, 1, 2, 0}; !slices.Equal(sender.records(), want) {
		t.Errorf("records sent = %v, want %v: the second stage went on", sender.records(), want)
	}
	if got := report.Methods[0].Dataset; got == nil || got.UsedMax != 4 {
		t.Errorf("dataset report = %+v, want UsedMax 4 of 10 over 3", got)
	}
}

// Ground: contract — D3: every step of a breaking-point search is a run of its
// own over the same Call; the count lives with the call, and each run's report
// is the count to the end of that run.
func TestEngine_DatasetCounterSurvivesRuns(t *testing.T) {
	counter := NewRecordCounter()

	first, second := answering(), answering()
	r1 := runEngine(t, datasetCall(3, counter, atRate(100, 4)), 0, first)
	r2 := runEngine(t, datasetCall(3, counter, atRate(100, 4)), 0, second)

	if want := []int{0, 1, 2, 0}; !slices.Equal(first.records(), want) {
		t.Errorf("first run sent %v, want %v", first.records(), want)
	}
	if want := []int{1, 2, 0, 1}; !slices.Equal(second.records(), want) {
		t.Errorf("second run sent %v, want %v: it goes on where the first ended", second.records(), want)
	}
	if got, want := r1.Methods[0].Dataset, (&DatasetReport{File: "data/users.jsonl", Records: 3, Used: 3, UsedMax: 2}); got == nil || *got != *want {
		t.Errorf("first report = %+v, want %+v", got, want)
	}
	// 8 handed out in all: cumulative to the end of the run.
	if got, want := r2.Methods[0].Dataset, (&DatasetReport{File: "data/users.jsonl", Records: 3, Used: 3, UsedMax: 3}); got == nil || *got != *want {
		t.Errorf("second report = %+v, want %+v", got, want)
	}
}

// Ground: contract — D4: a request that expired before it was sent used its
// record all the same, so the next one is not a repeat of it.
func TestEngine_UnsentRequestUsedItsRecord(t *testing.T) {
	counter := NewRecordCounter()

	unsent := &recordingSender{outcome: Outcome{Category: CategoryTimeout, NotSent: true, NotSentOn: BlockedOnConnection}}
	r1 := runEngine(t, datasetCall(4, counter, atRate(100, 6)), 0, unsent)
	if r1.NotSent != 6 {
		t.Fatalf("not sent = %d, want all 6 of the first run", r1.NotSent)
	}
	if got, want := r1.Methods[0].Dataset, (&DatasetReport{File: "data/users.jsonl", Records: 4, Used: 4, UsedMax: 2}); got == nil || *got != *want {
		t.Errorf("first report = %+v, want %+v: 6 handed out, none sent", got, want)
	}

	sent := answering()
	runEngine(t, datasetCall(4, counter, atRate(100, 2)), 0, sent)
	if want := []int{2, 3}; !slices.Equal(sent.records(), want) {
		t.Errorf("second run sent records %v, want %v: 0 to 5 are spent", sent.records(), want)
	}
}

// A call that has a counter but no records is not a division by zero: it sends
// one empty message, as a call without a dataset does, and counts.
// Ground: contract — pkg/engine is a library API; a caller can pass any Call.
func TestEngine_ZeroPayloadsIsOneEmpty(t *testing.T) {
	counter := NewRecordCounter()
	call := datasetCall(0, counter, atRate(100, 5))
	sender := answering()

	report := runEngine(t, call, 0, sender)

	if want := []int{-1, -1, -1, -1, -1}; !slices.Equal(sender.records(), want) {
		t.Errorf("records sent = %v, want 5 empty messages", sender.records())
	}
	if n := counter.Handed(); n != 5 {
		t.Errorf("handed out = %d, want 5", n)
	}
	if got := report.Methods[0].Dataset; got == nil || got.Records != 0 {
		t.Errorf("dataset report = %+v, want one with no records", got)
	}
}

// A call with no dataset beside one that has: it reports none and sends its
// Payload every time, the dataset's records do not leak into it.
// Ground: contract — pkg/engine is a library API; Payload is the one-request form.
func TestEngine_CallWithoutADatasetHasNoDatasetReport(t *testing.T) {
	withData := datasetCall(3, NewRecordCounter(), atRate(100, 4))
	plain := Call{Method: "a.B/Two", Payload: []byte{7}, Timeout: 100 * time.Millisecond, Stages: []Stage{atRate(100, 3)}}
	sender := answering()

	eng, err := New(Options{Calls: []Call{withData, plain}, Sender: sender, MaxInFlight: 64})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	byMethod := map[string]*DatasetReport{}
	for _, m := range eng.Report().Methods {
		byMethod[m.Method] = m.Dataset
	}
	if got := byMethod["a.B/One"]; got == nil || got.Records != 3 {
		t.Errorf("a.B/One: dataset report = %+v, want one of 3 records", got)
	}
	if got, ok := byMethod["a.B/Two"]; !ok || got != nil {
		t.Errorf("a.B/Two: dataset report = %+v (present %v), want nil", got, ok)
	}
	if want := []int{7, 7, 7}; !slices.Equal(sender.recordsOf("a.B/Two"), want) {
		t.Errorf("a.B/Two sent %v, want %v", sender.recordsOf("a.B/Two"), want)
	}
}

// The run is stopped after some requests: S is what was handed out, not what
// the plan had in store. A plan of an hour at 50 rps would say 72000 / 3.
// Ground: contract — D5: the report states what happened, from S; a number
// from the plan is wrong the moment the run stops early.
func TestEngine_StopAfterK(t *testing.T) {
	counter := NewRecordCounter()
	holder := newHoldingSender()
	call := datasetCall(3, counter, Stage{StartRPS: 50, TargetRPS: 50, Duration: time.Hour})
	call.Timeout = time.Second

	eng, err := New(Options{Calls: []Call{call}, Sender: holder, MaxInFlight: 1000})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	done := runAsync(t.Context(), eng)

	waitEntered(t, holder, 7)
	eng.Stop()
	close(holder.release)
	if err := waitRun(t, done); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Every request handed out reached the sender: K is what it was given, a
	// count the dispatcher's own does not feed.
	k := int(holder.calls.Load())
	if k < 7 {
		t.Fatalf("the sender was given %d requests, want at least the 7 waited for", k)
	}
	want := &DatasetReport{File: "data/users.jsonl", Records: 3, Used: 3, UsedMax: (k + 2) / 3}
	if got := eng.Report().Methods[0].Dataset; got == nil || *got != *want {
		t.Errorf("dataset report = %+v, want %+v for %d requests handed out", got, want, k)
	}
}

// Fewer requests than records: Used is the records the run reached, UsedMax is
// 1 — each used once at most.
// Ground: boundary — min(S, n) and ceil(S/n) at S below n, at S = n and one past it.
func TestEngine_DatasetUseAtTheEdgesOfTheFile(t *testing.T) {
	for _, tc := range []struct {
		requests, records, used, usedMax int
	}{
		{2, 5, 2, 1},
		{5, 5, 5, 1},
		{6, 5, 5, 2},
		{1, 1, 1, 1},
	} {
		report := runEngine(t, datasetCall(tc.records, NewRecordCounter(), atRate(100, tc.requests)), 0, answering())

		want := &DatasetReport{File: "data/users.jsonl", Records: tc.records, Used: tc.used, UsedMax: tc.usedMax}
		if got := report.Methods[0].Dataset; got == nil || *got != *want {
			t.Errorf("%d requests over %d records: report %+v, want %+v", tc.requests, tc.records, got, want)
		}
	}
}
