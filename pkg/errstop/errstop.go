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

package errstop

import (
	"context"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/share"
)

const (
	// Window is how many whole seconds the share is taken over.
	Window = 5
	// MinFailed is how many failed calls a method needs in the window before
	// its share can stop the run: one failure of one call is 100% and says
	// nothing.
	MinFailed = 2
)

// Source is what the watch reads of a run; *engine.Engine is one.
type Source interface {
	Recent(dst []engine.Recent, now time.Time, seconds int) []engine.Recent
	Progress(now time.Time) engine.Progress
}

// Trip is the moment the watch stopped a run, for the stop line and the JSON.
type Trip struct {
	// Method, Failed and Total are the method whose share passed the limit
	// and its counts over the window.
	Method string
	Failed int
	Total  int
	Limit  share.Percent
	// At is the time since the run started.
	At time.Duration
	// InFlight is the calls begun and not finished at that moment: on a stand
	// that hangs, the number the window cannot show.
	InFlight int
}

// Judge returns the method of window whose share of failed calls is over
// limit, and whether there is one: failed over total strictly above the
// limit, and failed at least MinFailed. Of several the largest share wins, a
// tie goes to the first by name. The comparison is integer.
func Judge(_ []engine.Recent, _ share.Percent) (engine.Recent, bool) {
	return engine.Recent{}, false
}

// Watch judges a run once a tick.
type Watch struct{}

// New returns the watch of src for limit, calling onTrip once, on the first
// tick that trips. It returns nil when there is nothing to watch: no limit
// (zero) or 100%, which no share is above.
func New(_ Source, _ share.Percent, _ func(Trip)) *Watch {
	return nil
}

// Tick reads src at now and judges it; true once the watch has tripped, then
// and on every later tick, without calling onTrip again.
func (w *Watch) Tick(_ time.Time) bool {
	return false
}

// Run ticks every second until ctx is done or the watch trips.
func (w *Watch) Run(_ context.Context) {}
