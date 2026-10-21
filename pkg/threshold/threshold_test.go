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

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

const shop = "shop.v1.Catalog/Get"

func exact(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Exact: true, Defined: true}
}

func lowerBound(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Defined: true}
}

// served is a method that sent n calls, all answered, with every percentile at
// latency.
func served(name string, n int, latency time.Duration) engine.MethodReport {
	q := exact(latency)

	return engine.MethodReport{
		Method: name, Sent: n, Latencies: n,
		Min: q, P50: q, P90: q, P95: q, P99: q, P999: q, Max: q, P99WithoutClientWaits: q,
	}
}

func runOf(methods ...engine.MethodReport) engine.Report {
	r := engine.Report{Methods: methods}
	for i := range methods {
		r.Sent += methods[i].Sent
		r.Failed += methods[i].Failed
	}

	return r
}

func p99Errors(p99 time.Duration, errors string, thousandths int64) threshold.Limits {
	return threshold.Limits{P99: p99, Errors: &threshold.Percent{Thousandths: thousandths, Text: errors}}
}

func evaluate(report engine.Report, limits ...threshold.MethodLimits) []threshold.Result {
	return threshold.Evaluate(report, threshold.Complete, limits)
}

// find is the one result for the method and key, failing the test when the
// run produced none or several.
func find(t *testing.T, results []threshold.Result, method string, key threshold.Key) threshold.Result {
	t.Helper()

	var found []threshold.Result
	for i := range results {
		if results[i].Method == method && results[i].Key == key {
			found = append(found, results[i])
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d results for %s %s, want 1, in %+v", len(found), method, key, results)
	}

	return found[0]
}

func verdictIs(t *testing.T, r threshold.Result, verdict threshold.Verdict, reason threshold.Reason) {
	t.Helper()

	if r.Verdict != verdict || r.Reason != reason {
		t.Errorf("%s %s: %q (%q), want %q (%q)", r.Method, r.Key, r.Verdict, r.Reason, verdict, reason)
	}
}

// Ground: contract — one result per limit that is set, in the config's order:
// each method's p50 to p999, then errors; a limit not set has no result.
func TestEvaluate_OneResultPerLimitInOrder(t *testing.T) {
	report := runOf(served("a.v1.A/One", 1000, 10*time.Millisecond), served("b.v1.B/Two", 1000, 10*time.Millisecond))
	results := evaluate(report,
		threshold.MethodLimits{Name: "a.v1.A/One", Limits: p99Errors(50*time.Millisecond, "1%", 1000)},
		threshold.MethodLimits{Name: "b.v1.B/Two", Limits: threshold.Limits{
			P50: 50 * time.Millisecond, P999: 50 * time.Millisecond,
			Errors: &threshold.Percent{Thousandths: 1000, Text: "1%"},
		}},
	)

	var got []string
	for _, r := range results {
		got = append(got, r.Method+" "+string(r.Key))
	}
	want := []string{"a.v1.A/One p99", "a.v1.A/One errors", "b.v1.B/Two p50", "b.v1.B/Two p999", "b.v1.B/Two errors"}
	if len(got) != len(want) {
		t.Fatalf("results %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("results %q, want %q", got, want)
		}
	}
}

// Ground: contract — each key reads its own percentile: with p50..p999 at
// 10, 20, 30, 40, 50ms and a 35ms limit on each, the ones above it fail.
func TestEvaluate_EachKeyReadsItsPercentile(t *testing.T) {
	m := served(shop, 1000, 0)
	m.P50, m.P90, m.P95, m.P99, m.P999 = exact(10*time.Millisecond), exact(20*time.Millisecond),
		exact(30*time.Millisecond), exact(40*time.Millisecond), exact(50*time.Millisecond)
	limit := 35 * time.Millisecond

	results := evaluate(runOf(m), threshold.MethodLimits{Name: shop, Limits: threshold.Limits{
		P50: limit, P90: limit, P95: limit, P99: limit, P999: limit, Errors: &threshold.Percent{Thousandths: 1000, Text: "1%"},
	}})

	for key, want := range map[threshold.Key]threshold.Verdict{
		threshold.P50: threshold.VerdictPass, threshold.P90: threshold.VerdictPass, threshold.P95: threshold.VerdictPass,
		threshold.P99: threshold.VerdictFail, threshold.P999: threshold.VerdictFail,
	} {
		verdictIs(t, find(t, results, shop, key), want, "")
	}
}

// Ground: boundary — the limit is compared with the value in nanoseconds, the
// one the run JSON carries as value.ns, not with whole microseconds cut off it:
// a value of 200.015871ms is over a 200015us limit (fail) and under a 200016us
// one (pass). Synthetic: through a fake target a bucket's top cannot be
// predicted.
func TestLatency_ComparesTheValueInNanoseconds(t *testing.T) {
	m := served(shop, 1000, 0)
	m.P99 = exact(200*time.Millisecond + 15871*time.Nanosecond)

	for _, tc := range []struct {
		limit time.Duration
		want  threshold.Verdict
	}{
		{200015 * time.Microsecond, threshold.VerdictFail},
		{200016 * time.Microsecond, threshold.VerdictPass},
		{200014 * time.Microsecond, threshold.VerdictFail},
		{200 * time.Millisecond, threshold.VerdictFail},
	} {
		r := find(t, evaluate(runOf(m), threshold.MethodLimits{Name: shop, Limits: p99Errors(tc.limit, "1%", 1000)}), shop, threshold.P99)
		verdictIs(t, r, tc.want, "")
		if r.Limit != tc.limit || r.Value.Value != m.P99.Value {
			t.Errorf("limit %v value %v in the result, want %v and %v", r.Limit, r.Value.Value, tc.limit, m.P99.Value)
		}
	}
}

// Ground: boundary — a percentile that is only a lower bound (it landed among
// timeouts) fails when the bound is already over the limit, and is unknown
// when it is not: the true value is above the bound by an unknown amount.
// Through the CLI the second case needs a limit within 1/1024 of the timeout
// (the timeout's bucket reports its lowest equivalent value) or a timeout over
// an hour.
func TestLatency_ALowerBoundDecidesOnlyWhenOverTheLimit(t *testing.T) {
	m := served(shop, 1000, 10*time.Millisecond)
	m.P99, m.Censored, m.TimedOut = lowerBound(180*time.Millisecond), 20, 20
	limits := threshold.MethodLimits{Name: shop, Limits: p99Errors(200*time.Millisecond, "1%", 1000)}

	verdictIs(t, find(t, evaluate(runOf(m), limits), shop, threshold.P99), threshold.VerdictUnknown, threshold.ReasonCensored)

	m.P99 = lowerBound(220 * time.Millisecond)
	verdictIs(t, find(t, evaluate(runOf(m), limits), shop, threshold.P99), threshold.VerdictFail, "")

	// A bound equal to the limit is still not above it.
	m.P99 = lowerBound(200 * time.Millisecond)
	verdictIs(t, find(t, evaluate(runOf(m), limits), shop, threshold.P99), threshold.VerdictUnknown, threshold.ReasonCensored)
}

// Ground: boundary — a latency under a 100us limit by less than a microsecond
// still fails, exact or a lower bound: under a millisecond one microsecond is
// up to 1% of the value, and the JSON's value.ns says what the row must not
// contradict. A value equal to the limit is not over it (exact: pass, bound:
// unknown), one nanosecond above it is. Synthetic: a histogram bucket's top
// cannot be set through a fake target.
func TestLatency_ASubMicrosecondOvershootFails(t *testing.T) {
	const limit = 100 * time.Microsecond

	limits := threshold.MethodLimits{Name: shop, Limits: p99Errors(limit, "1%", 1000)}

	for _, tc := range []struct {
		name   string
		value  metrics.Quantile
		want   threshold.Verdict
		reason threshold.Reason
	}{
		{"exact 100.4us", exact(100400 * time.Nanosecond), threshold.VerdictFail, ""},
		{"exact 100us + 1ns", exact(limit + 1), threshold.VerdictFail, ""},
		{"exact 100us", exact(limit), threshold.VerdictPass, ""},
		{"exact 100us - 1ns", exact(limit - 1), threshold.VerdictPass, ""},
		{"bound 100.4us", lowerBound(100400 * time.Nanosecond), threshold.VerdictFail, ""},
		{"bound 100us + 1ns", lowerBound(limit + 1), threshold.VerdictFail, ""},
		{"bound 100us", lowerBound(limit), threshold.VerdictUnknown, threshold.ReasonCensored},
		{"bound 100us - 1ns", lowerBound(limit - 1), threshold.VerdictUnknown, threshold.ReasonCensored},
	} {
		m := served(shop, 1000, 0)
		m.P99 = tc.value

		r := find(t, evaluate(runOf(m), limits), shop, threshold.P99)
		if r.Verdict != tc.want || r.Reason != tc.reason {
			t.Errorf("%s against %v: %q (reason %q), want %q (reason %q)", tc.name, limit, r.Verdict, r.Reason, tc.want, tc.reason)
		}
	}
}

// Ground: boundary — a p99.9 with fewer than 1000 observations is not
// defined; a limit on it is unknown, never pass.
func TestLatency_AnUndefinedP999IsUnknown(t *testing.T) {
	m := served(shop, 500, 10*time.Millisecond)
	m.P999 = metrics.Quantile{}

	r := find(t, evaluate(runOf(m), threshold.MethodLimits{Name: shop, Limits: threshold.Limits{
		P999: time.Second, Errors: &threshold.Percent{Thousandths: 1000, Text: "1%"},
	}}), shop, threshold.P999)

	verdictIs(t, r, threshold.VerdictUnknown, threshold.ReasonFewObservations)
}

// Ground: boundary — the share is compared in integers, so a limit written
// 0.7% holds for exactly 7 failures in 1000 and not for 8; a float division
// says 7/1000 > 0.7/100.
func TestErrors_ExactlyAtTheLimitPasses(t *testing.T) {
	for _, tc := range []struct {
		failed int
		want   threshold.Verdict
	}{
		{6, threshold.VerdictPass}, {7, threshold.VerdictPass}, {8, threshold.VerdictFail},
	} {
		m := served(shop, 1000, 10*time.Millisecond)
		m.Failed = tc.failed

		r := find(t, evaluate(runOf(m), threshold.MethodLimits{Name: shop, Limits: threshold.Limits{
			Errors: &threshold.Percent{Thousandths: 700, Text: "0.7%"},
		}}), shop, threshold.Errors)

		verdictIs(t, r, tc.want, "")
		if r.Bad != tc.failed || r.Total != 1000 || r.LimitPercent != "0.7%" {
			t.Errorf("%d failed: bad %d of %d against %q, want %d of 1000 against 0.7%%", tc.failed, r.Bad, r.Total, r.LimitPercent, tc.failed)
		}
	}
}

// Ground: boundary — the same exactness in the other direction, on the other
// kinds of limit: 0% allows no failure, 100% allows all of them, and a limit
// of 0.001% (one thousandth) is exact on 1 in 100000.
func TestErrors_TheEdgesOfThePercentScale(t *testing.T) {
	for _, tc := range []struct {
		thousandths int64
		sent        int
		failed      int
		want        threshold.Verdict
	}{
		{0, 1000, 0, threshold.VerdictPass}, {0, 1000, 1, threshold.VerdictFail},
		{100000, 1000, 1000, threshold.VerdictPass},
		{1, 100000, 1, threshold.VerdictPass}, {1, 100000, 2, threshold.VerdictFail},
	} {
		m := served(shop, tc.sent, 10*time.Millisecond)
		m.Failed = tc.failed

		r := find(t, evaluate(runOf(m), threshold.MethodLimits{Name: shop, Limits: threshold.Limits{
			Errors: &threshold.Percent{Thousandths: tc.thousandths},
		}}), shop, threshold.Errors)

		verdictIs(t, r, tc.want, "")
	}
}

// Ground: contract — a call that timed out in the wait for a connection never
// reached the target and is in neither sent nor failed, yet it is the
// target's refusal: the share counts it on both sides. 100 failed of 27000 sent
// is 0.37%; with 3000 unsent for the connection it is 3100 of 30000.
func TestErrors_CallsNotSentForTheConnectionCount(t *testing.T) {
	m := served(shop, 27000, 10*time.Millisecond)
	m.Failed, m.NotSent, m.NotSentConnection = 100, 3000, 3000
	report := runOf(m)
	report.NotSent, report.NotSentConnection = 3000, 3000

	r := find(t, evaluate(report, threshold.MethodLimits{Name: shop, Limits: threshold.Limits{
		Errors: &threshold.Percent{Thousandths: 1000, Text: "1%"},
	}}), shop, threshold.Errors)

	verdictIs(t, r, threshold.VerdictFail, "")
	if r.Bad != 3100 || r.Total != 30000 || r.BadClient != 0 {
		t.Errorf("bad %d (client %d) of %d, want 3100 (0) of 30000", r.Bad, r.BadClient, r.Total)
	}
}

// Ground: boundary — a run that sent nothing has no share: unknown, not a
// pass of 0 of 0; while calls all lost to the connection are all failures.
func TestErrors_NothingSentIsUnknown(t *testing.T) {
	limits := threshold.MethodLimits{Name: shop, Limits: threshold.Limits{Errors: &threshold.Percent{Thousandths: 1000, Text: "1%"}}}

	verdictIs(t, find(t, evaluate(runOf(served(shop, 0, 0)), limits), shop, threshold.Errors),
		threshold.VerdictUnknown, threshold.ReasonNothingSent)

	m := served(shop, 0, 0)
	m.NotSent, m.NotSentConnection = 500, 500
	report := runOf(m)
	report.NotSent, report.NotSentConnection = 500, 500
	r := find(t, evaluate(report, limits), shop, threshold.Errors)
	verdictIs(t, r, threshold.VerdictFail, "")
	if r.Bad != 500 || r.Total != 500 {
		t.Errorf("bad %d of %d, want 500 of 500", r.Bad, r.Total)
	}
}

// Ground: signal — the config's and the client's refusals (a rejected request,
// an error before sending, our bad response) say nothing of whether the target
// copes, so they never turn a pass into a fail: the share over all failures
// within the limit passes; the share without them over the limit fails; in
// between it is unknown with the reason client_errors.
func TestErrors_TheConfigsAndTheClientsRefusalsDoNotDecideAFail(t *testing.T) {
	limit := func(thousandths int64) threshold.MethodLimits {
		return threshold.MethodLimits{Name: shop, Limits: threshold.Limits{Errors: &threshold.Percent{Thousandths: thousandths}}}
	}
	rejected := func(failed, rejected, clientErr, bad int) engine.Report {
		m := served(shop, 1000, 10*time.Millisecond)
		m.Failed, m.ClientError = failed, clientErr
		m.Rejected.Count, m.BadResponse.Count = rejected, bad

		return runOf(m)
	}

	// 2% of the calls were rejected as invalid, limit 1%: unknown, not fail.
	r := find(t, evaluate(rejected(20, 20, 0, 0), limit(1000)), shop, threshold.Errors)
	verdictIs(t, r, threshold.VerdictUnknown, threshold.ReasonClientErrors)
	if r.Bad != 20 || r.BadClient != 20 || r.Total != 1000 {
		t.Errorf("bad %d (client %d) of %d, want 20 (20) of 1000", r.Bad, r.BadClient, r.Total)
	}

	// The same set as the engine's RequestRejected: rejected, client errors, bad responses.
	r = find(t, evaluate(rejected(20, 5, 7, 8), limit(1000)), shop, threshold.Errors)
	verdictIs(t, r, threshold.VerdictUnknown, threshold.ReasonClientErrors)
	if r.BadClient != 20 {
		t.Errorf("bad_client %d, want 5 + 7 + 8", r.BadClient)
	}

	// Within the limit even counting them: pass.
	verdictIs(t, find(t, evaluate(rejected(20, 20, 0, 0), limit(2000)), shop, threshold.Errors), threshold.VerdictPass, "")

	// 30 failed, 20 of them rejected: the target's 10 are 1.0%. At a 1% limit that
	// is not over it, so unknown; at 0.9% it is, so fail.
	verdictIs(t, find(t, evaluate(rejected(30, 20, 0, 0), limit(1000)), shop, threshold.Errors),
		threshold.VerdictUnknown, threshold.ReasonClientErrors)
	verdictIs(t, find(t, evaluate(rejected(30, 20, 0, 0), limit(900)), shop, threshold.Errors),
		threshold.VerdictFail, "")
}

// lagged is a report in which the generator held back calls and that is the
// cause in the p99 tail: the run's limit, not the target's.
func lagged(m engine.MethodReport, unsent int) engine.Report {
	m.NotSent, m.NotSentGenerator = unsent, unsent
	r := runOf(m)
	r.NotSent, r.NotSentGenerator, r.GeneratorTailCalls = unsent, unsent, unsent

	return r
}

// Ground: signal — latency is counted from the schedule, so a generator that
// fell behind lengthens it, and calls it never sent are in no distribution:
// neither says anything of the target. A latency limit is unknown whatever
// the percentile, a pass or a fail alike, reason generator; with the stream
// as the cause the reason is stream.
func TestRunSide_AGeneratorOrStreamWaitMakesLatencyUnknown(t *testing.T) {
	limits := threshold.MethodLimits{Name: shop, Limits: p99Errors(200*time.Millisecond, "1%", 1000)}

	for _, latency := range []time.Duration{20 * time.Millisecond, 312 * time.Millisecond} {
		r := find(t, evaluate(lagged(served(shop, 400, latency), 100), limits), shop, threshold.P99)
		verdictIs(t, r, threshold.VerdictUnknown, threshold.ReasonGenerator)
	}

	m := served(shop, 400, 20*time.Millisecond)
	m.NotSent, m.NotSentStream = 50, 50
	report := runOf(m)
	report.NotSent, report.NotSentStream, report.StreamTailCalls = 50, 50, 50
	verdictIs(t, find(t, evaluate(report, limits), shop, threshold.P99), threshold.VerdictUnknown, threshold.ReasonStream)
}

// Ground: signal — for the share of failures the run's side cannot make a
// fail into a pass: a fail stays a fail (failures are the target's), a pass
// becomes unknown, because 500 rps were never offered.
func TestRunSide_ErrorsKeepAFailAndLoseAPass(t *testing.T) {
	limits := threshold.MethodLimits{Name: shop, Limits: threshold.Limits{Errors: &threshold.Percent{Thousandths: 1000, Text: "1%"}}}

	ok := served(shop, 400, 20*time.Millisecond)
	verdictIs(t, find(t, evaluate(lagged(ok, 100), limits), shop, threshold.Errors), threshold.VerdictUnknown, threshold.ReasonGenerator)

	bad := served(shop, 400, 20*time.Millisecond)
	bad.Failed = 40
	verdictIs(t, find(t, evaluate(lagged(bad, 100), limits), shop, threshold.Errors), threshold.VerdictFail, "")
}

// Ground: signal — the run's side is the run's: when the wait verdict names
// the generator or a stream, every method's latency is unknown, also one with
// no unsent calls of its own.
func TestRunSide_TheVerdictIsOfTheWholeRun(t *testing.T) {
	lag := served("a.v1.A/One", 400, 20*time.Millisecond)
	clean := served("b.v1.B/Two", 1000, 20*time.Millisecond)
	report := lagged(lag, 100)
	report.Methods = append(report.Methods, clean)
	report.Sent += clean.Sent

	results := evaluate(report,
		threshold.MethodLimits{Name: "a.v1.A/One", Limits: p99Errors(200*time.Millisecond, "1%", 1000)},
		threshold.MethodLimits{Name: "b.v1.B/Two", Limits: p99Errors(200*time.Millisecond, "1%", 1000)})

	verdictIs(t, find(t, results, "b.v1.B/Two", threshold.P99), threshold.VerdictUnknown, threshold.ReasonGenerator)
}

// Ground: signal — unsent calls whose cause nobody named (a sender defect)
// are in NotSent but in none of the three counts; they still make latency
// unknown, with the reason generator, not a pass of the calls that were sent.
func TestRunSide_UnsentCallsOfNoNamedCauseMakeLatencyUnknown(t *testing.T) {
	m := served(shop, 960, 20*time.Millisecond)
	m.NotSent = 40
	report := runOf(m)
	report.NotSent = 40

	r := find(t, evaluate(report, threshold.MethodLimits{Name: shop, Limits: p99Errors(200*time.Millisecond, "1%", 1000)}), shop, threshold.P99)

	verdictIs(t, r, threshold.VerdictUnknown, threshold.ReasonGenerator)
}

// Ground: signal — a connection not ready is the target's side (or the
// network's), so a run whose unsent calls are all that is judged: latency
// by what was sent, errors with the unsent counted as failures.
func TestRunSide_ConnectionWaitsAreTheTargetsAndDoNotMakeLatencyUnknown(t *testing.T) {
	m := served(shop, 900, 20*time.Millisecond)
	m.NotSent, m.NotSentConnection = 100, 100
	report := runOf(m)
	report.NotSent, report.NotSentConnection, report.ConnectionTailCalls = 100, 100, 100

	results := evaluate(report, threshold.MethodLimits{Name: shop, Limits: p99Errors(200*time.Millisecond, "1%", 1000)})

	verdictIs(t, find(t, results, shop, threshold.P99), threshold.VerdictPass, "")
	verdictIs(t, find(t, results, shop, threshold.Errors), threshold.VerdictFail, "")
}

// Ground: contract — the reason is from the closed list the JSON documents:
// a method that also has generator-held calls while the tail's cause is the
// connection (side target) is unknown with generator, not with "connection".
func TestRunSide_TheReasonStaysInTheClosedList(t *testing.T) {
	m := served(shop, 800, 20*time.Millisecond)
	m.NotSent, m.NotSentGenerator, m.NotSentConnection = 200, 60, 140
	report := runOf(m)
	report.NotSent, report.NotSentGenerator, report.NotSentConnection = 200, 60, 140
	report.GeneratorTailCalls, report.ConnectionTailCalls = 60, 140

	r := find(t, evaluate(report, threshold.MethodLimits{Name: shop, Limits: p99Errors(200*time.Millisecond, "1%", 1000)}), shop, threshold.P99)

	verdictIs(t, r, threshold.VerdictUnknown, threshold.ReasonGenerator)
}

// Ground: contract — a run that did not finish is not checked: every limit is
// unknown, with incomplete for a run stopped early and invalid for a run
// whose numbers do not describe the target, whatever the values would say.
func TestEvaluate_AnUnfinishedRunDecidesNothing(t *testing.T) {
	m := served(shop, 1000, 900*time.Millisecond)
	m.Failed = 500
	limits := threshold.MethodLimits{Name: shop, Limits: p99Errors(200*time.Millisecond, "1%", 1000)}

	for state, reason := range map[threshold.State]threshold.Reason{
		threshold.Incomplete: threshold.ReasonIncomplete,
		threshold.Invalid:    threshold.ReasonInvalid,
	} {
		results := threshold.Evaluate(runOf(m), state, []threshold.MethodLimits{limits})
		if len(results) != 2 {
			t.Fatalf("state %d: %d results, want one per limit", state, len(results))
		}
		for _, r := range results {
			verdictIs(t, r, threshold.VerdictUnknown, reason)
			if r.Method != shop || r.LimitPercent == "" && r.Key == threshold.Errors {
				t.Errorf("state %d: result %+v lost its method or limit", state, r)
			}
		}
	}
}

// Ground: contract — the run's verdict is the worst of its limits: a fail
// over an unknown over a pass, and no limits is a pass.
func TestOverall_FailOverUnknownOverPass(t *testing.T) {
	res := func(v ...threshold.Verdict) []threshold.Result {
		var out []threshold.Result
		for _, x := range v {
			out = append(out, threshold.Result{Verdict: x})
		}

		return out
	}
	for _, tc := range []struct {
		results []threshold.Result
		want    threshold.Verdict
	}{
		{res(threshold.VerdictPass, threshold.VerdictPass), threshold.VerdictPass},
		{res(threshold.VerdictPass, threshold.VerdictUnknown), threshold.VerdictUnknown},
		{res(threshold.VerdictUnknown, threshold.VerdictFail, threshold.VerdictPass), threshold.VerdictFail},
		{res(threshold.VerdictFail), threshold.VerdictFail},
		{nil, threshold.VerdictPass},
	} {
		if got := threshold.Overall(tc.results); got != tc.want {
			t.Errorf("Overall(%v) = %q, want %q", tc.results, got, tc.want)
		}
	}
}

// Ground: contract — the search's limit: the rate it held, at least. A search
// that broke, broke at the first step or held all steps is decided by what it
// held; one that ran out of generator is a pass only when it held enough,
// otherwise unknown run_limit; one that stopped or was invalid decides nothing.
func TestEvaluateSearch_DecidedByWhatItHeld(t *testing.T) {
	for _, tc := range []struct {
		name   string
		res    breakpoint.Result
		limit  int
		want   threshold.Verdict
		reason threshold.Reason
	}{
		{"broke above the limit", breakpoint.Result{Outcome: breakpoint.BrokeBetween, Held: 744, Broke: 930}, 744, threshold.VerdictPass, ""},
		{"broke just under it", breakpoint.Result{Outcome: breakpoint.BrokeBetween, Held: 744, Broke: 930}, 930, threshold.VerdictFail, ""},
		{"broke at the first step", breakpoint.Result{Outcome: breakpoint.BrokeAtFirst, Broke: 100}, 100, threshold.VerdictFail, ""},
		{"held all", breakpoint.Result{Outcome: breakpoint.HeldThroughout, Held: 1818}, 1818, threshold.VerdictPass, ""},
		{"held all, limit above", breakpoint.Result{Outcome: breakpoint.HeldThroughout, Held: 1454}, 1818, threshold.VerdictFail, ""},
		{"run limit, held enough", breakpoint.Result{Outcome: breakpoint.RunLimit, Held: 744, Broke: 930}, 744, threshold.VerdictPass, ""},
		{"run limit, held too little", breakpoint.Result{Outcome: breakpoint.RunLimit, Held: 744, Broke: 930}, 930, threshold.VerdictUnknown, threshold.ReasonRunLimit},
		{"stopped", breakpoint.Result{Outcome: breakpoint.Stopped, Held: 744}, 744, threshold.VerdictUnknown, threshold.ReasonIncomplete},
		{"invalid", breakpoint.Result{Outcome: breakpoint.Invalid, Held: 744}, 744, threshold.VerdictUnknown, threshold.ReasonInvalid},
	} {
		r := threshold.EvaluateSearch(tc.res, threshold.SearchLimit{HeldRPS: tc.limit})

		if r.Verdict != tc.want || r.Reason != tc.reason {
			t.Errorf("%s: %q (%q), want %q (%q)", tc.name, r.Verdict, r.Reason, tc.want, tc.reason)
		}
		if r.Key != threshold.HeldRPS || r.Method != "" || r.LimitRPS != tc.limit || r.HeldRPS != tc.res.Held {
			t.Errorf("%s: key %q method %q limit %d held %d, want held_rps, no method, %d and %d",
				tc.name, r.Key, r.Method, r.LimitRPS, r.HeldRPS, tc.limit, tc.res.Held)
		}
	}
}
