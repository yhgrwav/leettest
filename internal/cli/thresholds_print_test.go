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

		switch {
		case tc.key == "errors" && strings.HasPrefix(value, "> "):
			// "> L" says over the limit, and says it with the limit as written.
			if value != "> "+limit {
				t.Errorf("%s: value printed %q, want \"> \" and the limit %q (row %q)", tc.name, value, limit, row)
			}
			over = true
		case tc.key == "errors":
			over = thousandths(t, value) > thousandths(t, limit)
		default:
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

// Ground: contract — beside a FAIL the row shows the first of three texts that
// is over the limit: the table's three figures, the value at whole
// microseconds (nearest for a measured value, down for a lower bound), the
// value to the nanosecond. A lower bound keeps its ">". The row never says a
// value "100us" is over a limit "100us".
func TestThresholdsText_AFailLatencyShowsWhatMakesItOver(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit time.Duration
		value time.Duration
		exact bool
		want  string
	}{
		{"to the nanosecond: 100.4us over 100us", 100 * time.Microsecond, 100400 * time.Nanosecond, true, "100.4us"},
		{"whole microseconds: 200.4003ms over 200ms", 200 * time.Millisecond, 200400300 * time.Nanosecond, true, "200.4ms"},
		{"whole microseconds, nearest: 1.0005ms over 1ms", time.Millisecond, 1000500 * time.Nanosecond, true, "1.001ms"},
		{"a lower bound to the nanosecond: 200.0006ms over 200ms", 200 * time.Millisecond, 200000600 * time.Nanosecond, false, ">200.0006ms"},
		{"the table's figures are enough: 312ms over 200ms", 200 * time.Millisecond, 312 * time.Millisecond, true, "312ms"},
	} {
		r := threshold.Result{
			Method: thresholdMethod, Key: threshold.P99, Verdict: threshold.VerdictFail, Limit: tc.limit,
			Value: metrics.Quantile{Value: tc.value, Exact: tc.exact, Defined: true},
		}

		row := rowOf(t, printedWithLimits(thresholdRun(r)), "p99")
		if _, verdict, value := rowTokens(t, row); verdict != "fail" || value != tc.want {
			t.Errorf("%s: verdict %q value %q, want fail and %q (row %q)", tc.name, verdict, value, tc.want, row)
		}
	}
}

// refSignificant is the table's text of n nanoseconds (n >= 1000, under 10s):
// three significant figures, rounded to the nearest, or down when roundUp is
// false (a lower bound). It also returns the nanoseconds the text stands for.
func refSignificant(t *testing.T, n int64, roundUp bool) (text string, ns int64) {
	t.Helper()

	for _, u := range []struct {
		size int64
		name string
	}{{1e3, "us"}, {1e6, "ms"}, {1e9, "s"}} {
		for dec, k := 2, int64(100); dec >= 0; dec, k = dec-1, k/10 {
			v := n * k / u.size
			if roundUp {
				v = (n*k + u.size/2) / u.size
			}
			if v >= 1000 {
				continue
			}

			figures := strconv.FormatInt(v, 10)
			if dec > 0 {
				figures = fmt.Sprintf("%d.%0*d", v/k, dec, v%k)
			}

			return figures + u.name, v * u.size / k
		}
	}
	t.Fatalf("the reference prints values from 1us to under 10s, got %dns", n)

	return "", 0
}

// refTrimmed is n nanoseconds in the unit its size picks (us, ms, s) with every
// digit of n and no trailing zero: 100400 is "100.4us", 1000000001 is
// "1.000000001s".
func refTrimmed(n int64) string {
	size, name := int64(1e9), "s"

	switch {
	case n < 1e3:
		return strconv.FormatInt(n, 10) + "ns"
	case n < 1e6:
		size, name = 1e3, "us"
	case n < 1e9:
		size, name = 1e6, "ms"
	}

	whole, frac := n/size, n%size
	if frac == 0 {
		return strconv.FormatInt(whole, 10) + name
	}

	width := len(strconv.FormatInt(size, 10)) - 1

	return fmt.Sprintf("%d.%s%s", whole, strings.TrimRight(fmt.Sprintf("%0*d", width, frac), "0"), name)
}

// Ground: contract — beside a FAIL the row is never understated and never says
// more than it takes, beside a pass never over the limit, whatever the value
// within 2us of the limit. The expected text is made here from integer
// nanoseconds, as the chain 3 figures -> whole microseconds -> nanoseconds
// (the first one over the limit); the sweep reaches every link for a measured
// value and for a lower bound.
func TestThresholdsText_AFailLatencyIsNotOverstated(t *testing.T) {
	const (
		stageFigures = iota
		stageMicroseconds
		stageNanoseconds
	)

	var reached [2][3]int // [exact?][stage] over the fails

	bad := 0

	for _, limit := range []time.Duration{
		100 * time.Microsecond, time.Millisecond, 200 * time.Millisecond, time.Second, 199950 * time.Microsecond,
	} {
		for _, exact := range []bool{true, false} {
			for v := limit.Nanoseconds() - 2000; v <= limit.Nanoseconds()+2000; v++ {
				var (
					want    string
					verdict = threshold.VerdictFail
					reason  threshold.Reason
				)

				figures, figuresNs := refSignificant(t, v, exact)
				prefix := ""
				if !exact {
					prefix = ">"
				}

				switch {
				case v > limit.Nanoseconds():
					micros := v / 1000
					if exact {
						micros = (v + 500) / 1000
					}

					stage, text := stageNanoseconds, refTrimmed(v)

					switch {
					case figuresNs > limit.Nanoseconds():
						stage, text = stageFigures, figures
					case micros*1000 > limit.Nanoseconds():
						stage, text = stageMicroseconds, refTrimmed(micros*1000)
					}

					want = prefix + text
					if exact {
						reached[1][stage]++
					} else {
						reached[0][stage]++
					}
				case exact:
					// A pass: the figures, or the microseconds cut off the value
					// when the figures would round up over the limit.
					verdict, want = threshold.VerdictPass, figures
					if figuresNs > limit.Nanoseconds() {
						want = refTrimmed(v / 1000 * 1000)
					}
				default:
					// A bound not over the limit decides nothing; its figures,
					// rounded down, are never over it.
					verdict, reason, want = threshold.VerdictUnknown, threshold.ReasonCensored, prefix+figures
				}

				r := threshold.Result{
					Method: thresholdMethod, Key: threshold.P99, Verdict: verdict, Reason: reason, Limit: limit,
					Value: metrics.Quantile{Value: time.Duration(v), Exact: exact, Defined: true},
				}

				got := latencyText(&r)

				printed, err := time.ParseDuration(strings.TrimPrefix(got, ">"))
				over := printed > limit

				switch {
				case err != nil, strings.HasPrefix(got, ">") == exact,
					verdict == threshold.VerdictFail && !over, verdict == threshold.VerdictPass && over,
					got != want:
					bad++
					if bad <= 10 {
						t.Errorf("limit %v, %dns exact=%v, %s: printed %q, want %q (parses: %v, over the limit: %v)",
							limit, v, exact, verdict, got, want, err, over)
					}
				}
			}
		}
	}

	if bad > 10 {
		t.Errorf("%d rows in all printed wrong", bad)
	}

	for i, kind := range []string{"a lower bound", "a measured value"} {
		for stage, name := range []string{"three figures", "whole microseconds", "nanoseconds"} {
			if reached[i][stage] == 0 {
				t.Errorf("the sweep never reaches %s for %s beside a fail: the check does not cover that link", name, kind)
			}
		}
	}
}
