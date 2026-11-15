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

package cli

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// Text report of a scenario (v0.9 unit 2): the block after the method table,
// the iteration as a unit and one row per step. The reports here are built by
// hand: what the engine counts is the engine's tests.

const (
	scStep1 = "/pay.v1.Wallet/GetBalance"
	scStep2 = "/pay.v1.Wallet/Transfer"
	scStep3 = "/pay.v1.Wallet/GetHistory"
)

func scExact(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Exact: true, Defined: true}
}

func scLower(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Defined: true}
}

// scenarioRun is a stopped run of a three-step scenario: 120 iterations were
// scheduled, 100 ended, so Report.Scheduled and Iterations differ. At step 1
// three iterations failed, two were aborted and one was never sent (3/2/1: no
// two alike, so a swapped column shows); at step 2 five failed and one was
// aborted; at step 3 four failed. 84 iterations got to the end. Latencies are
// 84 successes and the 7 lower bounds of the timed-out and aborted ones: under
// 100, so p99 is too few observations.
func scenarioRun() RunReport {
	return RunReport{Report: engine.Report{
		Duration: 10 * time.Second, Planned: 10 * time.Second,
		Sent: 281, Failed: 12, NotSent: 1, Aborted: 3, Scheduled: 120,
		Methods: []engine.MethodReport{
			{Method: scStep1, Sent: 99, Failed: 3, RPS: 10, RPSLow: 12, RPSHigh: 12, PlannedDuration: 10 * time.Second},
			{Method: scStep2, Sent: 94, Failed: 5, RPS: 9},
			{Method: scStep3, Sent: 88, Failed: 4, RPS: 9},
		},
		Scenarios: []engine.ScenarioReport{{
			Name: "pay-and-check", Iterations: 100,
			ByCategory: map[engine.Category]int{
				engine.CategorySuccess: 84, engine.CategoryClientFault: 3, engine.CategoryOverload: 2,
				engine.CategoryServerFault: 4, engine.CategoryTimeout: 4, engine.CategoryAborted: 3,
			},
			P50: scExact(31200 * time.Microsecond), Latencies: 91, Censored: 7,
			Steps: []engine.ScenarioStepReport{
				{Method: scStep1, Reached: 100, FailedHere: 3, AbortedHere: 2, NotSentHere: 1, NotReached: 0},
				{Method: scStep2, Reached: 94, FailedHere: 5, AbortedHere: 1, NotSentHere: 0, NotReached: 6},
				{Method: scStep3, Reached: 88, FailedHere: 4, AbortedHere: 0, NotSentHere: 0, NotReached: 12},
			},
		}},
	}}
}

const scenarioNote = "  step latency is measured from the end of the previous step; the rows above count calls, the iteration counts runs"

// blockAt is the scenario block of the report: from the blank line before
// "scenario <name>" as many lines as want has. It fails the test when the
// block is not there.
func blockAt(t *testing.T, run RunReport, name string, n int) []string {
	t.Helper()

	lines := echoLines(run)
	i := indexOfPrefix(lines, "scenario "+name)
	if i < 1 {
		t.Fatalf("no \"scenario %s\" block in the report:\n%s", name, strings.Join(lines, "\n"))
	}
	if lines[i-1] != "" {
		t.Errorf("the block is not preceded by a blank line: %q", lines[i-1])
	}
	if i+n > len(lines) {
		t.Fatalf("the block has %d lines, want %d:\n%s", len(lines)-i, n, strings.Join(lines[i:], "\n"))
	}

	return lines[i : i+n]
}

// Ground: contract — the block of a three-step scenario, line by line. The
// iteration count is the iterations that ended (100), not what the plan
// scheduled (120); the outcome split is the categories' names, nonzero only,
// success first, in the order of the contract; p99 under the minimum count is
// told with the count, as a method's p99.9 is; each step row has its own
// failed, aborted and not-sent numbers. Mutations "print scheduled for
// iterations", "swap failed here and aborted here" and "drop not sent" die here.
func TestPrintReport_AScenarioBlockOfThreeSteps(t *testing.T) {
	want := []string{
		"scenario pay-and-check",
		"  100 iterations, started at 12/s: success 84, request_error 3, overload 2, failure 4, timed_out 4, aborted 3",
		"  iteration latency (tick to end of the last step): p50 31.2ms, p99 too few observations (91 of 100)",
		"  step method                                        reached failed here aborted here not sent not reached",
		"  1    pay.v1.Wallet/GetBalance                          100           3            2        1           0",
		"  2    pay.v1.Wallet/Transfer                             94           5            1        0           6",
		"  3    pay.v1.Wallet/GetHistory                           88           4            0        0          12",
		scenarioNote,
	}

	got := blockAt(t, scenarioRun(), "pay-and-check", len(want))

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

// The block is the report's third part: after every method row, before the
// ramp blocks.
// Ground: contract — order of the report.
func TestPrintReport_AScenarioBlockComesAfterTheMethodTable(t *testing.T) {
	lines := echoLines(scenarioRun())

	last := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "pay.v1.Wallet/") {
			last = i
		}
	}
	block := indexOfPrefix(lines, "scenario pay-and-check")

	if last < 0 || block < 0 || block < last {
		t.Errorf("method rows end at line %d, the block starts at %d, want the block after the table:\n%s",
			last, block, strings.Join(lines, "\n"))
	}
}

// Ground: contract — the same percentile rule as the methods (#73): below the
// minimum count p99 is not a number; from it, a number, a lower bound marked
// as the method rows mark it. The count is the observations behind the
// percentiles (Latencies), not the iterations.
func TestPrintReport_AScenarioP99NeedsTheMinimumCount(t *testing.T) {
	for _, tc := range []struct {
		name      string
		latencies int
		p50, p99  metrics.Quantile
		want      string
	}{
		{"no observations", 0, metrics.Quantile{}, metrics.Quantile{}, "p50 -, p99 too few observations (0 of 100)"},
		{"one observation", 1, scExact(8 * time.Millisecond), metrics.Quantile{}, "p50 8.00ms, p99 too few observations (1 of 100)"},
		{"one under the minimum", 99, scExact(8 * time.Millisecond), metrics.Quantile{}, "p50 8.00ms, p99 too few observations (99 of 100)"},
		{"the minimum", 100, scExact(8 * time.Millisecond), scExact(150 * time.Millisecond), "p50 8.00ms, p99 150ms"},
		{"a lower bound", 400, scLower(12300 * time.Microsecond), scLower(456 * time.Millisecond), "p50 >12.3ms, p99 >456ms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := scenarioRun()
			s := &run.Scenarios[0]
			s.P50, s.P99, s.Latencies = tc.p50, tc.p99, tc.latencies

			got := blockAt(t, run, "pay-and-check", 3)

			want := "  iteration latency (tick to end of the last step): " + tc.want
			if got[2] != want {
				t.Errorf("latency line:\n got %q\nwant %q", got[2], want)
			}
		})
	}
}

// Ground: contract — «started at» takes the form of the head call's plan: line
// (low-high when the plan has more than one rate), and a one-rate plan is one
// number.
func TestPrintReport_AScenarioStartedAtTakesThePlanLineForm(t *testing.T) {
	for _, tc := range []struct {
		name      string
		low, high int
		want      string
	}{
		{"one rate", 50, 50, "  100 iterations, started at 50/s: "},
		{"a plan with two rates", 50, 80, "  100 iterations, started at 50-80/s: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := scenarioRun()
			run.Methods[0].RPSLow, run.Methods[0].RPSHigh = tc.low, tc.high

			got := blockAt(t, run, "pay-and-check", 2)

			if !strings.HasPrefix(got[1], tc.want) {
				t.Errorf("line %q lacks the prefix %q", got[1], tc.want)
			}
		})
	}
}

// printedRows reads the step rows of the block as it was printed: the fields
// of each row, and the iterations of its first line.
func printedRows(t *testing.T, run RunReport, name string, steps int) (iterations int, rows [][]string) {
	t.Helper()

	block := blockAt(t, run, name, 4+steps)
	n, err := strconv.Atoi(strings.Fields(block[1])[0])
	if err != nil {
		t.Fatalf("the first line %q does not start with the iterations: %v", block[1], err)
	}
	for _, l := range block[4 : 4+steps] {
		rows = append(rows, strings.Fields(l))
	}

	return n, rows
}

// Ground: contract — the counters of the report hold on the numbers as they
// are printed, not only in the engine: the next step is reached by those the
// last one let through, and reached plus not reached is the iterations.
// Mutations "Reached(k+1) off by one" and "not sent is not printed" die here.
func TestPrintReport_AScenarioRowsKeepTheCounterInvariant(t *testing.T) {
	for name, run := range map[string]RunReport{"three steps, stopped": scenarioRun(), "two steps, timed out": scenarioTwoStepRun()} {
		t.Run(name, func(t *testing.T) {
			steps := len(run.Scenarios[0].Steps)
			iterations, rows := printedRows(t, run, run.Scenarios[0].Name, steps)

			num := func(row []string, col int) int {
				v, err := strconv.Atoi(row[col])
				if err != nil {
					t.Fatalf("row %v column %d: %v", row, col, err)
				}

				return v
			}

			for k, row := range rows {
				if len(row) != 7 {
					t.Fatalf("row %d has %d fields, want 7: %v", k+1, len(row), row)
				}
				reached, failed, aborted, notSent, notReached := num(row, 2), num(row, 3), num(row, 4), num(row, 5), num(row, 6)
				if reached+notReached != iterations {
					t.Errorf("step %d: reached %d + not reached %d != iterations %d", k+1, reached, notReached, iterations)
				}
				if k+1 < len(rows) {
					if next := num(rows[k+1], 2); next != reached-failed-aborted-notSent {
						t.Errorf("step %d: reached %d - failed %d - aborted %d - not sent %d = %d, step %d printed reached %d",
							k+1, reached, failed, aborted, notSent, reached-failed-aborted-notSent, k+2, next)
					}
				}
			}
		})
	}
}

// scenarioTwoStepRun is a finished run of a two-step scenario: 200 iterations,
// ten of them timed out at step 2, so the p99 is a lower bound.
func scenarioTwoStepRun() RunReport {
	return RunReport{Report: engine.Report{
		Duration: 10 * time.Second, Planned: 10 * time.Second,
		Sent: 400, Failed: 10, Scheduled: 500,
		Methods: []engine.MethodReport{
			{Method: scStep1, Sent: 200, RPS: 20, RPSLow: 20, RPSHigh: 20, PlannedDuration: 10 * time.Second},
			{Method: scStep2, Sent: 200, Failed: 10, RPS: 20},
		},
		Scenarios: []engine.ScenarioReport{{
			Name: "pay", Iterations: 200,
			ByCategory: map[engine.Category]int{engine.CategorySuccess: 190, engine.CategoryTimeout: 10},
			P50:        scExact(12300 * time.Microsecond), P99: scLower(456 * time.Millisecond),
			Latencies: 200, Censored: 10,
			Steps: []engine.ScenarioStepReport{
				{Method: scStep1, Reached: 200},
				{Method: scStep2, Reached: 200, FailedHere: 10},
			},
		}},
	}}
}

// Ground: contract — a second golden, a finished run with a lower bound and
// no failures before the last step; only categories that happened are named.
func TestPrintReport_AScenarioBlockOfTwoStepsWithALowerBound(t *testing.T) {
	want := []string{
		"scenario pay",
		"  200 iterations, started at 20/s: success 190, timed_out 10",
		"  iteration latency (tick to end of the last step): p50 12.3ms, p99 >456ms",
		"  step method                                        reached failed here aborted here not sent not reached",
		"  1    pay.v1.Wallet/GetBalance                          200           0            0        0           0",
		"  2    pay.v1.Wallet/Transfer                            200          10            0        0           0",
		scenarioNote,
	}

	got := blockAt(t, scenarioTwoStepRun(), "pay", len(want))

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

// The scenario ran and no iteration has ended yet (a stop in the first tick): the
// block is still printed, its first line has no colon after the rate and no
// categories, p50 is a dash, p99 tells the count, every step row is zeros.
// Ground: contract — a boundary of the block, as the zero rows of a method.
func TestPrintReport_AScenarioBlockWithNoIterationEnded(t *testing.T) {
	run := scenarioRun()
	run.Scenarios[0] = engine.ScenarioReport{
		Name: "pay-and-check",
		Steps: []engine.ScenarioStepReport{
			{Method: scStep1}, {Method: scStep2}, {Method: scStep3},
		},
	}
	want := []string{
		"scenario pay-and-check",
		"  0 iterations, started at 12/s",
		"  iteration latency (tick to end of the last step): p50 -, p99 too few observations (0 of 100)",
		"  step method                                        reached failed here aborted here not sent not reached",
		"  1    pay.v1.Wallet/GetBalance                            0           0            0        0           0",
		"  2    pay.v1.Wallet/Transfer                              0           0            0        0           0",
		"  3    pay.v1.Wallet/GetHistory                            0           0            0        0           0",
		scenarioNote,
	}

	got := blockAt(t, run, "pay-and-check", len(want))

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

// A step method that went silent: its note keeps what the silence says (the
// last answer) and has no rate, as a step has no plan and its sent rate is the
// head call's tick seen from one step, not a rate the user set. The head call
// keeps its rate.
// Ground: contract — A5, a number the plan does not have is not printed.
func TestRunNotes_AStepMethodSilenceNoteHasNoRate(t *testing.T) {
	run := scenarioRun()
	run.Methods[0].TimedOut, run.Methods[0].Timeout = 2, 3*time.Second
	from, last := 3, 2500*time.Millisecond
	step := &run.Methods[1]
	step.TimedOut, step.Timeout = 3, 500*time.Millisecond
	step.SilentFrom, step.LastAnswerAt, step.SentRPS = &from, &last, 9

	got := noteOf(t, run, "pay.v1.Wallet/Transfer:")

	want := "pay.v1.Wallet/Transfer: 3 of 94 calls (3.2%) got no answer within 500ms, " +
		"and nothing after the call sent at 2.5s of the run got one."
	if got != want {
		t.Errorf("step note:\n got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "rps") {
		t.Errorf("a step method's note names a rate: %q", got)
	}
	if head := noteOf(t, run, "pay.v1.Wallet/GetBalance:"); !strings.Contains(head, "at 12 rps") {
		t.Errorf("head note %q lost its rate, want \"at 12 rps\"", head)
	}
}

// A step method has no plan of its own (RPSLow = RPSHigh = PlannedDuration = 0),
// so its timeout note has no rate: «at 0 rps» would be a number that lies. The
// head call's note keeps the rate.
// Ground: contract — A5, a number the plan does not have is not printed.
func TestRunNotes_AStepMethodTimeoutNoteHasNoRate(t *testing.T) {
	run := scenarioRun()
	run.Methods[0].TimedOut, run.Methods[0].Timeout = 2, 3*time.Second
	run.Methods[1].TimedOut, run.Methods[1].Timeout = 3, 500*time.Millisecond

	got := noteOf(t, run, "pay.v1.Wallet/Transfer:")

	if want := "pay.v1.Wallet/Transfer: 3 of 94 calls (3.2%) got no answer within 500ms."; got != want {
		t.Errorf("step note:\n got %q\nwant %q", got, want)
	}
	if head := noteOf(t, run, "pay.v1.Wallet/GetBalance:"); !strings.Contains(head, "at 12 rps") {
		t.Errorf("head note %q lost its rate, want \"at 12 rps\"", head)
	}
}

// With a ramp the head call has its block by second; a step method has no
// planned rate to show beside the calls it sent, so it has none, and the
// scenario block speaks for it.
// Ground: contract — A5, no ramp block for a step method.
func TestPrintReport_ARampBlockIsNotPrintedForAStepMethod(t *testing.T) {
	run := scenarioRun()
	run.Warmup, run.SendingEnded = 4*time.Second, 10*time.Second
	run.Settings = &RunSettings{MaxInFlight: 5000, Connections: 1, Ramp: 4 * time.Second}
	run.Methods[0].Seconds = secondsOf(4, func(i int) engine.Second { return blockSecond(float64(3*(i+1)), 3*(i+1), 0) })
	for _, m := range []int{1, 2} {
		run.Methods[m].Seconds = secondsOf(4, func(int) engine.Second { return blockSecond(0, 20, 0) })
	}

	out := strings.Join(echoLines(run), "\n")

	if !strings.Contains(out, "ramp by second, pay.v1.Wallet/GetBalance:") {
		t.Errorf("the head call has no ramp block:\n%s", out)
	}
	for _, step := range []string{"pay.v1.Wallet/Transfer", "pay.v1.Wallet/GetHistory"} {
		if strings.Contains(out, "by second, "+step) {
			t.Errorf("a ramp block is printed for the step method %s:\n%s", step, out)
		}
	}
	if !strings.Contains(out, "scenario pay-and-check") {
		t.Errorf("the scenario block is missing:\n%s", out)
	}
}

// GUARD (green at once): a step method is an ordinary row of the method table
// with no plan line: the cells the plan fills stay empty, not zero.
// Ground: contract — spec 6, step methods stay as ordinary rows.
func TestPrintReport_AStepMethodRowHasNoPlanLine(t *testing.T) {
	out := strings.Join(echoLines(scenarioRun()), "\n")

	if n := strings.Count(out, "  plan: "); n != 1 {
		t.Errorf("%d plan lines, want 1 (the head call's):\n%s", n, out)
	}
	if strings.Contains(out, "plan: 0 rps") {
		t.Errorf("a step method prints a plan of 0 rps:\n%s", out)
	}
}

// GUARD (green at once): the generator verdict is per method (A7). A step
// method whose calls the generator left unsent past 1% makes the run
// generator_late, as the same method of a plain run does, and the scenario
// entry changes neither the answer nor the leading cause.
// Ground: contract — the verdict is the engine's rule over the method rows.
func TestGeneratorLate_ScenarioEntryChangesNeitherTheVerdictNorTheCause(t *testing.T) {
	late := func(withScenario bool) RunReport {
		run := scenarioRun()
		step := &run.Methods[1]
		step.NotSent, step.NotSentGenerator = 5, 5
		run.NotSent, run.NotSentGenerator = 5, 5
		run.GeneratorCauseCalls, run.GeneratorTailCalls = 40, 12
		if !withScenario {
			run.Scenarios = nil
		}

		return run
	}

	with, without := late(true), late(false)

	if !GeneratorLate(without) {
		t.Fatal("the plain run is not generator_late: the guard is not set up")
	}
	if !GeneratorLate(with) {
		t.Error("GeneratorLate = false with a scenario entry, want true as without it")
	}
	causeWith, _, okWith := with.WaitVerdict()
	causeWithout, _, okWithout := without.WaitVerdict()
	if causeWith != causeWithout || okWith != okWithout {
		t.Errorf("wait verdict %v/%v with the scenario entry, %v/%v without", causeWith, okWith, causeWithout, okWithout)
	}
}
