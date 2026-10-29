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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// windowOf is a window of the engine's shape: sent calls of which failed
// failed, the measured latencies and the cut-off ones (each censored at the
// given value) in its histograms, the quantiles read from them.
func windowOf(start, width time.Duration, sent, failed int, measured, censored []time.Duration) engine.Window {
	l := metrics.NewWindowLatencies()
	for _, d := range measured {
		l.Record(d)
	}
	for _, d := range censored {
		l.RecordCensored(d)
	}

	snap := l.Snapshot()

	return engine.Window{
		Start: start, Width: width, Sent: sent, Failed: failed,
		Latency: snap, P50: snap.Percentile(0.5), P99: snap.Percentile(0.99),
	}
}

// evenWindows is n windows of width each, every one of 100 calls of 10 ms
// of which failed failed.
func evenWindows(n int, width time.Duration, failed int) []engine.Window {
	calls := make([]time.Duration, 100)
	for i := range calls {
		calls[i] = 10 * time.Millisecond
	}

	out := make([]engine.Window, n)
	for i := range out {
		out[i] = windowOf(time.Duration(i)*width, width, 100, failed, calls, nil)
	}

	return out
}

func windowedRun(warmup time.Duration, windows []engine.Window) RunReport {
	run := RunReport{Report: engine.Report{Methods: []engine.MethodReport{{Method: "/pkg.S/M", Sent: 100, Windows: windows}}}}
	if warmup > 0 {
		run.Settings = &RunSettings{Warmup: warmup, MaxInFlight: 100, Connections: 1}
	}

	return run
}

// timeBlock is one "over time" block of the text: the header
// `over time, <method> (<span>):`, the line it stands on, and the rows under
// it, a row being a line that starts with a window label (+m:ss).
type timeBlock struct {
	method string
	// span is what the parentheses of the header hold: `rows of 50 s`, with
	// `, since the end of warm-up` after it when the run had a warm-up.
	span string
	line int
	rows [][]string
}

// overTimeBlocks is every block of the text, in the order it prints them.
func overTimeBlocks(out string) []timeBlock {
	lines := strings.Split(out, "\n")

	var blocks []timeBlock
	for i, line := range lines {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "over time, ")
		if !ok {
			continue
		}

		at := strings.LastIndex(rest, " (rows of ")
		if at < 0 || !strings.HasSuffix(rest, "):") {
			continue
		}

		b := timeBlock{method: rest[:at], span: strings.TrimSuffix(rest[at+2:], "):"), line: i}
		for _, row := range lines[i+1:] {
			fields := strings.Fields(row)
			if len(fields) == 0 || !strings.HasPrefix(fields[0], "+") {
				break
			}
			b.rows = append(b.rows, fields)
		}
		blocks = append(blocks, b)
	}

	return blocks
}

// overTime is the first block of the text: what its header says in the
// parentheses, and its rows; nothing when there is no block.
func overTime(t *testing.T, out string) (span string, rows [][]string) {
	t.Helper()

	blocks := overTimeBlocks(out)
	if len(blocks) == 0 {
		return "", nil
	}

	return blocks[0].span, blocks[0].rows
}

func labels(rows [][]string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r[0]
	}

	return out
}

// Ground: contract (A3, T7) — at most 12 rows, whatever the run; the header
// names the width of a row.
func TestWindowsBlock_ShowsAtMostTwelveRowsAndNamesTheWidthOfARow(t *testing.T) {
	cases := []struct {
		n     int
		width time.Duration
		span  string
		rows  int
		first string
		last  string
	}{
		{60, 10 * time.Second, "rows of 50 s", 12, "+0:00", "+9:10"},
		{12, 10 * time.Second, "rows of 10 s", 12, "+0:00", "+1:50"},
		{13, 10 * time.Second, "rows of 20 s", 7, "+0:00", "+2:00"},
		{2, 10 * time.Second, "rows of 10 s", 2, "+0:00", "+0:10"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%d windows", c.n), func(t *testing.T) {
			span, rows := overTime(t, printedRun(windowedRun(0, evenWindows(c.n, c.width, 0))))

			if span != c.span {
				t.Errorf("header says (%s), want (%s)", span, c.span)
			}
			if len(rows) != c.rows {
				t.Fatalf("rows = %d, want %d", len(rows), c.rows)
			}
			if got := labels(rows); got[0] != c.first || got[len(got)-1] != c.last {
				t.Errorf("labels run %s .. %s, want %s .. %s", got[0], got[len(got)-1], c.first, c.last)
			}
		})
	}
}

// Ground: contract (A3) — one row is the run itself, and the table above says it.
func TestWindowsBlock_IsNotPrintedForFewerThanTwoRows(t *testing.T) {
	for _, n := range []int{0, 1} {
		out := printedRun(windowedRun(0, evenWindows(n, 10*time.Second, 0)))
		if strings.Contains(out, "over time") {
			t.Errorf("%d windows: the block is printed:\n%s", n, out)
		}
	}
}

// Ground: contract (A4) — the zero of the axis is not the start of the run
// and the text says so when there is a warm-up.
func TestWindowsBlock_SaysItCountsFromTheEndOfWarmupWhenThereIsOne(t *testing.T) {
	span, _ := overTime(t, printedRun(windowedRun(5*time.Second, evenWindows(4, 10*time.Second, 0))))
	if want := "rows of 10 s, since the end of warm-up"; span != want {
		t.Errorf("header says (%s), want (%s)", span, want)
	}

	span, rows := overTime(t, printedRun(windowedRun(0, evenWindows(4, 10*time.Second, 0))))
	if len(rows) == 0 || strings.Contains(span, "warm-up") {
		t.Errorf("header says (%s) over %d rows: with no warm-up there is a block and nothing to say about it", span, len(rows))
	}
}

// Ground: contract (A3, T7) — a merged row is the fold of its windows: its
// quantiles come from the folded histograms and its rate from the summed
// counters and widths, not from the first window, the largest or an average.
func TestWindowsBlock_AMergedRowIsRecomputedFromItsWindows(t *testing.T) {
	// 24 windows of 1 s fold in pairs. Pair 0: a fast window and a slow one;
	// the p99 of the pair is the slow window's, the p50 the fast one's.
	windows := make([]engine.Window, 24)
	fast := make([]time.Duration, 100)
	for i := range fast {
		fast[i] = 10 * time.Millisecond
	}
	for i := range windows {
		windows[i] = windowOf(time.Duration(i)*time.Second, time.Second, 10, 5, fast, nil)
	}
	windows[1] = windowOf(time.Second, time.Second, 30, 15, fast[:80], []time.Duration{900 * time.Millisecond, 900 * time.Millisecond})

	_, rows := overTime(t, printedRun(windowedRun(0, windows)))
	if len(rows) != 12 {
		t.Fatalf("rows = %d, want 12", len(rows))
	}

	// sent/s failed p50 p99
	if got, want := rows[0][1:3], []string{"20", "50%"}; got[0] != want[0] || got[1] != want[1] {
		t.Errorf("row 0 sent/s and failed = %v, want %v: 40 sent over 2 s, 20 of them failed", got, want)
	}
	if rows[0][3] != "10ms" {
		t.Errorf("row 0 p50 = %s, want 10ms", rows[0][3])
	}
	if !strings.HasPrefix(rows[0][4], ">") {
		t.Errorf("row 0 p99 = %s, want a lower bound: the cut-off calls of the second window are in the fold", rows[0][4])
	}
	if strings.HasPrefix(rows[1][4], ">") {
		t.Errorf("row 1 p99 = %s: nothing was cut off in it, a flag of its neighbour must not reach it", rows[1][4])
	}
}

// Ground: contract (A3, T7) — a cut-off call in a fold with fast ones moves
// no quantile that it cannot have moved: a one-call bound at 200 ms among 100
// calls of 10 ms leaves both quantiles at 10 ms and exact.
func TestWindowsBlock_ACensoredCallDoesNotMakeAFoldedP99ALowerBoundWhenItCannotMoveIt(t *testing.T) {
	fast := make([]time.Duration, 100)
	for i := range fast {
		fast[i] = 10 * time.Millisecond
	}
	windows := []engine.Window{
		windowOf(0, time.Second, 100, 0, fast, nil),
		windowOf(time.Second, time.Second, 1, 1, nil, []time.Duration{200 * time.Millisecond}),
	}

	_, rows := overTime(t, printedRun(windowedRun(0, windows)))
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}

	// Two windows are 12 or fewer, so no fold: the second row alone is the bound.
	if rows[0][3] != "10ms" || rows[0][4] != "10ms" {
		t.Errorf("row 0 = %v, want p50 and p99 of 10ms, exact", rows[0])
	}
	if !strings.HasPrefix(rows[1][4], ">") {
		t.Errorf("row 1 p99 = %s, want a lower bound from the one cut-off call", rows[1][4])
	}

	// 26 windows fold in threes; the first row holds the 100 fast calls and
	// the one cut-off at 200 ms. Its p99 is the 100th of 101 ranks, a fast call.
	many := make([]engine.Window, 26)
	for i := range many {
		many[i] = windowOf(time.Duration(i)*time.Second, time.Second, 0, 0, nil, nil)
	}
	many[0], many[1] = windows[0], windows[1]

	_, rows = overTime(t, printedRun(windowedRun(0, many)))
	if len(rows) == 0 || rows[0][3] != "10ms" || rows[0][4] != "10ms" {
		t.Errorf("folded rows = %v, want the first at p50 and p99 of 10ms, exact: a flag OR-ed from the windows would put a > here", rows)
	}
}

// Ground: contract (A5) — a gap in the middle is a signal.
func TestWindowsBlock_AWindowWithNoCallsPrintsNoCallsInsteadOfNumbers(t *testing.T) {
	fast := []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	windows := []engine.Window{
		windowOf(0, time.Second, 2, 0, fast, nil),
		windowOf(time.Second, time.Second, 0, 0, nil, nil),
		windowOf(2*time.Second, time.Second, 2, 0, fast, nil),
	}

	_, rows := overTime(t, printedRun(windowedRun(0, windows)))
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if got := strings.Join(rows[1], " "); got != "+0:01 no calls" {
		t.Errorf("empty row = %q, want %q", got, "+0:01 no calls")
	}
}

// Ground: contract (A2, A5) — the rate is the window's own sent over the
// width it covered; the short last window must not be divided by a full one.
func TestWindowsBlock_RateIsSentOverTheWidthTheWindowCovered(t *testing.T) {
	fast := []time.Duration{10 * time.Millisecond}
	windows := []engine.Window{
		windowOf(0, time.Second, 100, 0, fast, nil),
		windowOf(time.Second, 500*time.Millisecond, 50, 0, fast, nil),
	}

	_, rows := overTime(t, printedRun(windowedRun(0, windows)))
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][1] != "100" || rows[1][1] != "100" {
		t.Errorf("sent/s = %s and %s, want 100 and 100: 50 calls in the half second that was covered", rows[0][1], rows[1][1])
	}
}

// Ground: contract (A2) — failed share is of what was sent in the window.
func TestWindowsBlock_FailedShareIsOfTheCallsSentInTheWindow(t *testing.T) {
	windows := evenWindows(2, time.Second, 0)
	windows[1] = windowOf(time.Second, time.Second, 100, 25, []time.Duration{10 * time.Millisecond}, nil)

	_, rows := overTime(t, printedRun(windowedRun(0, windows)))
	if len(rows) != 2 || rows[0][2] != "0%" || rows[1][2] != "25%" {
		t.Errorf("failed shares = %v, want 0%% and 25%%", rows)
	}
}

// onlyColumns is the sent/s and failed cells of the one row that a run of
// two equal windows prints, each of width and holding sent calls of which
// failed failed.
func onlyColumns(t *testing.T, width time.Duration, sent, failed int) (rate, share string) {
	t.Helper()

	one := []time.Duration{10 * time.Millisecond}
	_, rows := overTime(t, printedRun(windowedRun(0, []engine.Window{
		windowOf(0, width, sent, failed, one, nil),
		windowOf(width, width, sent, failed, one, nil),
	})))
	if len(rows) != 2 || len(rows[0]) < 3 {
		t.Fatalf("rows = %v, want two rows of sent/s, failed and the quantiles", rows)
	}

	return rows[0][1], rows[0][2]
}

// Ground: contract (A2, K3) — the rate is 0 only when nothing was sent; under
// 10 it keeps a decimal, so 1 call in 2 s does not read as 0 or 1.
func TestWindowsBlock_RateKeepsADecimalUnderTenAndIsZeroOnlyWhenNothingWasSent(t *testing.T) {
	cases := []struct {
		width time.Duration
		sent  int
		want  string
	}{
		{2 * time.Second, 1, "0.5"},
		{2 * time.Second, 7, "3.5"},
		{time.Second, 3, "3.0"},
		{10 * time.Second, 99, "9.9"},
		{time.Second, 10, "10"},
		{100 * time.Second, 1234, "12"},
		{time.Second, 100, "100"},
	}
	for _, c := range cases {
		if got, _ := onlyColumns(t, c.width, c.sent, 0); got != c.want {
			t.Errorf("%d calls in %s: sent/s = %q, want %q", c.sent, c.width, got, c.want)
		}
	}

	// A rate of 0.01 is not nothing: it never prints as a zero. A row that sent
	// nothing says "no calls" (TestWindowsBlock_AWindowWithNoCallsPrintsNoCallsInsteadOfNumbers).
	if got, _ := onlyColumns(t, 100*time.Second, 1, 0); got == "0" || got == "0.0" || got == "" {
		t.Errorf("1 call in 100 s: sent/s = %q, want a mark that says it is more than nothing and under 0.1", got)
	}
}

// Ground: contract (A2, K4) — 0% only when nothing failed, 100% only when
// everything did; the edges in between are said as under 1% and over 99%.
func TestWindowsBlock_FailedShareIsZeroOrAHundredOnlyAtTheEdges(t *testing.T) {
	cases := []struct {
		sent, failed int
		want         string
	}{
		{1000, 0, "0%"},
		{1000, 1, "<1%"},
		{1000, 5, "<1%"},
		{1000, 10, "1%"},
		{1000, 250, "25%"},
		{1000, 990, "99%"},
		{1000, 995, ">99%"},
		{1000, 999, ">99%"},
		{1000, 1000, "100%"},
		{1, 0, "0%"},
		{1, 1, "100%"},
	}
	for _, c := range cases {
		if _, got := onlyColumns(t, time.Second, c.sent, c.failed); got != c.want {
			t.Errorf("%d of %d failed: share = %q, want %q", c.failed, c.sent, got, c.want)
		}
	}
}

// Ground: contract (A3, T7) — the p99 that is a lower bound is marked as in
// the totals (#73), by the ASCII sign the report already uses.
func TestWindowsBlock_ALowerBoundP99IsMarkedLikeInTheTotals(t *testing.T) {
	fast := []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	windows := []engine.Window{
		windowOf(0, time.Second, 2, 0, fast, nil),
		windowOf(time.Second, time.Second, 3, 1, fast, []time.Duration{300 * time.Millisecond}),
	}

	_, rows := overTime(t, printedRun(windowedRun(0, windows)))
	if len(rows) != 2 || strings.HasPrefix(rows[0][4], ">") || !strings.HasPrefix(rows[1][4], ">") {
		t.Errorf("p99 column = %v, want the second row alone marked >", rows)
	}
}

// Ground: contract (A7) — a window keeps two significant digits and the text
// does not print the third it does not have.
func TestWindowsBlock_PrintsTwoSignificantDigits(t *testing.T) {
	windows := []engine.Window{
		windowOf(0, time.Second, 1, 0, []time.Duration{1234 * time.Microsecond}, nil),
		windowOf(time.Second, time.Second, 1, 0, []time.Duration{12345 * time.Microsecond}, nil),
	}

	_, rows := overTime(t, printedRun(windowedRun(0, windows)))
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0][3] != "1.2ms" || rows[1][3] != "12ms" {
		t.Errorf("p50 = %s and %s, want 1.2ms and 12ms", rows[0][3], rows[1][3])
	}
}

// Ground: contract (A3) — a width that is not whole seconds needs a tenth to
// tell the rows apart.
func TestWindowsBlock_LabelsHaveATenthWhenTheWidthIsNotWholeSeconds(t *testing.T) {
	_, rows := overTime(t, printedRun(windowedRun(0, evenWindows(4, 2500*time.Millisecond, 0))))

	want := []string{"+0:00.0", "+0:02.5", "+0:05.0", "+0:07.5"}
	if got := labels(rows); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("labels = %v, want %v", got, want)
	}
}

// Ground: contract (A3, K2) — a label is a moment, so its tenth is cut off,
// never rounded up to a moment that has not come; the size of a row in the
// header is a quantity, so it is rounded to the nearest tenth. A run of 61 s
// in 60 windows of 1 016 666 667 ns, five of them to a row: 5 083 333 335 ns.
func TestWindowsBlock_ALabelTruncatesItsTenthAndTheHeaderRoundsTheRowSize(t *testing.T) {
	const width = 1016666667 * time.Nanosecond

	span, rows := overTime(t, printedRun(windowedRun(0, evenWindows(60, width, 0))))

	if want := "rows of 5.1 s"; span != want {
		t.Errorf("header says (%s), want (%s): 5.083 s to the nearest tenth", span, want)
	}

	want := []string{
		"+0:00.0", "+0:05.0", "+0:10.1", "+0:15.2", "+0:20.3", "+0:25.4",
		"+0:30.5", "+0:35.5", "+0:40.6", "+0:45.7", "+0:50.8", "+0:55.9",
	}
	if got := labels(rows); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("labels = %v, want %v", got, want)
	}

	// 3.05 s is +0:03.0, not +0:03.1; 9.15 s is +0:09.1, not +0:09.2.
	_, rows = overTime(t, printedRun(windowedRun(0, evenWindows(4, 3050*time.Millisecond, 0))))

	want = []string{"+0:00.0", "+0:03.0", "+0:06.1", "+0:09.1"}
	if got := labels(rows); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("labels of rows 3.05 s wide = %v, want %v", got, want)
	}
}

// Ground: contract (A3, K2) — the block names its method and stands after the
// table, one per method with windows, in the order of the table. What stands
// between the table and the blocks is not pinned.
func TestWindowsBlock_NamesItsMethodAndStandsAfterTheTableInTheTablesOrder(t *testing.T) {
	run := RunReport{Report: engine.Report{Methods: []engine.MethodReport{
		{Method: "/pkg.S/Zeta", Sent: 100, Windows: evenWindows(3, time.Second, 0)},
		{Method: "/pkg.S/Plain", Sent: 100},
		{Method: "/pkg.S/Alpha", Sent: 100, Windows: evenWindows(4, time.Second, 0)},
	}}}

	out := printedRun(run)
	blocks := overTimeBlocks(out)
	if len(blocks) != 2 {
		t.Fatalf("%d blocks, want 2:\n%s", len(blocks), out)
	}

	// The table spells the method without its leading slash; the header may
	// spell it either way, but it names the method of its rows.
	for i, name := range []string{"pkg.S/Zeta", "pkg.S/Alpha"} {
		if got := strings.TrimPrefix(blocks[i].method, "/"); got != name {
			t.Errorf("block %d is of %q, want %q: the blocks follow the table's order, not the alphabet", i, blocks[i].method, name)
		}
		if len(blocks[i].rows) != [2]int{3, 4}[i] {
			t.Errorf("block %d has %d rows, want %d: the rows of its own method", i, len(blocks[i].rows), [2]int{3, 4}[i])
		}
	}

	lastRow := -1
	for i, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "pkg.S/Alpha ") {
			lastRow = i
		}
	}
	if lastRow < 0 {
		t.Fatalf("no table row of the last method in:\n%s", out)
	}
	for i, b := range blocks {
		if b.line <= lastRow {
			t.Errorf("block %d (%s) stands on line %d, inside or above the table that ends on line %d", i, b.method, b.line, lastRow)
		}
	}
}

// Ground: contract (A3) — the block is per method.
func TestWindowsBlock_IsPrintedForEachMethodThatHasWindows(t *testing.T) {
	run := RunReport{Report: engine.Report{Methods: []engine.MethodReport{
		{Method: "/pkg.S/One", Sent: 100, Windows: evenWindows(3, time.Second, 0)},
		{Method: "/pkg.S/Two", Sent: 100, Windows: evenWindows(3, time.Second, 0)},
		{Method: "/pkg.S/Three", Sent: 100},
	}}}

	out := printedRun(run)
	if got := len(overTimeBlocks(out)); got != 2 {
		t.Errorf("%d blocks, want 2: one for each method with windows, none for the one without:\n%s", got, out)
	}
}

// Ground: contract (A5, T7) — all the windows, not the 12 rows of the text.
func TestWindowsJSON_CarriesEveryWindowWithItsFields(t *testing.T) {
	windows := []engine.Window{
		windowOf(0, 2500*time.Millisecond, 100, 4, []time.Duration{10 * time.Millisecond}, nil),
		windowOf(2500*time.Millisecond, 2500*time.Millisecond, 0, 0, nil, nil),
		windowOf(5*time.Second, 500*time.Millisecond, 7, 7, []time.Duration{20 * time.Millisecond}, []time.Duration{400 * time.Millisecond}),
	}
	out := writeJSON(t, jsonRun(engine.Report{Methods: []engine.MethodReport{{Method: "/pkg.S/M", Sent: 107, Windows: windows}}}))

	got, ok := field(t, field(t, out, "methods").([]any)[0], "windows").([]any)
	if !ok || len(got) != 3 {
		t.Fatalf("windows = %v, want 3", got)
	}

	for i, w := range got {
		if keys := len(w.(map[string]any)); keys != 7 {
			t.Errorf("window %d has %d keys, want the 7 of the contract", i, keys)
		}
	}

	num := func(i int, key string) float64 { return field(t, got[i], key).(float64) }
	if num(0, "start_us") != 0 || num(1, "start_us") != 2_500_000 || num(2, "start_us") != 5_000_000 {
		t.Errorf("start_us = %v, %v, %v", num(0, "start_us"), num(1, "start_us"), num(2, "start_us"))
	}
	if num(0, "width_us") != 2_500_000 || num(2, "width_us") != 500_000 {
		t.Errorf("width_us = %v and %v, want the covered widths, the last one short", num(0, "width_us"), num(2, "width_us"))
	}
	if num(0, "sent") != 100 || num(0, "failed") != 4 || num(2, "failed") != 7 {
		t.Errorf("sent and failed of window 0 = %v and %v, of window 2 failed = %v", num(0, "sent"), num(0, "failed"), num(2, "failed"))
	}

	// 2 significant digits: within 1% of what was recorded, in microseconds.
	if p := num(0, "p50_us"); p < 9_900 || p > 10_100 {
		t.Errorf("window 0 p50_us = %v, want about 10000", p)
	}
	for _, key := range []string{"p50_us", "p99_us"} {
		if v := field(t, got[1], key); v != nil {
			t.Errorf("empty window %s = %v, want null, not 0", key, v)
		}
	}
	if field(t, got[0], "p99_bound") != false || field(t, got[1], "p99_bound") != false || field(t, got[2], "p99_bound") != true {
		t.Errorf("p99_bound = %v, %v, %v; want false, false, true", field(t, got[0], "p99_bound"), field(t, got[1], "p99_bound"), field(t, got[2], "p99_bound"))
	}
}

// Ground: contract (A5) — the windows are display only; nothing but their own
// array differs in the JSON with and without them. Green on the stub.
func TestWindowsJSON_NothingButTheWindowsChangesWithThem(t *testing.T) {
	with := writeJSON(t, jsonRun(engine.Report{Methods: []engine.MethodReport{{Method: "/pkg.S/M", Sent: 100, Windows: evenWindows(3, time.Second, 50)}}}))
	without := writeJSON(t, jsonRun(engine.Report{Methods: []engine.MethodReport{{Method: "/pkg.S/M", Sent: 100}}}))

	for _, out := range []map[string]any{with, without} {
		delete(field(t, out, "methods").([]any)[0].(map[string]any), "windows")
	}
	if fmt.Sprint(with) != fmt.Sprint(without) {
		t.Errorf("JSON differs beyond the windows:\nwith:    %v\nwithout: %v", with, without)
	}
}

// Ground: contract (A5, T8) — compare_runs reads the totals of a report file;
// windows put there as garbage change neither its text nor its verdicts.
// Green on the stub: the file type already carries the field and a code that
// started to read it is what this one is for.
func TestWindowsJSON_CompareRunsDoesNotReadTheWindows(t *testing.T) {
	garbage := []jsonWindow{
		{StartUS: 0, WidthUS: 1_000_000, Sent: 999_999, Failed: 999_999, P99US: new(int64(9_999_999_999)), P99Bound: true},
		{StartUS: 1_000_000, WidthUS: 1_000_000},
	}

	compared := func(windows []jsonWindow) (string, Comparison) {
		base, now := crBase().file(t, "base.json"), crNew().file(t, "new.json")
		now.Report.Methods[0].Windows = windows

		var out bytes.Buffer
		c := CompareRuns(&out, base, now, crLimit(t, "-max-p99-rise", "10%"), crLimit(t, "-max-errors-rise", "10%"))

		return out.String(), c
	}

	wantOut, wantResult := compared(nil)
	gotOut, gotResult := compared(garbage)
	if gotOut != wantOut || gotResult != wantResult {
		t.Errorf("windows changed the comparison:\nwith:    %s %+v\nwithout: %s %+v", gotOut, gotResult, wantOut, wantResult)
	}
}

// Ground: contract (A5) — no windows is [], like the other lists. Green on
// the stub.
func TestWindowsJSON_NoWindowsIsAnEmptyArrayNotNull(t *testing.T) {
	out := writeJSON(t, jsonRun(engine.Report{Methods: []engine.MethodReport{{Method: "/pkg.S/M"}}}))

	if _, ok := field(t, field(t, out, "methods").([]any)[0], "windows").([]any); !ok {
		t.Errorf("methods[0].windows = %v, want []", field(t, field(t, out, "methods").([]any)[0], "windows"))
	}
}
