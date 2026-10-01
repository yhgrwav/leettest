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
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

func exact(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Exact: true, Defined: true}
}

// report is one step's run: sent calls, failed of them, and p99 with and
// without client-side waits.
func report(sent, failed int, p99, withoutWaits time.Duration) engine.Report {
	return engine.Report{Sent: sent, Failed: failed, Methods: []engine.MethodReport{{
		Method: "a.B/C", Sent: sent, Failed: failed, Latencies: sent,
		P99: exact(p99), P99WithoutClientWaits: exact(withoutWaits),
	}}}
}

// capacity is a target that answers in 20ms up to n rps and fails 5% above.
func capacity(n int) func(rps int) engine.Report {
	return func(rps int) engine.Report {
		if rps <= n {
			return report(rps*10, 0, 20*time.Millisecond, 20*time.Millisecond)
		}

		return report(rps*10, rps/2, 500*time.Millisecond, 500*time.Millisecond)
	}
}

// fake runs each step through target and records the rates it was asked for.
func fake(target func(rps int) engine.Report, asked *[]int) RunStep {
	return func(_ context.Context, rps int, _, _ time.Duration) (engine.Report, error) {
		*asked = append(*asked, rps)

		return target(rps), nil
	}
}

var plan = Plan{From: 100, To: 400, Settle: time.Second, Hold: 5 * time.Second}

// Ground: contract — the default step is ×1.25: it covers a wide range
// without knowing the scale, and the interval it names keeps its relative
// width.
func TestPlan_RatesAreGeometricByDefault(t *testing.T) {
	got, err := plan.Rates()
	if err != nil {
		t.Fatalf("rates: %v", err)
	}
	if want := []int{100, 125, 156, 195, 244, 305, 381}; !slices.Equal(got, want) {
		t.Errorf("rates %v, want %v", got, want)
	}

	abs := plan
	abs.Step = 100
	if got, _ := abs.Rates(); !slices.Equal(got, []int{100, 200, 300, 400}) {
		t.Errorf("an absolute step of 100: rates %v, want [100 200 300 400]", got)
	}
}

// Ground: boundary — a plan that cannot run as written is an error before any
// load, not a run that silently differs.
func TestPlan_RejectsWhatCannotRun(t *testing.T) {
	for name, p := range map[string]Plan{
		"settle at half the hold": {From: 100, To: 400, Settle: 2 * time.Second, Hold: 4 * time.Second},
		"no first rate":           {From: 0, To: 400, Hold: time.Second},
		"top below the start":     {From: 400, To: 100, Hold: time.Second},
		"a factor of 1":           {From: 100, To: 400, Factor: 1, Hold: time.Second},
		"factor and step":         {From: 100, To: 400, Factor: 2, Step: 50, Hold: time.Second},
		"no hold":                 {From: 100, To: 400},
	} {
		if _, err := p.Rates(); !errors.Is(err, ErrPlan) {
			t.Errorf("%s: err %v, want ErrPlan", name, err)
		}
	}
}

// The search names the interval the limit lies in, confirms the broken step
// by repeating it — never by a higher one, which on a live target is more
// harm — and stops there.
func TestSearch_NamesTheIntervalAndRepeatsTheBrokenStep(t *testing.T) {
	var asked []int
	res, err := Search(t.Context(), plan, fake(capacity(270), &asked))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Outcome != BrokeBetween || res.Held != 244 || res.Broke != 305 {
		t.Errorf("outcome %v held %d broke %d, want BrokeBetween 244 305", res.Outcome, res.Held, res.Broke)
	}
	if want := []int{100, 125, 156, 195, 244, 305, 305}; !slices.Equal(asked, want) {
		t.Errorf("ran %v, want %v: the broken step repeated, nothing above it", asked, want)
	}
	if len(res.Steps) == 0 {
		t.Fatalf("no steps in the result")
	}
	if last := res.Steps[len(res.Steps)-1]; !last.Repeat || !last.Broken {
		t.Errorf("the last step %+v, want the broken repeat", last)
	}
}

// Ground: boundary — the three answers at the edges of the profile.
func TestSearch_EdgesOfTheProfile(t *testing.T) {
	var asked []int
	if res, _ := Search(t.Context(), plan, fake(capacity(50), &asked)); res.Outcome != BrokeAtFirst || res.Broke != 100 {
		t.Errorf("a target below the first step: %v broke %d, want BrokeAtFirst 100", res.Outcome, res.Broke)
	}

	asked = nil
	if res, _ := Search(t.Context(), plan, fake(capacity(10000), &asked)); res.Outcome != HeldThroughout || res.Held != 381 {
		t.Errorf("a target above the profile: %v held %d, want HeldThroughout 381", res.Outcome, res.Held)
	}
}

// Ground: boundary — a step where the run gave out says nothing about the
// target: no breaking point is named above it.
func TestSearch_TheRunsOwnLimitIsNotTheTargets(t *testing.T) {
	for name, limit := range map[string]func(*engine.Report){
		"in-flight cap":    func(r *engine.Report) { r.CapHit = &engine.CapHit{} },
		"calls not sent":   func(r *engine.Report) { r.NotSent = 3 },
		"generator behind": func(r *engine.Report) { r.Methods[0].P99WithoutClientWaits = exact(10 * time.Millisecond) },
	} {
		var asked []int
		target := func(rps int) engine.Report {
			r := capacity(10000)(rps)
			if rps >= 195 {
				limit(&r)
			}

			return r
		}
		res, _ := Search(t.Context(), plan, fake(target, &asked))
		if res.Outcome != RunLimit || res.Held != 156 || res.Broke != 195 {
			t.Errorf("%s: %v held %d at %d, want RunLimit 156 195", name, res.Outcome, res.Held, res.Broke)
		}
	}
}

// A step that breaks once and holds on its repeat was noise: the search goes
// on up.
func TestSearch_ABreakTheRepeatDoesNotConfirmGoesOn(t *testing.T) {
	var asked []int
	seen := 0
	target := func(rps int) engine.Report {
		if rps == 195 {
			seen++
			if seen == 1 {
				return capacity(0)(rps)
			}
		}

		return capacity(270)(rps)
	}
	res, _ := Search(t.Context(), plan, fake(target, &asked))
	if res.Outcome != BrokeBetween || res.Held != 244 || res.Broke != 305 {
		t.Errorf("%v held %d broke %d, want BrokeBetween 244 305 past the noise at 195", res.Outcome, res.Held, res.Broke)
	}
}

// Ground: boundary — the criteria at their edges: failures at exactly 1%
// break, the knee only past 3× the first step's p99, a user's p99 limit
// replaces the knee.
func TestSearch_CriteriaAtTheirEdges(t *testing.T) {
	const ms = time.Millisecond
	for _, tc := range []struct {
		name  string
		limit time.Duration
		step2 engine.Report
		broke bool
	}{
		{"failed 1%", 0, report(1000, 10, 20*ms, 20*ms), true},
		{"failed 0.9%", 0, report(1000, 9, 20*ms, 20*ms), false},
		{"p99 exactly 3x", 0, report(1000, 0, 60*ms, 60*ms), false},
		{"p99 just over 3x", 0, report(1000, 0, 61*ms, 61*ms), true},
		{"over a user's limit", 30 * ms, report(1000, 0, 31*ms, 31*ms), true},
		{"5x, under the user's limit", 200 * ms, report(1000, 0, 100*ms, 100*ms), false},
	} {
		p := Plan{From: 100, To: 125, Settle: time.Second, Hold: 5 * time.Second, P99Limit: tc.limit}
		var asked []int
		target := func(rps int) engine.Report {
			if rps == 100 {
				return report(1000, 0, 20*ms, 20*ms)
			}

			return tc.step2
		}
		res, _ := Search(t.Context(), p, fake(target, &asked))
		if got := res.Outcome == BrokeBetween; got != tc.broke {
			t.Errorf("%s: broke %v (%v), want %v", tc.name, got, res.Outcome, tc.broke)
		}
	}
}
