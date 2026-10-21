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

package compare_test

import (
	"math"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/yhgrwav/leettest/pkg/compare"
	"github.com/yhgrwav/leettest/pkg/share"
)

func exactUS(us int64) compare.Latency {
	return compare.Latency{NS: us * 1000, Defined: true}
}

func lowerUS(us int64) compare.Latency {
	return compare.Latency{NS: us * 1000, LowerBound: true, Defined: true}
}

// Ground: boundary — the p99 rise is (new − base) / base in percent of the
// base, judged by the limit as the unit-1 drop is: exactly on the limit
// passes (≤, as pass:), one microsecond past fails, a fall passes. Integer
// arithmetic, so no float decides a boundary.
func TestP99Rise_Table(t *testing.T) {
	const (
		censored = compare.ReasonCensored
		few      = compare.ReasonFewObservations
		zero     = compare.ReasonBaseZero
	)

	for _, tc := range []struct {
		name     string
		base, nw compare.Latency
		limit    string
		want     compare.Verdict
		reason   string
	}{
		// Both exact.
		{"exactly on the limit", exactUS(100000), exactUS(120000), "20%", compare.Pass, ""},
		{"one microsecond past the limit", exactUS(100000), exactUS(120001), "20%", compare.Fail, ""},
		{"no change", exactUS(100000), exactUS(100000), "20%", compare.Pass, ""},
		{"a fall", exactUS(100000), exactUS(50000), "20%", compare.Pass, ""},
		{"a fall to nothing", exactUS(100000), exactUS(0), "20%", compare.Pass, ""},
		{"a zero limit holds no change", exactUS(100000), exactUS(100000), "0%", compare.Pass, ""},
		{"a zero limit fails one microsecond", exactUS(100000), exactUS(100001), "0%", compare.Fail, ""},
		{"a fraction of a percent, on it", exactUS(1000000), exactUS(1001250), "0.125%", compare.Pass, ""},
		{"a fraction of a percent, one more", exactUS(1000000), exactUS(1001251), "0.125%", compare.Fail, ""},
		{"a small base exactly on the limit", exactUS(5), exactUS(6), "20%", compare.Pass, ""},
		{"a small base one past", exactUS(5), exactUS(7), "20%", compare.Fail, ""},
		{"an hour exactly on the limit", exactUS(3600000000), exactUS(4320000000), "20%", compare.Pass, ""},
		{"an hour one past", exactUS(3600000000), exactUS(4320000001), "20%", compare.Fail, ""},
		{"a limit of 100% doubles", exactUS(100000), exactUS(200000), "100%", compare.Pass, ""},
		{"a limit of 100%, past doubling", exactUS(100000), exactUS(200001), "100%", compare.Fail, ""},

		// New is a lower bound: the true value is larger, so the rise is at
		// least the printed one. Over the limit already — fail; else nothing
		// is known, never a pass.
		{"new bound past the limit", exactUS(100000), lowerUS(120001), "20%", compare.Fail, ""},
		{"new bound far past the limit", exactUS(100000), lowerUS(900000), "20%", compare.Fail, ""},
		{"new bound exactly on the limit", exactUS(100000), lowerUS(120000), "20%", compare.Unknown, censored},
		{"new bound at the base", exactUS(100000), lowerUS(100000), "20%", compare.Unknown, censored},
		{"new bound below the base", exactUS(100000), lowerUS(50000), "20%", compare.Unknown, censored},

		// Base is a lower bound: the true base is larger, so the rise is at
		// most the printed one. Within the limit already — pass; else nothing
		// is known, never a fail.
		{"base bound, rise on the limit", lowerUS(100000), exactUS(120000), "20%", compare.Pass, ""},
		{"base bound, a fall", lowerUS(100000), exactUS(50000), "20%", compare.Pass, ""},
		{"base bound, rise one past", lowerUS(100000), exactUS(120001), "20%", compare.Unknown, censored},
		{"base bound, rise far past", lowerUS(100000), exactUS(900000), "20%", compare.Unknown, censored},

		// Two lower bounds say nothing of a rise.
		{"two bounds, same", lowerUS(100000), lowerUS(100000), "20%", compare.Unknown, censored},
		{"two bounds, new far above", lowerUS(100000), lowerUS(900000), "20%", compare.Unknown, censored},
		{"two bounds, new far below", lowerUS(100000), lowerUS(10), "20%", compare.Unknown, censored},

		// A p99 the method has too few observations for.
		{"base not defined", compare.Latency{}, exactUS(100000), "20%", compare.Unknown, few},
		{"new not defined", exactUS(100000), compare.Latency{}, "20%", compare.Unknown, few},
		{"neither defined", compare.Latency{}, compare.Latency{}, "20%", compare.Unknown, few},
		{"base not defined beside a zero", compare.Latency{NS: 0}, exactUS(100000), "20%", compare.Unknown, few},
		{"new not defined beside a zero base", exactUS(0), compare.Latency{}, "20%", compare.Unknown, few},

		// A base with nothing to take a share of.
		{"base zero, new above", exactUS(0), exactUS(100000), "20%", compare.Unknown, zero},
		{"base zero, new zero", exactUS(0), exactUS(0), "20%", compare.Unknown, zero},
		{"base zero, new a bound", exactUS(0), lowerUS(100000), "20%", compare.Unknown, zero},
		{"base zero bound", lowerUS(0), exactUS(100000), "20%", compare.Unknown, zero},
		{"base below zero", exactUS(-1), exactUS(100000), "20%", compare.Unknown, zero},
	} {
		got, reason := compare.P99Rise(tc.base, tc.nw, limit(tc.limit))
		if got != tc.want || reason != tc.reason {
			t.Errorf("%s: %s %q, want %s %q", tc.name, verdictNames[got], reason, verdictNames[tc.want], tc.reason)
		}
	}
}

// Ground: contract — a check that is not unknown carries no reason, and an
// unknown carries one from the closed list. The first row alone would pass a
// stub that answers unknown with no reason, so it also needs a pass.
func TestP99Rise_ReasonOnlyForUnknown(t *testing.T) {
	closed := map[string]bool{
		compare.ReasonCensored: true, compare.ReasonFewObservations: true, compare.ReasonBaseZero: true,
	}

	var passes int

	for _, tc := range []struct{ base, nw compare.Latency }{
		{exactUS(100000), exactUS(100000)}, {exactUS(100000), exactUS(500000)},
		{exactUS(100000), lowerUS(100000)}, {lowerUS(100000), exactUS(100000)},
		{lowerUS(100000), lowerUS(100000)}, {compare.Latency{}, exactUS(1)}, {exactUS(0), exactUS(1)},
	} {
		got, reason := compare.P99Rise(tc.base, tc.nw, limit("20%"))
		if got == compare.Pass {
			passes++
		}

		switch {
		case got == compare.Unknown && !closed[reason]:
			t.Errorf("%+v: unknown with reason %q, want one of the p99 reasons", tc, reason)
		case got != compare.Unknown && reason != "":
			t.Errorf("%+v: %s with reason %q, want none", tc, verdictNames[got], reason)
		}
	}

	if passes == 0 {
		t.Error("no pass among the rows")
	}
}

// Ground: boundary — a p99 is whole nanoseconds, up to math.MaxInt64, and the
// rise is scaled by 100000 (thousandths of a percent) on both sides of the
// comparison: plain int64 arithmetic wraps. share.Parse accepts at most 100%,
// so the limits here are 100% and 99.999%.
func TestP99Rise_NoOverflow(t *testing.T) {
	const (
		hour = int64(3600e9)
		big4 = int64(4e18)
	)

	ns := func(n int64) compare.Latency { return compare.Latency{NS: n, Defined: true} }

	for _, tc := range []struct {
		name     string
		base, nw int64
		limit    string
		want     compare.Verdict
	}{
		{"an hour doubled, on a limit of 100%", hour, 2 * hour, "100%", compare.Pass},
		{"an hour doubled and a nanosecond, past 100%", hour, 2*hour + 1, "100%", compare.Fail},
		{"4e18 ns doubled, on a limit of 100%", big4, 2 * big4, "100%", compare.Pass},
		{"4e18 ns doubled and a nanosecond, past 100%", big4, 2*big4 + 1, "100%", compare.Fail},
		{"1e18 ns, on a limit of 99.999%", 1e18, 1999990000000000000, "99.999%", compare.Pass},
		{"1e18 ns, a nanosecond past 99.999%", 1e18, 1999990000000000001, "99.999%", compare.Fail},
		{"half of int64 to int64, past 99.999%", math.MaxInt64 / 2, math.MaxInt64, "99.999%", compare.Fail},
		{"half of int64 to int64, past 100%", math.MaxInt64 / 2, math.MaxInt64, "100%", compare.Fail},
		{"one nanosecond to int64, past 100%", 1, math.MaxInt64, "100%", compare.Fail},
		{"int64 to int64, no change", math.MaxInt64, math.MaxInt64, "0%", compare.Pass},
		{"int64 to one nanosecond, a fall", math.MaxInt64, 1, "0%", compare.Pass},
	} {
		got, reason := compare.P99Rise(ns(tc.base), ns(tc.nw), limit(tc.limit))
		if got != tc.want || reason != "" {
			t.Errorf("%s: %s %q, want %s", tc.name, verdictNames[got], reason, verdictNames[tc.want])
		}
	}
}

// Ground: boundary — for any pair of non-negative int64 nanoseconds and any
// limit in thousandths of a percent that share.Parse accepts, the verdict of
// two exact p99s is that of the exact fractions, as a math/big reference
// computes it. Seeded: a failure names the pair.
func TestP99Rise_AgreesWithExactArithmetic(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261020, 1))

	// A value of a random magnitude, from a nanosecond to int64.
	value := func(least int64) int64 {
		v := rng.Int64N(math.MaxInt64) >> rng.UintN(63)

		return max(v, least)
	}

	var fails, passes int

	for range 20000 {
		base, nw := value(1), value(0)
		limitThousandths := rng.Int64N(100001)

		// Half of the pairs sit within a nanosecond of the limit itself.
		if rng.IntN(2) == 0 {
			edge := new(big.Int).Mul(big.NewInt(base), big.NewInt(limitThousandths))
			edge.Quo(edge, big.NewInt(100000))
			edge.Add(edge, big.NewInt(base))

			if edge.IsInt64() {
				nw = max(edge.Int64()+rng.Int64N(3)-1, 0)
			}
		}

		over := new(big.Int).Mul(big.NewInt(nw-base), big.NewInt(100000)).
			Cmp(new(big.Int).Mul(big.NewInt(limitThousandths), big.NewInt(base))) > 0

		want := compare.Pass
		if over {
			want = compare.Fail
		}

		got, reason := compare.P99Rise(
			compare.Latency{NS: base, Defined: true}, compare.Latency{NS: nw, Defined: true},
			share.Percent{Thousandths: limitThousandths})
		if got != want || reason != "" {
			t.Fatalf("base %d ns, new %d ns, limit %d thousandths: %s %q, want %s", base, nw, limitThousandths, verdictNames[got], reason, verdictNames[want])
		}

		if over {
			fails++
		} else {
			passes++
		}
	}

	// A reference that never failed, or never passed, would prove half.
	if fails < 1000 || passes < 1000 {
		t.Errorf("the pairs gave %d fails and %d passes, want both sides well covered", fails, passes)
	}
}

// Ground: boundary — the error share rises in points of share (new share −
// base share), judged by the limit: exactly on the limit passes. Client-side
// errors make the check an interval: it fails only on the target's failures
// (new side Bad − BadClient, base side Bad), passes only on all of them (new
// side Bad, base side Bad − BadClient), and is otherwise unknown.
func TestErrorsRise_Table(t *testing.T) {
	const (
		nothing = compare.ReasonNothingSent
		client  = compare.ReasonClientErrors
	)

	calls := func(bad, total int) compare.Share { return compare.Share{Bad: bad, Total: total} }

	for _, tc := range []struct {
		name     string
		base, nw compare.Share
		limit    string
		want     compare.Verdict
		reason   string
	}{
		// No client-side errors: a plain rise in points.
		{"exactly on the limit", calls(10, 1000), calls(20, 1000), "1%", compare.Pass, ""},
		{"one call past the limit", calls(10, 1000), calls(21, 1000), "1%", compare.Fail, ""},
		{"no change", calls(10, 1000), calls(10, 1000), "1%", compare.Pass, ""},
		{"a fall", calls(10, 1000), calls(0, 1000), "1%", compare.Pass, ""},
		{"a zero limit holds no change", calls(10, 1000), calls(10, 1000), "0%", compare.Pass, ""},
		{"a zero limit fails one call", calls(10, 1000), calls(11, 1000), "0%", compare.Fail, ""},
		{"nothing failed on either side", calls(0, 1000), calls(0, 1000), "0%", compare.Pass, ""},
		{"all failed on both sides", calls(1000, 1000), calls(1000, 1000), "0%", compare.Pass, ""},
		{"a limit of 100%", calls(0, 1000), calls(1000, 1000), "100%", compare.Pass, ""},

		// Different totals: the shares are compared, not the counts.
		{"totals differ, rise exactly on the limit", calls(1, 1000), calls(5, 2000), "0.15%", compare.Pass, ""},
		{"totals differ, rise past the limit by a thousandth", calls(1, 1000), calls(5, 2000), "0.149%", compare.Fail, ""},
		{"totals differ, a smaller rise", calls(1, 1000), calls(4, 2000), "0.15%", compare.Pass, ""},
		{"totals differ, the same share", calls(1, 1000), calls(2, 2000), "0%", compare.Pass, ""},
		{"totals differ, thirds", calls(1, 3), calls(2, 6), "0%", compare.Pass, ""},
		{"a few calls against many", calls(0, 3), calls(3000, 1000000), "0.1%", compare.Fail, ""},

		// Calls that did not send: no share to take.
		{"base sent nothing", calls(0, 0), calls(1, 1000), "1%", compare.Unknown, nothing},
		{"new sent nothing", calls(1, 1000), calls(0, 0), "1%", compare.Unknown, nothing},
		{"neither sent anything", calls(0, 0), calls(0, 0), "1%", compare.Unknown, nothing},

		// The new side's client-side errors: the target's own failures are
		// the lower end of the rise, all of them the upper end.
		{"all new failures are the client's", compare.Share{Total: 1000}, compare.Share{Bad: 30, BadClient: 30, Total: 1000}, "1%", compare.Unknown, client},
		{"some new failures are the client's", compare.Share{Total: 1000}, compare.Share{Bad: 20, BadClient: 15, Total: 1000}, "1%", compare.Unknown, client},
		{"the upper end on the limit", compare.Share{Total: 1000}, compare.Share{Bad: 20, BadClient: 15, Total: 1000}, "2%", compare.Pass, ""},
		{"the lower end past the limit", compare.Share{Total: 1000}, compare.Share{Bad: 20, BadClient: 15, Total: 1000}, "0.4%", compare.Fail, ""},
		{"the lower end on the limit", compare.Share{Total: 1000}, compare.Share{Bad: 20, BadClient: 15, Total: 1000}, "0.5%", compare.Unknown, client},
		{"the upper end within the limit", compare.Share{Total: 1000}, compare.Share{Bad: 20, BadClient: 20, Total: 1000}, "3%", compare.Pass, ""},

		// The base side's client-side errors work the other way: for the
		// fail the base's whole Bad is subtracted, for the pass only its
		// own failures.
		{"base client errors, rise within", compare.Share{Bad: 20, BadClient: 20, Total: 1000}, calls(20, 1000), "2%", compare.Pass, ""},
		{"base client errors, rise past by the upper end", compare.Share{Bad: 20, BadClient: 20, Total: 1000}, calls(20, 1000), "1%", compare.Unknown, client},
		{"base client errors do not lower the base of the fail", compare.Share{Bad: 20, BadClient: 20, Total: 1000}, calls(35, 1000), "2%", compare.Unknown, client},
		{"base client errors, the lower end past the limit", compare.Share{Bad: 20, BadClient: 20, Total: 1000}, calls(50, 1000), "1%", compare.Fail, ""},

		// Counts near 1e9 a side: the products pass int64.
		{"a billion exactly on the limit", compare.Share{Total: 2000000000}, calls(1000000000, 2000000000), "50%", compare.Pass, ""},
		{"a billion and one past the limit", compare.Share{Total: 2000000000}, calls(1000000001, 2000000000), "50%", compare.Fail, ""},
		{"billions, different totals, exactly on the limit", calls(1000000000, 2000000000), calls(1999999999, 1999999999), "50%", compare.Pass, ""},
		{"billions, different totals, a hair past the limit", calls(999999999, 2000000000), calls(1999999999, 1999999999), "50%", compare.Fail, ""},
		{"billions with client errors, upper end on the limit", compare.Share{Total: 2000000000}, compare.Share{Bad: 1000000000, BadClient: 1, Total: 2000000000}, "50%", compare.Pass, ""},
		{"billions with client errors, lower end on the limit", compare.Share{Total: 2000000000}, compare.Share{Bad: 1000000001, BadClient: 1, Total: 2000000000}, "50%", compare.Unknown, client},
		{"billions with client errors, lower end past the limit", compare.Share{Total: 2000000000}, compare.Share{Bad: 1000000002, BadClient: 1, Total: 2000000000}, "50%", compare.Fail, ""},
	} {
		got, reason := compare.ErrorsRise(tc.base, tc.nw, limit(tc.limit))
		if got != tc.want || reason != tc.reason {
			t.Errorf("%s: %s %q, want %s %q", tc.name, verdictNames[got], reason, verdictNames[tc.want], tc.reason)
		}
	}
}

// Ground: contract — the same inputs through a simple reference, exact
// rationals: the verdict of every combination of small shares, client errors
// and limits is the reference's, so no rounding sits between a count and a
// verdict.
func TestErrorsRise_AgreesWithExactRationals(t *testing.T) {
	var shares []compare.Share

	for _, total := range []int{1, 3, 7, 1000} {
		for _, bad := range []int{0, 1, total / 2, total - 1, total} {
			for _, client := range []int{0, 1, bad} {
				if client > bad {
					continue
				}

				shares = append(shares, compare.Share{Bad: bad, BadClient: client, Total: total})
			}
		}
	}

	frac := func(n, d int) *big.Rat { return big.NewRat(int64(n), int64(d)) }

	for _, text := range []string{"0%", "0.1%", "1%", "33.333%", "50%", "100%"} {
		lim := limit(text)
		l := big.NewRat(lim.Thousandths, 100000)

		for _, b := range shares {
			for _, n := range shares {
				lowest := new(big.Rat).Sub(frac(n.Bad-n.BadClient, n.Total), frac(b.Bad, b.Total))
				highest := new(big.Rat).Sub(frac(n.Bad, n.Total), frac(b.Bad-b.BadClient, b.Total))

				want, why := compare.Unknown, compare.ReasonClientErrors

				switch {
				case lowest.Cmp(l) > 0:
					want, why = compare.Fail, ""
				case highest.Cmp(l) <= 0:
					want, why = compare.Pass, ""
				}

				got, reason := compare.ErrorsRise(b, n, lim)
				if got != want || reason != why {
					t.Fatalf("base %+v new %+v limit %s: %s %q, want %s %q", b, n, text, verdictNames[got], reason, verdictNames[want], why)
				}
			}
		}
	}
}
