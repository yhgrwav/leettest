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
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// percentileMethod is how each tool turns its samples into p50 and p99; they
// differ, so a number is never shown without it.
var percentileMethod = map[string]string{
	leettest: "HDR histogram, 3 significant digits; latency counted from the scheduled send time",
	ghz:      "nearest rank over the exact latencies of successful calls; counted from the send",
	k6:       "exact values, linear interpolation between neighbours; counted from the invoke",
}

// spread is the median of xs and its range, as "m [lo–hi]".
func spread[T cmp.Ordered](xs []T, show func(T) string) string {
	if len(xs) == 0 {
		return "n/a"
	}
	s := slices.Clone(xs)
	slices.Sort(s)

	return fmt.Sprintf("%s [%s–%s]", show(s[len(s)/2]), show(s[0]), show(s[len(s)-1]))
}

func showInt(n int) string { return fmt.Sprint(n) }

func showFloat(x float64) string { return fmt.Sprintf("%.3f", x) }

func showDur(d time.Duration) string {
	if d >= time.Millisecond {
		return d.Round(10 * time.Microsecond).String()
	}

	return d.Round(time.Microsecond).String()
}

func collect[T any, R any](xs []T, get func(T) R) []R {
	out := make([]R, len(xs))
	for i, x := range xs {
		out[i] = get(x)
	}

	return out
}

// highestHeld is the last rate of the ladder that held and the first that did
// not; zero where there is none.
func highestHeld(points []point) (held, broke int) {
	for _, p := range points {
		if p.holds() {
			held = p.rate

			continue
		}
		broke = p.rate

		break
	}

	return held, broke
}

// unparsedLabel is what a table says where the tool's own report could not be
// read; the raw output is in the run's folder.
const unparsedLabel = "не разобран"

func parsedRuns(rs []runOut) []runOut {
	return slices.DeleteFunc(slices.Clone(rs), func(r runOut) bool { return r.ReportError != "" })
}

// fromReports is a number out of the tools' own reports over the runs whose
// report was read, with the count of those that were not.
func fromReports[T cmp.Ordered](rs []runOut, get func(runOut) T, show func(T) string) string {
	ok := parsedRuns(rs)
	if len(ok) == 0 {
		return unparsedLabel
	}
	s := spread(collect(ok, get), show)
	if n := len(rs) - len(ok); n > 0 {
		s += fmt.Sprintf(" (%s: %d of %d)", unparsedLabel, n, len(rs))
	}

	return s
}

// reportCell is the table cell for whether the tool's report was read in every
// run of the row.
func reportCell(rs []runOut) string {
	if n := len(rs) - len(parsedRuns(rs)); n > 0 {
		return fmt.Sprintf("%s: %d of %d", unparsedLabel, n, len(rs))
	}

	return "read"
}

func pointRows(w *strings.Builder, points []point) {
	fmt.Fprintln(w, "| rate | runs held | arrived / planned (window: after the skipped head, before the last second) | lowest second / rate | arrivals in the skipped head | tool says sent | tool says failed | resource marks |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|")
	for _, p := range points {
		n := 0
		for i := range p.runs {
			if p.runs[i].Verdict.Held {
				n++
			}
		}
		fmt.Fprintf(w, "| %d | %d of %d | %s | %s | %s | %s | %s | %s |\n", p.rate, n, len(p.runs),
			spread(collect(p.runs, func(r runOut) float64 { return r.Verdict.Share }), showFloat),
			spread(collect(p.runs, func(r runOut) float64 { return r.Verdict.MinShare }), showFloat),
			spread(collect(p.runs, func(r runOut) int { return r.Verdict.Head }), showInt),
			fromReports(p.runs, func(r runOut) int { return r.Reports.Sent }, showInt),
			fromReports(p.runs, func(r runOut) int { return r.Reports.Failed }, showInt),
			marksCell(p.runs))
	}
	fmt.Fprintln(w)
}

// connectionsNote says what each tool is given beyond the rate: connections
// and the number of calls it may have in flight. LeetTest's cap is raised to
// pass its own start check (rate × (timeout + 100 ms) + 1 must fit); against
// a stand that answers at once it is not reached.
func connectionsNote(f flags) string {
	return fmt.Sprintf("connections: leettest 1, ghz 1, k6 one per virtual user (%d); in flight: leettest -max-in-flight "+
		"= ceil(rate × %.1f) + 2 (its start check, not reached against a stand that answers at once), ghz -c 50, k6 %d virtual users",
		f.k6VUs, (callTimeout + 100*time.Millisecond).Seconds(), f.k6VUs)
}

// row1Table writes each tool's ladder and, when the control ran, what it says
// about the stand: a tool that reached the stand's limit has that as a lower
// bound, not as its number.
func row1Table(w *strings.Builder, f flags, results map[string][]point, ctl *control) {
	limit, limited := ctl.standLimit()

	fmt.Fprintf(w, "## Row 1: rate one core holds (stand answers at once)\n\n")
	fmt.Fprintf(w, "Held: the first %v of every run, counted from the first arrival at the stand, and its last whole second are "+
		"left out for all tools; at least %.1f%% of the planned calls of the window between them reach the stand in it, and the "+
		"lowest second of it is at least %.0f%% of the rate; a point holds when more than half of its %d runs do. "+
		"Run %v each, tool container %d core(s), stand %d core(s). All tools climb the same steps together; %s. "+
		"The tools' own numbers in the tables are read from their reports; a report that could not be read is marked %q, "+
		"the verdict does not depend on it. %s\n\n",
		headSkip, heldShare*100, steadyShare*100, f.runs, f.duration, f.toolCPUs, f.standCPUs, connectionsNote(f), unparsedLabel, marksNote())

	for _, tool := range f.tools {
		held, broke := highestHeld(results[tool])
		switch {
		case limited && held >= limit:
			fmt.Fprintf(w, "### %s: holds ≥ %d rps (stand's limit)\n\n", tool, held)
		case broke == 0 && held > 0:
			fmt.Fprintf(w, "### %s: holds ≥ %d rps (the top of the ladder, it never broke)\n\n", tool, held)
		default:
			fmt.Fprintf(w, "### %s: holds %d rps, did not hold %d rps\n\n", tool, held, broke)
		}
		pointRows(w, results[tool])
	}

	switch {
	case ctl != nil:
		cHeld, _ := highestHeld(ctl.Points)
		fmt.Fprintf(w, "### Control: %s with %d core(s) instead of %d, from %d rps\n\n", ctl.Tool, ctl.CPUs, f.toolCPUs, ctl.From)
		pointRows(w, ctl.Points)
		if limited {
			fmt.Fprintf(w, "With %d cores %s holds %d rps, no more than the %d with %d: row 1 reaches the stand's limit "+
				"(≥ %d rps), not the tools' own.\n\n", ctl.CPUs, ctl.Tool, cHeld, limit, f.toolCPUs, limit)
		} else {
			fmt.Fprintf(w, "With %d cores %s holds %d rps, more than the %d with %d: the stand was not the limit at the "+
				"tools' numbers above.\n\n", ctl.CPUs, ctl.Tool, cHeld, ctl.From, f.toolCPUs)
		}
	case f.ctlCPUs != 0:
		fmt.Fprintf(w, "No control ran: none of the tools held a rate and then broke on the ladder, or the row stopped early.\n\n")
	}
}

func row2Table(w *strings.Builder, f flags, results map[string][]runOut) {
	fmt.Fprintf(w, "## Row 2: schedule accuracy at %d rps, measured at the stand\n\n", f.fixedRate)
	fmt.Fprintf(w, "Steady seconds only (the first %v and the last second of each %v run are left out); median of %d runs "+
		"and the range. Gap percentiles are nearest rank over the gaps between consecutive arrivals. %s. %s\n\n",
		headSkip, f.duration, f.runs, connectionsNote(f), marksNote())
	fmt.Fprintln(w, "| tool | ideal gap | gap p50 | gap p99 | gap p99.9 | gap max | calls in a second, lowest | highest | std dev | resource marks | tool's report |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|")
	for _, tool := range f.tools {
		rs := results[tool]
		if len(rs) == 0 {
			fmt.Fprintf(w, "| %s | no run finished |\n", tool)

			continue
		}
		sp := func(get func(spacing) time.Duration) string {
			return spread(collect(rs, func(r runOut) time.Duration { return get(*r.Spacing) }), showDur)
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", tool,
			showDur(rs[0].Spacing.Ideal),
			sp(func(s spacing) time.Duration { return s.GapP50 }),
			sp(func(s spacing) time.Duration { return s.GapP99 }),
			sp(func(s spacing) time.Duration { return s.GapP999 }),
			sp(func(s spacing) time.Duration { return s.GapMax }),
			spread(collect(rs, func(r runOut) int { return r.Spacing.SecMin }), showInt),
			spread(collect(rs, func(r runOut) int { return r.Spacing.SecMax }), showInt),
			spread(collect(rs, func(r runOut) float64 { return r.Spacing.SecStdDev }), showFloat),
			marksCell(rs), reportCell(rs))
	}
	fmt.Fprintln(w)
}

func row3Table(w *strings.Builder, f flags, results map[string][]runOut) {
	fmt.Fprintf(w, "## Row 3: the tool's own share of latency, stand answers after %v, %d rps\n\n", f.latDelay, f.latRate)
	fmt.Fprintf(w, "Added = the tool's p50 (p99) minus the stand's own hold time at the same percentile, per run; median of %d "+
		"runs and the range. The stand's time is from the call reaching its handler to the answer leaving it; the "+
		"network between the containers is in every tool's number alike. %s\n\n", f.runs, marksNote())
	fmt.Fprintln(w, "| tool | tool p50 | stand p50 | p50 added | tool p99 | stand p99 | p99 added | failed | resource marks | percentile method |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|")
	for _, tool := range f.tools {
		rs := results[tool]
		if len(rs) == 0 {
			fmt.Fprintf(w, "| %s | no run finished |\n", tool)

			continue
		}
		if len(parsedRuns(rs)) == 0 {
			fmt.Fprintf(w, "| %s | %s: the report of none of the %d runs, raw output in runs/ |\n", tool, unparsedLabel, len(rs))

			continue
		}
		// The stand's time is paired with the tool's number: the runs whose
		// report was read.
		ok := parsedRuns(rs)
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", tool,
			fromReports(rs, func(r runOut) time.Duration { return r.Reports.P50 }, showDur),
			spread(collect(ok, func(r runOut) time.Duration { return r.Stand.HeldP50 }), showDur),
			spread(collect(ok, func(r runOut) time.Duration { return r.Reports.P50 - r.Stand.HeldP50 }), showDur),
			fromReports(rs, func(r runOut) time.Duration { return r.Reports.P99 }, showDur),
			spread(collect(ok, func(r runOut) time.Duration { return r.Stand.HeldP99 }), showDur),
			spread(collect(ok, func(r runOut) time.Duration { return r.Reports.P99 - r.Stand.HeldP99 }), showDur),
			fromReports(rs, func(r runOut) int { return r.Reports.Failed }, showInt),
			marksCell(rs), percentileMethod[tool])
	}
	fmt.Fprintln(w)
}

// plan lists every setting, the commands one run is made of and how many runs
// the measurement takes at most; it is what a reader checks the numbers against.
func plan(f flags) string {
	var b strings.Builder
	s := f.base()
	s.Rate, s.Delay = f.rateStart, 0
	rates := ladder(f.rateStart, f.rateFactor, f.rateMax)

	fmt.Fprintf(&b, "# compare-perf plan\n\n")
	fmt.Fprintf(&b, "rows %v, tools %s, runs %d, duration %v, tool cores %d, stand cores %d\n",
		f.rows, strings.Join(f.tools, ","), f.runs, f.duration, f.toolCPUs, f.standCPUs)
	fmt.Fprintf(&b, "images: %s (stand, leettest, ghz), %s (build ghz %s), %s (k6)\n", f.image, f.goImage, f.ghzVersion, f.k6Image)
	fmt.Fprintf(&b, "row 1 ladder (%d steps, x%v, at most %d): %v\n", len(rates), f.rateFactor, f.rateMax, rates)
	fmt.Fprintf(&b, "row 1 order: step by step, all tools at each rate (runs go round the tools, the order shifted), "+
		"a tool that does not hold a step drops out\n")
	if f.ctlCPUs != 0 {
		fmt.Fprintf(&b, "row 1 control: after the ladder the fastest tool again with %d core(s) (GOMAXPROCS %d) from its highest held rate "+
			"up to the first it does not hold, %d run(s) each; it holds no more than with %d: row 1 is the stand's limit and "+
			"the table says so; folders row1-<tool>-c%d-<rate>-<run>\n", f.ctlCPUs, f.ctlCPUs, f.runs, f.toolCPUs, f.ctlCPUs)
	} else {
		fmt.Fprintf(&b, "row 1 control: off (-control-cpus 0), the stand's own limit is not checked\n")
	}
	fmt.Fprintf(&b, "held: the first %v of every run (from the first arrival at the stand) and its last whole second are skipped for all tools, "+
		"the arrivals in the head printed; a run holds with at least %.1f%% of the planned calls of the window between them at the "+
		"stand in it, and no second of it under %.0f%% of the rate\n",
		headSkip, heldShare*100, steadyShare*100)
	fmt.Fprintf(&b, "a tool's report that cannot be parsed is recorded in the run (result.json, raw output kept) and the row goes on; "+
		"row 1's verdict reads the stand only, rows 2 and 3 mark the run %q\n", unparsedLabel)
	fmt.Fprintf(&b, "every run: a sampler reads docker stats and the cgroup's cpu.stat of both containers about once a second "+
		"(docker exec runs a short process inside the container, counted in its CPU) and the VM's load before and after; %s\n", marksNote())
	fmt.Fprintf(&b, "row 2: %d rps; row 3: %d rps, stand delay %v\n", f.fixedRate, f.latRate, f.latDelay)
	fmt.Fprintf(&b, "tool call timeout %v; every tool: one container, --cpus=%d, GOMAXPROCS=%d, the stand's address as an IP\n",
		callTimeout, f.toolCPUs, f.toolCPUs)
	fmt.Fprintf(&b, "%s\n\n", connectionsNote(f))

	fmt.Fprintf(&b, "stand:\n  docker %s\n\n", strings.Join(standRunArgs(prefix+"-stand-N", prefix+"-net", s, "<out>/bin", "<out>/runs/<run>"), " "))
	for _, tool := range f.tools {
		args, _ := toolRunArgs(prefix+"-"+tool+"-N", prefix+"-net", tool, s, "<stand-ip>", "<out>/bin", "<out>/runs/<run>")
		fmt.Fprintf(&b, "%s:\n  docker %s\n  percentiles: %s\n", tool, strings.Join(args, " "), percentileMethod[tool])
		switch tool {
		case leettest:
			fmt.Fprintf(&b, "  /data/leettest.yaml:\n%s\n", indent(leettestConfig(s, "<stand-ip>")))
		case k6:
			fmt.Fprintf(&b, "  /data/k6.js:\n%s\n", indent(k6Script(s, "<stand-ip>")))
		default:
			fmt.Fprintln(&b)
		}
	}

	const overhead = 15 * time.Second // assumed: stand start and stop, the tool's container and start-up
	perRun := f.duration + overhead
	row1, row23 := len(f.tools)*len(rates)*f.runs, len(f.tools)*f.runs
	if f.ctlCPUs != 0 {
		row1 += len(rates) * f.runs
	}
	total := 0
	for _, r := range f.rows {
		if r == 1 {
			total += row1
		} else {
			total += row23
		}
	}
	if f.ctlCPUs != 0 && slices.Contains(f.rows, 1) {
		fmt.Fprintf(&b, "free cores needed: %d (a tool) + %d (the stand) + the harness, Docker and the host\n",
			max(f.ctlCPUs, f.toolCPUs), f.standCPUs)
	}
	fmt.Fprintf(&b, "runs at most: row 1 %d (each tool stops at its first point that does not hold; the control, if any, is in it), rows 2 and 3 %d each; "+
		"%d runs at an assumed %v each is at most %v\n", row1, row23, total, perRun, (time.Duration(total) * perRun).Round(time.Minute))

	return b.String()
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}

	return strings.Join(lines, "\n")
}
