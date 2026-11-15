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

package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// scenarioCalls is the calls of a config: Health/Check as step 1, then the
// steps given as YAML under steps:, in a scenario named name.
func scenarioCalls(name, rps, duration, steps string) string {
	return fmt.Sprintf("    - method: %s\n      rps: %s\n      duration: %s\n      scenario: %s\n      steps:\n%s",
		checkMethod, rps, duration, name, steps)
}

const stepUnary = "        - method: " + unaryCallMethod + "\n"

// A typo in a step method must not cost a run: the target is asked about it
// before the first request and the error names the step.
// Ground: contract — a method the target does not know is an error before the load (A1).
func TestRun_ScenarioStepTypoIsRefusedBeforeTheFirstRequest(t *testing.T) {
	target := startReflectingTarget(t)
	typo := "        - method: grpc.testing.TestService/UnaryCal\n"
	cfg := dryConfig(t, target.addr, "", scenarioCalls("pay", "50", "300ms", typo))

	res := runCLI(t.Context(), t, 5*time.Second, "-c", cfg)

	if res.err == nil {
		t.Fatalf("run succeeded, want the unknown step method refused\nstdout:\n%s", res.stdout)
	}
	for _, want := range []string{"load.calls[0].steps[0]", "grpc.testing.TestService/UnaryCal"} {
		if !strings.Contains(res.err.Error(), want) {
			t.Errorf("error %q lacks %q", res.err, want)
		}
	}
	if got := target.health.calls.Load(); got != 0 {
		t.Errorf("Health/Check got %d requests, want 0: the run must not start", got)
	}
	if got := len(target.recorder.allSimple()); got != 0 {
		t.Errorf("UnaryCall got %d requests, want 0", got)
	}
}

// A dry run sends one request per call; a scenario has no such meaning yet, and
// half of one would be a silent partial check.
// Ground: contract — spec 5, refused with the scenario named.
func TestRun_DryRunRefusesAScenario(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dryConfig(t, target.addr, "", scenarioCalls("pay-and-check", "50", "300ms", stepUnary))

	res := runCLI(t.Context(), t, 5*time.Second, "-c", cfg, "-dry-run")

	if got := exitCode(res.err); got != 1 {
		t.Fatalf("exit code %d, want 1\nerr %v", got, res.err)
	}
	for _, want := range []string{"dry run", "pay-and-check"} {
		if !strings.Contains(res.err.Error(), want) {
			t.Errorf("error %q lacks %q", res.err, want)
		}
	}
	if got := target.health.calls.Load() + int64(len(target.recorder.allSimple())); got != 0 {
		t.Errorf("the target got %d requests, want none from a refused dry run", got)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want nothing", res.stdout)
	}
}

// Ground: contract — spec 4, a search cannot take a scenario; refused at load,
// before any connection. Mutation 3 (allow it) dies here.
func TestRun_ScenarioWithABreakpointSearchIsRefusedAtLoad(t *testing.T) {
	cfg := dryConfig(t, closedPort(t), "", "    - method: "+checkMethod+"\n      scenario: s\n      steps:\n"+stepUnary+
		"  breakpoint:\n    from: 100\n    to: 800\n    settle: 1s\n    hold: 5s\n    p99_limit: 200ms\n")

	res := runCLI(t.Context(), t, 3*time.Second, "-c", cfg)

	if res.err == nil {
		t.Fatal("run succeeded, want a scenario with a search refused")
	}
	if want := "a scenario can't be searched yet"; !strings.Contains(res.err.Error(), want) {
		t.Errorf("error %q lacks %q", res.err, want)
	}
}

// The step timeouts add up: an iteration holds its slot for all of them, and a
// timeout advice worked out for one call would name a number that does not fix
// anything. The advice for a scenario is the cap alone.
// Ground: contract — A4; the number is checked by running CheckOptions with it.
func TestBudgetAdvice_ScenarioGivesOnlyTheCap(t *testing.T) {
	scenario := engine.Call{
		Method: "/a.B/One", Timeout: time.Second, Scenario: "s",
		Next: []engine.Step{
			{Method: "/a.B/Two", Timeout: time.Second},
			{Method: "/a.B/Three", Timeout: time.Second},
		},
		Stages: []engine.Stage{{StartRPS: 1000, TargetRPS: 1000, Duration: time.Second}},
	}
	plain := engine.Call{
		Method: "/a.B/Plain", Timeout: time.Second,
		Stages: []engine.Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Second}},
	}

	for name, calls := range map[string][]engine.Call{
		"a scenario":            {scenario},
		"a scenario and a call": {scenario, plain},
	} {
		t.Run(name, func(t *testing.T) {
			opts := engine.Options{Calls: calls, Sender: engine.FakeSender{}, MaxInFlight: 2000}
			err := engine.CheckOptions(opts)
			var budget *engine.InFlightBudgetError
			if !errors.As(err, &budget) {
				t.Fatalf("CheckOptions() = %v, want the budget refused", err)
			}

			advice := withBudgetAdvice(err, opts).Error()

			if strings.Contains(advice, "set timeout to at most") {
				t.Errorf("advice names a timeout for a scenario:\n%s", advice)
			}
			last := advice[strings.LastIndex(advice, "\n")+1:]
			for _, want := range []string{"Σ step timeouts", fmt.Sprintf("-max-in-flight %d", budget.Need)} {
				if !strings.Contains(last, want) {
					t.Errorf("advice line %q lacks %q", last, want)
				}
			}

			opts.MaxInFlight = budget.Need
			if err := engine.CheckOptions(opts); err != nil {
				t.Errorf("CheckOptions with the advised cap %d = %v, want it to pass", budget.Need, err)
			}
		})
	}
}

// A scenario holds its slot for every step in turn: 50 rps × (2s + 2s + 2s) is
// far past a cap of 100, and the run is refused before it connects. With the
// step timeout left at 0 (no deadline) the product would be rps × 2s and the
// default cap of this test would let it through. Mutation 6 dies here.
// Ground: boundary — the budget is Σ step timeouts, the default timeout is 2s per step.
func TestRun_ScenarioBudgetIsTheSumOfStepTimeouts(t *testing.T) {
	steps := "        - method: " + unaryCallMethod + "\n        - method: " + emptyCallMethod + "\n"
	cfg := dryConfig(t, closedPort(t), "", scenarioCalls("pay", "50", "300ms", steps))

	res := runCLI(t.Context(), t, 3*time.Second, "-max-in-flight", "250", "-c", cfg)

	if !errors.Is(res.err, engine.ErrInFlightBudget) {
		t.Fatalf("error = %v, want %v (50 rps × 6s of step timeouts over a cap of 250)", res.err, engine.ErrInFlightBudget)
	}
	if want := "Σ step timeouts"; !strings.Contains(res.err.Error(), want) {
		t.Errorf("error %q lacks %q", res.err, want)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want no report", res.stdout)
	}
}
