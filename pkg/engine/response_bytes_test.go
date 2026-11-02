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
// latency later.
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

// Ground: contract — MethodReport.ResponseBytes is the mean over the measured
// calls that got a reply body, whatever else came back. Out of it: warm-up
// (#88) and a call with no body.
func TestReport_ResponseBytesAreTheMeanOfMeasuredCallsThatGotABody(t *testing.T) {
	ms := time.Millisecond
	stats := warmupRun(t, []Outcome{
		{Category: CategorySuccess, ResponseBytes: 90000}, // warm-up
		{Category: CategorySuccess, ResponseBytes: 1000},  // measured
		{Category: CategoryOverload, Code: "Unavailable"}, // measured, no body
		{Category: CategorySuccess, ResponseBytes: 3000},  // measured
		{Category: CategorySuccess, ResponseBytes: 2000},  // measured
	}, []time.Duration{100 * ms, 1100 * ms, 1200 * ms, 1300 * ms, 1400 * ms})

	got := stats.Report().Methods[0].ResponseBytes
	if got == nil {
		t.Fatal("ResponseBytes is nil, want the mean of the three measured calls that got a body")
	}
	if *got != 2000 {
		t.Errorf("ResponseBytes = %v, want 2000: (1000+3000+2000)/3, the call without a body and the warm-up out", *got)
	}
}

// Ground: contract — a method none of whose measured calls got a body has no
// size: nil, not a zero that reads as an empty reply. A warm-up call that got
// one does not give the measured part a size.
func TestReport_ResponseBytesAreNilWhenNoMeasuredCallGotABody(t *testing.T) {
	ms := time.Millisecond
	stats := warmupRun(t, []Outcome{
		{Category: CategorySuccess, ResponseBytes: 5000}, // warm-up
		{Category: CategoryUnreachable, Code: "Unavailable"},
		{Category: CategoryOverload, Code: "Unavailable"},
	}, []time.Duration{100 * ms, 1100 * ms, 1200 * ms})

	if got := stats.Report().Methods[0].ResponseBytes; got != nil {
		t.Errorf("ResponseBytes = %v, want nil", *got)
	}
}

// Ground: contract — MethodReport.Warmup is the warm-up in its own histogram:
// the calls that were answered, their latency from scheduledAt, the mean reply
// body. The measured histogram is not touched: its count and percentiles are
// those of the measured calls alone (#88).
func TestReport_WarmupHasItsOwnHistogramAndTheMeasuredOneIsUntouched(t *testing.T) {
	ms := time.Millisecond
	stats := NewStats()
	start := time.Now()
	stats.Start(start, time.Second)

	// Warm-up: three answered calls and one that never reached the target.
	recordAfter(stats, start, "a", 100*ms, 10*ms, Outcome{Category: CategorySuccess, ResponseBytes: 500})
	recordAfter(stats, start, "a", 200*ms, 20*ms, Outcome{Category: CategorySuccess, ResponseBytes: 700})
	recordAfter(stats, start, "a", 300*ms, 30*ms, Outcome{Category: CategorySuccess})
	recordAfter(stats, start, "a", 400*ms, time.Microsecond, Outcome{Category: CategoryUnreachable, Code: "Unavailable"})
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
		t.Errorf("measured ResponseBytes = %v, want 2000: the warm-up's 500 and 700 are not in it", m.ResponseBytes)
	}

	w := m.Warmup
	if w == nil {
		t.Fatal("Warmup is nil, want the three answered warm-up calls")
	}
	if w.Calls != 3 {
		t.Errorf("Warmup.Calls = %d, want 3: the unreachable call has no latency", w.Calls)
	}
	if !w.P50.Defined || !nearly(w.P50.Value, 20*ms) {
		t.Errorf("Warmup.P50 = %+v, want 20ms", w.P50)
	}
	if w.P99.Defined {
		t.Errorf("Warmup.P99 = %+v, want undefined: 3 observations are fewer than %d", w.P99, 100)
	}
	if w.ResponseBytes == nil || *w.ResponseBytes != 600 {
		t.Errorf("Warmup.ResponseBytes = %v, want 600: (500+700)/2, the call without a body out", w.ResponseBytes)
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
	if !w.P99.Defined || !nearly(w.P99.Value, 99*time.Millisecond) {
		t.Errorf("Warmup.P99 = %+v, want 99ms from 100 observations of 1..100ms", w.P99)
	}
}

// Ground: contract — a run without a warm-up, and a warm-up none of whose calls
// was answered, have no warm-up report: nil, not a zero.
func TestReport_WarmupIsNilWithoutAnsweredWarmupCalls(t *testing.T) {
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
		{Category: CategorySuccess, ResponseBytes: 10}, // measured
	}, []time.Duration{100 * ms, 200 * ms, 1500 * ms})
	if w := silent.Report().Methods[0].Warmup; w != nil {
		t.Errorf("Warmup with no answered call = %+v, want nil", w)
	}
}

// Ground: contract — Report.Traffic is the bits per second the measured calls'
// bodies came to over the window the rates divide by (end of warm-up to end of
// sending): the wire size of requests that went out, the size of replies that
// came. Warm-up is out; so is a body that was never handed over.
func TestReport_TrafficIsBodiesOverTheMeasuredWindow(t *testing.T) {
	ms := time.Millisecond
	// Window: warm-up 1s, sending ends at 3s, so 2s.
	stats := warmupRun(t, []Outcome{
		{Category: CategorySuccess, RawBytes: 9_000_000, WireBytes: 9_000_000, ResponseBytes: 9_000_000}, // warm-up
		{Category: CategorySuccess, RawBytes: 1000, WireBytes: 400, ResponseBytes: 2000},
		{Category: CategorySuccess, RawBytes: 3000, WireBytes: 600, ResponseBytes: 4000},
		{Category: CategoryUnreachable, Code: "Unavailable"},
	}, []time.Duration{100 * ms, 1100 * ms, 1200 * ms, 1300 * ms})

	tr := stats.Report().Traffic
	if tr == nil {
		t.Fatal("Traffic is nil, want the run's bits per second")
	}
	// Wire sizes 400+600 = 1000 B = 8000 bit over 2s; replies 2000+4000 = 6000 B = 48000 bit over 2s.
	if math.Abs(tr.SentBitsPerSecond-4000) > 1e-6 {
		t.Errorf("SentBitsPerSecond = %v, want 4000: (400+600) bytes x 8 / 2s, compressed size, warm-up out", tr.SentBitsPerSecond)
	}
	if math.Abs(tr.ReceivedBitsPerSecond-24000) > 1e-6 {
		t.Errorf("ReceivedBitsPerSecond = %v, want 24000: (2000+4000) bytes x 8 / 2s, warm-up out", tr.ReceivedBitsPerSecond)
	}
}

// Ground: contract — a run that never left the warm-up has no window to divide
// by: no traffic, not a division by zero.
func TestReport_TrafficIsNilWithoutAMeasuredWindow(t *testing.T) {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, time.Hour)
	recordAfter(stats, start, "a", time.Millisecond, time.Millisecond,
		Outcome{Category: CategorySuccess, RawBytes: 100, WireBytes: 100, ResponseBytes: 100})
	stats.EndSending(start.Add(time.Minute))
	stats.Finish(start.Add(time.Minute))

	if tr := stats.Report().Traffic; tr != nil {
		t.Errorf("Traffic = %+v, want nil: the run ended inside the warm-up", tr)
	}
}
