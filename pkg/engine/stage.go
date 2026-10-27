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
	"math/bits"
	"time"
)

// Stage is one stretch of a call's plan. A stage with StartRPS == TargetRPS
// holds that rate; otherwise the rate moves linearly from StartRPS to
// TargetRPS over Duration.
type Stage struct {
	StartRPS  int
	TargetRPS int
	Duration  time.Duration
}

// offset is the moment of tick i from the stage start; false once it is not
// before Duration. It is the only place the pace is computed: the dispatcher,
// the scheduler and the schedule's count all ask it.
//
// A constant stage is the integer formula i·1s/rps. A ramp a to b over D puts
// tick i at the least t with a·t + (b-a)·t²/(2D) >= i (the calls sent by t
// are the area under the rate), written as t = 2i / (a + sqrt(a² +
// 2i(b-a)/D)) so that a small a does not cancel; the count of ticks is
// ceil(D·(a+b)/2), decided in whole numbers (rampHasTick), not by where the
// float lands.
func (s Stage) offset(i int) (time.Duration, bool) {
	if s.StartRPS == s.TargetRPS {
		if s.TargetRPS < 1 {
			return 0, false
		}
		at := time.Duration(i) * time.Second / time.Duration(s.TargetRPS)

		return at, at < s.Duration
	}

	return s.rampOffset(i)
}

func (s Stage) rampOffset(i int) (time.Duration, bool) {
	if !s.rampHasTick(i) {
		return 0, false
	}
	if i == 0 {
		return 0, true
	}

	a, b := float64(s.StartRPS), float64(s.TargetRPS)
	n := float64(i)
	disc := a*a + 2*n*(b-a)/s.Duration.Seconds()
	if disc < 0 {
		return 0, false
	}
	at := time.Duration(math.Round(2 * n / (a + math.Sqrt(disc)) * float64(time.Second)))

	// The count above says this tick is before the end; a float that lands on
	// the end does not move it out of the stage.
	return min(at, s.Duration-1), true
}

// rampHasTick says whether tick i of a ramp exists: i < D·(a+b)/2, that is
// i·2·10⁹ < D(ns)·(a+b), in 128 bits (D·(a+b) on an hour passes int64).
func (s Stage) rampHasTick(i int) bool {
	if i < 0 || s.Duration <= 0 || s.StartRPS < 0 || s.TargetRPS < 0 {
		return false
	}
	leftHi, leftLo := bits.Mul64(uint64(i), 2*uint64(time.Second))
	rightHi, rightLo := bits.Mul64(uint64(s.Duration), uint64(s.StartRPS+s.TargetRPS))

	return leftHi < rightHi || (leftHi == rightHi && leftLo < rightLo)
}

// rateBounds is the planned rate x into the stage, as the whole numbers just
// under and just over it. Whole-number arithmetic: a float gives 28.000000000000004
// for an exact 28 and the report would say 29.
func (s Stage) rateBounds(x time.Duration) (floor, ceil int) {
	a, b := s.StartRPS, s.TargetRPS
	if a == b || s.Duration <= 0 {
		return a, a
	}
	gain := uint64(max(a, b) - min(a, b))
	hi, lo := bits.Mul64(gain, uint64(x))
	q, r := bits.Div64(hi, lo, uint64(s.Duration))
	moved := int(q)
	if b > a {
		return a + moved, a + moved + min(int(r), 1)
	}

	return a - moved - min(int(r), 1), a - moved
}
