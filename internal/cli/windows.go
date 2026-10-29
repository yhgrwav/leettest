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
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// maxWindowRows is the most rows a method's time block has; more windows are
// folded into the same rows.
const maxWindowRows = 12

// windowRow is one line of the time block: the fold of one or more windows,
// its quantiles recomputed from their merged distributions.
type windowRow struct {
	start, width time.Duration
	sent, failed int
	latency      *metrics.Snapshot
}

func printWindowsBlocks(w io.Writer, run RunReport) {
	for i := range run.Methods {
		printWindowsBlock(w, run, &run.Methods[i])
	}
}

// printWindowsBlock prints the method's windows over time: one row for each
// fold of them, and nothing when the fold would be one row — the table above
// already says what a run of one row would.
func printWindowsBlock(w io.Writer, run RunReport, m *engine.MethodReport) {
	rows := windowRows(m.Windows)
	if len(rows) < 2 {
		return
	}

	span := "rows of " + windowRowSize(rows[0].width)
	if hadWarmup(run) {
		span += ", since the end of warm-up"
	}

	fmt.Fprintf(w, "\nover time, %s (%s):\n", displayMethod(m.Method), span)
	for _, row := range rows {
		printWindowRow(w, row)
	}
}

func hadWarmup(run RunReport) bool {
	if run.Warmup > 0 {
		return true
	}

	return run.Settings != nil && run.Settings.Warmup > 0
}

// windowRows folds neighbouring windows into at most maxWindowRows rows: k of
// them to a row, the last row holding whatever is left.
func windowRows(windows []engine.Window) []windowRow {
	if len(windows) == 0 {
		return nil
	}

	perRow := (len(windows) + maxWindowRows - 1) / maxWindowRows
	rows := make([]windowRow, 0, (len(windows)+perRow-1)/perRow)

	for i := 0; i < len(windows); i += perRow {
		end := min(i+perRow, len(windows))
		row := windowRow{start: windows[i].Start}
		snapshots := make([]*metrics.Snapshot, 0, end-i)

		for j := i; j < end; j++ {
			window := &windows[j]
			row.sent += window.Sent
			row.failed += window.Failed
			row.width += window.Width
			if window.Latency != nil {
				snapshots = append(snapshots, window.Latency)
			}
		}

		if len(snapshots) == 0 {
			row.latency = metrics.NewWindowLatencies().Snapshot()
		} else {
			row.latency = metrics.Merge(snapshots...)
		}
		rows = append(rows, row)
	}

	return rows
}

func printWindowRow(w io.Writer, row windowRow) {
	label := windowLabel(row.start, row.width)
	if row.sent == 0 {
		fmt.Fprintf(w, "%s no calls\n", label)

		return
	}

	fmt.Fprintf(w, "%s %s %s %s %s\n", label,
		windowRate(row.sent, row.width),
		windowFailedShare(row.sent, row.failed),
		windowQuantile(row.latency.Percentile(0.50)),
		windowQuantile(row.latency.Percentile(0.99)))
}

// windowLabel is +m:ss from the row's start, with a tenth when the row is not
// a whole number of seconds. The tenth is truncated: a label is a moment that
// has come, not one rounded up to.
func windowLabel(start, width time.Duration) string {
	nanos := start.Nanoseconds()
	seconds := nanos / int64(time.Second)
	minute, second := seconds/60, seconds%60

	if width%time.Second == 0 {
		return fmt.Sprintf("+%d:%02d", minute, second)
	}

	tenth := nanos % int64(time.Second) / int64(100*time.Millisecond)

	return fmt.Sprintf("+%d:%02d.%d", minute, second, tenth)
}

// windowRowSize is a row's width in seconds, to the nearest tenth, without a
// trailing .0.
func windowRowSize(width time.Duration) string {
	tenths := scaled(width.Nanoseconds(), int64(time.Second), 1)
	if tenths%10 == 0 {
		return strconv.FormatInt(tenths/10, 10) + " s"
	}

	return fixed(tenths, 1) + " s"
}

// windowRate is sent calls over the width the row covered: one decimal below
// ten, a whole number from there, and never a zero for a non-zero rate.
func windowRate(sent int, width time.Duration) string {
	if sent == 0 {
		return "0"
	}

	rate := float64(sent) / width.Seconds()
	switch {
	case rate < 0.1:
		return "<0.1"
	case rate < 10:
		return strconv.FormatFloat(rate, 'f', 1, 64)
	}

	return strconv.Itoa(int(math.Round(rate)))
}

// windowFailedShare is failed over sent, with the edges said as such: 0% and
// 100% only when true, under 1% and over 99% when the rounded share would lie.
func windowFailedShare(sent, failed int) string {
	switch {
	case failed == 0:
		return "0%"
	case failed == sent:
		return "100%"
	case failed*100 < sent:
		return "<1%"
	case failed*100 > 99*sent:
		return ">99%"
	}

	return strconv.Itoa((failed*100+sent/2)/sent) + "%"
}

// windowQuantile prints a window's latency with the two significant digits
// its histogram keeps.
func windowQuantile(q metrics.Quantile) string {
	if !q.Defined {
		return "-"
	}

	value := twoSignificant(q.Value)
	if !q.Exact {
		return ">" + trimWindowLatency(formatLatencyWith(value, scaledDown))
	}

	return trimWindowLatency(formatLatency(value))
}

// twoSignificant truncates a duration to two significant decimal digits.
func twoSignificant(d time.Duration) time.Duration {
	nanos := d.Nanoseconds()
	if nanos < 100 {
		return d
	}

	step := int64(10)
	for nanos/step >= 100 {
		step *= 10
	}

	return time.Duration(nanos / step * step)
}

// trimWindowLatency drops the third significant digit the formatter writes as
// a trailing zero, so a two-digit value does not read as three.
func trimWindowLatency(text string) string {
	dot := strings.IndexByte(text, '.')
	if dot < 0 {
		return text
	}

	end := dot + 1
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	if end == dot+1 || text[end-1] != '0' {
		return text
	}

	fraction := text[dot+1 : end-1]
	if fraction == "" {
		return text[:dot] + text[end:]
	}

	return text[:dot+1] + fraction + text[end:]
}
