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

	"github.com/yhgrwav/leettest/pkg/engine"
)

// streamRun is oneStream's run (48 of 50 calls waited for a stream, which sets the tail) over
// several connections.
func streamRun(each ...engine.LinkReport) engine.Report {
	report := oneStream()
	report.Connections = several(nil, each...)

	return report
}

// streamVerdictOf is the stream verdict of a report, its line breaks made spaces.
func streamVerdictOf(t *testing.T, report engine.Report) string {
	t.Helper()

	v, ok := noteStarting(reportNotes(report, ""), "limited by")
	if !ok {
		t.Fatalf("no stream verdict in:\n%s", strings.Join(reportNotes(report, ""), "\n\n"))
	}

	return oneLine(v)
}

// Ground: contract — with several connections no scalar limit speaks for the run: connection 1's
// limit is not the target's, and a sum or a minimum has to be named as one. At every place the
// stream limit is told — the connections line, the verdict's heading, the in-flight bound, and
// the final screen's one-phrase verdict — all connections announced, equal: "on each" and the
// sum; different: "min to max" and the sum, by each connection's last limit; none announced: the
// "announced no limit" wording; some but not all: "announced by k of N", which says neither "no
// limit" (the tail may stand on the announced one) nor "0 streams". Mutations "the heading reads
// LastLimit" (it says "allows 0 streams") and "some announced reads as none" turn it red.
func TestReport_StreamLimitsAtN(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		each                         []engine.LinkReport
		line, heading, bound, phrase string
		after                        []string
		absent                       []string
	}{
		{
			name: "4 and 1",
			each: []engine.LinkReport{limited(addr1, 4, 4, 0), limited(addr2, 1, 1, 0)},
			line: "connections: 2; target stream limits 1 to 4 (5 in all).",
			heading: "on 2 connections the target allows 5 streams in all (MAX_CONCURRENT_STREAMS), " +
				"and 48 of 50 sent calls waited for one (p99 130ms)",
			bound:  "The target was not tested above 5 calls in flight.",
			phrase: "limited by 2 connections: target allows 5 streams in all",
		},
		{
			name: "4 and 4",
			each: []engine.LinkReport{limited(addr1, 4, 4, 0), limited(addr2, 4, 4, 0)},
			line: "connections: 2; target stream limit 4 on each (8 in all).",
			heading: "on 2 connections the target allows 8 streams in all (MAX_CONCURRENT_STREAMS), " +
				"and 48 of 50 sent calls waited for one (p99 130ms)",
			bound:  "The target was not tested above 8 calls in flight.",
			phrase: "limited by 2 connections: target allows 8 streams in all",
		},
		{
			// Some announced, some not: "no limit" would say there is none anywhere, and the
			// tail may have stood on the four streams of the connection that announced them.
			name:    "4 and none",
			each:    []engine.LinkReport{limited(addr1, 4, 4, 0), callsOn(addr2, 100, 0)},
			line:    "connections: 2; stream limit announced by 1 of 2 connections.",
			heading: "on 2 connections 48 of 50 sent calls waited for a stream (p99 130ms), and 1 of 2 connections announced a stream limit",
			phrase:  "limited by 2 connections: stream limit announced by 1 of 2",
			absent:  []string{"no stream limit announced", "announced no limit", "no limit"},
		},
		{
			name:    "none and none",
			each:    []engine.LinkReport{callsOn(addr1, 100, 0), callsOn(addr2, 100, 0)},
			line:    "connections: 2; no stream limit announced.",
			heading: "on 2 connections 48 of 50 sent calls waited for a stream (p99 130ms), and the target announced no limit",
			phrase:  "limited by 2 connections: no stream limit announced",
			absent:  []string{"stream limit announced by"},
		},
		{
			name: "equal at the end, different at the start: by the last limits",
			each: []engine.LinkReport{limited(addr1, 2, 4, 3), limited(addr2, 4, 4, 0)},
			line: "connections: 2; target stream limit 4 on each (8 in all).",
			heading: "on 2 connections the target allows 8 streams in all (MAX_CONCURRENT_STREAMS), " +
				"and 48 of 50 sent calls waited for one (p99 130ms)",
			bound:  "The target was not tested above 8 calls in flight.",
			phrase: "limited by 2 connections: target allows 8 streams in all",
			after:  []string{"connection 1: target stream limit 2 to 4, changed 3 times"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := streamRun(tc.each...)
			lines := streamLines(report)

			i := slices.Index(lines, tc.line)
			if i < 0 {
				t.Fatalf("no line %q in:\n%s", tc.line, strings.Join(lines, "\n"))
			}
			if got := lines[i+1:]; len(got) < len(tc.after) || !slices.Equal(got[:len(tc.after)], tc.after) {
				t.Errorf("after the line:\n%s\nwant first:\n%s", strings.Join(got, "\n"), strings.Join(tc.after, "\n"))
			}

			verdict := streamVerdictOf(t, report)
			if !strings.Contains(verdict, tc.heading) {
				t.Errorf("verdict:\n%s\nwant it to say %q", verdict, tc.heading)
			}
			if tc.bound != "" && !strings.Contains(verdict, tc.bound) {
				t.Errorf("verdict:\n%s\nwant it to say %q", verdict, tc.bound)
			}
			if tc.bound == "" && strings.Contains(verdict, "not tested above") {
				t.Errorf("a bound on calls in flight though a connection announced no limit:\n%s", verdict)
			}

			phrase := shortStreamVerdict(report)
			if phrase != tc.phrase {
				t.Errorf("short verdict %q, want %q", phrase, tc.phrase)
			}

			for _, said := range []string{strings.Join(lines, "\n"), verdict, phrase} {
				for _, claim := range append([]string{"limit 0", "allows 0", " 0 streams", "above 0 ", "(0 in all)"}, tc.absent...) {
					if strings.Contains(said, claim) {
						t.Errorf("%q in:\n%s", claim, said)
					}
				}
			}
		})
	}
}

// changed is a connection per number: one that changed its limit twice, from 4 to the number, or
// for 0 one that never changed it from 4.
func changed(numbers ...uint32) []engine.LinkReport {
	each := make([]engine.LinkReport, len(numbers))
	for i, n := range numbers {
		if n == 0 {
			each[i] = limited(addr1, 4, 4, 0)

			continue
		}

		each[i] = limited(addr1, 4, n, 2)
	}

	return each
}

// Ground: boundary — a connection that changed its limit gets a line of its own, numbered as the
// block numbers them (from 1) and in that order; at most five lines, so the notes stay short
// however many connections there are, then one line counting the rest (no indent: the notes
// print from the margin, as the lines above it do; "1 more connection", "1 time"). Exactly five
// has no "more" line. A connection that did not change has no line. Mutation "cap 5 -> 6"
// turns it red.
func TestReport_LimitChangeLinesCapped(t *testing.T) {
	line := func(i, last int) string {
		return fmt.Sprintf("connection %d: target stream limit 4 to %d, changed 2 times", i, last)
	}

	for _, tc := range []struct {
		name string
		each []engine.LinkReport
		head string
		want []string
	}{
		{"seven changed", changed(1, 2, 3, 4, 5, 6, 7), "connections: 7; target stream limits 1 to 7 (28 in all).", []string{
			line(1, 1), line(2, 2), line(3, 3), line(4, 4), line(5, 5),
			"and 2 more connections changed their stream limit",
		}},
		{"six changed", changed(1, 2, 3, 4, 5, 6), "connections: 6; target stream limits 1 to 6 (21 in all).", []string{
			line(1, 1), line(2, 2), line(3, 3), line(4, 4), line(5, 5),
			"and 1 more connection changed its stream limit",
		}},
		{"changed once", []engine.LinkReport{limited(addr1, 4, 3, 1), limited(addr2, 4, 4, 0)},
			"connections: 2; target stream limits 3 to 4 (7 in all).", []string{
				"connection 1: target stream limit 4 to 3, changed 1 time",
			}},
		{"exactly five", changed(1, 2, 3, 4, 5), "connections: 5; target stream limits 1 to 5 (15 in all).", []string{
			line(1, 1), line(2, 2), line(3, 3), line(4, 4), line(5, 5),
		}},
		{"two of four, the others did not change", changed(0, 2, 0, 3), "connections: 4; target stream limits 2 to 4 (13 in all).", []string{
			line(2, 2), line(4, 3),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := streamLines(streamRun(tc.each...))

			i := slices.Index(lines, tc.head)
			if i < 0 {
				t.Fatalf("no line %q in:\n%s", tc.head, strings.Join(lines, "\n"))
			}

			got := lines[i+1:]
			if len(got) < len(tc.want) || !slices.Equal(got[:len(tc.want)], tc.want) {
				t.Errorf("after the line:\n%s\nwant first:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}

			// Nothing past the end of the block says "connection N:" again.
			for _, extra := range got[min(len(tc.want), len(got)):] {
				if strings.HasPrefix(extra, "connection ") || strings.HasPrefix(extra, "and ") {
					t.Errorf("a line beyond the cap: %q", extra)
				}
			}
		})
	}
}

// waiting is n connections of limit 4 with the stream waits and unsent-for-a-stream counts given
// in pairs; the report's tail is a stream wait, which names the verdict.
func waiting(pairs ...[2]int) engine.Report {
	each := make([]engine.LinkReport, len(pairs))
	for i, p := range pairs {
		each[i] = limited(addr1, 4, 4, 0)
		each[i].StreamWaited, each[i].NotSentStream = p[0], p[1]
	}

	return streamRun(each...)
}

// Ground: contract — a tail that waited for a stream names the connections it waited on, so the
// reader sees which backend is at its limit: numbered from 1, those whose waits plus calls that
// never went out for a stream are over zero (a connection with only the second still counts),
// "connection" for one, the first five and "and <m> more" past five. Mutations "StreamWaited only"
// (connection 3 missing) and "cap 5 -> 6" turn it red.
func TestVerdict_StreamNamesTheLinks(t *testing.T) {
	none, one := [2]int{}, [2]int{0, 1}

	for _, tc := range []struct {
		name   string
		report engine.Report
		want   string
	}{
		{"two, one of them only unsent", waiting(none, [2]int{5, 0}, [2]int{0, 2}),
			"calls waited for a stream on connections 2, 3"},
		{"one", waiting(none, [2]int{0, 3}, none), "calls waited for a stream on connection 2"},
		{"exactly five", waiting(none, one, one, one, one, one, none), "calls waited for a stream on connections 2, 3, 4, 5, 6"},
		{"seven", waiting(one, one, one, one, one, one, one), "calls waited for a stream on connections 1, 2, 3, 4, 5 and 2 more"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := streamVerdictOf(t, tc.report)

			if !strings.Contains(v, tc.want) {
				t.Errorf("verdict:\n%s\nwant it to say %q", v, tc.want)
			}
			// The sentence ends there: nothing after the list names more connections.
			if _, rest, _ := strings.Cut(v, tc.want); strings.HasPrefix(rest, ",") || strings.HasPrefix(rest, " and") {
				t.Errorf("the list goes on after %q: %q", tc.want, rest)
			}
		})
	}
}

// Ground: contract — the connections are named only when the stream is among the causes of the
// tail: a verdict about the generator names no connection, however many of them waited a little.
// A guard: green before the code, the inverse of "names the connections whatever the cause".
func TestVerdict_NoLinksNamedWhenTheStreamIsNotACause(t *testing.T) {
	report := waiting([2]int{9, 0}, [2]int{4, 0})
	report.GeneratorTailCalls, report.GeneratorCauseCalls = 48, 48
	report.StreamTailCalls, report.StreamCauseCalls = 0, 0

	v := streamVerdictOf(t, report)

	if !strings.Contains(v, "generator fell behind") || strings.Contains(v, "waited for a stream on connection") {
		t.Errorf("verdict:\n%s\nwant the generator named and no connection", v)
	}
}
