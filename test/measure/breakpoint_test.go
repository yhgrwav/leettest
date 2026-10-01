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

	return func(ctx context.Context, rps int, settle, hold time.Duration) (engine.Report, error) {
		eng, err := engine.New(engine.Options{
			Calls: []engine.Call{{
				Method: s.Method(), Timeout: timeout,
				Stages: []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: hold}},
			}},
			Sender:      sender,
			MaxInFlight: 2 * rps,
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

// A target of known capacity is the outside source for "breaks at": the
// stand serves 270 calls a second, and the search must name an interval that
// holds it — held at 244, broke at 305, the steps of ×1.25 from 100 around
// 270. At 305 the queue grows by 1/270 − 1/305 = 0.42ms a call, ~320ms over
// the 2.5s hold: under the 500ms timeout, but p99 ~340ms against the first
// step's ~20ms is past the 3× knee. At 244 nothing queues.
func TestBreakpoint_FindsTheStandsCapacity(t *testing.T) {
	target := stand.Start(stand.Capacity(270, 20*time.Millisecond))
	t.Cleanup(target.Stop)

	plan := breakpoint.Plan{From: 100, To: 400, Settle: 500 * time.Millisecond, Hold: 2500 * time.Millisecond}
	res, err := breakpoint.Search(t.Context(), plan, stepsOn(t, target, 500*time.Millisecond))
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
