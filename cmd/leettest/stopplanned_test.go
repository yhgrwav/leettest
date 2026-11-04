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
	"math"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/errstop"
)

// twoPlans are two methods with plans of their own: "/svc/Flat" holds 50 rps
// for 10 s, "/svc/Ramp" climbs from 0 to 200 over 10 s and holds 200 for 10 s
// more.
func twoPlans() []engine.Call {
	return []engine.Call{
		{Method: "/svc/Flat", Stages: []engine.Stage{{StartRPS: 50, TargetRPS: 50, Duration: 10 * time.Second}}},
		{Method: "/svc/Ramp", Stages: []engine.Stage{
			{StartRPS: 0, TargetRPS: 200, Duration: 10 * time.Second},
			{StartRPS: 200, TargetRPS: 200, Duration: 10 * time.Second},
		}},
	}
}

// Ground: contract — the stop names the rate of the method that tripped, at the
// moment the watch judged, not the rate of another method or of the end of the
// plan. With two methods whose plans differ, the same moment gives each its
// own number: 4 s into the ramp of one is 80 rps, into the hold of the other 50.
// Mutations "the first method's stages" and "the rate at the plan's end" turn
// it red.
func TestStopPlannedRate_IsTheRateOfTheTrippedMethodAtTheMoment(t *testing.T) {
	calls := twoPlans()

	for _, c := range []struct {
		method string
		at     time.Duration
		want   float64
	}{
		{"/svc/Ramp", 4 * time.Second, 80},
		{"/svc/Flat", 4 * time.Second, 50},
		{"/svc/Ramp", 12 * time.Second, 200},
		{"/svc/Flat", 9999 * time.Millisecond, 50},
	} {
		trip := errstop.Trip{Method: c.method, At: c.at}

		got := stopPlannedRate(calls, &trip)
		if got == nil {
			t.Errorf("%s at %v: nil, want %v", c.method, c.at, c.want)

			continue
		}
		if math.Abs(*got-c.want) > 1e-9 {
			t.Errorf("%s at %v: %v, want %v", c.method, c.at, *got, c.want)
		}
	}
}

// Ground: boundary — a stop at or after the end of the method's plan has no
// rate: the engine's rate past the last stage is 0, and "the plan offered 0
// rps" would say the opposite of what happened (the timeouts of the tail come
// then). The moment the last stage ends already belongs to "ended".
func TestStopPlannedRate_IsNilOnceThePlanHasEnded(t *testing.T) {
	calls := twoPlans()

	for _, c := range []struct {
		method string
		at     time.Duration
	}{
		{"/svc/Flat", 10 * time.Second},
		{"/svc/Flat", 12 * time.Second},
		{"/svc/Ramp", 20 * time.Second},
		{"/svc/Ramp", 25 * time.Second},
	} {
		trip := errstop.Trip{Method: c.method, At: c.at}

		if got := stopPlannedRate(calls, &trip); got != nil {
			t.Errorf("%s at %v: %v, want nil, the plan had ended", c.method, c.at, *got)
		}
	}
}

// Ground: edge — nothing to name: no trip (the run was not stopped by the
// watch), a method that is not among the calls, a method with no stages.
func TestStopPlannedRate_IsNilWithoutATripOrAPlan(t *testing.T) {
	calls := append(twoPlans(), engine.Call{Method: "/svc/Empty"})

	if got := stopPlannedRate(calls, nil); got != nil {
		t.Errorf("no trip: %v, want nil", *got)
	}
	for _, method := range []string{"/svc/Other", "/svc/Empty"} {
		trip := errstop.Trip{Method: method, At: time.Second}

		if got := stopPlannedRate(calls, &trip); got != nil {
			t.Errorf("%s: %v, want nil", method, *got)
		}
	}
}
