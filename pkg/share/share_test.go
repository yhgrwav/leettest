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

package share_test

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/share"
)

// Ground: contract — one grammar for every key that takes a share: whole
// percent with up to three digits after the point, a sign, at most 100%. A
// refusal names the key it was read for.
func TestParse_OneGrammarForEveryKey(t *testing.T) {
	for text, want := range map[string]int64{
		"1%": 1000, "0.5%": 500, "0.125%": 125, "100%": 100000, "0%": 0, "99.999%": 99999,
	} {
		got, err := share.Parse(text, "breakpoint.errors_limit")
		if err != nil || got.Thousandths != want || got.Text != text {
			t.Errorf("%q = %+v, %v, want %d thousandths, text as written", text, got, err, want)
		}
	}

	for _, text := range []string{"0.1255%", "1", "101%", " 1%", "1% ", "1 %", "", "%", "-1%", "1.%", ".5%", "100.001%", "1e1%", "1,5%"} {
		_, err := share.Parse(text, "breakpoint.errors_limit")
		if err == nil || !strings.Contains(err.Error(), "breakpoint.errors_limit") {
			t.Errorf("%q: err %v, want a refusal naming breakpoint.errors_limit", text, err)
		}
	}

	if _, err := share.Parse("101%", "pass.errors"); err == nil || !strings.Contains(err.Error(), "pass.errors") {
		t.Errorf("err %v, want the refusal to name the key it was asked for, pass.errors", err)
	}
}

// Ground: boundary — exactly at the limit holds (≤), one call over breaks;
// the default is 1%.
func TestDecide_ExactlyAtTheLimitHolds(t *testing.T) {
	for _, tc := range []struct {
		name               string
		bad, client, total int
		limit              share.Percent
		want               share.Verdict
	}{
		{"10 of 1000 at 1%", 10, 0, 1000, share.Default, share.Holds},
		{"11 of 1000 at 1%", 11, 0, 1000, share.Default, share.Breaks},
		{"none", 0, 0, 1000, share.Default, share.Holds},
		{"0.1% limit, 1 of 1000", 1, 0, 1000, share.Percent{Thousandths: 100, Text: "0.1%"}, share.Holds},
		{"0.1% limit, 2 of 1000", 2, 0, 1000, share.Percent{Thousandths: 100, Text: "0.1%"}, share.Breaks},
		{"0% limit, one failure", 1, 0, 100000, share.Percent{Thousandths: 0, Text: "0%"}, share.Breaks},
		{"0% limit, none", 0, 0, 100000, share.Percent{Thousandths: 0, Text: "0%"}, share.Holds},
		{"100% limit, all failed", 1000, 0, 1000, share.Percent{Thousandths: 100000, Text: "100%"}, share.Holds},
		{"nothing sent", 0, 0, 0, share.Default, share.Holds},
	} {
		if got := share.Decide(tc.bad, tc.client, tc.total, tc.limit); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Ground: boundary — what the client got wrong says nothing of the target:
// over the limit with those, within it without them, nothing is decided; over
// it without them, the target broke.
func TestDecide_FailuresOfTheClientDecideNothing(t *testing.T) {
	for _, tc := range []struct {
		name               string
		bad, client, total int
		want               share.Verdict
	}{
		{"all within the limit", 10, 10, 1000, share.Holds},
		{"all client's, over", 20, 20, 1000, share.Undecided},
		{"target's within, all over", 14, 5, 1000, share.Undecided},
		{"target's exactly at the limit", 15, 5, 1000, share.Undecided},
		{"target's one over", 16, 5, 1000, share.Breaks},
		{"target's alone over", 20, 0, 1000, share.Breaks},
	} {
		if got := share.Decide(tc.bad, tc.client, tc.total, share.Default); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Ground: contract — the printed share never lands on the wrong side of the
// printed limit: a tenth of a percent when that is enough, more digits when
// it is not, rounded up for Breaks.
func TestFormat_NeverOnTheWrongSideOfTheLimit(t *testing.T) {
	limit := func(text string, thousandths int64) share.Percent {
		return share.Percent{Thousandths: thousandths, Text: text}
	}
	for _, tc := range []struct {
		name       string
		bad, total int
		limit      share.Percent
		verdict    share.Verdict
		want       string
	}{
		{"plain", 11, 1000, share.Default, share.Breaks, "1.1%"},
		{"at the limit", 10, 1000, share.Default, share.Holds, "1.0%"},
		{"1.001% breaks, a tenth would say 1.0%", 1001, 100000, share.Default, share.Breaks, "1.1%"},
		{"0.13% breaks 0.12%, a tenth would say 0.1%", 13, 10000, limit("0.12%", 120), share.Breaks, "0.13%"},
		{"0.15% holds 0.15%, a tenth would round up to 0.2%", 15, 10000, limit("0.15%", 150), share.Holds, "0.15%"},
		{"undecided is a tenth", 20, 1000, share.Default, share.Undecided, "2.0%"},
		{"100%", 1000, 1000, limit("100%", 100000), share.Holds, "100.0%"},
	} {
		if got := share.Format(tc.bad, tc.total, tc.limit, tc.verdict); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Ground: boundary — a share that is a difference of two fractions with
// totals near a billion does not fit an int64 product, and is printed by the
// same rules as Format: a hair past the limit says more than the limit, on
// the limit says the limit.
func TestFormatFraction_ExactBeyondInt64(t *testing.T) {
	half := share.Percent{Thousandths: 50000, Text: "50%"}

	for _, tc := range []struct {
		name    string
		rat     *big.Rat
		verdict share.Verdict
		want    string
	}{
		{"on the limit", big.NewRat(1, 2), share.Holds, "50.0%"},
		{"a hair past the limit", big.NewRat(1000000001, 2000000000), share.Breaks, "50.1%"},
		{"a hair under holds", big.NewRat(999999999, 2000000000), share.Holds, "50.0%"},
		{"a share of billions of calls", big.NewRat(3999999999*1000000000, 4000000000*1000000000), share.Undecided, "100.0%"},
	} {
		if got := share.FormatFraction(tc.rat, half, tc.verdict); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}

	// The same share through Format, which takes counts.
	if got, want := share.Format(1, 3, share.Default, share.Undecided), share.FormatFraction(big.NewRat(1, 3), share.Default, share.Undecided); got != want {
		t.Errorf("Format %q, FormatFraction %q", got, want)
	}
}

// boundText is a printed bound: its qualifier and the number, in percent.
var boundText = regexp.MustCompile(`^(<=|>=|>) (\d+(?:\.(\d+))?)%$`)

// boundPercent is the percent value written as text, exactly.
func boundPercent(t *testing.T, text string) *big.Rat {
	t.Helper()

	r, ok := new(big.Rat).SetString(text)
	if !ok {
		t.Fatalf("test bug: %q is not a number", text)
	}

	return r
}

// boundRoundAt is the percent p cut to k decimals, down or up: exact, with
// big.Rat, apart from the code under test.
func boundRoundAt(p *big.Rat, k int, up bool) *big.Rat {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(k)), nil)
	q, m := new(big.Int).DivMod(new(big.Int).Mul(p.Num(), scale), p.Denom(), new(big.Int))
	if up && m.Sign() != 0 {
		q.Add(q, big.NewInt(1))
	}

	return new(big.Rat).SetFrac(q, scale)
}

// boundReference is the text the spec asks for, written once more from the
// spec in big.Rat; x is not negative.
func boundReference(x *big.Rat, upper bool, limit *share.Percent, v share.Verdict) string {
	p := new(big.Rat).Mul(x, big.NewRat(100, 1))
	decided := limit != nil && v != share.Undecided

	var over *big.Rat // the limit
	digits := 1
	if limit != nil {
		over = big.NewRat(limit.Thousandths, 1000)
		if _, fraction, found := strings.Cut(strings.TrimSuffix(limit.Text, "%"), "."); found {
			digits = max(1, len(fraction))
		}
	}

	if upper {
		k, s := 1, boundRoundAt(p, 1, true)
		if decided && v == share.Holds && s.Cmp(over) > 0 {
			k, s = digits, boundRoundAt(p, digits, true)
		}

		return "<= " + s.FloatString(k) + "%"
	}

	s := boundRoundAt(p, 1, false)
	if decided && v == share.Breaks && s.Cmp(over) <= 0 {
		for k := 2; k <= 3; k++ {
			if s = boundRoundAt(p, k, false); s.Cmp(over) > 0 {
				return ">= " + s.FloatString(k) + "%"
			}
		}

		return "> " + limit.Text
	}

	return ">= " + s.FloatString(1) + "%"
}

// boundProblem is what is wrong with the printed bound got of the exact
// change x, "" when nothing: it is parsed back to a number and compared with
// x and the limit, never with the code's own arithmetic.
func boundProblem(t *testing.T, x *big.Rat, upper bool, limit *share.Percent, v share.Verdict, got string) string {
	t.Helper()

	m := boundText.FindStringSubmatch(got)
	if m == nil {
		return "not a bound"
	}
	qualifier, printed := m[1], new(big.Rat).Quo(boundPercent(t, m[2]), big.NewRat(100, 1))
	if len(m[3]) > 3 {
		return "more than 3 decimals"
	}

	switch {
	case upper && qualifier != "<=":
		return "an upper bound carries <="
	case !upper && qualifier == "<=":
		return "a lower bound carries >= or >"
	case qualifier == "<=" && printed.Cmp(x) < 0:
		return "<= below the exact change"
	case qualifier == ">=" && printed.Cmp(x) > 0:
		return ">= above the exact change"
	case qualifier == ">" && printed.Cmp(x) >= 0:
		return "> at or above the exact change"
	}

	if limit == nil || v == share.Undecided {
		if qualifier == ">" {
			return "> without a verdict to rest on"
		}

		return ""
	}

	over := big.NewRat(limit.Thousandths, 100000)
	switch {
	case v == share.Holds && qualifier == ">":
		return "> beside a pass"
	case v == share.Holds && printed.Cmp(over) > 0:
		return "beside a pass, over the limit"
	case v == share.Breaks && qualifier == ">" && got != "> "+limit.Text:
		return "> without the limit as written"
	case v == share.Breaks && qualifier != ">" && printed.Cmp(over) <= 0:
		return "beside a FAIL, not over the limit"
	}

	return ""
}

// Ground: contract — a bound printed with `<=` or `>=` is true of the exact
// change and on the verdict's side of the limit: an upper bound rounds up, a
// lower one down, at a tenth of a percent, with more digits only when the
// tenth would be on the wrong side, and `> L` when no digit kept is over L.
// The table pins the cases by hand; the sweep draws changes and limits
// (totals up to 10^7, near the limit, near a tenth, near a thousandth) and
// checks every answer against the exact value and a big.Rat reference.
func TestFormatBound_IsATrueBoundOnTheVerdictsSide(t *testing.T) {
	limitOf := func(text string) *share.Percent {
		p, err := share.Parse(text, "limit")
		if err != nil {
			t.Fatalf("test bug: limit %q: %v", text, err)
		}

		return &p
	}
	rat := big.NewRat

	for _, tc := range []struct {
		name    string
		x       *big.Rat
		upper   bool
		limit   *share.Percent
		verdict share.Verdict
		want    string
	}{
		{"upper, a third, no limit", rat(1, 3), true, nil, share.Undecided, "<= 33.4%"},
		{"lower, a third, no limit", rat(1, 3), false, nil, share.Undecided, ">= 33.3%"},
		{"no limit ignores a verdict, upper", rat(1, 3), true, nil, share.Holds, "<= 33.4%"},
		{"no limit ignores a verdict, lower", rat(1, 3), false, nil, share.Breaks, ">= 33.3%"},
		{"upper, a third, holds", rat(1, 3), true, limitOf("50%"), share.Holds, "<= 33.4%"},
		{"upper, an exact tenth stays", rat(2, 10), true, limitOf("20%"), share.Holds, "<= 20.0%"},
		{"lower, an exact tenth stays", rat(2, 10), false, limitOf("20%"), share.Holds, ">= 20.0%"},
		{"upper, a hair past a tenth, holds", rat(2004, 10000), true, limitOf("21%"), share.Holds, "<= 20.1%"},
		{"upper, the tenth is over a limit with digits", rat(20001, 100000), true, limitOf("20.01%"), share.Holds, "<= 20.01%"},
		{"upper, three digits of the limit", rat(200021, 1000000), true, limitOf("20.003%"), share.Holds, "<= 20.003%"},
		{"upper, up to 100%", rat(1, 1), true, limitOf("100%"), share.Holds, "<= 100.0%"},
		{"upper, zero", rat(0, 1), true, nil, share.Undecided, "<= 0.0%"},
		{"upper, unknown with a limit", rat(2004, 10000), true, limitOf("20%"), share.Undecided, "<= 20.1%"},
		{"upper beside a FAIL is plain", rat(2004, 10000), true, limitOf("20%"), share.Breaks, "<= 20.1%"},
		{"lower beside a pass is plain", rat(1995, 10000), false, limitOf("20%"), share.Holds, ">= 19.9%"},
		{"lower, zero beside a pass", rat(0, 1), false, limitOf("20%"), share.Holds, ">= 0.0%"},
		{"lower, unknown with a limit", rat(2004, 10000), false, limitOf("20%"), share.Undecided, ">= 20.0%"},
		{"lower, past a tenth, FAIL needs two decimals", rat(601, 3000), false, limitOf("20%"), share.Breaks, ">= 20.03%"},
		{"lower, FAIL needs three decimals", rat(20004, 100000), false, limitOf("20%"), share.Breaks, ">= 20.004%"},
		{"lower, FAIL, no digit kept is over the limit", rat(200001, 1000000), false, limitOf("20%"), share.Breaks, "> 20%"},
		{"lower, FAIL on a limit with three digits", rat(2000349, 10000000), false, limitOf("20.003%"), share.Breaks, "> 20.003%"},
		{"lower, FAIL on a limit with three digits, over", rat(200041, 1000000), false, limitOf("20.003%"), share.Breaks, ">= 20.004%"},
		{"lower, FAIL on a zero limit, a hundredth", rat(5, 10000), false, limitOf("0%"), share.Breaks, ">= 0.05%"},
		{"lower, FAIL on a zero limit, under a thousandth", rat(1, 10000000), false, limitOf("0%"), share.Breaks, "> 0%"},
		{"lower, FAIL, the tenth is over", rat(2051, 10000), false, limitOf("20%"), share.Breaks, ">= 20.5%"},
		{"lower, past 100%", rat(5, 2), false, nil, share.Undecided, ">= 250.0%"},
	} {
		got := share.FormatBound(tc.x, tc.upper, tc.limit, tc.verdict)

		if want := boundReference(tc.x, tc.upper, tc.limit, tc.verdict); want != tc.want {
			t.Fatalf("test bug, %s: the table says %q, the reference %q", tc.name, tc.want, want)
		}
		if got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
		if problem := boundProblem(t, tc.x, tc.upper, tc.limit, tc.verdict, got); problem != "" {
			t.Errorf("%s: %q: %s", tc.name, got, problem)
		}
	}

	rng := rand.New(rand.NewPCG(20261020, 1))
	failures := 0

	for range 4000 {
		// A limit with 0-3 digits after the point.
		digits := rng.IntN(4)
		step := int64(1000)
		for range digits {
			step /= 10
		}
		thousandths := rng.Int64N(100000/step+1) * step
		text := fmt.Sprintf("%d%%", thousandths/1000)
		if digits > 0 {
			text = fmt.Sprintf("%d.%0*d%%", thousandths/1000, digits, thousandths%1000/step)
		}
		limit := limitOf(text)
		if limit.Thousandths != thousandths {
			t.Fatalf("test bug: limit %q is %d thousandths, drawn %d", text, limit.Thousandths, thousandths)
		}

		// The change is bad/10^7 of the draws near something, or any fraction
		// with a total up to 10^7.
		offset := rng.Int64N(601) - 300
		if rng.IntN(4) == 0 {
			offset = 0
		}
		var x *big.Rat
		switch rng.IntN(4) {
		case 0:
			total := 1 + rng.Int64N(10000000)
			x = rat(rng.Int64N(2*total+1), total)
		case 1:
			x = rat(max(0, thousandths*100+offset), 10000000)
		case 2:
			x = rat(max(0, rng.Int64N(3001)*10000+offset), 10000000)
		default:
			x = rat(max(0, rng.Int64N(100001)*100+offset), 10000000)
		}

		side := share.Breaks
		if x.Cmp(rat(thousandths, 100000)) <= 0 {
			side = share.Holds
		}

		for _, upper := range []bool{true, false} {
			for _, tc := range []struct {
				limit   *share.Percent
				verdict share.Verdict
			}{
				{nil, share.Undecided}, {nil, share.Holds}, {nil, share.Breaks},
				{limit, share.Undecided}, {limit, side},
			} {
				got := share.FormatBound(x, upper, tc.limit, tc.verdict)

				want := boundReference(x, upper, tc.limit, tc.verdict)
				problem := boundProblem(t, x, upper, tc.limit, tc.verdict, got)
				if got == want && problem == "" {
					continue
				}

				failures++
				if failures > 10 {
					t.Fatalf("more than 10 wrong answers, stopped")
				}

				shown := "none"
				if tc.limit != nil {
					shown = text
				}
				t.Errorf("x %s, upper %v, limit %s, verdict %v: %q, reference %q, problem %q",
					x.String(), upper, shown, tc.verdict, got, want, problem)
			}
		}
	}
}
