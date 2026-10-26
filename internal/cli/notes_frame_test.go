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
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

var updateNotes = flag.Bool("update-notes", false, "rewrite testdata/notes from the current notes")

// allNotesReport is one run whose every note fires: warm-up, timeouts, codes
// from both sides, streams, start lag, the row notes, the cap, an incomplete
// run, the stream verdict, and the calls counted apart from the percentiles.
func allNotesReport() engine.Report {
	q := func(d time.Duration) metrics.Quantile {
		return metrics.Quantile{Value: d, Exact: true, Defined: true}
	}
	silentFrom, lastAnswerAt := 7, 6*time.Second

	return engine.Report{
		Duration: 10 * time.Second, Planned: 12 * time.Second, Incomplete: true,
		Warmup: 5 * time.Second, WarmupSent: 40, WarmupFailed: 1,
		Sent: 200, Failed: 60, NotSent: 4,
		StartLagP99: q(3 * time.Millisecond), StartLagMax: 12 * time.Millisecond,
		LateCancelMax: 7 * time.Millisecond,
		CapHit:        &engine.CapHit{At: 1200 * time.Millisecond, Unsent: 1, OverDeadline: 21},
		NotSentStream: 3, NotSentConnection: 1,
		StreamWaited: 48, StreamWaitP99: q(130 * time.Millisecond),
		StreamCauseCalls: 51, StreamTailCalls: 48,
		Connections: &engine.Connections{Open: 1, LimitAnnounced: true, FirstLimit: 1, LastLimit: 1},
		Methods: []engine.MethodReport{{
			Method: "a.B/One", Sent: 150, Failed: 60, RPSLow: 50, RPSHigh: 50,
			Timeout:  300 * time.Millisecond,
			TimedOut: 48, TimedOutAfterWait: 30, UnsentTimedOut: 4,
			SilentFrom: &silentFrom, SentRPS: 50, LastAnswerAt: &lastAnswerAt,
			SilentPlannedLow: 50, SilentPlannedHigh: 50,
			Censored: 50, Aborted: 5,
			Rejected:    refusals(10, time.Millisecond),
			Overload:    refusals(11, 2*time.Millisecond),
			Failure:     refusals(12, 3*time.Millisecond),
			BadResponse: refusals(13, 4*time.Millisecond),
			Unanswered:  2, CutOff: 3, Unclassified: 1, Invalid: 2,
			OutsideTimeline: 1,
			FailureCodes: []engine.CodeCount{
				{Code: "Unavailable", Count: 5, FromTarget: true},
				{Code: "DeadlineExceeded", Count: 3, FromTarget: false},
			},
			P50: q(10 * time.Millisecond), P90: q(20 * time.Millisecond), P95: q(30 * time.Millisecond),
			P99: q(130 * time.Millisecond), P99WithoutClientWaits: q(100 * time.Millisecond),
		}},
	}
}

// invalidNotesReport is one run whose every invalidNote branch fires: a method
// refused outright, one the client could not send, one whose replies were too
// large, one whose replies came compressed with an encoding the client does not
// accept, one with every kind of failure at once, and one that was fine.
func invalidNotesReport() engine.Report {
	const sent = 10

	return engine.Report{
		Duration:        10 * time.Second,
		Sent:            6 * sent,
		Failed:          5 * sent,
		RequestRejected: true,
		Methods: []engine.MethodReport{
			{
				Method: "inv.v1.Inv/Rejected", Sent: sent, Failed: sent,
				Rejected: refusals(sent, time.Millisecond),
			},
			{
				Method: "inv.v1.Inv/ClientError", Sent: sent, Failed: sent,
				ClientError: sent,
			},
			{
				Method: "inv.v1.Inv/Oversized", Sent: sent, Failed: sent,
				BadResponse: refusals(sent, time.Millisecond),
				FailureCodes: []engine.CodeCount{
					{Code: "ResourceExhausted", Count: sent, FromTarget: false},
				},
			},
			{
				Method: "inv.v1.Inv/Compressed", Sent: sent, Failed: sent,
				BadResponse: refusals(sent, time.Millisecond),
			},
			{
				Method: "inv.v1.Inv/Mixed", Sent: sent, Failed: sent,
				Rejected:    refusals(4, time.Millisecond),
				ClientError: 3,
				BadResponse: refusals(3, time.Millisecond),
			},
			{
				Method: "inv.v1.Inv/Fine", Sent: sent,
			},
		},
	}
}

// Ground: contract — every helper of reportNotes speaks for this report, so
// the frame below it holds a line for each of them.
func TestNotes_EveryHelperSpeaksForTheFrame(t *testing.T) {
	report := allNotesReport()
	totals := methodTotals(report)

	check := func(name string, lines []string) {
		t.Helper()
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				return
			}
		}

		t.Errorf("%s said nothing", name)
	}

	check("warmUpNote", warmUpNote(report))
	check("timedOutNotes", timedOutNotes(report))
	check("failedCodeNotes", failedCodeNotes(report))
	check("streamNotes", streamNotes(report))
	check("startLagNote", startLagNote(report))
	check("rowNotes", rowNotes(report, totals, "4MB"))
	check("capHitNote", capHitNote(report))
	check("incompleteNote", incompleteNote(report))
	check("streamVerdict", []string{streamVerdict(report)})
	check("cutOffNote", cutOffNote(totals))
	check("unansweredNote", unansweredNote(totals))
	check("outsideNote", outsideNote(totals))
	check("unclassifiedNote", unclassifiedNote(totals))
	check("invalidMeasurementNote", invalidMeasurementNote(totals))
}

// Ground: contract — the notes as they were before the next notes.go
// refactor: reportNotes over a report that makes every helper speak, in
// order, byte for byte. A change to the file is a change of the report, never
// a side effect of moving code. -update-notes rewrites it.
func TestNotes_TheFrameIsUnchanged(t *testing.T) {
	got := strings.Join(reportNotes(allNotesReport(), "4MB"), "\n\n")

	path := filepath.Join("testdata", "notes", "all.txt")
	if *updateNotes {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update-notes once)", err)
	}
	if got != string(want) {
		t.Errorf("the notes changed; first difference at byte %d", firstDiff(got, string(want)))
	}
}

// Ground: contract — the invalid-run notes as they were before the next
// notes.go refactor: reportNotes over a report that makes every invalidNote
// branch speak, with and without a max_response_size. -update-notes rewrites
// them.
func TestNotes_TheInvalidFramesAreUnchanged(t *testing.T) {
	for _, tc := range []struct {
		file        string
		maxResponse string
	}{
		{file: "invalid.txt", maxResponse: "4MB"},
		{file: "invalid-no-max.txt", maxResponse: ""},
	} {
		got := strings.Join(reportNotes(invalidNotesReport(), tc.maxResponse), "\n\n")

		path := filepath.Join("testdata", "notes", tc.file)
		if *updateNotes {
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
				t.Fatal(err)
			}

			continue
		}

		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run with -update-notes once)", err)
		}
		if got != string(want) {
			t.Errorf("the notes changed; first difference at byte %d", firstDiff(got, string(want)))
		}
	}
}
