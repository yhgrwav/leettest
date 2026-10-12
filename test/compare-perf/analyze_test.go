package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// even is arrivals at an exactly even schedule: rate a second for dur.
func even(rate int, dur time.Duration) []time.Duration {
	n := int(float64(rate) * dur.Seconds())
	offs := make([]time.Duration, n)
	for i := range offs {
		offs[i] = time.Duration(i) * time.Second / time.Duration(rate)
	}

	return offs
}

func TestJudge_AnEvenScheduleHolds(t *testing.T) {
	v := judge(even(1000, 10*time.Second), 1000, 10*time.Second)

	if !v.Held || v.Arrived != 8000 || v.Planned != 8000 || v.Head != 2000 || v.MinSecond != 1000 {
		t.Errorf("verdict = %+v, want held with 8000 of 8000 after the 2 s head (2000 in it) and 1000 in the lowest second", v)
	}
}

// k6 connects 100 virtual users at the start, ghz and LeetTest dial: the first
// 2 s are not judged for any of them. Here the head runs at half the rate and
// everything after it is on schedule.
func TestJudge_ASlowStartInTheSkippedHeadStillHolds(t *testing.T) {
	var offs []time.Duration
	for _, o := range even(1000, 10*time.Second) {
		if o >= headSkip || (o/time.Millisecond)%2 == 0 {
			offs = append(offs, o)
		}
	}

	v := judge(offs, 1000, 10*time.Second)
	if !v.Held || v.Head != 1000 || v.Arrived != 8000 {
		t.Errorf("verdict = %+v, want held, 1000 arrivals in the head and 8000 after it", v)
	}
}

// The head is skipped, not forgiven: a lag after it is judged in full.
func TestJudge_ALagRightAfterTheSkippedHeadDoesNotHold(t *testing.T) {
	var offs []time.Duration
	for _, o := range even(1000, 10*time.Second) {
		if o >= headSkip && o < headSkip+time.Second && (o/time.Millisecond)%2 == 0 {
			continue // half of the third second is missing
		}
		offs = append(offs, o)
	}

	v := judge(offs, 1000, 10*time.Second)
	if v.Held || v.MinSecond != 500 {
		t.Errorf("verdict = %+v, want not held with 500 in the lowest second", v)
	}
}

// One second at 97 % in a minute is 0.05 % of the calls, inside heldShare, but
// below the 99 % a steady second needs.
func TestJudge_ASecondUnderNinetyNinePercentDoesNotHold(t *testing.T) {
	var offs []time.Duration
	for i, o := range even(1000, time.Minute) {
		if o >= 10*time.Second && o < 11*time.Second && i%100 < 3 {
			continue
		}
		offs = append(offs, o)
	}

	v := judge(offs, 1000, time.Minute)
	if v.Share < heldShare {
		t.Fatalf("share %.4f: the premise is that the share alone would hold", v.Share)
	}
	if v.Held || v.MinSecond != 970 {
		t.Errorf("verdict = %+v, want not held with 970 in the lowest second", v)
	}
}

func TestJudge_AFourSecondRunHasOneSteadySecond(t *testing.T) {
	v := judge(even(1000, 4*time.Second), 1000, 4*time.Second)

	if !v.Held || v.Planned != 2000 || v.MinSecond != 1000 {
		t.Errorf("verdict = %+v, want held with 2000 planned and the one steady second full", v)
	}
}

func TestJudge_CallsMissingFromThePlanDoNotHold(t *testing.T) {
	var offs []time.Duration
	for i, o := range even(1000, 10*time.Second) {
		if i%100 != 0 { // 1% short, ten times the allowed 0.1%
			offs = append(offs, o)
		}
	}

	v := judge(offs, 1000, 10*time.Second)
	if v.Held {
		t.Errorf("held with %d of %d arrived (%.4f)", v.Arrived, v.Planned, v.Share)
	}
}

func TestJudge_AllCallsArrivingButNotSteadilyDoNotHold(t *testing.T) {
	// Every call arrives in the run, but in the fourth second half of them
	// are delivered in the fifth: a tool that stalled and caught up.
	var offs []time.Duration
	for i, o := range even(1000, 10*time.Second) {
		if o >= 3*time.Second && o < 4*time.Second && i%2 == 0 {
			o += time.Second
		}
		offs = append(offs, o)
	}

	v := judge(offs, 1000, 10*time.Second)
	if v.Share < 1 {
		t.Fatalf("share = %v, the premise is that every call arrived", v.Share)
	}
	if v.Held || v.MinSecond != 500 {
		t.Errorf("verdict = %+v, want not held with 500 in the lowest second", v)
	}
}

func TestJudge_CallsArrivingAfterTheAllowanceAreNotInTheRun(t *testing.T) {
	offs := even(1000, 10*time.Second)
	for i := range offs[9000:] { // the last tenth comes 2 s late
		offs[9000+i] += 2 * time.Second
	}

	v := judge(offs, 1000, 10*time.Second)
	if v.Held || v.Arrived >= 10000 {
		t.Errorf("verdict = %+v, want the late tenth left out", v)
	}
}

// A tool that falls behind in the last second (not judged second by second)
// and sends the late calls after the end (LeetTest does, until a call's
// deadline) has every call arrive, only after the run: ghz and k6 stop at the
// end and would count as missing, so must this one.
func TestJudge_ALagCaughtUpAfterTheRunEndsDoesNotHold(t *testing.T) {
	const rate, dur = 10000, 20 * time.Second

	var offs []time.Duration
	for i := range 190000 { // on time to the 19th second
		offs = append(offs, time.Duration(i)*time.Second/rate)
	}
	for i := range 2000 { // a fifth of the last second on time
		offs = append(offs, 19*time.Second+time.Duration(i)*time.Second/2000)
	}
	for i := range 8000 { // the rest, sent in the 0.83 s after the end
		offs = append(offs, dur+time.Duration(i)*830*time.Millisecond/8000)
	}

	v := judge(offs, rate, dur)
	if v.Held {
		t.Errorf("held with %d of %d arrived by the end (%.3f), lowest second %.3f", v.Arrived, v.Planned, v.Share, v.MinShare)
	}
	if v.MinShare < steadyShare {
		t.Fatalf("lowest second %.3f: the premise is that the steady-second check alone passes", v.MinShare)
	}
}

func TestAllowanceFor_IsWhatTheHeldShareAlreadyLetsBeMissing(t *testing.T) {
	if got := allowanceFor(20 * time.Second); got < 19*time.Millisecond || got > 21*time.Millisecond {
		t.Errorf("allowance for 20s = %v, want 20ms", got)
	}
}

func TestJudge_ARunTooShortToHaveSteadySecondsNeverHolds(t *testing.T) {
	if v := judge(even(1000, 3*time.Second), 1000, 3*time.Second); v.Held {
		t.Errorf("a 3 s run has no steady second to judge, got %+v", v)
	}
}

// The tool's report failing to parse must not take the stand's numbers with it:
// row 1's verdict is read at the stand only.
func TestCollectRun_TheStandsNumbersAreSavedWhenTheToolsReportCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	var lines strings.Builder
	for _, o := range even(1000, 5*time.Second) {
		lines.WriteString(strconv.FormatInt(int64(o), 10) + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "arrivals.txt"), []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	standLog := []byte("arrivals 5000, answered 5000\nper second, from the first arrival:\n  0  1000\n")
	res := runOut{Tool: ghz, Dir: dir, Setup: setup{Rate: 1000, Duration: 5 * time.Second}}

	got, err := collectRun(res, nil, standLog, false) // no ghz.json in the folder

	if err == nil {
		t.Fatal("want the report's error")
	}
	b, readErr := os.ReadFile(filepath.Join(dir, "result.json"))
	if readErr != nil {
		t.Fatalf("no result.json: %v", readErr)
	}
	var saved runOut
	if jsonErr := json.Unmarshal(b, &saved); jsonErr != nil {
		t.Fatal(jsonErr)
	}
	if !saved.Verdict.Held || saved.Verdict.Arrived != 3000 || saved.ReportError == "" || !got.Verdict.Held {
		t.Errorf("saved %+v, want the stand's verdict (held, 3000 arrived after the head) and the report's error named", saved)
	}
}

// A tool that exits 0 without writing its report must not be given the report
// of an earlier run with the same label (the 5 s smoke run, then the real one).
func TestFreshDir_WhatAnEarlierRunLeftIsGone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runs", "row3-ghz-c1-1")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, "ghz.json")
	if err := os.WriteFile(stale, []byte(`{"count":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := freshDir(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stale); err == nil {
		t.Error("the earlier run's ghz.json is still there")
	}
	if _, err := readReport(ghz, dir, nil); err == nil {
		t.Error("a ghz run without a report was read as if it had one")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("the folder is not there: %v", err)
	}
}

// Row 2 looks at the same window as row 1: a start that bursts or crawls in the
// first 2 s is not in the gaps.
func TestMeasureSpacing_TheSkippedHeadIsNotInTheGaps(t *testing.T) {
	var offs []time.Duration
	for _, o := range even(1000, 10*time.Second) {
		if o < headSkip {
			o /= 100 // the whole head in a burst
		}
		offs = append(offs, o)
	}

	sp, err := measureSpacing(offs, 1000, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if sp.GapMax != time.Millisecond || sp.Gaps != 6999 {
		t.Errorf("gap max %v over %d gaps, want 1ms over 6999: the steady window starts at 2 s", sp.GapMax, sp.Gaps)
	}
}

func TestMeasureSpacing_AnEvenScheduleHasNoSpread(t *testing.T) {
	sp, err := measureSpacing(even(1000, 10*time.Second), 1000, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if sp.Ideal != time.Millisecond || sp.GapP50 != time.Millisecond || sp.GapP99 != time.Millisecond ||
		sp.GapMax != time.Millisecond {
		t.Errorf("gaps = %+v, want 1ms throughout", sp)
	}
	if sp.SecMin != 1000 || sp.SecMax != 1000 || sp.SecStdDev != 0 {
		t.Errorf("seconds = %d..%d std dev %v, want 1000 in each", sp.SecMin, sp.SecMax, sp.SecStdDev)
	}
}

// Bursts of ten calls at once every 10 ms have the right rate and the wrong
// schedule: only the gaps show it.
func TestMeasureSpacing_BurstsShowInTheGapsNotInTheCounts(t *testing.T) {
	var offs []time.Duration
	for at := time.Duration(0); at < 10*time.Second; at += 10 * time.Millisecond {
		for range 10 {
			offs = append(offs, at)
		}
	}

	sp, err := measureSpacing(offs, 1000, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if sp.GapP50 != 0 || sp.GapP99 != 10*time.Millisecond {
		t.Errorf("gap p50 %v p99 %v, want 0 and 10ms: nine of ten gaps are zero", sp.GapP50, sp.GapP99)
	}
	if sp.SecMin != 1000 || sp.SecMax != 1000 {
		t.Errorf("seconds = %d..%d, want 1000: the counts alone see nothing wrong", sp.SecMin, sp.SecMax)
	}
}

func TestMeasureSpacing_NoArrivalsInTheSteadySecondsIsAnError(t *testing.T) {
	if _, err := measureSpacing(even(1000, 3*time.Second), 1000, 3*time.Second); err == nil {
		t.Error("want an error, a number from nothing would read as a result")
	}
}

func TestNearestRank(t *testing.T) {
	sorted := make([]time.Duration, 100)
	for i := range sorted {
		sorted[i] = time.Duration(i+1) * time.Millisecond
	}

	tests := []struct {
		p    float64
		want time.Duration
	}{
		{0.50, 50 * time.Millisecond},
		{0.99, 99 * time.Millisecond}, // 0.99 × 100 is 99.00000000000001 in floats
		{0.999, 100 * time.Millisecond},
		{0.001, time.Millisecond},
	}
	for _, tt := range tests {
		if got := nearestRank(sorted, tt.p); got != tt.want {
			t.Errorf("p%v = %v, want %v", tt.p, got, tt.want)
		}
	}
}

func TestReadArrivals_SortsWhatTheStandWroteInArrivalOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arrivals.txt")
	if err := os.WriteFile(path, []byte("0\n3000000\n1000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readArrivals(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []time.Duration{0, time.Millisecond, 3 * time.Millisecond}; len(got) != 3 || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("arrivals = %v, want %v", got, want)
	}
}

func TestReadArrivals_RejectsALineThatIsNotAnInteger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arrivals.txt")
	if err := os.WriteFile(path, []byte("0\n1.5ms\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readArrivals(path); err == nil {
		t.Error("want an error")
	}
}

// The text is what the stand prints on exit (test/stand/cmd/stand summarize).
func TestParseStandSummary(t *testing.T) {
	text := "stand on 0.0.0.0:50051 (plaintext): grpc.health.v1.Health/Check\n" +
		"arrivals 10000, answered 9990\nper second, from the first arrival:\n  0  1000\n  1  1000\n" +
		"held, answered calls: p50 20.12ms  p99 21.5ms  max 48.2ms\n"

	s, err := parseStandSummary(text)
	if err != nil {
		t.Fatal(err)
	}

	if s.Arrivals != 10000 || s.Answered != 9990 || s.HeldP50 != 20120*time.Microsecond || s.HeldP99 != 21500*time.Microsecond {
		t.Errorf("summary = %+v", s)
	}
}

func TestParseStandSummary_NothingAnsweredHasNoHeldTime(t *testing.T) {
	s, err := parseStandSummary("arrivals 3, answered 0\nper second, from the first arrival:\n  0  3\n")
	if err != nil {
		t.Fatal(err)
	}
	if s.Arrivals != 3 || s.HeldP50 != 0 || s.HeldP99 != 0 {
		t.Errorf("summary = %+v", s)
	}
}

func TestParseStandSummary_NoSummaryIsAnError(t *testing.T) {
	if _, err := parseStandSummary("stand on 0.0.0.0:50051 (plaintext)\n"); err == nil {
		t.Error("want an error: the stand did not get to print what it saw")
	}
}
