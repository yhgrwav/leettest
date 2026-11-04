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

// rampFootnote is the one line under every ramp block, word for word.
const rampFootnote = "failed is counted in the second a call was scheduled for; " +
	"calls still in flight when the run ended are not counted as failed"

// blockSecond is one second of the fixture: the planned rate, the calls
// scheduled for it that went out and how many of them failed.
func blockSecond(planned float64, sent, failed int) engine.Second {
	return engine.Second{PlannedRPS: planned, Begun: sent, ScheduledSent: sent, ScheduledFailed: failed}
}

// blockRun is a finished run of one method (a.B/One) with the given seconds.
// The ramp and warm-up are the config's; the engine's window is both of them,
// and the schedule ran to the end of the plan unless the test says otherwise.
func blockRun(ramp, warmup time.Duration, secs ...engine.Second) RunReport {
	run := rampRun(ramp, warmup, 1200, 0)
	run.SendingEnded = run.Planned
	run.Methods[0].Seconds = secs

	return run
}

// secondsOf is n seconds of the same shape, then three seconds of the measured
// part that no ramp block may show.
func secondsOf(n int, each func(i int) engine.Second) []engine.Second {
	out := make([]engine.Second, 0, n+3)
	for i := 0; i < n; i++ {
		out = append(out, each(i))
	}
	for i := 0; i < 3; i++ {
		out = append(out, blockSecond(800, 800, 0))
	}

	return out
}

// rampBlock finds the block under header in the report's lines and returns its
// column line and rows, each split into fields, stopping at the footnote. It
// fails the test when the block is missing or has no footnote.
func rampBlock(t *testing.T, run RunReport, header string) (columns []string, rows [][]string) {
	t.Helper()

	lines := echoLines(run)
	at := slices.Index(lines, header)
	if at < 0 {
		t.Fatalf("no line %q in:\n%s", header, strings.Join(lines, "\n"))
	}
	if at == 0 || lines[at-1] != "" {
		t.Errorf("the block's header is not after a blank line:\n%s", strings.Join(lines, "\n"))
	}

	if at+1 >= len(lines) {
		t.Fatalf("the block under %q has no column line:\n%s", header, strings.Join(lines, "\n"))
	}
	columns = strings.Fields(lines[at+1])

	for _, line := range lines[at+2:] {
		if line == rampFootnote {
			return columns, rows
		}
		if line == "" {
			break
		}
		rows = append(rows, strings.Fields(line))
	}

	t.Fatalf("the block under %q does not end in the footnote %q:\n%s", header, rampFootnote, strings.Join(lines, "\n"))

	return nil, nil
}

func noRampBlock(t *testing.T, run RunReport, why string) {
	t.Helper()

	out := strings.Join(echoLines(run), "\n")
	if strings.Contains(out, "by second") || strings.Contains(out, rampFootnote) {
		t.Errorf("%s: the report has a ramp block:\n%s", why, out)
	}
}

var rampColumns = []string{"at", "planned", "rps", "sent", "failed"}

// Ground: contract — a run with a ramp shows its ramp second by second: the
// plan's rate in that second, the calls scheduled for it that went out, and
// the share of those that failed. The failed share is by the second the call
// was scheduled for, over the calls that went out in it, not by the second it
// finished, and not over the calls that began: second 2 has 150 of 500 sent
// failed (30%; of the 520 begun it would be 28%), second 3 has none scheduled
// failed though 90 timed out in it (those were scheduled earlier). The block
// stops at the ramp's end, and its header is preceded by a blank line.
// Mutations "failed over begun" and "counted by the second a call finished in"
// turn it red.
func TestReport_ARampIsShownBySecond(t *testing.T) {
	secs := secondsOf(4, func(i int) engine.Second {
		s := []engine.Second{
			blockSecond(100, 98, 0),
			blockSecond(300, 301, 0),
			blockSecond(500, 500, 150),
			blockSecond(700, 640, 0),
		}[i]
		s.Begun += []int{2, 5, 20, 160}[i]
		s.TimedOut = []int{0, 0, 0, 90}[i]

		return s
	})
	run := blockRun(4*time.Second, 0, secs...)

	columns, rows := rampBlock(t, run, "ramp by second, a.B/One:")

	if !slices.Equal(columns, rampColumns) {
		t.Errorf("columns = %q, want %q", columns, rampColumns)
	}

	want := [][]string{
		{"0-1s", "100", "98", "0%"},
		{"1-2s", "300", "301", "0%"},
		{"2-3s", "500", "500", "30%"},
		{"3-4s", "700", "640", "0%"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
	for i := range want {
		if !slices.Equal(rows[i], want[i]) {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
}

// Ground: contract — the failed share is the #380 percent rule: floored, never
// rounded up (3 of 7 is 42%, 199 of 200 is 99%), under 1% printed as <1%, none
// as 0%, all as 100%; a second with nothing sent has no share, a dash.
func TestReport_TheRampFailedShareFollowsTheFloorRule(t *testing.T) {
	cases := []struct {
		sent, failed int
		want         string
	}{
		{7, 3, "42%"},
		{200, 1, "<1%"},
		{100, 0, "0%"},
		{1, 1, "100%"},
		{200, 199, "99%"},
		{0, 0, "-"},
	}

	secs := secondsOf(len(cases), func(i int) engine.Second {
		return blockSecond(float64(100*(i+1)), cases[i].sent, cases[i].failed)
	})
	_, rows := rampBlock(t, blockRun(time.Duration(len(cases))*time.Second, 0, secs...), "ramp by second, a.B/One:")

	if len(rows) != len(cases) {
		t.Fatalf("rows = %d, want %d", len(rows), len(cases))
	}
	for i, tc := range cases {
		if got := rows[i][len(rows[i])-1]; got != tc.want {
			t.Errorf("%d of %d failed printed %q, want %q", tc.failed, tc.sent, got, tc.want)
		}
	}
}

// Ground: contract — the planned rate is printed as a whole number of calls,
// rounded to the nearest (%.0f): 18.75 is 19, 6.25 is 6, 0.4 is 0, 2999.6 is
// 3000. The fixtures above hold whole numbers, so only this one tells the
// rounding from a cut (18, 2999), an integer verb or a decimal. No value ends in
// .5: %.0f rounds a tie to even, and a test of it would pin that, not the rule.
// Mutations "truncate", "%d" and "%.1f" turn it red.
func TestReport_TheRampPlannedRateIsRoundedToWholeCalls(t *testing.T) {
	cases := []struct {
		planned float64
		want    string
	}{
		{18.75, "19"},
		{6.25, "6"},
		{0.4, "0"},
		{2999.6, "3000"},
	}

	secs := secondsOf(len(cases), func(i int) engine.Second { return blockSecond(cases[i].planned, 100, 0) })
	_, rows := rampBlock(t, blockRun(time.Duration(len(cases))*time.Second, 0, secs...), "ramp by second, a.B/One:")

	if len(rows) != len(cases) {
		t.Fatalf("rows = %d, want %d", len(rows), len(cases))
	}
	for i, tc := range cases {
		if got := rows[i][1]; got != tc.want {
			t.Errorf("planned %v printed %q, want %q", tc.planned, got, tc.want)
		}
	}
}

// Ground: V7 — with a warm-up after the ramp the window the block covers is
// both, and the header says so by the same condition as the warm-up note: the
// engine's window is longer than the ramp. A ramp alone says ramp.
func TestReport_ARampAndWarmUpBlockSaysSo(t *testing.T) {
	secs := secondsOf(5, func(i int) engine.Second { return blockSecond(float64(100*(i+1)), 100, 0) })
	run := blockRun(2*time.Second, 3*time.Second, secs...)

	_, rows := rampBlock(t, run, "ramp and warm-up by second, a.B/One:")
	if len(rows) != 5 {
		t.Errorf("rows = %d, want 5: the ramp's 2 seconds and the warm-up's 3", len(rows))
	}
	if out := strings.Join(echoLines(run), "\n"); strings.Contains(out, "\nramp by second") {
		t.Errorf("a ramp with a warm-up is headed as a ramp alone:\n%s", out)
	}
}

// Ground: boundary — at most 20 rows, each ceil(seconds/20) seconds wide, the
// last one holding what is left: 60 seconds are 20 rows of 3 s, 61 are 16 rows
// of 4 s, the last of 1 s. A row's planned rate is the mean over its seconds,
// its sent the sum, and its failed share is the sum over the sum, not the mean
// of shares: seconds of (100 sent, 0 failed), (10, 10), (10, 0) are 10 of 120,
// 8%, not 33%. Mutation "threshold 21 rows" turns it red.
func TestReport_ALongRampIsGroupedIntoTwentyRows(t *testing.T) {
	shape := func(i int) engine.Second {
		switch i % 3 {
		case 0:
			return blockSecond(float64(10*i), 100, 0)
		case 1:
			return blockSecond(float64(10*i), 10, 10)
		}

		return blockSecond(float64(10*i), 10, 0)
	}

	_, rows := rampBlock(t, blockRun(60*time.Second, 0, secondsOf(60, shape)...), "ramp by second, a.B/One:")
	if len(rows) != 20 {
		t.Fatalf("60 s: rows = %d, want 20", len(rows))
	}
	if want := []string{"0-3s", "10", "120", "8%"}; !slices.Equal(rows[0], want) {
		t.Errorf("60 s: first row = %q, want %q (mean of 0, 10 and 20; 100+10+10 sent; 10 of 120)", rows[0], want)
	}
	if want := "57-60s"; rows[19][0] != want {
		t.Errorf("60 s: last row is %q, want %q", rows[19][0], want)
	}

	_, rows = rampBlock(t, blockRun(61*time.Second, 0, secondsOf(61, shape)...), "ramp by second, a.B/One:")
	if len(rows) != 16 {
		t.Fatalf("61 s: rows = %d, want 16: width 4 s, 15 of them and a last of 1 s", len(rows))
	}
	if rows[0][0] != "0-4s" || rows[14][0] != "56-60s" || rows[15][0] != "60-61s" {
		t.Errorf("61 s: labels %q, %q, %q; want 0-4s, 56-60s, 60-61s", rows[0][0], rows[14][0], rows[15][0])
	}

	// 20 seconds or fewer: a row each.
	_, rows = rampBlock(t, blockRun(20*time.Second, 0, secondsOf(20, shape)...), "ramp by second, a.B/One:")
	if len(rows) != 20 || rows[19][0] != "19-20s" {
		t.Errorf("20 s: %d rows, the last %q; want 20 rows of 1 s", len(rows), rows[len(rows)-1][0])
	}
}

// Ground: V4 — the group's width comes from the seconds printed, not from the
// planned ramp: a 60 s ramp the run stopped at 12.3 s into prints the 12 whole
// seconds as 12 rows of 1 s, not 4 rows of 3 s.
func TestReport_AStoppedLongRampIsGroupedByWhatIsPrinted(t *testing.T) {
	secs := secondsOf(13, func(i int) engine.Second { return blockSecond(float64(10*i), 50, 0) })
	run := blockRun(60*time.Second, 0, secs...)
	run.SendingEnded = 12300 * time.Millisecond

	_, rows := rampBlock(t, run, "ramp by second, a.B/One:")

	if len(rows) != 12 {
		t.Fatalf("rows = %d, want 12 (whole seconds 0..11, 1 s each)", len(rows))
	}
	if rows[0][0] != "0-1s" || rows[11][0] != "11-12s" {
		t.Errorf("labels %q .. %q, want 0-1s .. 11-12s", rows[0][0], rows[11][0])
	}
}

// Ground: V4 — only whole seconds inside the window are printed: a ramp of
// 2.5 s has the rows 0-1s and 1-2s, and the half second after them, where the
// calls are already measured ones, is left out. A ramp that leaves one whole
// second is one row, and one row is no block.
func TestReport_ARampWithAFractionalTailShowsWholeSeconds(t *testing.T) {
	secs := secondsOf(3, func(i int) engine.Second { return blockSecond(float64(100*(i+1)), 100, 0) })

	_, rows := rampBlock(t, blockRun(2500*time.Millisecond, 0, secs...), "ramp by second, a.B/One:")
	if len(rows) != 2 || rows[0][0] != "0-1s" || rows[1][0] != "1-2s" {
		t.Errorf("rows = %q, want 0-1s and 1-2s only", rows)
	}

	noRampBlock(t, blockRun(1900*time.Millisecond, 0, secs...), "a ramp of 1.9 s has one whole second")
}

// Ground: contract — a run without a ramp, or whose ramp is under two seconds,
// has no ramp block and no footnote, however many seconds its timeline holds.
// The text of such a run is the release's: the goldens of the plain report
// (full.txt, minimal.txt) pass as they did. Mutation "the block printed with
// ramp 0" turns it red.
func TestReport_NoRampNoRampBlock(t *testing.T) {
	secs := secondsOf(5, func(i int) engine.Second { return blockSecond(800, 800, 0) })

	noRampBlock(t, blockRun(0, 2*time.Second, secs...), "no ramp, a warm-up of 2 s")
	noRampBlock(t, blockRun(0, 0, secs...), "no ramp, no warm-up")
	noRampBlock(t, blockRun(time.Second, 0, secs...), "a ramp of one second")

	noSettings := blockRun(4*time.Second, 0, secs...)
	noSettings.Settings = nil
	noRampBlock(t, noSettings, "no settings: the window is a warm-up, not a ramp")
}

// Ground: contract — one block for each method, under its own name, with its
// own seconds.
func TestReport_EachMethodHasItsOwnRampBlock(t *testing.T) {
	run := blockRun(3*time.Second, 0,
		secondsOf(3, func(i int) engine.Second { return blockSecond(float64(100*(i+1)), 10, 0) })...)
	second := run.Methods[0]
	second.Method = "a.B/Two"
	second.Seconds = secondsOf(3, func(i int) engine.Second { return blockSecond(float64(7*(i+1)), 3, 3) })
	run.Methods = append(run.Methods, second)

	_, one := rampBlock(t, run, "ramp by second, a.B/One:")
	_, two := rampBlock(t, run, "ramp by second, a.B/Two:")

	if one[2][1] != "300" || one[2][3] != "0%" {
		t.Errorf("One's last row = %q, want planned 300 and 0%%", one[2])
	}
	if two[2][1] != "21" || two[2][3] != "100%" {
		t.Errorf("Two's last row = %q, want planned 21 and 100%%", two[2])
	}
}

// Ground: contract — the block sits where the report tells of the run's time:
// after the method table and before over time, which stays as it was, and both
// before the notes that name the ramp's excluded calls.
func TestReport_TheRampBlockComesBeforeOverTime(t *testing.T) {
	secs := secondsOf(4, func(i int) engine.Second { return blockSecond(float64(100*(i+1)), 100, 0) })
	run := blockRun(4*time.Second, 0, secs...)
	run.Methods[0].Windows = []engine.Window{
		{Start: 0, Width: 10 * time.Second, Sent: 8000},
		{Start: 10 * time.Second, Width: 10 * time.Second, Sent: 8000},
	}

	lines := echoLines(run)
	table := indexOfPrefix(lines, "  plan:")
	block := slices.Index(lines, "ramp by second, a.B/One:")
	over := indexOfPrefix(lines, "over time, ")
	note := slices.Index(lines, "ramp 1200 sent, excluded from stats")

	if block < 0 || over < 0 || note < 0 || table < 0 {
		t.Fatalf("plan line %d, ramp block %d, over time %d, note %d, all must be there:\n%s",
			table, block, over, note, strings.Join(lines, "\n"))
	}
	if block >= over || over >= note {
		t.Errorf("order is block %d, over time %d, note %d; want the block, then over time, then the note", block, over, note)
	}
}

// Ground: contract — every second of the JSON timeline carries the rate the
// plan offered in it and the calls scheduled for it, additive within schema 1.
// The planned rate is a float rounded to three places at the output, so the
// same number prints on every platform; 0 is written, not left out. The
// scheduled counters are by the second a call was scheduled for.
func TestJSON_EverySecondCarriesItsPlannedRate(t *testing.T) {
	run := blockRun(4*time.Second, 0,
		blockSecond(123.4567, 10, 3),
		blockSecond(2.0/3.0, 4, 0),
		blockSecond(50.0004, 0, 0),
		engine.Second{},
	)

	out := writeJSON(t, rampJSON(run))
	method := field(t, out, "methods").([]any)[0]
	seconds := field(t, method, "seconds").([]any)

	if len(seconds) != 4 {
		t.Fatalf("seconds = %d, want 4", len(seconds))
	}

	for i, want := range []struct {
		planned      float64
		sent, failed float64
	}{
		{123.457, 10, 3},
		{0.667, 4, 0},
		{50, 0, 0},
		{0, 0, 0},
	} {
		if got := field(t, seconds[i], "planned_rps"); got != want.planned {
			t.Errorf("seconds[%d].planned_rps = %v, want %v", i, got, want.planned)
		}
		if got := field(t, seconds[i], "scheduled_sent"); got != want.sent {
			t.Errorf("seconds[%d].scheduled_sent = %v, want %v", i, got, want.sent)
		}
		if got := field(t, seconds[i], "scheduled_failed"); got != want.failed {
			t.Errorf("seconds[%d].scheduled_failed = %v, want %v", i, got, want.failed)
		}
	}
}
