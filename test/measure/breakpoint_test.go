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

package measure

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
	"github.com/yhgrwav/leettest/test/stand"
)

// stepsOn runs each step through the whole stack against the stand, on one
// connection for the search, the step's settle as warm-up.
func stepsOn(t *testing.T, s *stand.Stand, timeout time.Duration) breakpoint.RunStep {
	t.Helper()

	sender := grpcsender.New(grpcsender.Options{
		Target:      s.Target(),
		DialOptions: []grpc.DialOption{s.DialOption()},
	})
	if err := sender.Connect(t.Context()); err != nil {
		t.Fatalf("connect to the stand: %v", err)
	}
	t.Cleanup(func() { _ = sender.Close() })

	return func(ctx context.Context, rps int, settle, hold time.Duration, maxInFlight int) (engine.Report, error) {
		eng, err := engine.New(engine.Options{
			Calls: []engine.Call{{
				Method: s.Method(), Timeout: timeout,
				Stages: []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: hold}},
			}},
			Sender:      sender,
			MaxInFlight: maxInFlight,
			Warmup:      settle,
		})
		if err != nil {
			return engine.Report{}, err
		}
		if err := eng.Run(ctx); err != nil {
			return engine.Report{}, err
		}

		return eng.Report(), nil
	}
}

// The premise of the cooldown before a repeat: a target keeps working off
// the calls we gave up on, so a step right after a broken one meets its
// backlog. 400 rps for 1.5s against 270 leaves (1/270 − 1/400) × 600 ≈ 0.72s
// queued; 244 rps right after waits it out (p99 far over 20ms). After the
// search's cooldown, max(timeout, settle) = 500ms, ~0.2s is left, and a step
// below the capacity works it off at its start: p99 stays near 20ms.
func TestBreakpoint_ATargetsQueueOutlivesTheStep(t *testing.T) {
	target := stand.Start(stand.Capacity(270, 20*time.Millisecond))
	t.Cleanup(target.Stop)

	run := stepsOn(t, target, 500*time.Millisecond)
	p99 := func(rps int) time.Duration {
		r, err := run(t.Context(), rps, 0, 1500*time.Millisecond, 2*rps)
		if err != nil {
			t.Fatalf("run %d: %v", rps, err)
		}

		return r.Methods[0].P99.Value
	}

	p99(400)
	if got := p99(244); got < 100*time.Millisecond {
		t.Errorf("right after a broken step, 244 rps p99 %v: no backlog seen", got)
	}

	p99(400)
	time.Sleep(500 * time.Millisecond)
	if got := p99(244); got > 40*time.Millisecond {
		t.Errorf("after a cooldown, 244 rps p99 %v, want near the 20ms delay", got)
	}
}

// A target of known capacity is the outside source for "breaks at": the
// stand serves 270 calls a second, and the search must name an interval that
// holds it — held at 244, broke at 305, the steps of ×1.25 from 100 around
// 270. At 305 the queue grows by 1/270 − 1/305 = 0.42ms a call, ~320ms over
// the 2.5s hold: under the 500ms timeout, but p99 ~340ms against the first
// step's ~20ms is past the 3× knee. At 244 nothing queues.
func TestBreakpoint_FindsTheStandsCapacity(t *testing.T) {
	target := stand.Start(stand.Capacity(270, 20*time.Millisecond))
	t.Cleanup(target.Stop)

	plan := breakpoint.Plan{
		From: 100, To: 400, Settle: 500 * time.Millisecond, Hold: 2500 * time.Millisecond,
		Timeout: 500 * time.Millisecond,
	}
	res, err := breakpoint.Search(t.Context(), plan, stepsOn(t, target, plan.Timeout))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if res.Outcome != breakpoint.BrokeBetween || res.Held >= 270 || res.Broke < 270 {
		t.Errorf("%v: held %d, broke %d; want BrokeBetween with 270 in (held, broke]", res.Outcome, res.Held, res.Broke)
	}
	for _, step := range res.Steps {
		t.Logf("%d rps: sent %d, failed %d, broken %v (%s)", step.RPS, step.Report.Sent, step.Report.Failed, step.Broken, step.Why)
	}
}

// A target that does not come back after its break: once a call has queued
// over 100ms, it hangs every call from then on. The probes never recover,
// and the search names the break without a repeat.
func TestBreakpoint_ATargetThatDoesNotRecoverIsNamedSo(t *testing.T) {
	var broken atomic.Bool
	limited := stand.Capacity(270, 20*time.Millisecond)
	target := stand.Start(func(c stand.Call) stand.Behavior {
		if broken.Load() {
			return stand.Behavior{Hang: true}
		}
		b := limited(c)
		if b.Delay > 100*time.Millisecond {
			broken.Store(true)
		}

		return b
	})
	t.Cleanup(target.Stop)

	plan := breakpoint.Plan{
		From: 100, To: 400, Settle: 500 * time.Millisecond, Hold: 2500 * time.Millisecond,
		Timeout: 500 * time.Millisecond,
	}
	res, err := breakpoint.Search(t.Context(), plan, stepsOn(t, target, plan.Timeout))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	recovered := slices.ContainsFunc(res.Notes, func(n string) bool { return strings.Contains(n, "did not recover") })
	if res.Outcome != breakpoint.BrokeBetween || !recovered {
		t.Errorf("%v notes %q, want BrokeBetween that did not recover", res.Outcome, res.Notes)
	}
	if slices.ContainsFunc(res.Steps, func(s breakpoint.Step) bool { return s.Repeat }) {
		t.Errorf("a repeat ran against a target that never recovered")
	}
}
