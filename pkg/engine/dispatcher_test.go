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

// Ground: boundary — a stop must not wait out the exact wait. Each part is
// waited on for 10ms and cancelled after 1ms; it must return within 1ms of
// the cancel, a tenth of the wait: cut, not waited out. Idle on Linux it
// returns in ~10µs (busy-wait) and ~100µs (a sleep chunk and a wake-up);
// with every core busy under -race 180µs was seen, so the bound is the
// tenth. Whether the exact wait beats the timer is measured in the CI tour,
// alone, not here next to other packages under -race.
func TestWaitUntil_CancelCutsTheWait(t *testing.T) {
	for _, part := range []struct {
		name string
		wait func(context.Context, time.Time) error
	}{
		{"busy-wait", spinUntil},
		{"microsecond sleep", sleepPrecisely},
	} {
		ctx, cancel := context.WithCancel(t.Context())
		start := time.Now()

		// Not a timer: a Go timer is itself up to 1ms late.
		var cancelledAt atomic.Int64
		go func() {
			for time.Since(start) < time.Millisecond {
				runtime.Gosched()
			}
			cancelledAt.Store(time.Now().UnixNano())
			cancel()
		}()

		if err := part.wait(ctx, start.Add(10*time.Millisecond)); err == nil {
			t.Fatalf("%s: returned nil, want the context's error", part.name)
		}
		took := time.Duration(time.Now().UnixNano() - cancelledAt.Load())
		if took > time.Millisecond+clock.StepOf(time.Now) {
			t.Errorf("%s: returned %v after the cancel, want at most 1ms", part.name, took)
		}
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

// Ground: boundary — exact only where a microsecond sleep exists (Linux) and
// the clock is finer than 1µs: on Windows (~0.5ms) a busy-wait was measured
// worse than the timer.
func TestExactSchedule_NeedsAPreciseSleepAndAFineClock(t *testing.T) {
	for _, tc := range []struct {
		precise bool
		step    time.Duration
		exact   bool
	}{
		{true, 40 * time.Nanosecond, true},
		{true, 999 * time.Nanosecond, true},
		{true, time.Microsecond, false},
		{true, 500 * time.Microsecond, false},
		{false, 40 * time.Nanosecond, false},
	} {
		if got := exactScheduleFor(tc.precise, tc.step); got != tc.exact {
			t.Errorf("precise sleep %v, step %v: exact %v, want %v", tc.precise, tc.step, got, tc.exact)
		}
	}
}

// Ground: hot path — one dispatcher for the run: its wait costs the same
// however many methods there are.
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
