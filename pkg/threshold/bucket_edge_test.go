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
	"github.com/yhgrwav/leettest/pkg/metrics"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

// The bucket the histogram keeps 100 ms in, at three significant digits: it
// holds 99 942 400 ns to 100 007 935 ns, and the report prints its top.
const (
	bucketLow100ms = 99_942_400 * time.Nanosecond
	bucketTop100ms = 100_007_935 * time.Nanosecond
)

// recorded is a method that sent n calls, every one answered in latency, with
// each percentile read from a real histogram of the recorder, as a run does.
func recorded(t *testing.T, n int, latency time.Duration) engine.MethodReport {
	t.Helper()

	l := metrics.NewLatencies()
	for range n {
		l.Record(latency)
	}

	s := l.Snapshot()
	m := served(shop, n, 0)
	m.Min, m.P50, m.P90, m.P95, m.P99, m.P999, m.Max = s.Percentile(0), s.Percentile(0.5), s.Percentile(0.9),
		s.Percentile(0.95), s.Percentile(0.99), s.Percentile(0.999), s.Percentile(1)
	m.P99WithoutClientWaits = m.P99

	return m
}

// onlyKey is the limits of one key, none of the others: a verdict on it alone.
func onlyKey(key threshold.Key, limit time.Duration) threshold.MethodLimits {
	l := threshold.Limits{Errors: &threshold.Percent{Thousandths: 1000, Text: "1%"}}
	switch key {
	case threshold.P50:
		l.P50 = limit
	case threshold.P90:
		l.P90 = limit
	case threshold.P95:
		l.P95 = limit
	case threshold.P99:
		l.P99 = limit
	case threshold.P999:
		l.P999 = limit
	}

	return threshold.MethodLimits{Name: shop, Limits: l}
}

// Ground: boundary — 1000 calls at exactly 100ms against a limit of 100ms
// pass. The run prints the bucket's top, 100.008ms, but nothing measured is
// over the limit: the bucket starts at 99.942ms and the calls are inside it.
// Failing here was the audit's finding (items 3 and 4). Every percentile a
// limit can name reads the same. A real histogram from the recorder: through a
// fake Quantile the bucket's edges cannot be seen.
func TestLimit_CallsExactlyAtTheLimitPass(t *testing.T) {
	m := recorded(t, 1000, 100*time.Millisecond)
	if m.P99.Value != bucketTop100ms || !m.P99.Exact {
		t.Fatalf("fixture: p99 %+v, want the exact bucket top %v", m.P99, bucketTop100ms)
	}

	for _, key := range []threshold.Key{threshold.P50, threshold.P90, threshold.P95, threshold.P99, threshold.P999} {
		r := find(t, evaluate(runOf(m), onlyKey(key, 100*time.Millisecond)), shop, key)
		verdictIs(t, r, threshold.VerdictPass, "")
	}
}

// Ground: boundary — the bucket's lowest value against the limit, to the
// nanosecond: a limit equal to the lower edge is not under it, so it passes;
// one nanosecond below it fails, since every call of the bucket is over it.
func TestLimit_TheBucketsLowerEdgeDecides(t *testing.T) {
	m := recorded(t, 1000, 100*time.Millisecond)

	for _, tc := range []struct {
		name  string
		limit time.Duration
		want  threshold.Verdict
	}{
		{"limit at the bucket's top", bucketTop100ms, threshold.VerdictPass},
		{"limit at the lower edge", bucketLow100ms, threshold.VerdictPass},
		{"limit 1ns under the lower edge", bucketLow100ms - 1, threshold.VerdictFail},
		{"limit 1ms under", 99 * time.Millisecond, threshold.VerdictFail},
	} {
		r := find(t, evaluate(runOf(m), onlyKey(threshold.P99, tc.limit)), shop, threshold.P99)
		if r.Verdict != tc.want || r.Reason != "" {
			t.Errorf("%s (%v): %q (reason %q), want %q", tc.name, tc.limit, r.Verdict, r.Reason, tc.want)
		}
	}
}

// Ground: contract — a p99 clearly above the limit still fails: 1000 calls at
// 100.2ms are in a bucket that starts at 100.139ms, over a 100ms limit by
// more than the bucket's width. The result still carries the printed value
// (the bucket's top), not the edge the verdict was read from.
func TestLimit_ClearlyOverTheLimitStillFails(t *testing.T) {
	m := recorded(t, 1000, 100200*time.Microsecond)

	r := find(t, evaluate(runOf(m), onlyKey(threshold.P99, 100*time.Millisecond)), shop, threshold.P99)
	verdictIs(t, r, threshold.VerdictFail, "")
	if r.Value.Value != m.P99.Value {
		t.Errorf("result carries %v, want the printed value %v", r.Value.Value, m.P99.Value)
	}
}

// Ground: contract — a percentile that fell into the censored mass is a lower
// bound and is compared as before: here the bound is the timeout itself,
// 100ms (not the bucket's lower edge, and not the top). It fails once over the
// limit by a nanosecond and decides nothing at the limit or under it.
// Green before the change: it keeps the line for the code.
func TestLimit_ALowerBoundInTheCensoredMassIsComparedAsBefore(t *testing.T) {
	l := metrics.NewLatencies()
	for range 980 {
		l.Record(10 * time.Millisecond)
	}
	for range 20 {
		l.RecordCensored(100 * time.Millisecond)
	}
	m := served(shop, 980, 10*time.Millisecond)
	m.P99, m.Censored, m.TimedOut = l.Snapshot().Percentile(0.99), 20, 20
	if m.P99.Exact || m.P99.Value != 100*time.Millisecond {
		t.Fatalf("fixture: p99 %+v, want the lower bound 100ms", m.P99)
	}

	for _, tc := range []struct {
		limit  time.Duration
		want   threshold.Verdict
		reason threshold.Reason
	}{
		{100*time.Millisecond - 1, threshold.VerdictFail, ""},
		{100 * time.Millisecond, threshold.VerdictUnknown, threshold.ReasonCensored},
		{200 * time.Millisecond, threshold.VerdictUnknown, threshold.ReasonCensored},
	} {
		r := find(t, evaluate(runOf(m), onlyKey(threshold.P99, tc.limit)), shop, threshold.P99)
		if r.Verdict != tc.want || r.Reason != tc.reason {
			t.Errorf("limit %v: %q (reason %q), want %q (reason %q)", tc.limit, r.Verdict, r.Reason, tc.want, tc.reason)
		}
	}
}
