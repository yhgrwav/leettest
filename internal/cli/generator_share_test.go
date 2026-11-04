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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// Calls the generator left unsent make the run invalid generator_late at 1% of
// a method's calls, whoever leads the wait verdict (engine.Report.
// GeneratorDecidedTail). The fixture is B 80k, run 3, of
// reviews/generator-limit-stand.md: GOMAXPROCS 2 of 4 CPUs busy 18 of 18 s,
// p99 2.000s (the timeout, a lower bound) and 1.842s without the waits, 7.9%:
// not moved; 37380 calls left unsent by the generator and 37452 by the
// connection, of 1 440 000.

// collapsedRun is that run. tail is how the p99 tail's causes read: the
// generator's calls and the connection's.
func collapsedRun(generatorTail, connectionTail int) RunReport {
	run := lateRun(lateMethod(lateA, 2*time.Second, 1842*time.Millisecond))

	m := &run.Methods[0]
	m.Sent, m.Latencies = 1365168, 1365168
	m.NotSent, m.NotSentGenerator, m.NotSentConnection = 74832, 37380, 37452

	run.Sent, run.NotSent, run.NotSentGenerator, run.NotSentConnection = m.Sent, m.NotSent, 37380, 37452
	run.GeneratorCauseCalls, run.GeneratorTailCalls = 400000, generatorTail
	run.ConnectionCauseCalls, run.ConnectionTailCalls = connectionTail, connectionTail
	run.StartLagP99, run.StartLagMax = lagQuantile(2680*time.Millisecond), 2910*time.Millisecond
	run.Passport, run.GeneratorCPU = limitPassport("linux", 4, 2, nil), cpuSeconds(2, 18, 18)

	return run
}

func TestGeneratorLate_TheGeneratorLeavingAShareUnsentIsLateWhileTheConnectionLeads(t *testing.T) {
	run := collapsedRun(165904, 37452)

	if cause, _, _ := run.WaitVerdict(); cause != "connection" {
		t.Fatalf("the fixture's wait verdict is %q, want connection (the B 80k-3 shape)", cause)
	}
	if !GeneratorLate(run) {
		t.Fatal("GeneratorLate = false: 37380 of 1440000 calls (2.6%) never left the generator")
	}
	if got, ok := GeneratorLimit(run); !ok || got != "gomaxprocs" {
		t.Errorf("GeneratorLimit = %q, %v; want gomaxprocs, true", got, ok)
	}
}

// The JSON: the reason and the limit are the run's; tail_wait_cause is the
// wait verdict's and stays the connection.
func TestGeneratorLate_TheJSONOfAShareUnsentRun(t *testing.T) {
	run := jsonRunOf(collapsedRun(165904, 37452))
	run.Outcome = OutcomeInvalid

	out := writeJSON(t, run)
	if got := anyStrings(field(t, out, "invalid_reasons")); !slices.Contains(got, "generator_late") {
		t.Errorf("invalid_reasons = %v, want generator_late in it", got)
	}
	if got := field(t, out, "generator_limit"); got != "gomaxprocs" {
		t.Errorf("generator_limit = %v, want gomaxprocs", got)
	}
	if got := field(t, out, "tail_wait_cause"); got != "connection" {
		t.Errorf("tail_wait_cause = %v, want connection: the wait verdict is not changed", got)
	}
}

// generatorShareSentence is the verdict of a run the generator left calls
// unsent in at the share, when the wait verdict does not name the generator
// (reviews/unsent-generator.md, 2026-11-04): it says what the generator did,
// not that it "decided the p99 tail", which tail_wait_cause would contradict.
// list is "<method> (<unsent by the generator> of <sent and unsent>)", joined
// by ", ".
func generatorShareSentence(run RunReport, list, limitLine string) string {
	return "invalid run: the generator fell behind its schedule and never sent 1% or more of the calls of " + list +
		": start lag p99 " + formatQuantile(run.StartLagP99) + ", max " + formatDuration(run.StartLagMax) +
		". The latencies include this machine's delay and do not measure the target. " + limitLine
}

// checkShareText holds the text of a run invalid by the share to its words:
// the sentence above, no claim that the generator decided the tail, nothing
// that puts the run down to the target's side, the short verdict the invalid
// one.
func checkShareText(t *testing.T, run RunReport, want string, notWant ...string) {
	t.Helper()

	if got := noteOf(t, run, "invalid run: the generator fell behind"); !strings.HasPrefix(got, want) {
		t.Errorf("note:\n%s\nwant it to start:\n%s", got, want)
	}

	text := flat(printedRun(run))
	if !strings.Contains(text, want) {
		t.Errorf("the printed report does not say it:\n%s", printedRun(run))
	}

	for _, bad := range append([]string{"decided the p99 tail", "limited by the run", "the connection to the target was not ready"}, notWant...) {
		if strings.Contains(text, bad) {
			t.Errorf("the printed report says %q:\n%s", bad, printedRun(run))
		}
	}

	if got, want := shortStreamVerdict(run.Report), "invalid run: the generator fell behind"; got != want {
		t.Errorf("short verdict %q, want %q", got, want)
	}
}

// The text, the connection leading the wait verdict while the generator leads
// the tail's ranking: the sentence of the share, the ranking as it is.
func TestGeneratorLate_TheReportOfAShareUnsentRunBlamesTheGenerator(t *testing.T) {
	run := collapsedRun(165904, 37452)

	checkShareText(t, run, generatorShareSentence(run, lateA+" (37380 of 1440000)", gomaxprocsLine(2, 4)))

	if text := flat(printedRun(run)); !strings.Contains(text, "causes in the p99 tail, largest first: generator late 165904; connection not ready 37452.") {
		t.Errorf("the ranking of the causes is gone:\n%s", printedRun(run))
	}
}

// The connection leading the tail as well (the printed case of the report):
// the run is the generator's by the share, in the exit code, the JSON and the
// text alike.
func TestGeneratorLate_TheShareDecidesAlsoWhenTheConnectionLeadsTheTail(t *testing.T) {
	run := collapsedRun(12000, 37452)

	if !GeneratorLate(run) {
		t.Fatal("GeneratorLate = false")
	}

	json := jsonRunOf(run)
	json.Outcome = OutcomeInvalid
	if got := anyStrings(field(t, writeJSON(t, json), "invalid_reasons")); !slices.Contains(got, "generator_late") {
		t.Errorf("invalid_reasons = %v, want generator_late in it", got)
	}

	checkShareText(t, run, generatorShareSentence(run, lateA+" (37380 of 1440000)", gomaxprocsLine(2, 4)))

	if text := flat(printedRun(run)); !strings.Contains(text, "causes in the p99 tail, largest first: connection not ready 37452; generator late 12000.") {
		t.Errorf("the ranking of the causes is gone:\n%s", printedRun(run))
	}
}

// A stream leading the unsent calls goes through other branches of the text
// (the stream heading, the lines about the connections' limits): none of them
// may stand over a run that is invalid by the share.
func TestGeneratorLate_TheShareDecidesAlsoWhenAStreamLeads(t *testing.T) {
	run := collapsedRun(12000, 0)

	m := &run.Methods[0]
	m.NotSentConnection, m.NotSentStream = 0, 37452
	run.NotSentConnection, run.NotSentStream = 0, 37452
	run.ConnectionCauseCalls, run.ConnectionTailCalls = 0, 0
	run.StreamCauseCalls, run.StreamTailCalls, run.StreamWaited = 37452, 37452, 90000
	run.Connections = &engine.Connections{
		Open: 2, LimitAnnounced: true, FirstLimit: 2, LastLimit: 2, InFlightLimit: 4, InFlightAnnounced: true,
		Each: []engine.LinkReport{{}, {}},
	}

	if cause, _, _ := run.WaitVerdict(); cause != "stream" {
		t.Fatalf("the fixture's wait verdict is %q, want stream", cause)
	}
	if !GeneratorLate(run) {
		t.Fatal("GeneratorLate = false")
	}

	checkShareText(t, run, generatorShareSentence(run, lateA+" (37380 of 1440000)", gomaxprocsLine(2, 4)),
		"waited for a stream (p99", "limited by 2 connections")

	json := jsonRunOf(run)
	json.Outcome = OutcomeInvalid
	out := writeJSON(t, json)
	if got := anyStrings(field(t, out, "invalid_reasons")); !slices.Contains(got, "generator_late") {
		t.Errorf("invalid_reasons = %v, want generator_late in it", got)
	}
	if got := field(t, out, "tail_wait_cause"); got != "stream" {
		t.Errorf("tail_wait_cause = %v, want stream: the wait verdict is not changed", got)
	}
}

// The list names the methods that left the share, by their own counts: a
// second method under the share is not in it, and its calls are not summed
// into the first's.
func TestGeneratorLate_TheShareSentenceNamesOnlyTheMethodsAtTheShare(t *testing.T) {
	run := collapsedRun(12000, 37452)

	other := lateMethod(lateB, 2*time.Second, 1842*time.Millisecond)
	other.Sent, other.Latencies, other.NotSent, other.NotSentGenerator, other.NotSentConnection = 990, 990, 10, 5, 5
	run.Methods = append(run.Methods, other)
	run.Sent += other.Sent
	run.NotSent += other.NotSent
	run.NotSentGenerator += other.NotSentGenerator
	run.NotSentConnection += other.NotSentConnection

	checkShareText(t, run, generatorShareSentence(run, lateA+" (37380 of 1440000)", gomaxprocsLine(2, 4)), lateB+" (")
}

// Under the share the run is as it was: complete, and the connection's.
func TestGeneratorLate_UnderTheShareTheConnectionStillLeads(t *testing.T) {
	run := collapsedRun(165904, 37452)

	run.Methods[0].NotSentGenerator, run.NotSentGenerator = 14399, 14399
	run.Methods[0].NotSent, run.NotSent = 14399+37452, 14399+37452
	run.Methods[0].Sent, run.Sent = 1440000-14399-37452, 1440000-14399-37452

	if GeneratorLate(run) {
		t.Error("GeneratorLate = true at 14399 of 1440000 calls, one under 1%")
	}
	if got := anyStrings(field(t, writeJSON(t, jsonRunOf(run)), "invalid_reasons")); slices.Contains(got, "generator_late") {
		t.Errorf("invalid_reasons = %v", got)
	}
}
