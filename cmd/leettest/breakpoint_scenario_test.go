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
	"reflect"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
)

func searchSetupOf(call engine.Call) searchSetup {
	return searchSetup{
		cfg: &config.MasterConfig{Load: config.Load{Breakpoint: &config.Breakpoint{
			From: 10, To: 100, Factor: 2, Settle: time.Second, Hold: 5 * time.Second,
		}}},
		calls: []engine.Call{call},
	}
}

func scenarioCallOf() engine.Call {
	return engine.Call{
		Method: "/pay.v1.Wallet/Transfer", Timeout: 2 * time.Second, Scenario: "pay",
		Next: []engine.Step{
			{Method: "/pay.v1.Wallet/GetBalance", Timeout: 3 * time.Second},
			{Method: "/pay.v1.Wallet/GetHistory", Timeout: 5 * time.Second},
		},
	}
}

// Ground: contract (spec 1.6, 6.1) — the plan of a scenario's search carries
// the deadlines of the steps after the head, in order, so that the search
// sizes the in-flight need, the cooldown and the worst case by the sum; the
// row it names is the head's key. A plain call's plan carries none and names
// its method as before, the leading slash cut. Mutation "no StepTimeouts"
// dies here.
func TestSearchPlan_ACarriesTheStepsDeadlinesAndNamesTheHead(t *testing.T) {
	plan, method := searchPlan(searchSetupOf(scenarioCallOf()), 0)

	if want := []time.Duration{3 * time.Second, 5 * time.Second}; !reflect.DeepEqual(plan.StepTimeouts, want) {
		t.Errorf("StepTimeouts = %v, want %v", plan.StepTimeouts, want)
	}
	if plan.Timeout != 2*time.Second {
		t.Errorf("Timeout = %v, want the head's 2s", plan.Timeout)
	}
	if method != "pay.v1.Wallet/Transfer" {
		t.Errorf("method = %q, want the head's key", method)
	}

	plain, name := searchPlan(searchSetupOf(engine.Call{Method: "/pkg.Svc/Do", Timeout: time.Second}), 0)
	if len(plain.StepTimeouts) != 0 || name != "pkg.Svc/Do" {
		t.Errorf("a plain call: StepTimeouts %v, name %q", plain.StepTimeouts, name)
	}
}

// The head's method may repeat among the steps; then its row is named as the
// report's rows are (engine.RowKeys), or the search would name a row the
// report has no such key for.
func TestSearchPlan_TheHeadIsNamedByItsRowKey(t *testing.T) {
	call := scenarioCallOf()
	call.Next = append(call.Next, engine.Step{Method: call.Method, Timeout: time.Second})

	_, method := searchPlan(searchSetupOf(call), 0)

	if want := "pay.v1.Wallet/Transfer (pay, step 1)"; method != want {
		t.Errorf("method = %q, want %q", method, want)
	}
}

// Ground: contract (spec 4.6) — the search's report carries the scenario's
// name, empty for a call.
func TestNewBreakpointRun_ANamesTheScenario(t *testing.T) {
	run := newBreakpointRun(searchSetupOf(scenarioCallOf()), "pay.v1.Wallet/Transfer", time.Now(),
		breakpoint.Plan{}, breakpoint.Result{}, nil, "", nil)
	if run.Scenario != "pay" {
		t.Errorf("Scenario = %q, want pay", run.Scenario)
	}

	plain := newBreakpointRun(searchSetupOf(engine.Call{Method: "/pkg.Svc/Do"}), "pkg.Svc/Do", time.Now(),
		breakpoint.Plan{}, breakpoint.Result{}, nil, "", nil)
	if plain.Scenario != "" {
		t.Errorf("a plain call's Scenario = %q, want none", plain.Scenario)
	}
}
