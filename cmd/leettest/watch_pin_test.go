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

package main

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
)

// stopWaitsForTheTick starts a watch whose first tick blocks, calls stop, and
// reports whether stop returned while the tick was still running. start gets
// the function that blocks the tick: it signals entered and waits for release.
func stopWaitsForTheTick(t *testing.T, start func(block func()) (stop func())) {
	t.Helper()

	var (
		entered = make(chan struct{})
		release = make(chan struct{})
		first   atomic.Bool
	)

	stop := start(func() {
		if first.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	})

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the watch never read its source")
	}

	returned := make(chan struct{})

	go func() {
		stop()
		close(returned)
	}()

	select {
	case <-returned:
		close(release)
		t.Fatal("stop returned while a tick was still reading the source")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return after the tick ended")
	}
}

// blockingLag is a lag source whose reads call block first.
type blockingLag struct{ block func() }

func (s blockingLag) LagWindow(time.Time, int) engine.LagWindow {
	s.block()

	return engine.LagWindow{}
}

// stop returns only once the watch goroutine has: nothing it does touches the
// notice or stderr afterwards.
//
// Ground: concurrency — main stops the watches before it writes to stderr, so a
// stop that returned early would let a tick write after it.
func TestStartLagWatch_StopWaitsForATickInProgress(t *testing.T) {
	stopWaitsForTheTick(t, func(block func()) func() {
		return startLagWatch(t.Context(), blockingLag{block}, time.Millisecond, func(string) {})
	})
}

// Ground: concurrency — as for the lag watch.
func TestStartRefreshWatch_StopWaitsForATickInProgress(t *testing.T) {
	stopWaitsForTheTick(t, func(block func()) func() {
		return startRefreshWatch(t.Context(), func() (grpcsender.RefreshStats, bool) {
			block()

			return grpcsender.RefreshStats{}, true
		}, time.Millisecond, func(string) {})
	})
}

// A source that says it does not refresh ends the watch: it is not asked again.
//
// Ground: contract — "does not refresh" is final, the watch is not a poll for
// the day it starts to.
func TestRefreshWatch_ASourceWithoutRefreshIsNotAskedAgain(t *testing.T) {
	var asked atomic.Int64

	stop := startRefreshWatch(t.Context(), func() (grpcsender.RefreshStats, bool) {
		asked.Add(1)

		return grpcsender.RefreshStats{}, false
	}, 2*time.Millisecond, func(string) {})

	time.Sleep(60 * time.Millisecond)
	stop()

	if n := asked.Load(); n != 1 {
		t.Errorf("the source was asked %d times, want once: the watch ends at the first false", n)
	}
}

// A line that arrives after the view has gone is printed by the delivery
// itself; nothing calls afterView again to rescue it.
//
// Ground: boundary — main calls afterView once, the watch may deliver after it.
func TestLagNotice_ALateLineIsPrintedWithoutAnotherAfterView(t *testing.T) {
	var stderr strings.Builder

	n := &lineNotice{stderr: &stderr, send: func(string) { t.Error("sent into a view that is gone") }}
	n.afterView()
	n.deliver("warning: late")

	if got, want := stderr.String(), "warning: late\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// With a live view the first line is the one held for stderr; a second one
// that arrives while the view is up does not replace it.
//
// Ground: contract — only the first line counts, also when nothing was printed
// yet (the line is the one that is printed later).
func TestRefreshNotice_ALiveViewKeepsTheFirstLine(t *testing.T) {
	var stderr strings.Builder

	n := newRefreshNotice(&stderr, true)
	n.deliver("first")
	n.deliver("second")
	n.afterView()

	if got, want := stderr.String(), "first\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}
