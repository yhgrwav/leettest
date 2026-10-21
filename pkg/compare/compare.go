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

package compare

import (
	"math/big"

	"github.com/yhgrwav/leettest/pkg/share"
)

// Bound says what a capacity's RPS is.
type Bound int

const (
	// Exact: the search found the step the service broke above; RPS is the
	// highest step that held.
	Exact Bound = iota
	// Lower: the search ended before it broke; the capacity is at least RPS.
	Lower
	// Upper: the search broke at its first step; the capacity is below RPS,
	// the rate of that step.
	Upper
	// None: the search measured no capacity.
	None
)

// Capacity is the capacity one breakpoint search found.
type Capacity struct {
	RPS   int
	Bound Bound
	// Outcome is the search's outcome as the report names it; it only words a
	// reason.
	Outcome string
}

// Verdict is the answer of a capacity check.
type Verdict int

const (
	// Unknown: the two capacities cannot tell whether the limit was met.
	Unknown Verdict = iota
	Pass
	Fail
)

// CapacityDrop judges the fall from base to new against limit. A reason (one
// of the Reason constants) and its words are given only for Unknown.
//
// The fall is (base − new) / base. A bound narrows it to a range, and the
// verdict follows the range: Fail only when the smallest possible fall is
// over the limit, Pass only when the largest possible fall is within it,
// otherwise Unknown.
func CapacityDrop(base, nw Capacity, limit share.Percent) (verdict Verdict, reason, words string) {
	switch {
	case base.Bound == None:
		return Unknown, ReasonNotMeasured, "base not measured (" + base.Outcome + ")"
	case nw.Bound == None:
		return Unknown, ReasonNotMeasured, "new not measured (" + nw.Outcome + ")"
	case base.Bound == Upper:
		return Unknown, ReasonBrokeAtFirst, "base broke at the first step: lower the plan to see a drop"
	case base.RPS <= 0:
		return Unknown, ReasonBaseZero, "base capacity is 0 rps"
	}

	over := share.Decide(max(base.RPS-nw.RPS, 0), 0, base.RPS, limit) == share.Breaks

	switch {
	case nw.Bound == Upper:
		if over {
			return Fail, "", ""
		}

		return Unknown, ReasonBrokeAtFirst, "new broke at the first step: lower the plan to see the drop"
	case base.Bound == Lower && nw.Bound == Lower:
		return Unknown, ReasonLowerBound, "both capacities are lower bounds"
	case base.Bound == Lower:
		if over {
			return Fail, "", ""
		}

		return Unknown, ReasonLowerBound, "base capacity is only a lower bound (" + base.Outcome + ")"
	case nw.Bound == Lower:
		if !over {
			return Pass, "", ""
		}

		return Unknown, ReasonLowerBound, "new run ended before its capacity (" + nw.Outcome + ")"
	case over:
		return Fail, "", ""
	}

	return Pass, "", ""
}

// The reasons a check can be unknown, a closed list. The first of
// NotComparable, MethodInvalid, Invalid, Incomplete, Generator and Stream that
// applies wins over the check's own.
const (
	ReasonNotComparable   = "not_comparable"
	ReasonMethodInvalid   = "method_invalid"
	ReasonInvalid         = "invalid"
	ReasonIncomplete      = "incomplete"
	ReasonGenerator       = "generator"
	ReasonStream          = "stream"
	ReasonFewObservations = "few_observations"
	ReasonBaseZero        = "base_zero"
	ReasonCensored        = "censored"
	ReasonNothingSent     = "nothing_sent"
	ReasonClientErrors    = "client_errors"
	// The reasons of a capacity check: a search that measured nothing, one
	// that broke at its first step, one that ended before it broke.
	ReasonNotMeasured  = "not_measured"
	ReasonBrokeAtFirst = "broke_at_first"
	ReasonLowerBound   = "lower_bound"
)

// Latency is the p99 of one method of a run report.
type Latency struct {
	// NS is whole nanoseconds: the quantile's `ns` in the report, or its
	// `us` × 1000 when the report was written before `ns` existed.
	NS int64
	// LowerBound: the tail ran past the timeout, the true value is larger.
	LowerBound bool
	// Defined is false when the method has too few observations.
	Defined bool
}

// Share is the failed calls of one method of a run report, the parts of
// `pass: errors`.
type Share struct {
	// Bad is every failed call and every call lost to the connection.
	Bad int
	// BadClient is the part of Bad that says nothing of the target: rejected
	// requests, client errors, our bad responses.
	BadClient int
	// Total is the calls sent and those lost to the connection.
	Total int
}

// P99Rise judges the rise of the p99 from base to new, in percent of base,
// against limit. A reason is given only for Unknown.
//
// The rise is (new − base) / base in whole nanoseconds. A lower bound says
// the true p99 is at least that, so a rise past the limit is certain when the
// base is exact (the new can only be larger) and a rise within it when the
// new is exact (the base can only be larger).
func P99Rise(base, nw Latency, limit share.Percent) (verdict Verdict, reason string) {
	if !base.Defined || !nw.Defined {
		return Unknown, ReasonFewObservations
	}

	if base.NS <= 0 {
		return Unknown, ReasonBaseZero
	}

	// big: (new − base) × 100000 does not fit int64 for an hour-long p99.
	rise := new(big.Int).Sub(big.NewInt(nw.NS), big.NewInt(base.NS))
	rise.Mul(rise, big.NewInt(100000))
	allowed := new(big.Int).Mul(big.NewInt(limit.Thousandths), big.NewInt(base.NS))
	over := rise.Cmp(allowed) > 0

	switch {
	case !base.LowerBound && !nw.LowerBound:
		if over {
			return Fail, ""
		}

		return Pass, ""
	case !base.LowerBound:
		if over {
			return Fail, ""
		}

		return Unknown, ReasonCensored
	case !nw.LowerBound:
		if over {
			return Unknown, ReasonCensored
		}

		return Pass, ""
	}

	return Unknown, ReasonCensored
}

// ErrorsRise judges the rise of the failed share from base to new, in points
// of share, against limit. A reason is given only for Unknown.
//
// The rise is new.Bad/new.Total − base.Bad/base.Total, as `pass: errors`
// reads one share: Fail when the target's own failures alone (without
// BadClient) on the new side exceed the limit over all the base side's
// failures, Pass when all failures on the new side stay within it over the
// base side's own, otherwise the client-side errors decide and the verdict is
// Unknown. Exact fractions in math/big: counts near 10^9 overflow int64.
func ErrorsRise(base, nw Share, limit share.Percent) (verdict Verdict, reason string) {
	if base.Total == 0 || nw.Total == 0 {
		return Unknown, ReasonNothingSent
	}

	b, d := int64(nw.Total), int64(base.Total)
	rhs := new(big.Int).Mul(big.NewInt(limit.Thousandths*b), big.NewInt(d))

	if risePast(int64(nw.Bad-nw.BadClient), d, int64(base.Bad), b).Cmp(rhs) > 0 {
		return Fail, ""
	}

	if risePast(int64(nw.Bad), d, int64(base.Bad-base.BadClient), b).Cmp(rhs) <= 0 {
		return Pass, ""
	}

	return Unknown, ReasonClientErrors
}

// risePast is (a·d − c·b) · 100000, a/b − c/d scaled by b·d and by a percent
// in thousandths.
func risePast(a, d, c, b int64) *big.Int {
	x := new(big.Int).Mul(big.NewInt(a), big.NewInt(d))
	x.Sub(x, new(big.Int).Mul(big.NewInt(c), big.NewInt(b)))

	return x.Mul(x, big.NewInt(100000))
}
