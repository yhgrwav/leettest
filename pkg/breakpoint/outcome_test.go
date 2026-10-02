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
	"math"
	"slices"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// long is a plan whose measured window, 900ms, makes the planned call count
// of a step large enough to tell a short run from rounding.
var long = Plan{From: 100, To: 400, Settle: 100 * time.Millisecond, Hold: time.Second}

// sending is a target of capacity 10000 that sends every planned call of
// long's window, and share of them at rps short.
func sending(short int, share float64) func(rps int) engine.Report {
	return func(rps int) engine.Report {
		planned := float64(rps) * (long.Hold - long.Settle).Seconds()
		sent := int(math.Ceil(planned))
		if rps == short {
			sent = int(planned * share)
		}

		return report(sent, 0, 20*time.Millisecond, 20*time.Millisecond)
	}
}

// A step is held at its planned rps only if the generator sent it: a run
// that sent 90% of its planned calls is the run's limit, not a held step.
// Within 1% (at least one call) is held: the last tick may fall outside.
func TestSearch_AStepTheGeneratorDidNotSendIsNotHeld(t *testing.T) {
	var asked []int
	res, err := Search(t.Context(), long, fake(sending(125, 0.9), &asked))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Outcome != RunLimit || res.Held != 100 || res.Broke != 125 || res.Cause != CauseGenerator {
		t.Errorf("%v held %d at %d cause %v, want RunLimit 100 125 generator", res.Outcome, res.Held, res.Broke, res.Cause)
	}
	if !slices.Equal(asked, []int{100, 125}) {
		t.Errorf("ran %v, want [100 125]", asked)
	}
	if last := res.Steps[len(res.Steps)-1]; last.Broken || last.Cause != CauseGenerator {
		t.Errorf("last step broken %v cause %v, want the run's limit: not broken, generator", last.Broken, last.Cause)
	}

	asked = nil
	res, _ = Search(t.Context(), long, fake(sending(125, 0.995), &asked))
	if res.Outcome != HeldThroughout {
		t.Errorf("0.5%% short: %v, want HeldThroughout: within the tolerance", res.Outcome)
	}
}

// Worst is every step broken once and held on its repeat after the
// cooldown and all probes; the first step has no probes. 100, 125, 156:
// 3 × (2 × 1s + 500ms) + 2 × 5 × ProbeHold(100) = 7.5s + 50s.
func TestPlan_WorstIsEveryStepRepeatedAfterAllProbes(t *testing.T) {
	p := Plan{From: 100, To: 156, Settle: 100 * time.Millisecond, Hold: time.Second, Timeout: 500 * time.Millisecond}
	got, err := p.Worst()
	if err != nil {
		t.Fatalf("worst: %v", err)
	}
	if want := 57500 * time.Millisecond; got != want {
		t.Errorf("worst %v, want %v", got, want)
	}
	if _, err := (Plan{From: 0, To: 10, Hold: time.Second}).Worst(); err == nil {
		t.Error("a plan that cannot run has no worst case")
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
		if res.Cause != tc.cause {
			t.Errorf("cause %v (%v, steps %d), want %v", res.Cause, res.Outcome, len(res.Steps), tc.cause)
		}
	}
}
