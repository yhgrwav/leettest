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
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/yhgrwav/leettest/pkg/compare"
	"github.com/yhgrwav/leettest/pkg/metrics"
	shr "github.com/yhgrwav/leettest/pkg/share"
)

// RunFile is a plain-run report and the path it was read from.
type RunFile struct {
	Path   string
	Report JSONReport
}

// sides are the two reports of a comparison, base first: the order a reason
// is looked for in and the words that name them.
var sides = [2]string{"base", "new"}

// CompareRuns prints the comparison of two plain-run reports: both headers,
// the differences and, per method on both sides in the base report's order,
// the p99 and the share of failed calls of each side, with the verdict of the
// limit given for each check. A nil limit is a check not asked for.
func CompareRuns(w io.Writer, base, nw RunFile, p99Limit, errorsLimit *shr.Percent) Comparison {
	c, _ := CompareRunsReport(w, base, nw, p99Limit, errorsLimit)

	return c
}

// CompareRunsReport is CompareRuns with the report it printed, for
// NewCompareReport.
func CompareRunsReport(w io.Writer, base, nw RunFile, p99Limit, errorsLimit *shr.Percent) (Comparison, CompareReport) {
	rep := CompareReport{
		ComparedMode: ModeRun,
		Base:         compareSide(base.Path, base.Report.Name, base.Report.StartedAt, base.Report.LeetTestVersion, base.Report.Passport),
		New:          compareSide(nw.Path, nw.Report.Name, nw.Report.StartedAt, nw.Report.LeetTestVersion, nw.Report.Passport),
	}
	printHeader(w, "base:", rep.Base)
	printHeader(w, "new: ", rep.New)

	rep.Differences = runDifferences(base, nw)
	printDifferences(w, rep.Differences)

	same := len(rep.Differences) == 0
	judged := p99Limit != nil || errorsLimit != nil
	reports := [2]*JSONReport{&base.Report, &nw.Report}
	pairs := commonMethods(base.Report, nw.Report)

	// With nothing to put side by side there is no line to carry a verdict.
	if len(pairs) == 0 {
		fmt.Fprintln(w, notComparable+": nothing judged")

		return finish(rep, Comparison{Judged: judged})
	}

	worst := compare.Pass

	for _, pair := range pairs {
		held := heldBack{reason: compare.ReasonNotComparable, words: notComparable}
		if same {
			held = heldBackWords(reports, pair)
		}

		checks := [2]CompareCheck{
			p99Check(pair, p99Limit, same, held),
			errorsCheck(pair, errorsLimit, same, held),
		}

		for i := range checks {
			check := &checks[i]

			fmt.Fprintln(w, check.line)

			rep.Checks = append(rep.Checks, *check)

			switch {
			case check.decided == compare.Fail:
				worst = compare.Fail
			case check.decided == compare.Unknown && check.asked && worst != compare.Fail:
				worst = compare.Unknown
			}
		}
	}

	switch {
	case judged:
		return finish(rep, Comparison{Comparable: same, Judged: true, Verdict: worst})
	case same:
		fmt.Fprintln(w, "no limit given: nothing judged (-max-p99-rise, -max-errors-rise)")
	default:
		fmt.Fprintln(w, notComparable+": nothing judged")
	}

	return finish(rep, Comparison{Comparable: same})
}

// heldBack is why a method's numbers are not compared though the reports are
// alike: one of the Reason constants and the words text puts after it. The
// zero value is nothing held them back.
type heldBack struct{ reason, words string }

// methodPair is one method as the two reports have it, its name without the
// leading slash a report writes.
type methodPair struct {
	name string
	side [2]*jsonMethod
}

// commonMethods are the methods both reports have, in the base report's order.
func commonMethods(base, nw JSONReport) []methodPair {
	var pairs []methodPair

	for i := range base.Methods {
		name := trimSlash(base.Methods[i].Method)

		j := slices.IndexFunc(nw.Methods, func(m jsonMethod) bool { return trimSlash(m.Method) == name })
		if j >= 0 {
			pairs = append(pairs, methodPair{name: name, side: [2]*jsonMethod{&base.Methods[i], &nw.Methods[j]}})
		}
	}

	return pairs
}

func trimSlash(method string) string { return strings.TrimPrefix(method, "/") }

// runDifferences are the lines naming what makes two run reports
// incomparable, in the order: target, version, passport, the sets of methods,
// the dataset of each method both have. Labels are never compared: they are
// what differs by design.
func runDifferences(base, nw RunFile) []CompareDiff {
	var out []CompareDiff

	out = differ(out, "target", base.Report.Target, nw.Report.Target, "")
	out = differ(out, "leettest_version", base.Report.LeetTestVersion, nw.Report.LeetTestVersion, "")
	out = append(out, passportDifferences(base.Path, nw.Path, base.Report.Passport, nw.Report.Passport)...)
	out = differ(out, "methods", methodSet(base.Report), methodSet(nw.Report), "")

	for _, pair := range commonMethods(base.Report, nw.Report) {
		out = differ(out, "dataset "+pair.name, methodDataset(pair.side[0]), methodDataset(pair.side[1]), "")
	}

	return out
}

// methodSet is the names of the methods of a report, sorted, "none" for no
// method.
func methodSet(r JSONReport) string {
	names := make([]string, 0, len(r.Methods))
	for i := range r.Methods {
		names = append(names, trimSlash(r.Methods[i].Method))
	}

	slices.Sort(names)

	if names = slices.Compact(names); len(names) == 0 {
		return "none"
	}

	return strings.Join(names, ", ")
}

func methodDataset(m *jsonMethod) string {
	if m.Dataset == nil {
		return "none"
	}

	return m.Dataset.SHA256
}

// heldBackWords says why a method's numbers are not compared though the
// reports are alike, the zero heldBack when they are: the first of an invalid
// side, an incomplete side, a side its own generator or stream held back, the
// base before the new in each. A method that is invalid names itself before
// the run does.
func heldBackWords(reports [2]*JSONReport, pair methodPair) heldBack {
	for i, r := range reports {
		switch {
		case pair.side[i].InvalidReason != nil:
			return heldBack{compare.ReasonMethodInvalid, sides[i] + " method is invalid (" + *pair.side[i].InvalidReason + ")"}
		case runInvalid(*r):
			return heldBack{compare.ReasonInvalid, sides[i] + " run is invalid"}
		}
	}

	for i, r := range reports {
		if r.Outcome == OutcomeIncomplete {
			return heldBack{compare.ReasonIncomplete, sides[i] + " run is incomplete"}
		}
	}

	for i, r := range reports {
		switch heldBackBy(*r, pair.side[i]) {
		case compare.ReasonGenerator:
			return heldBack{compare.ReasonGenerator, sides[i] + ": the generator held calls back"}
		case compare.ReasonStream:
			return heldBack{compare.ReasonStream, sides[i] + ": the stream limit held calls back"}
		}
	}

	return heldBack{}
}

// runInvalid: the run measured nothing about the target. A run invalid only
// because its own pass: limits could not be decided measured fine, and
// compare judges its own checks.
func runInvalid(r JSONReport) bool {
	return r.Outcome == OutcomeInvalid && !slices.Equal(r.InvalidReasons, []string{reasonThresholdsUndecided})
}

// heldBackBy is what held a side's calls back, ReasonGenerator or
// ReasonStream, "" when nothing did: the tail's cause when the generator or
// the stream set it, else the method's calls not sent for a reason other than
// the connection (the generator's when it left some, else the stream's, else
// the generator: the report does not say).
func heldBackBy(r JSONReport, m *jsonMethod) string {
	if r.TailWaitCause != nil {
		switch *r.TailWaitCause {
		case "generator":
			return compare.ReasonGenerator
		case "stream":
			return compare.ReasonStream
		}
	}

	switch {
	case m.NotSent-m.NotSentConnection <= 0:
		return ""
	case m.NotSentGenerator > 0:
		return compare.ReasonGenerator
	case m.NotSentStream > 0:
		return compare.ReasonStream
	}

	return compare.ReasonGenerator
}

func quantileOf(q *jsonQuantile) metrics.Quantile {
	if q == nil {
		return metrics.Quantile{}
	}

	return metrics.Quantile{Value: time.Duration(q.NS), Exact: !q.LowerBound, Defined: true}
}

func latencyOf(m *jsonMethod) compare.Latency {
	q := m.Latency.P99
	if q == nil {
		return compare.Latency{}
	}

	return compare.Latency{NS: q.NS, LowerBound: q.LowerBound, Defined: true}
}

// quantileCopy is a report's own quantile, a copy of it, nil when it has none.
func quantileCopy(q *jsonQuantile) *jsonQuantile {
	if q == nil {
		return nil
	}

	c := *q

	return &c
}

// p99Check is a method's p99: the report's own two values, the line text
// prints and the verdict. held is why the numbers are not compared, the zero
// heldBack when they are.
func p99Check(pair methodPair, limit *shr.Percent, same bool, held heldBack) CompareCheck {
	b, n := latencyOf(pair.side[0]), latencyOf(pair.side[1])
	check := CompareCheck{
		Method: pair.side[0].Method, Key: "p99",
		BaseP99: quantileCopy(pair.side[0].Latency.P99), NewP99: quantileCopy(pair.side[1].Latency.P99),
	}
	text := pair.name + ": p99 " + formatQuantile(quantileOf(pair.side[0].Latency.P99)) + " -> " +
		formatQuantile(quantileOf(pair.side[1].Latency.P99))

	verdict := compare.Unknown

	if same && held.reason == "" && limit != nil {
		var reason string

		verdict, reason = compare.P99Rise(b, n, *limit)
		held = heldBack{reason, p99Words(reason, b, n)}
	}

	change := "rise -"
	if same {
		change = p99Change(b, n, limit, verdict)
	}

	return finishCheck(check, text, change, limit, verdict, held)
}

func p99Words(reason string, b, n compare.Latency) string {
	switch reason {
	case compare.ReasonFewObservations:
		return "too few observations for p99"
	case compare.ReasonBaseZero:
		return "base p99 is 0 us"
	case compare.ReasonCensored:
		switch {
		case b.LowerBound && n.LowerBound:
			return "both p99 only a lower bound (timeouts)"
		case b.LowerBound:
			return "base p99 only a lower bound (timeouts)"
		}

		return "new p99 only a lower bound (timeouts)"
	}

	return reason
}

// finishCheck ends a check: its line with the change and, with a limit, the
// verdict; the reason and words are those of an unknown.
func finishCheck(check CompareCheck, text, change string, limit *shr.Percent, verdict compare.Verdict, held heldBack) CompareCheck {
	check.line = text + ", " + change
	if limit == nil {
		check.decide(nil, compare.Unknown, "", "")

		return check
	}

	check.line += limitSuffix(limit, verdict, held.words)
	check.decide(limit, verdict, held.reason, held.words)

	return check
}

// p99Change is the change from base to new as the line says it, worded by
// what is known: the capacity wording with the direction of the p99. A bound
// carries `<=` or `>=` by what it leaves open, rounded so the number never
// claims more than is known; two lower bounds, an undefined p99 and a base of
// 0 say no number. A rise bound the verdict was drawn on keeps its printed
// digits on the right side of the limit (share.FormatBound); a fall is judged
// by no limit.
func p99Change(b, n compare.Latency, limit *shr.Percent, v compare.Verdict) string {
	if !b.Defined || !n.Defined || b.NS <= 0 || b.LowerBound && n.LowerBound {
		return "rise -"
	}

	// "No change" is equal nanoseconds: two values that print alike but
	// differ are a rise or a fall of 0.0%, so a limit of 0% can fail them.
	if !b.LowerBound && !n.LowerBound && n.NS == b.NS {
		return "no change"
	}

	fall := big.NewRat(b.NS-n.NS, b.NS)
	rise := big.NewRat(n.NS-b.NS, b.NS)

	// Only a rise is judged by the limit of the rise: a fall carries no limit,
	// and its bound is a tenth, up or down.
	switch {
	case !b.LowerBound && !n.LowerBound && n.NS < b.NS:
		return "fall " + shr.FormatFraction(fall, shr.Percent{Text: "0%"}, shr.Holds)
	case !b.LowerBound && !n.LowerBound && limit != nil && v != compare.Unknown:
		return "rise " + shr.FormatFraction(rise, *limit, shareVerdict(v))
	case !b.LowerBound && !n.LowerBound:
		return "rise " + shr.FormatFraction(rise, shr.Percent{Text: "0%"}, shr.Holds)
	case n.LowerBound && n.NS < b.NS:
		return "fall " + shr.FormatBound(fall, true, nil, shareVerdict(v))
	case n.LowerBound:
		return "rise " + shr.FormatBound(rise, false, limit, shareVerdict(v))
	case n.NS < b.NS:
		return "fall " + shr.FormatBound(fall, false, nil, shareVerdict(v))
	case n.NS > b.NS:
		return "rise " + shr.FormatBound(rise, true, limit, shareVerdict(v))
	}

	return "fall " + shr.FormatBound(new(big.Rat), false, nil, shareVerdict(v))
}

func shareOf(m *jsonMethod) compare.Share {
	return compare.Share{
		Bad:       m.Failed + m.NotSentConnection,
		BadClient: m.RequestError.Count + m.ClientError + m.BadResponse.Count,
		Total:     m.Sent + m.NotSentConnection,
	}
}

func errorsOf(s compare.Share) *CompareErrors {
	return &CompareErrors{Bad: s.Bad, BadClient: s.BadClient, Total: s.Total}
}

// errorsCheck is a method's share of failed calls, the way p99Check is its p99.
func errorsCheck(pair methodPair, limit *shr.Percent, same bool, held heldBack) CompareCheck {
	b, n := shareOf(pair.side[0]), shareOf(pair.side[1])
	check := CompareCheck{Method: pair.side[0].Method, Key: "errors", BaseErrors: errorsOf(b), NewErrors: errorsOf(n)}
	text := pair.name + ": errors " + failedShareText(b) + " -> " + failedShareText(n)

	verdict := compare.Unknown

	if same && held.reason == "" && limit != nil {
		var reason string

		verdict, reason = compare.ErrorsRise(b, n, *limit)
		held = heldBack{reason: reason, words: reason}

		switch reason {
		case compare.ReasonNothingSent:
			held.words = "base: nothing sent"
			if b.Total != 0 {
				held.words = "new: nothing sent"
			}
		case compare.ReasonClientErrors:
			held.words = "client-side errors decide it"
		}
	}

	change := "rise -"
	if same {
		change, _ = errorsChange(b, n, limit, verdict)
	}

	return finishCheck(check, text, change, limit, verdict, held)
}

func failedShareText(s compare.Share) string {
	if s.Total == 0 {
		return "-"
	}

	return nearestTenth(s.Bad, s.Total)
}

// errorsChange is the change in the share of failed calls in points, from the
// exact difference of the two fractions: counts near 10^9 on both sides do not
// fit an int64 product. The second result is the digits after the point the
// change was printed with.
func errorsChange(b, n compare.Share, limit *shr.Percent, v compare.Verdict) (string, int) {
	if b.Total == 0 || n.Total == 0 {
		return "rise -", 1
	}

	diff := new(big.Rat).Sub(big.NewRat(int64(n.Bad), int64(n.Total)), big.NewRat(int64(b.Bad), int64(b.Total)))
	plain := shr.Percent{Text: "0%"}

	switch diff.Sign() {
	case 0:
		return "no change", 1
	case -1:
		return "fall " + shr.FormatFraction(diff.Neg(diff), plain, shr.Holds), 1
	}

	if limit != nil && v != compare.Unknown {
		text, digits := shr.FormatFractionDigits(diff, *limit, shareVerdict(v))

		return "rise " + text, digits
	}

	return "rise " + shr.FormatFraction(diff, plain, shr.Holds), 1
}
