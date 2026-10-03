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
	"bytes"
	"context"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
)

var searchStart = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func testSearchModel(t *testing.T) *searchModel {
	t.Helper()

	return testSearchModelOf(t, bpPlan)
}

func testSearchModelOf(t *testing.T, plan breakpoint.Plan) *searchModel {
	t.Helper()

	base := testModel(t)
	m := newSearchModel("localhost:50051", "pkg.Svc/Do", plan, 10*time.Second, base.settings,
		NewStopper(func() {}, func() {}, func() {}, time.Hour))
	m.start = searchStart
	m.now = func() time.Time { return searchStart.Add(72 * time.Second) }
	m.base.width, m.base.height = 100, 60

	return m
}

func stepEngine(t *testing.T, rps int) *engine.Engine {
	t.Helper()

	eng, err := engine.New(engine.Options{
		Calls: []engine.Call{{Method: "/pkg.Svc/Do", Timeout: bpPlan.Timeout,
			Stages: []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: bpPlan.Hold}}}},
		Sender: engine.FakeSender{}, MaxInFlight: 1000, Warmup: bpPlan.Settle,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	return eng
}

func send(m *searchModel, msgs ...any) {
	for _, msg := range msgs {
		m.Update(msg)
	}
}

// The header says where the search is without promising what may not come:
// "of at most", a repeat names its step, a probe its number; probes and
// repeats do not move the step count.
func TestSearchView_TheHeaderSaysWhereTheSearchIs(t *testing.T) {
	m := testSearchModel(t)
	for _, tc := range []struct {
		run  breakpoint.Run
		want []string
		not  string
	}{
		{breakpoint.Run{RPS: 156, Kind: breakpoint.RateStep, Step: 3, Steps: 6}, []string{"step 3 of at most 6", "156 rps"}, ""},
		{breakpoint.Run{RPS: 305, Kind: breakpoint.Repeat, Step: 6, Steps: 6}, []string{"repeat of step 6", "305 rps"}, "step 7"},
		{breakpoint.Run{RPS: 100, Kind: breakpoint.Probe, Step: 6, Steps: 6, Probe: 2}, []string{"probe 2 of at most 5", "100 rps"}, "step 7"},
	} {
		send(m, searchRunMsg{eng: stepEngine(t, tc.run.RPS), run: tc.run})
		screen := m.View()
		for _, w := range tc.want {
			if !strings.Contains(screen, w) {
				t.Errorf("%v: no %q in\n%s", tc.run, w, screen)
			}
		}
		if tc.not != "" && strings.Contains(screen, tc.not) {
			t.Errorf("%v: %q moved the step count", tc.run, tc.not)
		}
	}
}

// The time is the elapsed time against the plan line's worst case; past it,
// it is shown as it is, not cut.
func TestSearchView_ElapsedAgainstTheWorstCase(t *testing.T) {
	m := testSearchModel(t)
	send(m, searchRunMsg{eng: stepEngine(t, 100), run: breakpoint.Run{RPS: 100, Kind: breakpoint.RateStep, Step: 1, Steps: 6}})
	_, worst, _ := strings.Cut(PlanLine(bpPlan, 10*time.Second), "at most ")
	for _, elapsed := range []time.Duration{72 * time.Second, 3 * time.Hour} {
		m.now = func() time.Time { return searchStart.Add(elapsed) }
		if want := formatDuration(elapsed) + " of at most " + worst; !strings.Contains(m.View(), want) {
			t.Errorf("no %q in\n%s", want, m.View())
		}
	}
}

// collapse makes a table row comparable whatever its column widths.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// rowsOf are the lines of out that start with a run kind, collapsed.
func rowsOf(out string) []string {
	var rows []string
	out = regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(out, "")
	for _, line := range strings.Split(out, "\n") {
		line = strings.Trim(line, " │|")
		if f := strings.Fields(line); len(f) > 0 && (f[0] == "step" || f[0] == "repeat" || f[0] == "probe") {
			rows = append(rows, collapse(line))
		}
	}

	return rows
}

// A finished run is a row of the table, the same row the text report prints
// for it: one function renders both, the verdict word from the Step.
func TestSearchView_ARowIsTheReportsRow(t *testing.T) {
	m := testSearchModel(t)
	res := outcomes["broke"].res
	for _, s := range res.Steps {
		send(m, searchStepMsg{step: s})
	}
	screen := rowsOf(m.View())
	report := rowsOf(printedSearch(res))
	if len(screen) != len(report) {
		t.Fatalf("screen rows\n%s\nreport rows\n%s", strings.Join(screen, "\n"), strings.Join(report, "\n"))
	}
	for i := range report {
		if !strings.HasPrefix(report[i], screen[i]) && screen[i] != report[i] {
			t.Errorf("row %d: screen %q, report %q", i, screen[i], report[i])
		}
	}

	notRecovered := bpRun(breakpoint.Probe, 100, 400, 45*time.Millisecond, false, breakpoint.NoCause, "")
	send(m, searchStepMsg{step: notRecovered})
	if !strings.Contains(m.View(), "not recovered") {
		t.Errorf("a probe that did not recover:\n%s", m.View())
	}
}

// During a cooldown the panel says so with a countdown and shows none of the
// last run's numbers.
func TestSearchView_ACooldownShowsNoStaleNumbers(t *testing.T) {
	m := testSearchModel(t)
	send(m, searchRunMsg{eng: stepEngine(t, 305), run: breakpoint.Run{RPS: 305, Kind: breakpoint.RateStep, Step: 6, Steps: 6}})
	m.base.snapshot.Sent = 1220
	send(m, searchCooldownMsg{d: 2 * time.Second, rps: 305})
	screen := m.View()
	if !strings.Contains(screen, "cooldown") || !strings.Contains(screen, "repeat of 305 rps") {
		t.Errorf("no cooldown line:\n%s", screen)
	}
	if strings.Contains(screen, "p50") || strings.Contains(screen, "1,220") || strings.Contains(screen, "1220") {
		t.Errorf("the last run's numbers stay on screen during the cooldown:\n%s", screen)
	}
}

// searchStates is the search screen in each of its states.
func searchStates(t *testing.T) map[string]*searchModel {
	t.Helper()

	run := func(r breakpoint.Run) *searchModel {
		m := testSearchModel(t)
		steps := outcomes["broke"].res.Steps
		for i := range 5 {
			send(m, searchStepMsg{step: steps[i]})
		}
		send(m, searchRunMsg{eng: stepEngine(t, r.RPS), run: r})

		return m
	}
	states := map[string]*searchModel{
		"step":   run(breakpoint.Run{RPS: 305, Kind: breakpoint.RateStep, Step: 6, Steps: 6}),
		"repeat": run(breakpoint.Run{RPS: 305, Kind: breakpoint.Repeat, Step: 6, Steps: 6}),
		"probe":  run(breakpoint.Run{RPS: 100, Kind: breakpoint.Probe, Step: 6, Steps: 6, Probe: 3}),
	}
	cool := run(breakpoint.Run{RPS: 305, Kind: breakpoint.RateStep, Step: 6, Steps: 6})
	send(cool, searchCooldownMsg{d: 2 * time.Second, rps: 305})
	states["cooldown"] = cool

	stopping := run(breakpoint.Run{RPS: 305, Kind: breakpoint.RateStep, Step: 6, Steps: 6})
	stopping.base.stop()
	states["stopping"] = stopping

	short := run(breakpoint.Run{RPS: 305, Kind: breakpoint.RateStep, Step: 6, Steps: 6})
	for range 30 {
		send(short, searchStepMsg{step: held(244)})
	}
	short.base.height = 30
	states["short"] = short

	final := run(breakpoint.Run{RPS: 305, Kind: breakpoint.RateStep, Step: 6, Steps: 6})
	send(final, searchDoneMsg{res: outcomes["broke"].res})
	states["final"] = final

	return states
}

// A stop is the search's: the header says the search ends with this run.
func TestSearchView_AStopEndsTheSearch(t *testing.T) {
	if s := searchStates(t)["stopping"].View(); !strings.Contains(s, "the search ends with this run") {
		t.Errorf("no stop line:\n%s", s)
	}
}

// When the table does not fit, the oldest rows go and a line counts them;
// the newest rows stay.
func TestSearchView_AShortScreenKeepsTheNewestRows(t *testing.T) {
	s := searchStates(t)["short"].View()
	if !regexp.MustCompile(regexp.QuoteMeta(ellipsis) + ` \d+ earlier runs`).MatchString(s) {
		t.Errorf("no count of the hidden rows:\n%s", s)
	}
	if rows := rowsOf(s); len(rows) == 0 || !strings.HasSuffix(rows[len(rows)-1], "held") {
		t.Errorf("the newest row is not last:\n%s", s)
	}
	if n := strings.Count(s, "\n"); n >= 30 {
		t.Errorf("%d lines on a 30-line screen", n+1)
	}
}

// The final screen is the search's report.
func TestSearchView_TheFinalScreenIsTheReport(t *testing.T) {
	s := searchStates(t)["final"].View()
	if !strings.Contains(s, outcomes["broke"].headline) {
		t.Errorf("no headline on the final screen:\n%s", s)
	}
}

// No line of any state is wider than the screen.
func TestSearchView_FitsEveryWidth(t *testing.T) {
	for name, m := range searchStates(t) {
		for _, width := range []int{40, 80, 120} {
			m.base.width = width
			for i, line := range strings.Split(m.View(), "\n") {
				if w := lipglossWidth(line); w > width {
					t.Errorf("%s at %d: line %d is %d wide", name, width, i, w)
				}
			}
		}
	}
}

// #127: the search screen draws only the glyphs checked on cmd.exe, in every
// state; the method name is the user's and is cut out first.
func TestSearchView_DrawsOnlyCheckedGlyphs(t *testing.T) {
	var bad []string
	for name, m := range searchStates(t) {
		for _, width := range []int{40, 80, 160} {
			m.base.width = width
			screen := strings.ReplaceAll(m.View(), "pkg.Svc/Do", "")
			for _, r := range badRunes(screen, liveGlyphs) {
				bad = append(bad, fmt.Sprintf("%s, width %d: %s", name, width, r))
			}
		}
		if m.View() == "" {
			bad = append(bad, name+": empty screen")
		}
	}
	if len(bad) > 0 {
		t.Errorf("unchecked characters:\n  %s", strings.Join(bad, "\n  "))
	}
}

// A screen that takes 200ms for every message delays no call: each run sends
// all it scheduled, on time — start lag p99 under 1ms, nothing left unsent.
// Counting alone would not show it: in an open model late ticks still go
// out, so a run whose schedule began before the screen let it go keeps its
// count and spoils its latency.
//
// On every host the bound is half the screen's 200ms (the flaky-test rule):
// a schedule started before the screen let the run go puts its first calls
// up to 200ms late; the host's own timer (1-2ms on Windows) stays far under.
// Linux, where the schedule is exact, also holds 1ms outright (below).
func TestSearchFeed_ASlowScreenDelaysNoCall(t *testing.T) {
	for i, lag := range searchLags(t, func(any) { time.Sleep(200 * time.Millisecond) }) {
		if lag >= 100*time.Millisecond {
			t.Errorf("run %d: start lag p99 %v with a 200ms screen: the screen delayed calls", i, lag)
		}
	}
}

// searchLags runs a short search on a fake, its screen fed through send,
// and returns each run's start lag p99, failing on any call not sent.
func searchLags(t *testing.T, send func(any)) []time.Duration {
	t.Helper()

	feed := NewSearchFeed(send)
	p := breakpoint.Plan{From: 200, To: 250, Settle: 0, Hold: 400 * time.Millisecond, Timeout: 200 * time.Millisecond}
	run := func(ctx context.Context, rps int, settle, hold time.Duration, need int) (engine.Report, error) {
		eng, err := engine.New(engine.Options{
			Calls: []engine.Call{{Method: "/pkg.Svc/Do", Timeout: p.Timeout,
				Stages: []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: hold}}}},
			Sender: engine.FakeSender{Delay: time.Millisecond}, MaxInFlight: need, Warmup: settle,
		})
		if err != nil {
			return engine.Report{}, err
		}
		feed.Starting(eng)
		err = eng.Run(ctx)

		return eng.Report(), err
	}
	res, err := breakpoint.SearchWith(t.Context(), p, run, feed.Observer())
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Steps) == 0 {
		t.Fatal("no runs")
	}
	lags := make([]time.Duration, 0, len(res.Steps))
	for i := range res.Steps {
		s := &res.Steps[i]
		r := s.Report
		if r.Sent != r.Scheduled || r.NotSent != 0 || !r.StartLagP99.Defined {
			t.Errorf("%d rps %v: sent %d of %d, not sent %d; want all, none", s.RPS, s.Kind, r.Sent, r.Scheduled, r.NotSent)
		}
		lags = append(lags, r.StartLagP99.Value)
	}

	return lags
}

// On a host whose engine schedules exactly (Linux), a slow screen leaves the
// start lag under 1ms outright.
func TestSearchFeed_ASlowScreenKeepsTheLagUnderAMillisecond(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the absolute bound holds where the schedule is exact: Linux")
	}
	for i, lag := range searchLags(t, func(any) { time.Sleep(200 * time.Millisecond) }) {
		if lag >= time.Millisecond {
			t.Errorf("run %d: start lag p99 %v, want under 1ms", i, lag)
		}
	}
}

// The next run's first frame carries none of the last run's numbers: the
// panel starts afresh with each run.
func TestSearchView_ANewRunStartsWithAFreshPanel(t *testing.T) {
	m := testSearchModel(t)
	send(m, searchRunMsg{eng: stepEngine(t, 244), run: breakpoint.Run{RPS: 244, Kind: breakpoint.RateStep, Step: 5, Steps: 6}})
	m.base.snapshot = engine.Snapshot{Sent: 98765, Failed: 4321, InFlight: 777, RPS: 243,
		P50: exact(613), P90: exact(719), P99: exact(887)}
	h := &history{}
	h.push(243, exact(613), exact(719), exact(887))
	m.base.overall = *h
	last := m.View()
	for _, n := range []string{"98,765", "777", "613", "719", "887"} {
		if !strings.Contains(last, n) {
			t.Fatalf("the fixture's number %s is not on run k's frame:\n%s", n, last)
		}
	}

	send(m, searchRunMsg{eng: stepEngine(t, 305), run: breakpoint.Run{RPS: 305, Kind: breakpoint.RateStep, Step: 6, Steps: 6}})
	first := m.View()
	for _, n := range []string{"98,765", "98765", "4,321", "4321", "777", "613", "719", "887"} {
		if strings.Contains(first, n) {
			t.Errorf("run k+1's first frame still shows %s of run k:\n%s", n, first)
		}
	}
}

// End to end without a tty: a real search on a fake feeds the screen through
// its observer, and the final screen's rows are the text report's.
func TestSearchView_ARealSearchDrivesTheScreen(t *testing.T) {
	p := breakpoint.Plan{From: 100, To: 400, Settle: 10 * time.Millisecond, Hold: 50 * time.Millisecond}
	m := testSearchModelOf(t, p)
	feed := NewSearchFeed(func(msg any) { m.Update(msg) })
	run := func(_ context.Context, rps int, _, _ time.Duration, _ int) (engine.Report, error) {
		feed.Starting(stepEngine(t, rps))
		if rps > 270 {
			return engine.Report{Sent: rps, Failed: rps / 2, Methods: []engine.MethodReport{{Method: "/pkg.Svc/Do", Sent: rps, Failed: rps / 2}}}, nil
		}

		return engine.Report{Sent: rps, Methods: []engine.MethodReport{{Method: "/pkg.Svc/Do", Sent: rps}}}, nil
	}
	res, err := breakpoint.SearchWith(t.Context(), p, run, feed.Observer())
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	m.Update(searchDoneMsg{res: res})

	var buf bytes.Buffer
	PrintBreakpoint(&buf, BreakpointRun{Method: "pkg.Svc/Do", Plan: p, Result: res})
	screen, report := rowsOf(m.View()), rowsOf(buf.String())
	if len(screen) == 0 || strings.Join(screen, "\n") != strings.Join(report, "\n") {
		t.Errorf("final screen rows\n%s\nreport rows\n%s", strings.Join(screen, "\n"), strings.Join(report, "\n"))
	}
}
