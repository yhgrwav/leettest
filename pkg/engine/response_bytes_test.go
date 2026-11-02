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
	"math"
	"testing"
	"time"
)

// recordAfter records one call of method m scheduled off after the start, done
// latency later from its schedule. A SentAt the outcome carries stays.
func recordAfter(stats *Stats, start time.Time, m string, off, latency time.Duration, o Outcome) {
	sched := start.Add(off)
	if o.SentAt.IsZero() {
		o.SentAt = sched
	}
	o.DoneAt = sched.Add(latency)
	stats.Record(Result{Method: m, ScheduledAt: sched, BegunAt: sched, Deadline: sched.Add(time.Second), Outcome: o})
}

// nearly compares durations through the histogram's 3 significant digits.
func nearly(got, want time.Duration) bool {
	return math.Abs(float64(got-want)) <= float64(want)/100
}

// windowRun records one call of method "a" scheduled at each offset in at,
// each done a millisecond later, in a run with a warm-up, sending that ended
// at sendingEnd and the last answer at finish (both from the start).
func windowRun(calls []Outcome, at []time.Duration, warmup, sendingEnd, finish time.Duration) *Stats {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, warmup)

	for i := range calls {
		recordAfter(stats, start, "a", at[i], time.Millisecond, calls[i])
	}
	stats.EndSending(start.Add(sendingEnd))
	stats.Finish(start.Add(finish))

	return stats
}

// Ground: contract — MethodReport.ResponseBytes is the mean over the measured
// successful calls: a success always carried a body, the rest did not count
// even when a body came before the error. Out of it: warm-up (#88).
func TestReport_ResponseBytesAreTheMeanOfMeasuredSuccesses(t *testing.T) {
	ms := time.Millisecond
	stats := warmupRun(t, []Outcome{
		{Category: CategorySuccess, ResponseBytes: 90000},                      // warm-up
		{Category: CategorySuccess, ResponseBytes: 1000},                       // measured
		{Category: CategoryOverload, Code: "Unavailable", ResponseBytes: 3000}, // measured, a body and then an error
		{Category: CategorySuccess, ResponseBytes: 3000},                       // measured
		{Category: CategoryTimeout, ResponseBytes: 5000},                       // measured, not a success either
		{Category: CategorySuccess, ResponseBytes: 2000},                       // measured
	}, []time.Duration{100 * ms, 1100 * ms, 1150 * ms, 1200 * ms, 1250 * ms, 1300 * ms})

	got := stats.Report().Methods[0].ResponseBytes
	if got == nil {
		t.Fatal("ResponseBytes is nil, want the mean of the three measured successes")
	}
	if *got != 2000 {
		t.Errorf("ResponseBytes = %v, want 2000: (1000+3000+2000)/3, the failed calls' bodies and the warm-up out", *got)
	}
}

// Ground: contract — an empty reply is a body of 0 bytes, not a missing one:
// every success carried one (grpc-go turns a success with no message into
// Internal), so a method whose successes are all empty answers 0, and 0 is in
// the mean of the others.
func TestReport_AnEmptyReplyCountsAsZeroBytes(t *testing.T) {
	ms := time.Millisecond

	allEmpty := warmupRun(t, []Outcome{
		{Category: CategorySuccess}, // warm-up
		{Category: CategorySuccess},
		{Category: CategorySuccess},
	}, []time.Duration{100 * ms, 1100 * ms, 1200 * ms})
	got := allEmpty.Report().Methods[0].ResponseBytes
	if got == nil {
		t.Fatal("ResponseBytes is nil, want 0: the successes were empty replies")
	}
	if *got != 0 {
		t.Errorf("ResponseBytes = %v, want 0", *got)
	}

	mixed := warmupRun(t, []Outcome{
		{Category: CategorySuccess}, // warm-up
		{Category: CategorySuccess},
		{Category: CategorySuccess, ResponseBytes: 3000},
	}, []time.Duration{100 * ms, 1100 * ms, 1200 * ms})
	got = mixed.Report().Methods[0].ResponseBytes
	if got == nil || *got != 1500 {
		t.Errorf("ResponseBytes = %v, want 1500: (0+3000)/2, the empty reply is in the mean", got)
	}
}

// Ground: contract — a method none of whose measured calls succeeded has no
// size: nil, not a zero that reads as an empty reply. A warm-up success does
// not give the measured part one, and neither does a body that came with an
// error.
func TestReport_ResponseBytesAreNilWhenNoMeasuredCallSucceeded(t *testing.T) {
	ms := time.Millisecond
	stats := warmupRun(t, []Outcome{
		{Category: CategorySuccess, ResponseBytes: 5000}, // warm-up
		{Category: CategoryUnreachable, Code: "Unavailable"},
		{Category: CategoryOverload, Code: "Unavailable", ResponseBytes: 3000},
	}, []time.Duration{100 * ms, 1100 * ms, 1200 * ms})

	if got := stats.Report().Methods[0].ResponseBytes; got != nil {
		t.Errorf("ResponseBytes = %v, want nil", *got)
	}
}

// Ground: contract — MethodReport.Warmup is the warm-up in its own histogram:
// the successes (and the timed-out as censored ones), their latency from
// scheduledAt, the mean reply body of the successes with an empty reply
// counted as 0. The measured histogram is not touched: its count and
// percentiles are those of the measured calls alone (#88). The calls that were
// refused, never reached the target or were cut off are in neither.
func TestReport_WarmupHasItsOwnHistogramAndTheMeasuredOneIsUntouched(t *testing.T) {
	ms := time.Millisecond
	stats := NewStats()
	start := time.Now()
	stats.Start(start, time.Second)

	// Warm-up: three successes, and five calls that are not observations.
	recordAfter(stats, start, "a", 100*ms, 10*ms, Outcome{Category: CategorySuccess, ResponseBytes: 500})
	recordAfter(stats, start, "a", 200*ms, 20*ms, Outcome{Category: CategorySuccess, ResponseBytes: 700})
	recordAfter(stats, start, "a", 300*ms, 30*ms, Outcome{Category: CategorySuccess}) // an empty reply: 0
	recordAfter(stats, start, "a", 400*ms, time.Microsecond, Outcome{Category: CategoryUnreachable, Code: "Unavailable"})
	recordAfter(stats, start, "a", 450*ms, time.Microsecond, Outcome{Category: CategoryCutOff, Code: "Internal"})
	recordAfter(stats, start, "a", 500*ms, time.Microsecond, Outcome{Category: CategoryClientError})
	recordAfter(stats, start, "a", 550*ms, time.Microsecond, Outcome{Category: CategoryOverload, Code: "Unavailable", ResponseBytes: 9000})
	recordAfter(stats, start, "a", 600*ms, time.Microsecond, Outcome{Category: CategoryBadResponse})
	// Measured: one call, far from the warm-up's latencies.
	recordAfter(stats, start, "a", 1500*ms, 400*ms, Outcome{Category: CategorySuccess, ResponseBytes: 2000})

	end := start.Add(3 * time.Second)
	stats.EndSending(end)
	stats.Finish(end)

	m := stats.Report().Methods[0]

	if m.Latencies != 1 {
		t.Errorf("measured Latencies = %d, want 1: the warm-up is not in it", m.Latencies)
	}
	if !m.P50.Defined || !nearly(m.P50.Value, 400*ms) {
		t.Errorf("measured p50 = %+v, want 400ms: the warm-up's 10-30ms is not in it", m.P50)
	}
	if m.ResponseBytes == nil || *m.ResponseBytes != 2000 {
		t.Errorf("measured ResponseBytes = %v, want 2000: the warm-up's replies are not in it", m.ResponseBytes)
	}

	w := m.Warmup
	if w == nil {
		t.Fatal("Warmup is nil, want the three warm-up successes")
	}
	if w.Calls != 3 {
		t.Errorf("Warmup.Calls = %d, want 3: unreachable, cut off, client error, overload and bad response are not observations of the measured latency", w.Calls)
	}
	if !w.P50.Defined || !w.P50.Exact || !nearly(w.P50.Value, 20*ms) {
		t.Errorf("Warmup.P50 = %+v, want an exact 20ms", w.P50)
	}
	if w.P99.Defined {
		t.Errorf("Warmup.P99 = %+v, want undefined: 3 observations are fewer than %d", w.P99, 100)
	}
	if w.ResponseBytes == nil || *w.ResponseBytes != 400 {
		t.Errorf("Warmup.ResponseBytes = %v, want 400: (500+700+0)/3, the empty reply is in the mean, the overload's body is not", w.ResponseBytes)
	}
}

// Ground: contract — the warm-up latency is taken from scheduledAt, like the
// measured one: the time a call waited for the generator is part of it.
func TestReport_WarmupLatencyIsFromTheSchedule(t *testing.T) {
	ms := time.Millisecond
	stats := NewStats()
	start := time.Now()
	stats.Start(start, time.Second)

	// Scheduled at 100ms, sent 50ms later, answered 10ms after that.
	sent := start.Add(150 * ms)
	recordAfter(stats, start, "a", 100*ms, 60*ms, Outcome{Category: CategorySuccess, SentAt: sent})

	end := start.Add(3 * time.Second)
	stats.EndSending(end)
	stats.Finish(end)

	w := stats.Report().Methods[0].Warmup
	if w == nil {
		t.Fatal("Warmup is nil, want the one answered warm-up call")
	}
	if !w.P50.Defined || !nearly(w.P50.Value, 60*ms) {
		t.Errorf("Warmup.P50 = %+v, want 60ms: from the schedule, not the 10ms since the send", w.P50)
	}
}

// Ground: contract — with enough warm-up observations the p99 is defined.
func TestReport_WarmupP99IsDefinedFromMinCount(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, time.Minute)

	for i := range 100 {
		recordAfter(stats, start, "a", time.Duration(i)*time.Millisecond, time.Duration(i+1)*time.Millisecond,
			Outcome{Category: CategorySuccess, ResponseBytes: 100})
	}
	end := start.Add(time.Minute + time.Second)
	stats.EndSending(end)
	stats.Finish(end)

	w := stats.Report().Methods[0].Warmup
	if w == nil {
		t.Fatal("Warmup is nil, want 100 warm-up calls")
	}
	if w.Calls != 100 {
		t.Errorf("Warmup.Calls = %d, want 100", w.Calls)
	}
	if !w.P99.Defined || !w.P99.Exact || !nearly(w.P99.Value, 99*time.Millisecond) {
		t.Errorf("Warmup.P99 = %+v, want an exact 99ms from 100 observations of 1..100ms", w.P99)
	}
}

// Ground: contract — a call that timed out in the warm-up is a censored
// observation, as in the measured part: it is known to have lasted at least
// its deadline. With two of 100 timed out the p99 is a lower bound, p50 is
// still exact, and the timed-out calls count as observations.
func TestReport_WarmupTimedOutCallsAreCensoredObservations(t *testing.T) {
	ms := time.Millisecond
	stats := NewStats()
	start := time.Now()
	stats.Start(start, time.Minute)

	for i := range 98 {
		recordAfter(stats, start, "a", time.Duration(i)*ms, 10*ms, Outcome{Category: CategorySuccess, ResponseBytes: 100})
	}
	for i := range 2 {
		recordAfter(stats, start, "a", time.Duration(100+i)*ms, time.Second, Outcome{Category: CategoryTimeout})
	}
	end := start.Add(time.Minute + time.Second)
	stats.EndSending(end)
	stats.Finish(end)

	w := stats.Report().Methods[0].Warmup
	if w == nil {
		t.Fatal("Warmup is nil, want 100 observations")
	}
	if w.Calls != 100 {
		t.Errorf("Warmup.Calls = %d, want 100: the two timed-out calls are observations", w.Calls)
	}
	if !w.P50.Defined || !w.P50.Exact || !nearly(w.P50.Value, 10*ms) {
		t.Errorf("Warmup.P50 = %+v, want an exact 10ms", w.P50)
	}
	if !w.P99.Defined || w.P99.Exact || w.P99.Value < 990*ms {
		t.Errorf("Warmup.P99 = %+v, want a lower bound of at least the 1s deadline: the p99 landed among censored observations", w.P99)
	}
	if w.ResponseBytes == nil || *w.ResponseBytes != 100 {
		t.Errorf("Warmup.ResponseBytes = %v, want 100: the timed-out calls have no reply", w.ResponseBytes)
	}
}

// Ground: contract — a warm-up in which every call timed out is the warm-up
// report, not a missing one: it is the main signal of an unready target.
// Calls is the observations, p50 is a lower bound, there is no reply size.
func TestReport_WarmupOfOnlyTimedOutCallsIsStillReported(t *testing.T) {
	ms := time.Millisecond
	stats := warmupRun(t, []Outcome{
		{Category: CategoryTimeout},
		{Category: CategoryTimeout},
		{Category: CategorySuccess, ResponseBytes: 10}, // measured
	}, []time.Duration{100 * ms, 200 * ms, 1500 * ms})

	w := stats.Report().Methods[0].Warmup
	if w == nil {
		t.Fatal("Warmup is nil, want the two timed-out calls as observations")
	}
	if w.Calls != 2 {
		t.Errorf("Warmup.Calls = %d, want 2", w.Calls)
	}
	if !w.P50.Defined || w.P50.Exact {
		t.Errorf("Warmup.P50 = %+v, want a lower bound: every observation is censored", w.P50)
	}
	if w.P99.Defined {
		t.Errorf("Warmup.P99 = %+v, want undefined: 2 observations", w.P99)
	}
	if w.ResponseBytes != nil {
		t.Errorf("Warmup.ResponseBytes = %v, want nil: no warm-up call succeeded", *w.ResponseBytes)
	}
}

// Ground: contract — a run without a warm-up, and a warm-up none of whose
// calls left an observation, have no warm-up report: nil, not a zero.
func TestReport_WarmupIsNilWithoutObservations(t *testing.T) {
	ms := time.Millisecond

	noWarmup := NewStats()
	start := time.Now()
	noWarmup.Start(start, 0)
	recordAfter(noWarmup, start, "a", 100*ms, 5*ms, Outcome{Category: CategorySuccess, ResponseBytes: 10})
	noWarmup.EndSending(start.Add(time.Second))
	noWarmup.Finish(start.Add(time.Second))
	if w := noWarmup.Report().Methods[0].Warmup; w != nil {
		t.Errorf("Warmup without a warm-up = %+v, want nil", w)
	}

	silent := warmupRun(t, []Outcome{
		{Category: CategoryUnreachable, Code: "Unavailable"},
		{Category: CategoryCutOff, Code: "Internal"},
		{Category: CategoryOverload, Code: "Unavailable"},
		{Category: CategoryBadResponse},
		{Category: CategorySuccess, ResponseBytes: 10}, // measured
	}, []time.Duration{100 * ms, 200 * ms, 300 * ms, 400 * ms, 1500 * ms})
	if w := silent.Report().Methods[0].Warmup; w != nil {
		t.Errorf("Warmup with no observation = %+v, want nil", w)
	}
}

// Ground: contract — Report.Traffic is the bits per second the measured calls'
// bodies came to over the window the rates divide by (end of warm-up to end of
// sending): sent is the wire size of every request that went out, whatever
// came back; received is the compressed size of the replies of successes.
// Warm-up is out; so is a body that came with an error.
func TestReport_TrafficIsBodiesOverTheMeasuredWindow(t *testing.T) {
	ms := time.Millisecond
	// Window: warm-up 1s, sending ends at 3s, so 2s.
	stats := warmupRun(t, []Outcome{
		{Category: CategorySuccess, RawBytes: 9_000_000, WireBytes: 9_000_000, ResponseBytes: 9_000_000, ResponseWireBytes: 9_000_000}, // warm-up
		{Category: CategorySuccess, RawBytes: 1000, WireBytes: 400, ResponseBytes: 2000, ResponseWireBytes: 300},
		{Category: CategorySuccess, RawBytes: 3000, WireBytes: 600, ResponseBytes: 4000, ResponseWireBytes: 500},
		{Category: CategoryOverload, Code: "Unavailable", RawBytes: 1500, WireBytes: 500, ResponseBytes: 3000, ResponseWireBytes: 3000},
		{Category: CategoryUnreachable, Code: "Unavailable"},
	}, []time.Duration{100 * ms, 1100 * ms, 1200 * ms, 1250 * ms, 1300 * ms})

	tr := stats.Report().Traffic
	if tr == nil {
		t.Fatal("Traffic is nil, want the run's bits per second")
	}
	// Wire sizes 400+600+500 = 1500 B = 12000 bit over 2s; compressed replies of
	// the successes 300+500 = 800 B = 6400 bit over 2s.
	if math.Abs(tr.SentBitsPerSecond-6000) > 1e-6 {
		t.Errorf("SentBitsPerSecond = %v, want 6000: (400+600+500) bytes x 8 / 2s, the compressed size, the overload's request in, warm-up out", tr.SentBitsPerSecond)
	}
	if math.Abs(tr.ReceivedBitsPerSecond-3200) > 1e-6 {
		t.Errorf("ReceivedBitsPerSecond = %v, want 3200: (300+500) bytes x 8 / 2s, the size before decompression, the overload's body and the warm-up out", tr.ReceivedBitsPerSecond)
	}
}

// Ground: contract — the window is the one the rates divide by: it ends where
// sending ends, not where the last answer came. The drain after sending adds
// no traffic to divide into.
func TestReport_TrafficWindowEndsWithSending(t *testing.T) {
	s := time.Second
	calls := []Outcome{
		{Category: CategorySuccess, WireBytes: 400, ResponseBytes: 2000, ResponseWireBytes: 300},
		{Category: CategorySuccess, WireBytes: 600, ResponseBytes: 4000, ResponseWireBytes: 500},
		{Category: CategoryOverload, Code: "Unavailable", WireBytes: 500},
	}
	at := []time.Duration{1100 * time.Millisecond, 1200 * time.Millisecond, 1250 * time.Millisecond}

	// Sending ends at 3s, the last answer at 5s: still 2s of window.
	tr := windowRun(calls, at, s, 3*s, 5*s).Report().Traffic
	if tr == nil {
		t.Fatal("Traffic is nil, want the run's bits per second")
	}
	if math.Abs(tr.SentBitsPerSecond-6000) > 1e-6 || math.Abs(tr.ReceivedBitsPerSecond-3200) > 1e-6 {
		t.Errorf("Traffic = %+v, want 6000 and 3200: 2s of window, not the 4s since the warm-up ended", *tr)
	}
}

// Ground: contract — a window with no body at all is a measured window that
// moved nothing: zeros, not nil. Nil is for a run that has no window.
func TestReport_TrafficIsZeroInAWindowWithoutBodies(t *testing.T) {
	ms := time.Millisecond
	stats := warmupRun(t, []Outcome{
		{Category: CategoryUnreachable, Code: "Unavailable"},
		{Category: CategoryUnreachable, Code: "Unavailable"},
	}, []time.Duration{1100 * ms, 1200 * ms})

	tr := stats.Report().Traffic
	if tr == nil {
		t.Fatal("Traffic is nil, want zeros: the window is 2s and nothing went out")
	}
	if tr.SentBitsPerSecond != 0 || tr.ReceivedBitsPerSecond != 0 {
		t.Errorf("Traffic = %+v, want zeros", *tr)
	}
}

// Ground: contract — a run that never left the warm-up has no window to divide
// by: no traffic, not a division by zero.
func TestReport_TrafficIsNilWithoutAMeasuredWindow(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, time.Hour)
	recordAfter(stats, start, "a", time.Millisecond, time.Millisecond,
		Outcome{Category: CategorySuccess, RawBytes: 100, WireBytes: 100, ResponseBytes: 100, ResponseWireBytes: 100})
	stats.EndSending(start.Add(time.Minute))
	stats.Finish(start.Add(time.Minute))

	if tr := stats.Report().Traffic; tr != nil {
		t.Errorf("Traffic = %+v, want nil: the run ended inside the warm-up", tr)
	}
}

// sinkMethod keeps newMethod's result from being optimised away.
var sinkMethod *methodStats

// Ground: hot path — a warm-up call is recorded under the lock every worker
// shares; after the first it costs no allocation, and the measured success that
// carries the new sizes costs what one without them did.
func TestRecord_TheNewSizesCostNoAllocation(t *testing.T) {
	const runs = 200

	start := time.Now()
	record := func(warmup time.Duration, off time.Duration, o Outcome) float64 {
		stats := NewStats()
		stats.Reserve(time.Hour, "a")
		stats.Start(start, warmup)
		sched := start.Add(off)
		o.SentAt, o.DoneAt = sched, sched.Add(12*time.Millisecond)
		r := Result{Method: "a", ScheduledAt: sched, BegunAt: sched, Deadline: sched.Add(time.Second), Outcome: o}

		return testing.AllocsPerRun(runs, func() { stats.Record(r) })
	}

	if got := record(time.Minute, time.Second, Outcome{Category: CategorySuccess, ResponseBytes: 2000, ResponseWireBytes: 300}); got != 0 {
		t.Errorf("%v allocations per warm-up success, want 0 after the first", got)
	}
	if got := record(time.Minute, time.Second, Outcome{Category: CategoryTimeout}); got != 0 {
		t.Errorf("%v allocations per warm-up timeout, want 0 after the first", got)
	}

	plain := record(0, time.Second, Outcome{Category: CategorySuccess, WireBytes: 400})
	sized := record(0, time.Second, Outcome{Category: CategorySuccess, WireBytes: 400, ResponseBytes: 2000, ResponseWireBytes: 300})
	if sized != plain {
		t.Errorf("%v allocations per measured success with sizes, %v without: the sizes must cost none", sized, plain)
	}
}

// Ground: hot path — the warm-up histogram is allocated only for a run that has
// a warm-up: a method of a run without one pays nothing for it, "off is absent".
// Mutation "the histogram is made for every method" turns it red. Green only
// for a histogram made in newMethod; one made lazily at the first warm-up call
// would need this test to change.
func TestNewMethod_WithoutAWarmupHasNoWarmupHistogram(t *testing.T) {
	made := func(warmup time.Duration) float64 {
		stats := NewStats()
		stats.Start(time.Now(), warmup)

		return testing.AllocsPerRun(50, func() { sinkMethod = stats.newMethod() })
	}

	without, with := made(0), made(time.Second)
	if without >= with {
		t.Errorf("a method makes %v allocations without a warm-up and %v with one: the histogram is made either way", without, with)
	}
}
