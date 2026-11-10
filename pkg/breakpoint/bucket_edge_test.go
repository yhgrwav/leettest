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

package breakpoint

import (
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// recordedStep is one step's run in which all sent calls were answered in
// latency, the p99 read from a real histogram of the recorder, as a run does:
// the report holds the bucket's top, 100.008ms for 100ms.
func recordedStep(sent int, latency time.Duration) engine.Report {
	l := metrics.NewLatencies()
	for range sent {
		l.Record(latency)
	}
	p99 := l.Snapshot().Percentile(0.99)

	return engine.Report{Sent: sent, Methods: []engine.MethodReport{{
		Method: "a.B/C", Sent: sent, Latencies: sent, P99: p99, P99WithoutClientWaits: p99,
	}}}
}

// brokeAtSecondStep runs a search of two steps, 100 and 125 rps, the first at
// first and the second at second, and says whether the second broke.
func brokeAtSecondStep(t *testing.T, p Plan, first, second time.Duration) bool {
	t.Helper()

	p.From, p.To, p.Settle, p.Hold = 100, 125, 10*time.Millisecond, 50*time.Millisecond
	var asked []int
	target := func(rps int) engine.Report {
		if rps == 100 {
			return recordedStep(1000, first)
		}

		return recordedStep(1000, second)
	}
	res, err := Search(t.Context(), p, fake(target, &asked))
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	return res.Outcome == BrokeBetween
}

// Ground: boundary — a step whose 1000 calls all took exactly 100ms holds
// against p99_limit 100ms: the report prints the bucket's top, 100.008ms, but
// the bucket starts at 99.942ms and nothing measured is over the limit.
// 100.2ms is in a bucket that starts at 100.139ms, so it breaks. Real
// histograms: the edges of a bucket cannot be set through a fake Quantile.
func TestSearch_P99LimitIsComparedAtTheBucketsLowerEdge(t *testing.T) {
	const ms = time.Millisecond

	for _, tc := range []struct {
		name  string
		step2 time.Duration
		limit time.Duration
		broke bool
	}{
		{"exactly at the limit", 100 * ms, 100 * ms, false},
		{"limit at the bucket's lower edge", 100 * ms, 99_942_400 * time.Nanosecond, false},
		{"limit 1ns under the bucket's lower edge", 100 * ms, 99_942_400*time.Nanosecond - 1, true},
		{"100.2ms", 100200 * time.Microsecond, 100 * ms, true},
		{"2x the limit", 200 * ms, 100 * ms, true},
	} {
		if got := brokeAtSecondStep(t, Plan{P99Limit: tc.limit}, 10*ms, tc.step2); got != tc.broke {
			t.Errorf("%s: broke %v, want %v", tc.name, got, tc.broke)
		}
	}
}

// Ground: boundary — the knee is checked the same way: against a first step of
// 20ms the knee is 3 x the printed baseline, and a step at exactly 60ms holds
// (its bucket starts at 59.998ms, under the knee at 60.015ms) while 61ms
// breaks. Real histograms on both steps.
func TestSearch_TheKneeIsComparedAtTheBucketsLowerEdge(t *testing.T) {
	const ms = time.Millisecond

	for _, tc := range []struct {
		name  string
		step2 time.Duration
		broke bool
	}{
		{"exactly 3x the baseline", 60 * ms, false},
		{"61ms", 61 * ms, true},
		{"5x", 100 * ms, true},
	} {
		if got := brokeAtSecondStep(t, Plan{}, 20*ms, tc.step2); got != tc.broke {
			t.Errorf("%s: broke %v, want %v", tc.name, got, tc.broke)
		}
	}
}
