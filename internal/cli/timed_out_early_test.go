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
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

const echoMethod = "svc.v1.Echo/Say"

// timedOutMethod is one method of 100 calls sent at 100 rps with a 1 s timeout, of which timedOut
// ended as a timeout and early of those were answered by the target itself before our deadline.
func timedOutMethod(timedOut, early int) engine.Report {
	return engine.Report{
		Sent: 100,
		Methods: []engine.MethodReport{{
			Method: echoMethod, Sent: 100, RPSLow: 100, RPSHigh: 100, Timeout: time.Second,
			TimedOut: timedOut, AnsweredEarly: early,
		}},
	}
}

// Ground: contract — «got no answer within <timeout>» is about the calls we stopped waiting for. The
// audit run (3 of 100 calls answered DEADLINE_EXCEEDED at 50 ms by the target, p99 of the same
// report an exact 50 ms) must not also say nobody answered. The early ones stay in the codes line
// as sent by the target.
func TestTimedOutNotes_AnEarlyAnswerIsNotNoAnswer(t *testing.T) {
	report := timedOutMethod(3, 3)

	if got := timedOutNotes(report); len(got) != 0 {
		t.Errorf("timedOutNotes = %q, want none: every timeout was the target's own answer", got)
	}
	if text := strings.Join(reportNotes(report, "4MB"), "\n"); strings.Contains(text, "got no answer") {
		t.Errorf("report notes say «got no answer» when the target answered every timeout:\n%s", text)
	}
}

// Ground: contract — 3 early answers and 2 calls we gave up on: the note counts the 2.
func TestTimedOutNotes_OnlyTheCallsWeStoppedWaitingForAreCounted(t *testing.T) {
	got := timedOutNotes(timedOutMethod(5, 3))

	want := []string{echoMethod + ": at 100 rps, 2 of 100 calls (2.0%) got no answer within 1.0s."}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("timedOutNotes:\n got %q\nwant %q", got, want)
	}
}

// Ground: boundary — guard. With no early answer the note is what it was, byte for byte.
func TestTimedOutNotes_WithNoEarlyAnswerTheNoteIsUnchanged(t *testing.T) {
	got := timedOutNotes(timedOutMethod(5, 0))

	want := []string{echoMethod + ": at 100 rps, 5 of 100 calls (5.0%) got no answer within 1.0s."}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("timedOutNotes:\n got %q\nwant %q", got, want)
	}
}

// Ground: contract — the other line of the note is about calls that never went out and does not
// depend on what the target answered.
func TestTimedOutNotes_UnsentTimeoutsAreStillSaidWhenEveryTimeoutWasEarly(t *testing.T) {
	report := timedOutMethod(3, 3)
	report.Methods[0].UnsentTimedOut = 2

	got := timedOutNotes(report)

	want := echoMethod + ": 2 calls timed out before going out: they waited on the connection or the\n" +
		"generator, not the target."
	if len(got) != 1 || got[0] != want {
		t.Errorf("timedOutNotes:\n got %q\nwant %q", got, want)
	}
}

// Ground: boundary — «N of them went out with less than half the timeout left» is said of the
// calls the line before it counts: never more of them than that line's calls. With no early answer
// the number is the engine's, as before.
func TestTimedOutNotes_TheLateSentCountIsNotMoreThanTheCallsCounted(t *testing.T) {
	for _, tc := range []struct {
		name         string
		timedOut     int
		early        int
		afterWait    int
		wantLateLine string
	}{
		{"no early answer", 5, 0, 4, "4 of them went out with less than half the timeout left: the target had\nthe smaller part of it."},
		{"three early of five", 5, 3, 4, "2 of them went out with less than half the timeout left: the target had\nthe smaller part of it."},
		{"three early of five, one late", 5, 3, 1, "1 of them went out with less than half the timeout left: the target had\nthe smaller part of it."},
	} {
		report := timedOutMethod(tc.timedOut, tc.early)
		report.Methods[0].TimedOutAfterWait = tc.afterWait

		got := timedOutNotes(report)
		if len(got) != 1 || !strings.HasSuffix(got[0], "\n"+tc.wantLateLine) {
			t.Errorf("%s: timedOutNotes = %q, want it to end with %q", tc.name, got, tc.wantLateLine)
		}
	}
}
