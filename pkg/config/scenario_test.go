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
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
)

// Scenario config (v0.9 unit 2). The call is step 1 of its scenario, `steps`
// are the calls after it: steps[0] is step 2. Compile stubs: config.Call has
// Scenario and Steps, config.Step has Method and Timeout, none read from the
// file yet, so every test of a scenario that parses is red until the code.

const (
	scenarioHead = `app:
  target:
    ip: localhost
    port: 50051
load:
`
	getBalance = "wallet.v1.WalletService/GetBalance"
	transfer   = "wallet.v1.WalletService/Transfer"
	getHistory = "wallet.v1.WalletService/GetHistory"
)

// scenarioCall is a load section of one call, GetBalance at 10 rps for 1s,
// with callLines more fields of it (each indented 6 spaces by the caller).
func scenarioCall(callLines string) string {
	return scenarioHead + `  calls:
    - method: ` + getBalance + `
      rps: 10
      duration: 1s
` + callLines
}

// twoStepScenario is a scenario call with the steps given as YAML under steps:.
func twoStepScenario(steps string) string {
	return scenarioCall("      scenario: pay-and-check\n      steps:\n" + steps)
}

const twoSteps = "        - method: " + transfer + "\n          timeout: 500ms\n" +
	"        - method: " + getHistory + "\n"

// Ground: contract — the YAML shape of a scenario and what Parse hands the
// core: the name as written, the steps after the call in order.
func TestScenario_ParsesCallAndSteps(t *testing.T) {
	cfg, err := config.Parse([]byte(twoStepScenario(twoSteps)))
	if err != nil {
		t.Fatalf("Parse() = %v, want a valid scenario config", err)
	}

	call := cfg.Load.Calls[0]
	if call.Scenario != "pay-and-check" {
		t.Errorf("Scenario = %q, want pay-and-check", call.Scenario)
	}
	if len(call.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2 (the call itself is step 1)", len(call.Steps))
	}
	if call.Steps[0].Method != transfer || call.Steps[1].Method != getHistory {
		t.Errorf("step methods = %q, %q, want %q, %q", call.Steps[0].Method, call.Steps[1].Method, transfer, getHistory)
	}
	if call.Steps[0].Timeout != 500*time.Millisecond {
		t.Errorf("Steps[0].Timeout = %s, want 500ms as written", call.Steps[0].Timeout)
	}
}

// A step that leaves timeout out gets the default, not the timeout of the
// call: the call's 5s must not leak into step 2, and the step's own 100ms must
// not leak into the call. Mutation 6 (Step.Timeout stays 0) dies here.
// Ground: boundary — the budget is rps × Σ timeouts, so each must be a number.
func TestScenario_StepWithoutTimeoutGetsTheDefaultNotTheCalls(t *testing.T) {
	cfg, err := config.Parse([]byte(scenarioCall(
		"      timeout: 5s\n      scenario: s\n      steps:\n        - method: " + transfer + "\n")))
	if err != nil {
		t.Fatalf("Parse() = %v, want a valid scenario config", err)
	}

	call := cfg.Load.Calls[0]
	if call.Timeout != 5*time.Second {
		t.Errorf("call Timeout = %s, want 5s as written", call.Timeout)
	}
	if len(call.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(call.Steps))
	}
	if got := call.Steps[0].Timeout; got != config.DefaultTimeout {
		t.Errorf("Steps[0].Timeout = %s, want the default %s, not the call's 5s", got, config.DefaultTimeout)
	}
}

// Ground: contract — the name is printed as written, no rule beyond not blank.
func TestScenario_NameIsKeptAsWritten(t *testing.T) {
	cfg, err := config.Parse([]byte(scenarioCall(
		"      scenario: pay and check, v2\n      steps:\n        - method: " + transfer + "\n")))
	if err != nil {
		t.Fatalf("Parse() = %v, want a valid scenario config", err)
	}
	if got := cfg.Load.Calls[0].Scenario; got != "pay and check, v2" {
		t.Errorf("Scenario = %q, want it as written", got)
	}
}

// The call's own data keeps working for step 1, and a plain call beside a
// scenario stays plain.
// Ground: contract — a scenario call is still a call.
func TestScenario_CallKeepsItsOwnDataAndPlainCallsStayPlain(t *testing.T) {
	raw := scenarioCall("      data:\n        account: a-1\n      scenario: s\n      steps:\n        - method: "+transfer+"\n") +
		"    - method: " + getHistory + "\n      rps: 5\n      duration: 1s\n"

	cfg, err := config.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse() = %v, want a valid config", err)
	}
	if cfg.Load.Calls[0].Data == nil {
		t.Error("the scenario call lost its data")
	}
	if c := cfg.Load.Calls[1]; c.Scenario != "" || c.Steps != nil {
		t.Errorf("the plain call = scenario %q, steps %v, want neither", c.Scenario, c.Steps)
	}
}

// Warm-up and ramp are properties of the run, the scenario call takes them
// like any call.
// Ground: contract — spec 10.
func TestScenario_WithWarmupAndRamp(t *testing.T) {
	raw := strings.Replace(twoStepScenario(twoSteps), "load:\n", "load:\n  warmup: 200ms\n  ramp: 300ms\n", 1)

	if _, err := config.Parse([]byte(raw)); err != nil {
		t.Fatalf("Parse() = %v, want a valid config with load.warmup and load.ramp", err)
	}
}

// GUARD (green at once): a config without a scenario has none after the
// change, so the new fields cost the plain run nothing.
// Ground: contract — regression guard.
func TestScenario_PlainCallHasNoScenario(t *testing.T) {
	cfg, err := config.Parse([]byte(scenarioCall("")))
	if err != nil {
		t.Fatalf("Parse() = %v, want a valid config", err)
	}
	if c := cfg.Load.Calls[0]; c.Scenario != "" || c.Steps != nil {
		t.Errorf("plain call = scenario %q, steps %v, want neither", c.Scenario, c.Steps)
	}
}

// Every refusal names the key by its path, so the user finds it; the messages
// below are the contract the code is written to.
// Ground: contract — strict config (П13): a wrong scenario never runs.
func TestScenario_Refusals(t *testing.T) {
	step := func(lines string) string { return twoStepScenario("        - method: " + transfer + "\n" + lines) }
	oneStep := "        - method: " + transfer + "\n"

	for _, tc := range []struct {
		name string
		cfg  string
		is   error    // errors.Is target, nil when the text alone is the contract
		want []string // substrings of the message
	}{
		// Both keys or none.
		{"steps without scenario", scenarioCall("      steps:\n" + oneStep), nil,
			[]string{"load.calls[0].steps", "needs load.calls[0].scenario"}},
		{"scenario without steps", scenarioCall("      scenario: s\n"), nil,
			[]string{"load.calls[0].scenario", "needs load.calls[0].steps"}},
		{"empty steps list", scenarioCall("      scenario: s\n      steps: []\n"), nil,
			[]string{"load.calls[0].steps", "must not be empty"}},
		{"scenario in the second call", scenarioCall("") + "    - method: " + getHistory + "\n      rps: 5\n      duration: 1s\n      steps:\n" + oneStep, nil,
			[]string{"load.calls[1].steps", "needs load.calls[1].scenario"}},

		// Blank and null.
		{"empty scenario name", scenarioCall("      scenario: \"\"\n      steps:\n" + oneStep), nil,
			[]string{"load.calls[0].scenario", "must not be empty"}},
		{"blank scenario name", scenarioCall("      scenario: \"   \"\n      steps:\n" + oneStep), nil,
			[]string{"load.calls[0].scenario", "must not be empty"}},
		{"scenario with no value", scenarioCall("      scenario:\n      steps:\n" + oneStep), nil,
			[]string{"line 10:", "load.calls[0].scenario", "has no value"}},
		{"scenario null", scenarioCall("      scenario: null\n      steps:\n" + oneStep), nil,
			[]string{"load.calls[0].scenario", "has no value"}},
		{"steps with no value", scenarioCall("      scenario: s\n      steps:\n"), nil,
			[]string{"line 11:", "load.calls[0].steps", "has no value"}},
		{"steps tilde", scenarioCall("      scenario: s\n      steps: ~\n"), nil,
			[]string{"load.calls[0].steps", "has no value"}},
		{"step method with no value", twoStepScenario("        - method:\n"), nil,
			[]string{"line 12:", "load.calls[0].steps[0].method", "has no value"}},
		{"step timeout with no value", twoStepScenario("        - method: " + transfer + "\n          timeout:\n"), nil,
			[]string{"line 13:", "load.calls[0].steps[0].timeout", "has no value"}},
		{"second step timeout null", step("        - method: " + getHistory + "\n          timeout: null\n"), nil,
			[]string{"load.calls[0].steps[1].timeout", "has no value"}},
		{"step without method", twoStepScenario("        - timeout: 1s\n"), nil,
			[]string{"load.calls[0].steps[0].method", "is required"}},

		// A step's own values.
		{"step method without a service", twoStepScenario("        - method: Transfer\n"), config.ErrInvalidMethod,
			[]string{"load.calls[0].steps[0]"}},
		{"step timeout zero", twoStepScenario("        - method: " + transfer + "\n          timeout: 0s\n"), config.ErrInvalidTimeout,
			[]string{"load.calls[0].steps[0].timeout"}},
		{"step timeout negative", twoStepScenario("        - method: " + transfer + "\n          timeout: -1s\n"), config.ErrInvalidTimeout,
			[]string{"load.calls[0].steps[0].timeout"}},
		{"step timeout an hour", twoStepScenario("        - method: " + transfer + "\n          timeout: 1h\n"), config.ErrTimeoutTooLong,
			[]string{"load.calls[0].steps[0].timeout"}},
		{"second step timeout too long", step("        - method: " + getHistory + "\n          timeout: 90m\n"), config.ErrTimeoutTooLong,
			[]string{"load.calls[0].steps[1].timeout"}},
		{"unknown step key", step("        - method: " + getHistory + "\n          bogus: 1\n"), nil,
			[]string{"load.calls[0].steps[1].bogus"}},

		// Namespace: the scenario name, the call method and the step methods.
		{"scenario named as its call method", scenarioCall("      scenario: " + getBalance + "\n      steps:\n" + oneStep), config.ErrDuplicateMethod, nil},
		{"scenario named as a step method", scenarioCall("      scenario: " + transfer + "\n      steps:\n" + oneStep), config.ErrDuplicateMethod, nil},
		{"scenario named as another call method", scenarioCall("      scenario: "+getHistory+"\n      steps:\n"+oneStep) +
			"    - method: " + getHistory + "\n      rps: 5\n      duration: 1s\n", config.ErrDuplicateMethod, nil},
		{"two scenarios with one name", scenarioCall("      scenario: s\n      steps:\n"+oneStep) +
			"    - method: " + getHistory + "\n      rps: 5\n      duration: 1s\n      scenario: s\n      steps:\n        - method: wallet.v1.WalletService/Close\n",
			config.ErrDuplicateMethod, nil},
		{"step method equal to the call method", scenarioCall("      scenario: s\n      steps:\n        - method: " + getBalance + "\n"), config.ErrDuplicateMethod, nil},
		{"one method twice in a scenario", step("        - method: " + transfer + "\n"), config.ErrDuplicateMethod, nil},
		{"step method equal to another call method", scenarioCall("      scenario: s\n      steps:\n"+oneStep) +
			"    - method: " + transfer + "\n      rps: 5\n      duration: 1s\n", config.ErrDuplicateMethod, nil},

		// Search and pass: refused at load, before any run.
		{"scenario with a breakpoint search",
			scenarioHead + "  calls:\n    - method: " + getBalance + "\n      scenario: s\n      steps:\n" + oneStep +
				"  breakpoint:\n    from: 100\n    to: 2000\n    settle: 5s\n    hold: 30s\n    p99_limit: 200ms\n", nil,
			[]string{"a scenario can't be searched yet"}},
		{"pass on a scenario call", scenarioCall("      scenario: s\n      steps:\n" + oneStep + "      pass:\n        errors: 1%\n"), nil,
			[]string{"pass for a scenario isn't supported yet"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse([]byte(tc.cfg))

			if err == nil {
				t.Fatal("Parse() = nil, want a refusal")
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("error = %v, want it to be %v", err, tc.is)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
			if tc.is == nil && len(tc.want) == 0 {
				t.Fatal("a case needs a sentinel or text to check")
			}
		})
	}
}

// A step key this unit does not read is refused by name, in words that say it
// is coming and not a typo: the generic unknown-key error would read as a
// mistake of the user.
// Ground: contract — П13 and П29: no silent partial scenario.
func TestScenario_StepKeysNotSupportedYet(t *testing.T) {
	for key, value := range map[string]string{
		"data":          "{account: a-1}",
		"dataset":       "x.jsonl",
		"dataset_order": "random",
		"dataset_seed":  "7",
		"dataset_reuse": "false",
		"templates":     "true",
		"rps":           "5",
		"duration":      "1s",
		"pass":          "{errors: 1%}",
	} {
		t.Run(key, func(t *testing.T) {
			raw := twoStepScenario("        - method: " + transfer + "\n          " + key + ": " + value + "\n")

			_, err := config.Parse([]byte(raw))

			if err == nil {
				t.Fatalf("Parse() = nil, want a refusal of step key %s", key)
			}
			for _, want := range []string{"load.calls[0].steps[0]." + key, "not supported in a step yet"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}
