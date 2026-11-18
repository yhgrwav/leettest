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
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
)

// pass in a scenario (A2, U1): the head's limits are load.calls[i].pass, a
// step's are steps[k].pass; the keys and the grammar are the call's, the latency
// limit is checked against the timeout of the row it judges. Spec:
// reviews/scenario-pass.md round 0, sections 2 and 6. Compile stubs:
// config.Step has RawPass and Pass (yaml:"-"), nothing reads them yet.

// passScenario is a scenario call of GetBalance at rps for duration, with
// callLines before its scenario key and the given steps under steps:.
func passScenario(rps, duration, callLines, steps string) []byte {
	return []byte(scenarioHead + "  calls:\n    - method: " + getBalance + "\n      rps: " + rps + "\n      duration: " + duration +
		"\n" + callLines + "      scenario: s\n      steps:\n" + steps)
}

// transferStep is a step of Transfer with the given lines under it, each
// already indented 10 spaces by the caller.
func transferStep(lines string) string {
	return "        - method: " + transfer + "\n" + lines
}

// Ground: contract — the YAML keys of a step's pass become the limits of that
// step's row, in the field the core reads; the head's own limits stay empty.
func TestScenarioPass_StepKeysBecomeLimits(t *testing.T) {
	cfg, err := config.Parse(passScenario("10", "1s", "", transferStep(
		"          pass:\n            p99: 200ms\n            errors: 1%\n")))
	if err != nil {
		t.Fatalf("Parse() = %v, want a scenario with a pass on its step", err)
	}

	call := cfg.Load.Calls[0]
	got := call.Steps[0].Pass
	if got.P99 != 200*time.Millisecond || got.Errors == nil || got.Errors.Thousandths != 1000 || got.Errors.Text != "1%" {
		t.Errorf("Steps[0].Pass = %+v, want p99 200ms and errors 1%%", got)
	}
	if call.Pass.P99 != 0 || call.Pass.Errors != nil || call.RawPass != nil {
		t.Errorf("Call.Pass = %+v / %+v, want none: a step's pass is not the head's", call.RawPass, call.Pass)
	}
}

// The row of step k is the pass of steps[k]: a pass on the second step lands on
// the second row, and the first has none.
// Ground: contract — a step's limits are read per step, none borrowed from a neighbour.
func TestScenarioPass_SecondStepHasItsOwnLimits(t *testing.T) {
	cfg, err := config.Parse(passScenario("10", "1s", "",
		transferStep("")+"        - method: "+getHistory+"\n          pass:\n            p50: 30ms\n            errors: 2%\n"))
	if err != nil {
		t.Fatalf("Parse() = %v, want accepted", err)
	}

	steps := cfg.Load.Calls[0].Steps
	if steps[0].Pass.P50 != 0 || steps[0].Pass.Errors != nil {
		t.Errorf("Steps[0].Pass = %+v, want none", steps[0].Pass)
	}
	if steps[1].Pass.P50 != 30*time.Millisecond || steps[1].Pass.Errors == nil || steps[1].Pass.Errors.Thousandths != 2000 {
		t.Errorf("Steps[1].Pass = %+v, want p50 30ms and errors 2%%", steps[1].Pass)
	}
}

// pass on the call of a scenario judges the head's row and is not inherited by
// the steps, as timeout, data and dataset of the call are not.
// Ground: contract — the refusal "pass for a scenario isn't supported yet" is gone; the head's limits are the call's.
func TestScenarioPass_OnTheCallIsTheHeadsAndNotInherited(t *testing.T) {
	cfg, err := config.Parse(passScenario("10", "1s", "      pass:\n        p99: 200ms\n        errors: 1%\n", transferStep("")))
	if err != nil {
		t.Fatalf("Parse() = %v, want a scenario call with pass accepted", err)
	}

	call := cfg.Load.Calls[0]
	if call.Pass.P99 != 200*time.Millisecond || call.Pass.Errors == nil || call.Pass.Errors.Text != "1%" {
		t.Errorf("Call.Pass = %+v, want p99 200ms and errors 1%%", call.Pass)
	}
	if got := call.Steps[0].Pass; got.P99 != 0 || got.Errors != nil {
		t.Errorf("Steps[0].Pass = %+v, want none: the call's pass is the head's alone", got)
	}
}

// A step's latency limit is below the step's own timeout, not the call's: a step
// that timed out is known only as at least its timeout.
// Ground: contract — "pass.p99 is not below the step's timeout": the same rule as a call's, the timeout of the judged row.
func TestScenarioPass_LatencyLimitIsBelowTheStepsTimeout(t *testing.T) {
	step := func(timeout string) string {
		return transferStep("          timeout: " + timeout + "\n          pass:\n            p99: 500ms\n            errors: 1%\n")
	}

	refuses(t, "step 300ms, call 2s", passScenario("10", "1s", "", step("300ms")),
		"load.calls[0].steps[0]: ",
		"invalid pass: pass.p99 500ms is not below the step's timeout 300ms: a call that timed out is known only as at least that, so the limit would check nothing")

	refuses(t, "step without a timeout, the default 2s",
		passScenario("10", "1s", "", transferStep("          pass:\n            p99: 3s\n            errors: 1%\n")),
		"load.calls[0].steps[0]: ", "pass.p99 3s is not below the step's timeout 2s")

	cfg, err := config.Parse(passScenario("10", "1s", "      timeout: 300ms\n", step("2s")))
	if err != nil {
		t.Fatalf("step 2s, call 300ms: %v, want accepted: the step's timeout is the bar", err)
	}
	if got := cfg.Load.Calls[0].Steps[0].Pass.P99; got != 500*time.Millisecond {
		t.Errorf("Steps[0].Pass.P99 = %v, want 500ms", got)
	}

	// The head keeps the call's timeout and the call's wording.
	refuses(t, "head 500ms against the call's 300ms",
		passScenario("10", "1s", "      timeout: 300ms\n      pass:\n        p99: 500ms\n        errors: 1%\n", transferStep("          timeout: 2s\n")),
		"pass.p99 500ms is not below the call's timeout 300ms")
}

// Ground: contract — the grammar of a call's pass, kept for a step: p99 needs
// errors, a key outside the list is refused, each with the step's path.
func TestScenarioPass_StepGrammarIsTheCalls(t *testing.T) {
	refuses(t, "p99 without errors",
		passScenario("10", "1s", "", transferStep("          pass:\n            p99: 200ms\n")),
		"load.calls[0].steps[0]: ", "pass.p99 needs pass.errors next to it")

	refuses(t, "an unknown key",
		passScenario("10", "1s", "", transferStep("          pass:\n            p98: 200ms\n            errors: 1%\n")),
		"load.calls[0].steps[0]: ", "pass.p98 is not a limit; the keys are p50, p90, p95, p99, p999 and errors")

	refuses(t, "an empty mapping",
		passScenario("10", "1s", "", transferStep("          pass: {}\n")),
		"load.calls[0].steps[0]: ", "pass has no limits")

	refuses(t, "errors above 100%",
		passScenario("10", "1s", "", transferStep("          pass:\n            errors: 101%\n")),
		"load.calls[0].steps[0]: ", "pass.errors")

	refuses(t, "a latency not in whole microseconds",
		passScenario("10", "1s", "", transferStep("          pass:\n            p99: 200500ns\n            errors: 1%\n")),
		"load.calls[0].steps[0]: ", "pass.p99 200500ns is not whole microseconds")

	// Only the second step is wrong: its index is in the path.
	refuses(t, "the second step",
		passScenario("10", "1s", "", transferStep("")+"        - method: "+getHistory+"\n          pass:\n            p98: 1ms\n            errors: 1%\n"),
		"load.calls[0].steps[1]: ", "pass.p98 is not a limit")
}

// p99.9 needs a thousand calls, and a step gets no more calls than the head: the
// plan is the call's (the iteration is the unit of the schedule).
// Ground: contract — "pass.p999 needs 1000 calls measured"; a step's p999 is checked against the call's rps and duration.
func TestScenarioPass_StepP999NeedsThePlanOfTheCall(t *testing.T) {
	p999 := transferStep("          pass:\n            p999: 100ms\n            errors: 1%\n")

	refuses(t, "10 rps for 50s", passScenario("10", "50s", "", p999),
		"load.calls[0].steps[0]: ",
		"invalid pass: pass.p999 needs 1000 calls measured: at 10 rps that is 1m40s after the warm-up, the scenario runs 50s")

	if _, err := config.Parse(passScenario("10", "100s", "", p999)); err != nil {
		t.Errorf("10 rps for 100s: %v, want accepted: exactly 1000 calls", err)
	}

	// The head's own p999 keeps the call's wording.
	refuses(t, "head, 10 rps for 50s",
		passScenario("10", "50s", "      pass:\n        p999: 100ms\n        errors: 1%\n", transferStep("")),
		"pass.p999 needs 1000 calls measured", "the call runs 50s")
}

// A pass that holds no value is a limit nobody judges: the decoder reads it as
// the key left out. Refused by the step's path, as in a call.
// Ground: contract — a limit written by the user is never dropped without a word; the key's path names the step.
func TestScenarioPass_NullIsRefusedInAStep(t *testing.T) {
	for name, tc := range map[string]struct{ callLines, pass string }{
		"nothing after the colon": {"", "          pass:\n"},
		"a tilde":                 {"", "          pass: ~\n"},
		"the word null":           {"", "          pass: null\n"},
		"a null tag":              {"", "          pass: !!null\n"},
		"an alias of null":        {nullAnchor, "          pass: *n\n"},
	} {
		t.Run(name, func(t *testing.T) {
			refuses(t, name, passScenario("10", "1s", tc.callLines, transferStep(tc.pass)),
				"load.calls[0].steps[0].pass has no value: write the limit or leave it out")
		})
	}

	t.Run("on the second step", func(t *testing.T) {
		refuses(t, "second", passScenario("10", "1s", "", transferStep("")+"        - method: "+getHistory+"\n          pass: ~\n"),
			"load.calls[0].steps[1].pass has no value")
	})
}

// Ground: contract — the same one level down: a limit with no number is refused by its own path.
func TestScenarioPass_KeyWithNoValueIsRefusedInAStep(t *testing.T) {
	for name, tc := range map[string]struct{ callLines, pass, key string }{
		"p99 with nothing":    {"", "          pass:\n            p99:\n            errors: 1%\n", "load.calls[0].steps[0].pass.p99"},
		"p99 a tilde":         {"", "          pass:\n            p99: ~\n            errors: 1%\n", "load.calls[0].steps[0].pass.p99"},
		"p99 null":            {"", "          pass:\n            p99: null\n            errors: 1%\n", "load.calls[0].steps[0].pass.p99"},
		"p99 an alias":        {nullAnchor, "          pass:\n            p99: *n\n            errors: 1%\n", "load.calls[0].steps[0].pass.p99"},
		"errors with nothing": {"", "          pass:\n            p99: 200ms\n            errors:\n", "load.calls[0].steps[0].pass.errors"},
		"errors a tilde":      {"", "          pass:\n            p99: 200ms\n            errors: ~\n", "load.calls[0].steps[0].pass.errors"},
		"errors alone":        {"", "          pass:\n            errors: null\n", "load.calls[0].steps[0].pass.errors"},
	} {
		t.Run(name, func(t *testing.T) {
			refuses(t, name, passScenario("10", "1s", tc.callLines, transferStep(tc.pass)), tc.key+" has no value")
		})
	}
}

// pass left the list of step keys a step does not take yet, so the text of a
// key that is not a step key lists it among the ones a step has.
// Ground: contract — the refusal names every key a step takes, `pass` among them.
func TestScenarioPass_StepKeyTextListsPass(t *testing.T) {
	_, err := config.Parse(passScenario("10", "1s", "", transferStep("          bogus: 1\n")))
	if err == nil {
		t.Fatal("Parse() = nil, want a refusal of an unknown step key")
	}

	want := "load.calls[0].steps[0].bogus is not a step key: a step has method, timeout, data, templates, extract, " +
		"dataset, dataset_order, dataset_seed and pass"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
}
