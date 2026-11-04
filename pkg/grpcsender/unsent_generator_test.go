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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// sleepsPastTheDeadline stands in for a generator that hands a call to grpc-go
// late: the call waits in the interceptor until its deadline has passed, then
// goes on to the invoker, which refuses it before any stats.Begin. The
// connection is READY the whole time.
func sleepsPastTheDeadline() grpc.DialOption {
	return grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		<-ctx.Done()

		return invoker(ctx, method, req, reply, cc, opts...)
	})
}

// A call that never began on a connection that was READY since the sender
// took it, with the resolver done, did not wait for the connection: the
// target's connection is not what held it. It is the generator's.
// Mutation "no Begin is the connection" must turn this red;
// TestSend_UnsentWhileReconnectingIsBlockedOnTheConnection is its other side.
func TestSend_UnsentOnAReadyConnectionIsBlockedOnTheGenerator(t *testing.T) {
	opts := listen(t, &seeingTarget{})
	opts.DialOptions = append(opts.DialOptions, sleepsPastTheDeadline())
	sender := connected(t, opts)

	req := request(time.Now())
	req.Deadline = req.ScheduledAt.Add(100 * time.Millisecond)

	out, err := sender.Send(bounded(t), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if !out.NotSent {
		t.Fatalf("not sent = false, category %v: the deadline passed before the call began", out.Category)
	}
	if out.NotSentOn != engine.BlockedOnGenerator {
		t.Errorf("blocked on %v, want the generator: the connection was READY the whole time", out.NotSentOn)
	}
}

// holdsUntil stands in for a generator that hands a call over late, with the
// target blinking meanwhile: the call tells the test it has begun waiting,
// waits for go-ahead, then waits out its deadline and goes to the invoker.
func holdsUntil(started chan<- struct{}, proceed <-chan struct{}) grpc.DialOption {
	return grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		started <- struct{}{}
		<-proceed
		<-ctx.Done()

		return invoker(ctx, method, req, reply, cc, opts...)
	})
}

// Ground: boundary — the connection was READY when the call was handed over
// and READY again at its deadline, but not in between: the target blinked, and
// the call is the connection's (#91). A rule that looks at the state now, or
// from the call's Begin (there is none), would put it on the generator and turn
// a verdict on the target into generator_late. Green before the change and
// after it. Mutation "READY now instead of throughout since the call was
// handed over" must turn this red.
func TestSend_UnsentAfterABlinkOfTheConnectionIsBlockedOnTheConnection(t *testing.T) {
	started, proceed := make(chan struct{}, 1), make(chan struct{})
	target := dropping(t, holdsUntil(started, proceed))
	link := target.sender.link

	req := request(time.Now())
	req.Deadline = time.Now().Add(time.Second)

	type result struct {
		out engine.Outcome
		err error
	}

	done := make(chan result, 1)

	go func() {
		out, err := target.sender.Send(context.Background(), req)
		done <- result{out, err}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the call never reached the interceptor")
	}

	dropped := time.Now()

	target.drop(t)
	waitUntil(t, "the watcher to see the connection leave READY", func() bool { return !link.ready.throughout(time.Now()) })

	// The clock may tick coarsely: a re-entry stamped in the tick of the exit
	// would not read as one.
	time.Sleep(20 * time.Millisecond)
	target.open()
	target.sender.conn.Connect()
	waitUntil(t, "the watcher to see the connection READY again", func() bool {
		link.ready.mu.Lock()
		defer link.ready.mu.Unlock()

		return target.sender.conn.GetState() == connectivity.Ready && link.ready.enterAt.After(dropped)
	})

	close(proceed)

	got := <-done
	if got.err != nil {
		t.Fatalf("send: %v", got.err)
	}

	if !got.out.NotSent {
		t.Fatalf("not sent = false, category %v: the deadline passed before the call began", got.out.Category)
	}
	if got.out.NotSentOn != engine.BlockedOnConnection {
		t.Errorf("blocked on %v, want the connection: it left READY while the call waited", got.out.NotSentOn)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()

	for deadline := time.Now().Add(5 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(time.Millisecond)
	}
}
