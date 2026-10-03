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
	"errors"
	"testing"
	"time"
)

// Ground: contract — FakeSender is the -fake target, its behaviour part of the API (reviewer, 2026-10-02).
func TestFakeSenderReportsTimeoutOnPastDeadline(t *testing.T) {
	f := FakeSender{Delay: time.Hour}
	req := Request{Deadline: time.Now().Add(-time.Millisecond)}

	outcome, err := f.Send(context.Background(), req)

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if outcome.Category != CategoryTimeout {
		t.Errorf("category = %v, want %v", outcome.Category, CategoryTimeout)
	}
	if outcome.SentAt.IsZero() || outcome.DoneAt.IsZero() {
		t.Errorf("outcome = %+v, want non-zero SentAt and DoneAt", outcome)
	}
}

// Ground: contract — FakeSender is the -fake target, its behaviour part of the API (reviewer, 2026-10-02).
func TestFakeSenderReportsErrorOnCanceledParent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	f := FakeSender{Delay: time.Hour}

	_, err := f.Send(ctx, Request{})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want %v", err, context.Canceled)
	}
}

// Ground: contract — FakeSender is the -fake target, its behaviour part of the API (reviewer, 2026-10-02).
func TestFakeSenderAlwaysFails(t *testing.T) {
	f := FakeSender{FailRatio: 1}

	outcome, err := f.Send(context.Background(), Request{})

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if outcome.Category != CategoryServerFault {
		t.Errorf("category = %v, want %v", outcome.Category, CategoryServerFault)
	}
	if !errors.Is(outcome.Err, ErrFakeFailure) {
		t.Errorf("outcome.Err = %v, want %v", outcome.Err, ErrFakeFailure)
	}
}

// Ground: contract — FakeSender is the -fake target, its behaviour part of the API (reviewer, 2026-10-02).
func TestFakeSenderSucceeds(t *testing.T) {
	f := FakeSender{}

	outcome, err := f.Send(context.Background(), Request{})

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if outcome.Category != CategorySuccess {
		t.Errorf("category = %v, want %v", outcome.Category, CategorySuccess)
	}
	if outcome.Err != nil {
		t.Errorf("outcome.Err = %v, want nil", outcome.Err)
	}
	if outcome.SentAt.IsZero() || outcome.DoneAt.IsZero() {
		t.Errorf("outcome = %+v, want non-zero SentAt and DoneAt", outcome)
	}
}

// Ground: contract — the quickstart's -fake-delay 30ms -fake-jitter 10ms:
// every answer takes 30ms to 40ms, and the jitter spreads them. The upper
// margin is a timer's lateness, under half the delay; below 30ms is strict.
func TestFakeSenderAnswersAfterTheDelayPlusJitter(t *testing.T) {
	f := FakeSender{Delay: 30 * time.Millisecond, Jitter: 10 * time.Millisecond}

	took := make([]time.Duration, 50)
	done := make(chan struct{})
	for i := range took {
		go func() {
			defer func() { done <- struct{}{} }()
			outcome, err := f.Send(context.Background(), Request{})
			if err != nil || outcome.Category != CategorySuccess {
				t.Errorf("call %d: %v %v, want success", i, outcome.Category, err)
			}
			took[i] = outcome.DoneAt.Sub(outcome.SentAt)
		}()
	}
	for range took {
		<-done
	}

	lo, hi := took[0], took[0]
	for _, d := range took {
		lo, hi = min(lo, d), max(hi, d)
	}
	if lo < 30*time.Millisecond || hi > 55*time.Millisecond {
		t.Errorf("answers took %v to %v, want 30ms to 40ms (and a timer's lateness)", lo, hi)
	}
	if hi-lo < 2*time.Millisecond {
		t.Errorf("answers took %v to %v: no jitter", lo, hi)
	}
}

// Ground: contract — -fake-fail-ratio is the share of calls that fail: 0.3
// over 4000 calls is 1200, and 5 standard deviations (sqrt(4000 × 0.21) ≈
// 29) is ±145.
func TestFakeSenderFailsItsShareOfCalls(t *testing.T) {
	f := FakeSender{FailRatio: 0.3}

	failed := 0
	for range 4000 {
		outcome, err := f.Send(context.Background(), Request{})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if outcome.Category == CategoryServerFault {
			failed++
		}
	}
	if failed < 1200-145 || failed > 1200+145 {
		t.Errorf("%d of 4000 failed, want 1200 ± 145", failed)
	}
}
