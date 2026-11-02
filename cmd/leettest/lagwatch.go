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

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// lagSource is where the early warning reads the generator's window; tests
// replace it to script one.
var lagSource = func(eng *engine.Engine) cli.LagWindowSource { return eng }

// lagTickEvery is how often the warning looks at the window.
var lagTickEvery = time.Second

// startLagWatch looks at src every tick until ctx ends or stop is called, and
// hands deliver the warning line once, the first time the generator is behind.
// stop returns once the watch has.
func startLagWatch(ctx context.Context, src cli.LagWindowSource, every time.Duration, deliver func(line string)) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)

		watch := cli.NewLagWatch(src)
		ticker := time.NewTicker(every)

		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if line, ok := watch.Tick(now); ok {
					deliver(line)
				}
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

// lagNotice carries the warning to the person: on stderr as one line when there
// is no live view, into the view when there is one, and, since the view takes
// the screen with it when it leaves, once more on stderr after it is gone.
type lagNotice struct {
	stderr io.Writer
	// send puts the line into the live view; nil without one.
	send func(line string)

	mu      sync.Mutex
	line    string
	printed bool
	closed  bool
}

// deliver takes the warning line; only the first one counts. A view that has
// already gone gets no send: the line goes to stderr at once.
func (n *lagNotice) deliver(line string) {
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

// afterView is called once the live view is gone; it repeats a warning the view
// showed, once however often it is called.
func (n *lagNotice) afterView() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.closed = true

	if n.send != nil && n.line != "" {
		n.print()
	}
}

// print writes the line to stderr unless it was written. The caller holds mu.
func (n *lagNotice) print() {
	if n.printed {
		return
	}

	n.printed = true

	fmt.Fprintln(n.stderr, n.line)
}
