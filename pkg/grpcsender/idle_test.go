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

package grpcsender

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
)

// stalled dials a target that never completes the dial, so only the check
// before it can end Connect early.
func stalled(idle time.Duration) *Sender {
	return dialing(idle, func(ctx context.Context, _ string) (net.Conn, error) {
		<-ctx.Done()

		return nil, ctx.Err()
	})
}

// refused dials a target that refuses at once: Connect without a deadline
// still ends.
func refused(idle time.Duration) *Sender {
	return dialing(idle, func(context.Context, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	})
}

func dialing(idle time.Duration, dial func(context.Context, string) (net.Conn, error)) *Sender {
	return New(Options{
		Target:      "passthrough:///target",
		IdleTimeout: idle,
		DialOptions: []grpc.DialOption{grpc.WithContextDialer(dial)},
	})
}

// Ground: boundary — an idle timeout shorter than the time Connect has cuts
// the first dial, and Connect hangs to its deadline with "deadline exceeded"
// (checked 2026-09-25). It is refused at once, naming both durations and what
// to change. Equal or longer is allowed: Connect gives up first, the idle
// timer never cuts its dial. No deadline is unbounded time, so any idle
// timeout is shorter.
func TestConnect_AnIdleTimeoutShorterThanTheConnectIsRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := stalled(100 * time.Millisecond).Connect(ctx)
	if !errors.Is(err, ErrIdleShorterThanConnect) || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("after %v: %v, want ErrIdleShorterThanConnect at once", time.Since(start), err)
	}
	for _, want := range []string{"idle timeout 100ms", "raise the idle timeout or shorten the connect timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not say %q", err, want)
		}
	}
	// What is left of 300ms, rounded up: the timeout the user set, not
	// 299.987ms or 299ms.
	if !strings.Contains(err.Error(), "connect timeout 300ms;") {
		t.Errorf("%q does not give the connect timeout as 300ms", err)
	}

	// The deadline at the idle timeout leaves Connect a little less than it.
	ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if equal := stalled(100 * time.Millisecond).Connect(ctx); errors.Is(equal, ErrIdleShorterThanConnect) {
		t.Errorf("idle equal to the connect time: %v, want it allowed", equal)
	}

	err = refused(time.Hour).Connect(context.WithoutCancel(t.Context()))
	if want := "connect has no deadline; set a connect timeout or drop the idle timeout"; !errors.Is(err, ErrIdleShorterThanConnect) ||
		!strings.Contains(err.Error(), want) {
		t.Errorf("no deadline: %v, want ErrIdleShorterThanConnect saying %q", err, want)
	}

	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := stalled(0).Connect(ctx); errors.Is(err, ErrIdleShorterThanConnect) {
		t.Errorf("no idle timeout set: %v, want no check", err)
	}
}
