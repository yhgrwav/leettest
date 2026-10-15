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

// p999Report records successes of 1..ok ms, timeouts that ran out their one second and
// overload refusals, and reports the only method.
func p999Report(ok, timeouts, overload int) MethodReport {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)

	for i := 1; i <= ok; i++ {
		stats.Record(Result{Method: "a", ScheduledAt: start, BegunAt: start, Deadline: start.Add(time.Second),
			Outcome: Outcome{Category: CategorySuccess, SentAt: start, DoneAt: start.Add(time.Duration(i) * time.Millisecond)}})
	}
	for range timeouts {
		stats.Record(Result{Method: "a", ScheduledAt: start, BegunAt: start, Deadline: start.Add(time.Second),
			Outcome: Outcome{Category: CategoryTimeout, SentAt: start, DoneAt: start.Add(time.Second)}})
	}
	for range overload {
		stats.Record(Result{Method: "a", ScheduledAt: start, BegunAt: start, Deadline: start.Add(time.Second),
			Outcome: Outcome{Category: CategoryOverload, SentAt: start, DoneAt: start.Add(time.Millisecond)}})
	}
	stats.EndSending(start.Add(2 * time.Second))
	stats.Finish(start.Add(2 * time.Second))

	return stats.Report().Methods[0]
}

// Ground: boundary — with 999 observations p99.9 is the max; the report says "not defined"
// rather than printing it (ghz#432).
func TestP999IsUndefinedBelowTheThreshold(t *testing.T) {
	m := p999Report(999, 0, 0)
	if m.Latencies != 999 {
		t.Fatalf("Latencies = %d, want 999", m.Latencies)
	}
	if m.P999.Defined {
		t.Errorf("P999 = %+v with 999 observations, want undefined", m.P999)
	}
}

// Ground: boundary — at 1000 observations the rank is 999, below n: the value is the one
// Snapshot.Percentile(0.999) gives for the same distribution.
func TestP999AtTheThresholdIsThePercentileRule(t *testing.T) {
	m := p999Report(1000, 0, 0)

	l := metrics.NewLatencies()
	for i := 1; i <= 1000; i++ {
		l.Record(time.Duration(i) * time.Millisecond)
	}
	want := l.Snapshot().Percentile(0.999)

	if !m.P999.Defined || !want.Defined {
		t.Fatalf("P999 = %+v, rule = %+v, want both defined", m.P999, want)
	}
	if m.P999.Value != want.Value || m.P999.Exact != want.Exact {
		t.Errorf("P999 = %+v, want %+v", m.P999, want)
	}
	if m.P999.Value >= m.Max.Value {
		t.Errorf("P999 = %v is not below the max %v", m.P999.Value, m.Max.Value)
	}
}

// Ground: boundary — censored observations are counted and hold the rank: 990 successes and
// 10 timeouts make 1000 observations, rank 999 sits among the timeouts, a lower bound.
func TestP999WithCensoredObservationsIsALowerBound(t *testing.T) {
	m := p999Report(990, 10, 0)
	if m.Latencies != 1000 || m.Censored != 10 {
		t.Fatalf("Latencies = %d, Censored = %d, want 1000 and 10", m.Latencies, m.Censored)
	}
	if !m.P999.Defined || m.P999.Exact {
		t.Errorf("P999 = %+v, want a defined lower bound", m.P999)
	}
}

// Ground: boundary — the threshold counts observations, not sent calls: refusals are not in
// the distribution. 900 + 600 refusals = 1500 sent, 900 observations.
func TestP999CountsObservationsNotSentCalls(t *testing.T) {
	m := p999Report(900, 0, 600)
	if m.Sent != 1500 || m.Latencies != 900 {
		t.Fatalf("Sent = %d, Latencies = %d, want 1500 and 900", m.Sent, m.Latencies)
	}
	if m.P999.Defined {
		t.Errorf("P999 = %+v with 900 observations of 1500 sent, want undefined", m.P999)
	}
}
