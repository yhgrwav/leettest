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
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/metrics"
)

// Ground: boundary — which rank is p50 and p99: end-to-end only the stop tests catch p99 read as
// p95, and by chance (mutation 2026-09-22).
func TestStatsReportsPercentiles(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)

	for i := 1; i <= 100; i++ {
		stats.Record(Result{
			Method:      "a",
			ScheduledAt: start,
			Outcome:     Outcome{DoneAt: start.Add(time.Duration(i) * time.Millisecond), Category: CategorySuccess},
		})
	}

	report := stats.Report()
	if len(report.Methods) != 1 {
		t.Fatalf("methods = %d, want 1", len(report.Methods))
	}

	// The histogram answers within its relative accuracy, so the check is a
	// bound rather than equality.
	for _, tt := range []struct {
		name string
		got  metrics.Quantile
		want time.Duration
	}{
		{"p50", report.Methods[0].P50, 50 * time.Millisecond},
		{"p95", report.Methods[0].P95, 95 * time.Millisecond},
		{"p99", report.Methods[0].P99, 99 * time.Millisecond},
		{"max", report.Methods[0].Max, 100 * time.Millisecond},
	} {
		if !tt.got.Defined || !tt.got.Exact {
			t.Errorf("%s = %+v, want an exact defined value", tt.name, tt.got)
			continue
		}
		if tt.got.Value < tt.want || tt.got.Value > tt.want+tt.want/1024+time.Microsecond {
			t.Errorf("%s = %s, want %s", tt.name, tt.got.Value, tt.want)
		}
	}
}

// Ground: boundary — zero observations.
func TestStatsLeavesPercentileUndefinedWithoutObservations(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)

	// The call never reached the target, so the method exists in the report but
	// has nothing to build a percentile from.
	stats.Record(Result{
		Method:      "a",
		ScheduledAt: start,
		Outcome:     Outcome{DoneAt: start.Add(time.Millisecond), Category: CategoryUnreachable},
	})

	report := stats.Report()

	if got := report.Methods[0].P99; got.Defined {
		t.Errorf("p99 = %+v, want undefined: nothing was measured", got)
	}
	if got := report.Methods[0].Sent; got != 1 {
		t.Errorf("sent = %d, want the failed request counted", got)
	}
}

// Ground: contract — one MethodReport per method.
func TestStatsSplitsMethods(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)

	stats.Record(Result{Method: "a", ScheduledAt: start, Outcome: Outcome{DoneAt: start.Add(10 * time.Millisecond), Category: CategorySuccess}})
	stats.Record(Result{Method: "a", ScheduledAt: start, Outcome: Outcome{DoneAt: start.Add(20 * time.Millisecond), Category: CategorySuccess}})
	stats.Record(Result{Method: "b", ScheduledAt: start, Outcome: Outcome{DoneAt: start.Add(30 * time.Millisecond), Category: CategoryServerFault, Err: ErrFakeFailure}})

	stats.Finish(start.Add(time.Second))

	report := stats.Report()

	if report.Sent != 3 || report.Failed != 1 {
		t.Fatalf("sent %d failed %d, want 3 and 1", report.Sent, report.Failed)
	}
	if len(report.Methods) != 2 {
		t.Fatalf("methods = %d, want 2", len(report.Methods))
	}
	if report.Methods[0].Method != "a" || report.Methods[0].Sent != 2 {
		t.Errorf("first method = %q with %d requests, want a with 2", report.Methods[0].Method, report.Methods[0].Sent)
	}
	if got := report.Methods[0].Max; !got.Defined || got.Value < 20*time.Millisecond {
		t.Errorf("max = %+v, want at least 20ms", got)
	}
}

// Ground: boundary — a category outside the enum, which no real sender returns.
func TestStatsCountsUnfilledCategoryAsFailure(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)

	stats.Record(Result{Method: "a", ScheduledAt: start, Outcome: Outcome{DoneAt: start.Add(time.Millisecond)}})

	report := stats.Report()

	if report.Sent != 1 || report.Failed != 1 {
		t.Fatalf("sent %d failed %d, want a sender that forgot Category to count as failed", report.Sent, report.Failed)
	}
}

// Ground: contract — a refused connection is not latency, until the stand can drop connections
// mid-run; end-to-end an unreachable target ends at connect.
func TestStatsKeepsUnreachableCallsOutOfLatency(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)

	for range 9 {
		stats.Record(Result{
			Method:      "a",
			ScheduledAt: start,
			Outcome:     Outcome{DoneAt: start.Add(100 * time.Millisecond), Category: CategorySuccess},
		})
	}
	// A refused connection comes back almost instantly; counted as a latency it
	// would drag the median down while the target is in fact unreachable.
	stats.Record(Result{
		Method:      "a",
		ScheduledAt: start,
		Outcome:     Outcome{DoneAt: start.Add(50 * time.Microsecond), Category: CategoryUnreachable},
	})

	report := stats.Report()
	method := report.Methods[0]

	if method.Unanswered != 1 {
		t.Errorf("unanswered = %d, want 1", method.Unanswered)
	}
	if method.Latencies != 9 {
		t.Errorf("latencies = %d, want 9: an unreachable call is not a measurement", method.Latencies)
	}
	if method.Failed != 1 {
		t.Errorf("failed = %d, want 1: it still failed", method.Failed)
	}
	if got := method.P50; !got.Defined || got.Value < 100*time.Millisecond {
		t.Errorf("p50 = %+v, want the median of the calls that answered", got)
	}
}

// Ground: boundary — the threshold is the deadline to the nanosecond, not a later moment.
func TestStatsCensorsAtTheDeadlineNotAtTheReport(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)

	// The sender noticed the timeout 40ms after the deadline; only the deadline
	// itself is a justified lower bound.
	stats.Record(Result{
		Method:      "a",
		ScheduledAt: start,
		Deadline:    start.Add(time.Second),
		Outcome:     Outcome{DoneAt: start.Add(1040 * time.Millisecond), Category: CategoryTimeout},
	})

	report := stats.Report()

	if got := report.Methods[0].P99; got.Value >= 1040*time.Millisecond {
		t.Errorf("bound = %s, want the deadline of 1s, not the moment the timeout was noticed", got.Value)
	}
	if got := report.Methods[0].P99; got.Value < time.Second {
		t.Errorf("bound = %s, want at least the deadline", got.Value)
	}
}

// Ground: contract — Options.Warmup, until the stand switches its delay by time instead of call
// number.
func TestStatsExcludesWarmupFromCountsAndRate(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, time.Second)

	stats.Record(Result{
		Method:      "a",
		ScheduledAt: start,
		Outcome:     Outcome{DoneAt: start.Add(time.Millisecond), Category: CategoryServerFault},
	})
	stats.Record(Result{
		Method:      "a",
		ScheduledAt: start.Add(1500 * time.Millisecond),
		Outcome:     Outcome{DoneAt: start.Add(1510 * time.Millisecond), Category: CategorySuccess},
	})

	stats.Finish(start.Add(3 * time.Second))

	report := stats.Report()

	if report.Sent != 1 || report.Failed != 0 {
		t.Fatalf("sent %d failed %d, want the warmup request left out of both", report.Sent, report.Failed)
	}
	// Two measured seconds of the three, one request: the rate divides by the
	// measured window, not by the whole run.
	if got := report.Methods[0].RPS; got < 0.49 || got > 0.51 {
		t.Errorf("rps = %.3f, want ~0.5: one request over the two measured seconds", got)
	}
}

func recordSent(stats *Stats, start time.Time, from, to time.Duration, n int) {
	step := (to - from) / time.Duration(n)
	for i := range n {
		at := start.Add(from + time.Duration(i)*step)
		stats.Record(Result{Method: "a", ScheduledAt: at, Outcome: Outcome{Category: CategorySuccess, SentAt: at, DoneAt: at}})
	}
}

// Ground: boundary — the rate window with warmup and a drain at once: the
// warmup is out of both the count and the window, the drain is out of the
// window. An end-to-end run cannot pin both edges without conducting time.
func TestStatsRatesOverTheSendingWindowWithoutWarmupOrDrain(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, time.Second)

	recordSent(stats, start, 0, time.Second, 50)              // warmup
	recordSent(stats, start, time.Second, 3*time.Second, 200) // measured
	stats.EndSending(start.Add(3 * time.Second))
	stats.Finish(start.Add(5 * time.Second)) // two seconds of drain

	if got := stats.Report().Methods[0].RPS; got != 100 {
		t.Errorf("rps = %v, want 200 calls over the 2s between warmup and the end of sending", got)
	}
}

// Ground: boundary — a stop that comes earlier than the planned end wins,
// whichever is reported first.
func TestStatsSendingEndsAtTheEarliestReportedMoment(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)

	recordSent(stats, start, 0, time.Second, 100)
	stats.EndSending(start.Add(3 * time.Second))
	stats.EndSending(start.Add(time.Second))
	stats.EndSending(start.Add(2 * time.Second))
	stats.Finish(start.Add(4 * time.Second))

	if got := stats.Report().Methods[0].RPS; got != 100 {
		t.Errorf("rps = %v, want 100 calls over the 1s until the stop", got)
	}
}

// Ground: boundary — sending ended inside the warmup: nothing was measured,
// and the rate is zero, not a division by zero.
func TestStatsRateIsZeroWhenSendingEndsInsideTheWarmup(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 2*time.Second)

	recordSent(stats, start, 0, time.Second, 50)
	stats.EndSending(start.Add(time.Second))
	stats.Finish(start.Add(2 * time.Second))

	report := stats.Report()
	if got := report.Methods[0].RPS; got != 0 {
		t.Errorf("rps = %v, want 0", got)
	}
}

// Ground: boundary — the live view during a long plan: the window is the time
// the run has been sending so far, not the hour it was asked for. Dividing by
// the plan would show 0.03 rps a second into a 3600s run.
func TestStatsLiveRateDividesByTheTimeSoFarNotThePlan(t *testing.T) {
	stats := NewStats()
	start := time.Now().Add(-time.Second)
	stats.Start(start, 0)
	stats.EndSending(start.Add(time.Hour))

	recordSent(stats, start, 0, time.Second, 100)

	var snapshot Snapshot
	stats.SnapshotInto(&snapshot, NewLiveBuffer(), false)

	if got := snapshot.RPS; got < 50 || got > 200 {
		t.Errorf("live rps = %.2f after 100 calls in a second, want about 100", got)
	}
}

// timeoutAfter is a call with a 1 s deadline that ended as a timeout took
// after its schedule, the status from the target or set by the client.
func timeoutAfter(start time.Time, took time.Duration, fromTarget bool) Result {
	return Result{
		Method:      "a",
		ScheduledAt: start,
		BegunAt:     start,
		Deadline:    start.Add(time.Second),
		Outcome: Outcome{
			SentAt: start, DoneAt: start.Add(took),
			Category: CategoryTimeout, Code: "DeadlineExceeded", CodeFromTarget: fromTarget,
		},
	}
}

// auditRun is the audit's run (F1): 97 successes at 10 ms and 3 timeouts that
// ended took after their schedule against a 1 s budget.
func auditRun(took time.Duration, fromTarget bool) MethodReport {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)

	for range 97 {
		stats.Record(Result{
			Method:      "a",
			ScheduledAt: start,
			BegunAt:     start,
			Outcome:     Outcome{SentAt: start, DoneAt: start.Add(10 * time.Millisecond), Category: CategorySuccess},
		})
	}
	for range 3 {
		stats.Record(timeoutAfter(start, took, fromTarget))
	}

	return stats.Report().Methods[0]
}

// Ground: contract — a DEADLINE_EXCEEDED the target itself sent well before our deadline is an
// answer with a latency we saw, not a call nobody listened to; censoring it at the whole deadline
// printed "p99 >= 1s" for 3 calls that answered in 50 ms (audit F1).
func TestRecord_AnEarlyDeadlineAnswerKeepsItsLatency(t *testing.T) {
	m := auditRun(50*time.Millisecond, true)

	if m.Censored != 0 || m.Latencies != 100 {
		t.Errorf("censored %d, latencies %d; want 0 and 100: the answers were seen", m.Censored, m.Latencies)
	}
	if !m.P99.Defined || !m.P99.Exact || m.P99.Floor() > 50*time.Millisecond || !nearly(m.P99.Value, 50*time.Millisecond) {
		t.Errorf("p99 = %+v, want exact at 50ms, not a bound at the 1s deadline", m.P99)
	}
	if !m.Max.Exact || !nearly(m.Max.Value, 50*time.Millisecond) {
		t.Errorf("max = %+v, want exact at 50ms", m.Max)
	}
	if !m.P99WithoutClientWaits.Exact || !nearly(m.P99WithoutClientWaits.Value, 50*time.Millisecond) {
		t.Errorf("p99 without waits = %+v, want exact at 50ms: no client waits to take out", m.P99WithoutClientWaits)
	}
	if m.Failed != 3 || m.Sent != 100 {
		t.Errorf("failed %d of %d sent, want 3 of 100: the answer is still a failure", m.Failed, m.Sent)
	}
}

// Ground: contract — the early answer is an ordinary observation everywhere a call is: the
// window it was scheduled in, the connection, the served time (latency without the client's waits)
// and the waited-calls histogram of the cause that held it back.
func TestRecord_AnEarlyDeadlineAnswerIsExactInEveryHistogram(t *testing.T) {
	start := time.Now()
	stats := windowed(start, 0, 120*time.Second)

	r := timeoutAfter(start, 80*time.Millisecond, true)
	r.StreamWait = 60 * time.Millisecond
	stats.Record(r)

	method := stats.byMethod["a"]

	if got := method.latency.Snapshot(); got.Count() != 1 || got.CensoredCount() != 0 {
		t.Errorf("method latency: %d observations, %d censored; want 1 and 0", got.Count(), got.CensoredCount())
	}
	if got := method.served.Snapshot(); got.Count() != 1 || got.CensoredCount() != 0 || !nearly(got.Percentile(1).Value, 20*time.Millisecond) {
		t.Errorf("served: %d observations, %d censored, max %s; want 1, 0 and 20ms (80ms less the 60ms stream wait)",
			got.Count(), got.CensoredCount(), got.Percentile(1).Value)
	}
	if w := method.waited[2]; w == nil {
		t.Errorf("no waited-for-a-stream histogram: the call waited 60ms for a stream")
	} else if got := w.Snapshot(); got.Count() != 1 || got.CensoredCount() != 0 || !nearly(got.Percentile(1).Value, 80*time.Millisecond) {
		t.Errorf("waited for a stream: %d observations, %d censored, max %s; want 1, 0 and 80ms",
			got.Count(), got.CensoredCount(), got.Percentile(1).Value)
	}

	win := windowsOf(t, stats)[0]
	if win.Latency.Count() != 1 || win.Latency.CensoredCount() != 0 || !win.P99.Exact || !nearly(win.P99.Value, 80*time.Millisecond) {
		t.Errorf("window 0: %d observations, %d censored, p99 %+v; want 1, 0 and exact 80ms",
			win.Latency.Count(), win.Latency.CensoredCount(), win.P99)
	}
}

// Ground: contract — same rule on a connection's own p99.
func TestRecord_AnEarlyDeadlineAnswerIsExactOnItsConnection(t *testing.T) {
	eng, start := linkedEngine(t, linkedSender{addrs: addrs3()[:2]})

	for range 100 {
		eng.stats.Record(onLink(0, start, Outcome{
			Category: CategoryTimeout, Code: "DeadlineExceeded", CodeFromTarget: true,
			DoneAt: start.Add(2*time.Second + 50*time.Millisecond),
		}))
	}

	got := eachOf(t, finish(eng, start))[0].P99
	if !got.Defined || !got.Exact || !nearly(got.Value, 50*time.Millisecond) {
		t.Errorf("connection p99 = %+v, want exact at 50ms", got)
	}
}

// Ground: boundary — guard. 95% of the budget is the target's copy of our own deadline, which
// shows nothing of the target: it stays a lower bound at the deadline.
func TestRecord_ADeadlineCopyStaysCensored(t *testing.T) {
	m := auditRun(950*time.Millisecond, true)

	if m.Censored != 3 {
		t.Errorf("censored = %d, want 3: a copy of our deadline is not an observation", m.Censored)
	}
	if !m.P99.Defined || m.P99.Exact || m.P99.Floor() < time.Second {
		t.Errorf("p99 = %+v, want a lower bound of at least the 1s deadline", m.P99)
	}
}

// Ground: contract — MethodReport.AnsweredEarly is the timeouts the target answered itself
// before 90% of the budget (the report's note leaves them out of «got no answer»); TimedOut keeps
// counting every timeout, so the category count is unchanged. A copy of our deadline and a
// timeout with no status are not early answers.
func TestRecord_AnsweredEarlyCountsOnlyTheEarlyAnswers(t *testing.T) {
	for _, tc := range []struct {
		name       string
		took       time.Duration
		fromTarget bool
		want       int
	}{
		{"50 ms from the target", 50 * time.Millisecond, true, 3},
		{"95% from the target", 950 * time.Millisecond, true, 0},
		{"50 ms, no status", 50 * time.Millisecond, false, 0},
	} {
		m := auditRun(tc.took, tc.fromTarget)

		if m.AnsweredEarly != tc.want {
			t.Errorf("%s: AnsweredEarly = %d, want %d", tc.name, m.AnsweredEarly, tc.want)
		}
		if m.TimedOut != 3 {
			t.Errorf("%s: TimedOut = %d, want 3: the category count does not move", tc.name, m.TimedOut)
		}
	}
}

// Ground: boundary — guard. With no status from the target the timeout is ours, however early it
// was noticed.
func TestRecord_ATimeoutWithNoStatusStaysCensored(t *testing.T) {
	m := auditRun(50*time.Millisecond, false)

	if m.Censored != 3 {
		t.Errorf("censored = %d, want 3: no status came back from the target", m.Censored)
	}
	if !m.P99.Defined || m.P99.Exact || m.P99.Floor() < time.Second {
		t.Errorf("p99 = %+v, want a lower bound of at least the 1s deadline", m.P99)
	}
}

// Ground: boundary — guard. An aborted call was watched until the abort, and is a lower bound of
// that moment, early or not.
func TestRecord_AnAbortedCallStaysAtItsAbort(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)

	r := timeoutAfter(start, 50*time.Millisecond, true)
	r.Category = CategoryAborted
	stats.Record(r)

	m := stats.Report().Methods[0]
	if m.Censored != 1 {
		t.Errorf("censored = %d, want 1", m.Censored)
	}
	if !m.Max.Defined || m.Max.Exact || m.Max.Floor() < 49*time.Millisecond || m.Max.Floor() > 51*time.Millisecond {
		t.Errorf("max = %+v, want a lower bound at the abort, 50ms: min(budget, latency)", m.Max)
	}
}

// Ground: boundary — one rule with the sender's expiredCopy (pkg/grpcsender): 90% of the budget
// the call went out with, counted from SentAt, is the first moment of a copy; the number is
// written here on purpose. Same boundary values as its table; a timeout with no deadline, no
// SentAt, no status from the target or another category is never an early answer.
func TestAnsweredEarly_IsTheSendersRuleInverted(t *testing.T) {
	sent := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	const budget = time.Second

	for _, tc := range []struct {
		name string
		edit func(*Result)
		want bool
	}{
		{"one nanosecond before 90%", func(r *Result) { r.DoneAt = sent.Add(900*time.Millisecond - 1) }, true},
		{"exactly 90%", func(r *Result) { r.DoneAt = sent.Add(900 * time.Millisecond) }, false},
		{"one nanosecond after 90%", func(r *Result) { r.DoneAt = sent.Add(900*time.Millisecond + 1) }, false},
		{"95%", func(r *Result) { r.DoneAt = sent.Add(950 * time.Millisecond) }, false},
		{"at the deadline", func(r *Result) { r.DoneAt = sent.Add(budget) }, false},
		{"half of the budget", func(r *Result) { r.DoneAt = sent.Add(500 * time.Millisecond) }, true},
		{"no deadline", func(r *Result) { r.Deadline = time.Time{} }, false},
		{"no SentAt", func(r *Result) { r.SentAt = time.Time{} }, false},
		{"no status from the target", func(r *Result) { r.CodeFromTarget = false }, false},
		{"an abort", func(r *Result) { r.Category = CategoryAborted }, false},
		{"a success", func(r *Result) { r.Category = CategorySuccess }, false},
	} {
		r := Result{
			ScheduledAt: sent,
			Deadline:    sent.Add(budget),
			Outcome: Outcome{
				SentAt: sent, DoneAt: sent.Add(50 * time.Millisecond),
				Category: CategoryTimeout, CodeFromTarget: true,
			},
		}
		tc.edit(&r)

		if got := r.answeredEarly(); got != tc.want {
			t.Errorf("%s: answeredEarly = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Ground: boundary — the budget is counted from SentAt, as the sender counts it, not from the
// schedule: a call that waited 600 ms to go out has 400 ms of budget left, and its answer at
// 950 ms after the schedule is 87.5% of that budget, an early one.
func TestAnsweredEarly_CountsTheBudgetFromSentAt(t *testing.T) {
	sent := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	r := Result{
		ScheduledAt: sent,
		Deadline:    sent.Add(time.Second),
		Outcome: Outcome{
			SentAt: sent.Add(600 * time.Millisecond), DoneAt: sent.Add(950 * time.Millisecond),
			Category: CategoryTimeout, CodeFromTarget: true,
		},
	}

	if !r.answeredEarly() {
		t.Errorf("answeredEarly = false at 350 ms of a 400 ms budget (87.5%%), want true")
	}

	r.DoneAt = sent.Add(960 * time.Millisecond)
	if r.answeredEarly() {
		t.Errorf("answeredEarly = true at 360 ms of a 400 ms budget (90%%), want false")
	}
}
