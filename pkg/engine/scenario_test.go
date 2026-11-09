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
	"math/rand/v2"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// scripted is a Sender answering by method: what each method does is a
// function of its own, and every request it gets is kept in arrival order.
type scripted struct {
	mu   sync.Mutex
	seen []Request
	do   map[string]func(context.Context, Request) (Outcome, error)
}

func (s *scripted) Send(ctx context.Context, req Request) (Outcome, error) {
	s.mu.Lock()
	s.seen = append(s.seen, req)
	do := s.do[req.Method]
	s.mu.Unlock()

	if do == nil {
		return Outcome{Category: CategorySuccess}, nil
	}

	return do(ctx, req)
}

func (s *scripted) methods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, 0, len(s.seen))
	for i := range s.seen {
		out = append(out, s.seen[i].Method)
	}

	return out
}

// reply answers after d with category; an abort in the meantime is an error
// wrapping the context's, as a sender's must be.
func reply(d time.Duration, category Category) func(context.Context, Request) (Outcome, error) {
	return func(ctx context.Context, _ Request) (Outcome, error) {
		select {
		case <-time.After(d):
			return Outcome{Category: category}, nil
		case <-ctx.Done():
			return Outcome{}, fmt.Errorf("send: %w", ctx.Err())
		}
	}
}

// hang does not answer: the call ends when the run cuts it off. entered is
// closed when it begins. A ceiling of 5 s keeps a regression that never cuts it
// off from hanging the package: the call then succeeds and the test fails on
// its own assert.
func hang(entered chan<- struct{}) func(context.Context, Request) (Outcome, error) {
	var once sync.Once

	return func(ctx context.Context, _ Request) (Outcome, error) {
		if entered != nil {
			once.Do(func() { close(entered) })
		}
		select {
		case <-ctx.Done():
			return Outcome{}, fmt.Errorf("send: %w", ctx.Err())
		case <-time.After(5 * time.Second):
			return Outcome{Category: CategorySuccess}, nil
		}
	}
}

// runPool runs a worker pool over what feed sends and returns every result it
// delivered. feed must not send past the moment the pool stops listening.
func runPool(ctx context.Context, sender Sender, maxInFlight int, feed func(in chan<- Request)) ([]Result, error) {
	in := make(chan Request)
	out := make(chan Result)

	var got []Result

	done := make(chan struct{})

	go func() {
		defer close(done)

		for r := range out {
			got = append(got, r)
		}
	}()

	go func() {
		defer close(in)

		feed(in)
	}()

	err := NewWorkerPool(sender, maxInFlight).Run(ctx, in, out)
	close(out)
	<-done

	return got, err
}

// scenarioReq is the request of an iteration ticking at tick: first is step 1,
// next the steps after it.
func scenarioReq(tick time.Time, scenario string, first Step, next ...Step) Request {
	req := Request{Method: first.Method, Scenario: scenario, Next: next, ScheduledAt: tick}
	if first.Timeout > 0 {
		req.Deadline = tick.Add(first.Timeout)
	}

	return req
}

// only is the one result of a method, or the end of the test.
func only(t *testing.T, results []Result, method string) Result {
	t.Helper()

	var found []Result

	for i := range results {
		if results[i].Method == method {
			found = append(found, results[i])
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d results for %s, want 1 (of %d results)", len(found), method, len(results))
	}

	return found[0]
}

func scenarioStats(results []Result, name string, methods []string, start time.Time, warmup time.Duration) Report {
	stats := NewStats()
	stats.setScenario(name, methods)
	stats.Start(start, warmup)

	end := start
	for i := range results {
		stats.Record(results[i])
		end = maxTime(end, results[i].DoneAt)
	}

	stats.EndSending(end)
	stats.Finish(end)

	return stats.Report()
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

func scenarioOf(t *testing.T, r Report, name string) ScenarioReport {
	t.Helper()

	for _, s := range r.Scenarios {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no scenario %q among %d in the report", name, len(r.Scenarios))

	return ScenarioReport{}
}

func methodRow(r Report, method string) (MethodReport, bool) {
	for i := range r.Methods {
		if r.Methods[i].Method == method {
			return r.Methods[i], true
		}
	}

	return MethodReport{}, false
}

// wantSteps compares the counters of every step: reached, failed here,
// aborted here, not sent here, not reached.
func wantSteps(t *testing.T, got []ScenarioStepReport, want [][5]int) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%d steps in the scenario entry, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := [5]int{got[i].Reached, got[i].FailedHere, got[i].AbortedHere, got[i].NotSentHere, got[i].NotReached}
		if g != w {
			t.Errorf("step %d (%s): reached/failed/aborted/not sent/not reached = %v, want %v", i+1, got[i].Method, g, w)
		}
	}
}

// Ground: contract — Request.ScheduledAt of a step is public to the sender and the result:
// step k+1 starts from the end of step k, so its latency is its own and the wait it
// saw is not step 1's service time (wrk2 rule: each call from its own schedule).
func TestScenario_StepTwoIsScheduledAtStepOnesEnd(t *testing.T) {
	tick := time.Now()
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": reply(60*time.Millisecond, CategorySuccess),
		"a/Two": reply(20*time.Millisecond, CategorySuccess),
	}}

	results, err := runPool(t.Context(), sender, 4, func(in chan<- Request) {
		in <- scenarioReq(tick, "flow", Step{Method: "a/One", Timeout: time.Second}, Step{Method: "a/Two", Timeout: time.Second})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	one, two := only(t, results, "a/One"), only(t, results, "a/Two")
	if !two.ScheduledAt.Equal(one.DoneAt) {
		t.Errorf("step 2 scheduled at %v, step 1 done at %v; want equal", two.ScheduledAt.Sub(tick), one.DoneAt.Sub(tick))
	}
	if lat := two.Latency(); lat < 20*time.Millisecond {
		t.Errorf("step 2 latency %v, want at least its own 20ms", lat)
	}
	if want := two.ScheduledAt.Add(time.Second); !two.Deadline.Equal(want) {
		t.Errorf("step 2 deadline %v after its schedule, want its own timeout, 1s", two.Deadline.Sub(two.ScheduledAt))
	}
	for _, r := range []Result{one, two} {
		if !r.IterationTick.Equal(tick) {
			t.Errorf("%s carries tick %v, want the iteration's %v", r.Method, r.IterationTick, tick)
		}
	}
}

// Ground: contract — a step is sent only after a success: a failed step ends the iteration, its
// category is the iteration's, and the counters name the step it ended at.
func TestScenario_AFailedStepEndsTheIteration(t *testing.T) {
	tick := time.Now()
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": reply(10*time.Millisecond, CategoryOverload),
	}}

	results, err := runPool(t.Context(), sender, 4, func(in chan<- Request) {
		in <- scenarioReq(tick, "flow", Step{Method: "a/One", Timeout: time.Second}, Step{Method: "a/Two", Timeout: time.Second})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := sender.methods(); !slices.Equal(got, []string{"a/One"}) {
		t.Errorf("sent %v, want only step 1: step 2 follows a success", got)
	}
	it := only(t, results, "flow")
	if !it.Iteration || it.Category != CategoryOverload || it.IterationStep != 0 {
		t.Errorf("iteration result = iteration %v, category %v, step %d; want true, overload, 0",
			it.Iteration, it.Category, it.IterationStep)
	}

	report := scenarioStats(results, "flow", []string{"a/One", "a/Two"}, tick, 0)
	sc := scenarioOf(t, report, "flow")
	if sc.Iterations != 1 || sc.ByCategory[CategoryOverload] != 1 {
		t.Errorf("iterations %d, overload %d; want 1 and 1", sc.Iterations, sc.ByCategory[CategoryOverload])
	}
	wantSteps(t, sc.Steps, [][5]int{{1, 1, 0, 0, 0}, {0, 0, 0, 0, 1}})
}

// Ground: contract — iteration latency runs from the tick to the last step's end, as a call's
// from its schedule: a late start (generator lag) is inside it, and it is not step 1's
// BegunAt that it counts from.
func TestScenario_IterationLatencyRunsFromTheTick(t *testing.T) {
	tick := time.Now().Add(-50 * time.Millisecond)
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": reply(30*time.Millisecond, CategorySuccess),
		"a/Two": reply(30*time.Millisecond, CategorySuccess),
	}}

	results, err := runPool(t.Context(), sender, 4, func(in chan<- Request) {
		in <- scenarioReq(tick, "flow", Step{Method: "a/One", Timeout: time.Second}, Step{Method: "a/Two", Timeout: time.Second})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	one, two, it := only(t, results, "a/One"), only(t, results, "a/Two"), only(t, results, "flow")
	if one.QueueTime() < 50*time.Millisecond {
		t.Fatalf("premise: step 1 started %v late, want 50ms or more", one.QueueTime())
	}
	if !it.ScheduledAt.Equal(tick) || !it.IterationTick.Equal(tick) {
		t.Errorf("iteration scheduled at %v, tick %v; want the tick %v for both", it.ScheduledAt, it.IterationTick, tick)
	}
	if !it.DoneAt.Equal(two.DoneAt) {
		t.Errorf("iteration done at %v, last step at %v; want equal", it.DoneAt, two.DoneAt)
	}
	if want := two.DoneAt.Sub(tick); it.Latency() != want || it.Latency() < 100*time.Millisecond {
		t.Errorf("iteration latency %v, want %v (tick to the last step's end, the 50ms late start inside)", it.Latency(), want)
	}
}

// Ground: contract — MaxInFlight and the cap count iterations: the slot is held from step 1's
// launch to the iteration's end, so a second tick finds it taken exactly as with one
// call that lasts as long as the whole iteration.
func TestScenario_OneSlotForTheWholeIteration(t *testing.T) {
	// The second tick comes once the first unit holds its slot in the call that
	// does not end: step 2 of the iteration, or the one long call.
	second := func(in chan<- Request, entered <-chan struct{}) {
		if !await(entered) {
			return
		}
		in <- Request{Method: "b/Other", ScheduledAt: time.Now()}
	}

	t.Run("a two-step iteration", func(t *testing.T) {
		entered := make(chan struct{})
		sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
			"a/One": reply(time.Millisecond, CategorySuccess),
			"a/Two": hang(entered),
		}}

		_, err := runPool(t.Context(), sender, 1, func(in chan<- Request) {
			in <- scenarioReq(time.Now(), "flow", Step{Method: "a/One", Timeout: 5 * time.Second}, Step{Method: "a/Two", Timeout: 5 * time.Second})
			second(in, entered)
		})

		var capHit *InFlightCapError
		if !errors.As(err, &capHit) {
			t.Fatalf("err = %v, want the cap hit: step 1 ended but the iteration still held the one slot", err)
		}
	})

	t.Run("one call as long as the sum", func(t *testing.T) {
		entered := make(chan struct{})
		sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
			"a/Long": hang(entered),
		}}

		_, err := runPool(t.Context(), sender, 1, func(in chan<- Request) {
			in <- Request{Method: "a/Long", ScheduledAt: time.Now(), Deadline: time.Now().Add(5 * time.Second)}
			second(in, entered)
		})

		var capHit *InFlightCapError
		if !errors.As(err, &capHit) {
			t.Fatalf("err = %v, want the cap hit (the control)", err)
		}
	})
}

// Ground: contract — a run aborted by the caller records the call in flight as aborted, never as
// the target's fault (CategoryCutOff is a reset by the target or a proxy): the step in flight
// is aborted, the iteration is aborted and censored at the abort, later steps are not reached.
func TestScenario_AbortDuringAStepIsAborted(t *testing.T) {
	tick := time.Now()
	entered := make(chan struct{})
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": reply(10*time.Millisecond, CategorySuccess),
		"a/Two": hang(entered),
	}}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		if await(entered) {
			cancel()
		}
	}()

	results, err := runPool(ctx, sender, 4, func(in chan<- Request) {
		in <- scenarioReq(tick, "flow", Step{Method: "a/One", Timeout: time.Second},
			Step{Method: "a/Two", Timeout: time.Second}, Step{Method: "a/Three", Timeout: time.Second})
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run: %v, want the abort", err)
	}

	if got := sender.methods(); !slices.Equal(got, []string{"a/One", "a/Two"}) {
		t.Errorf("sent %v, want steps 1 and 2: step 3 is never launched after the abort", got)
	}
	two, it := only(t, results, "a/Two"), only(t, results, "flow")
	if two.Category != CategoryAborted {
		t.Errorf("step 2 = %v, want aborted", two.Category)
	}
	if it.Category != CategoryAborted || it.IterationStep != 1 {
		t.Errorf("iteration = %v at step %d, want aborted at step 2 (index 1)", it.Category, it.IterationStep)
	}
	if !it.DoneAt.Equal(two.DoneAt) || it.CensorThreshold() != it.Latency() {
		t.Errorf("iteration done %v, censored at %v; want the abort moment %v and its latency", it.DoneAt, it.CensorThreshold(), two.DoneAt)
	}

	sc := scenarioOf(t, scenarioStats(results, "flow", []string{"a/One", "a/Two", "a/Three"}, tick, 0), "flow")
	wantSteps(t, sc.Steps, [][5]int{{1, 0, 0, 0, 0}, {1, 0, 1, 0, 0}, {0, 0, 0, 0, 1}})
}

// Ground: contract — an iteration whose step succeeded and whose next step the abort kept from
// launching counts as reached that step and aborted there: the invariant
// reached(k+1) = reached(k) − failed(k) − aborted(k) holds, the step is no call, and the
// iteration keeps a zero Deadline so it is censored at abort − tick.
func TestScenario_AbortBetweenStepsAbortsTheIteration(t *testing.T) {
	tick := time.Now()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var cancelAt time.Time

	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		// Succeeds at the very moment the run is aborted; the abort has reached
		// the send context by the time it returns.
		"a/One": func(sendCtx context.Context, _ Request) (Outcome, error) {
			cancelAt = time.Now()
			cancel()
			<-sendCtx.Done()

			return Outcome{Category: CategorySuccess}, nil
		},
	}}

	results, err := runPool(ctx, sender, 4, func(in chan<- Request) {
		in <- scenarioReq(tick, "flow", Step{Method: "a/One", Timeout: time.Second}, Step{Method: "a/Two", Timeout: time.Second})
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run: %v, want the abort", err)
	}

	if got := sender.methods(); !slices.Equal(got, []string{"a/One"}) {
		t.Errorf("sent %v, want only step 1", got)
	}
	one, it := only(t, results, "a/One"), only(t, results, "flow")
	if it.Category != CategoryAborted || it.IterationStep != 1 {
		t.Errorf("iteration = %v at step %d, want aborted at step 2 (index 1)", it.Category, it.IterationStep)
	}
	if !it.Deadline.IsZero() {
		t.Errorf("iteration deadline %v, want zero: no step was in flight", it.Deadline)
	}
	if it.DoneAt.Before(cancelAt) || it.DoneAt.After(one.DoneAt) || it.CensorThreshold() != it.Latency() {
		t.Errorf("iteration done at %v, want the abort moment between %v and %v, censored at its latency",
			it.DoneAt.Sub(tick), cancelAt.Sub(tick), one.DoneAt.Sub(tick))
	}

	report := scenarioStats(results, "flow", []string{"a/One", "a/Two"}, tick, 0)
	sc := scenarioOf(t, report, "flow")
	wantSteps(t, sc.Steps, [][5]int{{1, 0, 0, 0, 0}, {1, 0, 1, 0, 0}})
	if sc.Latencies != 1 || sc.Censored != 1 {
		t.Errorf("iteration observations %d, censored %d; want 1 and 1", sc.Latencies, sc.Censored)
	}
	if report.Sent != 1 || report.NotSent != 0 {
		t.Errorf("sent %d, not sent %d; want 1 and 0: the step never launched is neither", report.Sent, report.NotSent)
	}
}

// Ground: contract — Stop ends the schedule, not the work in flight: an iteration in flight is the
// unit in flight, and its later steps still start.
func TestScenario_StopLetsTheIterationInFlightFinish(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once

	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": func(ctx context.Context, req Request) (Outcome, error) {
			once.Do(func() { close(entered) })

			return reply(80*time.Millisecond, CategorySuccess)(ctx, req)
		},
		"a/Two":   reply(80*time.Millisecond, CategorySuccess),
		"a/Three": reply(80*time.Millisecond, CategorySuccess),
	}}

	eng, err := New(Options{
		Calls: []Call{{
			Method: "a/One", Scenario: "flow", Timeout: 2 * time.Second,
			Next:   []Step{{Method: "a/Two", Timeout: 2 * time.Second}, {Method: "a/Three", Timeout: 2 * time.Second}},
			Stages: []Stage{{StartRPS: 1, TargetRPS: 1, Duration: time.Second}},
		}},
		Sender:      sender,
		MaxInFlight: 8,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	go func() {
		await(entered)
		eng.Stop()
	}()

	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := sender.methods(); !slices.Equal(got, []string{"a/One", "a/Two", "a/Three"}) {
		t.Errorf("sent %v, want all three steps: the iteration in flight runs to its end", got)
	}
	sc := scenarioOf(t, eng.Report(), "flow")
	if sc.Iterations != 1 || sc.ByCategory[CategorySuccess] != 1 {
		t.Errorf("iterations %d, successes %d; want 1 and 1", sc.Iterations, sc.ByCategory[CategorySuccess])
	}
}

// Ground: contract — the in-flight budget: a hung target holds an iteration on its step for that
// step's timeout and the iteration's slot for all of them, so the check counts
// Σ step timeouts + ReleaseMargin per step.
func TestBudget_ScenarioCountsTheSumOfStepTimeouts(t *testing.T) {
	flow := func(second time.Duration) Call {
		return Call{
			Method: "a/One", Scenario: "flow", Timeout: time.Second,
			Next:   []Step{{Method: "a/Two", Timeout: second}},
			Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Second}},
		}
	}

	// 100 rps × (1s + 1s + 2 × 100ms) = 220, plus one for the window's edge.
	if err := newWithCap(221, flow(time.Second)); err != nil {
		t.Errorf("cap 221: %v, want it accepted: it is the need", err)
	}

	err := newWithCap(220, flow(time.Second))

	var budget *InFlightBudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("cap 220: err = %v, want *InFlightBudgetError: need - 1 fails", err)
	}
	if budget.Need != 221 || budget.Cap != 220 || budget.PeakRPS != 100 {
		t.Errorf("budget = %+v, want Need 221, Cap 220, PeakRPS 100", *budget)
	}
	if text := budget.Error(); !strings.Contains(text, "Σ step timeouts") || strings.Contains(text, "rps × timeout") {
		t.Errorf("%q, want it to name Σ step timeouts and not rps × timeout", text)
	}
	if need := InFlightNeed([]Call{flow(time.Second)}); need != 221 {
		t.Errorf("InFlightNeed = %d, want 221", need)
	}

	err = newWithCap(1_000_000, flow(0))
	if !errors.As(err, &budget) || !slices.Contains(budget.Unbounded, "a/Two") {
		t.Errorf("a step without a timeout: err = %v, want Unbounded naming a/Two", err)
	}
}

// Ground: contract — InFlightCapError.OverDeadline puts a cap hit on the generator: it counts
// the slots held past their deadline, and the deadline that counts is the one of the step
// holding the slot at that moment, not of step 1.
func TestCapHit_OverDeadlineUsesTheStepHoldingTheSlot(t *testing.T) {
	// stepOneEnded is when step 1 reports it ended: now, or a second ago, which
	// puts step 2's schedule, and so its deadline, in the past too. The tick is
	// in the past either way, so step 1's own deadline has passed.
	hit := func(t *testing.T, stepTwo time.Duration, stepOneEnded time.Time) *InFlightCapError {
		t.Helper()

		entered := make(chan struct{})
		sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
			"a/One": func(context.Context, Request) (Outcome, error) {
				return Outcome{Category: CategorySuccess, DoneAt: stepOneEnded}, nil
			},
			"a/Two": hang(entered),
		}}

		_, err := runPool(t.Context(), sender, 1, func(in chan<- Request) {
			tick := time.Now().Add(-time.Second)
			in <- scenarioReq(tick, "flow", Step{Method: "a/One", Timeout: 100 * time.Millisecond}, Step{Method: "a/Two", Timeout: stepTwo})
			if !await(entered) {
				return
			}
			in <- Request{Method: "b/Other", ScheduledAt: time.Now()}
		})

		var capHit *InFlightCapError
		if !errors.As(err, &capHit) {
			t.Fatalf("err = %v, want the cap hit", err)
		}

		return capHit
	}

	// Step 1's deadline passed long before the hit; step 2's (2s on) did not.
	if got := hit(t, 2*time.Second, time.Now()); got.OverDeadline != 0 {
		t.Errorf("over deadline %d, want 0: the iteration is inside its step 2's budget", got.OverDeadline)
	}
	// Step 2 was scheduled a second ago with 60ms: its own deadline passed too.
	if got := hit(t, 60*time.Millisecond, time.Now().Add(-time.Second)); got.OverDeadline != 1 {
		t.Errorf("over deadline %d, want 1: step 2 held the slot past its own deadline", got.OverDeadline)
	}
}

// Ground: contract — a timed-out call is censored by its deadline, not by when it came back
// (past the deadline nobody was listening): so is the iteration it ended.
func TestScenario_ATimedOutStepCensorsTheIterationAtItsDeadline(t *testing.T) {
	tick := time.Now()
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": reply(10*time.Millisecond, CategorySuccess),
		// Back 50ms after its deadline.
		"a/Two": func(_ context.Context, req Request) (Outcome, error) {
			return Outcome{Category: CategoryTimeout, DoneAt: req.Deadline.Add(50 * time.Millisecond)}, nil
		},
	}}

	results, err := runPool(t.Context(), sender, 4, func(in chan<- Request) {
		in <- scenarioReq(tick, "flow", Step{Method: "a/One", Timeout: time.Second}, Step{Method: "a/Two", Timeout: 100 * time.Millisecond})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	two, it := only(t, results, "a/Two"), only(t, results, "flow")
	if it.Category != CategoryTimeout || it.IterationStep != 1 {
		t.Fatalf("iteration = %v at step %d, want timed out at step 2 (index 1)", it.Category, it.IterationStep)
	}
	if !it.Deadline.Equal(two.Deadline) {
		t.Errorf("iteration deadline %v, want step 2's %v", it.Deadline, two.Deadline)
	}
	if want := two.Deadline.Sub(tick); it.CensorThreshold() != want || it.CensorThreshold() >= it.Latency() {
		t.Errorf("iteration censored at %v, want step 2's deadline − tick = %v, below its latency %v",
			it.CensorThreshold(), want, it.Latency())
	}

	sc := scenarioOf(t, scenarioStats(results, "flow", []string{"a/One", "a/Two"}, tick, 0), "flow")
	if sc.Latencies != 1 || sc.Censored != 1 {
		t.Errorf("iteration observations %d, censored %d; want 1 and 1", sc.Latencies, sc.Censored)
	}
}

// Ground: contract — the result of a step goes to the collector after the slot is released, as a
// plain call's does: a collector that is not reading never delays step k+1, nor shows as
// its start lag (which would pass a slow collector off as a late generator).
func TestScenario_ASlowCollectorDoesNotDelayTheNextStep(t *testing.T) {
	stepTwo := make(chan struct{})
	var once sync.Once

	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": reply(5*time.Millisecond, CategorySuccess),
		"a/Two": func(context.Context, Request) (Outcome, error) {
			once.Do(func() { close(stepTwo) })

			return Outcome{Category: CategorySuccess}, nil
		},
	}}

	in := make(chan Request, 1)
	in <- scenarioReq(time.Now(), "flow", Step{Method: "a/One", Timeout: time.Second}, Step{Method: "a/Two", Timeout: time.Second})
	close(in)

	out := make(chan Result) // nobody reads until step 2 has begun

	var (
		got     []Result
		reached bool
	)

	done := make(chan struct{})

	go func() {
		defer close(done)

		select {
		case <-stepTwo:
			reached = true
		case <-time.After(2 * time.Second):
		}

		for r := range out {
			got = append(got, r)
		}
	}()

	if err := NewWorkerPool(sender, 4).Run(t.Context(), in, out); err != nil {
		t.Fatalf("run: %v", err)
	}
	close(out)
	<-done

	if !reached {
		t.Fatal("step 2 did not begin while the collector was not reading: step 1's result is held up in front of it")
	}
	if lag := only(t, got, "a/Two").QueueTime(); lag > 50*time.Millisecond {
		t.Errorf("step 2 queue time %v, want about 0", lag)
	}
}

// Ground: contract — an iteration gives its slot back before its results go to the collector: with
// one slot, the next request goes out while nobody reads the results.
func TestScenario_TheSlotIsBackBeforeTheResultsAreDelivered(t *testing.T) {
	entered := make(chan struct{})
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/Plain": func(context.Context, Request) (Outcome, error) {
			close(entered)

			return Outcome{Category: CategorySuccess}, nil
		},
	}}

	two := make(chan struct{})
	sender.do["a/Two"] = func(context.Context, Request) (Outcome, error) {
		close(two)

		return Outcome{Category: CategorySuccess}, nil
	}

	pool := NewWorkerPool(sender, 1)
	in := make(chan Request)

	go func() {
		defer close(in)

		in <- scenarioReq(time.Now(), "flow", Step{Method: "a/One", Timeout: time.Second}, Step{Method: "a/Two", Timeout: time.Second})

		// The second request is sent once the slot is back, not while the results
		// wait: with one slot, a request that finds it taken stops the run.
		<-two

		for end := time.Now().Add(time.Second); pool.inFlight.Load() != 0 && time.Now().Before(end); {
			runtime.Gosched()
		}
		in <- Request{Method: "a/Plain", ScheduledAt: time.Now()}
	}()

	out := make(chan Result) // nobody reads until the plain request is in the sender

	var reached bool

	done := make(chan struct{})

	go func() {
		defer close(done)

		select {
		case <-entered:
			reached = true
		case <-time.After(2 * time.Second):
		}

		for range out {
		}
	}()

	if err := pool.Run(t.Context(), in, out); err != nil {
		t.Errorf("run: %v: the iteration still held the slot when the next request came", err)
	}
	close(out)
	<-done

	if !reached {
		t.Error("the next request did not go out while the collector was not reading: the iteration still held the slot")
	}
}

// Ground: contract — Report.Sent, Failed and NotSent count the calls of the steps, as they count a
// plain call's; an iteration is only in the scenario's entry. A step that did not go out is
// NotSent, with its cause, on the run and on its method; the scenario is not a method.
func TestScenario_TheIterationIsNotCountedAsACall(t *testing.T) {
	tick := time.Now()
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": reply(10*time.Millisecond, CategorySuccess),
		"a/Two": func(context.Context, Request) (Outcome, error) {
			return Outcome{Category: CategoryTimeout, NotSent: true, NotSentOn: BlockedOnConnection, ConnWait: 90 * time.Millisecond}, nil
		},
	}}

	results, err := runPool(t.Context(), sender, 4, func(in chan<- Request) {
		in <- scenarioReq(tick, "flow", Step{Method: "a/One", Timeout: time.Second}, Step{Method: "a/Two", Timeout: 100 * time.Millisecond})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	report := scenarioStats(results, "flow", []string{"a/One", "a/Two"}, tick, 0)
	if report.Sent != 1 || report.Failed != 0 || report.NotSent != 1 || report.NotSentConnection != 1 {
		t.Errorf("sent %d, failed %d, not sent %d (connection %d); want 1, 0, 1, 1",
			report.Sent, report.Failed, report.NotSent, report.NotSentConnection)
	}
	if m, ok := methodRow(report, "a/Two"); !ok || m.Sent != 0 || m.NotSent != 1 || m.NotSentConnection != 1 {
		t.Errorf("step 2's row = %+v (found %v), want sent 0, not sent 1 on the connection", m, ok)
	}
	if _, ok := methodRow(report, "flow"); ok {
		t.Error("the scenario has a method row: an iteration is not a call")
	}
	sc := scenarioOf(t, report, "flow")
	if sc.Iterations != 1 || sc.ByCategory[CategoryTimeout] != 1 {
		t.Errorf("iterations %d, timed out %d; want 1 and 1", sc.Iterations, sc.ByCategory[CategoryTimeout])
	}
	// A step never sent ends the iteration there; the iteration is no timed call.
	wantSteps(t, sc.Steps, [][5]int{{1, 0, 0, 0, 0}, {1, 0, 0, 1, 0}})
	if sc.Latencies != 0 || sc.Censored != 0 {
		t.Errorf("iteration observations %d, censored %d; want 0 and 0: a not-sent iteration is outside the percentiles", sc.Latencies, sc.Censored)
	}
}

// Ground: contract — Report.Scheduled counts ticks, so it counts iterations; Sent counts the
// calls of the steps; every step is a method row and the scenario's entry says how far
// each iteration got. The whole wiring: engine, dispatcher, pool, stats.
func TestScenario_ARunCountsIterationsAndCalls(t *testing.T) {
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": reply(2*time.Millisecond, CategorySuccess),
		"a/Two": reply(2*time.Millisecond, CategorySuccess),
	}}

	eng, err := New(Options{
		Calls: []Call{{
			Method: "a/One", Scenario: "flow", Timeout: time.Second,
			Next:   []Step{{Method: "a/Two", Timeout: time.Second}},
			Stages: []Stage{{StartRPS: 50, TargetRPS: 50, Duration: 100 * time.Millisecond}},
		}},
		Sender:      sender,
		MaxInFlight: 256,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	report := eng.Report()
	if report.Scheduled != 5 || report.Sent != 10 {
		t.Errorf("scheduled %d, sent %d; want 5 iterations and 10 calls", report.Scheduled, report.Sent)
	}
	if len(report.Methods) != 2 {
		t.Errorf("%d method rows, want the 2 steps", len(report.Methods))
	}
	for _, name := range []string{"a/One", "a/Two"} {
		if m, ok := methodRow(report, name); !ok || m.Sent != 5 {
			t.Errorf("%s sent %d (found %v), want 5", name, m.Sent, ok)
		}
	}
	sc := scenarioOf(t, report, "flow")
	if sc.Iterations != 5 || sc.ByCategory[CategorySuccess] != 5 {
		t.Errorf("iterations %d, successes %d; want 5 and 5", sc.Iterations, sc.ByCategory[CategorySuccess])
	}
	wantSteps(t, sc.Steps, [][5]int{{5, 0, 0, 0, 0}, {5, 0, 0, 0, 0}})
}

// Ground: contract — the per-second timelines are reserved for what can still be running past the
// plan: after the last tick an iteration lives up to the Σ of its timeouts, so the
// reserve is the longest iteration's, not the longest single timeout, or the tail steps
// land in OutsideTimeline.
func TestScenario_TheTimelineReservesTheLongestIteration(t *testing.T) {
	stages := []Stage{{StartRPS: 1, TargetRPS: 1, Duration: 2 * time.Second}}

	eng, err := New(Options{
		Calls: []Call{
			{Method: "a/Plain", Timeout: time.Second, Stages: stages},
			{
				Method: "a/One", Scenario: "flow", Timeout: 10 * time.Second,
				Next:   []Step{{Method: "a/Two", Timeout: 10 * time.Second}, {Method: "a/Three", Timeout: 10 * time.Second}},
				Stages: stages,
			},
		},
		Sender:      FakeSender{},
		MaxInFlight: 1000,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	// The plan 2s, the iteration 30s, the slack 10s.
	if got, want := eng.timelineSpan(), 42*time.Second; got != want {
		t.Errorf("timeline span %v, want %v", got, want)
	}

	plain, err := New(Options{
		Calls:       []Call{{Method: "a/Plain", Timeout: time.Second, Stages: stages}},
		Sender:      FakeSender{},
		MaxInFlight: 1000,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if got, want := plain.timelineSpan(), 13*time.Second; got != want {
		t.Errorf("plain call: timeline span %v, want %v (unchanged)", got, want)
	}
}

// Ground: contract — scenario names and methods are one namespace in the report: a repeat is a
// config error naming both places (a scenario row and a method row share a key space), and
// a call with steps needs a name.
func TestNew_ScenarioNamesAndMethodsShareOneNamespace(t *testing.T) {
	stages := []Stage{{StartRPS: 1, TargetRPS: 1, Duration: time.Second}}
	plain := func(method string) Call {
		return Call{Method: method, Timeout: time.Second, Stages: stages}
	}
	flow := func(name string, methods ...string) Call {
		steps := make([]Step, 0, len(methods)-1)
		for _, m := range methods[1:] {
			steps = append(steps, Step{Method: m, Timeout: time.Second})
		}

		return Call{Method: methods[0], Scenario: name, Next: steps, Timeout: time.Second, Stages: stages}
	}
	noName := flow("", "a/One", "a/Two")

	for _, tc := range []struct {
		name  string
		calls []Call
		want  error
		text  []string // what the message must name
	}{
		{"distinct names and methods", []Call{plain("a/Plain"), flow("s1", "a/One", "a/Two"), flow("s2", "a/Three", "a/Four")}, nil, nil},
		{"steps without a name", []Call{noName}, ErrScenarioNeedsName, nil},
		{"a name and no steps", []Call{flow("s1", "a/One")}, ErrScenario, nil},
		{"name is a call's method", []Call{plain("a/Name"), flow("a/Name", "a/One", "a/Two")}, ErrDuplicateMethod, []string{"a/Name", "call 0", "call 1"}},
		{"name is a step's method", []Call{flow("s1", "a/One", "a/Two"), flow("a/Two", "a/Three", "a/Four")}, ErrDuplicateMethod, []string{"a/Two", "call 0", "call 1"}},
		{"name is its own first method", []Call{flow("a/One", "a/One", "a/Two")}, ErrDuplicateMethod, []string{"a/One"}},
		{"two scenarios, one name", []Call{flow("s1", "a/One", "a/Two"), flow("s1", "a/Three", "a/Four")}, ErrDuplicateMethod, []string{"s1", "call 0", "call 1"}},
		{"method in two scenarios", []Call{flow("s1", "a/One", "a/Two"), flow("s2", "a/Three", "a/Two")}, ErrDuplicateMethod, []string{"a/Two", "call 0", "call 1"}},
		{"method as a call and as a step", []Call{plain("a/Two"), flow("s1", "a/One", "a/Two")}, ErrDuplicateMethod, []string{"a/Two", "call 0", "call 1"}},
		{"method twice in one scenario", []Call{flow("s1", "a/One", "a/Two", "a/One")}, ErrDuplicateMethod, []string{"a/One", "call 0"}},
		{"first method repeated as a step", []Call{flow("s1", "a/One", "a/One")}, ErrDuplicateMethod, []string{"a/One", "call 0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckOptions(Options{Calls: tc.calls, MaxInFlight: 100})
			if tc.want == nil {
				if err != nil {
					t.Fatalf("err = %v, want none", err)
				}

				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			for _, part := range tc.text {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("%q does not name %q", err, part)
				}
			}
		})
	}
}

// okIteration is the results of a two-step iteration of scenario "flow" that
// went well: step 1 takes d1 from the tick, step 2 d2 more.
func okIteration(tick time.Time, d1, d2 time.Duration) []Result {
	deadline := tick.Add(2 * time.Second)

	return []Result{
		{
			Method: "a/One", ScheduledAt: tick, BegunAt: tick, Deadline: deadline, IterationTick: tick,
			Outcome: Outcome{Category: CategorySuccess, SentAt: tick, DoneAt: tick.Add(d1)},
		},
		{
			Method: "a/Two", ScheduledAt: tick.Add(d1), BegunAt: tick.Add(d1), Deadline: deadline, IterationTick: tick,
			Outcome: Outcome{Category: CategorySuccess, SentAt: tick.Add(d1), DoneAt: tick.Add(d1 + d2)},
		},
		{
			Method: "flow", Iteration: true, IterationStep: 1, ScheduledAt: tick, BegunAt: tick, Deadline: deadline, IterationTick: tick,
			Outcome: Outcome{Category: CategorySuccess, SentAt: tick, DoneAt: tick.Add(d1 + d2)},
		},
	}
}

// Ground: contract — warm-up is decided by the tick: an iteration that started inside the warm-up
// is out of the measured numbers with all its steps, even a step that began after the
// warm-up ended — a step's own schedule is the end of the step before it.
func TestScenario_WarmUpIsDecidedByTheTick(t *testing.T) {
	start := time.Now()
	inWarmup := okIteration(start.Add(900*time.Millisecond), 300*time.Millisecond, 100*time.Millisecond)
	measured := okIteration(start.Add(1100*time.Millisecond), 300*time.Millisecond, 100*time.Millisecond)

	if second := inWarmup[1].ScheduledAt.Sub(start); second < time.Second {
		t.Fatalf("premise: step 2 of the warm-up iteration is scheduled at %v, want past the 1s warm-up", second)
	}

	report := scenarioStats(append(inWarmup, measured...), "flow", []string{"a/One", "a/Two"}, start, time.Second)
	if report.WarmupSent != 2 || report.Sent != 2 {
		t.Errorf("warm-up sent %d, sent %d; want 2 and 2: both steps of the first iteration are warm-up", report.WarmupSent, report.Sent)
	}
	sc := scenarioOf(t, report, "flow")
	if sc.Iterations != 1 || sc.Latencies != 1 {
		t.Errorf("iterations %d, observations %d; want 1 and 1: the warm-up iteration is out", sc.Iterations, sc.Latencies)
	}
	wantSteps(t, sc.Steps, [][5]int{{1, 0, 0, 0, 0}, {1, 0, 0, 0, 0}})
}

// Ground: contract — the scenario entry's counters and percentiles follow the rule of a call:
// successes timed, a timed-out iteration a lower bound at its deadline, another failure
// counted by its category and not timed.
func TestScenarioStats_PercentilesOfTheIterationEntry(t *testing.T) {
	start := time.Now()
	ms := time.Millisecond

	var results []Result

	iteration := func(i int, category Category, latency time.Duration) {
		tick := start.Add(time.Duration(i) * ms)
		step := 0 // a success ends at the last of the two steps
		if category == CategorySuccess {
			step = 1
		}
		results = append(results, Result{
			Method: "flow", Iteration: true, IterationStep: step, ScheduledAt: tick, BegunAt: tick, IterationTick: tick,
			Deadline: tick.Add(300 * ms),
			Outcome:  Outcome{Category: category, SentAt: tick, DoneAt: tick.Add(latency)},
		})
	}
	for i := range 6 {
		iteration(i, CategorySuccess, 10*ms)
	}
	for i := 6; i < 9; i++ {
		iteration(i, CategorySuccess, 100*ms)
	}
	iteration(9, CategoryTimeout, 350*ms)
	iteration(10, CategoryOverload, 5*ms)

	sc := scenarioOf(t, scenarioStats(results, "flow", []string{"a/One", "a/Two"}, start, 0), "flow")
	if sc.Iterations != 11 {
		t.Errorf("iterations %d, want 11: failed ones count", sc.Iterations)
	}
	if sc.ByCategory[CategorySuccess] != 9 || sc.ByCategory[CategoryTimeout] != 1 || sc.ByCategory[CategoryOverload] != 1 {
		t.Errorf("by category %v, want 9 successes, 1 timeout, 1 overload", sc.ByCategory)
	}
	if sc.Latencies != 10 || sc.Censored != 1 {
		t.Errorf("observations %d, censored %d; want 10 and 1: the overload is not timed", sc.Latencies, sc.Censored)
	}
	near := func(got, want time.Duration) bool { return got >= want-ms && got <= want+ms }
	if !sc.P50.Defined || !sc.P50.Exact || !near(sc.P50.Value, 10*ms) {
		t.Errorf("p50 = %+v, want an exact 10ms", sc.P50)
	}
	if !sc.P99.Defined || sc.P99.Exact || !near(sc.P99.Value, 300*ms) {
		t.Errorf("p99 = %+v, want the timeout's 300ms bound, not exact", sc.P99)
	}
}

// Ground: contract — the invariants of ScenarioStepReport over any mix of outcomes: every step's
// counters equal what a plain model of the iterations gives, reached(k+1) =
// reached(k) − failed(k) − aborted(k) − not sent(k), and reached(k) + not reached(k) =
// iterations. A step never sent is its own outcome: it is no failure and no abort.
func TestScenarioStats_InvariantsOverARandomSequence(t *testing.T) {
	const steps = 3

	rng := rand.New(rand.NewPCG(7, 11))
	start := time.Now()
	failures := []Category{
		CategoryOverload, CategoryServerFault, CategoryTimeout, CategoryUnreachable,
		CategoryCutOff, CategoryClientError, CategoryBadResponse, CategoryClientFault, CategoryUnknown,
	}
	methods := []string{"a/One", "a/Two", "a/Three"}

	var (
		results                                               []Result
		wantReached, wantFailed, wantAborted, wantNotSent, nr [steps]int
	)

	const iterations = 400

	for i := range iterations {
		tick := start.Add(time.Duration(i) * time.Millisecond)
		deadline := tick.Add(time.Second)
		ended := rng.IntN(steps) // the step the iteration ends at

		// How it ends: a success on the last step, a failure, an abort or a
		// step never sent at a step, or an abort before a step (never before
		// the first).
		var (
			category Category
			launched int // steps whose calls ran, or were handed to the sender
			notSent  bool
		)

		switch rng.IntN(5) {
		case 0:
			ended, category, launched = steps-1, CategorySuccess, steps
		case 1:
			category, launched = failures[rng.IntN(len(failures))], ended+1
			wantFailed[ended]++
		case 2:
			category, launched = CategoryAborted, ended+1
			wantAborted[ended]++
		case 3:
			category, launched, notSent = CategoryTimeout, ended+1, true
			wantNotSent[ended]++
		default:
			ended = 1 + rng.IntN(steps-1)
			category, launched = CategoryAborted, ended
			wantAborted[ended]++
		}

		for k := 0; k <= ended; k++ {
			wantReached[k]++
		}
		for k := ended + 1; k < steps; k++ {
			nr[k]++
		}

		for k := range launched {
			last := notSent && k == launched-1
			results = append(results, Result{
				Method: methods[k], ScheduledAt: tick, BegunAt: tick, Deadline: deadline, IterationTick: tick,
				Outcome: Outcome{
					Category: CategorySuccess, SentAt: tick, DoneAt: tick.Add(time.Millisecond),
					NotSent: last, NotSentOn: BlockedOnConnection,
				},
			})
		}
		results = append(results, Result{
			Method: "flow", Iteration: true, IterationStep: ended, ScheduledAt: tick, BegunAt: tick,
			Deadline: deadline, IterationTick: tick,
			Outcome: Outcome{
				Category: category, SentAt: tick, DoneAt: tick.Add(5 * time.Millisecond),
				NotSent: notSent, NotSentOn: BlockedOnConnection,
			},
		})
	}

	rng.Shuffle(len(results), func(i, j int) { results[i], results[j] = results[j], results[i] })

	sc := scenarioOf(t, scenarioStats(results, "flow", methods, start, 0), "flow")
	if sc.Iterations != iterations {
		t.Fatalf("iterations %d, want %d", sc.Iterations, iterations)
	}

	want := make([][5]int, steps)
	for k := range steps {
		want[k] = [5]int{wantReached[k], wantFailed[k], wantAborted[k], wantNotSent[k], nr[k]}
	}
	wantSteps(t, sc.Steps, want)

	for k, step := range sc.Steps {
		if step.Reached+step.NotReached != sc.Iterations {
			t.Errorf("step %d: reached %d + not reached %d != iterations %d", k+1, step.Reached, step.NotReached, sc.Iterations)
		}
		if k+1 < len(sc.Steps) {
			left := step.Reached - step.FailedHere - step.AbortedHere - step.NotSentHere
			if next := sc.Steps[k+1].Reached; next != left {
				t.Errorf("step %d: reached %d != reached %d - failed %d - aborted %d - not sent %d",
					k+2, next, step.Reached, step.FailedHere, step.AbortedHere, step.NotSentHere)
			}
		}
	}
}

// Ground: contract — a run that mixes a plain call and a scenario names each term by its kind in
// the budget message: «rps × timeout» for the call, «rps × Σ step timeouts» for the scenario.
func TestBudget_AMixedRunNamesEachTermByItsKind(t *testing.T) {
	stages := []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Second}}
	plain := Call{Method: "a/Plain", Timeout: time.Second, Stages: stages}
	flow := Call{
		Method: "a/One", Scenario: "flow", Timeout: time.Second,
		Next:   []Step{{Method: "a/Two", Timeout: time.Second}},
		Stages: stages,
	}

	var budget *InFlightBudgetError

	err := newWithCap(10, plain, flow)
	if !errors.As(err, &budget) {
		t.Fatalf("err = %v, want *InFlightBudgetError", err)
	}

	text := budget.Error()
	if !strings.Contains(text, "rps × timeout") || !strings.Contains(text, "rps × Σ step timeouts") {
		t.Errorf("%q, want both «rps × timeout» (the call) and «rps × Σ step timeouts» (the scenario)", text)
	}
}

// await waits for a signal from a step of the test; a step that never begins
// ends the wait, so the test fails on its own assert and does not hang.
func await(signal <-chan struct{}) bool {
	select {
	case <-signal:
		return true
	case <-time.After(3 * time.Second):
		return false
	}
}
