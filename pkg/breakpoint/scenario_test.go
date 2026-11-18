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
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// scUnit is the millisecond: the p99s of the scenarios below are written as
// multiples of it.
const scUnit = time.Millisecond

// The rows of the scenario "flow" are keyed so that the report's order, by
// key, puts the head last: Methods[0] is step 2, never the head.
var scKeys = []string{"", "z.Head/Call", "a.Step/One", "b.Step/Two"}

// scRow is step n's row (1 is the head): sent calls, failed of them, p99; no
// p99 leaves it undefined.
func scRow(step, sent, failed int, p99 time.Duration) engine.MethodReport {
	m := engine.MethodReport{
		Method: scKeys[step], Scenario: "flow", Step: step, GRPCMethod: scKeys[step],
		Sent: sent, Failed: failed, Latencies: sent,
	}
	if p99 > 0 {
		m.P99, m.P99WithoutClientWaits = exact(p99), exact(p99)
	}

	return m
}

// scReport is one step's run of a scenario that scheduled scheduled
// iterations: the rows sorted by key as the engine reports them, Sent and
// Failed over all of them, the scenario entry with how far the iterations got.
func scReport(scheduled int, rows ...engine.MethodReport) engine.Report {
	r := engine.Report{Scheduled: scheduled}
	sc := engine.ScenarioReport{Name: "flow", Iterations: scheduled}

	byStep := slices.Clone(rows)
	slices.SortFunc(byStep, func(a, b engine.MethodReport) int { return a.Step - b.Step })

	for i := range byStep {
		m := &byStep[i]
		r.Sent += m.Sent
		r.Failed += m.Failed
		reached := m.Sent + m.NotSent
		sc.Steps = append(sc.Steps, engine.ScenarioStepReport{Method: m.Method, Reached: reached, NotReached: scheduled - reached})
	}

	r.Methods = slices.Clone(rows)
	slices.SortFunc(r.Methods, func(a, b engine.MethodReport) int { return strings.Compare(a.Method, b.Method) })
	r.Scenarios = []engine.ScenarioReport{sc}

	return r
}

// scGood is a run where every row holds: 1000 iterations, p99 20, 30 and 40ms.
func scGood() engine.Report {
	return scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, 0, 40*scUnit))
}

// scFrom195 answers held below 195 and broken from it.
func scFrom195(held, broken engine.Report) func(int) engine.Report {
	return func(rps int) engine.Report {
		if rps >= 195 {
			return broken
		}

		return held
	}
}

// scPlan is the search's plan with the errors limit at 5%, rates 100 … 381.
var scPlan = Plan{From: 100, To: 400, Settle: 10 * time.Millisecond, Hold: 50 * time.Millisecond, ErrorsLimit: percent("5%", 5000)}

func scRowIs(t *testing.T, what string, got *Row, step int) {
	t.Helper()

	if got == nil || got.Step != step || got.Method != scKeys[step] {
		t.Errorf("%s: names %+v, want step %d, %s", what, got, step, scKeys[step])
	}
}

// scBrokeAt195 checks the search broke at 195 after holding 156, by cause, and
// that the step that ended it names the row.
func scBrokeAt195(t *testing.T, what string, res Result, cause Cause, step int) {
	t.Helper()

	if res.Outcome != BrokeBetween || res.Held != 156 || res.Broke != 195 || res.Cause != cause {
		t.Errorf("%s: %v held %d broke %d cause %v, want BrokeBetween 156 195 cause %v", what, res.Outcome, res.Held, res.Broke, res.Cause, cause)
	}
	scRowIs(t, what+": the search", res.BrokeStep, step)

	if len(res.Steps) == 0 {
		t.Fatalf("%s: no steps", what)
	}
	scRowIs(t, what+": the last run", res.Steps[len(res.Steps)-1].BrokeStep, step)
}

// Ground: contract — a row's failures are judged over the calls that reached
// it: step 3 of 3 failing 100 of its 1000 calls is 10% against a limit of 5%.
// Summed over the rows it is 100 of 3000, 3.3%, and the step would hold. At
// exactly 5% it holds, one call over breaks.
func TestScenarioSearch_ALateStepFailingItsReachedCallsBreaksTheStep(t *testing.T) {
	for _, tc := range []struct {
		failed int
		broke  bool
	}{{50, false}, {51, true}, {100, true}} {
		late := scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, tc.failed, 40*scUnit))

		var asked []int
		res, err := Search(t.Context(), scPlan, fake(scFrom195(scGood(), late), &asked))
		if err != nil {
			t.Fatalf("%d failed: search: %v", tc.failed, err)
		}
		if !tc.broke {
			if res.Outcome != HeldThroughout || res.Held != 381 || res.BrokeStep != nil {
				t.Errorf("%d of 1000 failed: %v held %d names %+v, want HeldThroughout 381 and no row", tc.failed, res.Outcome, res.Held, res.BrokeStep)
			}

			continue
		}
		scBrokeAt195(t, "failed "+strconv.Itoa(tc.failed), res, CauseErrors, 3)

		want := "first to break: step 3 of 3, b.Step/Two: failed " + strconv.Itoa(tc.failed) + " of 1000 calls ("
		if why := res.Steps[len(res.Steps)-1].Why; !strings.HasPrefix(why, want) {
			t.Errorf("why %q, want it to start %q", why, want)
		}
	}
}

// Ground: contract — a call lost to the connection is among the calls that
// reached the row and counts as bad: 51 of a row's 1000 are 5.1% against 5%,
// 50 hold (50 of the 950 that were sent would be 5.3%).
func TestScenarioSearch_CallsLostToTheConnectionCountAmongTheRowsReached(t *testing.T) {
	for _, tc := range []struct {
		lost  int
		broke bool
	}{{50, false}, {51, true}, {60, true}} {
		lost := scRow(3, 1000-tc.lost, 0, 40*scUnit)
		lost.NotSent, lost.NotSentConnection = tc.lost, tc.lost
		broken := scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), lost)

		var asked []int
		res, err := Search(t.Context(), scPlan, fake(scFrom195(scGood(), broken), &asked))
		if err != nil {
			t.Fatalf("%d lost: search: %v", tc.lost, err)
		}
		if !tc.broke {
			if res.Outcome != HeldThroughout || res.BrokeStep != nil {
				t.Errorf("%d of 1000 lost to the connection: %v names %+v, want HeldThroughout and no row", tc.lost, res.Outcome, res.BrokeStep)
			}

			continue
		}
		scBrokeAt195(t, strconv.Itoa(tc.lost)+" of 1000 lost to the connection", res, CauseErrors, 3)
	}
}

// Ground: contract — the knee is a row's own: step 3 growing from 5ms to 50ms
// breaks beside step 2 that stays at 500ms, where the highest p99 of the rows
// would not move. Exactly 3× the row's baseline holds, a millisecond over
// breaks.
func TestScenarioSearch_AFastStepGrowingPastTheKneeBreaksBesideASlowOne(t *testing.T) {
	run := func(fast time.Duration) engine.Report {
		return scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 500*scUnit), scRow(3, 1000, 0, fast))
	}
	for _, tc := range []struct {
		fast  time.Duration
		broke bool
	}{{15 * scUnit, false}, {16 * scUnit, true}, {50 * scUnit, true}} {
		var asked []int
		res, err := Search(t.Context(), scPlan, fake(scFrom195(run(5*scUnit), run(tc.fast)), &asked))
		if err != nil {
			t.Fatalf("%v: search: %v", tc.fast, err)
		}
		if !tc.broke {
			if res.Outcome != HeldThroughout || res.BrokeStep != nil {
				t.Errorf("%v: %v names %+v, want HeldThroughout and no row", tc.fast, res.Outcome, res.BrokeStep)
			}

			continue
		}
		scBrokeAt195(t, tc.fast.String(), res, CauseP99VsBase, 3)

		if why := res.Steps[len(res.Steps)-1].Why; !strings.HasPrefix(why, "first to break: step 3 of 3, b.Step/Two: p99 ") {
			t.Errorf("%v: why %q, want it to name step 3 first", tc.fast, why)
		}
	}
}

// Ground: boundary — the knee of a row is crossed at its p99 bucket's lower
// edge, as a plain call's is: against a baseline of 20ms a row at exactly 60ms
// holds (its bucket starts at 59.998ms, the knee is at 60.015ms) and 61ms
// breaks. Real histograms: the edges of a bucket cannot be set through a fake
// Quantile.
func TestScenarioSearch_TheKneeOfARowIsComparedAtTheBucketsLowerEdge(t *testing.T) {
	recorded := func(latency time.Duration) metrics.Quantile {
		l := metrics.NewLatencies()
		for range 1000 {
			l.Record(latency)
		}

		return l.Snapshot().Percentile(0.99)
	}
	run := func(third time.Duration) engine.Report {
		r := scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, 0, 20*scUnit))
		for i := range r.Methods {
			if r.Methods[i].Step == 3 {
				r.Methods[i].P99 = recorded(third)
			}
		}

		return r
	}

	for _, tc := range []struct {
		third time.Duration
		broke bool
	}{{60 * scUnit, false}, {61 * scUnit, true}} {
		var asked []int
		res, err := Search(t.Context(), scPlan, fake(scFrom195(run(20*scUnit), run(tc.third)), &asked))
		if err != nil {
			t.Fatalf("%v: search: %v", tc.third, err)
		}
		if !tc.broke {
			if res.Outcome != HeldThroughout {
				t.Errorf("%v: %v, want HeldThroughout", tc.third, res.Outcome)
			}

			continue
		}
		scBrokeAt195(t, tc.third.String(), res, CauseP99VsBase, 3)
	}
}

// Ground: contract — a row's baseline is the lowest p99 of the steps that held,
// not the first: 10ms, then 8ms, so 25ms is over 3 × 8 and 24ms is not. The
// highest p99 of the rows, 30ms, never reaches a knee.
func TestScenarioSearch_ARowsBaselineIsItsLowestHeldP99(t *testing.T) {
	for _, tc := range []struct {
		last  time.Duration
		broke bool
	}{{24 * scUnit, false}, {25 * scUnit, true}} {
		third := func(rps int) time.Duration {
			switch {
			case rps == 100:
				return 10 * scUnit
			case rps < 195:
				return 8 * scUnit
			default:
				return tc.last
			}
		}
		target := func(rps int) engine.Report {
			return scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, 0, third(rps)))
		}

		var asked []int
		res, err := Search(t.Context(), scPlan, fake(target, &asked))
		if err != nil {
			t.Fatalf("%v: search: %v", tc.last, err)
		}
		if !tc.broke {
			if res.Outcome != HeldThroughout {
				t.Errorf("%v: %v, want HeldThroughout", tc.last, res.Outcome)
			}

			continue
		}
		scBrokeAt195(t, tc.last.String(), res, CauseP99VsBase, 3)
	}
}

// Ground: boundary — a held step too short for a row's p99 leaves that row's
// baseline alone: zero would be the lowest and no knee would ever be checked,
// whether the short step is the first or the last that held.
func TestScenarioSearch_ARowWithNoP99AtAHeldStepLeavesItsBaselineAlone(t *testing.T) {
	for _, short := range []int{100, 156} {
		third := func(rps int) time.Duration {
			switch {
			case rps == short:
				return 0
			case rps < 195:
				return 5 * scUnit
			default:
				return 20 * scUnit
			}
		}
		target := func(rps int) engine.Report {
			return scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, 0, third(rps)))
		}

		var asked []int
		res, err := Search(t.Context(), scPlan, fake(target, &asked))
		if err != nil {
			t.Fatalf("no p99 at %d: search: %v", short, err)
		}
		scBrokeAt195(t, "no p99 at "+strconv.Itoa(short)+", 20ms over a 5ms baseline", res, CauseP99VsBase, 3)
	}
}

// Ground: contract — the head's sent calls are what the generator owes: the
// scheduled iterations. With three steps the report's Sent is three times
// that, which hides a head short by a tenth. Short by 0.1%, at least one, still
// holds; one more is the run's limit. The head is the row of step 1, not the
// first row of the report (here step 2).
func TestScenarioSearch_AGeneratorLateOnTheHeadIsTheRunsLimitAtThreeSteps(t *testing.T) {
	for _, tc := range []struct {
		headSent int
		held     bool
	}{{1000, true}, {999, true}, {998, false}, {900, false}} {
		target := func(rps int) engine.Report {
			if rps == 125 {
				return scReport(1000, scRow(1, tc.headSent, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, 0, 40*scUnit))
			}

			return scGood()
		}

		var asked []int
		res, err := Search(t.Context(), scPlan, fake(target, &asked))
		if err != nil {
			t.Fatalf("%d sent: search: %v", tc.headSent, err)
		}
		if tc.held {
			if res.Outcome != HeldThroughout {
				t.Errorf("head sent %d of 1000: %v, want HeldThroughout", tc.headSent, res.Outcome)
			}

			continue
		}
		if res.Outcome != RunLimit || res.Held != 100 || res.Broke != 125 || res.Cause != CauseGenerator || res.BrokeStep != nil {
			t.Errorf("head sent %d: %v held %d at %d cause %v names %+v, want RunLimit 100 125 generator and no row",
				tc.headSent, res.Outcome, res.Held, res.Broke, res.Cause, res.BrokeStep)
		}
		if !slices.Equal(asked, []int{100, 125}) {
			t.Errorf("head sent %d: ran %v, want [100 125]", tc.headSent, asked)
		}
		last := res.Steps[len(res.Steps)-1]
		if want := "the generator sent " + strconv.Itoa(tc.headSent) + " of 1000 scheduled iterations at 125 iterations/s"; last.Broken || !strings.HasPrefix(last.Why, want) {
			t.Errorf("head sent %d: broken %v why %q, want not broken and %q", tc.headSent, last.Broken, last.Why, want)
		}
	}
}

// Ground: contract — a step after the head that the generator or a stream left
// unsent is the run's limit when it lost over 0.1% of the calls that reached
// it (at least one) for a reason other than the connection; a call not sent
// for the connection is the target's and counts as a failed call instead.
func TestScenarioSearch_CallsOfAStepLeftUnsentAreTheRunsLimit(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		notSent, gen, str, conn int
		cause                   Cause
	}{
		{"one by the generator", 1, 1, 0, 0, NoCause},
		{"two by the generator", 2, 2, 0, 0, CauseGenerator},
		{"two for a stream", 2, 0, 2, 0, CauseStreamWait},
		{"five for the connection", 5, 0, 0, 5, NoCause},
	} {
		third := scRow(3, 1000, 0, 40*scUnit)
		third.NotSent, third.NotSentGenerator, third.NotSentStream, third.NotSentConnection = tc.notSent, tc.gen, tc.str, tc.conn
		target := func(rps int) engine.Report {
			if rps == 125 {
				return scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), third)
			}

			return scGood()
		}

		var asked []int
		res, err := Search(t.Context(), scPlan, fake(target, &asked))
		if err != nil {
			t.Fatalf("%s: search: %v", tc.name, err)
		}
		if tc.cause == NoCause {
			if res.Outcome != HeldThroughout {
				t.Errorf("%s: %v, want HeldThroughout", tc.name, res.Outcome)
			}

			continue
		}
		if res.Outcome != RunLimit || res.Held != 100 || res.Broke != 125 || res.Cause != tc.cause {
			t.Errorf("%s: %v held %d at %d cause %v, want RunLimit 100 125 cause %v", tc.name, res.Outcome, res.Held, res.Broke, res.Cause, tc.cause)
		}
		if last := res.Steps[len(res.Steps)-1]; last.Broken || !strings.Contains(last.Why, "step 3 of 3, b.Step/Two") {
			t.Errorf("%s: broken %v why %q, want not broken and the row named", tc.name, last.Broken, last.Why)
		}
	}
}

// Ground: boundary — the allowance of a run's side is a thousandth of the
// calls, not one call: of 10000 iterations the head may be short by 10 and a
// step after it may leave 10 of its 10000 reached calls unsent; one more is the
// run's limit.
func TestScenarioSearch_TheAllowanceIsAThousandthAtTenThousandIterations(t *testing.T) {
	for _, tc := range []struct {
		name           string
		headSent, lost int
		limit          bool
	}{
		{"head short by 10", 9990, 0, false},
		{"head short by 11", 9989, 0, true},
		{"step 3 left 10", 10000, 10, false},
		{"step 3 left 11", 10000, 11, true},
	} {
		third := scRow(3, 10000-tc.lost, 0, 40*scUnit)
		third.NotSent, third.NotSentGenerator = tc.lost, tc.lost
		target := func(rps int) engine.Report {
			if rps == 125 {
				return scReport(10000, scRow(1, tc.headSent, 0, 20*scUnit), scRow(2, 10000, 0, 30*scUnit), third)
			}

			return scReport(10000, scRow(1, 10000, 0, 20*scUnit), scRow(2, 10000, 0, 30*scUnit), scRow(3, 10000, 0, 40*scUnit))
		}

		var asked []int
		res, err := Search(t.Context(), scPlan, fake(target, &asked))
		if err != nil {
			t.Fatalf("%s: search: %v", tc.name, err)
		}
		if got := res.Outcome == RunLimit; got != tc.limit {
			t.Errorf("%s: %v, want run limit %v", tc.name, res.Outcome, tc.limit)
		}
	}
}

// Ground: contract — the in-flight cap is sized for the iteration, as the
// engine will: a call that waits the sum of its steps' timeouts. Timeouts of
// 500ms, 200ms and 300ms hold a slot a second, so 100 iterations/s need
// 100 + 1 + 30 = 131 slots, where the head alone would need 61. The number is
// the engine's own (InFlightNeed of a real scenario call), at each step.
func TestScenarioSearch_TheCapIsSizedForTheSumOfTheStepTimeouts(t *testing.T) {
	p := scPlan
	p.To = 156
	p.Timeout, p.StepTimeouts = 500*time.Millisecond, []time.Duration{200 * time.Millisecond, 300 * time.Millisecond}

	var caps []int
	run := func(_ context.Context, rps int, _, _ time.Duration, maxInFlight int) (engine.Report, error) {
		caps = append(caps, maxInFlight)

		return scGood(), nil
	}
	if _, err := Search(t.Context(), p, run); err != nil {
		t.Fatalf("search: %v", err)
	}

	var want []int
	for _, rps := range []int{100, 125, 156} {
		want = append(want, engine.InFlightNeed([]engine.Call{{
			Method: "z.Head/Call", Scenario: "flow", Timeout: p.Timeout,
			Next: []engine.Step{
				{Method: "a.Step/One", Timeout: p.StepTimeouts[0]},
				{Method: "b.Step/Two", Timeout: p.StepTimeouts[1]},
			},
			Stages: []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: p.Hold}},
		}}))
	}
	if !slices.Equal(caps, want) {
		t.Errorf("caps %v, want %v: what the engine's InFlightNeed says of the scenario", caps, want)
	}
	if len(caps) == 0 || caps[0] != 131 {
		t.Errorf("caps %v, want the first to be 131 (100 + 1 + 30)", caps)
	}

	p.MaxInFlight = 130
	var asked []int
	res, err := Search(t.Context(), p, fake(func(int) engine.Report { return scGood() }, &asked))
	if err != nil {
		t.Fatalf("a cap too low for a step is no error: %v", err)
	}
	if res.Outcome != RunLimit || res.Cause != CauseInFlightCap || res.Broke != 100 || len(asked) != 0 {
		t.Errorf("%v at %d cause %v ran %v, want RunLimit at 100 by the cap and nothing run", res.Outcome, res.Broke, res.Cause, asked)
	}
}

// Ground: contract — the pause before a repeat and the worst case use the
// iteration's longest life, the sum of the timeouts of the call and its steps:
// 500ms + 500ms + 1s = 2s. Worst of 100, 125, 156 with hold 1s is
// 6 × (1s + 2s) + 10 × (5s + 2s) + 3 × 2s + 16 × 100ms = 95.6s.
func TestPlan_CooldownAndWorstUseTheSumOfTheStepTimeouts(t *testing.T) {
	p := Plan{
		From: 100, To: 156, Settle: 100 * time.Millisecond, Hold: time.Second,
		Timeout: 500 * time.Millisecond, StepTimeouts: []time.Duration{500 * time.Millisecond, time.Second},
	}
	if got := p.Cooldown(); got != 2*time.Second {
		t.Errorf("cooldown %v, want 2s", got)
	}

	worst, err := p.Worst()
	if err != nil {
		t.Fatalf("worst: %v", err)
	}
	if want := 95*time.Second + 600*time.Millisecond; worst != want {
		t.Errorf("worst %v, want %v", worst, want)
	}

	long := Plan{From: 100, To: 156, Settle: 3 * time.Second, Hold: 7 * time.Second, Timeout: time.Second, StepTimeouts: []time.Duration{time.Second}}
	if got := long.Cooldown(); got != 3*time.Second {
		t.Errorf("settle over the sum: cooldown %v, want 3s", got)
	}
}

// Ground: contract — the head collapsing breaks the step on the head: it is
// named step 1 of 3, the verdict is a break, not an unknown, and steps nobody
// reached say nothing, not even in the reason.
func TestScenarioSearch_ACollapsedHeadIsNamedStepOne(t *testing.T) {
	collapsed := scReport(1000, scRow(1, 1000, 500, 20*scUnit), scRow(2, 0, 0, 0), scRow(3, 0, 0, 0))

	var asked []int
	res, err := Search(t.Context(), scPlan, fake(scFrom195(scGood(), collapsed), &asked))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	scBrokeAt195(t, "head collapsed", res, CauseErrors, 1)

	why := res.Steps[len(res.Steps)-1].Why
	if !strings.HasPrefix(why, "first to break: step 1 of 3, z.Head/Call: failed 500 of 1000 calls (") ||
		strings.Contains(why, "step 2") || strings.Contains(why, "step 3") {
		t.Errorf("why %q, want step 1 only", why)
	}
}

// Ground: boundary — a row nobody reached gives no verdict: a step with no
// calls at any rate holds the search.
func TestScenarioSearch_ARowNobodyReachedGivesNoVerdict(t *testing.T) {
	target := func(int) engine.Report {
		return scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 0, 0, 0))
	}

	var asked []int
	res, err := Search(t.Context(), scPlan, fake(target, &asked))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Outcome != HeldThroughout || res.BrokeStep != nil {
		t.Errorf("%v names %+v, want HeldThroughout and no row", res.Outcome, res.BrokeStep)
	}
}

// Ground: contract — the first row to break in the order of the steps is the
// name, and the reason lists every broken row in that order: step 2 and step 3
// both failing name step 2 first, step 3 after it.
func TestScenarioSearch_TheFirstBrokenRowIsNamedAndAllAreListed(t *testing.T) {
	broken := scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 100, 30*scUnit), scRow(3, 1000, 100, 40*scUnit))

	var asked []int
	res, err := Search(t.Context(), scPlan, fake(scFrom195(scGood(), broken), &asked))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	scBrokeAt195(t, "two rows failing", res, CauseErrors, 2)

	why := res.Steps[len(res.Steps)-1].Why
	first, second := strings.Index(why, "step 2 of 3, a.Step/One: failed 100 of 1000"), strings.Index(why, "step 3 of 3, b.Step/Two: failed 100 of 1000")
	if !strings.HasPrefix(why, "first to break: step 2 of 3") || first < 0 || second < first {
		t.Errorf("why %q, want step 2 first, then step 3", why)
	}
}

// Ground: contract — a step's tiers are errors that break on any row, then an
// undecided share on any row, then the knee on any row; a step that broke is
// named by its lowest broken row whatever the tier. A knee on the head with
// failures on step 3 is broken by errors, named step 1; an undecided share on
// step 2 with a knee on the head is the run's limit, not a break; failures
// beside an undecided share break.
func TestScenarioSearch_TheTiersOfAStepGoErrorsUndecidedKnee(t *testing.T) {
	undecided := scRow(2, 1000, 100, 30*scUnit)
	undecided.Rejected.Count = 100

	t.Run("errors before the knee", func(t *testing.T) {
		broken := scReport(1000, scRow(1, 1000, 0, 120*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, 100, 40*scUnit))

		var asked []int
		res, err := Search(t.Context(), scPlan, fake(scFrom195(scGood(), broken), &asked))
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		scBrokeAt195(t, "knee on the head, failures on step 3", res, CauseErrors, 1)

		why := res.Steps[len(res.Steps)-1].Why
		if !strings.HasPrefix(why, "first to break: step 1 of 3, z.Head/Call: p99 ") || !strings.Contains(why, "step 3 of 3, b.Step/Two: failed 100 of 1000") {
			t.Errorf("why %q, want the head's knee first and step 3's failures after", why)
		}
	})

	t.Run("undecided before the knee", func(t *testing.T) {
		limited := scReport(1000, scRow(1, 1000, 0, 120*scUnit), undecided, scRow(3, 1000, 0, 40*scUnit))

		var asked []int
		res, err := Search(t.Context(), scPlan, fake(scFrom195(scGood(), limited), &asked))
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if res.Outcome != RunLimit || res.Held != 156 || res.Broke != 195 || res.Cause != CauseClientErrors || res.BrokeStep != nil {
			t.Errorf("%v held %d at %d cause %v names %+v, want RunLimit 156 195 by the client's errors and no row",
				res.Outcome, res.Held, res.Broke, res.Cause, res.BrokeStep)
		}
		if last := res.Steps[len(res.Steps)-1]; last.Broken || !strings.Contains(last.Why, "step 2 of 3, a.Step/One: failed 100 of 1000 calls") || !strings.Contains(last.Why, "only with the client's") {
			t.Errorf("broken %v why %q, want not broken and step 2's undecided share", last.Broken, last.Why)
		}
	})

	t.Run("errors before undecided", func(t *testing.T) {
		broken := scReport(1000, scRow(1, 1000, 0, 20*scUnit), undecided, scRow(3, 1000, 100, 40*scUnit))

		var asked []int
		res, err := Search(t.Context(), scPlan, fake(scFrom195(scGood(), broken), &asked))
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		scBrokeAt195(t, "failures on step 3, undecided on step 2", res, CauseErrors, 3)
	})
}

// Ground: contract — a probe has recovered when every row is within 1.5× its
// own baseline. The head at 2× its 10ms baseline has not, though the highest
// p99 of the rows, step 2's 100ms, is under 1.5× the highest baseline; at 14ms
// it has. Nor has a later row: step 3 at 2× its 40ms baseline, the head at its
// own.
func TestScenarioSearch_AProbeRecoversRowByRow(t *testing.T) {
	rows := func(head, third time.Duration, failed int) engine.Report {
		return scReport(1000, scRow(1, 1000, 0, head), scRow(2, 1000, 0, 100*scUnit), scRow(3, 1000, failed, third))
	}
	for _, tc := range []struct {
		head, third time.Duration
		recovered   bool
	}{{20 * scUnit, 40 * scUnit, false}, {10 * scUnit, 80 * scUnit, false}, {14 * scUnit, 40 * scUnit, true}} {
		p := scPlan
		p.To = 195

		var asked []int
		target := func(rps int) engine.Report {
			switch {
			case rps == 100 && len(asked) > 4:
				return rows(tc.head, tc.third, 0)
			case rps >= 195:
				return rows(10*scUnit, 40*scUnit, 100)
			default:
				return rows(10*scUnit, 40*scUnit, 0)
			}
		}
		res, err := Search(t.Context(), p, fake(target, &asked))
		if err != nil {
			t.Fatalf("%v: search: %v", tc.head, err)
		}

		want := []int{100, 125, 156, 195, 100, 100, 100, 100, 100}
		if tc.recovered {
			want = []int{100, 125, 156, 195, 100, 195}
		}
		if !slices.Equal(asked, want) {
			t.Errorf("head %v third %v in the probe: ran %v, want %v", tc.head, tc.third, asked, want)
		}
		if tc.recovered {
			scBrokeAt195(t, "recovered", res, CauseErrors, 3)

			continue
		}
		if res.Outcome != BrokeBetween || res.Broke != 195 || res.Cause != CauseNoRecovery ||
			!slices.ContainsFunc(res.Notes, func(n string) bool { return strings.HasPrefix(n, "195: broke and did not recover within") }) {
			t.Errorf("head %v in the probe: %v broke %d cause %v notes %q, want BrokeBetween 195, no recovery", tc.head, res.Outcome, res.Broke, res.Cause, res.Notes)
		}
	}
}

// Ground: contract — a probe only has to show the p99s back: a failure share
// that is over the limit only with the client's own failures, which would end a
// step as the run's limit, does not keep a probe from recovering.
func TestScenarioSearch_AProbeWithClientErrorsOnARowStillRecovers(t *testing.T) {
	undecided := scRow(2, 1000, 100, 30*scUnit)
	undecided.Rejected.Count = 100
	probe := scReport(1000, scRow(1, 1000, 0, 20*scUnit), undecided, scRow(3, 1000, 0, 40*scUnit))
	broken := scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, 100, 40*scUnit))

	p := scPlan
	p.To = 195

	var asked []int
	target := func(rps int) engine.Report {
		switch {
		case rps == 100 && len(asked) > 4:
			return probe
		case rps >= 195:
			return broken
		default:
			return scGood()
		}
	}
	res, err := Search(t.Context(), p, fake(target, &asked))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if want := []int{100, 125, 156, 195, 100, 195}; !slices.Equal(asked, want) {
		t.Errorf("ran %v, want %v: the probe recovers at its first run", asked, want)
	}
	scBrokeAt195(t, "probe with client errors", res, CauseErrors, 3)
}

// Ground: contract — the texts of a scenario's search count iterations: no
// "rps" in a reason or a note, which the report prints as they are.
func TestScenarioSearch_TheTextsCountIterationsNotRps(t *testing.T) {
	quickPlan := scPlan
	quickPlan.To = 195
	quickPlan.Timeout, quickPlan.StepTimeouts = 5*time.Millisecond, []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}

	capped := scPlan
	capped.To = 156
	capped.Timeout, capped.StepTimeouts = 500*time.Millisecond, []time.Duration{200 * time.Millisecond, 300 * time.Millisecond}
	capped.MaxInFlight = 130

	short := scReport(1000, scRow(1, 900, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, 0, 40*scUnit))
	withCap := scGood()
	withCap.CapHit = &engine.CapHit{}
	broken := scReport(1000, scRow(1, 1000, 0, 20*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, 100, 40*scUnit))
	slowProbe := scReport(1000, scRow(1, 1000, 0, 100*scUnit), scRow(2, 1000, 0, 30*scUnit), scRow(3, 1000, 0, 40*scUnit))

	var asked []int
	probing := func(rps int) engine.Report {
		switch {
		case rps == 100 && len(asked) > 4:
			return slowProbe
		case rps >= 195:
			return broken
		default:
			return scGood()
		}
	}

	for _, tc := range []struct {
		name   string
		plan   Plan
		target func(int) engine.Report
		want   string
	}{
		{"cap too low", capped, func(int) engine.Report { return scGood() }, "in-flight cap 130 is too low for 100 iterations/s"},
		{"generator short", quickPlan, scFrom195(scGood(), short), "scheduled iterations at 195 iterations/s"},
		{"cap reached", quickPlan, scFrom195(scGood(), withCap), "in-flight cap reached at 195 iterations/s"},
		{"no recovery", quickPlan, probing, "at 100 iterations/s"},
	} {
		asked = nil
		res, err := Search(t.Context(), tc.plan, fake(tc.target, &asked))
		if err != nil {
			t.Fatalf("%s: search: %v", tc.name, err)
		}

		var texts []string
		for _, s := range res.Steps {
			texts = append(texts, s.Why)
		}
		texts = append(texts, res.Notes...)

		if !slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(s, tc.want) }) {
			t.Errorf("%s: texts %q, want one with %q", tc.name, texts, tc.want)
		}
		for _, s := range texts {
			if strings.Contains(s, " rps") {
				t.Errorf("%s: %q counts rps", tc.name, s)
			}
		}
	}
}
