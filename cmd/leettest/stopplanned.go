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

package main

import (
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/errstop"
)

// stopPlannedRate is the rate the plan of the tripped method offered at the
// moment of the trip; nil when there is no trip, the method is not among
// calls, or the plan had ended by then (At is at or past the sum of its
// stages: the timeouts of the tail come exactly there). A method that is not
// among calls or has no stages is the same nil, "the plan had ended": a stop
// with a breakpoint search is forbidden by the config, so the tripped method
// is always one of calls and has a plan.
func stopPlannedRate(calls []engine.Call, t *errstop.Trip) *float64 {
	if t == nil {
		return nil
	}

	for _, call := range calls {
		if call.Method != t.Method {
			continue
		}

		var length time.Duration
		for _, stage := range call.Stages {
			length += max(stage.Duration, 0)
		}
		if t.At >= length {
			return nil
		}

		rate := engine.PlannedRate(call.Stages, t.At)

		return &rate
	}

	return nil
}
