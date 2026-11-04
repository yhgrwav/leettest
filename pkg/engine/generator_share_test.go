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

import "testing"

// Calls the generator left unsent are its failure to keep the plan, not a vote
// in the ranking of causes: a method with at least ceil(1%) of its calls
// (sent and unsent) unsent by the generator makes the p99 tail the
// generator's whoever leads the verdict (docs/decisions.md, «Unsent by the
// generator: a share, not a vote»). The verdict itself, tail_wait_cause, is
// not touched.

// unsentMethod is a method of sent calls that went out, generator calls the
// generator left unsent and connection calls that waited on the connection
// in vain. Its p99 does not move without the waits: only the unsent calls
// speak.
func unsentMethod(name string, sent, generator, connection int) MethodReport {
	m := steadyMethod(name, sent)
	m.NotSent = generator + connection
	m.NotSentGenerator, m.NotSentConnection = generator, connection

	return m
}

// connectionLedReport has the unsent calls of the methods summed in, the
// p99 unmoved, so the wait verdict is the leader of the unsent calls.
func connectionLedReport(methods ...MethodReport) Report {
	r := Report{Methods: methods}
	for i := range methods {
		r.Sent += methods[i].Sent
		r.NotSent += methods[i].NotSent
		r.NotSentGenerator += methods[i].NotSentGenerator
		r.NotSentConnection += methods[i].NotSentConnection
	}

	return r
}

func TestGeneratorDecidedTail_AShareOfUnsentCallsDecidesWhoeverLeads(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report Report
		want   bool
	}{
		// 1000 calls of the method: 1% is 10.
		{"exactly 1%", connectionLedReport(unsentMethod("/a.B/C", 700, 10, 290)), true},
		{"1% minus one call", connectionLedReport(unsentMethod("/a.B/C", 700, 9, 291)), false},
		// 1050 calls: 1% is 10.5, the share rounds up to 11.
		{"the share rounds up: 10 of 1050", connectionLedReport(unsentMethod("/a.B/C", 750, 10, 290)), false},
		{"the share rounds up: 11 of 1050", connectionLedReport(unsentMethod("/a.B/C", 750, 11, 289)), true},
		// B 80k, run 3: 37380 of 1.44M calls were left by the generator, 37452 by
		// the connection.
		{"the measured shape, a near tie with the connection",
			connectionLedReport(unsentMethod("/a.B/C", 1365168, 37380, 37452)), true},
		{"the share is the method's, not the run's: two methods at 0.9%",
			connectionLedReport(unsentMethod("/a.B/C", 700, 9, 291), unsentMethod("/a.B/D", 700, 9, 291)), false},
		{"one method at 1% among methods under it",
			connectionLedReport(unsentMethod("/a.B/C", 700, 10, 290), unsentMethod("/a.B/D", 1000, 1, 299)), true},
		{"a small method counts by its own calls: 1 of 100",
			connectionLedReport(unsentMethod("/a.B/C", 50, 1, 49)), true},
		{"a method that sent nothing and lost nothing has no share",
			connectionLedReport(unsentMethod("/a.B/C", 0, 0, 0), unsentMethod("/a.B/D", 700, 9, 291)), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.report.GeneratorDecidedTail(); got != tc.want {
				t.Errorf("GeneratorDecidedTail = %v, want %v", got, tc.want)
			}
		})
	}
}

// The share does not change who the verdict names: with the connection
// leading the unsent calls, tail_wait_cause stays the connection.
func TestGeneratorDecidedTail_TheShareLeavesTheWaitVerdictAlone(t *testing.T) {
	for _, report := range []Report{
		connectionLedReport(unsentMethod("/a.B/C", 700, 10, 290)),
		connectionLedReport(unsentMethod("/a.B/C", 700, 9, 291)),
	} {
		cause, _, ok := report.WaitVerdict()
		if !ok || cause != WaitConnection {
			t.Errorf("WaitVerdict = %q, %v, want the connection", cause, ok)
		}
	}
}

// The connection leading and the generator under 1% (and the other way
// round, a stream): the run is valid as before.
func TestGeneratorDecidedTail_ALeaderOtherThanTheGeneratorUnderTheShareStaysValid(t *testing.T) {
	stream := unsentMethod("/a.B/C", 700, 9, 0)
	stream.NotSent, stream.NotSentStream = 9+291, 291

	for name, report := range map[string]Report{
		"connection": connectionLedReport(unsentMethod("/a.B/C", 700, 9, 291)),
		"stream": {
			Methods: []MethodReport{stream}, Sent: 700, NotSent: 300, NotSentGenerator: 9, NotSentStream: 291,
		},
	} {
		if report.GeneratorDecidedTail() {
			t.Errorf("%s leads, the generator left 0.9%%: GeneratorDecidedTail = true", name)
		}
	}
}
