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

package breakpoint

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// sending is a target of capacity 10000 whose every run schedules scheduled
// calls after its settle and sends sent of them at rps short, all at others.
func sending(short, scheduled, sent int) func(rps int) engine.Report {
	return func(rps int) engine.Report {
		r := report(scheduled, 0, 20*time.Millisecond, 20*time.Millisecond)
		r.Scheduled = scheduled
		if rps == short {
			r.Sent, r.Methods[0].Sent, r.Methods[0].Latencies = sent, sent, sent
		}

		return r
	}
}

// A step is held at its planned rps only if the generator sent it: the
// planned calls are the ticks the schedule put in the measured window
// (Report.Scheduled), sent are counted in the same window. Short by 0.1% of
// them, at least one call, still holds; one call more is the run's limit.
func TestSearch_AStepTheGeneratorDidNotSendIsNotHeld(t *testing.T) {
	for _, tc := range []struct {
		scheduled, sent int
		held            bool
	}{
		{1000, 1000, true}, {1000, 999, true}, {1000, 998, false},
		{100, 99, true}, {100, 98, false},
		{20000, 19980, true}, {20000, 19979, false},
	} {
		var asked []int
		res, err := Search(t.Context(), plan, fake(sending(125, tc.scheduled, tc.sent), &asked))
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if tc.held {
			if res.Outcome != HeldThroughout {
				t.Errorf("%d of %d: %v, want HeldThroughout", tc.sent, tc.scheduled, res.Outcome)
			}

			continue
		}
		if res.Outcome != RunLimit || res.Held != 100 || res.Broke != 125 || res.Cause != CauseGenerator {
			t.Errorf("%d of %d: %v held %d at %d cause %v, want RunLimit 100 125 generator",
				tc.sent, tc.scheduled, res.Outcome, res.Held, res.Broke, res.Cause)
		}
		if !slices.Equal(asked, []int{100, 125}) {
			t.Errorf("%d of %d: ran %v, want [100 125]", tc.sent, tc.scheduled, asked)
		}
		if last := res.Steps[len(res.Steps)-1]; last.Broken || last.Cause != CauseGenerator {
			t.Errorf("%d of %d: last step broken %v cause %v, want not broken, generator", tc.sent, tc.scheduled, last.Broken, last.Cause)
		}
	}
}

// Worst is every step broken once and held on its repeat after its cooldown
// and all probes, each run waiting out its calls in flight for the timeout:
// 2N × (hold + timeout) + (N − 1) × MaxProbes × (ProbeHold(from) + timeout)
// + N × cooldown. 100, 125, 156 with hold 1s, timeout 500ms:
// 9s + 55s + 1.5s.
func TestPlan_WorstIsEveryStepRepeatedAfterAllProbes(t *testing.T) {
	p := Plan{From: 100, To: 156, Settle: 100 * time.Millisecond, Hold: time.Second, Timeout: 500 * time.Millisecond}
	got, err := p.Worst()
	if err != nil {
		t.Fatalf("worst: %v", err)
	}
	if want := 65500 * time.Millisecond; got != want {
		t.Errorf("worst %v, want %v", got, want)
	}
	if _, err := (Plan{From: 0, To: 10, Hold: time.Second}).Worst(); err == nil {
		t.Error("a plan that cannot run has no worst case")
	}
}

// Worst is an upper bound of a real search: a target whose calls hang to the
// timeout, every step broken once, a probe after each break but the first.
func TestPlan_WorstBoundsASearchOfHangingCalls(t *testing.T) {
	p := Plan{From: 1000, To: 1250, Settle: 10 * time.Millisecond, Hold: 50 * time.Millisecond, Timeout: 30 * time.Millisecond}
	worst, err := p.Worst()
	if err != nil {
		t.Fatalf("worst: %v", err)
	}
	runs := 0
	run := func(ctx context.Context, rps int, _, hold time.Duration, _ int) (engine.Report, error) {
		runs++
		if err := sleep(ctx, hold+p.Timeout); err != nil {
			return engine.Report{}, err
		}
		if runs == 1 || runs == 3 { // each step's first run
			return report(rps, rps/2, 30*time.Millisecond, 30*time.Millisecond), nil
		}

		return report(rps, 0, 20*time.Millisecond, 20*time.Millisecond), nil
	}
	start := time.Now()
	res, _ := Search(t.Context(), p, run)
	took := time.Since(start)
	if res.Outcome != HeldThroughout || runs != 5 {
		t.Fatalf("%v after %d runs, want HeldThroughout after 5: step, repeat, step, probe, repeat", res.Outcome, runs)
	}
	if took > worst {
		t.Errorf("the search took %v, over its worst case %v", took, worst)
	}
}

// A context ended during a run stops the search: the run it ended is kept
// with its kind, the outcome is Stopped, what held before stays held and no
// rate is broke. Not an error: the report prints what was found.
func TestSearch_StoppedOnAProbeKeepsTheProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runs := 0
	run := func(_ context.Context, rps int, _, _ time.Duration, _ int) (engine.Report, error) {
		runs++
		if runs == 7 { // 100 125 156 195 244 305, then the first probe
			cancel()
		}

		return capacity(270)(rps), nil
	}
	res, err := Search(ctx, plan, run)
	if err != nil {
		t.Errorf("stopped is an outcome, not an error: %v", err)
	}
	if res.Outcome != Stopped || res.Held != 244 || res.Broke != 0 {
		t.Errorf("%v held %d broke %d, want Stopped 244 0", res.Outcome, res.Held, res.Broke)
	}
	if n := len(res.Steps); n != 7 || res.Steps[n-1].Kind != Probe {
		t.Errorf("%d runs, last %v, want 7 ending with the probe", n, res.Steps[n-1].Kind)
	}

	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	runs = 0
	res, _ = Search(ctx, plan, func(c context.Context, rps int, s, h time.Duration, m int) (engine.Report, error) {
		cancel()

		return run(c, rps, s, h, m)
	})
	if res.Outcome != Stopped || res.Held != 0 || len(res.Steps) != 1 {
		t.Errorf("stopped on the first step: %v held %d, %d runs, want Stopped 0, 1 run", res.Outcome, res.Held, len(res.Steps))
	}
}

// An invalid run anywhere makes the whole search invalid: a broke step
// judged on a bad clock makes the breaking point wrong. The runs are kept.
func TestSearch_AnInvalidRunInvalidatesTheSearch(t *testing.T) {
	for _, tc := range []struct {
		err   error
		cause Cause
	}{{ErrClockStep, CauseClockStep}, {ErrRequestErrors, CauseRequestErrors}} {
		run := func(_ context.Context, rps int, _, _ time.Duration, _ int) (engine.Report, error) {
			if rps == 305 {
				return capacity(270)(rps), fmt.Errorf("step: %w", tc.err)
			}

			return capacity(270)(rps), nil
		}
		res, err := Search(t.Context(), plan, run)
		if err != nil {
			t.Errorf("%v: invalid is an outcome, not an error: %v", tc.err, err)
		}
		if res.Outcome != Invalid || res.Cause != tc.cause || res.Broke != 0 {
			t.Errorf("%v: %v cause %v broke %d, want Invalid %v 0", tc.err, res.Outcome, res.Cause, res.Broke, tc.cause)
		}
		if n := len(res.Steps); n != 6 || res.Steps[n-1].RPS != 305 || res.Steps[n-1].Cause != tc.cause {
			t.Errorf("%v: %d runs, want 6 ending with the invalid 305", tc.err, n)
		}
	}
}

// neverRecovers breaks above 170 rps and stays slow at every rate after.
func neverRecovers() func(int) engine.Report {
	broke := false

	return func(rps int) engine.Report {
		if broke || rps > 170 {
			broke = true

			return report(rps*10, 0, time.Second, time.Second)
		}

		return capacity(10000)(rps)
	}
}

// Every cause the closed list names comes from the search, on the run that
// ended it.
func TestSearch_EveryCauseIsReachable(t *testing.T) {
	at195 := func(change func(*engine.Report)) func(int) engine.Report {
		return func(rps int) engine.Report {
			r := capacity(10000)(rps)
			if rps >= 195 {
				change(&r)
			}

			return r
		}
	}
	slowFrom195 := func(rps int) engine.Report {
		if rps >= 195 {
			return report(rps*10, 0, 500*time.Millisecond, 500*time.Millisecond)
		}

		return capacity(10000)(rps)
	}
	invalidAt305 := func(err error) RunStep {
		return func(_ context.Context, rps int, _, _ time.Duration, _ int) (engine.Report, error) {
			if rps == 305 {
				return capacity(10000)(rps), err
			}

			return capacity(10000)(rps), nil
		}
	}
	seen := map[Cause]bool{}
	for _, tc := range []struct {
		cause Cause
		run   RunStep
	}{
		{CauseClockStep, invalidAt305(ErrClockStep)},
		{CauseRequestErrors, invalidAt305(ErrRequestErrors)},
	} {
		res, _ := Search(t.Context(), plan, tc.run)
		seen[res.Cause] = true
		if res.Cause != tc.cause {
			t.Errorf("cause %v (%v), want %v", res.Cause, res.Outcome, tc.cause)
		}
	}
	defer func() {
		for c := CauseErrors; c <= CauseRequestErrors; c++ {
			if !seen[c] {
				t.Errorf("cause %v reached by no scenario", c)
			}
		}
	}()
	for _, tc := range []struct {
		cause  Cause
		plan   Plan
		target func(int) engine.Report
	}{
		{CauseErrors, plan, capacity(170)},
		{CauseP99VsBase, plan, slowFrom195},
		{CauseP99Limit, Plan{From: 100, To: 400, Settle: plan.Settle, Hold: plan.Hold, P99Limit: 100 * time.Millisecond}, slowFrom195},
		{CauseConnection, plan, at195(func(r *engine.Report) {
			r.Methods[0].P99WithoutClientWaits = exact(10 * time.Millisecond)
			r.ConnectionTailCalls, r.ConnectionCauseCalls = 30, 40
		})},
		{CauseNoRecovery, plan, neverRecovers()},
		{CauseGenerator, plan, at195(func(r *engine.Report) {
			r.Methods[0].P99WithoutClientWaits, r.GeneratorTailCalls = exact(10*time.Millisecond), 30
		})},
		{CauseInFlightCap, plan, at195(func(r *engine.Report) { r.CapHit = &engine.CapHit{} })},
		{CauseStreamLimit, plan, at195(func(r *engine.Report) {
			r.NotSent, r.NotSentStream, r.StreamTailCalls = 3, 3, 3
			r.Connections = &engine.Connections{Open: 1, LimitAnnounced: true, FirstLimit: 1, LastLimit: 1}
		})},
		{CauseStreamWait, plan, at195(func(r *engine.Report) {
			r.NotSent, r.NotSentStream, r.StreamTailCalls = 3, 3, 3
		})},
	} {
		var asked []int
		res, _ := Search(t.Context(), tc.plan, fake(tc.target, &asked))
		seen[res.Cause] = true
		if res.Cause != tc.cause {
			t.Errorf("cause %v (%v, steps %d), want %v", res.Cause, res.Outcome, len(res.Steps), tc.cause)
		}
	}
}
