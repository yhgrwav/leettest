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

package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// Scenario wiring (v0.9 unit 2): config.Call{Scenario, Steps} to
// engine.Call{Scenario, Next}, and the pre-run check of the step methods.
// The config here is built by hand: the red tests of Parse are in pkg/config.

const (
	wsFirst  = "wallet.v1.WalletService/GetBalance"
	wsSecond = "wallet.v1.WalletService/Transfer"
	wsThird  = "wallet.v1.WalletService/GetHistory"
)

func scenarioConfig() *config.MasterConfig {
	return &config.MasterConfig{Load: config.Load{Calls: []config.Call{
		{
			Method: wsFirst, RPS: 20, Duration: 10 * time.Second, Timeout: 3 * time.Second,
			Scenario: "pay-and-check",
			Steps: []config.Step{
				{Method: wsSecond, Timeout: 500 * time.Millisecond},
				{Method: wsThird, Timeout: config.DefaultTimeout},
			},
		},
		{Method: "wallet.v1.WalletService/Ping", RPS: 5, Duration: 10 * time.Second, Timeout: time.Second},
	}}}
}

// Ground: contract — the call is step 1, the steps become Next with the gRPC
// path («/» added once, here), each with its own timeout and the empty message
// of its method; a plain call beside it stays plain.
func TestCallsFromConfig_ScenarioBuildsNext(t *testing.T) {
	calls, err := CallsFromConfig(scenarioConfig())
	if err != nil {
		t.Fatalf("CallsFromConfig() = %v", err)
	}

	first := calls[0]
	if first.Scenario != "pay-and-check" {
		t.Errorf("Scenario = %q, want pay-and-check", first.Scenario)
	}
	if first.Method != "/"+wsFirst || first.Timeout != 3*time.Second {
		t.Errorf("step 1 = %q at %s, want %q at 3s: the call is step 1 with its own timeout", first.Method, first.Timeout, "/"+wsFirst)
	}
	if len(first.Next) != 2 {
		t.Fatalf("len(Next) = %d, want 2", len(first.Next))
	}
	for k, want := range []engine.Step{
		{Method: "/" + wsSecond, Timeout: 500 * time.Millisecond},
		{Method: "/" + wsThird, Timeout: config.DefaultTimeout},
	} {
		got := first.Next[k]
		if got.Method != want.Method || got.Timeout != want.Timeout || got.Payload != nil {
			t.Errorf("Next[%d] = %+v, want method %q, timeout %s, no payload", k, got, want.Method, want.Timeout)
		}
	}

	if plain := calls[1]; plain.Scenario != "" || plain.Next != nil {
		t.Errorf("plain call = scenario %q, next %v, want neither", plain.Scenario, plain.Next)
	}
}

// Ground: contract — load.ramp and load.warmup are about the iteration tick,
// so they stay on step 1's stages and the steps carry none.
func TestCallsFromConfig_ScenarioWithRampKeepsTheRampOnTheTick(t *testing.T) {
	cfg := scenarioConfig()
	cfg.Load.Ramp = 4 * time.Second

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("CallsFromConfig() = %v", err)
	}

	first := calls[0]
	if len(first.Stages) != 2 || first.Stages[0].StartRPS != 0 || first.Stages[0].TargetRPS != 20 || first.Stages[0].Duration != 4*time.Second {
		t.Errorf("stages = %+v, want the ramp 0 to 20 over 4s, then the rate", first.Stages)
	}
	if len(first.Next) != 2 {
		t.Errorf("len(Next) = %d, want 2: the ramp does not drop the steps", len(first.Next))
	}
}

// Steps are checked before the run: a typo in a step method is a config error
// naming the step, not a run whose step 2 is UNIMPLEMENTED from first to last.
// Ground: contract — a method the target does not know is an error before the load.
func TestAttachMethods_AStepMethodTheTargetDoesNotHaveIsRefused(t *testing.T) {
	cfg := scenarioConfig()
	cfg.Load.Calls[0].Steps[1].Method = "wallet.v1.WalletService/GetHistroy"
	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{known: map[string]protoreflect.MessageDescriptor{
		wsFirst: orderMessage(t), wsSecond: orderMessage(t), wsThird: orderMessage(t),
		"wallet.v1.WalletService/Ping": orderMessage(t),
	}}

	_, _, err = AttachMethods(t.Context(), resolver, cfg, calls)

	if !errors.Is(err, descriptor.ErrMethodNotFound) {
		t.Fatalf("error = %v, want %v", err, descriptor.ErrMethodNotFound)
	}
	for _, want := range []string{"load.calls[0].steps[1]", "wallet.v1.WalletService/GetHistroy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

// The methods AttachMethods hands out stay one per call; the steps are only checked.
// Ground: contract — the return shape callers index by call.
func TestAttachMethods_StepsAreResolvedAndMethodsStayOnePerCall(t *testing.T) {
	cfg := scenarioConfig()
	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{known: map[string]protoreflect.MessageDescriptor{
		wsFirst: orderMessage(t), wsSecond: orderMessage(t), wsThird: orderMessage(t),
		"wallet.v1.WalletService/Ping": orderMessage(t),
	}}

	_, methods, err := AttachMethods(t.Context(), resolver, cfg, calls)

	if err != nil {
		t.Fatalf("AttachMethods() = %v, want the steps known", err)
	}
	if len(methods) != 2 {
		t.Errorf("len(methods) = %d, want 2: one per call, not per step", len(methods))
	}
	for _, step := range []string{wsSecond, wsThird} {
		if !slices.Contains(resolver.asked, step) {
			t.Errorf("resolver asked %v, want step method %s among them", resolver.asked, step)
		}
	}
}

// Without reflection a call that has no data only costs the check; the same
// goes for a step, which has no data in this unit.
// Ground: contract — unchecked, as for a call without data.
func TestAttachMethods_StepsWithoutReflectionAreUncheckedNotAnError(t *testing.T) {
	cfg := scenarioConfig()
	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{err: descriptor.ErrReflectionUnsupported}

	unchecked, _, err := AttachMethods(t.Context(), resolver, cfg, calls)

	if err != nil {
		t.Fatalf("AttachMethods() = %v, want no error: nothing here needs a schema", err)
	}
	var named []string
	for _, u := range unchecked {
		named = append(named, u.Method)
	}
	for _, step := range []string{wsSecond, wsThird} {
		if !slices.Contains(named, step) {
			t.Errorf("unchecked = %v, want step method %s warned about", named, step)
		}
	}
}

// GUARD (green at once): the dataset fit counts the engine's scheduled
// iterations of step 1, so a scenario call is checked exactly as a plain one.
// Ground: contract — spec 10, CheckDatasetsFit handles a call with Next.
func TestCheckDatasetsFit_ACallWithStepsIsCheckedLikeAPlainOne(t *testing.T) {
	plain := engine.Call{
		Method:   "/" + wsFirst,
		Payloads: [][]byte{{}, {}},
		Dataset:  &engine.DatasetRef{File: "ids.jsonl", Once: true},
		Stages:   []engine.Stage{{StartRPS: 10, TargetRPS: 10, Duration: time.Second}},
	}
	scenario := plain
	scenario.Scenario = "s"
	scenario.Next = []engine.Step{{Method: "/" + wsSecond, Timeout: time.Second}}

	plainErr := CheckDatasetsFit([]engine.Call{plain})
	scenarioErr := CheckDatasetsFit([]engine.Call{scenario})

	if plainErr == nil {
		t.Fatal("a plain call of 10 iterations over 2 records passed: the guard is not set up")
	}
	if scenarioErr == nil || scenarioErr.Error() != plainErr.Error() {
		t.Errorf("scenario error = %v, want the plain call's: %v", scenarioErr, plainErr)
	}
}
