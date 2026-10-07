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

const (
	// timerMargin is how long before a moment the exact wait leaves the Go
	// timer, which wakes up to 1ms late on Linux.
	timerMargin = 2 * time.Millisecond
	// spinWindow is how long before a moment it stops sleeping and
	// busy-waits.
	spinWindow = 50 * time.Microsecond
)

// Dispatcher hands out the requests of every call of a run, in the order of
// their scheduled moments. One goroutine for the whole run: its wait costs
// the same however many methods there are.
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
		// A dataset call with no counter counts this run from its first record.
		c := &cursor{call: counted(call), stageStart: start}
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
		// The count of the call is S, not c.i: that one starts over with each
		// stage. It moves once the request has left, so a request built and
		// never handed out used no record.
		c.call.handedOut()

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

// exactSchedule says whether the dispatcher waits to each moment exactly, on
// this host.
func exactSchedule() bool {
	return exactScheduleFor(preciseSleep, clock.StepOf(time.Now))
}

// exactScheduleFor is exactSchedule on a host that can or cannot sleep to a
// microsecond (Linux, clock_nanosleep) and has a clock of step. Not on a
// clock of 1µs or coarser (Windows, ~0.5ms): a wait cannot end closer than a
// step, and there a busy-wait was measured worse than the plain timer.
func exactScheduleFor(precise bool, step time.Duration) bool {
	return precise && step < time.Microsecond
}

// waitUntil returns at at, or when ctx ends with its error. Not exact: a plain
// timer, which on Linux wakes up to 1ms late (the runtime waits in epoll with
// millisecond timeouts). Exact: the Go timer to timerMargin before at, a
// microsecond sleep to spinWindow before it, a busy-wait for the rest — short,
// so that a host whose every core is busy does not preempt it for a slice.
func waitUntil(ctx context.Context, at time.Time, exact bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	sleep := time.Until(at)
	if exact {
		sleep -= timerMargin
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

	if err := sleepPrecisely(ctx, at.Add(-spinWindow)); err != nil {
		return err
	}

	return spinUntil(ctx, at)
}

// spinUntil busy-waits to at, or until ctx ends with its error.
func spinUntil(ctx context.Context, at time.Time) error {
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
