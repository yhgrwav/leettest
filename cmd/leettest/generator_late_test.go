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
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

// v0.7 unit 1 (reviews/generator-limit.md): a plain run whose p99 tail the
// generator's lag decided ends invalid, exit code 2, however the limits read.

func quantile(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Exact: true, Defined: true}
}

// lateGenerator is a run whose wait verdict names the generator: p99 50ms,
// 5ms without the waits, the generator the cause that leads the tail.
func lateGenerator() cli.RunReport {
	r := engine.Report{
		Duration: 10 * time.Second, Planned: 10 * time.Second, Sent: 1000,
		Methods: []engine.MethodReport{{
			Method: "/shop.v1.Catalog/Get", Sent: 1000, Latencies: 1000,
			P99: quantile(50 * time.Millisecond), P99WithoutClientWaits: quantile(5 * time.Millisecond),
		}},
		GeneratorCauseCalls: 40, GeneratorTailCalls: 12,
		StartLagP99: quantile(12 * time.Millisecond), StartLagMax: 30 * time.Millisecond,
	}

	return cli.RunReport{Report: r}
}

func TestRunState_ALateGeneratorIsInvalidAndExit2(t *testing.T) {
	run := lateGenerator()

	if got := runState(run); got != threshold.Invalid {
		t.Errorf("runState = %v, want Invalid (%v)", got, threshold.Invalid)
	}

	err := runResult(run, nil)
	if !errors.Is(err, ErrInvalidRun) {
		t.Fatalf("runResult = %v, want ErrInvalidRun", err)
	}
	if code := exitCode(err); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
	if outcome, ok := outcomeOf(err); !ok || outcome != cli.OutcomeInvalid {
		t.Errorf("outcome %q (%v), want %q", outcome, ok, cli.OutcomeInvalid)
	}
}

// An invalid run before a short one, as for the cap and the clock: a run
// stopped by Ctrl-C whose generator was behind is still not a number about
// the target.
func TestRunState_ALateGeneratorBeatsAnEarlyStop(t *testing.T) {
	run := lateGenerator()
	run.Incomplete = true

	if got := runState(run); got != threshold.Invalid {
		t.Errorf("runState = %v, want Invalid (%v)", got, threshold.Invalid)
	}
}

// The limits that would pass do not rescue the run: the exit code is 2, not 0.
func TestRunResult_ALateGeneratorIsNotRescuedByPassingLimits(t *testing.T) {
	run := lateGenerator()
	run.Thresholds = []threshold.Result{{
		Method: "shop.v1.Catalog/Get", Key: threshold.P99, Verdict: threshold.VerdictPass,
	}}

	if code := exitCode(runResult(run, nil)); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
}

// The stream and the connection are the run's limits on the target's side of
// the wire: complete, as before. So are the run with no verdict at all and
// the lag that moved nothing.
func TestRunState_OtherCausesAndNoVerdictStayComplete(t *testing.T) {
	stream := lateGenerator()
	stream.GeneratorCauseCalls, stream.GeneratorTailCalls = 0, 0
	stream.StreamCauseCalls, stream.StreamTailCalls = 40, 40

	conn := lateGenerator()
	conn.GeneratorCauseCalls, conn.GeneratorTailCalls = 0, 0
	conn.ConnectionCauseCalls, conn.ConnectionTailCalls = 40, 40

	// The lag is in the tail but p99 drops by a hair under a tenth.
	hair := lateGenerator()
	hair.Methods[0].P99 = quantile(100 * time.Millisecond)
	hair.Methods[0].P99WithoutClientWaits = quantile(90*time.Millisecond + time.Microsecond)

	clean := lateGenerator()
	clean.GeneratorCauseCalls, clean.GeneratorTailCalls = 0, 0
	clean.StartLagP99, clean.StartLagMax = metrics.Quantile{}, 0
	clean.Methods[0].P99WithoutClientWaits = clean.Methods[0].P99

	for name, run := range map[string]cli.RunReport{"stream": stream, "connection": conn, "p99 moved by a hair under a tenth": hair, "clean": clean} {
		if got := runState(run); got != threshold.Complete {
			t.Errorf("%s: runState = %v, want Complete (%v)", name, got, threshold.Complete)
		}
		if err := runResult(run, nil); err != nil {
			t.Errorf("%s: runResult = %v, want nil", name, err)
		}
	}
}
