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
	"context"
	"math/rand/v2"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/clock"
)

// Ground: boundary — a call that leaves before its moment is a call the plan
// did not make; latency from scheduledAt would then be short.
func TestWaitUntil_NeverBeforeTheMoment(t *testing.T) {
	for i := range 300 {
		at := time.Now().Add(time.Duration(rand.Int64N(int64(3 * time.Millisecond))))
		if err := waitUntil(t.Context(), at, true); err != nil {
			t.Fatalf("wait %d: %v", i, err)
		}
		if now := time.Now(); now.Before(at) {
			t.Fatalf("wait %d returned %v before its moment", i, at.Sub(now))
		}
	}
}

// Ground: hot path — every latency counts from scheduledAt, so the wait's
// lateness is in all of them. The Go timer wakes up to 1ms late on Linux
// (epoll waits in milliseconds); the exact wait lands within a clock step.
func TestWaitUntil_ExactLandsWithinAStep(t *testing.T) {
	// Off with one P or a coarse clock (exactScheduleFor); the dispatcher
	// keeps the plain timer there, and this host cannot check the spin.
	if !exactScheduleFor(runtime.GOMAXPROCS(0), clock.StepOf(time.Now)) {
		t.Skip("the exact wait is off on this host")
	}
	if exact := lateness(t, true); exact > 200*time.Microsecond {
		t.Errorf("p99 lateness %v, want at most 200µs", exact)
	}
}

// lateness is the p99 of how late 300 waits return.
func lateness(t *testing.T, exact bool) time.Duration {
	t.Helper()

	late := make([]time.Duration, 0, 300)
	for range 300 {
		at := time.Now().Add(2*time.Millisecond + time.Duration(rand.Int64N(int64(time.Millisecond))))
		if err := waitUntil(t.Context(), at, exact); err != nil {
			t.Fatalf("wait: %v", err)
		}
		late = append(late, time.Since(at))
	}
	slices.Sort(late)

	return late[len(late)*99/100]
}

// Ground: boundary — a stop must not wait out the busy-wait.
func TestWaitUntil_CancelCutsTheSpin(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	start := time.Now()
	at := start.Add(5 * time.Millisecond)

	// Inside the last millisecond, where the wait spins. Not a timer: a Go
	// timer is itself up to 1ms late.
	var cancelledAt atomic.Int64
	go func() {
		for time.Since(start) < 4300*time.Microsecond {
			runtime.Gosched()
		}
		cancelledAt.Store(time.Now().UnixNano())
		cancel()
	}()

	if err := waitUntil(ctx, at, true); err == nil {
		t.Fatalf("returned nil, want the context's error")
	}
	if took := time.Duration(time.Now().UnixNano() - cancelledAt.Load()); took > 100*time.Microsecond+clock.StepOf(time.Now) {
		t.Errorf("returned %v after the cancel, want at most 100µs", took)
	}
}

// Ground: boundary — a moment already gone is not waited for.
func TestWaitUntil_APastMomentReturnsAtOnce(t *testing.T) {
	start := time.Now()
	if err := waitUntil(t.Context(), start.Add(-time.Second), true); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if took := time.Since(start); took > 100*time.Microsecond+clock.StepOf(time.Now) {
		t.Errorf("took %v for a past moment", took)
	}
}

// Ground: concurrency — with too few Ps a busy-wait keeps the senders, the stats
// handler and the replies from running until preemption (~10ms): it adds
// more than it saves. The plain timer stays.
// A clock of 1µs or coarser (Windows) cannot end a spin closer than a step,
// and there the spin was measured worse than the timer.
func TestExactSchedule_NeedsThreeProcsAndAFineClock(t *testing.T) {
	for _, tc := range []struct {
		procs int
		step  time.Duration
		exact bool
	}{
		{1, 40 * time.Nanosecond, false},
		{2, 40 * time.Nanosecond, false},
		{3, 40 * time.Nanosecond, true},
		{3, 999 * time.Nanosecond, true},
		{3, time.Microsecond, false},
		{8, 500 * time.Microsecond, false},
	} {
		if got := exactScheduleFor(tc.procs, tc.step); got != tc.exact {
			t.Errorf("%d Ps, step %v: exact %v, want %v", tc.procs, tc.step, got, tc.exact)
		}
	}
}

// Ground: hot path — one dispatcher for the run: its busy-wait costs at most
// one core however many methods there are.
func TestDispatcher_OneGoroutineForEveryCall(t *testing.T) {
	calls := []Call{
		{Method: "a.B/One", Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 200 * time.Millisecond}}},
		{Method: "a.B/Two", Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 200 * time.Millisecond}}},
		{Method: "a.B/Three", Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 200 * time.Millisecond}}},
	}
	before := runtime.NumGoroutine()

	out := make(chan Request, 4096)
	done := make(chan error, 1)
	go func() { done <- NewDispatcher(calls).Run(t.Context(), out) }()

	peak := 0
	for range 20 {
		time.Sleep(5 * time.Millisecond)
		peak = max(peak, runtime.NumGoroutine()-before)
	}
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	// The one above is the Run goroutine itself.
	if peak > 1 {
		t.Errorf("%d goroutines while dispatching 3 calls, want 1", peak)
	}
}

// Ground: contract — one stream of requests, in the order of their moments,
// every call's count exact.
func TestDispatcher_MergesInScheduleOrder(t *testing.T) {
	calls := []Call{
		{Method: "a.B/One", Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 100 * time.Millisecond}}},
		{Method: "a.B/Two", Stages: []Stage{{StartRPS: 150, TargetRPS: 150, Duration: 100 * time.Millisecond}}},
		{Method: "a.B/Three", Stages: []Stage{{StartRPS: 200, TargetRPS: 200, Duration: 100 * time.Millisecond}}},
	}
	out := make(chan Request, 4096)
	if err := NewDispatcher(calls).Run(t.Context(), out); err != nil {
		t.Fatalf("run: %v", err)
	}
	close(out)

	counts := map[string]int{}
	var last time.Time
	for req := range out {
		if req.ScheduledAt.Before(last) {
			t.Errorf("%s at %v came after a later moment", req.Method, req.ScheduledAt.Sub(last))
		}
		last = req.ScheduledAt
		counts[req.Method]++
	}
	for method, want := range map[string]int{"a.B/One": 10, "a.B/Two": 15, "a.B/Three": 20} {
		if counts[method] != want {
			t.Errorf("%s: %d requests, want %d", method, counts[method], want)
		}
	}
}
