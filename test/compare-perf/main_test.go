package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseFlags_DefaultsAreTheBriefsSetup(t *testing.T) {
	f, err := parseFlags(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	if f.toolCPUs != 1 || f.standCPUs != 4 || f.runs != 3 || f.latDelay != 20*time.Millisecond {
		t.Errorf("flags = %+v", f)
	}
	if !slices.Equal(f.tools, []string{leettest, ghz, k6}) || !slices.Equal(f.rows, []int{1, 2, 3}) {
		t.Errorf("tools %v rows %v, want all three of each", f.tools, f.rows)
	}
	if f.ghzVersion != "v0.121.0" || f.k6Image != "grafana/k6:2.3.0" {
		t.Errorf("versions %s %s, want the ones the scoreboard names", f.ghzVersion, f.k6Image)
	}
}

func TestParseFlags_HelpListsTheFlags(t *testing.T) {
	var usage bytes.Buffer

	_, err := parseFlags([]string{"-h"}, &usage)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	for _, name := range []string{"-plan", "-rows", "-tools", "-runs", "-duration", "-rate-start", "-rate-factor", "-tool-cpus", "-stand-cpus", "-control-cpus", "-k6-vus", "-keep-arrivals"} {
		if !strings.Contains(usage.String(), name) {
			t.Errorf("usage lacks %s:\n%s", name, usage.String())
		}
	}
}

func TestParseFlags_RejectsWhatCannotBeMeasured(t *testing.T) {
	for _, args := range [][]string{
		{"-duration", "3s"},
		{"-tools", "leettest,wrk"},
		{"-rows", "4"},
		{"-rows", "1,"},
		{"-rate-factor", "1"},
		{"-rate-start", "5000", "-rate-max", "1000"},
		{"-runs", "0"},
		{"-latency-delay", "0s"},
		{"-control-cpus", "1"}, // not more than a tool's own cores: it controls nothing
	} {
		if _, err := parseFlags(args, io.Discard); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestLadder(t *testing.T) {
	got := ladder(1000, 1.25, 3000)
	if want := []int{1000, 1250, 1550, 1950, 2450}; !slices.Equal(got, want) {
		t.Errorf("ladder = %v, want %v", got, want)
	}

	// A factor that rounds to no step still climbs.
	got = ladder(100, 1.01, 300)
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("ladder %v does not climb at %d", got, i)
		}
	}
}

func TestPointHolds_NeedsMoreThanHalfOfTheRuns(t *testing.T) {
	run := func(held bool) runOut { return runOut{Verdict: verdict{Held: held}} }

	tests := []struct {
		runs []runOut
		want bool
	}{
		{[]runOut{run(true), run(true), run(false)}, true},
		{[]runOut{run(true), run(false), run(false)}, false},
		{[]runOut{run(true), run(false)}, false},
		{[]runOut{run(true)}, true},
	}
	for _, tt := range tests {
		if got := (point{runs: tt.runs}).holds(); got != tt.want {
			t.Errorf("%d held of %d: holds = %v, want %v", countHeld(tt.runs), len(tt.runs), got, tt.want)
		}
	}
}

func countHeld(rs []runOut) int {
	n := 0
	for i := range rs {
		if rs[i].Verdict.Held {
			n++
		}
	}

	return n
}

func TestHighestHeld(t *testing.T) {
	p := func(rate int, held bool) point {
		return point{rate: rate, runs: []runOut{{Verdict: verdict{Held: held}}}}
	}

	held, broke := highestHeld([]point{p(1000, true), p(1250, true), p(1550, false)})
	if held != 1250 || broke != 1550 {
		t.Errorf("held %d broke %d, want 1250 and 1550", held, broke)
	}

	// A ladder that never broke has no breaking rate to report.
	held, broke = highestHeld([]point{p(1000, true), p(1250, true)})
	if held != 1250 || broke != 0 {
		t.Errorf("held %d broke %d, want 1250 and 0", held, broke)
	}
}

func TestRotated_NoToolAlwaysFirst(t *testing.T) {
	tools := []string{"a", "b", "c"}
	first := map[string]bool{}
	for i := range 3 {
		got := rotated(tools, i)
		if len(got) != 3 {
			t.Fatalf("rotated = %v", got)
		}
		first[got[0]] = true
	}
	if len(first) != 3 {
		t.Errorf("firsts = %v, want each tool first once in three runs", first)
	}
	if !slices.Equal(tools, []string{"a", "b", "c"}) {
		t.Errorf("the input was changed: %v", tools)
	}
}

func TestRow3Table_AddedIsTheToolMinusTheStandAtTheSamePercentile(t *testing.T) {
	f, _ := parseFlags([]string{"-tools", "ghz"}, io.Discard)
	rs := []runOut{{
		Reports: toolResult{P50: 21500 * time.Microsecond, P99: 30 * time.Millisecond},
		Stand:   standSummary{HeldP50: 20100 * time.Microsecond, HeldP99: 20800 * time.Microsecond},
	}}

	var b strings.Builder
	row3Table(&b, f, map[string][]runOut{ghz: rs})

	// 21.5 - 20.1 = 1.4ms at p50, 30 - 20.8 = 9.2ms at p99
	for _, want := range []string{"1.4ms", "9.2ms", "nearest rank"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("table lacks %q:\n%s", want, b.String())
		}
	}
}

func TestRow1Table_NamesWhereEachToolStoppedHolding(t *testing.T) {
	f, _ := parseFlags([]string{"-tools", "k6"}, io.Discard)
	p := func(rate int, held bool) point {
		return point{rate: rate, runs: []runOut{{Verdict: verdict{Held: held, Share: 1}}}}
	}

	var b strings.Builder
	row1Table(&b, f, map[string][]point{k6: {p(1000, true), p(1250, false)}}, nil)

	if !strings.Contains(b.String(), "k6: holds 1000 rps, did not hold 1250 rps") {
		t.Errorf("table:\n%s", b.String())
	}
}

func TestSpread(t *testing.T) {
	got := spread([]int{5, 1, 3}, showInt)
	if got != "3 [1–5]" {
		t.Errorf("spread = %q, want the median and the range", got)
	}
	if spread(nil, showInt) != "n/a" {
		t.Error("nothing to spread should say so")
	}
}

// -plan is how a reader sees every setting before anything runs; it must need
// neither Docker nor the tools.
func TestRun_PlanPrintsEverySettingAndRunsNothing(t *testing.T) {
	var out, errOut bytes.Buffer

	if err := run(context.Background(), []string{"-plan"}, &out, &errOut); err != nil {
		t.Fatalf("run -plan: %v", err)
	}

	for _, want := range []string{
		"--cpus=1", "GOMAXPROCS=1", "--cpus=4", "/perf/leettest", "/perf/ghz", "--async", "grafana/k6:2.3.0",
		"constant-arrival-rate", "ladder", "percentiles:", "runs at most",
		"row 1 control: after the ladder the fastest tool again with 4 core(s)", "row1-<tool>-c4-",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan lacks %q", want)
		}
	}
}

// What each tool is given beyond the rate is printed with its number, LeetTest's
// raised in-flight cap like k6's virtual users and ghz's -c; so is the head
// skipped for all of them.
func TestPlanAndTables_PrintTheInFlightSettingsAndTheSkippedHead(t *testing.T) {
	f, _ := parseFlags(nil, io.Discard)

	var b strings.Builder
	row1Table(&b, f, map[string][]point{}, nil)
	row2Table(&b, f, map[string][]runOut{})

	for name, text := range map[string]string{"plan": plan(f), "tables": b.String()} {
		for _, want := range []string{"leettest -max-in-flight = ceil(rate × 5.1) + 2", "ghz -c 50", "k6 100 virtual users", "first 2s"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lack %q", name, want)
			}
		}
	}
}

func TestPlan_ControlOffIsSaid(t *testing.T) {
	f, _ := parseFlags([]string{"-control-cpus", "0"}, io.Discard)

	text := plan(f)
	if !strings.Contains(text, "control: off") || strings.Contains(text, "after the ladder") {
		t.Errorf("plan:\n%s", text)
	}
}

func TestPlan_OnlyTheChosenToolsAreListed(t *testing.T) {
	f, _ := parseFlags([]string{"-tools", "ghz", "-rows", "2"}, io.Discard)

	text := plan(f)
	if strings.Contains(text, "/perf/leettest") || strings.Contains(text, "constant-arrival-rate") {
		t.Errorf("plan lists tools that are not chosen:\n%s", text)
	}
	if !strings.Contains(text, "/perf/ghz") {
		t.Errorf("plan lacks ghz:\n%s", text)
	}
}
