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

package engine

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// finishing is a call of method begun at begun and finished at done, both
// offsets from start.
func finishing(start time.Time, method string, begun, done time.Duration, category Category) Result {
	r := call(start, begun, done, category)
	r.Method = method

	return r
}

func recentOf(t *testing.T, stats *Stats, start time.Time, at time.Duration, seconds int) []Recent {
	t.Helper()

	return stats.Recent(nil, start.Add(at), seconds)
}

func recentFor(t *testing.T, got []Recent, method string) Recent {
	t.Helper()

	for _, r := range got {
		if r.Method == method {
			return r
		}
	}

	t.Fatalf("Recent has no %s: %+v", method, got)

	return Recent{}
}

// Ground: boundary — the window is the last N whole seconds before now, by
// the second a call finished in: a call that began long ago and finished
// inside is in, one that finished before the window is out, and so is the
// second now is in, which is not whole yet.
func TestRecent_WindowIsTheLastWholeSecondsByCompletion(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := reserved(start, 0)

	// now is 8.5 s in: whole seconds 3 to 7 are the window of 5.
	stats.Record(finishing(start, "a", 0, 2900*time.Millisecond, CategoryServerFault))             // second 2: before
	stats.Record(finishing(start, "a", 0, 3*time.Second, CategoryServerFault))                     // second 3: the first of the window, begun long before
	stats.Record(finishing(start, "a", 6*time.Second, 7999*time.Millisecond, CategorySuccess))     // second 7: the last of the window
	stats.Record(finishing(start, "a", 7*time.Second, 8*time.Second, CategoryServerFault))         // second 8: not whole
	stats.Record(finishing(start, "a", 8*time.Second, 8400*time.Millisecond, CategoryServerFault)) // second 8: not whole

	got := recentFor(t, recentOf(t, stats, start, 8500*time.Millisecond, 5), "a")

	if got.Failed != 1 || got.Total != 2 {
		t.Errorf("window 3..7: failed %d of %d, want 1 of 2", got.Failed, got.Total)
	}
}

// Ground: boundary — before the window is full the window is the seconds
// there are, so a spike in the first seconds of a run is seen at once, not
// after five of them.
func TestRecent_EarlyInTheRunTheWindowIsWhatThereIs(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := reserved(start, 0)

	stats.Record(finishing(start, "a", 0, 100*time.Millisecond, CategoryServerFault))
	stats.Record(finishing(start, "a", 0, 1200*time.Millisecond, CategorySuccess))

	got := recentFor(t, recentOf(t, stats, start, 2500*time.Millisecond, 5), "a")

	if got.Failed != 1 || got.Total != 2 {
		t.Errorf("2 whole seconds in, window 5: failed %d of %d, want 1 of 2", got.Failed, got.Total)
	}
}

// Ground: contract — ramp and warm-up seconds are in the window: a target
// that falls over on the way up is what a stop is for, and the report's own
// totals leaving warm-up out do not change that.
func TestRecent_WarmupSecondsAreInTheWindow(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := reserved(start, 4*time.Second)

	stats.Record(finishing(start, "a", 0, time.Second, CategoryOverload))
	stats.Record(finishing(start, "a", 0, 2*time.Second, CategorySuccess))

	got := recentFor(t, recentOf(t, stats, start, 5*time.Second, 5), "a")

	if got.Failed != 1 || got.Total != 2 {
		t.Errorf("during warm-up: failed %d of %d, want 1 of 2", got.Failed, got.Total)
	}
}

// Ground: contract — which categories are failures: every one that is not a
// success, the client's own and the target's. An abort and a timeout whose
// request never went out say nothing of the target and are in neither count.
func TestRecent_WhichCategoriesFail(t *testing.T) {
	for _, tc := range []struct {
		name     string
		category Category
		notSent  bool
		failed   int
		total    int
	}{
		{"success", CategorySuccess, false, 0, 1},
		{"server fault", CategoryServerFault, false, 1, 1},
		{"client fault", CategoryClientFault, false, 1, 1},
		{"overload", CategoryOverload, false, 1, 1},
		{"timeout that went out", CategoryTimeout, false, 1, 1},
		{"unreachable", CategoryUnreachable, false, 1, 1},
		{"cut off", CategoryCutOff, false, 1, 1},
		{"client error", CategoryClientError, false, 1, 1},
		{"bad response", CategoryBadResponse, false, 1, 1},
		{"unclassified", CategoryUnknown, false, 1, 1},
		{"aborted", CategoryAborted, false, 0, 0},
		{"timeout that never went out", CategoryTimeout, true, 0, 0},
	} {
		start := time.Unix(1_000, 0)
		stats := reserved(start, 0)

		r := finishing(start, "a", 0, time.Second, tc.category)
		r.NotSent = tc.notSent
		stats.Record(r)

		got := recentOf(t, stats, start, 3*time.Second, 5)

		var failed, total int
		for _, m := range got {
			failed += m.Failed
			total += m.Total
		}
		if failed != tc.failed || total != tc.total {
			t.Errorf("%s: failed %d of %d, want %d of %d", tc.name, failed, total, tc.failed, tc.total)
		}
	}
}

// Ground: contract — counts are per method, the method named, in the order
// of the names: a stop reads one method's share and says which, and a method
// that is fine does not dilute one that is not.
func TestRecent_IsPerMethodByName(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := reserved(start, 0)

	stats.Record(finishing(start, "b.S/Two", 0, time.Second, CategorySuccess))
	stats.Record(finishing(start, "b.S/Two", 0, time.Second, CategorySuccess))
	stats.Record(finishing(start, "a.S/One", 0, time.Second, CategoryServerFault))
	stats.Record(finishing(start, "a.S/One", 0, time.Second, CategorySuccess))

	got := recentOf(t, stats, start, 3*time.Second, 5)

	want := []Recent{{Method: "a.S/One", Failed: 1, Total: 2}, {Method: "b.S/Two", Failed: 0, Total: 2}}
	if !slices.Equal(got, want) {
		t.Errorf("Recent = %+v, want %+v", got, want)
	}
}

// Ground: hot path — the monitor reads once a second for the whole run and
// must not make garbage in the process or stall recording behind a long
// copy: with a dst that has room it allocates nothing.
func TestRecent_WithRoomInDstAllocatesNothing(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := reserved(start, 0)

	for m := range 4 {
		stats.Record(finishing(start, fmt.Sprintf("a.S/M%d", m), 0, time.Second, CategorySuccess))
	}

	dst := make([]Recent, 0, 8)
	if got := stats.Recent(dst, start.Add(3*time.Second), 5); len(got) != 4 {
		t.Fatalf("Recent has %d methods, want 4", len(got))
	}

	allocs := testing.AllocsPerRun(100, func() {
		dst = stats.Recent(dst, start.Add(3*time.Second), 5)
	})
	if allocs != 0 {
		t.Errorf("Recent allocates %v times per read, want 0", allocs)
	}
}

// Ground: concurrency — workers record while the monitor reads, under -race;
// once they are done every call is in the window, none twice, none lost.
func TestRecent_ReadsWhileWorkersRecord(t *testing.T) {
	const workers, each = 8, 500

	start := time.Unix(1_000, 0)
	stats := reserved(start, 0)

	var wg sync.WaitGroup

	stop := make(chan struct{})
	readerDone := make(chan struct{})

	go func() {
		defer close(readerDone)

		var dst []Recent
		for {
			select {
			case <-stop:
				return
			default:
				dst = stats.Recent(dst, start.Add(4*time.Second), 5)
			}
		}
	}()

	for range workers {
		wg.Go(func() {
			for i := range each {
				category := CategorySuccess
				if i%4 == 0 {
					category = CategoryServerFault
				}
				stats.Record(finishing(start, "a", 0, time.Duration(i%4)*time.Second, category))
			}
		})
	}

	wg.Wait()
	close(stop)
	<-readerDone

	got := recentFor(t, recentOf(t, stats, start, 4*time.Second, 5), "a")

	if got.Total != workers*each || got.Failed != workers*each/4 {
		t.Errorf("failed %d of %d, want %d of %d", got.Failed, got.Total, workers*each/4, workers*each)
	}
}

// Ground: signal — a run whose calls hang records nothing for a timeout's
// worth of time, so the completed counters say 0 sent. Progress adds the
// calls in flight to those finished, and it is what a stop line prints as
// the rate and the number in flight.
func TestProgress_CountsCallsThatHang(t *testing.T) {
	h := newHoldingSender()
	eng := stopEngine(t, h, time.Minute)
	done := runAsync(t.Context(), eng)

	waitEntered(t, h, 10)

	got := eng.Progress(time.Now())

	eng.Stop()
	close(h.release)

	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}

	if got.InFlight < 10 {
		t.Errorf("InFlight = %d, want at least the 10 calls held in the sender", got.InFlight)
	}
	if got.Begun < got.InFlight {
		t.Errorf("Begun = %d below InFlight = %d: every call in flight has begun", got.Begun, got.InFlight)
	}
	if got.Elapsed <= 0 {
		t.Errorf("Elapsed = %v, want the time since the run started", got.Elapsed)
	}
}

// Ground: signal — Begun never goes back: a call that finishes moves from
// the in-flight part to the finished part, and the sum stays. Two readings
// a second apart are a rate only if the second is never below the first.
func TestProgress_BegunDoesNotDropWhenCallsFinish(t *testing.T) {
	h := newHoldingSender()
	eng := stopEngine(t, h, time.Minute)
	done := runAsync(t.Context(), eng)

	waitEntered(t, h, 10)

	before := eng.Progress(time.Now())

	eng.Stop()
	close(h.release)

	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}

	after := eng.Progress(time.Now())

	if before.Begun == 0 {
		t.Fatal("Begun = 0 with calls held in the sender")
	}
	if after.Begun < before.Begun {
		t.Errorf("Begun fell from %d to %d when the calls finished", before.Begun, after.Begun)
	}
	if after.InFlight != 0 {
		t.Errorf("InFlight = %d after the run, want 0", after.InFlight)
	}
}
