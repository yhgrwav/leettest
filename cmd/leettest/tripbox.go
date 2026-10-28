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
	"sync/atomic"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/errstop"
)

// tripBox is where the stop watch leaves its trip. The watch ticks on its own
// goroutine and may decide while the last calls are drained, or a moment after
// the run is over; such a decision must not say a run that finished by itself
// was stopped. The first cause of a stop wins: a run a person stopped, whose
// drain then fills the window, was not stopped by the watch.
type tripBox struct {
	first atomic.Pointer[stopCause]
}

// stopCause is who stopped the run first; trip is nil for anyone but the watch.
type stopCause struct {
	trip *errstop.Trip
}

// Trip keeps t unless the run was already stopped by something else, and says
// whether it did: only then is the watch's stop to be passed on to the engine.
func (b *tripBox) Trip(t errstop.Trip) bool {
	return b.first.CompareAndSwap(nil, &stopCause{trip: &t})
}

// Other is called when something else than the watch stops the run (Ctrl-C,
// SIGTERM), before it acts. A trip that comes later is not the run's stop.
func (b *tripBox) Other() {
	b.first.CompareAndSwap(nil, &stopCause{})
}

// Of is the trip the report may tell of: the watch's, when it cut the run
// short and was the first to. The engine marks a report incomplete only when
// the stop reached the dispatcher; a trip that came with the last calls
// already out, or after Run returned, stopped nothing. It is the one place
// that decides, so the stop line, the text report, the JSON and the exit code
// agree.
func (b *tripBox) Of(report engine.Report) *errstop.Trip {
	if !report.Incomplete {
		return nil
	}

	if c := b.first.Load(); c != nil {
		return c.trip
	}

	return nil
}
