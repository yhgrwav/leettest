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

import "github.com/yhgrwav/leettest/pkg/share"

// Bound says what a capacity's RPS is.
type Bound int

const (
	// Exact: the search found the step the service broke above; RPS is the
	// highest step that held. Zero RPS: it broke at the first step.
	Exact Bound = iota
	// Lower: the search ended before it broke; the capacity is at least RPS.
	Lower
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

// CapacityDrop judges the fall from base to new against limit. A reason is
// given only for Unknown.
func CapacityDrop(_, _ Capacity, _ share.Percent) (verdict Verdict, reason string) {
	return Unknown, ""
}
