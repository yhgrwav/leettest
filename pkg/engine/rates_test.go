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
	"testing"
	"time"
)

// Ground: boundary — stage edges and a silence past the plan's end, which
// calls that waited for a stream reach: there the rate is unknown, not 0.
func TestRatesInWindow(t *testing.T) {
	flat := []Stage{{StartRPS: 300, TargetRPS: 300, Duration: 40 * time.Second}}
	ramp := []Stage{
		{StartRPS: 100, TargetRPS: 100, Duration: 20 * time.Second},
		{StartRPS: 100, TargetRPS: 300, Duration: 20 * time.Second},
	}
	idle := []Stage{
		{StartRPS: 200, TargetRPS: 200, Duration: 10 * time.Second},
		{StartRPS: 0, TargetRPS: 0, Duration: 10 * time.Second},
	}
	for _, tc := range []struct {
		name      string
		stages    []Stage
		from      int
		low, high int
	}{
		{"inside the stage", flat, 25, 300, 300},
		{"silent from second 0", flat, 0, 300, 300},
		{"the plan's last second", flat, 40, 300, 300},
		{"past the plan", flat, 41, 0, 0},
		{"the second before the ramp", ramp, 20, 100, 100},
		{"the ramp's first second", ramp, 21, 100, 300},
		{"a stage of rate 0", idle, 15, 0, 0},
	} {
		low, high := ratesInWindow(tc.stages, tc.from)
		if low != tc.low || high != tc.high {
			t.Errorf("%s: from %d gives %d-%d, want %d-%d", tc.name, tc.from, low, high, tc.low, tc.high)
		}
	}
}
