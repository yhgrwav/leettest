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
	"strconv"
	"strings"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// maxRampRows is the most rows a method's ramp block has; more seconds are
// folded into the same rows.
const maxRampRows = 20

// rampBlockNote is the one line under every ramp block.
const rampBlockNote = "failed is counted in the second a call was scheduled for; " +
	"calls still in flight when the run ended are not counted as failed"

// rampRow is one line of the ramp block: the fold of one or more seconds.
type rampRow struct {
	from, to     int
	planned      float64
	sent, failed int
}

func printRampBlocks(w io.Writer, run RunReport) {
	if run.ramp() <= 0 {
		return
	}

	for i := range run.Methods {
		printRampBlock(w, run, &run.Methods[i])
	}
}

// printRampBlock prints the seconds of the engine's window (the ramp, and the
// warm-up after it) with the rate the plan offered in each, the calls
// scheduled for it that went out and the share of those that failed. Only
// whole seconds inside the window and inside what the schedule ran: the part
// of a second after the window holds measured calls, and a run stopped inside
// the ramp ends the block where it stopped. One row is no block.
func printRampBlock(w io.Writer, run RunReport, m *engine.MethodReport) {
	whole := int(min(run.Warmup, run.SendingEnded) / time.Second)

	secs := m.Seconds[:min(max(whole, 0), len(m.Seconds))]
	if len(secs) < 2 {
		return
	}

	what := "ramp"
	if run.Warmup > run.ramp() {
		what = "ramp and warm-up"
	}

	rows := rampRows(secs)
	cells := make([][4]string, 0, len(rows)+1)
	cells = append(cells, [4]string{"at", "planned rps", "sent", "failed"})

	for _, row := range rows {
		cells = append(cells, [4]string{
			fmt.Sprintf("%d-%ds", row.from, row.to),
			strconv.FormatFloat(row.planned, 'f', 0, 64),
			strconv.Itoa(row.sent),
			rampShare(row.sent, row.failed),
		})
	}

	var width [4]int
	for _, line := range cells {
		for i, cell := range line {
			width[i] = max(width[i], len(cell))
		}
	}

	fmt.Fprintf(w, "\n%s by second, %s:\n", what, displayMethod(m.Method))

	for _, line := range cells {
		var out strings.Builder

		// The label is left-aligned, the numbers right-aligned under their names.
		out.WriteString(line[0])
		out.WriteString(strings.Repeat(" ", width[0]-len(line[0])))

		for i := 1; i < len(line); i++ {
			out.WriteString("  ")
			out.WriteString(strings.Repeat(" ", width[i]-len(line[i])))
			out.WriteString(line[i])
		}

		fmt.Fprintln(w, out.String())
	}

	fmt.Fprintln(w, rampBlockNote)
}

// rampRows folds the seconds into at most maxRampRows rows: k of them to a
// row, the last row holding whatever is left. A row's planned rate is the mean
// of its seconds' (each one whole), its sent and failed are sums, so its share
// is failed over sent of the row and not a mean of shares.
func rampRows(secs []engine.Second) []rampRow {
	perRow := (len(secs) + maxRampRows - 1) / maxRampRows
	rows := make([]rampRow, 0, (len(secs)+perRow-1)/perRow)

	for from := 0; from < len(secs); from += perRow {
		to := min(from+perRow, len(secs))
		row := rampRow{from: from, to: to}

		for i := from; i < to; i++ {
			row.planned += secs[i].PlannedRPS
			row.sent += secs[i].ScheduledSent
			row.failed += secs[i].ScheduledFailed
		}

		row.planned /= float64(to - from)
		rows = append(rows, row)
	}

	return rows
}

// rampShare is failed over sent as a whole percent, floored: a share rounded
// up could print a limit's own number on a run that is under it. Under 1% is
// <1%, none is 0%, a second with nothing sent has no share to print.
func rampShare(sent, failed int) string {
	switch {
	case sent == 0:
		return "-"
	case failed == 0:
		return "0%"
	case failed*100 < sent:
		return "<1%"
	}

	return strconv.Itoa(failed*100/sent) + "%"
}
