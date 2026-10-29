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
	"reflect"
	"testing"
	"time"
)

// Ground: contract — a plan that was walked to its end covers the whole
// measured part, though the drain ended a tick before the plan did; a plan
// that was not walked keeps the moment sending ended at. The totals are the
// same either way: the signal belongs to the windows alone.
func TestWindows_AWalkedPlanCoversAllTheMeasuredPartAndTouchesNoTotal(t *testing.T) {
	start := time.Now()
	const measured = 3 * time.Second

	fill := func(walked bool) *Stats {
		stats := windowed(start, 0, measured)
		// The drain ended 50 ms before the plan did.
		stats.EndSending(start.Add(measured - 50*time.Millisecond))
		for i := range 59 {
			at := time.Duration(i) * 50 * time.Millisecond
			stats.Record(inMeasured(start, 0, at, time.Millisecond, CategorySuccess))
		}
		if walked {
			stats.planWalked()
		}
		stats.Finish(start.Add(measured))

		return stats
	}

	walked, cut := fill(true), fill(false)

	got := windowsOf(t, walked)
	if len(got) != 3 || got[2].Width != time.Second {
		t.Errorf("a walked plan has %d windows, the last %v wide; want 3 and 1 s", len(got), got[len(got)-1].Width)
	}
	got = windowsOf(t, cut)
	if len(got) != 3 || got[2].Width != time.Second-50*time.Millisecond {
		t.Errorf("a plan not walked has %d windows, the last %v wide; want 3 and 950 ms", len(got), got[len(got)-1].Width)
	}

	a, b := walked.Report(), cut.Report()
	for i := range a.Methods {
		a.Methods[i].Windows, b.Methods[i].Windows = nil, nil
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("the plan being walked changed a number outside the windows:\nwalked: %+v\ncut:    %+v", a, b)
	}
}
