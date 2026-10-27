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

package spike_test

import (
	"context"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/share"
	"github.com/yhgrwav/leettest/pkg/spike"
)

func percent(t *testing.T, written string) share.Percent {
	t.Helper()

	p, err := share.Parse(written, "test")
	if err != nil {
		t.Fatalf("parse %q: %v", written, err)
	}

	return p
}

func one(failed, total int) []engine.Recent {
	return []engine.Recent{{Method: "a.S/One", Failed: failed, Total: total}}
}

// Ground: boundary — the stop condition: failed over total strictly above the
// limit, and at least two failed. Exactly at the limit holds; one failure of
// one call is 100% and holds; the integer comparison is exact where a float
// is not (7 of 1000 at 0.7% is exactly the limit, and 7/1000*100 is a hair
// above 0.7 in floating point).
func TestJudge_Boundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  string
		failed int
		total  int
		trips  bool
	}{
		{"over the limit", "5%", 3, 40, true},
		{"exactly at the limit", "5%", 2, 40, false},
		{"just under", "5%", 2, 41, false},
		{"one failed of one", "5%", 1, 1, false},
		{"one failed of two", "5%", 1, 2, false},
		{"two failed of two", "5%", 2, 2, true},
		{"two failed of a thousand under a fraction", "0.1%", 2, 1000, true},
		{"two failed of two thousand at a fraction", "0.1%", 2, 2000, false},
		{"exactly 0.7% is not above it", "0.7%", 7, 1000, false},
		{"one call over 0.7%", "0.7%", 8, 1000, true},
		{"a very large total", "0.001%", 2001, 200_000_000, true},
		{"a very large total at the limit", "0.001%", 2000, 200_000_000, false},
		{"nothing counted", "5%", 0, 0, false},
		{"all failed, a hundred is off", "100%", 50, 50, false},
	} {
		_, got := spike.Judge(one(tc.failed, tc.total), percent(t, tc.limit))

		if got != tc.trips {
			t.Errorf("%s: %d of %d at %s: trips = %v, want %v", tc.name, tc.failed, tc.total, tc.limit, got, tc.trips)
		}
	}
}

// Ground: contract — the method that tripped is named with its own counts,
// and when several are over the limit the largest share is named, a tie going
// to the first by name: a stop line names one method, deterministically.
func TestJudge_NamesTheMethodOverTheLimit(t *testing.T) {
	window := []engine.Recent{
		{Method: "a.S/Fine", Failed: 0, Total: 500},
		{Method: "b.S/Mild", Failed: 30, Total: 100},
		{Method: "c.S/Worst", Failed: 80, Total: 100},
		{Method: "d.S/Worst", Failed: 80, Total: 100},
		{Method: "e.S/Lone", Failed: 1, Total: 1},
	}

	got, ok := spike.Judge(window, percent(t, "5%"))
	if !ok {
		t.Fatal("no trip with three methods over 5%")
	}

	want := engine.Recent{Method: "c.S/Worst", Failed: 80, Total: 100}
	if got != want {
		t.Errorf("named %+v, want %+v", got, want)
	}
}

// Ground: contract — failures of one method do not borrow the successes of
// another: 3 failed of 4 on one method trips even beside a method with a
// million good calls, which a share over the sum would hide.
func TestJudge_IsPerMethodNotOverTheSum(t *testing.T) {
	window := []engine.Recent{
		{Method: "a.S/Busy", Failed: 0, Total: 1_000_000},
		{Method: "b.S/Rare", Failed: 3, Total: 4},
	}

	got, ok := spike.Judge(window, percent(t, "5%"))

	if !ok || got.Method != "b.S/Rare" {
		t.Errorf("Judge = %+v, %v, want b.S/Rare over the limit", got, ok)
	}
}

// fake is a Source that answers from a script, one entry per tick.
type fake struct {
	windows  [][]engine.Recent
	progress []engine.Progress
	seconds  []int
	// reads and looks count the calls of Recent and of Progress: each answers
	// its own script, so the order a tick asks in does not matter.
	reads, looks int
}

func (f *fake) Recent(dst []engine.Recent, _ time.Time, seconds int) []engine.Recent {
	f.seconds = append(f.seconds, seconds)
	i := min(f.reads, len(f.windows)-1)
	f.reads++

	return append(dst[:0], f.windows[i]...)
}

func (f *fake) Progress(time.Time) engine.Progress {
	i := min(f.looks, len(f.progress)-1)
	f.looks++

	return f.progress[i]
}

// Ground: contract — off is absent: no limit and 100% give no watch at all,
// so a run without the feature has no goroutine and no timer for it.
func TestNew_OffIsNil(t *testing.T) {
	src := &fake{windows: [][]engine.Recent{one(0, 0)}, progress: []engine.Progress{{}}}

	for _, tc := range []struct {
		name  string
		limit share.Percent
	}{
		{"left out", share.Percent{}},
		{"a hundred", percent(t, "100%")},
	} {
		if w := spike.New(src, tc.limit, func(spike.Trip) {}); w != nil {
			t.Errorf("%s: New returned a watch, want nil", tc.name)
		}
	}
}

// Ground: contract — a limit gives a watch.
func TestNew_ALimitGivesAWatch(t *testing.T) {
	src := &fake{windows: [][]engine.Recent{one(0, 0)}, progress: []engine.Progress{{}}}

	if w := spike.New(src, percent(t, "5%"), func(spike.Trip) {}); w == nil {
		t.Error("New returned nil for 5%")
	}
}

func watchOf(t *testing.T, src spike.Source, limit string, trips *[]spike.Trip) *spike.Watch {
	t.Helper()

	w := spike.New(src, percent(t, limit), func(tr spike.Trip) { *trips = append(*trips, tr) })
	if w == nil {
		t.Fatal("New returned nil")
	}

	return w
}

// Ground: contract — the watch asks for the window of five whole seconds.
func TestWatch_ReadsTheWindowOfFiveSeconds(t *testing.T) {
	src := &fake{windows: [][]engine.Recent{one(0, 10)}, progress: []engine.Progress{{}}}

	var trips []spike.Trip

	watchOf(t, src, "5%", &trips).Tick(time.Unix(0, 0))

	if len(src.seconds) != 1 || src.seconds[0] != spike.Window || spike.Window != 5 {
		t.Errorf("Recent read with seconds %v, Window = %d, want one read of 5", src.seconds, spike.Window)
	}
}

// Ground: contract — the trip carries what the stop line prints: the method
// and its counts, the limit as written, the time since the start, the rate
// calls were begun at in the last second (from two readings of Begun) and the
// number in flight; and it is reported once, the watch saying tripped on every
// tick after without calling again.
func TestWatch_TripCarriesTheStopLineAndHappensOnce(t *testing.T) {
	src := &fake{
		windows: [][]engine.Recent{one(0, 100), one(1, 200), one(30, 200), one(60, 200)},
		progress: []engine.Progress{
			{Elapsed: time.Second, Begun: 100, InFlight: 3},
			{Elapsed: 2 * time.Second, Begun: 300, InFlight: 5},
			{Elapsed: 3 * time.Second, Begun: 520, InFlight: 40},
			{Elapsed: 4 * time.Second, Begun: 700, InFlight: 90},
		},
	}

	var trips []spike.Trip

	w := watchOf(t, src, "5%", &trips)

	var tripped []bool
	for range 4 {
		tripped = append(tripped, w.Tick(time.Unix(0, 0)))
	}

	if want := []bool{false, false, true, true}; len(tripped) != 4 || tripped[0] != want[0] || tripped[1] != want[1] ||
		tripped[2] != want[2] || tripped[3] != want[3] {
		t.Errorf("Tick returned %v, want %v", tripped, want)
	}
	if len(trips) != 1 {
		t.Fatalf("onTrip called %d times, want once", len(trips))
	}

	want := spike.Trip{
		Method: "a.S/One", Failed: 30, Total: 200, Limit: percent(t, "5%"),
		At: 3 * time.Second, SentRPS: 220, InFlight: 40,
	}
	if trips[0] != want {
		t.Errorf("Trip = %+v, want %+v", trips[0], want)
	}
}

// Ground: boundary — a run that is fine never trips, however long it is
// watched.
func TestWatch_AFineRunNeverTrips(t *testing.T) {
	src := &fake{windows: [][]engine.Recent{one(1, 1000)}, progress: []engine.Progress{{Elapsed: time.Second, Begun: 10}}}

	var trips []spike.Trip

	w := watchOf(t, src, "5%", &trips)
	for range 10 {
		if w.Tick(time.Unix(0, 0)) {
			t.Fatal("Tick says tripped on 1 failure of 1000")
		}
	}

	if len(trips) != 0 {
		t.Errorf("onTrip called %d times, want 0", len(trips))
	}
}

// Ground: signal — Run ticks by itself, once a second: a trip is found at the
// first or second tick, and Run returns after it. The limit is the test's
// ceiling, not a sleep to wait out.
func TestWatch_RunStopsAfterTheTrip(t *testing.T) {
	src := &fake{windows: [][]engine.Recent{one(50, 100)}, progress: []engine.Progress{{Elapsed: time.Second, Begun: 100}}}

	tripped := make(chan spike.Trip, 1)
	w := spike.New(src, percent(t, "5%"), func(tr spike.Trip) { tripped <- tr })
	if w == nil {
		t.Fatal("New returned nil")
	}

	returned := make(chan struct{})
	go func() {
		w.Run(t.Context())
		close(returned)
	}()

	select {
	case <-tripped:
	case <-time.After(3 * time.Second):
		t.Fatal("no trip within 3 s of a window at 50% against 5%")
	}

	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Error("Run has not returned after the trip")
	}
}

// Ground: signal — a run that ends first ends the watch: no goroutine is left
// behind when the context is done, and nothing is reported.
func TestWatch_RunReturnsWhenTheContextIsDone(t *testing.T) {
	src := &fake{windows: [][]engine.Recent{one(0, 100)}, progress: []engine.Progress{{}}}

	var trips []spike.Trip

	w := watchOf(t, src, "5%", &trips)

	ctx, cancel := context.WithCancel(t.Context())
	returned := make(chan struct{})

	go func() {
		w.Run(ctx)
		close(returned)
	}()

	cancel()

	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("Run has not returned after the context was cancelled")
	}

	if len(trips) != 0 {
		t.Errorf("onTrip called %d times, want 0", len(trips))
	}
}
