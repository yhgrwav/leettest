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

// The generator decided the p99 tail (Report.GeneratorDecidedTail) when the
// wait verdict names the generator and either a method with at least
// metrics.MinCount(0.99) observations has a p99 the lag moved, or the generator
// left calls unsent. Below that count a p99 is the maximum, and one call over
// the floor moves it: the verdict stays, the run is not called late for it.

func q(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Exact: true, Defined: true}
}

// movedMethod is a method of n observations whose p99 is 50ms and 5ms without
// the waits.
func movedMethod(name string, n int) MethodReport {
	return MethodReport{Method: name, Sent: n, Latencies: n, P99: q(50 * time.Millisecond), P99WithoutClientWaits: q(5 * time.Millisecond)}
}

// steadyMethod is a method of n observations whose p99 the waits did not move.
func steadyMethod(name string, n int) MethodReport {
	return MethodReport{Method: name, Sent: n, Latencies: n, P99: q(8 * time.Millisecond), P99WithoutClientWaits: q(8 * time.Millisecond)}
}

// lostCalls is a method of n observations that lost k calls to the generator.
func lostCalls(m MethodReport, k int) MethodReport {
	m.NotSent, m.NotSentGenerator = k, k

	return m
}

// generatorReport has the generator leading the tail; the unsent calls of the
// methods are summed into the report.
func generatorReport(methods ...MethodReport) Report {
	r := Report{Methods: methods, GeneratorCauseCalls: 40, GeneratorTailCalls: 12}
	for i := range methods {
		r.Sent += methods[i].Sent
		r.NotSent += methods[i].NotSent
		r.NotSentGenerator += methods[i].NotSentGenerator
	}

	return r
}

func TestGeneratorDecidedTail_ThePerMethodObservationBoundary(t *testing.T) {
	if got := metrics.MinCount(0.99); got != 100 {
		t.Fatalf("metrics.MinCount(0.99) = %d, the boundary of these rows is 99/100", got)
	}

	for _, tc := range []struct {
		name   string
		report Report
		want   bool
	}{
		{"moved, 99 observations", generatorReport(movedMethod("/a.B/C", 99)), false},
		{"moved, 100 observations", generatorReport(movedMethod("/a.B/C", 100)), true},
		{"moved, 15 observations", generatorReport(movedMethod("/a.B/C", 15)), false},
		{"moved, 1000 observations", generatorReport(movedMethod("/a.B/C", 1000)), true},
		{"two moved methods of 60: the count is per method, not the sum",
			generatorReport(movedMethod("/a.B/C", 60), movedMethod("/a.B/D", 60)), false},
		{"1000 observations unmoved, 15 moved",
			generatorReport(steadyMethod("/a.B/C", 1000), movedMethod("/a.B/D", 15)), false},
		{"15 moved, 100 moved", generatorReport(movedMethod("/a.B/C", 15), movedMethod("/a.B/D", 100)), true},
		{"15 observations, p99 unmoved, one call left unsent by the generator",
			generatorReport(lostCalls(steadyMethod("/a.B/C", 15), 1)), true},
		{"15 moved and another method lost a call",
			generatorReport(movedMethod("/a.B/C", 15), lostCalls(steadyMethod("/a.B/D", 15), 1)), true},
		{"unmoved, nothing unsent", generatorReport(steadyMethod("/a.B/C", 1000)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.report.GeneratorDecidedTail(); got != tc.want {
				t.Errorf("GeneratorDecidedTail = %v, want %v", got, tc.want)
			}
		})
	}
}

// The predicate is about the generator: a stream or a connection leading the
// tail is not it, however many observations the moved method has.
func TestGeneratorDecidedTail_OnlyWhenTheVerdictNamesTheGenerator(t *testing.T) {
	stream := generatorReport(movedMethod("/a.B/C", 1000))
	stream.GeneratorCauseCalls, stream.GeneratorTailCalls = 0, 0
	stream.StreamCauseCalls, stream.StreamTailCalls = 30, 30

	conn := generatorReport(movedMethod("/a.B/C", 1000))
	conn.GeneratorCauseCalls, conn.GeneratorTailCalls = 0, 0
	conn.ConnectionCauseCalls, conn.ConnectionTailCalls = 30, 30

	for name, report := range map[string]Report{"stream": stream, "connection": conn, "nothing": {}} {
		if report.GeneratorDecidedTail() {
			t.Errorf("%s: GeneratorDecidedTail = true", name)
		}
	}
}

// The methods the lag decided: a moved one with enough observations, or one
// that lost calls to the generator whatever its count.
func TestMethodReport_LateByGeneratorNeedsEnoughObservationsToBeMoved(t *testing.T) {
	stream := steadyMethod("/a.B/C", 15)
	stream.NotSent, stream.NotSentStream = 2, 2

	for _, tc := range []struct {
		name string
		m    MethodReport
		want bool
	}{
		{"moved, 99", movedMethod("/a.B/C", 99), false},
		{"moved, 100", movedMethod("/a.B/C", 100), true},
		{"unmoved, 1000", steadyMethod("/a.B/C", 1000), false},
		{"unmoved, 15, lost a call to the generator", lostCalls(steadyMethod("/a.B/C", 15), 1), true},
		{"moved, 15, lost a call to the generator", lostCalls(movedMethod("/a.B/C", 15), 1), true},
		{"unmoved, 15, lost calls to a stream", stream, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.LateByGenerator(); got != tc.want {
				t.Errorf("LateByGenerator = %v, want %v", got, tc.want)
			}
		})
	}
}
