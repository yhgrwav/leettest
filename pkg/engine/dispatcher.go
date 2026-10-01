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
	"container/heap"
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/yhgrwav/leettest/pkg/clock"
)

// spinWindow is how long before a moment the exact wait stops sleeping and
// busy-waits: the Go timer on Linux wakes up to 1ms late (the runtime waits in
// epoll with millisecond timeouts).
const spinWindow = time.Millisecond

// Dispatcher hands out the requests of every call of a run, in the order of
// their scheduled moments. One goroutine for the whole run: its busy-wait
// costs at most one core however many methods there are.
type Dispatcher struct {
	calls []Call
	// start is the plan's moment zero; zero means when Run begins.
	start time.Time
	exact bool
}

func NewDispatcher(calls []Call) *Dispatcher {
	return &Dispatcher{calls: calls, exact: exactSchedule()}
}

// Run sends each request to out at its scheduled moment until every call's
// plan is done or ctx ends.
func (d *Dispatcher) Run(ctx context.Context, out chan<- Request) error {
	for _, call := range d.calls {
		if err := NewScheduler(call).validate(); err != nil {
			return fmt.Errorf("%s: %w", call.Method, err)
		}
	}

	exact, start := d.exact, d.start
	if start.IsZero() {
		start = time.Now()
	}

	var queue cursors
	for _, call := range d.calls {
		c := &cursor{call: call, stageStart: start}
		if c.advance() {
			queue = append(queue, c)
		}
	}
	heap.Init(&queue)

	for queue.Len() > 0 {
		c := queue[0]
		if err := waitUntil(ctx, c.at, exact); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- NewScheduler(c.call).newRequest(c.at):
		}

		c.i++
		if c.advance() {
			heap.Fix(&queue, 0)
		} else {
			heap.Pop(&queue)
		}
	}

	return nil
}

// cursor walks one call's plan: the i-th call of stage stage.
type cursor struct {
	call       Call
	stage, i   int
	stageStart time.Time
	at         time.Time
}

// advance sets at to the moment of the current call, moving on to the next
// stage when this one is done; false when the plan is.
func (c *cursor) advance() bool {
	for c.stage < len(c.call.Stages) {
		stage := c.call.Stages[c.stage]
		offset := time.Duration(c.i) * time.Second / time.Duration(stage.TargetRPS)
		if offset < stage.Duration {
			c.at = c.stageStart.Add(offset)

			return true
		}
		c.stageStart = c.stageStart.Add(stage.Duration)
		c.stage++
		c.i = 0
	}

	return false
}

type cursors []*cursor

func (q cursors) Len() int           { return len(q) }
func (q cursors) Less(i, j int) bool { return q[i].at.Before(q[j].at) }
func (q cursors) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *cursors) Push(x any) {
	if c, ok := x.(*cursor); ok {
		*q = append(*q, c)
	}
}

func (q *cursors) Pop() any {
	old := *q
	last := old[len(old)-1]
	*q = old[:len(old)-1]

	return last
}

// exactSchedule says whether the dispatcher may busy-wait the last
// millisecond to a moment, on this host.
func exactSchedule() bool {
	return exactScheduleFor(runtime.GOMAXPROCS(0), clock.StepOf(time.Now))
}

// exactScheduleFor is exactSchedule for procs Ps and a clock of step. The
// spin takes a whole core: it needs three Ps, so two are left for the
// senders, the stats and the replies. Not two: under a cgroup CPU limit Go
// sets at least 2 Ps (runtime/cgroup_linux_test.go), so --cpus=1 shows as 2,
// and there the spin was measured to raise start lag from 1ms to 44ms. Not on
// a clock of 1µs or coarser (Windows, ~0.5ms): a spin cannot end closer than
// a step, and there it was measured worse than the plain timer.
func exactScheduleFor(procs int, step time.Duration) bool {
	return procs >= 3 && step < time.Microsecond
}

// waitUntil returns at at, or when ctx ends with its error. exact sleeps to
// spinWindow before at and busy-waits the rest; otherwise a plain timer.
func waitUntil(ctx context.Context, at time.Time, exact bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	sleep := time.Until(at)
	if exact {
		sleep -= spinWindow
	}
	if sleep > 0 {
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()

			return ctx.Err()
		case <-timer.C:
		}
	}
	if !exact {
		return nil
	}

	for time.Now().Before(at) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		runtime.Gosched()
	}

	return nil
}
