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

// Ground: reference — Ticks is the count of ticks the schedule places, told by
// arithmetic: it equals the walk of offset on stages small enough to walk, ramps
// and fractions of a second included.
// Mutation: ceil becomes floor.
func TestStage_TicksEqualsTheWalkOfOffset(t *testing.T) {
	durations := []time.Duration{time.Nanosecond, 7 * time.Millisecond, 333 * time.Millisecond, time.Second, 1500 * time.Millisecond, 2*time.Second + 1}
	rates := []int{0, 1, 2, 3, 7, 100, 1000}

	for _, d := range durations {
		for _, a := range rates {
			for _, b := range rates {
				stage := Stage{StartRPS: a, TargetRPS: b, Duration: d}

				var walked uint64
				for i := 0; ; i++ {
					if _, ok := stage.offset(i); !ok {
						break
					}
					walked++
				}

				if got := stage.Ticks(); got != walked {
					t.Fatalf("%d to %d rps over %s: Ticks = %d, the schedule places %d", a, b, d, got, walked)
				}
			}
		}
	}
}

// Ground: boundary — a stage of real size does not walk and does not overflow:
// 1e6 rps for 3 hours is 1.08e10, and a count past uint64 saturates.
func TestStage_TicksOfAHugeStageIsArithmetic(t *testing.T) {
	long := Stage{StartRPS: 1_000_000, TargetRPS: 1_000_000, Duration: 3 * time.Hour}
	if got, want := long.Ticks(), uint64(10_800_000_000); got != want {
		t.Errorf("Ticks = %d, want %d", got, want)
	}

	ramp := Stage{StartRPS: 0, TargetRPS: 1_000_000, Duration: 3 * time.Hour}
	if got, want := ramp.Ticks(), uint64(5_400_000_000); got != want {
		t.Errorf("ramp Ticks = %d, want %d", got, want)
	}

	// 2^63-1 ns x 2^62 rps / 1e9 does not fit uint64.
	if got := (Stage{StartRPS: math.MaxInt, TargetRPS: math.MaxInt, Duration: math.MaxInt64}).Ticks(); got != math.MaxUint64 {
		t.Errorf("Ticks = %d, want a saturated %d", got, uint64(math.MaxUint64))
	}
}
