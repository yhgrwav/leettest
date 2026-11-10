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
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// Ground: contract — methods[].rps is the method's sent calls over its own
// sending time: a plan that was sent in full next to a plan twice as long
// reports the rate it was planned at, not half of it.
func TestJSON_MethodRPSOfAShorterPlanIsItsPlannedRate(t *testing.T) {
	eng, err := engine.New(engine.Options{
		Calls: []engine.Call{
			{Method: "/pkg.Svc/Short", Timeout: time.Second,
				Stages: []engine.Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Second}}},
			{Method: "/pkg.Svc/Long", Timeout: time.Second,
				Stages: []engine.Stage{{StartRPS: 50, TargetRPS: 50, Duration: 2 * time.Second}}},
		},
		Sender: engine.FakeSender{}, MaxInFlight: 1000,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	methods := writeJSON(t, jsonRun(eng.Report()))["methods"].([]any)
	byName := map[string]map[string]any{}
	for _, m := range methods {
		method := m.(map[string]any)
		byName[method["method"].(string)] = method
	}

	for name, low := range map[string]float64{"/pkg.Svc/Short": 100, "/pkg.Svc/Long": 50} {
		method := byName[name]
		if method == nil {
			t.Fatalf("no method %s in %v", name, methods)
		}
		if method["planned_rps_low"] != low {
			t.Fatalf("%s: planned_rps_low = %v, want %v", name, method["planned_rps_low"], low)
		}
		got, _ := method["rps"].(float64)
		// The short plan's window is its planned second: exact. The long
		// plan's window ends at its last scheduled call, a tick short of the
		// plan, so it is near its planned rate, not on it.
		if tolerance := map[string]float64{"/pkg.Svc/Short": 0, "/pkg.Svc/Long": 0.03 * low}[name]; got < low-tolerance || got > low+tolerance {
			t.Errorf("%s: rps = %v, want %v = planned_rps_low (±%v): the plan was sent in full", name, method["rps"], low, tolerance)
		}
	}
}
