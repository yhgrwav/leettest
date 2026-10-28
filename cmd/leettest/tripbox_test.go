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
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/errstop"
)

var aTrip = errstop.Trip{Method: "/svc/Method", Failed: 5, Total: 5, At: 3 * time.Second}

// Ground: contract — a trip is a stop when it cut the plan short, and only
// then: the engine marks the report incomplete when its dispatcher was
// stopped, and a trip that came while the last calls were already out, or after
// Run returned, changes nothing, so the report that is complete says nothing of
// one. The check is the box's alone: the stop line is told from what it gives.
func TestTripBox_ATripIsAStopOnlyWhenThePlanWasCut(t *testing.T) {
	var box tripBox

	box.Trip(aTrip)

	got := box.Of(engine.Report{Incomplete: true})
	if got == nil || got.Method != aTrip.Method || got.Failed != aTrip.Failed || got.At != aTrip.At {
		t.Errorf("a trip that cut the run short is not in the report: %+v", got)
	}

	if got := box.Of(engine.Report{Incomplete: false}); got != nil {
		t.Errorf("a trip in a run that completed is in the report: %+v", got)
	}
}

// Ground: contract — nothing tripped, nothing to tell.
func TestTripBox_NoTripNoStop(t *testing.T) {
	var box tripBox

	if got := box.Of(engine.Report{Incomplete: true}); got != nil {
		t.Errorf("a run nobody stopped has a stop: %+v", got)
	}
}
