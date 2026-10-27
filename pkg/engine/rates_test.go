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
	up := []Stage{{StartRPS: 0, TargetRPS: 800, Duration: 30 * time.Second}}
	down := []Stage{{StartRPS: 900, TargetRPS: 100, Duration: 10 * time.Second}}
	downToZero := []Stage{{StartRPS: 100, TargetRPS: 0, Duration: 10 * time.Second}}
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
		// 100 to 300 over 20s is 10 rps per second: the second [20s, 21s) is
		// 100 to 110, not the whole stage's 100 to 300 (the number this row had).
		{"the ramp's first second", ramp, 21, 100, 110},
		{"a stage of rate 0", idle, 15, 0, 0},
		// The rate at the ends of the overlap, whole numbers: floor for low,
		// ceil for high. 800 over 30s: 26.67 rps per second.
		{"inside a ramp from zero", up, 16, 400, 427},
		{"earlier in the ramp", up, 11, 266, 294},
		{"a second ending at the middle: a whole 400, not rounded up", up, 15, 373, 400},
		{"the ramp's last second", up, 30, 773, 800},
		{"past the ramp from zero", up, 32, 0, 0},
		{"a ramp down, the first second", down, 1, 820, 900},
		{"a ramp down to zero, the last second", downToZero, 10, 0, 10},
	} {
		low, high := ratesInWindow(tc.stages, tc.from)
		if low != tc.low || high != tc.high {
			t.Errorf("%s: from %d gives %d-%d, want %d-%d", tc.name, tc.from, low, high, tc.low, tc.high)
		}
	}
}

// Ground: contract — the report's planned rate is of the stages from the end of
// the excluded window on, [warmup, end): a ramp that ends at the window's edge
// is not in it (Spec 6), a window cut through a ramp gives the rates at its
// ends, and a window past the plan gives 0-0 (a library caller; the config
// keeps warmup below duration).
func TestRatesOver_TheMeasuredPart(t *testing.T) {
	plan := []Stage{
		{StartRPS: 0, TargetRPS: 800, Duration: 30 * time.Second},
		{StartRPS: 800, TargetRPS: 800, Duration: 20 * time.Second},
	}
	for _, tc := range []struct {
		name      string
		from, to  time.Duration
		low, high int
	}{
		{"the whole plan", 0, math.MaxInt64, 0, 800},
		{"from the ramp's end", 30 * time.Second, math.MaxInt64, 800, 800},
		{"from the ramp's end and 2s of warm-up", 32 * time.Second, math.MaxInt64, 800, 800},
		{"from the middle of the ramp", 15 * time.Second, math.MaxInt64, 400, 800},
		{"from a tick inside a second", 15500 * time.Millisecond, math.MaxInt64, 413, 800},
		{"a window of half a second inside the ramp", 15 * time.Second, 15500 * time.Millisecond, 400, 414},
		// 800/30 s is 80/3 rps per second: 525 ms to 1.05 s are exactly 14 and
		// 28 rps. A float formula lands at 28.000000000000004 and 29.
		{"a window that ends on a whole rate", 525 * time.Millisecond, 1050 * time.Millisecond, 14, 28},
		// 1.275 s is exactly 34 rps; a float formula gives 33.99999999999999.
		{"a window that starts on a whole rate", 1275 * time.Millisecond, math.MaxInt64, 34, 800},
		{"from the plan's end", 50 * time.Second, math.MaxInt64, 0, 0},
		{"from past the plan", 60 * time.Second, math.MaxInt64, 0, 0},
	} {
		low, high := ratesOver(plan, tc.from, tc.to)
		if low != tc.low || high != tc.high {
			t.Errorf("%s: [%s, %s) gives %d-%d, want %d-%d", tc.name, tc.from, tc.to, low, high, tc.low, tc.high)
		}
	}
}
