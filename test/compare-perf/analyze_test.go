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

	if !v.Held || v.Arrived != 10000 || v.Planned != 10000 || v.MinSecond != 1000 {
		t.Errorf("verdict = %+v, want held with 10000 of 10000 and 1000 in the lowest second", v)
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

// A tool with 0.96 of the capacity the rate needs lags behind; a tool that
// sends late calls after the end (LeetTest does, until a call's deadline) catches
// up in the next second. Every call arrives, only after the run: ghz and k6
// stop at the end and would count as missing 4%, so must this one.
func TestJudge_ALagCaughtUpAfterTheRunEndsDoesNotHold(t *testing.T) {
	const rate, dur = 10000, 20 * time.Second

	var offs []time.Duration
	for i := range 192000 { // 0.96 of the calls, on time at 9600 a second
		offs = append(offs, time.Duration(i)*time.Second/9600)
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
	if !saved.Verdict.Held || saved.Verdict.Arrived != 5000 || saved.ReportError == "" || !got.Verdict.Held {
		t.Errorf("saved %+v, want the stand's verdict (held, 5000 arrived) and the report's error named", saved)
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
