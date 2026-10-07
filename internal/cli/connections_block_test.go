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
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// Ground: contract — the block is the report's answer to "which backend is slow or dead": one row
// per connection with its address, calls, share failed and p99, and the calls that waited for a
// stream only where there are any. Its columns line up, the percentiles are the report's own
// rule (formatQuantile), and the failures are the connection's own count (LinkReport.Failed),
// over its own calls, not the run's. Mutations "denominator Report.Sent" (4.5%, not 12.5%) and
// "one column not padded" turn it red. The block is no element of runNotes or of the JSON
// `notes`: mutation "the block is a note" turns it red.
func TestReport_ConnectionsBlockIsExact(t *testing.T) {
	slow := callsOn(addr2, 400, 50)
	slow.StreamWaited, slow.NotSentStream, slow.P99 = 30, 7, exactUS(1_000_000)

	first, last := callsOn(addr1, 400, 0), callsOn(addr1, 400, 0)
	first.P99, last.P99 = exactUS(3200), exactUS(3100)

	run := overConnections(several([]string{addr1, addr2}, first, slow, last))
	// Not the run's rows of its connections: 100 of the 1200 calls never went out.
	run.Sent, run.NotSent, run.NotSentStream, run.NotSentConnection = 1100, 100, 7, 93

	want := strings.Join([]string{
		"Connections: 3 to 2 addresses of api.example.com",
		"  1  10.0.0.1:443  400 calls   0.0% failed  p99 3.20ms",
		"  2  10.0.0.2:443  400 calls  12.5% failed  p99 1.00s   37 waited for a stream",
		"  3  10.0.0.1:443  400 calls   0.0% failed  p99 3.10ms",
	}, "\n")

	text := printedTo("api.example.com:443", run)

	if got := strings.Join(blockOf(t, text), "\n"); got != want {
		t.Errorf("block:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(text, "\n\n"+want+"\n") {
		t.Errorf("the block is not a paragraph of its own:\n%s", text)
	}

	// After the stream lines, which say how many connections ran and what the target allows.
	if i, j := strings.Index(text, "connections: 3;"), strings.Index(text, "Connections: 3 to"); i < 0 || j < i {
		t.Errorf("the block (at %d) must come after the stream line (at %d):\n%s", j, i, text)
	}

	// Not a note: the notes are also the JSON `notes` (where the block would repeat
	// `per_connection`) and the live view's final screen, which gets no new block.
	_, listed := notesOf(t, "api.example.com:443", run)

	for where, notes := range map[string][]string{"runNotes": runNotes(run), "JSON notes": listed} {
		for _, note := range notes {
			if strings.Contains(note, "Connections: ") {
				t.Errorf("%s holds the block:\n%s", where, note)
			}
		}
	}
}

// Ground: contract — the header says what the connections went to without claiming what
// LeetTest did not do: a name it resolved is named with how many addresses it gave and the host;
// an IP address (the usual L4 case: config is ip and port) or a target with a scheme, which is
// its own resolver's, is printed as given. "1 address", never "addresss". Mutation "of <host>
// for a scheme target" and "address(es) for an IP literal" turn it red.
func TestReport_ConnectionsBlockHeader(t *testing.T) {
	over := func(n int, resolved []string) *engine.Connections {
		each := make([]engine.LinkReport, n)
		for i := range each {
			each[i] = callsOn(resolved[i%len(resolved)], 10, 0)
		}

		return several(resolved, each...)
	}

	for _, tc := range []struct {
		name     string
		target   string
		n        int
		resolved []string
		want     string
	}{
		{"a name that gave two addresses", "api.example.com:443", 3, []string{addr1, addr2},
			"Connections: 3 to 2 addresses of api.example.com"},
		{"a name that gave one", "api.example.com:443", 2, []string{addr1},
			"Connections: 2 to 1 address of api.example.com"},
		{"an IP address", addr1, 3, []string{addr1}, "Connections: 3 to 10.0.0.1:443"},
		{"an IPv6 address", "[::1]:50051", 2, []string{"[::1]:50051"}, "Connections: 2 to [::1]:50051"},
		{"a target with a scheme", "dns:///api.example.com:443", 3, []string{"dns:///api.example.com:443"},
			"Connections: 3 to dns:///api.example.com:443"},
		{"a name that gave sixteen", "api.example.com:443", 2, manyAddresses(16),
			"Connections: 2 to 16 addresses of api.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := printedTo(tc.target, overConnections(over(tc.n, tc.resolved)))

			if got := blockOf(t, text)[0]; got != tc.want {
				t.Errorf("header %q, want %q", got, tc.want)
			}
		})
	}
}

func manyAddresses(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("10.0.1.%d:443", i+1)
	}

	return out
}

// subjectRow is the row of a connection printed as the second of two, its columns' widths
// aside. The run's own Sent is far from the calls, so a row built over it is wrong.
func subjectRow(t *testing.T, subject engine.LinkReport) string {
	t.Helper()

	run := overConnections(several([]string{addr1, subject.Address}, callsOn(addr1, 10, 0), subject))
	run.Sent = 7777

	for _, line := range blockOf(t, printedTo("api.example.com:443", run)) {
		if strings.HasPrefix(strings.TrimSpace(line), "2 ") {
			return flat(line)
		}
	}

	t.Fatalf("no row 2 in the block")

	return ""
}

// Ground: contract — each cell of a row: "1 call" or "n calls", the calls that waited for a
// stream (those that went out and waited over the floor, and those that never went out for a
// stream) only when there are any, and "p99 -" for a connection with no call that has a
// latency, as the method table prints an undefined percentile. Mutation "the wait column counts
// StreamWaited only" turns it red.
func TestReport_ConnectionsBlockRows(t *testing.T) {
	withWaits := func(sent, notSent int) engine.LinkReport {
		l := callsOn(addr2, 400, 0)
		l.P99, l.StreamWaited, l.NotSentStream = exactUS(3200), sent, notSent

		return l
	}
	lowerBound := callsOn(addr2, 400, 400)
	lowerBound.P99 = metrics.Quantile{Value: time.Second, Defined: true}

	refused := callsOn(addr2, 10, 10)

	one := callsOn(addr2, 1, 0)
	one.P99 = exactUS(5000)

	for _, tc := range []struct {
		name string
		link engine.LinkReport
		want string
	}{
		{"calls, no failures, no waits", withWaits(0, 0), "2 10.0.0.2:443 400 calls 0.0% failed p99 3.20ms"},
		{"waited and went out", withWaits(3, 0), "2 10.0.0.2:443 400 calls 0.0% failed p99 3.20ms 3 waited for a stream"},
		{"never went out for a stream", withWaits(0, 7), "2 10.0.0.2:443 400 calls 0.0% failed p99 3.20ms 7 waited for a stream"},
		{"both", withWaits(30, 7), "2 10.0.0.2:443 400 calls 0.0% failed p99 3.20ms 37 waited for a stream"},
		{"a lower bound", lowerBound, "2 10.0.0.2:443 400 calls 100.0% failed p99 >1.00s"},
		{"every call refused: no latency", refused, "2 10.0.0.2:443 10 calls 100.0% failed p99 -"},
		{"no call at all: no share either", callsOn(addr2, 0, 0), "2 10.0.0.2:443 0 calls - failed p99 -"},
		{"one call", one, "2 10.0.0.2:443 1 call 0.0% failed p99 5.00ms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := subjectRow(t, tc.link); got != tc.want {
				t.Errorf("row %q, want %q", got, tc.want)
			}
		})
	}
}

// Ground: boundary — the share failed has one decimal and never states more than is known: a
// broken connection never reads as "0.0%" (under 0.05% it is "<0.1%"), and a working one never as
// "100.0%" (from 99.95% it is ">99.9%"); "-" for a connection with no call. The cases sit on
// both sides of each edge, not on a tie. Mutation "rounding without the thresholds" turns it red.
func TestReport_ConnectionsBlockShareFailed(t *testing.T) {
	for _, tc := range []struct {
		failed, calls int
		want          string
	}{
		{0, 400, "0.0%"},
		{50, 400, "12.5%"},
		{1, 3, "33.3%"},
		{1, 3000, "<0.1%"},
		{1, 2001, "<0.1%"}, // 0.04998%: just under the edge
		{1, 1999, "0.1%"},  // 0.05003%: just over it
		{1998, 1999, "99.9%"},
		{2000, 2001, ">99.9%"}, // 99.95002%
		{2999, 3000, ">99.9%"},
		{3000, 3000, "100.0%"},
		{0, 0, "-"},
	} {
		got := subjectRow(t, callsOn(addr2, tc.calls, tc.failed))
		want := fmt.Sprintf("2 10.0.0.2:443 %d calls %s failed p99 -", tc.calls, tc.want)

		if got != want {
			t.Errorf("%d failed of %d: row %q, want %q", tc.failed, tc.calls, got, want)
		}
	}
}

// Ground: contract — with one connection the report is as it was: no block. And a sender that
// said nothing of its connections gets none either. Guards, green before the code: the inverse
// of "the block is printed at N = 1" and "a missing Connections reads as one".
func TestReport_NoConnectionsBlockWithOneConnectionOrNone(t *testing.T) {
	for name, conns := range map[string]*engine.Connections{
		"one connection": {Open: 1, LimitAnnounced: true, FirstLimit: 4, LastLimit: 4, InFlightLimit: 4, InFlightAnnounced: true},
		"no data":        nil,
	} {
		if text := printedTo("api.example.com:443", overConnections(conns)); strings.Contains(text, "Connections:") {
			t.Errorf("%s: a Connections block in\n%s", name, text)
		}
	}
}

// notesOf is the notes of a run as the text report prints them and as the JSON report lists them.
func notesOf(t *testing.T, target string, run RunReport) (text string, listed []string) {
	t.Helper()

	return printedTo(target, run), NewJSONReport(JSONRun{Target: target, Version: "v0", Outcome: OutcomeComplete, Run: run}).Notes
}

// Ground: contract — the notes say the two ways connections can be spread badly, in the same
// words in the text and in JSON: more connections than addresses that do not divide evenly load
// some backends more (those over the even share are named); more addresses than connections
// leave some unused (those are named). A name resolving to more addresses than there are
// connections is not "uneven": the first note is only for N > M (a mutation dropping that
// condition turns the N = 2, M = 3 case red).
func TestReport_ConnectionsNotes(t *testing.T) {
	const name = "api.example.com:443"

	uneven := func(parts string) string {
		return parts + ", the backends are loaded unevenly."
	}

	cases := []struct {
		name     string
		target   string
		resolved []string
		each     []string
		want     []string
	}{
		{"3 over 2", name, []string{addr1, addr2}, []string{addr1, addr2, addr1}, []string{
			uneven("3 connections over 2 addresses: 10.0.0.1:443 carries 2 of 3")}},
		{"5 over 3: two over the even share", name, []string{addr1, addr2, addr3},
			[]string{addr1, addr2, addr3, addr1, addr2}, []string{
				uneven("5 connections over 3 addresses: 10.0.0.1:443 carries 2 of 5, 10.0.0.2:443 carries 2 of 5")}},
		{"2 over 3: one address unused, nothing uneven", name, []string{addr1, addr2, addr3}, []string{addr1, addr2}, []string{
			"api.example.com resolved to 3 addresses; 2 connections load 2 of them: 10.0.0.3:443 got no calls."}},
		{"2 over 4: two unused", name, []string{addr1, addr2, addr3, addr4}, []string{addr1, addr2}, []string{
			"api.example.com resolved to 4 addresses; 2 connections load 2 of them: 10.0.0.3:443, 10.0.0.4:443 got no calls."}},
		{"4 over 2 is even", name, []string{addr1, addr2}, []string{addr1, addr2, addr1, addr2}, nil},
		{"2 over 2 is even", name, []string{addr1, addr2}, []string{addr1, addr2}, nil},
		{"3 over the one address of an IP", addr1, []string{addr1}, []string{addr1, addr1, addr1}, nil},
		{"2 over the one address of a scheme target", "dns:///api.example.com:443", []string{"dns:///api.example.com:443"},
			[]string{"dns:///api.example.com:443", "dns:///api.example.com:443"}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			links := make([]engine.LinkReport, len(tc.each))
			for i, a := range tc.each {
				links[i] = callsOn(a, 10, 0)
			}

			text, listed := notesOf(t, tc.target, overConnections(several(tc.resolved, links...)))

			for _, note := range tc.want {
				if !strings.Contains(text, "\n"+note+"\n") {
					t.Errorf("no note %q in the text report:\n%s", note, text)
				}
				if !slices.Contains(listed, note) {
					t.Errorf("no note %q in the JSON notes:\n%q", note, listed)
				}
			}

			// Exactly these two kinds of note, and no more than the case says.
			for _, claim := range []string{"loaded unevenly", "got no calls"} {
				wanted := slices.ContainsFunc(tc.want, func(n string) bool { return strings.Contains(n, claim) })
				if got := strings.Contains(text, claim); got != wanted {
					t.Errorf("text report says %q: %v, want %v:\n%s", claim, got, wanted, text)
				}
				if got := slices.ContainsFunc(listed, func(n string) bool { return strings.Contains(n, claim) }); got != wanted {
					t.Errorf("JSON notes say %q: %v, want %v:\n%q", claim, got, wanted, listed)
				}
			}
		})
	}
}
