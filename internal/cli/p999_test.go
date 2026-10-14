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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// p999Method records successes of 1..ok ms and timeouts that ran out their second.
func p999Method(t *testing.T, ok, timeouts int) engine.MethodReport {
	t.Helper()

	if ok+timeouts == 0 {
		return engine.MethodReport{Method: "a.B/One"}
	}

	stats := engine.NewStats()
	start := time.Now()
	stats.Start(start, 0)
	for i := 1; i <= ok; i++ {
		stats.Record(engine.Result{Method: "a.B/One", ScheduledAt: start, BegunAt: start, Deadline: start.Add(time.Second),
			Outcome: engine.Outcome{Category: engine.CategorySuccess, SentAt: start, DoneAt: start.Add(time.Duration(i) * time.Millisecond)}})
	}
	for range timeouts {
		stats.Record(engine.Result{Method: "a.B/One", ScheduledAt: start, BegunAt: start, Deadline: start.Add(time.Second),
			Outcome: engine.Outcome{Category: engine.CategoryTimeout, SentAt: start, DoneAt: start.Add(time.Second)}})
	}
	stats.EndSending(start.Add(2 * time.Second))
	stats.Finish(start.Add(2 * time.Second))

	return stats.Report().Methods[0]
}

func p999Text(m engine.MethodReport) string {
	var out strings.Builder
	PrintReport(&out, "localhost:50051", RunReport{Report: engine.Report{Sent: m.Sent, Methods: []engine.MethodReport{m}}})

	return out.String()
}

func p999JSON(t *testing.T, m engine.MethodReport) any {
	t.Helper()

	out := writeJSON(t, jsonRun(engine.Report{Sent: m.Sent, Methods: []engine.MethodReport{m}}))
	method := field(t, out, "methods").([]any)[0]

	return field(t, method, "latency", "p999")
}

// Ground: contract — below the threshold the JSON field is null and the text says why, with
// the observations behind the percentile and the threshold from metrics.MinCount.
func TestP999BelowTheThresholdIsNullAndExplained(t *testing.T) {
	for _, n := range []int{0, 999} {
		m := p999Method(t, n, 0)
		if v := p999JSON(t, m); v != nil {
			t.Errorf("n=%d: latency.p999 = %v, want null", n, v)
		}
		want := "  p99.9: too few observations (" + strconv.Itoa(n) + " of 1000)"
		if text := p999Text(m); !strings.Contains(text, want+"\n") {
			t.Errorf("n=%d: report has no line %q:\n%s", n, want, text)
		}
	}
}

// Ground: contract — at the threshold the value is the percentile rule's, exact, and text and
// JSON agree on it.
func TestP999AtTheThresholdIsTheRulesValueInTextAndJSON(t *testing.T) {
	m := p999Method(t, 1000, 0)

	l := metrics.NewLatencies()
	for i := 1; i <= 1000; i++ {
		l.Record(time.Duration(i) * time.Millisecond)
	}
	want := l.Snapshot().Percentile(0.999)

	v := p999JSON(t, m)
	if v == nil {
		t.Fatal("latency.p999 is null with 1000 observations")
	}
	if us := field(t, v, "us").(float64); int64(us) != want.Value.Microseconds() {
		t.Errorf("latency.p999.us = %v, want %d", us, want.Value.Microseconds())
	}
	if lb := field(t, v, "lower_bound"); lb != false {
		t.Errorf("latency.p999.lower_bound = %v, want false", lb)
	}
	line := "  p99.9: " + formatQuantile(want) + "\n"
	if text := p999Text(m); !strings.Contains(text, line) {
		t.Errorf("report has no line %q:\n%s", line, text)
	}
}

// Ground: contract — censored observations hold rank 999: a lower bound, ">" in the text and
// lower_bound in the JSON, the same number in both.
func TestP999HeldByCensoredCallsIsALowerBoundInTextAndJSON(t *testing.T) {
	m := p999Method(t, 990, 10)

	v := p999JSON(t, m)
	if v == nil {
		t.Fatal("latency.p999 is null with 1000 observations")
	}
	if lb := field(t, v, "lower_bound"); lb != true {
		t.Errorf("latency.p999.lower_bound = %v, want true", lb)
	}
	if us := field(t, v, "us").(float64); int64(us) != m.P999.Value.Microseconds() {
		t.Errorf("latency.p999.us = %v, want %d", us, m.P999.Value.Microseconds())
	}
	line := "  p99.9: >"
	text := p999Text(m)
	if !strings.Contains(text, line) || !strings.Contains(text, "  p99.9: "+formatQuantile(m.P999)+"\n") {
		t.Errorf("report has no lower-bound p99.9 line:\n%s", text)
	}
}

// Ground: contract — the line follows the method row, before the dataset line.
// Ground: contract — with a plan line the p99.9 line comes right after it, before the dataset.
func TestP999LineFollowsThePlanLine(t *testing.T) {
	m := p999Method(t, 999, 0)
	m.PlannedDuration = 10 * time.Second
	m.RPSLow, m.RPSHigh = 100, 100
	m.Dataset = &engine.DatasetReport{File: "d.jsonl", Records: 5}

	lines := strings.Split(p999Text(m), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "  plan:") {
			if i+2 >= len(lines) || !strings.HasPrefix(lines[i+1], "  p99.9:") || !strings.Contains(lines[i+2], "d.jsonl") {
				t.Fatalf("p99.9 is not between plan and dataset:\n%s", strings.Join(lines, "\n"))
			}
			return
		}
	}
	t.Fatalf("no plan line in the report:\n%s", strings.Join(lines, "\n"))
}

func TestP999LineFollowsTheMethodRow(t *testing.T) {
	m := p999Method(t, 999, 0)
	m.Dataset = &engine.DatasetReport{File: "d.jsonl", Records: 5}

	lines := strings.Split(p999Text(m), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "a.B/One") {
			if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "  p99.9:") {
				t.Fatalf("the line after the method row is not p99.9:\n%s", strings.Join(lines, "\n"))
			}
			return
		}
	}
	t.Fatal("no method row in the report")
}
