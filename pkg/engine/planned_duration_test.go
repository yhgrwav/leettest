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

package engine

import (
	"testing"
	"time"
)

// Ground: contract — each method reports its own planned length (its stages
// summed), not the run's longest: the report echoes the config per method.
func TestReport_PlannedDurationIsTheMethodsOwn(t *testing.T) {
	eng, err := New(Options{
		Calls: []Call{
			{Method: "short", Timeout: 100 * time.Millisecond, Stages: []Stage{
				{StartRPS: 20, TargetRPS: 20, Duration: 100 * time.Millisecond},
				{StartRPS: 40, TargetRPS: 40, Duration: 200 * time.Millisecond},
			}},
			{Method: "long", Timeout: 100 * time.Millisecond, Stages: []Stage{
				{StartRPS: 20, TargetRPS: 20, Duration: 500 * time.Millisecond},
			}},
		},
		Sender:      FakeSender{Delay: time.Millisecond},
		MaxInFlight: 100,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := map[string]time.Duration{}
	for _, m := range eng.Report().Methods {
		got[m.Method] = m.PlannedDuration
	}
	if got["short"] != 300*time.Millisecond || got["long"] != 500*time.Millisecond {
		t.Errorf("planned durations %v, want short 300ms (100ms + 200ms) and long 500ms", got)
	}
}
