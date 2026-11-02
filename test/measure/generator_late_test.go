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

const (
	// lateEvery is the spacing of the scheduled moments.
	lateEvery = 5 * time.Millisecond
	lateCalls = 200
)

// lateStarter runs n calls through the real pool, the real gRPC sender and the
// stand, each handed to the pool `late` after the moment it was scheduled for:
// a feeding goroutine sleeps until due_i+late, due_i = start + i*lateEvery, so
// every call starts exactly `late` behind its schedule and none ahead of it,
// not left to how busy the machine is. The stand answers at once, so a call's
// latency is its lateness plus a few milliseconds, and without the lateness a
// few milliseconds. The pool is as large as the run, so no call waits for a
// slot.
//
// The engine builds its schedule itself and a test cannot hand it a late one,
// so the pool and the stats, the parts the engine is made of, are driven
// directly. The report is Stats.Report, the one the engine returns; the
// results are returned beside it for the tests to check their premise.
func lateStarter(t *testing.T, s *stand.Stand, n int, late time.Duration) (engine.Report, []engine.Result) {
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
	stats.Reserve(time.Duration(n)*lateEvery+late+timeout+5*time.Second, s.Method())

	start := time.Now().Add(50 * time.Millisecond)
	stats.Start(start, 0)
	stats.EndSending(start.Add(time.Duration(n) * lateEvery))

	in := make(chan engine.Request)
	go func() {
		defer close(in)

		for i := range n {
			due := start.Add(time.Duration(i) * lateEvery)
			select {
			case <-time.After(time.Until(due.Add(late))):
			case <-t.Context().Done():
				return
			}
			in <- engine.Request{Method: s.Method(), ScheduledAt: due, Deadline: due.Add(late + timeout)}
		}
	}()

	var results []engine.Result
	out := make(chan engine.Result, n)
	done := make(chan struct{})
	go func() {
		defer close(done)

		for r := range out {
			stats.Record(r)
			results = append(results, r)
		}
	}()

	pool := engine.NewWorkerPool(sender, n)
	if err := pool.Run(t.Context(), in, out); err != nil {
		t.Fatalf("pool: %v", err)
	}
	close(out)
	<-done

	stats.Finish(time.Now())

	return stats.Report(), results
}

// checkLateStarter fails the test, with the numbers, when the run was not the
// one the test describes: every call counted in the latencies (a call ahead
// of its schedule is dropped from them) and every call as late as asked, or
// none of them over the wait floor.
func checkLateStarter(t *testing.T, report engine.Report, results []engine.Result, late time.Duration) {
	t.Helper()

	if len(report.Methods) != 1 {
		t.Fatalf("%d methods in the report, want 1", len(report.Methods))
	}
	if m := report.Methods[0]; m.Sent != lateCalls || m.Latencies != lateCalls {
		t.Fatalf("sent %d, latencies %d, want %d of both: calls were dropped from the latencies", m.Sent, m.Latencies, lateCalls)
	}
	if len(results) != lateCalls {
		t.Fatalf("%d results, want %d", len(results), lateCalls)
	}

	for i := range results {
		lag := results[i].QueueTime()
		if late > 0 && lag < late {
			t.Fatalf("a call began %v behind its schedule, want every call at least %v behind", lag, late)
		}
	}

	if late == 0 && report.StartLagP99.Value >= report.WaitFloor {
		t.Fatalf("start lag p99 %v, want under the wait floor %v: the machine was too busy for the on-time run to mean anything",
			report.StartLagP99.Value, report.WaitFloor)
	}
}

// The whole way from a generator that started its calls late to the exit
// code's reason: the wait verdict of the report names the generator, the
// report is invalid for it, and the JSON says generator_late (the exit code 2
// is runState's, tested in cmd/leettest on the same report).
func TestReport_ALateStarterMakesTheRunInvalid(t *testing.T) {
	target := stand.Start(stand.Constant(time.Millisecond))
	t.Cleanup(target.Stop)

	const late = 150 * time.Millisecond

	report, results := lateStarter(t, target, lateCalls, late)
	checkLateStarter(t, report, results, late)

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

	report, results := lateStarter(t, target, lateCalls, 0)
	checkLateStarter(t, report, results, 0)

	if cli.GeneratorLate(cli.RunReport{Report: report}) {
		cause, side, ok := report.WaitVerdict()
		t.Errorf("GeneratorLate = true for calls started on time (start lag p99 %v, wait verdict %v %v %v)",
			report.StartLagP99.Value, cause, side, ok)
	}
}
