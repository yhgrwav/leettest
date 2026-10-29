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
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// seqBody is a body that is only its seq, as text: the tests read the number
// the engine gave a request straight from the payload.
func seqBody(seq uint64) []byte {
	return []byte(strconv.FormatUint(seq, 10))
}

// countingTemplate is a seq-only template; builds counts the calls of Build and
// top is the largest seq it was asked for.
type countingTemplate struct {
	*Template

	builds atomic.Int64
	top    atomic.Uint64
}

func newCountingTemplate(limit uint64) *countingTemplate {
	c := &countingTemplate{}
	c.Template = NewTemplate([]string{"seq"}, limit, func(seq uint64) []byte {
		c.builds.Add(1)

		for {
			old := c.top.Load()
			if seq <= old || c.top.CompareAndSwap(old, seq) {
				break
			}
		}

		return seqBody(seq)
	})

	return c
}

func templateCall(method string, tpl *Template, stages ...Stage) Call {
	return Call{Method: method, Template: tpl, Timeout: 100 * time.Millisecond, Stages: stages}
}

// payloadSender answers every request and keeps the body it was given.
type payloadSender struct {
	mu   sync.Mutex
	seen []seenPayload
}

type seenPayload struct {
	at      time.Time
	payload string
}

func (s *payloadSender) Send(_ context.Context, req Request) (Outcome, error) {
	s.mu.Lock()
	s.seen = append(s.seen, seenPayload{at: req.ScheduledAt, payload: string(req.Payload)})
	s.mu.Unlock()

	now := time.Now()

	return Outcome{Category: CategorySuccess, SentAt: now, DoneAt: now}, nil
}

// payloads are the bodies the sender saw, in the order the requests were
// scheduled.
func (s *payloadSender) payloads() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := slices.Clone(s.seen)
	slices.SortStableFunc(seen, func(a, b seenPayload) int { return a.at.Compare(b.at) })

	out := make([]string, 0, len(seen))
	for _, r := range seen {
		out = append(out, r.payload)
	}

	return out
}

// dispatched runs the dispatcher over calls to the end and returns the bodies
// it handed out, per method, in the order it did.
func dispatched(t *testing.T, calls ...Call) map[string][]string {
	t.Helper()

	out := make(chan Request, 1024)
	if err := NewDispatcher(calls).Run(t.Context(), out); err != nil {
		t.Fatalf("run: %v", err)
	}
	close(out)

	got := map[string][]string{}
	for req := range out {
		got[req.Method] = append(got[req.Method], string(req.Payload))
	}

	return got
}

func seqs(from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, strconv.Itoa(i))
	}

	return out
}

// Ground: contract — {{seq}} is the number of the request, from 1, in the
// order the calls were scheduled, and each request is built once.
// Mutations: seq from 0; two requests get one seq.
func TestDispatcher_TemplateSeqCountsFromOneInOrder(t *testing.T) {
	tpl := newCountingTemplate(0)

	got := dispatched(t, templateCall("a.B/One", tpl.Template, atRate(200, 5)))["a.B/One"]

	if want := seqs(1, 5); !slices.Equal(got, want) {
		t.Fatalf("bodies = %q, want %q", got, want)
	}
	if n := tpl.builds.Load(); n != 5 {
		t.Fatalf("Build was called %d times for 5 requests: a request is built once", n)
	}
}

// Ground: contract — the engine path builds the same bodies as the dispatcher.
func TestEngine_TemplateSeqCountsFromOneInOrder(t *testing.T) {
	tpl := newCountingTemplate(0)
	sender := &payloadSender{}

	runEngine(t, templateCall("a.B/One", tpl.Template, atRate(200, 5)), 0, sender)

	if got, want := sender.payloads(), seqs(1, 5); !slices.Equal(got, want) {
		t.Fatalf("the sender saw %q, want %q", got, want)
	}
}

// Ground: contract — the counter lives with the Template, as the dataset's
// does: a copy of the call (a search step is one) continues the count.
// Mutation: the seq counter resets between search steps.
func TestDispatcher_TemplateCopiesOfACallShareTheCounter(t *testing.T) {
	tpl := newCountingTemplate(0)
	call := templateCall("a.B/One", tpl.Template, atRate(200, 3))

	first := dispatched(t, call)["a.B/One"]
	second := dispatched(t, call)["a.B/One"]

	if want := seqs(1, 3); !slices.Equal(first, want) {
		t.Fatalf("first run = %q, want %q", first, want)
	}
	if want := seqs(4, 6); !slices.Equal(second, want) {
		t.Fatalf("second run = %q, want %q: the count starts over", second, want)
	}
}

// Ground: contract — a request built and not handed out spends no seq. A run
// stopped while the dispatcher holds the next request leaves a number for the
// run after it, not a hole.
// Mutation: a number is spent on a built but unsent request.
func TestDispatcher_TemplateBuiltButUnsentRequestSpendsNoSeq(t *testing.T) {
	built := make(chan struct{}, 16)
	tpl := NewTemplate([]string{"seq"}, 0, func(seq uint64) []byte {
		built <- struct{}{}

		return seqBody(seq)
	})
	call := templateCall("a.B/One", tpl, atRate(100, 50))

	// Nobody reads the third request: the dispatcher builds it and waits.
	out := make(chan Request)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- NewDispatcher([]Call{call}).Run(ctx, out) }()

	for i, want := range []string{"1", "2"} {
		select {
		case req := <-out:
			if got := string(req.Payload); got != want {
				t.Fatalf("request %d body = %q, want %q", i+1, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("request %d did not arrive", i+1)
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
		t.Fatalf("counter = %d after 2 requests were handed out and a third was built, want 2", n)
	}

	next := dispatched(t, templateCall("a.B/One", tpl, atRate(200, 1)))["a.B/One"]
	if want := []string{"3"}; !slices.Equal(next, want) {
		t.Fatalf("the run after the stop starts at %q, want %q: a seq was spent on the unsent request", next, want)
	}
}

// Ground: contract — two calls count on their own.
func TestDispatcher_TemplateCallsKeepTheirOwnCounters(t *testing.T) {
	got := dispatched(t,
		templateCall("a.B/One", newCountingTemplate(0).Template, atRate(200, 3)),
		templateCall("a.B/Two", newCountingTemplate(0).Template, atRate(200, 4)),
	)

	if want := seqs(1, 3); !slices.Equal(got["a.B/One"], want) {
		t.Fatalf("One = %q, want %q", got["a.B/One"], want)
	}
	if want := seqs(1, 4); !slices.Equal(got["a.B/Two"], want) {
		t.Fatalf("Two = %q, want %q", got["a.B/Two"], want)
	}
}

// Ground: contract — a ramp and a warm-up count: the seq is the request of the
// call, whether the report counts it or not, and the report says how far it got.
func TestEngine_TemplateReportsTheLastSeqWarmupIncluded(t *testing.T) {
	tpl := newCountingTemplate(0)
	sender := &payloadSender{}

	report := runEngine(t, templateCall("a.B/One", tpl.Template, atRate(50, 20)), 200*time.Millisecond, sender)

	m := report.Methods[0]
	if m.WarmupSent == 0 || m.Sent == 0 {
		t.Fatalf("warm-up sent %d, run sent %d: the test needs both", m.WarmupSent, m.Sent)
	}
	if m.Templates == nil {
		t.Fatal("the report has no Templates for a templated call")
	}
	if m.Templates.LastSeq != 20 {
		t.Fatalf("LastSeq = %d, want 20: warm-up requests count", m.Templates.LastSeq)
	}
	if !slices.Equal(m.Templates.Placeholders, []string{"seq"}) {
		t.Fatalf("Placeholders = %q, want [seq]", m.Templates.Placeholders)
	}
}

// Ground: contract — a call without templates reports none.
func TestEngine_PlainCallHasNoTemplatesInTheReport(t *testing.T) {
	call := Call{Method: "a.B/One", Payload: []byte("x"), Timeout: 100 * time.Millisecond, Stages: []Stage{atRate(200, 3)}}

	m := runEngine(t, call, 0, answering()).Methods[0]

	if m.Templates != nil {
		t.Fatalf("Templates = %+v on a call without templates", m.Templates)
	}
}

// spin keeps the processor busy for d: a stand-in for a slow Build that no
// sleep can be blamed for.
func spin(d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end); {
	}
}

// delayedSender answers after the delay it is told, with the times it was
// given, so that service time is known.
type delayedSender struct{ service time.Duration }

func (s delayedSender) Send(_ context.Context, _ Request) (Outcome, error) {
	now := time.Now()

	return Outcome{Category: CategorySuccess, SentAt: now, DoneAt: now.Add(s.service)}, nil
}

// Ground: contract (amendment A) — the body is built by the scheduler, before
// the request is handed to a worker. A slow build is a late start, which the
// report counts as start lag and takes out of "p99 without waits" like any
// other client-side wait; built in the worker it would sit between the start
// and the send and read as the target's time.
// Mutation: build in the worker instead of the scheduler.
func TestEngine_TemplateBuildTimeIsStartLagNotTargetTime(t *testing.T) {
	const build = 40 * time.Millisecond

	var builds atomic.Int64
	tpl := NewTemplate([]string{"seq"}, 0, func(seq uint64) []byte {
		builds.Add(1)
		spin(build)

		return seqBody(seq)
	})

	report := runEngine(t, templateCall("a.B/One", tpl, atRate(10, 4)), 0, delayedSender{service: 3 * time.Millisecond})

	if n := builds.Load(); n != 4 {
		t.Fatalf("Build was called %d times for 4 requests", n)
	}

	m := report.Methods[0]
	if report.StartLagMax < 35*time.Millisecond {
		t.Fatalf("start lag max = %s, want at least the build time %s: the build is before the start mark", report.StartLagMax, build)
	}
	if got := m.P99WithoutClientWaits.Value; got > 20*time.Millisecond {
		t.Fatalf("p99 without waits = %s, want about the 3ms the target took: the build time is counted as the target's", got)
	}
}

// Ground: contract — a seq the field cannot hold is never built: the run stops
// with ErrSeqOverflow naming the call and the limit, after the requests that
// fit, and no body has wrapped around.
// Mutation: seq wraps in int32.
func TestEngine_TemplateOverflowStopsTheRunBeforeTheFirstSeqThatDoesNotFit(t *testing.T) {
	tpl := newCountingTemplate(3)
	sender := &payloadSender{}

	eng, err := New(Options{
		Calls:       []Call{templateCall("a.B/One", tpl.Template, atRate(200, 5))},
		Sender:      sender,
		MaxInFlight: 64,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	runErr := eng.Run(t.Context())
	if !errors.Is(runErr, ErrSeqOverflow) {
		t.Fatalf("run error = %v, want ErrSeqOverflow", runErr)
	}
	if !strings.Contains(runErr.Error(), "a.B/One") || !strings.Contains(runErr.Error(), "3") {
		t.Fatalf("error %q names neither the call nor the limit 3", runErr)
	}
	if got, want := sender.payloads(), seqs(1, 3); !slices.Equal(got, want) {
		t.Fatalf("the sender saw %q, want %q: the requests that fit, and nothing after", got, want)
	}
	if top := tpl.top.Load(); top > 3 {
		t.Fatalf("Build was asked for seq %d, above the limit 3", top)
	}
}

// Ground: contract — the guard is the same for a call run alone.
func TestScheduler_TemplateOverflowStopsTheRun(t *testing.T) {
	tpl := newCountingTemplate(3)
	out := make(chan Request, 16)

	err := NewScheduler(templateCall("a.B/One", tpl.Template, atRate(200, 5))).Run(t.Context(), out)
	close(out)

	if !errors.Is(err, ErrSeqOverflow) {
		t.Fatalf("run error = %v, want ErrSeqOverflow", err)
	}

	var got []string
	for req := range out {
		got = append(got, string(req.Payload))
	}
	if want := seqs(1, 3); !slices.Equal(got, want) {
		t.Fatalf("handed out %q, want %q", got, want)
	}
	if top := tpl.top.Load(); top > 3 {
		t.Fatalf("Build was asked for seq %d, above the limit 3", top)
	}
}

// Ground: boundary — the last seq that fits is legal: a limit of 5 and exactly
// 5 requests is not an overflow.
func TestEngine_TemplateExactlyTheLimitIsNotAnOverflow(t *testing.T) {
	tpl := newCountingTemplate(5)
	sender := &payloadSender{}

	runEngine(t, templateCall("a.B/One", tpl.Template, atRate(200, 5)), 0, sender)

	if got, want := sender.payloads(), seqs(1, 5); !slices.Equal(got, want) {
		t.Fatalf("the sender saw %q, want %q", got, want)
	}
}

// Ground: boundary — a limit of 0 is no limit.
func TestEngine_TemplateNoLimitNeverOverflows(t *testing.T) {
	tpl := newCountingTemplate(0)
	sender := &payloadSender{}

	runEngine(t, templateCall("a.B/One", tpl.Template, atRate(200, 40)), 0, sender)

	if got, want := sender.payloads(), seqs(1, 40); !slices.Equal(got, want) {
		t.Fatalf("the sender saw %d bodies, first %q; want 1..40 in order", len(got), got[:min(len(got), 3)])
	}
}

// Ground: hot path — a call without templates builds its request with no
// allocation, as before; a template in the code must not move it.
func TestScheduler_NewRequestWithoutTemplatesAllocatesNothing(t *testing.T) {
	plain := NewScheduler(Call{Method: "a.B/One", Payload: []byte("x"), Timeout: time.Second})
	data := NewScheduler(datasetCall(3, NewRecordCounter(), atRate(10, 3)))
	at := time.Now()

	if n := testing.AllocsPerRun(1000, func() { _ = plain.newRequest(at) }); n != 0 {
		t.Errorf("a plain call allocates %v per request, want 0", n)
	}
	if n := testing.AllocsPerRun(1000, func() { _ = data.newRequest(at) }); n != 0 {
		t.Errorf("a dataset call allocates %v per request, want 0", n)
	}
}

func BenchmarkNewRequest_Plain(b *testing.B) {
	s := NewScheduler(Call{Method: "a.B/One", Payload: []byte("x"), Timeout: time.Second})
	at := time.Now()

	b.ReportAllocs()

	for b.Loop() {
		_ = s.newRequest(at)
	}
}

func BenchmarkNewRequest_Templated(b *testing.B) {
	s := NewScheduler(templateCall("a.B/One", NewTemplate([]string{"seq"}, 0, seqBody), atRate(10, 3)))
	at := time.Now()

	b.ReportAllocs()

	for b.Loop() {
		_ = s.newRequest(at)
	}
}
