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
	"io"
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
func startRefreshWatch(_ context.Context, _ refreshSource, _ time.Duration, _ func(line string)) (stop func()) {
	return func() {}
}

// refreshNotice carries the failed-refresh line to stderr: at once without a
// live view, after the view has gone with one.
type refreshNotice struct {
	stderr io.Writer
	// live is true while a live view owns the screen.
	live bool
}

// deliver takes the line; only the first one counts.
func (n *refreshNotice) deliver(_ string) {}

// afterView is called once the live view is gone.
func (n *refreshNotice) afterView() {}
