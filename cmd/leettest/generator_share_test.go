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
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

// B 80k, run 3, of reviews/generator-limit-stand.md: the connection leads the
// wait verdict (37452 calls to the generator's 37380) and p99 moves 7.9%
// without the waits, but 37380 of the 1.44M calls never left the generator.
// Calls the generator left unsent are 1% or more of a method's calls: the run
// is invalid generator_late, exit code 2, whoever leads.
func collapsedConnectionLed() cli.RunReport {
	r := engine.Report{
		Duration: 20 * time.Second, Planned: 20 * time.Second,
		Sent: 1365168, NotSent: 74832, NotSentGenerator: 37380, NotSentConnection: 37452,
		Methods: []engine.MethodReport{{
			Method: "/shop.v1.Catalog/Get", Sent: 1365168, Latencies: 1365168,
			NotSent: 74832, NotSentGenerator: 37380, NotSentConnection: 37452,
			P99: quantile(2 * time.Second), P99WithoutClientWaits: quantile(1842 * time.Millisecond),
		}},
		ConnectionCauseCalls: 37452, ConnectionTailCalls: 37452,
		GeneratorCauseCalls: 37380, GeneratorTailCalls: 12,
		StartLagP99: quantile(2680 * time.Millisecond), StartLagMax: 2910 * time.Millisecond,
	}

	return cli.RunReport{Report: r}
}

func TestRunState_AShareOfUnsentCallsIsInvalidWhileTheConnectionLeads(t *testing.T) {
	run := collapsedConnectionLed()

	if cause, _, _ := run.WaitVerdict(); cause != engine.WaitConnection {
		t.Fatalf("the fixture's wait verdict is %q, want the connection", cause)
	}
	if got := runState(run); got != threshold.Invalid {
		t.Errorf("runState = %v, want Invalid (%v)", got, threshold.Invalid)
	}

	err := runResult(run, nil)
	if code := exitCode(err); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
	if outcome, ok := outcomeOf(err); !ok || outcome != cli.OutcomeInvalid {
		t.Errorf("outcome %q (%v), want %q", outcome, ok, cli.OutcomeInvalid)
	}
}

// One call under the share the run is the connection's as before: complete.
func TestRunState_UnderTheShareTheConnectionStillLeadsAndTheRunIsComplete(t *testing.T) {
	run := collapsedConnectionLed()
	m := &run.Methods[0]
	m.NotSentGenerator, m.NotSentConnection = 14399, 60433
	run.NotSentGenerator, run.NotSentConnection = 14399, 60433

	if got := runState(run); got != threshold.Complete {
		t.Errorf("runState = %v, want Complete (%v)", got, threshold.Complete)
	}
}
