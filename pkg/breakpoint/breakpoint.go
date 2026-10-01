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

package breakpoint

import (
	"context"
	"errors"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

var ErrPlan = errors.New("breaking-point plan")

// Plan is a rising profile: rates from From up to To, each step Factor times
// the last (or Step more, when Step is set), each held for Hold, of which the
// first Settle is out of the step's verdict.
type Plan struct {
	From, To int
	// Factor is the geometric step, 1.25 when zero and Step is zero.
	Factor float64
	// Step is an absolute step in rps; it excludes Factor.
	Step   int
	Settle time.Duration
	Hold   time.Duration
	// Timeout is the calls' deadline. A broken step is repeated after a
	// cooldown of max(Timeout, Settle) without load, so the target's queue
	// left from the first try does not confirm the break.
	Timeout time.Duration
	// MaxInFlight is the in-flight cap. Zero: each step gets the smallest it
	// can run with (engine.InFlightNeed). Set and too low for a step: the
	// search stops there with RunLimit, keeping what held below.
	MaxInFlight int
	// P99Limit, when set, breaks a step whose p99 is above it. Without it a
	// step breaks on the knee: p99 over KneeRatio times the baseline, the
	// lowest p99 of the steps that held before it.
	P99Limit time.Duration
}

const (
	// FailShare breaks a step whose failed calls are this share or more of
	// the sent ones. Hypothesis, docs/decisions.md.
	FailShare = 0.01
	// KneeRatio breaks a step whose p99 is over this many times the
	// baseline, when no P99Limit is set. Hypothesis, docs/decisions.md.
	KneeRatio = 3
)

// Rates are the step rates of the plan, rounded to whole rps, none over To,
// each above the last: next = max(prev+1, round(prev×Factor)).
func (p Plan) Rates() ([]int, error) {
	return nil, nil
}

// RunStep runs one step: rps for hold, the first settle of it out of the
// statistics (as warm-up), with an in-flight cap of maxInFlight, and returns
// the run's report.
type RunStep func(ctx context.Context, rps int, settle, hold time.Duration, maxInFlight int) (engine.Report, error)

const (
	// ProbeHold is how long a recovery probe runs, at the first step's rate.
	ProbeHold = time.Second
	// MaxProbes is how many probes a broken step waits for the target to
	// recover before its repeat.
	MaxProbes = 5
	// Recovered is how close to the baseline a probe's p99 must come.
	Recovered = 1.5
)

// Outcome is what the search found.
type Outcome int

const (
	// BrokeBetween: the target held at Result.Held and broke at Result.Broke.
	BrokeBetween Outcome = iota + 1
	// BrokeAtFirst: the first step already broke: the limit is at or below
	// Result.Broke.
	BrokeAtFirst
	// HeldThroughout: no step broke; the limit is above Result.Held.
	HeldThroughout
	// RunLimit: the run, not the target, gave out at Result.Broke —
	// generator late, in-flight cap, connection or stream: nothing is said
	// about the target above Result.Held.
	RunLimit
)

// Step is one step's run and verdict.
type Step struct {
	RPS    int
	Report engine.Report
	// Broken says the step broke; Why names the criterion with its numbers.
	Broken bool
	Why    string
	// Repeat marks the run that confirmed a broken step; Probe a recovery
	// probe before it.
	Repeat bool
	Probe  bool
}

type Result struct {
	Outcome     Outcome
	Held, Broke int
	Steps       []Step
	// Notes say what the outcome does not: a step that broke once and held
	// on its repeat, a knee with no baseline below it.
	Notes []string
}

// Cooldown is the pause without load before a broken step is repeated.
func (p Plan) Cooldown() time.Duration {
	return 0
}

// Search runs the plan's steps from the lowest until one breaks and a repeat
// of it breaks too, or the plan ends.
func Search(ctx context.Context, plan Plan, run RunStep) (Result, error) {
	return Result{}, nil
}
