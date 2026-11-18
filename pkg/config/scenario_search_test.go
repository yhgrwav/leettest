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

package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// Search of a scenario (reviews/scenario-breakpoint.md §3, unit U2). The call is
// the head: its rate is the search's, in iterations per second.

// scenarioSearch is a search over a scenario of three steps; stepExtra goes
// under the first step (indented 10 spaces), headExtra under the head (6),
// searchExtra under breakpoint (4).
func scenarioSearch(headExtra, stepExtra, searchExtra string) []byte {
	steps := "        - method: " + transfer + "\n" + stepExtra + "        - method: " + getHistory + "\n"

	return []byte(scenarioHead + "  calls:\n    - method: " + getBalance + "\n      timeout: 500ms\n" + headExtra +
		"      scenario: s\n      steps:\n" + steps +
		"  breakpoint:\n    from: 100\n    to: 2000\n    settle: 5s\n    hold: 30s\n" + searchExtra)
}

// Ground: contract (spec 3.1) — a scenario with a breakpoint section is a valid
// config: the refusal "a scenario can't be searched yet" is gone. Mutation
// "keep the refusal" dies here.
func TestScenarioSearch_AScenarioWithABreakpointIsValid(t *testing.T) {
	cfg, err := config.Parse(scenarioSearch("", "", ""))
	if err != nil {
		t.Fatalf("Parse() = %v, want a scenario search accepted", err)
	}

	call := cfg.Load.Calls[0]
	if call.Scenario != "s" || len(call.Steps) != 2 {
		t.Errorf("scenario %q with %d steps, want s with 2", call.Scenario, len(call.Steps))
	}
	if cfg.Load.Breakpoint == nil {
		t.Fatal("Breakpoint = nil, want the section read")
	}
	if got := cfg.Load.Breakpoint.Plan(call.Timeout, 0).StepTimeouts; len(got) != 0 {
		t.Errorf("the config's plan carries StepTimeouts %v: the runner adds them, the config knows no steps' deadlines of the search", got)
	}
}

// Ground: contract (spec 3.2) — a step's pass under a search is refused with the
// path of the step and the text of the head's refusal. Mutation "remove the
// steps[k].pass refusal" dies here: the config would be accepted and the limit
// silently never judged.
func TestScenarioSearch_AStepsPassIsRefusedWithItsPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  []byte
		want string
	}{
		{"the first step", scenarioSearch("", "          pass:\n            errors: 1%\n", ""), "load.calls[0].steps[0]: pass with load.breakpoint: a search has no single p99, use breakpoint.pass.held_rps"},
		{"the second step", []byte(strings.Replace(string(scenarioSearch("", "", "")),
			"        - method: "+getHistory+"\n", "        - method: "+getHistory+"\n          pass:\n            p99: 200ms\n", 1)),
			"load.calls[0].steps[1]: pass with load.breakpoint: a search has no single p99, use breakpoint.pass.held_rps"},
	} {
		_, err := config.Parse(tc.cfg)
		if err == nil {
			t.Errorf("%s: accepted, want a refusal", tc.name)

			continue
		}
		if !errors.Is(err, config.ErrThreshold) {
			t.Errorf("%s: %v, want ErrThreshold", tc.name, err)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %q lacks %q", tc.name, err, tc.want)
		}
	}
}

// The head's own pass under a search is refused as it was, for a scenario's head
// too (red only behind the lifted refusal).
func TestScenarioSearch_TheHeadsPassIsStillRefused(t *testing.T) {
	refuses(t, "the head's pass", scenarioSearch("      pass:\n        errors: 1%\n", "", ""),
		"load.calls[0]", "pass with load.breakpoint", "breakpoint.pass.held_rps")
}

// Ground: contract (spec 3.3) — breakpoint.p99_limit with a scenario is refused
// with the exact text; a call's p99_limit stays accepted. Mutation "allow
// p99_limit with a scenario" dies here.
func TestScenarioSearch_P99LimitIsRefusedWithAScenario(t *testing.T) {
	_, err := config.Parse(scenarioSearch("", "", "    p99_limit: 200ms\n"))
	if err == nil {
		t.Fatal("accepted, want p99_limit refused with a scenario")
	}
	if !errors.Is(err, config.ErrBreakpoint) {
		t.Errorf("%v, want ErrBreakpoint", err)
	}

	want := "load.breakpoint.p99_limit with a scenario: a step's p99 is judged by the knee, " +
		"a limit for every step or for the iteration is not decided yet; leave p99_limit out"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q lacks %q", err, want)
	}

	// A plain call's p99_limit is a limit as before.
	if _, err := config.Parse([]byte(searchYAML)); err != nil {
		t.Errorf("a call with p99_limit: %v, want accepted", err)
	}
}

// Ground: contract (spec 3.4) — held_rps is iterations per second for a
// scenario, and the text says so; a call's text still says requests. Mutation
// "no unit argument" dies here.
func TestScenarioSearch_HeldRPSIsInIterationsForAScenario(t *testing.T) {
	for name, held := range map[string]string{"not a number": "fast", "zero": "0", "negative": "-5"} {
		_, err := config.Parse(scenarioSearch("", "", "    pass:\n      held_rps: "+held+"\n"))
		if err == nil {
			t.Errorf("%s: accepted, want a refusal", name)

			continue
		}
		if !strings.Contains(err.Error(), "iterations per second") || strings.Contains(err.Error(), "requests per second") {
			t.Errorf("%s: %q, want iterations per second and no requests", name, err)
		}
	}

	_, err := config.Parse(searchWithHeld("fast"))
	if err == nil || !strings.Contains(err.Error(), "requests per second") || strings.Contains(err.Error(), "iterations") {
		t.Errorf("a call's held_rps: %v, want requests per second", err)
	}

	// A step of the plan is held_rps for a scenario as for a call.
	cfg, err := config.Parse(scenarioSearch("", "", "    pass:\n      held_rps: 244\n"))
	if err != nil {
		t.Fatalf("held_rps 244: %v, want accepted", err)
	}
	if got := cfg.Load.Breakpoint.Pass.HeldRPS; got != 244 {
		t.Errorf("held_rps = %d, want 244", got)
	}
	refuses(t, "between steps", scenarioSearch("", "", "    pass:\n      held_rps: 800\n"), "breakpoint.pass.held_rps", "744", "930")
}

// Ground: contract (spec 3.5) — rps and duration of a scenario's head under a
// search are refused as before; the search sets them. (GUARD, green at once.)
func TestScenarioSearch_RPSAndDurationAreStillRefused(t *testing.T) {
	for name, extra := range map[string]string{"rps": "      rps: 10\n", "duration": "      duration: 1s\n"} {
		_, err := config.Parse(scenarioSearch(extra, "", ""))
		if err == nil || !errors.Is(err, config.ErrBreakpoint) || !strings.Contains(err.Error(), name+" with load.breakpoint") {
			t.Errorf("%s: %v, want ErrBreakpoint naming %q", name, err, name+" with load.breakpoint")
		}
	}
}

// A search is of one call; a scenario beside another call is two calls. (GUARD.)
func TestScenarioSearch_TwoCallsAreStillRefused(t *testing.T) {
	cfg := string(scenarioSearch("", "", ""))
	cfg = strings.Replace(cfg, "  breakpoint:", "    - method: "+transfer+"\n      timeout: 500ms\n  breakpoint:", 1)

	_, err := config.Parse([]byte(cfg))
	if err == nil || !errors.Is(err, config.ErrBreakpoint) || !strings.Contains(err.Error(), "searches one call") {
		t.Errorf("%v, want ErrBreakpoint: searches one call", err)
	}
}
