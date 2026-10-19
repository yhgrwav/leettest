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
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/yhgrwav/leettest/pkg/threshold"
)

// jsonThreshold is the verdict on one limit, the same twelve keys for every
// key: the ones that do not belong to it are null. Durations are whole
// microseconds, the percent is as the config wrote it.
type jsonThreshold struct {
	// Method is null for a search's limit.
	Method  *string `json:"method"`
	Key     string  `json:"key"`
	Verdict string  `json:"verdict"`
	// Reason says why a verdict is unknown; null for pass and fail.
	Reason *string `json:"reason"`
	// LimitUS and Value belong to a latency limit; Value is null when the
	// percentile was not measured.
	LimitUS *int64        `json:"limit_us"`
	Value   *jsonQuantile `json:"value"`
	// LimitPercent, Bad, BadClient and Total belong to errors; the counts are
	// null when no call was counted.
	LimitPercent *string `json:"limit_percent"`
	Bad          *int    `json:"bad"`
	BadClient    *int    `json:"bad_client"`
	Total        *int    `json:"total"`
	// LimitRPS and HeldRPS belong to a search's limit.
	LimitRPS *int `json:"limit_rps"`
	HeldRPS  *int `json:"held_rps"`
}

func ptrTo[T any](v T) *T { return &v }

// thresholdsJSON is the list a report writes; nil, which writes null, when
// the config set no limits, never an empty list that reads as "all pass".
func thresholdsJSON(results []threshold.Result) *[]jsonThreshold {
	if len(results) == 0 {
		return nil
	}

	out := make([]jsonThreshold, len(results))
	for i := range results {
		out[i] = jsonThresholdOf(&results[i])
	}

	return &out
}

func jsonThresholdOf(r *threshold.Result) jsonThreshold {
	t := jsonThreshold{Key: string(r.Key), Verdict: string(r.Verdict)}
	if r.Method != "" {
		t.Method = ptrTo(r.Method)
	}
	if r.Reason != "" {
		t.Reason = ptrTo(string(r.Reason))
	}

	switch r.Key {
	case threshold.HeldRPS:
		t.LimitRPS, t.HeldRPS = ptrTo(r.LimitRPS), ptrTo(r.HeldRPS)
	case threshold.Errors:
		t.LimitPercent = ptrTo(r.LimitPercent)
		if r.Total > 0 {
			t.Bad, t.BadClient, t.Total = ptrTo(r.Bad), ptrTo(r.BadClient), ptrTo(r.Total)
		}
	case threshold.P50, threshold.P90, threshold.P95, threshold.P99, threshold.P999:
		t.LimitUS = ptrTo(micros(r.Limit))
		t.Value = quantile(r.Value)
	}

	return t
}

// thresholdsUndecided says some limit stayed unknown for a reason of its own:
// not because the run was stopped or already invalid, which the outcome and
// the other invalid reasons say.
func thresholdsUndecided(results []threshold.Result) bool {
	for i := range results {
		r := &results[i]
		if r.Verdict == threshold.VerdictUnknown && r.Reason != threshold.ReasonInvalid && r.Reason != threshold.ReasonIncomplete {
			return true
		}
	}

	return false
}

// reasonWords say in words why a limit is not decided.
var reasonWords = map[threshold.Reason]string{
	threshold.ReasonIncomplete:      "the run was stopped",
	threshold.ReasonInvalid:         "the run is invalid",
	threshold.ReasonFewObservations: "too few observations",
	threshold.ReasonRunLimit:        "the search hit the run's own limit",
	threshold.ReasonGenerator:       "the generator was late",
	threshold.ReasonStream:          "calls waited for a free stream",
	threshold.ReasonClientErrors:    "the client's own failures decide it",
	threshold.ReasonNothingSent:     "nothing was sent",
	threshold.ReasonCensored:        "timeouts in the tail",
}

// thresholdsBlock is the text report's last block: the worst verdict, then a
// row per limit. run is the report the limits were checked against, nil for a
// search. Empty when there are no limits.
func thresholdsBlock(results []threshold.Result, run *RunReport) string {
	if len(results) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString("Thresholds: ")
	b.WriteString(thresholdsHeader(results))
	b.WriteString("\n")

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for i := range results {
		fmt.Fprintf(tw, "  %s\n", strings.Join(thresholdCells(&results[i], run), "\t"))
	}
	_ = tw.Flush()

	return strings.TrimRight(b.String(), "\n")
}

// thresholdsHeader is the worst row's verdict; the count is of the rows with
// that verdict.
func thresholdsHeader(results []threshold.Result) string {
	switch threshold.Overall(results) {
	case threshold.VerdictFail:
		return fmt.Sprintf("FAILED, %d of %d limits missed", countVerdict(results, threshold.VerdictFail), len(results))
	case threshold.VerdictUnknown:
		return fmt.Sprintf("not decided, %d of %d limits could not be checked", countVerdict(results, threshold.VerdictUnknown), len(results))
	case threshold.VerdictPass:
	}

	return "passed"
}

func countVerdict(results []threshold.Result, v threshold.Verdict) int {
	n := 0

	for i := range results {
		if results[i].Verdict == v {
			n++
		}
	}

	return n
}

// thresholdCells are a row's columns: what is checked, the limit, the
// verdict, and what was measured with the reason of an unknown.
func thresholdCells(r *threshold.Result, run *RunReport) []string {
	var cells []string

	if r.Method != "" {
		cells = append(cells, displayMethod(r.Method))
	}
	cells = append(cells, string(r.Key))

	var detail string

	switch r.Key {
	case threshold.HeldRPS:
		cells = append(cells, fmt.Sprintf(">= %d", r.LimitRPS))
		detail = fmt.Sprintf("held %d", r.HeldRPS)
	case threshold.Errors:
		cells = append(cells, "<= "+r.LimitPercent)
		detail = shareText(r, run)
	case threshold.P50, threshold.P90, threshold.P95, threshold.P99, threshold.P999:
		cells = append(cells, "<= "+exactLatency(r.Limit))
		detail = latencyText(r)
	}

	if r.Reason != "" {
		detail += " - " + reasonWords[r.Reason]
	}

	return append(cells, string(r.Verdict), detail)
}

// exactLatency prints whole microseconds with no rounding: 199950us is
// 199.95ms. A limit is printed this way, so it is read as it was set.
func exactLatency(d time.Duration) string {
	us := d.Microseconds()

	switch {
	case us >= 1_000_000:
		return exactUnits(us, 1_000_000, 6) + "s"
	case us >= 1000:
		return exactUnits(us, 1000, 3) + "ms"
	}

	return strconv.FormatInt(us, 10) + "us"
}

// exactUnits is us in units of unit, whose zeros are digits long, without the
// trailing zeros of the fraction.
func exactUnits(us, unit int64, digits int) string {
	whole, frac := us/unit, us%unit
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}

	return strings.TrimRight(fmt.Sprintf("%d.%0*d", whole, digits, frac), "0")
}

// latencyText is the measured percentile as the report prints it. The row
// must not contradict its verdict: a value rounded onto the wrong side of the
// printed limit is written as the whole microseconds the verdict was made on.
func latencyText(r *threshold.Result) string {
	q := r.Value
	if !q.Defined {
		return "-"
	}

	text := formatQuantile(q)

	printed, err := time.ParseDuration(strings.TrimPrefix(text, ">"))
	if err != nil {
		return text
	}

	over := printed > r.Limit
	if (r.Verdict == threshold.VerdictPass && over) || (r.Verdict == threshold.VerdictFail && !over) {
		text = exactLatency(q.Value)
		if !q.Exact {
			text = ">" + text
		}
	}

	return text
}

// shareText is the share of failed calls with the parts it is made of.
func shareText(r *threshold.Result, run *RunReport) string {
	if r.Total == 0 {
		return "-"
	}

	text := shareWithoutLying(r)

	noConn := 0

	if run != nil {
		for i := range run.Methods {
			if run.Methods[i].Method == r.Method {
				noConn = run.Methods[i].NotSentConnection
			}
		}
	}

	parts := ""

	if noConn > 0 || r.BadClient > 0 {
		var p []string
		if failed := r.Bad - noConn; failed > 0 {
			p = append(p, fmt.Sprintf("%d failed", failed))
		}
		if noConn > 0 {
			p = append(p, fmt.Sprintf("%d no connection", noConn))
		}
		if r.BadClient > 0 {
			p = append(p, fmt.Sprintf("%d client side", r.BadClient))
		}
		parts = ": " + strings.Join(p, ", ")
	}

	return fmt.Sprintf("%s (%d of %d%s)", text, r.Bad, r.Total, parts)
}

// percentScale is 100% in thousandths of a percent.
const percentScale = 100 * 1000

// shareWithoutLying prints the share to a tenth of a percent, as the report
// does elsewhere; when that lands on the wrong side of the printed limit, to
// as many digits as the limit has, rounded up for a fail so that it is over.
func shareWithoutLying(r *threshold.Result) string {
	text := shareAt(r, 1, false)

	limit, decimals := percentThousandths(r.LimitPercent)
	printed, _ := percentThousandths(text)
	over := printed > limit

	switch {
	case r.Verdict == threshold.VerdictPass && over:
		return shareAt(r, max(1, decimals), false)
	case r.Verdict == threshold.VerdictFail && !over:
		return shareAt(r, max(1, decimals), true)
	}

	return text
}

// percentThousandths reads a percent such as "0.125%" in thousandths of a
// percent, and the digits after its point. The config only lets through up to
// three.
func percentThousandths(text string) (thousandths int64, decimals int) {
	whole, frac, _ := strings.Cut(strings.TrimSuffix(text, "%"), ".")
	w, _ := strconv.ParseInt(whole, 10, 64)
	f, _ := strconv.ParseInt((frac + "000")[:3], 10, 64)

	return w*1000 + f, len(frac)
}

// shareAt is the share of r with decimals digits after the point, rounded
// half up, or up when up is set. Integer arithmetic: a float would round
// 0.125 either way.
func shareAt(r *threshold.Result, decimals int, up bool) string {
	unit := int64(1)
	for range 3 - decimals {
		unit *= 10
	}

	num, den := int64(r.Bad)*percentScale, unit*int64(r.Total)

	var n int64
	if up {
		n = (num + den - 1) / den
	} else {
		n = (2*num + den) / (2 * den)
	}

	return fixed(n, decimals) + "%"
}
