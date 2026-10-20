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
	printHeader(w, "base:", base.Path, base.Report.Name, base.Report.StartedAt, base.Report.Passport)
	printHeader(w, "new: ", nw.Path, nw.Report.Name, nw.Report.StartedAt, nw.Report.Passport)

	diffs := runDifferences(base, nw)
	for _, d := range diffs {
		fmt.Fprintln(w, d)
	}

	same := len(diffs) == 0
	judged := p99Limit != nil || errorsLimit != nil
	reports := [2]*JSONReport{&base.Report, &nw.Report}
	pairs := commonMethods(base.Report, nw.Report)

	// With nothing to put side by side there is no line to carry a verdict.
	if len(pairs) == 0 {
		fmt.Fprintln(w, notComparable+": nothing judged")

		return Comparison{Judged: judged}
	}

	worst := compare.Pass

	for _, pair := range pairs {
		words := notComparable
		if same {
			words = heldBackWords(reports, pair)
		}

		for _, line := range []checkLine{
			p99Line(pair, p99Limit, same, words),
			errorsLine(pair, errorsLimit, same, words),
		} {
			fmt.Fprintln(w, line.text)

			switch {
			case line.verdict == compare.Fail:
				worst = compare.Fail
			case line.verdict == compare.Unknown && line.asked && worst != compare.Fail:
				worst = compare.Unknown
			}
		}
	}

	switch {
	case judged:
		return Comparison{Comparable: same, Judged: true, Verdict: worst}
	case same:
		fmt.Fprintln(w, "no limit given: nothing judged (-max-p99-rise, -max-errors-rise)")
	default:
		fmt.Fprintln(w, notComparable+": nothing judged")
	}

	return Comparison{Comparable: same}
}

// checkLine is a printed check and what it decided; asked is false for a
// check no limit was given for.
type checkLine struct {
	text    string
	verdict compare.Verdict
	asked   bool
}

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
func runDifferences(base, nw RunFile) []string {
	var out []string

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
// reports are alike, "" when they are: the first of an invalid side, an
// incomplete side, a side its own generator or stream held back, the base
// before the new in each. A method that is invalid names itself before the
// run does.
func heldBackWords(reports [2]*JSONReport, pair methodPair) string {
	for i, r := range reports {
		switch {
		case pair.side[i].InvalidReason != nil:
			return sides[i] + " method is invalid (" + *pair.side[i].InvalidReason + ")"
		case runInvalid(*r):
			return sides[i] + " run is invalid"
		}
	}

	for i, r := range reports {
		if r.Outcome == OutcomeIncomplete {
			return sides[i] + " run is incomplete"
		}
	}

	for i, r := range reports {
		switch heldBackBy(*r, pair.side[i]) {
		case compare.ReasonGenerator:
			return sides[i] + ": the generator held calls back"
		case compare.ReasonStream:
			return sides[i] + ": the stream limit held calls back"
		}
	}

	return ""
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

	return metrics.Quantile{Value: time.Duration(q.US) * time.Microsecond, Exact: !q.LowerBound, Defined: true}
}

func latencyOf(m *jsonMethod) compare.Latency {
	q := m.Latency.P99
	if q == nil {
		return compare.Latency{}
	}

	return compare.Latency{US: q.US, LowerBound: q.LowerBound, Defined: true}
}

// p99Line is the line of a method's p99. words is why the numbers are not
// compared, "" when they are.
func p99Line(pair methodPair, limit *shr.Percent, same bool, words string) checkLine {
	b, n := latencyOf(pair.side[0]), latencyOf(pair.side[1])
	text := pair.name + ": p99 " + formatQuantile(quantileOf(pair.side[0].Latency.P99)) + " -> " +
		formatQuantile(quantileOf(pair.side[1].Latency.P99))

	verdict := compare.Unknown

	if same && words == "" && limit != nil {
		var reason string

		verdict, reason = compare.P99Rise(b, n, *limit)
		words = p99Words(reason, b, n)
	}

	change := "rise -"
	if same {
		change = p99Change(b, n, limit, verdict)
	}

	return finishLine(text, change, limit, verdict, words)
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

// finishLine ends a check's line: its change and, with a limit, the verdict.
func finishLine(text, change string, limit *shr.Percent, verdict compare.Verdict, words string) checkLine {
	text += ", " + change
	if limit == nil {
		return checkLine{text: text, verdict: compare.Unknown}
	}

	text += ", limit " + limit.Text + ": "

	switch verdict {
	case compare.Pass:
		text += "pass"
	case compare.Fail:
		text += "FAIL"
	case compare.Unknown:
		text += "unknown, " + words
	}

	return checkLine{text: text, verdict: verdict, asked: true}
}

// p99Change is the change from base to new as the line says it, worded by
// what is known: the capacity wording with the direction of the p99. A bound
// carries `<=` or `>=` by what it leaves open, rounded so the number never
// claims more than is known; two lower bounds, an undefined p99 and a base of
// 0 say no number. A rise the verdict was drawn on keeps its printed number on
// the right side of the limit.
func p99Change(b, n compare.Latency, limit *shr.Percent, v compare.Verdict) string {
	if !b.Defined || !n.Defined || b.US <= 0 || b.LowerBound && n.LowerBound {
		return "rise -"
	}

	bus, nus := int(b.US), int(n.US)

	if !b.LowerBound && !n.LowerBound && nus == bus {
		return "no change"
	}

	rise := func(qualifier string, round func(bad, total int) string) string {
		if limit != nil && v != compare.Unknown {
			return "rise " + qualifier + shr.Format(nus-bus, bus, *limit, limitSide(v))
		}

		return "rise " + qualifier + round(nus-bus, bus)
	}

	switch {
	case !b.LowerBound && !n.LowerBound && nus < bus:
		return "fall " + nearestTenth(bus-nus, bus)
	case !b.LowerBound && !n.LowerBound:
		return rise("", nearestTenth)
	case n.LowerBound && nus < bus:
		return "fall <= " + tenthUp(bus-nus, bus)
	case n.LowerBound:
		return rise(">= ", tenthDown)
	case nus < bus:
		return "fall >= " + tenthDown(bus-nus, bus)
	case nus > bus:
		return rise("<= ", tenthUp)
	}

	return "fall >= " + tenthDown(0, bus)
}

// limitSide is the side of the limit a decided check fell on.
func limitSide(v compare.Verdict) shr.Verdict {
	if v == compare.Fail {
		return shr.Breaks
	}

	return shr.Holds
}

func shareOf(m *jsonMethod) compare.Share {
	return compare.Share{
		Bad:       m.Failed + m.NotSentConnection,
		BadClient: m.RequestError.Count + m.ClientError + m.BadResponse.Count,
		Total:     m.Sent + m.NotSentConnection,
	}
}

// errorsLine is the line of a method's share of failed calls.
func errorsLine(pair methodPair, limit *shr.Percent, same bool, words string) checkLine {
	b, n := shareOf(pair.side[0]), shareOf(pair.side[1])
	text := pair.name + ": errors " + failedShareText(b) + " -> " + failedShareText(n)

	verdict := compare.Unknown

	if same && words == "" && limit != nil {
		var reason string

		verdict, reason = compare.ErrorsRise(b, n, *limit)

		switch reason {
		case compare.ReasonNothingSent:
			words = "base: nothing sent"
			if b.Total != 0 {
				words = "new: nothing sent"
			}
		case compare.ReasonClientErrors:
			words = "client-side errors decide it"
		default:
			words = reason
		}
	}

	change := "rise -"
	if same {
		change = errorsChange(b, n, limit, verdict)
	}

	return finishLine(text, change, limit, verdict, words)
}

func failedShareText(s compare.Share) string {
	if s.Total == 0 {
		return "-"
	}

	return nearestTenth(s.Bad, s.Total)
}

// errorsChange is the change in the share of failed calls in points, from the
// exact difference of the two fractions: counts near 10^9 on both sides do not
// fit an int64 product.
func errorsChange(b, n compare.Share, limit *shr.Percent, v compare.Verdict) string {
	if b.Total == 0 || n.Total == 0 {
		return "rise -"
	}

	diff := new(big.Rat).Sub(big.NewRat(int64(n.Bad), int64(n.Total)), big.NewRat(int64(b.Bad), int64(b.Total)))
	plain := shr.Percent{Text: "0%"}

	switch diff.Sign() {
	case 0:
		return "no change"
	case -1:
		return "fall " + shr.FormatFraction(diff.Neg(diff), plain, shr.Holds)
	}

	if limit != nil && v != compare.Unknown {
		return "rise " + shr.FormatFraction(diff, *limit, limitSide(v))
	}

	return "rise " + shr.FormatFraction(diff, plain, shr.Holds)
}
