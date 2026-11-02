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
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// A plain run whose p99 tail the generator's lag decided is an invalid run,
// invalid_reasons generator_late. The test of the verdict is the one the JSON
// already uses for tail_wait_cause: engine.Report.WaitVerdict, run-wide.

const (
	lateA = "shop.v1.Catalog/Get"
	lateB = "shop.v1.Orders/List"
)

func lagQuantile(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Exact: true, Defined: true}
}

// lateMethod is a method whose p99 is p99 and would be without the
// client-side waits.
func lateMethod(name string, p99, without time.Duration) engine.MethodReport {
	return engine.MethodReport{
		Method: "/" + name, Sent: 1000, Latencies: 1000,
		P99: lagQuantile(p99), P99WithoutClientWaits: lagQuantile(without),
	}
}

// lateRun is a run in which the generator started calls late and that is the
// cause in the tail: the first method's p99 is 50ms, 5ms without the waits.
func lateRun(methods ...engine.MethodReport) RunReport {
	if len(methods) == 0 {
		methods = []engine.MethodReport{lateMethod(lateA, 50*time.Millisecond, 5*time.Millisecond)}
	}

	r := engine.Report{
		Duration: 10 * time.Second, Planned: 10 * time.Second, Methods: methods,
		GeneratorCauseCalls: 40, GeneratorTailCalls: 12,
		StartLagP99: lagQuantile(12 * time.Millisecond), StartLagMax: 30 * time.Millisecond,
	}
	for i := range methods {
		r.Sent += methods[i].Sent
	}

	return RunReport{Report: r}
}

func noteOf(t *testing.T, run RunReport, prefix string) string {
	t.Helper()

	notes := runNotes(run)
	if n, ok := noteStarting(notes, prefix); ok {
		return flat(n)
	}
	t.Fatalf("no note starting %q in:\n%s", prefix, strings.Join(notes, "\n\n"))

	return ""
}

// The rule is engine.Report.WaitVerdict's, not a new one: the generator is
// the cause that leads the p99 tail, a method's p99 drops by a tenth or more
// without the waits (or calls went unsent), the threshold of #140. A stream
// or a connection leading the tail is the run's limit on the target's side of
// the wire and leaves the run as it is. What the JSON calls
// tail_wait_cause "generator" and what makes the run invalid are one thing.
func TestGeneratorLate_ItIsTheGeneratorVerdictOfTheTail(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*engine.Report)
		want bool
	}{
		{"the generator leads the tail", func(*engine.Report) {}, true},
		{"a stream leads the tail", func(r *engine.Report) {
			r.StreamCauseCalls, r.StreamTailCalls = 30, 30
		}, false},
		{"a connection leads the tail", func(r *engine.Report) {
			r.ConnectionCauseCalls, r.ConnectionTailCalls = 30, 30
		}, false},
		{"a tie with the stream goes to the generator", func(r *engine.Report) {
			r.StreamCauseCalls, r.StreamTailCalls = 12, 12
		}, true},
		{"the generator lagged but is not in the tail", func(r *engine.Report) {
			r.GeneratorCauseCalls, r.GeneratorTailCalls = 500, 0
		}, false},
		{"p99 drops by exactly a tenth", func(r *engine.Report) {
			r.Methods[0] = lateMethod(lateA, 100*time.Millisecond, 90*time.Millisecond)
		}, true},
		{"p99 drops by a hair under a tenth", func(r *engine.Report) {
			r.Methods[0] = lateMethod(lateA, 100*time.Millisecond, 90*time.Millisecond+time.Microsecond)
		}, false},
		{"lag in the tail and p99 does not move", func(r *engine.Report) {
			r.Methods[0] = lateMethod(lateA, 50*time.Millisecond, 50*time.Millisecond)
		}, false},
		{"calls the generator left unsent, p99 does not move", func(r *engine.Report) {
			r.Methods[0] = lateMethod(lateA, 50*time.Millisecond, 50*time.Millisecond)
			r.Methods[0].NotSent, r.Methods[0].NotSentGenerator = 3, 3
			r.NotSent, r.NotSentGenerator = 3, 3
		}, true},
		{"calls left unsent for a stream", func(r *engine.Report) {
			r.Methods[0] = lateMethod(lateA, 50*time.Millisecond, 50*time.Millisecond)
			r.Methods[0].NotSent, r.Methods[0].NotSentStream = 3, 3
			r.NotSent, r.NotSentStream = 3, 3
			r.GeneratorCauseCalls, r.GeneratorTailCalls = 0, 0
			r.StreamCauseCalls, r.StreamTailCalls = 3, 3
		}, false},
		{"no lag at all", func(r *engine.Report) {
			r.GeneratorCauseCalls, r.GeneratorTailCalls = 0, 0
			r.StartLagP99, r.StartLagMax = metrics.Quantile{}, 0
			r.Methods[0] = lateMethod(lateA, 5*time.Millisecond, 5*time.Millisecond)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := lateRun()
			tc.edit(&run.Report)

			if got := GeneratorLate(run); got != tc.want {
				t.Errorf("GeneratorLate = %v, want %v", got, tc.want)
			}

			// One calculation: the JSON's tail cause says "generator" exactly then.
			cause := field(t, writeJSON(t, jsonRunOf(run)), "tail_wait_cause")
			if isGenerator := cause == "generator"; isGenerator != tc.want {
				t.Errorf("tail_wait_cause = %v while GeneratorLate = %v: the fixture and the rule disagree", cause, tc.want)
			}
		})
	}
}

// Calls left unsent by a stream or a connection, while the sent calls started a
// little late (over the floor, in the tail) and p99 does not move: the verdict
// rests on the unsent calls alone, so its cause is theirs. The run is not
// invalid for the generator, the report does not say the generator fell behind,
// and the JSON names the cause that left the calls unsent.
func TestGeneratorLate_UnsentCallsOfAnotherCauseAreNotTheGenerators(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*engine.Report)
		cause string
	}{
		{"connection", func(r *engine.Report) {
			r.Methods[0].NotSent, r.Methods[0].NotSentConnection = 3, 3
			r.NotSent, r.NotSentConnection = 3, 3
			r.ConnectionCauseCalls, r.ConnectionTailCalls = 3, 3
		}, "connection"},
		{"stream", func(r *engine.Report) {
			r.Methods[0].NotSent, r.Methods[0].NotSentStream = 3, 3
			r.NotSent, r.NotSentStream = 3, 3
			r.StreamCauseCalls, r.StreamTailCalls = 3, 3
		}, "stream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 12 sent calls started late in the tail (lateRun), p99 50ms and
			// 48ms without the waits: not moved.
			run := lateRun(lateMethod(lateA, 50*time.Millisecond, 48*time.Millisecond))
			tc.edit(&run.Report)

			if GeneratorLate(run) {
				t.Error("GeneratorLate = true: unsent calls of another cause make the run invalid for the generator")
			}
			if got := field(t, writeJSON(t, jsonRunOf(run)), "tail_wait_cause"); got != tc.cause {
				t.Errorf("tail_wait_cause = %v, want %s", got, tc.cause)
			}
			if got := anyStrings(field(t, writeJSON(t, jsonRunOf(run)), "invalid_reasons")); slices.Contains(got, "generator_late") {
				t.Errorf("invalid_reasons = %v", got)
			}
			if text := printedRun(run); strings.Contains(text, "the generator fell behind") {
				t.Errorf("the report blames the generator for calls the %s left unsent:\n%s", tc.name, text)
			}
		})
	}
}

func jsonRunOf(run RunReport) JSONRun {
	r := jsonRun(run.Report)
	r.Run = run

	return r
}

// The report says the run is invalid, which methods the lag decided and what to
// change; it replaces the line "limited by the run, not the target: the
// generator fell behind for N calls". The sentence is fixed word for word:
// <methods> are the ones the lag moved (and the ones that lost calls to the
// generator), joined by ", "; the start lag p99 and max come from the run; the
// tail ", N calls never sent" is there only when the generator left calls
// unsent.
func generatorLateSentence(run RunReport, methods string) string {
	s := "invalid run: the generator fell behind its schedule and decided the p99 tail of " + methods +
		": start lag p99 " + formatQuantile(run.StartLagP99) + ", max " + formatDuration(run.StartLagMax)
	if n := run.NotSentGenerator; n > 0 {
		s += ", " + strconv.Itoa(n) + " calls never sent"
	}

	return s + ". The latencies include this machine's delay and do not measure the target. " +
		"Give the generator more CPU, lower the rate, or split it over several machines."
}

func TestGeneratorLate_TheReportSaysInvalidAndWhatToChange(t *testing.T) {
	run := lateRun()

	want := generatorLateSentence(run, lateA)
	if got := noteOf(t, run, "invalid run: the generator fell behind"); !strings.HasPrefix(got, want) {
		t.Errorf("note:\n%s\nwant it to start:\n%s", got, want)
	}

	text := flat(printedRun(run))
	if !strings.Contains(text, want) {
		t.Errorf("the printed report does not say it:\n%s", printedRun(run))
	}
	if strings.Contains(text, "limited by the run") {
		t.Errorf("the old verdict stays beside the invalid one:\n%s", printedRun(run))
	}
	if n := strings.Count(text, "invalid run: the generator fell behind"); n != 1 {
		t.Errorf("the invalid verdict is printed %d times, want once", n)
	}
}

// The verdict is the run's, as the wait verdict is; the text names the methods
// the lag decided: those whose p99 drops by a tenth or more without the waits.
func TestGeneratorLate_OnlyTheMethodsTheLagDecidedAreNamed(t *testing.T) {
	run := lateRun(
		lateMethod(lateA, 50*time.Millisecond, 5*time.Millisecond),
		lateMethod(lateB, 8*time.Millisecond, 8*time.Millisecond),
	)

	if !GeneratorLate(run) {
		t.Fatal("GeneratorLate = false: a lag that moved one method's p99 makes the run invalid, whatever the others did")
	}

	note := noteOf(t, run, "invalid run: the generator fell behind")
	head, _, _ := strings.Cut(note, ": start lag p99")
	if !strings.Contains(head, lateA) || strings.Contains(head, lateB) {
		t.Errorf("methods named in %q, want %s and not %s", head, lateA, lateB)
	}

	both := lateRun(
		lateMethod(lateA, 50*time.Millisecond, 5*time.Millisecond),
		lateMethod(lateB, 40*time.Millisecond, 5*time.Millisecond),
	)
	head, _, _ = strings.Cut(noteOf(t, both, "invalid run: the generator fell behind"), ": start lag p99")
	if !strings.Contains(head, lateA) || !strings.Contains(head, lateB) {
		t.Errorf("methods named in %q, want both", head)
	}
}

// The methods named are those whose p99 moved by a tenth or more, and those
// that lost calls to the generator, in the report's order, joined by ", ". A
// verdict on unsent calls with a p99 that did not move names the methods that
// lost them, and says how many calls were never sent. The list is never empty
// when the run is invalid for the generator.
func TestGeneratorLate_TheMethodsAreTheMovedOnesAndTheOnesThatLostCalls(t *testing.T) {
	const lateC = "shop.v1.Cart/Add"

	lost := func(name string, n int) engine.MethodReport {
		m := lateMethod(name, 50*time.Millisecond, 48*time.Millisecond)
		m.NotSent, m.NotSentGenerator = n, n

		return m
	}
	steady := func(name string) engine.MethodReport { return lateMethod(name, 8*time.Millisecond, 8*time.Millisecond) }
	moved := func(name string) engine.MethodReport {
		return lateMethod(name, 50*time.Millisecond, 5*time.Millisecond)
	}

	for _, tc := range []struct {
		name    string
		methods []engine.MethodReport
		never   int
		want    string
	}{
		{"unsent only: the method that lost calls", []engine.MethodReport{lost(lateA, 3), steady(lateB)}, 3, lateA},
		{"moved and lost: both, in the report's order", []engine.MethodReport{steady(lateC), lost(lateB, 2), moved(lateA)}, 2, lateB + ", " + lateA},
		{"moved that also lost calls is named once", []engine.MethodReport{func() engine.MethodReport {
			m := moved(lateA)
			m.NotSent, m.NotSentGenerator = 4, 4

			return m
		}(), steady(lateB)}, 4, lateA},
		{"two that lost calls", []engine.MethodReport{lost(lateA, 1), steady(lateC), lost(lateB, 2)}, 3, lateA + ", " + lateB},
		{"moved only: no tail about unsent calls", []engine.MethodReport{moved(lateA), steady(lateB)}, 0, lateA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := lateRun(tc.methods...)
			run.NotSent, run.NotSentGenerator = tc.never, tc.never

			if !GeneratorLate(run) {
				t.Fatal("GeneratorLate = false, want the run invalid for the generator")
			}
			want := generatorLateSentence(run, tc.want)
			if got := noteOf(t, run, "invalid run: the generator fell behind"); !strings.HasPrefix(got, want) {
				t.Errorf("note:\n%s\nwant it to start:\n%s", got, want)
			}
			if strings.Contains(want, "calls never sent") != (tc.never > 0) {
				t.Fatalf("the fixture and the sentence disagree about the tail: %q", want)
			}
		})
	}
}

// The stream and the connection read as before: limited by the run (stream) or
// by the target's side (connection), the run itself complete.
func TestGeneratorLate_AStreamOrAConnectionLeavesTheVerdictAsItWas(t *testing.T) {
	stream := lateRun()
	stream.GeneratorCauseCalls, stream.GeneratorTailCalls = 0, 0
	stream.StreamWaited, stream.StreamCauseCalls, stream.StreamTailCalls = 30, 30, 30
	stream.StreamWaitP99 = exact(20)

	conn := lateRun()
	conn.GeneratorCauseCalls, conn.GeneratorTailCalls = 0, 0
	conn.ConnectionCauseCalls, conn.ConnectionTailCalls = 30, 30

	for name, tc := range map[string]struct {
		run  RunReport
		want string
	}{
		"stream":     {stream, "limited by the run, not the target: "},
		"connection": {conn, "the connection to the target was not ready for 30 calls"},
	} {
		notes := strings.Join(reportNotes(tc.run.Report, ""), "\n\n")
		if strings.Contains(notes, "invalid run:") {
			t.Errorf("%s: the run reads as invalid:\n%s", name, notes)
		}
		if !strings.Contains(notes, tc.want) {
			t.Errorf("%s: the verdict changed, no %q in:\n%s", name, tc.want, notes)
		}
		if GeneratorLate(tc.run) {
			t.Errorf("%s: GeneratorLate = true", name)
		}
	}
}

// The final screen's one-phrase verdict: invalid, not "limited by the run".
func TestGeneratorLate_TheShortVerdictIsInvalid(t *testing.T) {
	run := lateRun()

	if got, want := shortStreamVerdict(run.Report), "invalid run: the generator fell behind"; got != want {
		t.Errorf("short verdict %q, want %q", got, want)
	}

	stream := lateRun()
	stream.GeneratorCauseCalls, stream.GeneratorTailCalls = 0, 0
	stream.StreamCauseCalls, stream.StreamTailCalls = 30, 30
	if got := shortStreamVerdict(stream.Report); strings.HasPrefix(got, "invalid") {
		t.Errorf("short verdict of a stream %q", got)
	}
}

// The JSON: outcome and exit code are the caller's; the reason is the report's
// own, beside the others. The schema does not change: a new value in a list
// that is already open.
func TestGeneratorLate_TheJSONNamesTheReason(t *testing.T) {
	run := jsonRunOf(lateRun())
	run.Outcome = OutcomeInvalid

	out := writeJSON(t, run)
	if got := field(t, out, "invalid_reasons"); !slices.Equal(anyStrings(got), []string{"generator_late"}) {
		t.Errorf("invalid_reasons = %v, want [generator_late]", got)
	}
	if got := field(t, out, "tail_wait_cause"); got != "generator" {
		t.Errorf("tail_wait_cause = %v, want generator", got)
	}
	if got := field(t, out, "schema_version"); got != 1.0 {
		t.Errorf("schema_version = %v, want 1", got)
	}
	if notes := anyStrings(field(t, out, "notes")); !slices.ContainsFunc(notes, func(n string) bool {
		return strings.HasPrefix(flat(n), "invalid run: the generator fell behind")
	}) {
		t.Errorf("the notes of the JSON do not carry the verdict: %v", notes)
	}

	// Beside another reason: both, the cap first as for the clock.
	capped := lateRun()
	capped.CapHit = &engine.CapHit{Unsent: 1}
	capRun := jsonRunOf(capped)
	capRun.Outcome = OutcomeInvalid
	if got := anyStrings(field(t, writeJSON(t, capRun), "invalid_reasons")); !slices.Equal(got, []string{"in_flight_cap", "generator_late"}) {
		t.Errorf("invalid_reasons = %v, want [in_flight_cap generator_late]", got)
	}

	// A stream leading the tail is not a reason.
	stream := lateRun()
	stream.GeneratorCauseCalls, stream.GeneratorTailCalls = 0, 0
	stream.StreamCauseCalls, stream.StreamTailCalls = 30, 30
	if got := anyStrings(field(t, writeJSON(t, jsonRunOf(stream)), "invalid_reasons")); len(got) != 0 {
		t.Errorf("invalid_reasons = %v for a stream, want none", got)
	}
}

func anyStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, _ := e.(string)
		out = append(out, s)
	}

	return out
}

// A breaking-point search has its own causes (generator, run_limit): a step the
// generator held back ends the search as before, and its step report neither
// carries generator_late nor turns invalid.
func TestGeneratorLate_ASearchStepIsNotInvalidForIt(t *testing.T) {
	step := bpRun(breakpoint.RateStep, 125, 400, 21*time.Millisecond, true, breakpoint.CauseGenerator, "the generator fell behind")
	step.Report = lateRun().Report
	res := breakpoint.Result{
		Outcome: breakpoint.RunLimit, Held: 100, Broke: 125, Cause: breakpoint.CauseGenerator,
		Steps: []breakpoint.Step{bpRun(breakpoint.RateStep, 100, 400, 21*time.Millisecond, false, breakpoint.NoCause, ""), step},
	}

	out := searchJSON(t, res)
	bp, _ := out["breakpoint"].(map[string]any)
	if bp["outcome"] != "run_limit" || bp["why"] != "generator" {
		t.Errorf("outcome %v why %v, want run_limit generator", bp["outcome"], bp["why"])
	}
	runs, _ := bp["runs"].([]any)
	if len(runs) != 2 {
		t.Fatalf("%d runs, want 2", len(runs))
	}
	report := field(t, runs[1], "report")
	if got := field(t, report, "tail_wait_cause"); got != "generator" {
		t.Fatalf("tail_wait_cause = %v: the fixture is not a generator-limited step", got)
	}
	if got := anyStrings(field(t, report, "invalid_reasons")); slices.Contains(got, "generator_late") {
		t.Errorf("a search step carries generator_late: %v", got)
	}
	if got := field(t, report, "outcome"); got != OutcomeComplete {
		t.Errorf("the step's outcome %v, want %s", got, OutcomeComplete)
	}

	// The step's notes say what they said before: limited by the run. The new
	// text would contradict "complete" in the same object.
	notes := anyStrings(field(t, report, "notes"))
	if slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, "invalid run:") }) {
		t.Errorf("a search step's notes call it an invalid run: %v", notes)
	}
	if !slices.ContainsFunc(notes, func(n string) bool {
		return strings.Contains(flat(n), "limited by the run, not the target: the generator fell behind")
	}) {
		t.Errorf("a search step's notes lost \"limited by the run, not the target: the generator fell behind\": %v", notes)
	}
}
