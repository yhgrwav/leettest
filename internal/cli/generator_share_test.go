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

// The text: the first line keeps its words, the run is not put down to the
// target's connection, the short verdict is the invalid one.
func TestGeneratorLate_TheReportOfAShareUnsentRunBlamesTheGenerator(t *testing.T) {
	run := collapsedRun(165904, 37452)

	want := generatorLateSentenceWith(run, lateA, gomaxprocsLine(2, 4))
	if got := noteOf(t, run, "invalid run: the generator fell behind"); !strings.HasPrefix(got, want) {
		t.Errorf("note:\n%s\nwant it to start:\n%s", got, want)
	}

	text := flat(printedRun(run))
	if !strings.Contains(text, want) {
		t.Errorf("the printed report does not say it:\n%s", printedRun(run))
	}
	if strings.Contains(text, "the connection to the target was not ready") {
		t.Errorf("the run is put down to the target's connection:\n%s", printedRun(run))
	}
	if got, want := shortStreamVerdict(run.Report), "invalid run: the generator fell behind"; got != want {
		t.Errorf("short verdict %q, want %q", got, want)
	}
}

// The connection leading the tail as well: the run is still the generator's by
// the share, exit code and JSON go by the one predicate. (How the text reads
// then is for the reviewer; see the report.)
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
