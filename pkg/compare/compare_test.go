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
	"testing"

	"github.com/yhgrwav/leettest/pkg/compare"
	"github.com/yhgrwav/leettest/pkg/share"
)

var verdictNames = map[compare.Verdict]string{compare.Unknown: "unknown", compare.Pass: "pass", compare.Fail: "fail"}

func exact(rps int) compare.Capacity {
	return compare.Capacity{RPS: rps, Bound: compare.Exact, Outcome: "broke"}
}

func lower(rps int, outcome string) compare.Capacity {
	return compare.Capacity{RPS: rps, Bound: compare.Lower, Outcome: outcome}
}

func notMeasured(outcome string) compare.Capacity {
	return compare.Capacity{Bound: compare.None, Outcome: outcome}
}

func limit(text string) share.Percent {
	p, err := share.Parse(text, "limit")
	if err != nil {
		panic(err)
	}

	return p
}

// Ground: boundary — a fall exactly on the limit passes (≤, as pass: and
// errors_limit), one rps past it fails; arithmetic is share.Decide's, with
// the fall as the share of the base, so no float decides a boundary. A rise
// and no change pass.
func TestCapacity_ExactValuesAreJudgedByTheLimit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		base, nw int
		limit    string
		want     compare.Verdict
	}{
		{"exactly on the limit", 1000, 900, "10%", compare.Pass},
		{"one rps past the limit", 1000, 899, "10%", compare.Fail},
		{"no change", 1000, 1000, "10%", compare.Pass},
		{"a rise", 1000, 1200, "10%", compare.Pass},
		{"fell to the first step's floor", 1000, 0, "10%", compare.Fail},
		{"large numbers exactly on the limit", 100000, 90000, "10%", compare.Pass},
		{"large numbers one rps past", 100000, 89999, "10%", compare.Fail},
		{"a zero limit holds no change", 1000, 1000, "0%", compare.Pass},
		{"a zero limit fails one rps", 1000, 999, "0%", compare.Fail},
		{"a fraction of a percent", 1000, 999, "0.1%", compare.Pass},
		{"a fraction of a percent, one more", 1000, 998, "0.1%", compare.Fail},
	} {
		got, reason := compare.CapacityDrop(exact(tc.base), exact(tc.nw), limit(tc.limit))
		if got != tc.want || reason != "" {
			t.Errorf("%s: %s %q, want %s with no reason", tc.name, verdictNames[got], reason, verdictNames[tc.want])
		}
	}
}

// Ground: contract — a base that broke at the first step (exact 0) has no
// drop to measure: unknown with the reason that says what to do, whatever the
// new value is.
func TestCapacity_BaseThatBrokeAtTheFirstStepIsUnknown(t *testing.T) {
	const reason = "base broke at the first step: lower the plan to see a drop"

	for name, nw := range map[string]compare.Capacity{
		"new also zero": exact(0),
		"new above":     exact(500),
		"new a floor":   lower(500, "held_all"),
	} {
		got, why := compare.CapacityDrop(exact(0), nw, limit("10%"))
		if got != compare.Unknown || why != reason {
			t.Errorf("%s: %s %q, want unknown %q", name, verdictNames[got], why, reason)
		}
	}
}

// Ground: contract — a capacity that was not measured makes the check
// unknown, naming which side and the outcome; this comes before the base-zero
// rule.
func TestCapacity_NotMeasuredIsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name     string
		base, nw compare.Capacity
		reason   string
	}{
		{"base invalid", notMeasured("invalid"), exact(800), "base not measured (invalid)"},
		{"new invalid", exact(1000), notMeasured("invalid"), "new not measured (invalid)"},
		{"new has no held rate", exact(1000), notMeasured("run_limit"), "new not measured (run_limit)"},
		{"new not measured beside a zero base", exact(0), notMeasured("invalid"), "new not measured (invalid)"},
	} {
		got, why := compare.CapacityDrop(tc.base, tc.nw, limit("10%"))
		if got != compare.Unknown || why != tc.reason {
			t.Errorf("%s: %s %q, want unknown %q", tc.name, verdictNames[got], why, tc.reason)
		}
	}
}

// Ground: boundary — a new search that ended before it broke gives only a
// lower bound of its capacity: the fall can be at most base − bound. Within
// the limit by that — pass; otherwise nothing is known: never a fail (the
// search might have held far higher). The inverse of the base-lower rows.
func TestCapacity_NewLowerBoundNeverFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		nw     compare.Capacity
		want   compare.Verdict
		reason string
	}{
		{"bound within the limit", lower(950, "held_all"), compare.Pass, ""},
		{"bound exactly on the limit", lower(900, "held_all"), compare.Pass, ""},
		{"bound above the base", lower(1100, "held_all"), compare.Pass, ""},
		{"bound one rps past the limit", lower(899, "held_all"), compare.Unknown, "new run ended before its capacity (held_all)"},
		{"bound far past the limit", lower(500, "held_all"), compare.Unknown, "new run ended before its capacity (held_all)"},
		{"stopped far past the limit", lower(500, "stopped"), compare.Unknown, "new run ended before its capacity (stopped)"},
		{"run limit far past the limit", lower(500, "run_limit"), compare.Unknown, "new run ended before its capacity (run_limit)"},
	} {
		got, why := compare.CapacityDrop(exact(1000), tc.nw, limit("10%"))
		if got != tc.want || why != tc.reason {
			t.Errorf("%s: %s %q, want %s %q", tc.name, verdictNames[got], why, verdictNames[tc.want], tc.reason)
		}
	}
}

// Ground: boundary — a base that ended before it broke is only a lower bound:
// the fall is at least bound − new. Past the limit by that — fail; within it
// nothing is known: never a pass (the true base might be far higher). The
// inverse of the new-lower rows.
func TestCapacity_BaseLowerBoundNeverPasses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		nw     compare.Capacity
		want   compare.Verdict
		reason string
	}{
		{"new far below", exact(800), compare.Fail, ""},
		{"new one rps past the limit", exact(899), compare.Fail, ""},
		{"new fell to the floor", exact(0), compare.Fail, ""},
		{"new exactly on the limit", exact(900), compare.Unknown, "base capacity is only a lower bound (held_all)"},
		{"new within the limit", exact(950), compare.Unknown, "base capacity is only a lower bound (held_all)"},
		{"new above the base", exact(1200), compare.Unknown, "base capacity is only a lower bound (held_all)"},
	} {
		got, why := compare.CapacityDrop(lower(1000, "held_all"), tc.nw, limit("10%"))
		if got != tc.want || why != tc.reason {
			t.Errorf("%s: %s %q, want %s %q", tc.name, verdictNames[got], why, verdictNames[tc.want], tc.reason)
		}
	}
}

// Ground: contract — two lower bounds say nothing of a fall, whatever they
// are.
func TestCapacity_TwoLowerBoundsAreUnknown(t *testing.T) {
	const reason = "both capacities are lower bounds"

	for name, nw := range map[string]compare.Capacity{
		"new far below": lower(100, "held_all"),
		"same":          lower(1000, "held_all"),
		"new above":     lower(2000, "stopped"),
	} {
		got, why := compare.CapacityDrop(lower(1000, "held_all"), nw, limit("10%"))
		if got != compare.Unknown || why != reason {
			t.Errorf("%s: %s %q, want unknown %q", name, verdictNames[got], why, reason)
		}
	}
}
