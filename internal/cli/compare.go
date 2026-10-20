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

// ComparedReport is a report read for compare. Mode says which of the two
// reports is filled: ModeRun Run, ModeBreakpoint Search.
type ComparedReport struct {
	Mode   string
	Run    JSONReport
	Search BreakpointReport
}

// ReadReport reads a report as -json-file writes it, a plain run or a
// breaking-point search. A field this leettest does not know is ignored.
func ReadReport(path string) (ComparedReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ComparedReport{}, err
	}

	var head struct {
		SchemaVersion int    `json:"schema_version"`
		Mode          string `json:"mode"`
	}
	if err = json.Unmarshal(raw, &head); err != nil {
		return ComparedReport{}, fmt.Errorf("%s: not a JSON report: %w", path, err)
	}

	if head.SchemaVersion != JSONSchemaVersion {
		return ComparedReport{}, fmt.Errorf("%s: schema_version %d, this leettest reads %d", path, head.SchemaVersion, JSONSchemaVersion)
	}

	out := ComparedReport{Mode: head.Mode}

	switch head.Mode {
	case ModeRun:
		err = json.Unmarshal(raw, &out.Run)
	case ModeBreakpoint:
		err = json.Unmarshal(raw, &out.Search)
	default:
		return ComparedReport{}, fmt.Errorf("%s: mode %q is neither a plain run nor a breakpoint search report", path, head.Mode)
	}

	if err != nil {
		return ComparedReport{}, fmt.Errorf("%s: not a %s report: %w", path, KindOf(head.Mode), err)
	}

	return out, nil
}

// KindOf words a report's mode for a message: a plain run, a breakpoint
// search.
func KindOf(mode string) string {
	if mode == ModeBreakpoint {
		return "breakpoint search"
	}

	return "plain run"
}

// SearchFile is a search report and the path it was read from.
type SearchFile struct {
	Path   string
	Report BreakpointReport
}

// Comparison is what CompareSearches and CompareRuns decided.
type Comparison struct {
	// Comparable: no difference between the reports that would make them
	// incomparable (for runs, also at least one method on both sides).
	Comparable bool
	// Judged: at least one limit was given; Verdict is then the worst of the
	// checks asked for (Fail, else Unknown, else Pass), Unknown when the
	// reports are not comparable.
	Judged  bool
	Verdict compare.Verdict
}

// finish puts the comparison's own outcome into its report.
func finish(rep CompareReport, c Comparison) (Comparison, CompareReport) {
	rep.Comparable = c.Comparable

	if c.Judged {
		rep.Verdict = ptrTo(verdictName(c.Verdict))
	}

	return c, rep
}

// CompareSearches prints the comparison of two search reports: both headers,
// the differences, the capacity of each and, with a limit, the verdict on its
// fall.
func CompareSearches(w io.Writer, base, nw SearchFile, limit *shr.Percent) Comparison {
	c, _ := CompareSearchesReport(w, base, nw, limit)

	return c
}

// CompareSearchesReport is CompareSearches with the report it printed, for
// NewCompareReport.
func CompareSearchesReport(w io.Writer, base, nw SearchFile, limit *shr.Percent) (Comparison, CompareReport) {
	rep := CompareReport{
		ComparedMode: ModeBreakpoint,
		Base:         compareSide(base.Path, base.Report.Name, base.Report.StartedAt, base.Report.LeetTestVersion, base.Report.Passport),
		New:          compareSide(nw.Path, nw.Report.Name, nw.Report.StartedAt, nw.Report.LeetTestVersion, nw.Report.Passport),
	}
	printHeader(w, "base:", rep.Base)
	printHeader(w, "new: ", rep.New)

	rep.Differences = differences(base, nw)
	printDifferences(w, rep.Differences)
	same := len(rep.Differences) == 0

	b, n := reportCapacity(base.Report), reportCapacity(nw.Report)
	check := CompareCheck{
		Method: base.Report.Method, Key: "capacity",
		BaseCapacity: capacityOf(b), NewCapacity: capacityOf(n),
		BaseOutcome: ptrTo(b.Outcome), NewOutcome: ptrTo(n.Outcome),
	}
	line := "capacity: " + capacityText(b) + " -> " + capacityText(n)

	var (
		res  Comparison
		tail string
	)

	switch {
	case !same && limit == nil:
		check.line, tail = line+", drop -", notComparable+": nothing judged"
		check.decide(nil, compare.Unknown, "", "")
	case !same:
		check.line = line + ", drop -" + limitSuffix(limit, compare.Unknown, notComparable)
		check.decide(limit, compare.Unknown, compare.ReasonNotComparable, notComparable)
		res = Comparison{Judged: true}
	case limit == nil:
		check.line, tail = line+", "+fallText(b, n, nil, compare.Unknown), "no limit given: nothing judged (-max-capacity-drop)"
		check.decide(nil, compare.Unknown, "", "")
		res = Comparison{Comparable: true}
	default:
		verdict, reason, words := compare.CapacityDrop(b, n, *limit)
		check.line = line + ", " + fallText(b, n, limit, verdict) + limitSuffix(limit, verdict, words)
		check.decide(limit, verdict, reason, words)
		res = Comparison{Comparable: true, Judged: true, Verdict: verdict}
	}

	fmt.Fprintln(w, check.line)

	if tail != "" {
		fmt.Fprintln(w, tail)
	}

	rep.Checks = []CompareCheck{check}

	return finish(rep, res)
}

// limitSuffix ends a check's line with its limit and verdict; words are the
// reason of an unknown.
func limitSuffix(limit *shr.Percent, v compare.Verdict, words string) string {
	text := ", limit " + limit.Text + ": "

	switch v {
	case compare.Pass:
		return text + "pass"
	case compare.Fail:
		return text + "FAIL"
	case compare.Unknown:
	}

	return text + "unknown, " + words
}

// printHeader is the line naming a report: its file, name, start and labels.
func printHeader(w io.Writer, tag string, s CompareSide) {
	name := s.Name
	if name == "" {
		name = "-"
	}

	line := fmt.Sprintf("%s %s  %s  %s", tag, s.Path, name, s.StartedAt)
	if len(s.Labels) > 0 {
		pairs := make([]string, 0, len(s.Labels))
		for _, k := range slices.Sorted(maps.Keys(s.Labels)) {
			pairs = append(pairs, k+"="+s.Labels[k])
		}
		line += "  labels: " + strings.Join(pairs, ", ")
	}
	fmt.Fprintln(w, line)
}

func printDifferences(w io.Writer, diffs []CompareDiff) {
	for _, d := range diffs {
		for _, line := range d.lines {
			fmt.Fprintln(w, line)
		}
	}
}

// differences name what makes two reports incomparable, in the order the
// fields are compared. Labels are never compared: they are what differs by
// design.
func differences(base, nw SearchFile) []CompareDiff {
	var out []CompareDiff

	out = differ(out, "target", base.Report.Target, nw.Report.Target, "")
	out = differ(out, "method", base.Report.Method, nw.Report.Method, "")
	out = differ(out, "leettest_version", base.Report.LeetTestVersion, nw.Report.LeetTestVersion, "")
	out = append(out, passportDifferences(base.Path, nw.Path, base.Report.Passport, nw.Report.Passport)...)

	return differ(out, "dataset", datasetHash(base.Report), datasetHash(nw.Report), "")
}

// differ adds the field whose two values differ; the note is only in its text
// line.
func differ(out []CompareDiff, field, b, n, note string) []CompareDiff {
	if b == n {
		return out
	}

	return append(out, CompareDiff{Field: field, Base: b, New: n, lines: []string{"differs: " + field + ": " + b + " -> " + n + note}})
}

// passportDifferences are the one difference of a missing passport (a text
// line for each report that lacks it), else one for each field of the two that
// differs, in the order of the passport. The paths name the file without one.
func passportDifferences(basePath, newPath string, bp, np *jsonPassport) []CompareDiff {
	var out []CompareDiff

	if bp == nil || np == nil {
		d := CompareDiff{Field: "passport", Base: "present", New: "present"}

		if bp == nil {
			d.Base = "none"
			d.lines = append(d.lines, basePath+": "+noPassportNote)
		}

		if np == nil {
			d.New = "none"
			d.lines = append(d.lines, newPath+": "+noPassportNote)
		}

		return append(out, d)
	}

	out = differ(out, "passport.config_sha256", bp.ConfigSHA256, np.ConfigSHA256, " (config bytes differ: a comment or line endings count too)")
	out = differ(out, "passport.os", bp.OS, np.OS, "")
	out = differ(out, "passport.arch", bp.Arch, np.Arch, "")
	out = differ(out, "passport.num_cpu", strconv.Itoa(bp.NumCPU), strconv.Itoa(np.NumCPU), "")
	out = differ(out, "passport.gomaxprocs", strconv.Itoa(bp.GOMAXPROCS), strconv.Itoa(np.GOMAXPROCS), "")
	out = differ(out, "passport.cpu_quota", quotaText(bp.CPUQuota), quotaText(np.CPUQuota), "")

	return differ(out, "passport.go_version", bp.GoVersion, np.GoVersion, "")
}

func quotaText(q *float64) string {
	if q == nil {
		return "none"
	}

	return strconv.FormatFloat(*q, 'f', -1, 64)
}

// datasetHash is the sha256 of the dataset the searched method used, "none"
// when it used none. The report names the method without a leading slash on
// top and with one in each run's report.
func datasetHash(r BreakpointReport) string {
	if len(r.Breakpoint.Runs) > 0 {
		methods := r.Breakpoint.Runs[0].Report.Methods
		for i := range methods {
			if strings.TrimPrefix(methods[i].Method, "/") == strings.TrimPrefix(r.Method, "/") && methods[i].Dataset != nil {
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

// capacityOf is the capacity a report carries, nil when none was measured.
func capacityOf(c compare.Capacity) *CompareCapacity {
	switch c.Bound {
	case compare.Exact:
		return &CompareCapacity{RPS: c.RPS, Bound: "exact"}
	case compare.Lower:
		return &CompareCapacity{RPS: c.RPS, Bound: "lower"}
	case compare.Upper:
		return &CompareCapacity{RPS: c.RPS, Bound: "upper"}
	case compare.None:
	}

	return nil
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
