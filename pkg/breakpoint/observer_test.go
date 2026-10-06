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
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// longestPath is a target down the search's longest path over 1000 and
// 1250 rps: each step breaks once, four slow probes and a fifth recovered,
// each repeat holds.
func longestPath() func(rps int) engine.Report {
	seen, probes := map[int]int{}, 0
	probing := false

	return func(rps int) engine.Report {
		if probing && rps == 1000 {
			probes++
			if probes < MaxProbes {
				return report(rps, 0, 45*time.Millisecond, 45*time.Millisecond)
			}
			probing = false

			return report(rps, 0, 20*time.Millisecond, 20*time.Millisecond)
		}
		seen[rps]++
		if seen[rps] == 1 {
			probing = rps != 1000

			return report(rps, rps/2, 30*time.Millisecond, 30*time.Millisecond)
		}

		return report(rps, 0, 20*time.Millisecond, 20*time.Millisecond)
	}
}

var longPlan = Plan{From: 1000, To: 1250, Settle: 10 * time.Millisecond, Hold: 50 * time.Millisecond, Timeout: 30 * time.Millisecond}

// The observer hears every run before it starts, with its kind and where it
// stands: step i of at most n, a repeat of step i, probe k of at most 5; and
// every run after, with its judgement; and each cooldown.
func TestSearchWith_TellsEveryRun(t *testing.T) {
	var started []Run
	var finished []Kind
	var cooldowns []int
	var asked []int
	obs := Observer{
		Started:  func(r Run) { started = append(started, r) },
		Finished: func(s Step) { finished = append(finished, s.Kind) },
		Cooldown: func(_ time.Duration, rps int) { cooldowns = append(cooldowns, rps) },
	}
	res, err := SearchWith(t.Context(), longPlan, fake(longestPath(), &asked), obs)
	if err != nil || res.Outcome != HeldThroughout {
		t.Fatalf("%v, %v: want HeldThroughout down the longest path", res.Outcome, err)
	}
	want := []Run{
		{RPS: 1000, Kind: RateStep, Step: 1, Steps: 2},
		{RPS: 1000, Kind: Repeat, Step: 1, Steps: 2},
		{RPS: 1250, Kind: RateStep, Step: 2, Steps: 2},
	}
	for k := 1; k <= MaxProbes; k++ {
		want = append(want, Run{RPS: 1000, Kind: Probe, Step: 2, Steps: 2, Probe: k})
	}
	want = append(want, Run{RPS: 1250, Kind: Repeat, Step: 2, Steps: 2})
	if !slices.Equal(started, want) {
		t.Errorf("started\n%v\nwant\n%v", started, want)
	}
	kinds := make([]Kind, 0, len(res.Steps))
	for _, s := range res.Steps {
		kinds = append(kinds, s.Kind)
	}
	if !slices.Equal(finished, kinds) {
		t.Errorf("finished %v, want the runs' kinds %v", finished, kinds)
	}
	if !slices.Equal(cooldowns, []int{1000, 1250}) {
		t.Errorf("cooldowns before %v, want [1000 1250]", cooldowns)
	}
}

// The observer is called only between runs: no call falls inside a run, so
// a slow screen never lands in a measured window.
func TestSearchWith_TheObserverIsNeverInsideARun(t *testing.T) {
	var mu sync.Mutex
	inRun := false
	var calledInside int
	note := func() {
		mu.Lock()
		defer mu.Unlock()
		if inRun {
			calledInside++
		}
	}
	run := func(_ context.Context, rps int, _, _ time.Duration, _ int) (engine.Report, error) {
		mu.Lock()
		inRun = true
		mu.Unlock()
		defer func() {
			mu.Lock()
			inRun = false
			mu.Unlock()
		}()

		return capacity(270)(rps), nil
	}
	calls := 0
	obs := Observer{
		Started:  func(Run) { calls++; note() },
		Finished: func(Step) { calls++; note() },
		Cooldown: func(time.Duration, int) { calls++; note() },
	}
	if _, err := SearchWith(t.Context(), plan, run, obs); err != nil {
		t.Fatalf("search: %v", err)
	}
	if calls == 0 || calledInside != 0 {
		t.Errorf("%d observer calls, %d inside a run; want some, none inside", calls, calledInside)
	}
}

// An observer that takes 200ms changes nothing a run measured: every run of
// a real engine still sends all it scheduled.
func TestSearchWith_ASlowObserverChangesNoRun(t *testing.T) {
	p := Plan{From: 100, To: 156, Settle: 50 * time.Millisecond, Hold: 300 * time.Millisecond, Timeout: 200 * time.Millisecond}
	run := func(ctx context.Context, rps int, settle, hold time.Duration, need int) (engine.Report, error) {
		eng, err := engine.New(engine.Options{
			Calls: []engine.Call{{Method: "/a.B/C", Timeout: p.Timeout,
				Stages: []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: hold}}}},
			Sender: engine.FakeSender{Delay: time.Millisecond}, MaxInFlight: need, Warmup: settle,
		})
		if err != nil {
			return engine.Report{}, err
		}
		err = eng.Run(ctx)

		return eng.Report(), err
	}
	slow := func() { time.Sleep(200 * time.Millisecond) }
	obs := Observer{
		Started:  func(Run) { slow() },
		Finished: func(Step) { slow() },
		Cooldown: func(time.Duration, int) { slow() },
	}
	res, err := SearchWith(t.Context(), p, run, obs)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, s := range res.Steps {
		if s.Report.Scheduled == 0 || s.Report.Sent != s.Report.Scheduled {
			t.Errorf("%d rps %v: sent %d of %d scheduled", s.RPS, s.Kind, s.Report.Sent, s.Report.Scheduled)
		}
	}
}
