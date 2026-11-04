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

package engine

import "time"

// meanRate is the mean of PlannedRate over [from, to): the integral divided by the
// width. The part of the interval past the plan counts as 0 rate, so the mean
// stays over the whole width; 0 for to <= from. Each stage is linear, so the
// integral over its part of the interval is the width times the rate at the
// part's midpoint. Checked against a numeric integral in the tests.
func meanRate(stages []Stage, from, to time.Duration) float64 {
	if to <= from {
		return 0
	}

	var calls float64

	var base time.Duration

	for _, stage := range stages {
		if stage.Duration <= 0 {
			continue
		}

		end := base + stage.Duration
		lo, hi := max(from, base), min(to, end)

		if lo < hi {
			rate := float64(stage.StartRPS)
			if stage.StartRPS != stage.TargetRPS {
				slope := float64(stage.TargetRPS-stage.StartRPS) / float64(stage.Duration)
				rate += slope * float64(lo+hi-2*base) / 2
			}

			calls += rate * float64(hi-lo)
		}

		base = end
		if base >= to {
			break
		}
	}

	return calls / float64(to-from)
}

// planLength is how long the plan runs: the sum of its stages.
func planLength(stages []Stage) time.Duration {
	var total time.Duration
	for _, stage := range stages {
		total += max(stage.Duration, 0)
	}

	return total
}

// PlannedRate is the rate the plan offers elapsed after its start, in calls per
// second: a constant stage holds its rate, a ramp moves linearly from its
// StartRPS to its TargetRPS over its Duration, and a stage that has ended
// hands over to the next one at exactly its end (the next stage's rate at the
// boundary). A negative elapsed is the plan's first rate, a stage of zero
// Duration is skipped, and 0 comes after the last stage and for a plan with
// none. Pure: it reads nothing but its arguments.
func PlannedRate(stages []Stage, elapsed time.Duration) float64 {
	var base time.Duration
	for _, stage := range stages {
		if stage.Duration <= 0 {
			continue
		}
		if elapsed < 0 {
			return float64(stage.StartRPS)
		}
		if elapsed < base+stage.Duration {
			if stage.StartRPS == stage.TargetRPS {
				return float64(stage.StartRPS)
			}
			within := elapsed - base
			slope := float64(stage.TargetRPS-stage.StartRPS) / float64(stage.Duration)
			return max(float64(stage.StartRPS)+slope*float64(within), 0)
		}
		base += stage.Duration
	}

	return 0
}
