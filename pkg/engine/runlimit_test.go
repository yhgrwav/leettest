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

// statsOf runs n calls through the engine's own statistics: each started lag
// late, waited stream for a stream, and was served in 2ms; the first unsent
// of them never went out, held for a stream to their 1s deadline.
func statsOf(n, unsent int, lag, stream time.Duration) Report {
	stats := NewStats()
	start := time.Now()
	stats.Start(start, 0)
	for i := range n {
		sched := start.Add(time.Duration(i) * 5 * time.Millisecond)
		begun := sched.Add(lag)
		out := Outcome{Category: CategorySuccess, StreamWait: stream, SentAt: begun.Add(stream)}
		out.DoneAt = out.SentAt.Add(2 * time.Millisecond)
		if i < unsent {
			out = Outcome{Category: CategoryTimeout, NotSent: true, StreamWait: time.Second - lag,
				SentAt: sched.Add(time.Second), DoneAt: sched.Add(time.Second)}
		}
		r := Result{Method: "a.B/C", ScheduledAt: sched, BegunAt: begun, Deadline: sched.Add(time.Second), Outcome: out}
		if i < unsent {
			r.NotSentOn = BlockedOnStream
		}
		stats.Record(r)
	}
	stats.EndSending(start.Add(2 * time.Second))
	stats.Finish(start.Add(2 * time.Second))

	return stats.Report()
}

// The rule on the engine's own reports, not hand-made ones, and the fields
// that hand-made reports in other packages' tests must carry to match: a
// wait that moved p99 or unsent calls come with that cause's tail calls.
func TestReport_WaitVerdictOnTheEnginesOwnReports(t *testing.T) {
	for _, tc := range []struct {
		name    string
		report  Report
		cause   WaitCause
		limited bool
	}{
		{"nobody waited", statsOf(200, 0, 0, 0), "", false},
		{"the generator 20ms late", statsOf(200, 0, 20*time.Millisecond, 0), WaitGenerator, true},
		{"3 unsent, held for a stream", statsOf(200, 3, 0, 0), WaitStream, true},
	} {
		r := tc.report
		cause, _, limited := r.WaitVerdict()
		if cause != tc.cause || limited != tc.limited {
			t.Errorf("%s: %q %v, want %q %v", tc.name, cause, limited, tc.cause, tc.limited)
		}
		switch tc.cause {
		case WaitGenerator:
			if !r.ClientWaitsMoved() || r.GeneratorTailCalls == 0 {
				t.Errorf("%s: moved %v, generator tail %d: a moved p99 comes with its tail calls", tc.name, r.ClientWaitsMoved(), r.GeneratorTailCalls)
			}
		case WaitStream:
			if r.NotSent != 3 || r.StreamTailCalls == 0 {
				t.Errorf("%s: unsent %d, stream tail %d: unsent calls count in their cause's tail", tc.name, r.NotSent, r.StreamTailCalls)
			}
		}
	}
}

// Ground: contract — one rule for the report's verdict and the search (#140,
// moved from internal/cli): a tenth of p99 on the histogram's values, or
// unsent calls, and a wait in the tail; and one place says whose limit it is:
// generator and stream the run's, a connection not ready the target's (#90).
func TestReport_WaitVerdictIsTheVerdictRule(t *testing.T) {
	q := func(ms float64) metrics.Quantile {
		return metrics.Quantile{Value: time.Duration(ms * float64(time.Millisecond)), Exact: true, Defined: true}
	}
	for _, tc := range []struct {
		name   string
		report Report
		cause  WaitCause
		side   Side
		ok     bool
	}{
		{"1.1ms of 21.9ms", Report{GeneratorTailCalls: 100, Methods: []MethodReport{{P99: q(21.9), P99WithoutClientWaits: q(20.8)}}}, "", "", false},
		{"exactly a tenth", Report{GeneratorTailCalls: 100, Methods: []MethodReport{{P99: q(20), P99WithoutClientWaits: q(18)}}}, WaitGenerator, SideRun, true},
		{"unsent for a stream", Report{NotSent: 2, NotSentStream: 2, StreamTailCalls: 5, Methods: []MethodReport{{P99: q(20), P99WithoutClientWaits: q(20)}}}, WaitStream, SideRun, true},
		{"moved, nobody waited in the tail", Report{Methods: []MethodReport{{P99: q(20), P99WithoutClientWaits: q(10)}}}, "", "", false},
		{"the larger cause in the tail", Report{GeneratorTailCalls: 3, StreamTailCalls: 9, Methods: []MethodReport{{P99: q(20), P99WithoutClientWaits: q(10)}}}, WaitStream, SideRun, true},
		{"a connection not ready", Report{ConnectionTailCalls: 9, Methods: []MethodReport{{P99: q(20), P99WithoutClientWaits: q(10)}}}, WaitConnection, SideTarget, true},
	} {
		cause, side, ok := tc.report.WaitVerdict()
		if cause != tc.cause || side != tc.side || ok != tc.ok {
			t.Errorf("%s: %q %q %v, want %q %q %v", tc.name, cause, side, ok, tc.cause, tc.side, tc.ok)
		}
	}
}
