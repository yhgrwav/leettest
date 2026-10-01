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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// cancelling answers CANCELLED after wait, or on its own ctx.Done() when
// wait is 0: the target's reaction to our cancel.
func cancelling(wait func(ctx context.Context) time.Duration) grpc.ServerOption {
	return grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		var in []byte
		if err := stream.RecvMsg(&in); err != nil {
			return err
		}
		if d := wait(stream.Context()); d > 0 {
			<-time.After(d)
		} else {
			<-stream.Context().Done()
		}

		return status.Error(codes.Canceled, "the target cancelled")
	})
}

func cancelRequest(deadline time.Duration) engine.Request {
	req := request(time.Now())
	req.Method = "/leettest.test.Cancel/Get"
	req.Deadline = time.Now().Add(deadline)

	return req
}

// echoAfterOurDeadline stands in for the race a loaded client loses: our
// deadline fires, the target answers our RST with CANCELLED, and its trailer
// is read before our own handling of the deadline ends. A stand cannot order
// that; the interceptor does, by recording the trailer as arriving after the
// call's deadline and returning its status.
func echoAfterOurDeadline() grpc.DialOption {
	return grpc.WithUnaryInterceptor(func(ctx context.Context, _ string, _, _ any,
		_ *grpc.ClientConn, _ grpc.UnaryInvoker, _ ...grpc.CallOption,
	) error {
		call, _ := ctx.Value(callKey{}).(*callStats)
		call.mu.Lock()
		call.times.sentAt = time.Now()
		call.mu.Unlock()

		<-ctx.Done()

		call.mu.Lock()
		call.times.answered, call.times.answeredAt = true, time.Now()
		call.mu.Unlock()

		return status.Error(codes.Canceled, "context canceled")
	})
}

// Ground: concurrency — the target's CANCELLED in answer to our own deadline
// is our timeout echoed back, not the target failing: counted as failure it
// would move timeouts into the target's faults, and its code into "sent by
// the target".
func TestCancelled_AnEchoOfOurDeadlineIsATimeout(t *testing.T) {
	opts := listen(t, &seeingTarget{})
	opts.DialOptions = append(opts.DialOptions, echoAfterOurDeadline())
	sender := connected(t, opts)

	out, err := sender.Send(context.Background(), cancelRequest(300*time.Millisecond))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if out.Category != engine.CategoryTimeout {
		t.Errorf("category = %v, want %v: the CANCELLED came after our deadline", out.Category, engine.CategoryTimeout)
	}
	if out.CodeFromTarget {
		t.Errorf("code %s from target: it echoes our cancel", out.Code)
	}
}

// Ground: concurrency — the echo is grpc-go on the target answering our RST,
// frozen application or not: like a DEADLINE_EXCEEDED copy of our deadline,
// it does not show the target alive, and must not move where the silence
// begins.
func TestCancelled_AnEchoOfOurDeadlineIsNotHeard(t *testing.T) {
	opts := listen(t, &seeingTarget{})
	opts.DialOptions = append(opts.DialOptions, echoAfterOurDeadline())
	sender := connected(t, opts)

	out, err := sender.Send(context.Background(), cancelRequest(300*time.Millisecond))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if out.Heard {
		t.Error("heard = true: the CANCELLED is the target's echo of our deadline")
	}
}

// Ground: contract — the inverse: a CANCELLED of the target's own, before our
// deadline, is the target answering.
func TestCancelled_AnEarlyCancelFromTheTargetIsHeard(t *testing.T) {
	wait := func(context.Context) time.Duration { return 50 * time.Millisecond }
	sender := connected(t, listen(t, &seeingTarget{}, grpc.ForceServerCodec(rawCodec{}), cancelling(wait)))

	out, err := sender.Send(context.Background(), cancelRequest(time.Second))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !out.Heard {
		t.Errorf("heard = false on the target's own CANCELLED 950ms before our deadline (%v)", out.Err)
	}
}

// Ground: contract — a target that cancels on its own, well before our
// deadline, failed the call itself.
func TestCancelled_AnEarlyCancelFromTheTargetIsAFailure(t *testing.T) {
	checkTargetCancel(t, func(context.Context) time.Duration { return 50 * time.Millisecond })
}

// Ground: boundary — a cancel of the target's own at 95% of our deadline is
// still before our cancel: a 90% threshold would take it for an echo.
func TestCancelled_ACancelFromTheTargetJustBeforeOurDeadlineIsAFailure(t *testing.T) {
	checkTargetCancel(t, func(ctx context.Context) time.Duration {
		deadline, _ := ctx.Deadline()
		return time.Until(deadline) * 95 / 100
	})
}

func checkTargetCancel(t *testing.T, wait func(context.Context) time.Duration) {
	t.Helper()

	sender := connected(t, listen(t, &seeingTarget{}, grpc.ForceServerCodec(rawCodec{}), cancelling(wait)))

	out, err := sender.Send(context.Background(), cancelRequest(time.Second))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if out.Category != engine.CategoryServerFault || !out.CodeFromTarget || out.Code != codes.Canceled.String() {
		t.Errorf("category %v, code %s from target %v: want the target's own CANCELLED as a failure (%v)",
			out.Category, out.Code, out.CodeFromTarget, out.Err)
	}
}

// Ground: contract — stopping the run cancels the calls in flight, and the
// target's echo of that is not a code of the target: the call is aborted.
func TestCancelled_AnEchoOfTheRunStoppingIsNoCode(t *testing.T) {
	sender := connected(t, listen(t, &seeingTarget{}, grpc.ForceServerCodec(rawCodec{}),
		cancelling(func(context.Context) time.Duration { return 0 })))

	ctx, stop := context.WithCancel(context.Background())
	go func() {
		<-time.After(100 * time.Millisecond)
		stop()
	}()

	_, err := sender.Send(ctx, cancelRequest(time.Second))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the run's stop: an aborted call, kept out of the codes", err)
	}
}
