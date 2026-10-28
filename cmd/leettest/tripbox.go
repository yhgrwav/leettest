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
	trip atomic.Pointer[errstop.Trip]
}

// Trip keeps t and reports whether the watch should go on to print its line
// and stop the engine.
func (b *tripBox) Trip(t errstop.Trip) bool {
	b.trip.Store(&t)

	return true
}

// End is called the moment the engine's Run returns.
func (b *tripBox) End() {}

// Of is the trip the report may tell of: the watch's, when it cut the run
// short.
func (b *tripBox) Of(report engine.Report) *errstop.Trip {
	return b.trip.Load()
}
