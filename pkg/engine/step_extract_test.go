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

// Extract in a scenario (v0.9 unit 4b, the engine side). The engine knows no
// proto: Extract and Build are functions the CLI makes, and here they are plain
// text. Stub: Call.Extract, Call.Values and Step.Extract exist and iterate does
// not look at them, so every test that expects a value to travel is red until
// the code; the guard tests (a plain call, a scenario with no extract) are green
// now and stay so.

var errNoValue = errors.New("extract tx: id not in the response")

// kept is a sender answer: a success from the target whose reply is what body
// says about the request, kept only when the request asked for it, as the gRPC
// sender does. A reply takes after of its own time.
func kept(after time.Duration, body func(Request) string) func(context.Context, Request) (Outcome, error) {
	return func(ctx context.Context, req Request) (Outcome, error) {
		select {
		case <-time.After(after):
		case <-ctx.Done():
			return Outcome{}, ctx.Err()
		}

		out := Outcome{Category: CategorySuccess, Code: "OK", CodeFromTarget: true}
		if req.KeepResponse {
			out.Response = []byte(body(req))
		}

		return out, nil
	}
}

// replyOfSeq is a reply holding the iteration's seq: w1, w2.
func replyOfSeq(req Request) string { return "w" + strconv.FormatUint(req.Seq, 10) }

// replyOfBody is a reply holding the request body it answers.
func replyOfBody(req Request) string { return "t-" + string(req.Payload) }

// take is an Extract that keeps the reply, as it is, under index.
func take(index int) func([]byte, []string) error {
	return func(resp []byte, vals []string) error {
		vals[index] = string(resp)

		return nil
	}
}

// join is a Build: the tag and every value extracted so far.
func join(tag string) func(uint64, []string) []byte {
	return func(_ uint64, vals []string) []byte { return []byte(tag + strings.Join(vals, "+")) }
}

// extractRequest is the first request of an iteration of call, as newRequest
// makes it: the call's own KeepResponse and the seq.
func extractRequest(tick time.Time, seq uint64, call *Call) Request {
	return Request{
		Method: call.Method, scenario: call, ScheduledAt: tick, Seq: seq, KeepResponse: call.KeepResponse,
	}
}

// Ground: concurrency — two iterations run at once and each reads the values its
// own step 1 got: both are held in step 1's Extract until the other has stored
// its value, so one slice shared by the call would give both the later reply.
// A value goes from step 1 and from a step to every step after it.
// Mutations: skip Extract; one vals per Call instead of per iteration.
func TestIterate_ExtractedValuesReachTheLaterStepsOfTheirOwnIteration(t *testing.T) {
	var (
		stored  atomic.Int32
		both    = make(chan struct{})
		release sync.Once
	)

	call := &Call{
		Method: "a/One", Scenario: "flow", KeepResponse: true, Values: 2,
		Extract: func(resp []byte, vals []string) error {
			vals[0] = string(resp)
			if stored.Add(1) == 2 {
				release.Do(func() { close(both) })
			}

			select {
			case <-both:
			case <-time.After(5 * time.Second):
			}

			return nil
		},
		Next: []Step{
			{Method: "a/Two", Build: join("two:"), Extract: take(1)},
			{Method: "a/Three", Build: join("three:")},
		},
	}
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": kept(time.Millisecond, replyOfSeq),
		"a/Two": kept(time.Millisecond, replyOfBody),
	}}

	tick := time.Now()

	_, err := runPool(t.Context(), sender, 4, func(in chan<- Request) {
		in <- extractRequest(tick, 1, call)
		in <- extractRequest(tick, 2, call)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	bodies := map[string][]string{}

	sender.mu.Lock()
	for i := range sender.seen {
		bodies[sender.seen[i].Method] = append(bodies[sender.seen[i].Method], string(sender.seen[i].Payload))
	}
	sender.mu.Unlock()

	two, three := bodies["a/Two"], bodies["a/Three"]
	slices.Sort(two)
	slices.Sort(three)

	// Step 2 reads what step 1 extracted; step 3 reads that and what step 2 did.
	if want := []string{"two:w1+", "two:w2+"}; !slices.Equal(two, want) {
		t.Errorf("step 2 sent %q, want %q: each iteration its own step 1 value", two, want)
	}
	if want := []string{"three:w1+t-two:w1+", "three:w2+t-two:w2+"}; !slices.Equal(three, want) {
		t.Errorf("step 3 sent %q, want %q: the value of step 1 and of step 2, of one iteration", three, want)
	}
}

// Ground: contract — Request.KeepResponse is true on the request of a call that
// extracts, which the CLI sets on the call, and on a step that extracts, which
// iterate sets; it is false on a step that does not, on a plain call and on a
// scenario that extracts nothing: no reply body is kept without a use for it.
// Mutations: KeepResponse not set on a step; true on every request.
func TestEngine_KeepResponseIsOnlyOnTheRequestsThatExtract(t *testing.T) {
	for _, tc := range []struct {
		name string
		call Call
		want map[string]bool
	}{
		{
			"a scenario with extracts",
			Call{
				Method: "a/One", Scenario: "flow", Timeout: stepTimeout, KeepResponse: true, Values: 1,
				Extract: take(0), Stages: []Stage{atRate(100, 3)},
				Next: []Step{
					{Method: "a/Two", Timeout: stepTimeout, Build: join("two:"), Extract: func([]byte, []string) error { return nil }},
					{Method: "a/Three", Timeout: stepTimeout, Build: join("three:")},
				},
			},
			map[string]bool{"a/One": true, "a/Two": true, "a/Three": false},
		},
		{
			"a scenario with no extract",
			Call{
				Method: "a/One", Scenario: "flow", Timeout: stepTimeout, Stages: []Stage{atRate(100, 3)},
				Next: []Step{{Method: "a/Two", Timeout: stepTimeout, Payload: []byte("x")}},
			},
			map[string]bool{"a/One": false, "a/Two": false},
		},
		{
			"a plain call",
			Call{Method: "a/Plain", Timeout: stepTimeout, Stages: []Stage{atRate(100, 3)}},
			map[string]bool{"a/Plain": false},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := &scripted{}

			eng, err := New(Options{Calls: []Call{tc.call}, Sender: sender, MaxInFlight: 64})
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			if err = eng.Run(t.Context()); err != nil {
				t.Fatalf("run: %v", err)
			}

			sender.mu.Lock()
			defer sender.mu.Unlock()

			seen := map[string]int{}

			for i := range sender.seen {
				req := sender.seen[i]
				seen[req.Method]++

				if want := tc.want[req.Method]; req.KeepResponse != want {
					t.Errorf("%s: KeepResponse = %v, want %v", req.Method, req.KeepResponse, want)
				}
			}
			for method := range tc.want {
				if seen[method] == 0 {
					t.Errorf("%s was never sent: the check above saw nothing of it", method)
				}
			}
		})
	}
}

// Ground: contract — no reply body travels to the collector: the results of a
// step that extracted, and the iteration's, carry no Response, however the
// sender kept it; the next step's body is the only place it went.
// Mutation: keep Response in the results.
func TestIterate_ResultsCarryNoResponse(t *testing.T) {
	call := &Call{
		Method: "a/One", Scenario: "flow", KeepResponse: true, Values: 2, Extract: take(0),
		Next: []Step{
			{Method: "a/Two", Build: join("two:"), Extract: take(1)},
			{Method: "a/Three", Build: join("three:")},
		},
	}
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": kept(time.Millisecond, replyOfSeq),
		"a/Two": kept(time.Millisecond, replyOfBody),
	}}

	results, err := runPool(t.Context(), sender, 4, func(in chan<- Request) {
		in <- extractRequest(time.Now(), 1, call)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("%d results, want 3 steps and the iteration", len(results))
	}

	for i := range results {
		if results[i].Response != nil {
			t.Errorf("%s (iteration %v) carries a Response of %d bytes, want none", results[i].Method, results[i].Iteration, len(results[i].Response))
		}
	}
}

// Ground: contract — a reply that lacks a value is not accepted by the client:
// the step's result is bad_response, its code stays the target's OK, the
// iteration ends there (failed_here at that step, the rest not reached, nothing
// of them sent), and the answer's time is real and counted in the method's
// bad_response row with the OK code from the target.
// Mutations: ignore the error of Extract; CodeFromTarget=false.
func TestIterate_AMissingValueIsABadResponseThatEndsTheIteration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		call       Call
		wantSent   []string
		wantMethod string
		wantStep   int
		wantSteps  [][5]int
	}{
		{
			"step 1 lacks it",
			Call{
				Method: "a/One", Scenario: "flow", KeepResponse: true, Values: 1,
				Extract: func([]byte, []string) error { return errNoValue },
				Next:    []Step{{Method: "a/Two", Build: join("two:")}},
			},
			[]string{"a/One"}, "a/One", 0, [][5]int{{1, 1, 0, 0, 0}, {0, 0, 0, 0, 1}},
		},
		{
			"a later step lacks it",
			Call{
				Method: "a/One", Scenario: "flow", KeepResponse: true, Values: 1, Extract: take(0),
				Next: []Step{
					{Method: "a/Two", Build: join("two:"), Extract: func([]byte, []string) error { return errNoValue }},
					{Method: "a/Three", Build: join("three:")},
				},
			},
			[]string{"a/One", "a/Two"}, "a/Two", 1, [][5]int{{1, 0, 0, 0, 0}, {1, 1, 0, 0, 0}, {0, 0, 0, 0, 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const answer = 20 * time.Millisecond

			sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
				"a/One": kept(answer, replyOfSeq),
				"a/Two": kept(answer, replyOfBody),
			}}
			tick := time.Now()

			results, err := runPool(t.Context(), sender, 4, func(in chan<- Request) {
				in <- extractRequest(tick, 1, &tc.call)
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}

			if got := sender.methods(); !slices.Equal(got, tc.wantSent) {
				t.Errorf("sent %v, want %v: nothing after the step that lacks the value", got, tc.wantSent)
			}

			step := only(t, results, tc.wantMethod)
			if step.Category != CategoryBadResponse {
				t.Errorf("the step's category = %v, want bad_response", step.Category)
			}
			if !errors.Is(step.Err, errNoValue) {
				t.Errorf("the step's Err = %v, want the error of Extract", step.Err)
			}
			if step.Code != "OK" || !step.CodeFromTarget {
				t.Errorf("the step's code = %q from target %v, want OK from the target: that is what came back", step.Code, step.CodeFromTarget)
			}
			if lat := step.Latency(); lat < answer {
				t.Errorf("the step's latency = %v, want at least the %v the target took: the answer's time is real", lat, answer)
			}

			it := only(t, results, "flow")
			if !it.Iteration || it.Category != CategoryBadResponse || it.IterationStep != tc.wantStep {
				t.Errorf("iteration result = iteration %v, category %v, step %d; want true, bad_response, %d",
					it.Iteration, it.Category, it.IterationStep, tc.wantStep)
			}
			if it.Code != "OK" || !it.CodeFromTarget {
				t.Errorf("the iteration's code = %q from target %v, want OK from the target", it.Code, it.CodeFromTarget)
			}

			report := scenarioStats(results, "flow", []string{"a/One", "a/Two", "a/Three"}[:len(tc.wantSteps)], tick, 0)
			wantSteps(t, scenarioOf(t, report, "flow").Steps, tc.wantSteps)

			row, ok := methodRow(report, tc.wantMethod)
			if !ok {
				t.Fatalf("no row for %s", tc.wantMethod)
			}
			if row.BadResponse.Count != 1 {
				t.Errorf("%s bad_response count = %d, want 1: the latency stays in its answers", tc.wantMethod, row.BadResponse.Count)
			}
			if want := (CodeCount{Code: "OK", Count: 1, FromTarget: true}); !slices.Contains(row.FailureCodes, want) {
				t.Errorf("%s failure codes = %+v, want %+v among them", tc.wantMethod, row.FailureCodes, want)
			}
		})
	}
}

// Ground: contract — a step that failed is not asked for its values: there is
// no reply to read, and a refusal is the iteration's end as before.
// Mutation: Extract is called whatever the category.
func TestIterate_AFailedStepIsNotAskedToExtract(t *testing.T) {
	var calls atomic.Int32

	call := &Call{
		Method: "a/One", Scenario: "flow", KeepResponse: true, Values: 1,
		Extract: func([]byte, []string) error {
			calls.Add(1)

			return errNoValue
		},
		Next: []Step{{Method: "a/Two", Build: join("two:")}},
	}
	sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
		"a/One": reply(time.Millisecond, CategoryOverload),
	}}

	results, err := runPool(t.Context(), sender, 4, func(in chan<- Request) {
		in <- extractRequest(time.Now(), 1, call)
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("Extract was called %d times for a step that ended in overload, want 0", n)
	}
	if it := only(t, results, "flow"); it.Category != CategoryOverload {
		t.Errorf("iteration category = %v, want overload: the step's own, not bad_response", it.Category)
	}
}

// Ground: hot path — a scenario that extracts nothing makes no slice per
// iteration and gives Build none; one that extracts gives exactly Values of
// them, empty, to the first step to read.
// Mutation: a slice made for every scenario.
func TestIterate_TheValuesSliceIsMadeOnlyWhenThereAreNames(t *testing.T) {
	for _, tc := range []struct {
		name    string
		values  int
		extract func([]byte, []string) error
		wantNil bool
		wantLen int
	}{
		{"no names", 0, nil, true, 0},
		{"three names", 3, take(0), false, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mu   sync.Mutex
				seen [][]string
			)

			call := &Call{
				Method: "a/One", Scenario: "flow", KeepResponse: tc.extract != nil, Values: tc.values, Extract: tc.extract,
				Next: []Step{{Method: "a/Two", Build: func(_ uint64, vals []string) []byte {
					mu.Lock()
					seen = append(seen, vals)
					mu.Unlock()

					return []byte("body")
				}}},
			}
			sender := &scripted{do: map[string]func(context.Context, Request) (Outcome, error){
				"a/One": kept(time.Millisecond, replyOfSeq),
			}}

			_, err := runPool(t.Context(), sender, 4, func(in chan<- Request) {
				in <- extractRequest(time.Now(), 1, call)
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if len(seen) != 1 {
				t.Fatalf("Build was called %d times, want 1", len(seen))
			}
			if got := seen[0]; (got == nil) != tc.wantNil || len(got) != tc.wantLen {
				t.Errorf("Build got vals %q (nil %v), want nil %v and %d of them", got, got == nil, tc.wantNil, tc.wantLen)
			}
		})
	}
}
