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
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// startTickWatch calls tick every interval, with the time of the tick, until
// ctx ends, stop is called, or tick returns false. stop returns once the
// goroutine has, so nothing the tick does happens after it.
func startTickWatch(ctx context.Context, every time.Duration, tick func(now time.Time) bool) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(every)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if !tick(now) {
					return
				}
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

// lineNotice carries one line from a watch to the person: on stderr at once when
// there is no live view, into the view when there is one, and, since the view
// takes the screen with it when it leaves, once more on stderr after it is gone.
type lineNotice struct {
	stderr io.Writer
	// send puts the line into the live view; nil when there is no view. A view
	// that shows nothing of the line passes holdForView: the line waits for the
	// view to leave.
	send func(line string)

	mu      sync.Mutex
	line    string
	printed bool
	closed  bool
}

// holdForView is the send of a view that does not show the line itself.
func holdForView(string) {}

// deliver takes the line; only the first one counts. A view that has already
// gone gets no send: the line goes to stderr at once.
func (n *lineNotice) deliver(line string) {
	n.mu.Lock()

	if n.line != "" {
		n.mu.Unlock()

		return
	}

	n.line = line

	if n.send == nil || n.closed {
		n.print()
		n.mu.Unlock()

		return
	}

	n.mu.Unlock()
	n.send(line)
}

// afterView is called once the live view is gone; it prints a line the view
// held or showed, once however often it is called.
func (n *lineNotice) afterView() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.closed = true

	if n.line != "" {
		n.print()
	}
}

// print writes the line to stderr unless it was written. The caller holds mu.
func (n *lineNotice) print() {
	if n.printed {
		return
	}

	n.printed = true

	fmt.Fprintln(n.stderr, n.line)
}
