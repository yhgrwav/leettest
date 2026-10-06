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
	"fmt"
	"math"
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
	switch {
	case p.From < 1:
		return nil, fmt.Errorf("%w: from %d, want at least 1 rps", ErrPlan, p.From)
	case p.To < p.From:
		return nil, fmt.Errorf("%w: to %d is below from %d", ErrPlan, p.To, p.From)
	case p.Factor != 0 && p.Step != 0:
		return nil, fmt.Errorf("%w: factor and step both set; set one", ErrPlan)
	case p.Factor != 0 && p.Factor <= 1:
		return nil, fmt.Errorf("%w: factor %v, want over 1", ErrPlan, p.Factor)
	case p.Step < 0:
		return nil, fmt.Errorf("%w: step %d, want over 0", ErrPlan, p.Step)
	case p.Hold <= 0:
		return nil, fmt.Errorf("%w: no hold", ErrPlan)
	case p.Settle < 0 || 2*p.Settle >= p.Hold:
		return nil, fmt.Errorf("%w: settle %v, want under half the hold %v", ErrPlan, p.Settle, p.Hold)
	}

	factor := p.Factor
	if factor == 0 {
		factor = 1.25
	}
	rates := []int{p.From}
	for {
		prev := rates[len(rates)-1]
		next := prev + p.Step
		if p.Step == 0 {
			next = max(prev+1, int(math.Round(float64(prev)*factor)))
		}
		if next > p.To || next <= prev {
			return rates, nil
		}
		rates = append(rates, next)
	}
}

// RunStep runs one step: rps for hold, the first settle of it out of the
// statistics (as warm-up), with an in-flight cap of maxInFlight, and returns
// the run's report.
type RunStep func(ctx context.Context, rps int, settle, hold time.Duration, maxInFlight int) (engine.Report, error)

const (
	// ProbeCalls is how many calls a recovery probe at the first step's rate
	// holds for at least, so its p99 rests on 5 tail calls; it holds a
	// second at the least.
	ProbeCalls = 500
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
	// generator late, in-flight cap or the stream limit: nothing is said
	// about the target above Result.Held.
	RunLimit
	// Stopped: the context ended the search; the last of Result.Steps is the
	// run it ended, Result.Held what held before it.
	Stopped
	// Invalid: a run was an invalid run (ErrInvalidRun); no breaking point.
	Invalid
)

// ErrInvalidRun, wrapped by a RunStep's error, says the run's numbers cannot
// be trusted (clock step, every call a request error); the search ends Invalid.
var (
	ErrInvalidRun    = errors.New("invalid run")
	ErrClockStep     = fmt.Errorf("%w: clock step", ErrInvalidRun)
	ErrRequestErrors = fmt.Errorf("%w: every call a request error", ErrInvalidRun)
)

// Cause is the closed list of reasons a run broke, gave out or was invalid;
// Step.Why carries the same with its numbers.
type Cause int

const (
	NoCause Cause = iota
	// The target's side.
	CauseErrors
	CauseP99Limit
	CauseP99VsBase
	CauseConnection
	CauseNoRecovery
	// The run's side.
	CauseGenerator
	CauseInFlightCap
	CauseStreamLimit
	CauseStreamWait
	// Validity.
	CauseClockStep
	CauseRequestErrors
)

// Step is one step's run and verdict.
type Step struct {
	RPS    int
	Report engine.Report
	// Broken says the step broke; Why names the criterion with its numbers.
	Broken bool
	Why    string
	Cause  Cause
	Kind   Kind
	// Recovered, for a Probe, says its p99 came within Recovered times the
	// baseline: a probe that did not break may still not have recovered.
	Recovered bool
}

// Worst is the longest the search can take: every step breaks once and
// holds on its repeat after the cooldown and all probes.
func (p Plan) Worst() (time.Duration, error) {
	rates, err := p.Rates()
	if err != nil {
		return 0, err
	}
	n := time.Duration(len(rates))
	runs := 2*n + (n-1)*MaxProbes

	return 2*n*(p.Hold+p.Timeout) + (n-1)*MaxProbes*(ProbeHold(rates[0])+p.Timeout) + n*p.Cooldown() + runs*RunSlack, nil
}

// RunSlack is what a run takes beyond its hold and timeout in Worst: building
// the engine, a coarse timer (15.6ms on Windows), a late cancel. Hypothesis,
// docs/decisions.md: measured 0.5ms a run on Windows; 100ms keeps "at most" true.
const RunSlack = 100 * time.Millisecond

// Kind is what a run in Result.Steps was. Held and Broke come only from
// RateStep and Repeat runs, never from a Probe.
type Kind int

const (
	// RateStep is a step of the profile.
	RateStep Kind = iota
	// Repeat confirms or clears a broken step after the cooldown.
	Repeat
	// Probe checks at the first step's rate that the target has recovered.
	Probe
)

func (k Kind) String() string {
	switch k {
	case Repeat:
		return "repeat"
	case Probe:
		return "probe"
	default:
		return "step"
	}
}

type Result struct {
	Outcome     Outcome
	Held, Broke int
	// Cause is the reason of the run that ended the search.
	Cause Cause
	Steps []Step
	// Notes say what the outcome does not: a step that broke once and held
	// on its repeat, a knee with no baseline below it.
	Notes []string
}

// Cooldown is the pause without load before a broken step is repeated.
func (p Plan) Cooldown() time.Duration {
	return max(p.Timeout, p.Settle)
}

// ProbeHold is how long a recovery probe at rps runs: max(1s, ProbeCalls/rps).
func ProbeHold(rps int) time.Duration {
	return max(time.Second, time.Duration(ProbeCalls)*time.Second/time.Duration(rps))
}

// Run is a run about to start, as an Observer sees it. Step is the 1-based
// index of the step it runs or repeats, of at most Steps; Probe the 1-based
// index of a probe, of at most MaxProbes.
type Run struct {
	RPS   int
	Kind  Kind
	Step  int
	Steps int
	Probe int
}

// Observer is told of the search's progress, only between runs, never inside
// one's measured window. It must return quickly: the search waits for it, and
// the time it takes is added to the search's.
type Observer struct {
	// Started is called before each run.
	Started func(Run)
	// Finished is called after each run with its judgement.
	Finished func(Step)
	// Cooldown is called before the pause without load ahead of a repeat of
	// rps.
	Cooldown func(d time.Duration, rps int)
}

// SearchWith is Search telling obs of its progress.
func SearchWith(ctx context.Context, plan Plan, run RunStep, obs Observer) (Result, error) {
	rates, err := plan.Rates()
	if err != nil {
		return Result{}, err
	}

	s := search{plan: plan, run: run, first: rates[0], obs: obs, steps: len(rates)}
	res, err := s.loop(ctx, rates)
	switch {
	case errors.Is(err, ErrInvalidRun):
		res.Outcome, res.Broke, res.Cause = Invalid, 0, res.Steps[len(res.Steps)-1].Cause

		return res, nil
	case ctx.Err() != nil && errors.Is(err, ctx.Err()):
		res.Outcome, res.Broke, res.Cause = Stopped, 0, NoCause

		return res, nil
	}

	return res, err
}

// Search runs the plan's steps from the lowest until one breaks and a repeat
// of it breaks too, or the plan ends. A context ended on the way is the
// outcome Stopped and an invalid run the outcome Invalid, not errors.
func Search(ctx context.Context, plan Plan, run RunStep) (Result, error) {
	return SearchWith(ctx, plan, run, Observer{})
}

type search struct {
	plan     Plan
	run      RunStep
	first    int
	baseline time.Duration
	res      Result

	obs Observer
	// index is the 1-based step under way, of steps; probeN the probe's.
	index, steps, probeN int
}

// record keeps a run's judgement and tells the observer.
func (s *search) record(step Step) {
	s.res.Steps = append(s.res.Steps, step)
	if s.obs.Finished != nil {
		s.obs.Finished(step)
	}
}

func (s *search) started(rps int, kind Kind) {
	if s.obs.Started == nil {
		return
	}
	r := Run{RPS: rps, Kind: kind, Step: s.index, Steps: s.steps}
	if kind == Probe {
		r.Probe = s.probeN
	}
	s.obs.Started(r)
}

func (s *search) loop(ctx context.Context, rates []int) (Result, error) {
	plan := s.plan
	for i, rps := range rates {
		s.index = i + 1
		step, err := s.step(ctx, rps, plan.Hold, RateStep)
		if err != nil || s.limited(step) {
			return s.res, err
		}
		if !step.Broken {
			s.hold(step)

			continue
		}

		if s.obs.Cooldown != nil {
			s.obs.Cooldown(plan.Cooldown(), rps)
		}
		if cooled := sleep(ctx, plan.Cooldown()); cooled != nil {
			return s.res, cooled
		}
		if i > 0 {
			var recovered bool
			if recovered, err = s.probe(ctx); err != nil {
				return s.res, err
			}
			if !recovered {
				s.res.Notes = append(s.res.Notes, fmt.Sprintf("%d: broke and did not recover within %d probes of %v at %d rps",
					rps, MaxProbes, ProbeHold(s.first), s.first))

				s.broke(step, BrokeBetween)
				s.res.Cause = CauseNoRecovery

				return s.res, nil
			}
		}

		repeat, err := s.step(ctx, rps, plan.Hold, Repeat)
		if err != nil {
			return s.res, err
		}
		if s.limited(repeat) {
			return s.res, nil
		}
		if repeat.Broken {
			if i == 0 {
				s.res.Notes = append(s.res.Notes, "no lower step to check recovery against; start lower (from) for a reliable result")

				return s.broke(repeat, BrokeAtFirst), nil
			}

			return s.broke(repeat, BrokeBetween), nil
		}
		s.res.Notes = append(s.res.Notes, fmt.Sprintf("%d broke once, held on repeat", rps))
		s.hold(repeat)
	}

	s.res.Outcome = HeldThroughout

	return s.res, nil
}

// step runs rps for hold and judges it, or names the cap that cannot run it.
func (s *search) step(ctx context.Context, rps int, hold time.Duration, kind Kind) (Step, error) {
	need := engine.InFlightNeed([]engine.Call{{
		Timeout: s.plan.Timeout,
		Stages:  []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: hold}},
	}})
	step := Step{RPS: rps, Kind: kind}
	if s.plan.MaxInFlight > 0 && need > s.plan.MaxInFlight {
		step.Cause = CauseInFlightCap
		step.Why = fmt.Sprintf("in-flight cap %d is too low for %d rps with timeout %v", s.plan.MaxInFlight, rps, s.plan.Timeout)
		s.record(step)

		return step, nil
	}
	if s.plan.MaxInFlight > 0 {
		need = s.plan.MaxInFlight
	}

	s.started(rps, kind)
	report, err := s.run(ctx, rps, s.plan.Settle, hold, need)
	step.Report = report
	switch {
	case errors.Is(err, ErrInvalidRun):
		step.Cause, step.Why = CauseRequestErrors, err.Error()
		if errors.Is(err, ErrClockStep) {
			step.Cause = CauseClockStep
		}
		s.record(step)

		return step, err
	case ctx.Err() != nil:
		// Stopped inside the run: kept as it ran, never judged.
		s.record(step)

		return step, ctx.Err()
	case err != nil:
		return step, err
	}
	step.Broken, step.Cause, step.Why = s.waits(rps, report)
	if step.Why == "" {
		step.Cause, step.Why = undersent(rps, report)
	}
	if step.Why == "" {
		step.Broken, step.Cause, step.Why = s.broken(report)
	}
	if kind == Probe && !step.Broken && step.Why == "" {
		step.Recovered = float64(p99(report)) <= Recovered*float64(s.baseline)
	}
	s.record(step)

	return step, nil
}

// undersent names a run whose generator did not send its schedule: sent
// short of the calls scheduled in the measured window by over 0.1%, at least
// one call.
func undersent(rps int, r engine.Report) (cause Cause, why string) {
	if r.Sent >= r.Scheduled-max(1, r.Scheduled/1000) {
		return NoCause, ""
	}

	return CauseGenerator, fmt.Sprintf("the generator sent %d of %d scheduled calls at %d rps; the target above that is untested",
		r.Sent, r.Scheduled, rps)
}

// limited ends the search on a step where the run gave out.
func (s *search) limited(step Step) bool {
	if step.Broken || step.Why == "" {
		return false
	}
	s.res.Outcome, s.res.Broke, s.res.Cause = RunLimit, step.RPS, step.Cause

	return true
}

func (s *search) hold(step Step) {
	s.res.Held = step.RPS
	if p99 := p99(step.Report); s.baseline == 0 || p99 < s.baseline {
		s.baseline = p99
	}
}

func (s *search) broke(step Step, outcome Outcome) Result {
	s.res.Outcome, s.res.Broke, s.res.Cause = outcome, step.RPS, step.Cause

	return s.res
}

// waits judges a step by its client-side waits (engine.Report.WaitVerdict):
// the run's side gave out — why without broken — or the target's side broke.
// Nothing to say: "".
func (s *search) waits(rps int, r engine.Report) (broken bool, cause Cause, why string) {
	if r.CapHit != nil {
		return false, CauseInFlightCap, fmt.Sprintf("in-flight cap reached at %d rps; the target above that is untested", rps)
	}
	wait, side, ok := r.WaitVerdict()
	switch {
	case !ok:
		return false, NoCause, ""
	case side == engine.SideTarget:
		return true, CauseConnection, fmt.Sprintf("the connection to the target was not ready for %d calls at %d rps", r.ConnectionCauseCalls, rps)
	case wait == engine.WaitStream && r.Connections != nil && r.Connections.LimitAnnounced:
		return false, CauseStreamLimit, fmt.Sprintf("stream limit %d of a single connection reached at %d rps; the target above that is untested",
			r.Connections.LastLimit, rps)
	case wait == engine.WaitStream:
		return false, CauseStreamWait, fmt.Sprintf("calls waited for a stream at %d rps; the target above that is untested", rps)
	default:
		return false, CauseGenerator, fmt.Sprintf("the generator fell behind at %d rps; the target above that is untested", rps)
	}
}

// broken judges a step the run held: failures, then the user's p99 limit or
// the knee over the baseline.
func (s *search) broken(r engine.Report) (broken bool, cause Cause, why string) {
	if r.Sent > 0 && float64(r.Failed)/float64(r.Sent) >= FailShare {
		return true, CauseErrors, fmt.Sprintf("failed %d of %d calls (%.1f%%)", r.Failed, r.Sent, 100*float64(r.Failed)/float64(r.Sent))
	}
	got := p99(r)
	if s.plan.P99Limit > 0 {
		if got > s.plan.P99Limit {
			return true, CauseP99Limit, fmt.Sprintf("p99 %v over p99_limit %v", short(got), s.plan.P99Limit)
		}

		return false, NoCause, ""
	}
	if s.baseline > 0 && got > KneeRatio*s.baseline {
		return true, CauseP99VsBase, fmt.Sprintf("p99 %v = %.1fx baseline %v (no p99_limit set)", short(got), float64(got)/float64(s.baseline), short(s.baseline))
	}

	return false, NoCause, ""
}

// probe runs up to MaxProbes probes at the first step's rate until one comes
// within Recovered of the baseline.
func (s *search) probe(ctx context.Context) (bool, error) {
	for k := range MaxProbes {
		s.probeN = k + 1
		step, err := s.step(ctx, s.first, ProbeHold(s.first), Probe)
		if err != nil {
			return false, err
		}
		if step.Recovered {
			return true, nil
		}
	}

	return false, nil
}

// short rounds d to 3 significant digits for the text.
func short(d time.Duration) time.Duration {
	unit := time.Duration(1)
	for d/unit >= 1000 {
		unit *= 10
	}

	return d.Round(unit)
}

// p99 is the highest p99 over the methods.
func p99(r engine.Report) time.Duration {
	var top time.Duration
	for i := range r.Methods {
		if m := &r.Methods[i]; m.P99.Defined {
			top = max(top, m.P99.Value)
		}
	}

	return top
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
