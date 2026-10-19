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
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/yhgrwav/leettest/pkg/compare"
	shr "github.com/yhgrwav/leettest/pkg/share"
)

// noPassportNote is said of a report that has none.
const noPassportNote = "no passport (report from leettest before v0.3)"

// notComparable is the reason a capacity check is unknown when the reports
// differ.
const notComparable = "reports are not comparable"

// ReadSearchReport reads a breaking-point search report as -json-file writes
// it. A field this leettest does not know is ignored.
func ReadSearchReport(path string) (BreakpointReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return BreakpointReport{}, err
	}

	var head struct {
		SchemaVersion int    `json:"schema_version"`
		Mode          string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return BreakpointReport{}, fmt.Errorf("%s: not a JSON report: %w", path, err)
	}

	switch {
	case head.SchemaVersion != JSONSchemaVersion:
		return BreakpointReport{}, fmt.Errorf("%s: schema_version %d, this leettest reads %d", path, head.SchemaVersion, JSONSchemaVersion)
	case head.Mode == ModeRun:
		return BreakpointReport{}, fmt.Errorf("%s is a plain run; compare reads two breakpoint search reports for now", path)
	case head.Mode != ModeBreakpoint:
		return BreakpointReport{}, fmt.Errorf("%s: mode %q is not a breakpoint search report", path, head.Mode)
	}

	var report BreakpointReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return BreakpointReport{}, fmt.Errorf("%s: not a breakpoint search report: %w", path, err)
	}

	return report, nil
}

// SearchFile is a search report and the path it was read from.
type SearchFile struct {
	Path   string
	Report BreakpointReport
}

// Comparison is what CompareSearches decided.
type Comparison struct {
	// Comparable: no difference between the reports that would make their
	// capacities incomparable.
	Comparable bool
	// Judged: a limit was given; Verdict is then the check's answer, Unknown
	// when the reports are not comparable.
	Judged  bool
	Verdict compare.Verdict
}

// CompareSearches prints the comparison of two search reports: both headers,
// the differences, the capacity of each and, with a limit, the verdict on its
// fall.
func CompareSearches(w io.Writer, base, nw SearchFile, limit *shr.Percent) Comparison {
	printHeader(w, "base:", base)
	printHeader(w, "new: ", nw)

	diffs := differences(base, nw)
	for _, d := range diffs {
		fmt.Fprintln(w, d)
	}
	same := len(diffs) == 0

	b, n := reportCapacity(base.Report), reportCapacity(nw.Report)
	line := "capacity: " + capacityText(b) + " -> " + capacityText(n)

	switch {
	case !same && limit == nil:
		fmt.Fprintln(w, line+", drop -")
		fmt.Fprintln(w, notComparable+": nothing judged")

		return Comparison{}
	case !same:
		fmt.Fprintf(w, "%s, drop -, limit %s: unknown, %s\n", line, limit.Text, notComparable)

		return Comparison{Judged: true}
	case limit == nil:
		fmt.Fprintln(w, line+", "+fallText(b, n, nil, compare.Unknown))
		fmt.Fprintln(w, "no limit given: nothing judged (-max-capacity-drop)")

		return Comparison{Comparable: true}
	}

	verdict, reason := compare.CapacityDrop(b, n, *limit)
	line += ", " + fallText(b, n, limit, verdict) + ", limit " + limit.Text + ": "

	switch verdict {
	case compare.Pass:
		line += "pass"
	case compare.Fail:
		line += "FAIL"
	case compare.Unknown:
		line += "unknown, " + reason
	}
	fmt.Fprintln(w, line)

	return Comparison{Comparable: true, Judged: true, Verdict: verdict}
}

func printHeader(w io.Writer, tag string, f SearchFile) {
	name := f.Report.Name
	if name == "" {
		name = "-"
	}

	line := fmt.Sprintf("%s %s  %s  %s", tag, f.Path, name, f.Report.StartedAt)
	if p := f.Report.Passport; p != nil && len(p.Labels) > 0 {
		pairs := make([]string, 0, len(p.Labels))
		for _, k := range slices.Sorted(maps.Keys(p.Labels)) {
			pairs = append(pairs, k+"="+p.Labels[k])
		}
		line += "  labels: " + strings.Join(pairs, ", ")
	}
	fmt.Fprintln(w, line)
}

// differences are the lines naming what makes two reports incomparable, in
// the order the fields are compared. Labels are never compared: they are what
// differs by design.
func differences(base, nw SearchFile) []string {
	var out []string
	differ := func(field, b, n, note string) {
		if b != n {
			out = append(out, "differs: "+field+": "+b+" -> "+n+note)
		}
	}

	differ("target", base.Report.Target, nw.Report.Target, "")
	differ("method", base.Report.Method, nw.Report.Method, "")
	differ("leettest_version", base.Report.LeetTestVersion, nw.Report.LeetTestVersion, "")

	bp, np := base.Report.Passport, nw.Report.Passport
	if bp == nil {
		out = append(out, base.Path+": "+noPassportNote)
	}
	if np == nil {
		out = append(out, nw.Path+": "+noPassportNote)
	}
	if bp != nil && np != nil {
		differ("passport.config_sha256", bp.ConfigSHA256, np.ConfigSHA256, " (config bytes differ: a comment or line endings count too)")
		differ("passport.os", bp.OS, np.OS, "")
		differ("passport.arch", bp.Arch, np.Arch, "")
		differ("passport.num_cpu", strconv.Itoa(bp.NumCPU), strconv.Itoa(np.NumCPU), "")
		differ("passport.gomaxprocs", strconv.Itoa(bp.GOMAXPROCS), strconv.Itoa(np.GOMAXPROCS), "")
		differ("passport.cpu_quota", quotaText(bp.CPUQuota), quotaText(np.CPUQuota), "")
		differ("passport.go_version", bp.GoVersion, np.GoVersion, "")
	}

	differ("dataset", datasetHash(base.Report), datasetHash(nw.Report), "")

	return out
}

func quotaText(q *float64) string {
	if q == nil {
		return "none"
	}

	return strconv.FormatFloat(*q, 'f', -1, 64)
}

// datasetHash is the sha256 of the dataset the searched method used, "none"
// when it used none.
func datasetHash(r BreakpointReport) string {
	if len(r.Breakpoint.Runs) > 0 {
		methods := r.Breakpoint.Runs[0].Report.Methods
		for i := range methods {
			if methods[i].Method == r.Method && methods[i].Dataset != nil {
				return methods[i].Dataset.SHA256
			}
		}
	}

	return "none"
}

// reportCapacity is the capacity a search found, from its outcome.
func reportCapacity(r BreakpointReport) compare.Capacity {
	bp := r.Breakpoint
	c := compare.Capacity{Bound: compare.None, Outcome: bp.Outcome}

	switch bp.Outcome {
	case "broke":
		if bp.HeldRPS != nil {
			c.RPS, c.Bound = *bp.HeldRPS, compare.Exact
		}
	case "broke_at_first":
		if len(bp.Runs) > 0 {
			c.RPS, c.Bound = bp.Runs[0].PlannedRPS, compare.Upper
		}
	case "held_all", "run_limit", "stopped":
		if bp.HeldRPS != nil {
			c.RPS, c.Bound = *bp.HeldRPS, compare.Lower
		}
	}

	return c
}

func capacityText(c compare.Capacity) string {
	switch c.Bound {
	case compare.Exact:
		return strconv.Itoa(c.RPS) + " rps"
	case compare.Lower:
		return ">= " + strconv.Itoa(c.RPS) + " rps"
	case compare.Upper:
		return "below " + strconv.Itoa(c.RPS) + " rps"
	case compare.None:
	}

	return "not measured (" + c.Outcome + ")"
}

// fallText is the change from base to new as the capacity line says it. A
// bound carries `<=` or `>=` by what it leaves open, rounded so the number
// never claims more than is known: an upper bound up, a lower bound down.
// Two lower bounds, a base below its first step and a capacity not measured
// say no number.
func fallText(base, nw compare.Capacity, limit *shr.Percent, v compare.Verdict) string {
	b, n := base.RPS, nw.RPS
	if base.Bound == compare.None || nw.Bound == compare.None || base.Bound == compare.Upper || b <= 0 ||
		base.Bound == compare.Lower && nw.Bound == compare.Lower {
		return "drop -"
	}

	exact := base.Bound == compare.Exact && nw.Bound == compare.Exact
	if exact && n == b {
		return "no change"
	}

	// A fall the verdict was drawn on keeps its printed number on the right
	// side of the limit; the rest are printed as they are, to a tenth.
	fall := func(qualifier string, round func(bad, total int) string) string {
		if limit != nil && v != compare.Unknown {
			verdict := shr.Holds
			if v == compare.Fail {
				verdict = shr.Breaks
			}

			return "drop " + qualifier + shr.Format(b-n, b, *limit, verdict)
		}

		return "drop " + qualifier + round(b-n, b)
	}
	rise := func(qualifier string, round func(bad, total int) string) string {
		return "rise " + qualifier + round(n-b, b)
	}

	switch {
	case exact && n < b:
		return fall("", nearestTenth)
	case exact:
		return rise("", nearestTenth)
	case nw.Bound == compare.Lower && n < b:
		return fall("<= ", tenthUp)
	case nw.Bound == compare.Lower:
		return rise(">= ", tenthDown)
	case n < b:
		return fall(">= ", tenthDown)
	case n > b:
		return rise("<= ", tenthUp)
	}

	return "drop >= " + tenthDown(0, b)
}

// nearestTenth is the share bad/total to a tenth of a percent, half up, as
// every share leettest prints.
func nearestTenth(bad, total int) string {
	return shr.Format(bad, total, shr.Percent{Text: "0%"}, shr.Holds)
}

// tenthUp is the share bad/total to a tenth of a percent, rounded up.
func tenthUp(bad, total int) string {
	return tenths((int64(bad)*1000 + int64(total) - 1) / int64(total))
}

// tenthDown is the share bad/total to a tenth of a percent, rounded down.
func tenthDown(bad, total int) string {
	return tenths(int64(bad) * 1000 / int64(total))
}

func tenths(t int64) string {
	return fmt.Sprintf("%d.%d%%", t/10, t%10)
}
