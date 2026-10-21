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

package share

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// Percent is a share written in the config, kept in thousandths of a percent
// so that comparisons are integer: 0.7% is 700. Text is it as written; empty
// Text is a Percent nobody wrote.
type Percent struct {
	Thousandths int64
	Text        string
}

// Default is the limit of a search that sets none.
var Default = Percent{Thousandths: 1000, Text: "1%"}

// Verdict is which side of the limit a run's numbers fall on.
type Verdict int

const (
	// Undecided: the target's own failures are within the limit, all of them
	// together are not; the rest are the client's, which say nothing of the
	// target.
	Undecided Verdict = iota
	// Holds: all failures together are within the limit.
	Holds
	// Breaks: the target's own failures are over the limit.
	Breaks
)

// percentText is a share as written: up to three digits before and after the
// point, and the sign.
var percentText = regexp.MustCompile(`^(\d{1,3})(?:\.(\d{1,3}))?%$`)

// scale is 100% in thousandths of a percent: bad of total is within P
// thousandths of a percent when bad*scale <= P*total.
const scale = 100 * 1000

// Parse reads written such as 0.125% for the config key key, which a refusal
// names: up to three digits after the point, at most 100%.
func Parse(written, key string) (Percent, error) {
	groups := percentText.FindStringSubmatch(written)
	if groups == nil {
		return Percent{}, fmt.Errorf("%s is %s, want a percent such as 1%% or 0.5%%: up to 3 digits after the point, at most 100%%; a number without %% is not guessed", key, written)
	}

	whole, _ := strconv.Atoi(groups[1])
	fraction, _ := strconv.Atoi((groups[2] + "000")[:3])

	thousandths := int64(whole)*1000 + int64(fraction)
	if thousandths > scale {
		return Percent{}, fmt.Errorf("%s %s is over 100%%", key, written)
	}

	return Percent{Thousandths: thousandths, Text: written}, nil
}

// Decide judges bad failed calls of total, badClient of them the client's
// (rejected requests, client errors, our bad responses). Nothing sent holds.
func Decide(bad, badClient, total int, limit Percent) Verdict {
	allowed := limit.Thousandths * int64(total)

	switch {
	case int64(bad)*scale <= allowed:
		return Holds
	case int64(bad-badClient)*scale > allowed:
		return Breaks
	default:
		return Undecided
	}
}

// Format is the share bad/total as a percent to print beside limit, rounded
// to the nearest: to a tenth of a percent, or, when that lands on the wrong
// side of the printed limit, for Holds to as many digits as the limit has, for
// Breaks to the first of two and three digits that is over it, or "> " and the
// limit as written when neither is. A share beside a FAIL is never rounded up.
func Format(bad, total int, limit Percent, v Verdict) string {
	if total <= 0 {
		return "-"
	}

	return FormatFraction(big.NewRat(int64(bad), int64(total)), limit, v)
}

// FormatAt is the share bad/total to the nearest 10^-decimals percent, 3
// decimals at most. total is positive.
func FormatAt(bad, total, decimals int) string {
	return fixed(at(big.NewRat(int64(bad), int64(total)), decimals, nearest), decimals) + "%"
}

// FormatFraction is Format for a share that is an exact fraction r (1/2 is
// 50%), such as the difference of two shares with different totals, whose
// numerator an int64 cannot hold. r is not negative.
func FormatFraction(r *big.Rat, limit Percent, v Verdict) string {
	text, _ := FormatFractionDigits(r, limit, v)

	return text
}

// FormatFractionDigits is FormatFraction with the digits after the point the
// printed number holds, 3 for the "> L" form.
func FormatFractionDigits(r *big.Rat, limit Percent, v Verdict) (text string, digits int) {
	tenth := at(r, 1, nearest)
	over := tenth*100 > limit.Thousandths
	decimals := max(1, digitsAfterPoint(limit.Text))

	switch {
	case v == Holds && over:
		return fixed(at(r, decimals, nearest), decimals) + "%", decimals
	case v == Breaks && !over:
		for d := 2; d <= 3; d++ {
			if n := at(r, d, nearest); n*unitOf(d) > limit.Thousandths {
				return fixed(n, d) + "%", d
			}
		}

		return "> " + limit.Text, 3
	}

	return fixed(tenth, 1) + "%", 1
}

// FormatBound is the change r, a fraction (1/2 is 50%), printed as a bound
// that is true of the exact change, with its qualifier: "<= 33.4%" for an
// upper bound (upper is set), ">= 20.04%" for a lower one, or "> 20%", the
// limit as written, when no digits kept are over it. limit is nil when there
// is none; with a limit, Undecided is an unknown verdict. r is not negative.
//
// An upper bound is rounded up and a lower bound down, to a tenth of a
// percent, whatever the verdict. Beside a limit the digits follow the
// verdict: an upper bound beside Holds that a tenth would put over the limit
// takes as many digits as the limit has (r is within the limit, so that is
// not over it); a lower bound beside Breaks that a tenth would leave within
// the limit takes two, then three digits, and past that prints the limit with
// ">". limit is the limit r is judged by: Holds means r is within it, Breaks
// that r is over it.
func FormatBound(r *big.Rat, upper bool, limit *Percent, v Verdict) string {
	decided := limit != nil && v != Undecided

	if upper {
		decimals := 1
		n := at(r, decimals, ceil)

		if decided && v == Holds && n*unitOf(1) > limit.Thousandths {
			decimals = max(1, digitsAfterPoint(limit.Text))
			n = at(r, decimals, ceil)
		}

		return "<= " + fixed(n, decimals) + "%"
	}

	tenth := at(r, 1, floor)

	if decided && v == Breaks && tenth*unitOf(1) <= limit.Thousandths {
		for decimals := 2; decimals <= 3; decimals++ {
			if n := at(r, decimals, floor); n*unitOf(decimals) > limit.Thousandths {
				return ">= " + fixed(n, decimals) + "%"
			}
		}

		return "> " + limit.Text
	}

	return ">= " + fixed(tenth, 1) + "%"
}

func digitsAfterPoint(written string) int {
	_, fraction, _ := strings.Cut(strings.TrimSuffix(written, "%"), ".")

	return len(fraction)
}

type rounding int

const (
	nearest rounding = iota // half up
	ceil
	floor
)

// unitOf is the thousandths of a percent in one unit of 10^-decimals percent.
func unitOf(decimals int) int64 {
	unit := int64(1)
	for range 3 - decimals {
		unit *= 10
	}

	return unit
}

// at is the share bad/total in units of 10^-decimals percent, rounded as
// asked. Integer arithmetic: a float would round 0.125 either way.
func at(r *big.Rat, decimals int, how rounding) int64 {
	num := new(big.Int).Mul(r.Num(), big.NewInt(scale))
	den := new(big.Int).Mul(r.Denom(), big.NewInt(unitOf(decimals)))

	switch how {
	case ceil:
		num.Add(num, den).Sub(num, big.NewInt(1))
	case nearest:
		num.Lsh(num, 1).Add(num, den)
		den.Lsh(den, 1)
	case floor:
	}

	return num.Div(num, den).Int64()
}

// fixed writes n, holding decimals decimals, as a number: 104 with 1 is 10.4.
func fixed(n int64, decimals int) string {
	k := int64(1)
	for range decimals {
		k *= 10
	}

	return fmt.Sprintf("%d.%0*d", n/k, decimals, n%k)
}
