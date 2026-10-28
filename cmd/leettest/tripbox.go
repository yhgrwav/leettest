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
// goroutine and may decide a moment after the run is over; such a decision
// must not say a run that finished by itself was stopped.
type tripBox struct {
	trip  atomic.Pointer[errstop.Trip]
	ended atomic.Bool
}

// Trip keeps t and reports whether the watch should go on to print its line
// and stop the engine. After End it keeps nothing and says no.
func (b *tripBox) Trip(t errstop.Trip) bool {
	if b.ended.Load() {
		return false
	}

	b.trip.Store(&t)

	return true
}

// End is called the moment the engine's Run returns.
func (b *tripBox) End() {
	b.ended.Store(true)
}

// Of is the trip the report may tell of: the watch's, when it cut the run
// short. The engine marks a report incomplete only when the stop reached the
// dispatcher; a trip that came with the last calls already out, or between
// End and the watch being cancelled, stopped nothing.
func (b *tripBox) Of(report engine.Report) *errstop.Trip {
	if !report.Incomplete {
		return nil
	}

	return b.trip.Load()
}
