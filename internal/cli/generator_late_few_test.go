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
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// A p99 of fewer than metrics.MinCount(0.99) = 100 observations is the maximum:
// one call that started over the floor moves it. generator_late judges a method
// only from 100 observations; below that the verdict of the wait stays
// (tail_wait_cause "generator", the note of a run limited by itself), the run
// is complete. Calls the generator left unsent are a fact, not a percentile,
// and count at any size. One predicate (engine.Report.GeneratorDecidedTail)
// answers for the exit code, the text, the short verdict and the JSON.

func observed(m engine.MethodReport, n int) engine.MethodReport {
	m.Sent, m.Latencies = n, n

	return m
}

func lostToGenerator(m engine.MethodReport, k int) engine.MethodReport {
	m.NotSent, m.NotSentGenerator = k, k

	return m
}

func lateRunOf(methods ...engine.MethodReport) RunReport {
	run := lateRun(methods...)
	run.NotSent, run.NotSentGenerator = 0, 0
	for i := range methods {
		run.NotSent += methods[i].NotSent
		run.NotSentGenerator += methods[i].NotSentGenerator
	}

	return run
}

func movedM(name string, n int) engine.MethodReport {
	return observed(lateMethod(name, 50*time.Millisecond, 5*time.Millisecond), n)
}

func steadyM(name string, n int) engine.MethodReport {
	return observed(lateMethod(name, 8*time.Millisecond, 8*time.Millisecond), n)
}

func TestGeneratorLate_ARunOfFewObservationsIsNotInvalidForIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  RunReport
		want bool
		// named is the methods the invalid text names; "" when the run is not invalid.
		named string
		never int
	}{
		{"99 observations, moved", lateRunOf(movedM(lateA, 99)), false, "", 0},
		{"100 observations, moved", lateRunOf(movedM(lateA, 100)), true, lateA, 0},
		{"two methods of 60, both moved: the count is per method",
			lateRunOf(movedM(lateA, 60), movedM(lateB, 60)), false, "", 0},
		{"1000 unmoved and 15 moved",
			lateRunOf(steadyM(lateA, 1000), movedM(lateB, 15)), false, "", 0},
		{"15 observations, p99 unmoved, one call left unsent by the generator",
			lateRunOf(lostToGenerator(steadyM(lateA, 15), 1)), true, lateA, 1},
		{"15 moved, another method lost a call: only the one that lost it is named",
			lateRunOf(movedM(lateA, 15), lostToGenerator(steadyM(lateB, 15), 1)), true, lateB, 1},
		{"1000 moved and 15 moved: only the one with enough is named",
			lateRunOf(movedM(lateA, 1000), movedM(lateB, 15)), true, lateA, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := tc.run

			if got := GeneratorLate(run); got != tc.want {
				t.Fatalf("GeneratorLate = %v, want %v", got, tc.want)
			}

			// The wait verdict is as it was: the generator, in every row.
			out := writeJSON(t, jsonRunOf(run))
			if got := field(t, out, "tail_wait_cause"); got != "generator" {
				t.Errorf("tail_wait_cause = %v, want generator", got)
			}
			if got := slices.Contains(anyStrings(field(t, out, "invalid_reasons")), "generator_late"); got != tc.want {
				t.Errorf("invalid_reasons has generator_late = %v, want %v", got, tc.want)
			}

			text := flat(printedRun(run))
			if tc.want {
				want := generatorLateSentence(run, tc.named)
				if !strings.Contains(text, want) {
					t.Errorf("the report does not say, word for word:\n%s\ngot:\n%s", want, printedRun(run))
				}

				return
			}

			if strings.Contains(text, "invalid run: the generator") {
				t.Errorf("the report calls the run invalid:\n%s", printedRun(run))
			}
			if !strings.Contains(text, "limited by the run, not the target: the generator fell behind") {
				t.Errorf("the note of a run limited by itself is gone:\n%s", printedRun(run))
			}
		})
	}
}

// The final screen's one phrase follows the same predicate.
func TestGeneratorLate_TheShortVerdictOfFewObservationsIsLimitedByTheRun(t *testing.T) {
	few := lateRunOf(movedM(lateA, 15))
	if got, want := shortStreamVerdict(few.Report), "limited by the run: the generator fell behind"; got != want {
		t.Errorf("short verdict %q, want %q", got, want)
	}

	enough := lateRunOf(movedM(lateA, 100))
	if got, want := shortStreamVerdict(enough.Report), "invalid run: the generator fell behind"; got != want {
		t.Errorf("short verdict %q, want %q", got, want)
	}

	lost := lateRunOf(lostToGenerator(steadyM(lateA, 15), 1))
	if got, want := shortStreamVerdict(lost.Report), "invalid run: the generator fell behind"; got != want {
		t.Errorf("short verdict of a lost call %q, want %q", got, want)
	}
}

// The notes of the JSON carry the same text as the report.
func TestGeneratorLate_TheJSONNotesOfFewObservationsAreTheRunLimit(t *testing.T) {
	run := jsonRunOf(lateRunOf(movedM(lateA, 99)))

	notes := anyStrings(field(t, writeJSON(t, run), "notes"))
	if slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, "invalid run:") }) {
		t.Errorf("the notes call the run invalid: %v", notes)
	}
	if !slices.ContainsFunc(notes, func(n string) bool {
		return strings.Contains(flat(n), "limited by the run, not the target: the generator fell behind")
	}) {
		t.Errorf("the notes lost the run-limit line: %v", notes)
	}
}
