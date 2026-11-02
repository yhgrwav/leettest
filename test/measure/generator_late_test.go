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

package measure

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
	"github.com/yhgrwav/leettest/test/stand"
)

// lateStarter runs n calls through the real pool, the real gRPC sender and the
// stand, each handed over `late` after the moment it was scheduled for: the
// generator's lateness is set exactly, by the scheduled moments, not left to
// how busy the machine is. The stand answers at once, so a call's latency is
// its lateness plus a few milliseconds, and without the lateness a few
// milliseconds.
//
// The engine builds its schedule itself and a test cannot hand it a late one,
// so the pool and the stats, the parts the engine is made of, are driven
// directly. The report is Stats.Report, the one the engine returns.
func lateStarter(t *testing.T, s *stand.Stand, n int, late time.Duration) engine.Report {
	t.Helper()

	sender := grpcsender.New(grpcsender.Options{
		Target:      s.Target(),
		DialOptions: []grpc.DialOption{s.DialOption()},
	})
	if err := sender.Connect(t.Context()); err != nil {
		t.Fatalf("connect to the stand: %v", err)
	}
	t.Cleanup(func() { _ = sender.Close() })

	const timeout = 2 * time.Second

	stats := engine.NewStats()
	stats.Reserve(time.Duration(n)*time.Millisecond+late+timeout+5*time.Second, s.Method())

	// Moment zero is `late` before the first call is handed over: the i-th call
	// was due i ms after it and is started at least late-i ms behind.
	zero := time.Now().Add(-late)
	stats.Start(zero, 0)
	stats.EndSending(zero.Add(time.Duration(n) * time.Millisecond))

	in := make(chan engine.Request, n)
	for i := range n {
		due := zero.Add(time.Duration(i) * time.Millisecond)
		in <- engine.Request{Method: s.Method(), ScheduledAt: due, Deadline: due.Add(late + timeout)}
	}
	close(in)

	out := make(chan engine.Result, n)
	done := make(chan struct{})
	go func() {
		defer close(done)

		for r := range out {
			stats.Record(r)
		}
	}()

	pool := engine.NewWorkerPool(sender, n)
	if err := pool.Run(t.Context(), in, out); err != nil {
		t.Fatalf("pool: %v", err)
	}
	close(out)
	<-done

	stats.Finish(time.Now())

	return stats.Report()
}

// The whole way from a generator that started its calls late to the exit
// code's reason: the wait verdict of the report names the generator, the
// report is invalid for it, and the JSON says generator_late (the exit code 2
// is runState's, tested in cmd/leettest on the same report).
func TestReport_ALateStarterMakesTheRunInvalid(t *testing.T) {
	target := stand.Start(stand.Constant(time.Millisecond))
	t.Cleanup(target.Stop)

	const late = 150 * time.Millisecond

	report := lateStarter(t, target, 200, late)

	// The premise: what the stand did and the wait verdict that is not new.
	if report.StartLagP99.Value < late/2 {
		t.Fatalf("start lag p99 %v, want over %v: the starter was not late", report.StartLagP99.Value, late/2)
	}
	if cause, side, ok := report.WaitVerdict(); !ok || cause != engine.WaitGenerator || side != engine.SideRun {
		t.Fatalf("wait verdict %v %v %v, want the generator on the run's side", cause, side, ok)
	}

	run := cli.RunReport{Report: report}
	if !cli.GeneratorLate(run) {
		t.Error("GeneratorLate = false for a run whose generator started every call late")
	}

	var buf bytes.Buffer
	err := cli.WriteJSON(&buf, cli.JSONRun{
		Target: "localhost:50051", Version: "v0.0.0-test", Outcome: cli.OutcomeInvalid, StartedAt: time.Now(), Run: run,
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	var out struct {
		Outcome        string   `json:"outcome"`
		InvalidReasons []string `json:"invalid_reasons"`
		TailWaitCause  *string  `json:"tail_wait_cause"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}
	if !slices.Contains(out.InvalidReasons, "generator_late") {
		t.Errorf("invalid_reasons = %v, want generator_late", out.InvalidReasons)
	}
	if out.TailWaitCause == nil || *out.TailWaitCause != "generator" {
		t.Errorf("tail_wait_cause = %v, want generator", out.TailWaitCause)
	}
}

// The same stand and pool, every call started on time: nothing to name.
func TestReport_AnOnTimeStarterIsNotLate(t *testing.T) {
	target := stand.Start(stand.Constant(time.Millisecond))
	t.Cleanup(target.Stop)

	report := lateStarter(t, target, 200, 0)

	if cli.GeneratorLate(cli.RunReport{Report: report}) {
		cause, side, ok := report.WaitVerdict()
		t.Errorf("GeneratorLate = true for calls started on time (start lag p99 %v, wait verdict %v %v %v)",
			report.StartLagP99.Value, cause, side, ok)
	}
}
