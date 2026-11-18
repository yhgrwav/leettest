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

	"github.com/yhgrwav/leettest/pkg/grpcsender"
)

// refreshSource is where the refresh watch reads the sender's counters:
// grpcsender.Sender.MetadataRefresh, or a script in a test. The bool is false
// when the sender does not refresh.
type refreshSource func() (grpcsender.RefreshStats, bool)

// startRefreshWatch polls src every tick until ctx ends or stop is called, and
// hands deliver the stderr line once, at the first failed read. stop returns
// once the watch has.
func startRefreshWatch(ctx context.Context, src refreshSource, every time.Duration, deliver func(line string)) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(every)
		defer ticker.Stop()

		delivered := false

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				st, ok := src()
				if !ok {
					return
				}

				if st.Failures > 0 && !delivered {
					delivered = true

					deliver("metadata refresh failed: " + st.LastError +
						"; the last good value stays in use (the report counts every failed read)")
				}
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

// refreshNotice carries the failed-refresh line to stderr: at once without a
// live view, after the view has gone with one.
type refreshNotice struct {
	stderr io.Writer
	// live is true while a live view owns the screen.
	live bool

	mu      sync.Mutex
	line    string
	printed bool
	closed  bool
}

// deliver takes the line; only the first one counts.
func (n *refreshNotice) deliver(line string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.line != "" {
		return
	}

	n.line = line

	if !n.live || n.closed {
		n.print()
	}
}

// afterView is called once the live view is gone.
func (n *refreshNotice) afterView() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.closed = true

	if n.line != "" {
		n.print()
	}
}

// print writes the line to stderr unless it was written. The caller holds mu.
func (n *refreshNotice) print() {
	if n.printed {
		return
	}

	n.printed = true

	fmt.Fprintln(n.stderr, n.line)
}
