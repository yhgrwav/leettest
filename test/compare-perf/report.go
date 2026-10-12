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

func row1Table(w *strings.Builder, f flags, results map[string][]point) {
	fmt.Fprintf(w, "## Row 1: rate one core holds (stand answers at once)\n\n")
	fmt.Fprintf(w, "Held: at least %.1f%% of the planned calls reach the stand within the run (%v allowance), and the lowest "+
		"steady second is at least %.0f%% of the rate; a point holds when more than half of its %d runs do. "+
		"Run %v each, tool container %d core(s), stand %d core(s).\n\n",
		heldShare*100, allowance, steadyShare*100, f.runs, f.duration, f.toolCPUs, f.standCPUs)

	for _, tool := range f.tools {
		held, broke := highestHeld(results[tool])
		fmt.Fprintf(w, "### %s: holds %d rps, did not hold %d rps\n\n", tool, held, broke)
		fmt.Fprintln(w, "| rate | runs held | arrived / planned | lowest second / rate | tool says sent | tool says failed |")
		fmt.Fprintln(w, "|---|---|---|---|---|---|")
		for _, p := range results[tool] {
			n := 0
			for i := range p.runs {
				if p.runs[i].Verdict.Held {
					n++
				}
			}
			fmt.Fprintf(w, "| %d | %d of %d | %s | %s | %s | %s |\n", p.rate, n, len(p.runs),
				spread(collect(p.runs, func(r runOut) float64 { return r.Verdict.Share }), showFloat),
				spread(collect(p.runs, func(r runOut) float64 { return r.Verdict.MinShare }), showFloat),
				spread(collect(p.runs, func(r runOut) int { return r.Reports.Sent }), showInt),
				spread(collect(p.runs, func(r runOut) int { return r.Reports.Failed }), showInt))
		}
		fmt.Fprintln(w)
	}
}

func row2Table(w *strings.Builder, f flags, results map[string][]runOut) {
	fmt.Fprintf(w, "## Row 2: schedule accuracy at %d rps, measured at the stand\n\n", f.fixedRate)
	fmt.Fprintf(w, "Steady seconds only (the first and the last second of each %v run are left out); median of %d runs "+
		"and the range. Gap percentiles are nearest rank over the gaps between consecutive arrivals.\n\n", f.duration, f.runs)
	fmt.Fprintln(w, "| tool | ideal gap | gap p50 | gap p99 | gap p99.9 | gap max | calls in a second, lowest | highest | std dev |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|")
	for _, tool := range f.tools {
		rs := results[tool]
		sp := func(get func(spacing) time.Duration) string {
			return spread(collect(rs, func(r runOut) time.Duration { return get(*r.Spacing) }), showDur)
		}
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", tool,
			showDur(rs[0].Spacing.Ideal),
			sp(func(s spacing) time.Duration { return s.GapP50 }),
			sp(func(s spacing) time.Duration { return s.GapP99 }),
			sp(func(s spacing) time.Duration { return s.GapP999 }),
			sp(func(s spacing) time.Duration { return s.GapMax }),
			spread(collect(rs, func(r runOut) int { return r.Spacing.SecMin }), showInt),
			spread(collect(rs, func(r runOut) int { return r.Spacing.SecMax }), showInt),
			spread(collect(rs, func(r runOut) float64 { return r.Spacing.SecStdDev }), showFloat))
	}
	fmt.Fprintln(w)
}

func row3Table(w *strings.Builder, f flags, results map[string][]runOut) {
	fmt.Fprintf(w, "## Row 3: the tool's own share of latency, stand answers after %v, %d rps\n\n", f.latDelay, f.latRate)
	fmt.Fprintf(w, "Added = the tool's p50 (p99) minus the stand's own hold time at the same percentile, per run; median of %d "+
		"runs and the range. The stand's time is from the call reaching its handler to the answer leaving it; the "+
		"network between the containers is in every tool's number alike.\n\n", f.runs)
	fmt.Fprintln(w, "| tool | tool p50 | stand p50 | p50 added | tool p99 | stand p99 | p99 added | failed | percentile method |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|")
	for _, tool := range f.tools {
		rs := results[tool]
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", tool,
			spread(collect(rs, func(r runOut) time.Duration { return r.Reports.P50 }), showDur),
			spread(collect(rs, func(r runOut) time.Duration { return r.Stand.HeldP50 }), showDur),
			spread(collect(rs, func(r runOut) time.Duration { return r.Reports.P50 - r.Stand.HeldP50 }), showDur),
			spread(collect(rs, func(r runOut) time.Duration { return r.Reports.P99 }), showDur),
			spread(collect(rs, func(r runOut) time.Duration { return r.Stand.HeldP99 }), showDur),
			spread(collect(rs, func(r runOut) time.Duration { return r.Reports.P99 - r.Stand.HeldP99 }), showDur),
			spread(collect(rs, func(r runOut) int { return r.Reports.Failed }), showInt),
			percentileMethod[tool])
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
	fmt.Fprintf(&b, "row 2: %d rps; row 3: %d rps, stand delay %v\n", f.fixedRate, f.latRate, f.latDelay)
	fmt.Fprintf(&b, "tool call timeout %v; every tool: one container, --cpus=%d, GOMAXPROCS=%d, the stand's address as an IP\n",
		callTimeout, f.toolCPUs, f.toolCPUs)
	fmt.Fprintf(&b, "connections: leettest 1, ghz 1, k6 one per virtual user (%d)\n\n", f.k6VUs)

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
	total := 0
	for _, r := range f.rows {
		if r == 1 {
			total += row1
		} else {
			total += row23
		}
	}
	fmt.Fprintf(&b, "runs at most: row 1 %d (each tool stops at its first point that does not hold), rows 2 and 3 %d each; "+
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
