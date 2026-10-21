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
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/metrics"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

// rowTokens are the limit, the verdict and the value of a threshold row, read
// off the text: the word after the comparison, the word after that, and the
// word after it. The layout between them is not checked.
func rowTokens(t *testing.T, row string) (limit, verdict, value string) {
	t.Helper()

	fields := strings.Fields(row)
	for i, f := range fields {
		if f == "<=" && i+3 < len(fields) {
			if fields[i+3] == ">" && i+4 < len(fields) {
				// The "> L" form of a share over a limit that no digit kept is over.
				return fields[i+1], fields[i+2], "> " + fields[i+4]
			}

			return fields[i+1], fields[i+2], fields[i+3]
		}
	}
	t.Fatalf("no \"<= limit verdict value\" in the row %q", row)

	return "", "", ""
}

func parseLatencyToken(t *testing.T, s string) (time.Duration, bool) {
	t.Helper()

	lowerBound := strings.HasPrefix(s, ">")

	d, err := time.ParseDuration(strings.TrimPrefix(s, ">"))
	if err != nil {
		t.Fatalf("latency token %q is not a duration: %v", s, err)
	}

	return d, lowerBound
}

// thousandths is a percent token such as "0.125%" in thousandths of a percent.
func thousandths(t *testing.T, s string) int64 {
	t.Helper()

	f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	if err != nil {
		t.Fatalf("percent token %q is not a number: %v", s, err)
	}

	return int64(math.Round(f * 1000))
}

// Ground: contract — a row never contradicts its verdict. The limit is printed
// exactly as set; the value is read against it from the text alone: a pass is
// at or under the printed limit, a fail is over it, whatever the rounding of
// the value would have said (P7).
func TestThresholdsText_ARowDoesNotContradictItsVerdict(t *testing.T) {
	latency := func(limit time.Duration, value time.Duration, exact bool, v threshold.Verdict) threshold.Result {
		return threshold.Result{
			Method: thresholdMethod, Key: threshold.P99, Verdict: v, Limit: limit,
			Value: metrics.Quantile{Value: value, Exact: exact, Defined: true},
		}
	}
	share := func(limit string, bad, total int, v threshold.Verdict) threshold.Result {
		return threshold.Result{Method: thresholdMethod, Key: threshold.Errors, Verdict: v, LimitPercent: limit, Bad: bad, Total: total}
	}

	for _, tc := range []struct {
		name      string
		result    threshold.Result
		key       string
		wantLimit string
	}{
		{"a: 200.4ms over 200ms rounds to 200ms", latency(200*time.Millisecond, 200400*time.Microsecond, true, threshold.VerdictFail), "p99", "200ms"},
		{"b: 199.9496ms under 199950us rounds to 200ms", latency(199950*time.Microsecond, 199949600*time.Nanosecond, true, threshold.VerdictPass), "p99", "199.95ms"},
		{"a lower bound over the limit rounds down onto it", latency(200*time.Millisecond, 200400*time.Microsecond, false, threshold.VerdictFail), "p99", "200ms"},
		{"c: 0.13% over 0.125% prints as 0.1%", share("0.125%", 13, 10000, threshold.VerdictFail), "errors", "0.125%"},
		{"g: 1.001% over 1% prints as 1.001%", share("1%", 1001, 100000, threshold.VerdictFail), "errors", "1%"},
		{"h: 1.000001% over 1% prints as > 1%", share("1%", 1000001, 100000000, threshold.VerdictFail), "errors", "1%"},
		{"d: 0.96% under 0.99% prints as 1.0%", share("0.99%", 96, 10000, threshold.VerdictPass), "errors", "0.99%"},
		{"the plain cases stay plain: 312ms over 200ms", latency(200*time.Millisecond, 312*time.Millisecond, true, threshold.VerdictFail), "p99", "200ms"},
		{"the plain cases stay plain: 10.3% over 1%", share("1%", 3100, 30000, threshold.VerdictFail), "errors", "1%"},
	} {
		row := rowOf(t, printedWithLimits(thresholdRun(tc.result)), tc.key)
		limit, verdict, value := rowTokens(t, row)

		if limit != tc.wantLimit {
			t.Errorf("%s: limit printed %q, want exactly %q (row %q)", tc.name, limit, tc.wantLimit, row)
		}
		if verdict != string(tc.result.Verdict) {
			t.Errorf("%s: verdict word %q, want %q (row %q)", tc.name, verdict, tc.result.Verdict, row)
		}

		var over bool

		if tc.key == "errors" && strings.HasPrefix(value, "> ") {
			// "> L" says over the limit, and says it with the limit as written.
			if value != "> "+limit {
				t.Errorf("%s: value printed %q, want \"> \" and the limit %q (row %q)", tc.name, value, limit, row)
			}
			over = true
		} else if tc.key == "errors" {
			over = thousandths(t, value) > thousandths(t, limit)
		} else {
			lim, _ := parseLatencyToken(t, limit)
			val, _ := parseLatencyToken(t, value)
			over = val > lim
		}

		switch {
		case tc.result.Verdict == threshold.VerdictPass && over:
			t.Errorf("%s: a pass whose printed value %q is over the printed limit %q (row %q)", tc.name, value, limit, row)
		case tc.result.Verdict == threshold.VerdictFail && !over:
			t.Errorf("%s: a fail whose printed value %q is not over the printed limit %q (row %q)", tc.name, value, limit, row)
		}
	}
}

// Ground: contract — the share beside a FAIL in a row is the exact share
// rounded to the nearest with the digits it takes (1.001% against 1%), and
// "> 1%" when no digit kept is over the limit; a tenth is printed as before.
func TestThresholdsText_AFailShareIsNotOverstated(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bad, total int
		want       string
	}{
		{"1.001% over 1%", 1001, 100000, "1.001%"},
		{"1.000001% over 1%", 1000001, 100000000, "> 1%"},
		{"a tenth is enough: 10.3%", 3100, 30000, "10.3%"},
	} {
		r := threshold.Result{Method: thresholdMethod, Key: threshold.Errors, Verdict: threshold.VerdictFail, LimitPercent: "1%", Bad: tc.bad, Total: tc.total}

		row := rowOf(t, printedWithLimits(thresholdRun(r)), "errors")
		if _, _, value := rowTokens(t, row); value != tc.want {
			t.Errorf("%s: value printed %q, want %q (row %q)", tc.name, value, tc.want, row)
		}
	}
}

// Ground: contract — a latency that needs no correction is printed as the
// table prints it, with three significant figures, so that the row and the
// table above it show the same number.
func TestThresholdsText_ACorrectionOnlyWhereTheRowWouldLie(t *testing.T) {
	r := threshold.Result{
		Method: thresholdMethod, Key: threshold.P99, Verdict: threshold.VerdictFail, Limit: 200 * time.Millisecond,
		Value: metrics.Quantile{Value: 312345 * time.Microsecond, Exact: true, Defined: true},
	}

	if _, _, value := rowTokens(t, rowOf(t, printedWithLimits(thresholdRun(r)), "p99")); value != "312ms" {
		t.Errorf("value printed %q, want 312ms as in the table", value)
	}
}
