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

import (
	"math"
	"time"
)

// rateAt is the rate the plan offers elapsed after its start, in calls per
// second: a constant stage holds its rate, a ramp moves linearly from its
// StartRPS to its TargetRPS over its Duration, and a stage that has ended
// hands over to the next one at exactly its end (the next stage's rate at the
// boundary). A negative elapsed is the plan's first rate, a stage of zero
// Duration is skipped, and 0 comes after the last stage and for a plan with
// none. Pure: it reads nothing but its arguments.
//
// STUB (prometheus-export, layer 2): returns NaN, so that no test passes by
// the answer 0 alone.
func rateAt(_ []Stage, _ time.Duration) float64 {
	return math.NaN()
}
