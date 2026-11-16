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
	"bytes"
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

func collect(ctx context.Context, t *testing.T, s *Scheduler) ([]Request, error) {
	t.Helper()

	out := make(chan Request, 4096)
	errc := make(chan error, 1)

	go func() {
		errc <- s.Run(ctx, out)
		close(out)
	}()

	var got []Request
	for req := range out {
		got = append(got, req)
	}

	return got, <-errc
}

// Ground: boundary — the exact count of a stage: a mutation that loses its last call stays green in
// every end-to-end test (2026-09-22).
func TestRunEmitsOneRequestPerInterval(t *testing.T) {
	s := NewScheduler(Call{Method: "a.B/C", Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 100 * time.Millisecond}}})

	got, err := collect(context.Background(), t, s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(got) != 10 {
		t.Fatalf("got %d requests, want 10", len(got))
	}

	for i := 1; i < len(got); i++ {
		gap := got[i].ScheduledAt.Sub(got[i-1].ScheduledAt)
		if gap != 10*time.Millisecond {
			t.Errorf("gap %d = %s, want 10ms", i, gap)
		}
	}
}

// Ground: contract — Scheduler is exported; its requests are what a library caller sends.
func TestRunWalksEveryStage(t *testing.T) {
	s := NewScheduler(Call{Method: "a.B/C", Stages: []Stage{
		{StartRPS: 100, TargetRPS: 100, Duration: 50 * time.Millisecond},
		{StartRPS: 200, TargetRPS: 200, Duration: 50 * time.Millisecond},
	}})

	got, err := collect(context.Background(), t, s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(got) != 15 {
		t.Fatalf("got %d requests, want 15", len(got))
	}
}

// Ground: boundary — the exact span across a stage seam, to the nanosecond; arrivals at the stand
// carry scheduling jitter.
func TestStagesDoNotDriftApart(t *testing.T) {
	s := NewScheduler(Call{Method: "a.B/C", Stages: []Stage{
		{StartRPS: 100, TargetRPS: 100, Duration: 50 * time.Millisecond},
		{StartRPS: 100, TargetRPS: 100, Duration: 50 * time.Millisecond},
	}})

	got, err := collect(context.Background(), t, s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	span := got[len(got)-1].ScheduledAt.Sub(got[0].ScheduledAt)
	if want := 90 * time.Millisecond; span != want {
		t.Errorf("span = %s, want %s", span, want)
	}
}

// Ground: boundary — a stage whose length is not a multiple of its interval: 3 rps for 500ms
// sends at 0 and 333ms, and the next stage starts at 500ms, not at the third interval (1s) nor
// right after the last call.
func TestNextStageStartsWhereThePreviousEnds(t *testing.T) {
	s := NewScheduler(Call{Method: "a.B/C", Stages: []Stage{
		{StartRPS: 3, TargetRPS: 3, Duration: 500 * time.Millisecond},
		{StartRPS: 100, TargetRPS: 100, Duration: 20 * time.Millisecond},
	}})

	got, err := collect(context.Background(), t, s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(got) != 4 {
		t.Fatalf("got %d requests, want 2 + 2", len(got))
	}
	if at := got[1].ScheduledAt.Sub(got[0].ScheduledAt); at != time.Second/3 {
		t.Errorf("second call at %s, want %s", at, time.Second/3)
	}
	if at := got[2].ScheduledAt.Sub(got[0].ScheduledAt); at != 500*time.Millisecond {
		t.Errorf("second stage starts at %s, want 500ms", at)
	}
}

// Ground: boundary — rounding of nanoseconds per interval must not accumulate; arrivals at the
// stand carry scheduling jitter.
func TestScheduledTimeDoesNotAccumulateRounding(t *testing.T) {
	const rps = 3000

	s := NewScheduler(Call{Method: "a.B/C", Stages: []Stage{{StartRPS: rps, TargetRPS: rps, Duration: 100 * time.Millisecond}}})

	got, err := collect(context.Background(), t, s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	last := got[len(got)-1]
	want := got[0].ScheduledAt.Add(time.Duration(len(got)-1) * time.Second / rps)

	if !last.ScheduledAt.Equal(want) {
		t.Errorf("last scheduled at %s, want %s", last.ScheduledAt, want)
	}
}

// Ground: contract — Scheduler is exported; its requests are what a library caller sends.
func TestRunStopsOnCancel(t *testing.T) {
	// The consumer cancels, not a short deadline: Run measures the clock step once, and that
	// takes 8..40 ms on Windows, enough to eat a 60 ms budget before the first request.
	const guard = 10 * time.Second

	stage := Stage{StartRPS: 100, TargetRPS: 100, Duration: time.Hour}
	s := NewScheduler(Call{Method: "a.B/C", Stages: []Stage{stage}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan Request)
	errc := make(chan error, 1)

	go func() {
		errc <- s.Run(ctx, out)
		close(out)
	}()

	timer := time.NewTimer(guard)
	defer timer.Stop()

	var emitted int

	for open := true; open; {
		select {
		case _, ok := <-out:
			open = ok
			if ok {
				emitted++
				cancel()
			}
		case <-timer.C:
			t.Fatalf("Run did not stop within %s: %d requests emitted", guard, emitted)
		}
	}

	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want %v", err, context.Canceled)
	}
	if emitted == 0 {
		t.Error("no requests emitted before cancel")
	}
	// Stopped early: under a thousandth of what the stage plans.
	if limit := int(stage.Ticks() / 1000); emitted > limit {
		t.Errorf("emitted %d requests, want at most %d (a thousandth of the stage's %d)", emitted, limit, stage.Ticks())
	}
}

// standsInSend reports whether some goroutine is blocked in a select whose own frame is
// Scheduler.runStage: the select that sends to out, the one place Run waits on its reader (the
// wait for the next tick is inside waitUntil, another frame). Run has no seam there, so the test
// reads the stacks.
func standsInSend() bool {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]

	for _, g := range strings.Split(string(buf), "\n\n") {
		lines := strings.SplitN(g, "\n", 3)
		if len(lines) < 2 {
			continue
		}

		if strings.Contains(lines[0], "[select") && strings.Contains(lines[1], "(*Scheduler).runStage") {
			return true
		}
	}

	return false
}

// Ground: contract — Run returns on cancel when nobody reads: it stands in the send to out, and
// the context is its only way out. Mutation: remove `case <-ctx.Done()` from that select, the run
// never returns and the guard fails the test.
func TestRunStopsOnCancelWhileStandingInTheSend(t *testing.T) {
	const guard = 10 * time.Second

	s := NewScheduler(Call{Method: "a.B/C", Stages: []Stage{{StartRPS: 1000, TargetRPS: 1000, Duration: time.Hour}}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Nothing receives from out: the first request builds and Run waits on it for good.
	out := make(chan Request)
	errc := make(chan error, 1)

	go func() { errc <- s.Run(ctx, out) }()

	limit := time.Now().Add(guard)
	for !standsInSend() {
		if time.Now().After(limit) {
			t.Fatalf("Run did not reach the send to out within %s", guard)
		}

		time.Sleep(time.Millisecond)
	}

	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(guard):
		t.Fatalf("Run did not return within %s of the cancel while standing in the send to out", guard)
	}
}

// Ground: contract — a run judges its schedule once, as the Dispatcher does ("nothing in Run asks
// again"). A measure of the clock step per request costs 8..40 ms on Windows, 0.25 s on a 15.6 ms
// clock: it delays every request and turns TestRunStopsOnCancel into a coin flip (8/20 and 16/20
// red at -count=20).
func TestRunMeasuresTheClockStepOnce(t *testing.T) {
	const requests = 5

	var measured int

	saved := stepOf
	stepOf = func(func() time.Time) time.Duration {
		measured++

		return time.Millisecond
	}
	t.Cleanup(func() { stepOf = saved })

	s := NewScheduler(Call{Method: "a.B/C", Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 50 * time.Millisecond}}})

	got, err := collect(context.Background(), t, s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got) != requests {
		t.Fatalf("got %d requests, want %d", len(got), requests)
	}
	if measured > 1 {
		t.Errorf("the clock step was measured %d times for %d requests, want at most once per run", measured, len(got))
	}
}

// Ground: contract — Scheduler is exported; its requests are what a library caller sends.
func TestRequestsCarryCallFields(t *testing.T) {
	call := Call{
		Method:       "a.B/C",
		Payload:      []byte("payload"),
		KeepResponse: true,
		Stages:       []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 20 * time.Millisecond}},
	}
	s := NewScheduler(call)

	got, err := collect(context.Background(), t, s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no requests emitted")
	}

	for i, req := range got {
		if req.Method != call.Method {
			t.Errorf("request %d method = %q, want %q", i, req.Method, call.Method)
		}
		if !bytes.Equal(req.Payload, call.Payload) {
			t.Errorf("request %d payload = %q, want %q", i, req.Payload, call.Payload)
		}
		if req.KeepResponse != call.KeepResponse {
			t.Errorf("request %d KeepResponse = %v, want %v", i, req.KeepResponse, call.KeepResponse)
		}
	}
}

// Ground: contract — Scheduler is exported; its requests are what a library caller sends.
func TestRequestDeadlineFollowsTimeout(t *testing.T) {
	const timeout = 25 * time.Millisecond

	call := Call{
		Method:  "a.B/C",
		Timeout: timeout,
		Stages:  []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 20 * time.Millisecond}},
	}
	s := NewScheduler(call)

	got, err := collect(context.Background(), t, s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no requests emitted")
	}

	for i, req := range got {
		want := req.ScheduledAt.Add(timeout)
		if !req.Deadline.Equal(want) {
			t.Errorf("request %d deadline = %s, want %s", i, req.Deadline, want)
		}
	}
}

// Ground: contract — Scheduler is exported; its requests are what a library caller sends.
func TestRequestDeadlineIsZeroWithoutTimeout(t *testing.T) {
	call := Call{
		Method: "a.B/C",
		Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 20 * time.Millisecond}},
	}
	s := NewScheduler(call)

	got, err := collect(context.Background(), t, s)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no requests emitted")
	}

	for i, req := range got {
		if !req.Deadline.IsZero() {
			t.Errorf("request %d deadline = %s, want zero", i, req.Deadline)
		}
	}
}

// Ground: contract — Scheduler is exported; its requests are what a library caller sends.
func TestRunRejectsBadStages(t *testing.T) {
	tests := []struct {
		name    string
		stages  []Stage
		wantErr error
	}{
		{
			name:    "no stages",
			stages:  nil,
			wantErr: ErrNoStages,
		},
		{
			name:    "zero rps",
			stages:  []Stage{{StartRPS: 0, TargetRPS: 0, Duration: time.Second}},
			wantErr: ErrStageRPS,
		},
		{
			name:    "negative rps",
			stages:  []Stage{{StartRPS: -1, TargetRPS: -1, Duration: time.Second}},
			wantErr: ErrStageRPS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScheduler(Call{Method: "a.B/C", Stages: tt.stages})

			_, err := collect(context.Background(), t, s)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// Ground: contract — the rule for a stage's rates (Spec 4): both ends are at
// least 0, the larger is at least 1, so a ramp up from 0 and a ramp down to 0
// run and a stage idle at both ends does not. validate is asked directly.
func TestScheduler_ValidateStages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stage   Stage
		wantErr error
	}{
		{"start negative", Stage{StartRPS: -1, TargetRPS: 5, Duration: time.Second}, ErrStageRPS},
		{"target negative", Stage{StartRPS: 5, TargetRPS: -1, Duration: time.Second}, ErrStageRPS},
		{"both zero", Stage{StartRPS: 0, TargetRPS: 0, Duration: time.Second}, ErrStageRPS},
		{"a ramp up from zero", Stage{StartRPS: 0, TargetRPS: 100, Duration: time.Second}, nil},
		{"a ramp down to zero", Stage{StartRPS: 100, TargetRPS: 0, Duration: time.Second}, nil},
		{"a ramp down", Stage{StartRPS: 900, TargetRPS: 100, Duration: time.Second}, nil},
		{"a constant stage", Stage{StartRPS: 5, TargetRPS: 5, Duration: time.Second}, nil},
		{"a ramp of no length", Stage{StartRPS: 0, TargetRPS: 100, Duration: 0}, ErrStageDuration},
		{"a ramp of negative length", Stage{StartRPS: 0, TargetRPS: 100, Duration: -time.Second}, ErrStageDuration},
	} {
		err := NewScheduler(Call{Method: "a.B/C", Stages: []Stage{tc.stage}}).validate()
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: validate = %v, want %v", tc.name, err, tc.wantErr)
		}
	}
}

// Ground: contract — a ramp is a stage like the others for a library caller:
// 0 to 100 over 1s is 50 requests (N = 50 whole, the tick at 1s is the hold's),
// the k-th at the stage's offset(k) from the first, which is at the start.
func TestScheduler_ARampRuns(t *testing.T) {
	stage := Stage{StartRPS: 0, TargetRPS: 100, Duration: time.Second}
	got, err := collect(context.Background(), t, NewScheduler(Call{Method: "a.B/C", Stages: []Stage{stage}}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("got %d requests, want 50", len(got))
	}
	for k, req := range got {
		want, _ := stage.offset(k)
		if at := req.ScheduledAt.Sub(got[0].ScheduledAt); at != want {
			t.Fatalf("request %d at %s from the first, want offset(%d) = %s", k, at, k, want)
		}
	}
	for k := 2; k < len(got); k++ {
		prev := got[k-1].ScheduledAt.Sub(got[k-2].ScheduledAt)
		if gap := got[k].ScheduledAt.Sub(got[k-1].ScheduledAt); gap > prev {
			t.Fatalf("gap %d is %s, longer than the gap before it, %s: the ramp slows down", k, gap, prev)
		}
	}
	if first := got[1].ScheduledAt.Sub(got[0].ScheduledAt); first < 100*time.Millisecond {
		t.Errorf("the first gap is %s: a ramp from 0 starts slow, a tenth of a second at least", first)
	}
}

// Ground: boundary — the seam between a ramp and its hold (JMeter's burst at a
// phase start, upstream П8): the hold's first tick is at exactly the ramp's
// length, the gap to the last ramp tick is above 0 and no longer than the gap
// between the last two ramp ticks, and the hold's own gaps are 10ms exactly.
// A ramp of 1s has 50 ticks, one of 1010ms has 50.5 and so 51.
func TestScheduler_NoBurstAtTheRampEnd(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ramp      time.Duration
		hold      time.Duration
		rampTicks int
	}{
		{"a whole count", time.Second, time.Second, 50},
		{"half a tick over", 1010 * time.Millisecond, 200 * time.Millisecond, 51},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := Call{Method: "a.B/C", Stages: []Stage{
				{StartRPS: 0, TargetRPS: 100, Duration: tc.ramp},
				{StartRPS: 100, TargetRPS: 100, Duration: tc.hold},
			}}
			got, err := collect(context.Background(), t, NewScheduler(call))
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			holdTicks := int(tc.hold / (10 * time.Millisecond))
			if len(got) != tc.rampTicks+holdTicks {
				t.Fatalf("got %d requests, want %d + %d", len(got), tc.rampTicks, holdTicks)
			}
			at := func(k int) time.Duration { return got[k].ScheduledAt.Sub(got[0].ScheduledAt) }
			if last := at(tc.rampTicks - 1); last >= tc.ramp {
				t.Errorf("the last ramp tick is at %s, not before the ramp's end %s", last, tc.ramp)
			}
			if first := at(tc.rampTicks); first != tc.ramp {
				t.Errorf("the hold starts at %s, want exactly the ramp's length %s", first, tc.ramp)
			}
			seam := at(tc.rampTicks) - at(tc.rampTicks-1)
			lastGap := at(tc.rampTicks-1) - at(tc.rampTicks-2)
			if seam <= 0 || seam > lastGap {
				t.Errorf("the gap over the seam is %s, want above 0 and at most the last ramp gap %s", seam, lastGap)
			}
			for k := tc.rampTicks + 1; k < len(got); k++ {
				if gap := at(k) - at(k-1); gap != 10*time.Millisecond {
					t.Errorf("hold gap %d is %s, want 10ms", k, gap)
				}
			}
		})
	}
}
