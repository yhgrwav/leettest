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

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// point is one rate of row 1 for one tool.
type point struct {
	rate int
	runs []runOut
}

// holds reports whether more than half of the runs held.
func (p point) holds() bool {
	n := 0
	for i := range p.runs {
		if p.runs[i].Verdict.Held {
			n++
		}
	}

	return 2*n > len(p.runs)
}

// control is row 1's check of the stand: the fastest tool again with more
// cores. A tool that holds no more with them than with one is held back by
// the stand, not by itself.
type control struct {
	Tool string
	CPUs int
	// From is the highest rate the tool held with the tool cores of row 1; the
	// control's ladder starts there.
	From   int
	Points []point
}

// standLimit is the rate at which row 1 measures the stand: the control, with
// more cores, held no more than the fastest tool did with one.
func (c *control) standLimit() (rate int, limited bool) {
	if c == nil {
		return 0, false
	}
	held, _ := highestHeld(c.Points)

	return c.From, held <= c.From
}

// fastest is the tool that held the highest rate (the first of the tools on a
// tie) and the first rate it did not hold; held is zero when none held any.
func fastest(tools []string, results map[string][]point) (tool string, held, broke int) {
	for _, t := range tools {
		h, b := highestHeld(results[t])
		if h > held {
			tool, held, broke = t, h, b
		}
	}

	return tool, held, broke
}

// runPoint is one run of a tool at a rate, printed as it ends.
func (h *harness) runPoint(ctx context.Context, stdout io.Writer, tool string, rate, cpus, n int) (runOut, error) {
	s := h.f.base()
	s.Rate, s.ToolCPUs = rate, cpus

	r, err := h.run(ctx, tool, s, runLabel(1, tool, cpus, rate, n))
	if err != nil {
		return r, fmt.Errorf("row 1 %s on %d core(s) at %d rps, run %d: %w", tool, cpus, rate, n, err)
	}
	fmt.Fprintf(stdout, "row1 %-8s c%d %6d rps run %d: held %v, arrived %d of %d, lowest second %d, tool says sent %d failed %d %s\n",
		tool, cpus, rate, n, r.Verdict.Held, r.Verdict.Arrived, r.Verdict.Planned, r.Verdict.MinSecond,
		r.Reports.Sent, r.Reports.Failed, r.Reports.Note)

	return r, nil
}

// ladderStep runs every tool still climbing at one rate: the runs of the step
// go round the tools, the order shifted each time, so a drift of the machine
// falls on all of them alike.
func (h *harness) ladderStep(ctx context.Context, stdout io.Writer, active []string, step, rate int) (map[string]*point, error) {
	pts := map[string]*point{}
	for _, tool := range active {
		pts[tool] = &point{rate: rate}
	}
	for i := range h.f.runs {
		for _, tool := range rotated(active, step+i) {
			r, err := h.runPoint(ctx, stdout, tool, rate, h.f.toolCPUs, i+1)
			if err != nil {
				return nil, err
			}
			pts[tool].runs = append(pts[tool].runs, r)
		}
	}

	return pts, nil
}

// row1 climbs one ladder for all the tools together, step by step; a tool that
// does not hold a step drops out, the others go on. The control follows. On an
// error the table has the steps finished before it.
func (h *harness) row1(ctx context.Context, summary *strings.Builder, stdout io.Writer) error {
	results := map[string][]point{}
	active := slices.Clone(h.f.tools)

	var runErr error
	for step, rate := range ladder(h.f.rateStart, h.f.rateFactor, h.f.rateMax) {
		if len(active) == 0 {
			break
		}
		pts, err := h.ladderStep(ctx, stdout, active, step, rate)
		if err != nil {
			runErr = err

			break
		}
		var next []string
		for _, tool := range active {
			results[tool] = append(results[tool], *pts[tool])
			if pts[tool].holds() {
				next = append(next, tool)
			}
		}
		active = next
	}

	var ctl *control
	if runErr == nil {
		ctl, runErr = h.control(ctx, stdout, results)
	}
	row1Table(summary, h.f, results, ctl)
	if runErr != nil {
		fmt.Fprintf(summary, "Row 1 stopped by an error, the step it stopped in is not in the table (its finished runs are under runs/): %v\n\n", runErr)
	}

	return runErr
}

// control runs the fastest tool with more cores from the rate it held; none if
// no tool held a rate and then broke, or the control is off.
func (h *harness) control(ctx context.Context, stdout io.Writer, results map[string][]point) (*control, error) {
	if h.f.ctlCPUs == 0 {
		return nil, nil
	}
	tool, held, broke := fastest(h.f.tools, results)
	if held == 0 || broke == 0 {
		return nil, nil
	}

	c := &control{Tool: tool, CPUs: h.f.ctlCPUs, From: held}
	for _, rate := range ladder(h.f.rateStart, h.f.rateFactor, h.f.rateMax) {
		if rate < held {
			continue
		}
		p := point{rate: rate}
		for i := range h.f.runs {
			r, err := h.runPoint(ctx, stdout, tool, rate, c.CPUs, i+1)
			if err != nil {
				return nil, err
			}
			p.runs = append(p.runs, r)
		}
		c.Points = append(c.Points, p)
		if !p.holds() {
			break
		}
	}

	return c, nil
}

func (h *harness) row2(ctx context.Context, summary *strings.Builder, stdout io.Writer) error {
	results := map[string][]runOut{}

	var runErr error
rounds:
	for i := range h.f.runs {
		for _, tool := range rotated(h.f.tools, i) {
			s := h.f.base()
			s.Rate = h.f.fixedRate
			r, err := h.run(ctx, tool, s, runLabel(2, tool, s.ToolCPUs, i+1))
			if err == nil && r.Spacing == nil {
				err = errors.New("no arrivals in the steady seconds")
			}
			if err != nil {
				runErr = fmt.Errorf("row 2 %s run %d: %w", tool, i+1, err)

				break rounds
			}
			fmt.Fprintf(stdout, "row2 %-8s run %d: gap p99 %v (ideal %v), seconds %d..%d\n",
				tool, i+1, r.Spacing.GapP99, r.Spacing.Ideal, r.Spacing.SecMin, r.Spacing.SecMax)
			results[tool] = append(results[tool], r)
		}
	}
	row2Table(summary, h.f, results)
	noteStopped(summary, 2, runErr)

	return runErr
}

func (h *harness) row3(ctx context.Context, summary *strings.Builder, stdout io.Writer) error {
	results := map[string][]runOut{}

	var runErr error
rounds:
	for i := range h.f.runs {
		for _, tool := range rotated(h.f.tools, i) {
			s := h.f.base()
			s.Rate, s.Delay = h.f.latRate, h.f.latDelay
			r, err := h.run(ctx, tool, s, runLabel(3, tool, s.ToolCPUs, i+1))
			if err != nil {
				runErr = fmt.Errorf("row 3 %s run %d: %w", tool, i+1, err)

				break rounds
			}
			fmt.Fprintf(stdout, "row3 %-8s run %d: tool p50 %v p99 %v, stand held p50 %v p99 %v\n",
				tool, i+1, r.Reports.P50, r.Reports.P99, r.Stand.HeldP50, r.Stand.HeldP99)
			results[tool] = append(results[tool], r)
		}
	}
	row3Table(summary, h.f, results)
	noteStopped(summary, 3, runErr)

	return runErr
}

// noteStopped says under a table that an error ended its row, so tools may
// have different numbers of runs in it.
func noteStopped(w *strings.Builder, row int, err error) {
	if err != nil {
		fmt.Fprintf(w, "Row %d stopped by an error; the tools may have different numbers of runs above: %v\n\n", row, err)
	}
}
