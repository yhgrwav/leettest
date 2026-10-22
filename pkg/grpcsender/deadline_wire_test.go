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
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/health/grpc_health_v1"
)

// deadlineTarget records what the server side of a call sees of the client's
// deadline: grpc-go builds the handler's ctx deadline from the grpc-timeout
// header, so a deadline here means the header came over the wire.
type deadlineTarget struct {
	grpc_health_v1.UnimplementedHealthServer

	mu    sync.Mutex
	calls int
	has   bool
	left  time.Duration
}

func (d *deadlineTarget) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (
	*grpc_health_v1.HealthCheckResponse, error,
) {
	deadline, ok := ctx.Deadline()

	d.mu.Lock()
	d.calls++
	d.has = ok
	d.left = time.Until(deadline)
	d.mu.Unlock()

	return &grpc_health_v1.HealthCheckResponse{}, nil
}

// Ground: contract — the call's deadline goes to the target as grpc-timeout,
// so the target works to the budget the load test measures against. Without it
// the target runs unbounded and a slow answer is measured as a success. The
// budget is long next to the test's own scheduling: the target must see most of
// it, never more.
func TestSend_TheCallDeadlineReachesTheTargetAsItsBudget(t *testing.T) {
	const budget = 10 * time.Second

	target := &deadlineTarget{}
	sender := connected(t, listen(t, target))

	req := request(time.Now())
	req.Deadline = time.Now().Add(budget)

	out, err := sender.Send(t.Context(), req)
	if err != nil || out.Code != "OK" {
		t.Fatalf("send: %v, code %v", err, out.Code)
	}

	target.mu.Lock()
	defer target.mu.Unlock()

	if target.calls != 1 {
		t.Fatalf("the target saw %d calls, want 1", target.calls)
	}
	if !target.has {
		t.Fatal("the target saw no deadline: grpc-timeout did not go out")
	}
	if target.left > budget+time.Millisecond {
		t.Errorf("the target sees %v left, more than the budget %v", target.left, budget)
	}
	if target.left < budget/2 {
		t.Errorf("the target sees %v left of the budget %v", target.left, budget)
	}
}

// Ground: contract — the inverse: a call without a deadline sends no
// grpc-timeout, the target sees no budget the user did not set.
func TestSend_ACallWithoutADeadlineSendsNoBudget(t *testing.T) {
	target := &deadlineTarget{}
	sender := connected(t, listen(t, target))

	out, err := sender.Send(t.Context(), request(time.Now()))
	if err != nil || out.Code != "OK" {
		t.Fatalf("send: %v, code %v", err, out.Code)
	}

	target.mu.Lock()
	defer target.mu.Unlock()

	if target.calls != 1 {
		t.Fatalf("the target saw %d calls, want 1", target.calls)
	}
	if target.has {
		t.Errorf("the target saw a deadline (%v left) the call did not have", target.left)
	}
}
