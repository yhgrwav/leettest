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
