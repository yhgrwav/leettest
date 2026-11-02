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

package threshold_test

import (
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

// A plain run whose p99 tail the generator decided is invalid. The reason of
// its thresholds stays the more telling one, "the generator was late", not the
// bare "invalid": the user reads the cause in the verdict line, not only in the
// run's reasons.

// generatorLate is a run whose wait verdict names the generator: a p99 of
// 50ms that is 5ms without the waits, the generator the cause of the tail.
func generatorLate() engine.Report {
	m := served(shop, 1000, 50*time.Millisecond)
	m.P99WithoutClientWaits = exact(5 * time.Millisecond)

	r := runOf(m)
	r.GeneratorCauseCalls, r.GeneratorTailCalls = 40, 12

	return r
}

func bothLimits() threshold.MethodLimits {
	return threshold.MethodLimits{Name: shop, Limits: p99Errors(200*time.Millisecond, "1%", 1000)}
}

// Ground: contract — invalid beats nothing the generator said: every limit is
// still unknown, and says the generator, for the latency and for the share of
// failures alike (a fail the run's side kept in a complete run has no place
// in an invalid one: nothing in it is decided).
func TestGeneratorLate_AnInvalidRunKeepsTheGeneratorAsTheReason(t *testing.T) {
	results := threshold.Evaluate(generatorLate(), threshold.Invalid, []threshold.MethodLimits{bothLimits()})

	verdictIs(t, find(t, results, shop, threshold.P99), threshold.VerdictUnknown, threshold.ReasonGenerator)
	verdictIs(t, find(t, results, shop, threshold.Errors), threshold.VerdictUnknown, threshold.ReasonGenerator)
}

// A run invalid for another reason as well (the in-flight cap, the clock) with
// the generator's verdict is still "the generator was late": in an ordinary run
// that verdict means generator_late is among the reasons, so the generator is
// the more telling thing to say.
func TestGeneratorLate_BesideAnotherInvalidReasonTheGeneratorIsStillTheReason(t *testing.T) {
	report := generatorLate()
	report.CapHit = &engine.CapHit{Unsent: 1}

	results := threshold.Evaluate(report, threshold.Invalid, []threshold.MethodLimits{bothLimits()})

	verdictIs(t, find(t, results, shop, threshold.P99), threshold.VerdictUnknown, threshold.ReasonGenerator)
	verdictIs(t, find(t, results, shop, threshold.Errors), threshold.VerdictUnknown, threshold.ReasonGenerator)
}

// A method of fewer than 100 observations (metrics.MinCount(0.99)) is not
// judged for the generator: its p99 is the maximum. In a run invalid for
// another reason (the cap) the limits are "invalid", not "the generator";
// with enough observations they stay "the generator was late".
func TestGeneratorLate_TheReasonNeedsEnoughObservations(t *testing.T) {
	few := generatorLate()
	few.Methods[0].Sent, few.Methods[0].Latencies = 15, 15
	few.Sent = 15
	few.CapHit = &engine.CapHit{Unsent: 1}

	results := threshold.Evaluate(few, threshold.Invalid, []threshold.MethodLimits{bothLimits()})
	verdictIs(t, find(t, results, shop, threshold.P99), threshold.VerdictUnknown, threshold.ReasonInvalid)
	verdictIs(t, find(t, results, shop, threshold.Errors), threshold.VerdictUnknown, threshold.ReasonInvalid)

	enough := generatorLate()
	enough.CapHit = &engine.CapHit{Unsent: 1}

	results = threshold.Evaluate(enough, threshold.Invalid, []threshold.MethodLimits{bothLimits()})
	verdictIs(t, find(t, results, shop, threshold.P99), threshold.VerdictUnknown, threshold.ReasonGenerator)
	verdictIs(t, find(t, results, shop, threshold.Errors), threshold.VerdictUnknown, threshold.ReasonGenerator)
}

// The stream and the connection are not the generator: an invalid run (a
// clock, a cap) stays "invalid" for them, as it was.
func TestGeneratorLate_OtherCausesKeepTheOldReasons(t *testing.T) {
	stream := generatorLate()
	stream.GeneratorCauseCalls, stream.GeneratorTailCalls = 0, 0
	stream.StreamCauseCalls, stream.StreamTailCalls = 40, 40

	conn := generatorLate()
	conn.GeneratorCauseCalls, conn.GeneratorTailCalls = 0, 0
	conn.ConnectionCauseCalls, conn.ConnectionTailCalls = 40, 40

	none := generatorLate()
	none.GeneratorCauseCalls, none.GeneratorTailCalls = 0, 0
	none.Methods[0].P99WithoutClientWaits = none.Methods[0].P99

	for name, report := range map[string]engine.Report{"stream": stream, "connection": conn, "no verdict": none} {
		results := threshold.Evaluate(report, threshold.Invalid, []threshold.MethodLimits{bothLimits()})

		for _, key := range []threshold.Key{threshold.P99, threshold.Errors} {
			r := find(t, results, shop, key)
			if r.Verdict != threshold.VerdictUnknown || r.Reason != threshold.ReasonInvalid {
				t.Errorf("%s %s: %q (%q), want unknown (invalid)", name, key, r.Verdict, r.Reason)
			}
		}
	}
}

// An unfinished run stays "incomplete" whatever the generator did: the
// generator is the reason only of a run that is invalid for it.
func TestGeneratorLate_AnIncompleteRunStaysIncomplete(t *testing.T) {
	results := threshold.Evaluate(generatorLate(), threshold.Incomplete, []threshold.MethodLimits{bothLimits()})

	verdictIs(t, find(t, results, shop, threshold.P99), threshold.VerdictUnknown, threshold.ReasonIncomplete)
	verdictIs(t, find(t, results, shop, threshold.Errors), threshold.VerdictUnknown, threshold.ReasonIncomplete)
}
