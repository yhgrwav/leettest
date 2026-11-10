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
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strings"
	"time"
)

var ErrInFlightBudget = errors.New("calls can outgrow the in-flight cap before any request times out")

// InFlightBudgetError says how many requests a hung target would hold in
// flight. By Little's law that is rate × wait, and against a target that has
// stopped answering the wait is the timeout: when the sum exceeds the cap, the
// cap is hit before the first timeout, and the run ends with a cap error
// instead of a single censored observation.
//
// A call without a timeout waits forever, and forever times any rate exceeds
// any cap: such calls are named in Unbounded and Need is MaxInt.
type InFlightBudgetError struct {
	// Need bounds Σ in flight from above: each call counts at its busiest
	// stage, ⌈peak RPS × timeout⌉. Stages shorter than the timeout overlap in
	// flight, and the peak covers that; a spike shorter than the timeout is
	// overcounted.
	Need int
	Cap  int
	// PeakRPS is the summed peak rate, so the caller can say which timeout
	// would fit: Cap / PeakRPS.
	PeakRPS int
	// Reserved is the part of Need that is not rps × timeout: the edge slot
	// and the ReleaseMargin of every call. Each of the Calls rounds its own
	// rps × timeout up, so a timeout surely fits when
	// PeakRPS × timeout ≤ Cap − Reserved − Calls.
	Reserved int
	Calls    int
	// Scenarios is how many of the Calls are scenarios: their wait is the sum
	// of their steps' timeouts, and the margin is counted per step.
	Scenarios int
	Unbounded []string
}

func (e *InFlightBudgetError) Error() string {
	if len(e.Unbounded) > 0 {
		return fmt.Sprintf("%v: %s without a timeout would hold its requests forever if the target "+
			"stops answering, past any in-flight cap", ErrInFlightBudget, strings.Join(e.Unbounded, ", "))
	}

	// Reserved is one edge slot per call plus the late release: Calls of it
	// are the edge.
	edge := " for the call on the window's edge"
	if e.Calls > 1 {
		edge = ", one per configured call for the call on its window's edge"
	}

	// A scenario waits the sum of its steps' timeouts, a call its own.
	term := "rps × timeout"
	switch {
	case e.Scenarios == e.Calls && e.Scenarios > 0:
		term = "rps × Σ step timeouts"
	case e.Scenarios > 0:
		term = "rps × timeout for calls, rps × Σ step timeouts for scenarios"
	}

	return fmt.Sprintf("%v: a target that stops answering could hold up to %d requests in flight "+
		"(%s = %d, plus %d for calls released up to %v past their deadline, plus %d%s), "+
		"and the cap is %d",
		ErrInFlightBudget, e.Need, term, e.Need-e.Reserved, e.Reserved-e.Calls, ReleaseMargin, e.Calls, edge, e.Cap)
}

func (e *InFlightBudgetError) Unwrap() error { return ErrInFlightBudget }

// ReleaseMargin is how late past its deadline a slot may be released before
// the budget runs out: cancellation and scheduling are the generator's side.
// Measured up to 32ms under -race and 146ms with every core busy
// (decisions.md, "Запас бюджета…"); the value is a hypothesis.
const ReleaseMargin = 100 * time.Millisecond

// InFlightNeed is the smallest in-flight cap the calls can run with: the one
// checkInFlightBudget accepts. 0 when some call has no deadline.
func InFlightNeed(calls []Call) int {
	var e *InFlightBudgetError
	if !errors.As(checkInFlightBudget(calls, 0), &e) || len(e.Unbounded) > 0 {
		return 0
	}

	return e.Need
}

// checkInFlightBudget rejects calls that a hung target would push past the
// cap. A call needs ⌈rps × timeout⌉ slots for its window, one for the call
// scheduled on the window's closing edge, which starts before the first slot
// is released, and ⌈rps × ReleaseMargin⌉ for late release.
func checkInFlightBudget(calls []Call, maxInFlight int) error {
	var (
		need, peak, reserved, bounded, scenarios int
		unbounded                                []string
	)

	for _, call := range calls {
		rps := peakRPS(call)
		if rps == 0 {
			continue
		}

		// Zero means no deadline, as with Request.Deadline; a negative timeout
		// sets none either.
		if call.Timeout <= 0 {
			unbounded = append(unbounded, call.Method)

			continue
		}

		// An iteration holds its slot for every step in turn, each of which may
		// give the slot back late: the wait is Σ step timeouts, the margin one
		// per step.
		hold, margin, stepless := call.Timeout, ReleaseMargin, true
		for _, step := range call.Next {
			if step.Timeout <= 0 {
				unbounded = append(unbounded, step.Method)
				stepless = false

				continue
			}
			hold += step.Timeout
			margin += ReleaseMargin
		}
		if !stepless {
			continue
		}
		if len(call.Next) > 0 {
			scenarios++
		}

		reserve := saturatingAdd(1, inFlightFor(rps, margin))
		need = saturatingAdd(need, saturatingAdd(inFlightFor(rps, hold), reserve))
		reserved = saturatingAdd(reserved, reserve)
		bounded++
		peak = saturatingAdd(peak, rps)
	}

	if len(unbounded) > 0 {
		return &InFlightBudgetError{Need: math.MaxInt, Cap: maxInFlight, PeakRPS: peak, Unbounded: unbounded}
	}
	if need > maxInFlight {
		return &InFlightBudgetError{Need: need, Cap: maxInFlight, PeakRPS: peak, Reserved: reserved, Calls: bounded, Scenarios: scenarios}
	}

	return nil
}

func peakRPS(call Call) int {
	peak := 0
	for _, stage := range call.Stages {
		peak = max(peak, stage.StartRPS, stage.TargetRPS)
	}

	return peak
}

// inFlightFor is ⌈rps × timeout⌉, saturating at MaxInt: a product that wrapped
// around would pass the check.
func inFlightFor(rps int, timeout time.Duration) int {
	if rps <= 0 {
		return 0
	}

	hi, lo := bits.Mul64(uint64(rps), uint64(timeout)) //nolint:gosec // rps > 0 is checked above; config validation keeps timeout positive (ErrInvalidTimeout)
	if hi >= uint64(time.Second) {
		return math.MaxInt
	}

	quo, rem := bits.Div64(hi, lo, uint64(time.Second))
	if rem > 0 {
		quo++
	}
	if quo > math.MaxInt {
		return math.MaxInt
	}

	return int(quo)
}

func saturatingAdd(a, b int) int {
	if a > math.MaxInt-b {
		return math.MaxInt
	}

	return a + b
}
