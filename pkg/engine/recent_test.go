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
// success, the client's own and the target's. An abort and a call that never
// went out, whatever kept it back (the generator, the stream, the connection
// or a cause nobody set), say nothing of the target and are in neither count.
// The unclassified failure that did go out is a failure, and the unsent call
// of unknown cause must not be mixed into it: they are different cells.
//
// Each case is recorded beside one good call of the same method, so that "in
// neither count" reads as 1 call and 0 failed, not as an empty answer.
func TestRecent_WhichCategoriesFail(t *testing.T) {
	for _, tc := range []struct {
		name     string
		category Category
		notSent  bool
		cause    Blocker
		failed   int
		total    int // of the case's call; the good call beside it is added
	}{
		{"success", CategorySuccess, false, 0, 0, 1},
		{"server fault", CategoryServerFault, false, 0, 1, 1},
		{"client fault", CategoryClientFault, false, 0, 1, 1},
		{"overload", CategoryOverload, false, 0, 1, 1},
		{"timeout that went out", CategoryTimeout, false, 0, 1, 1},
		{"unreachable", CategoryUnreachable, false, 0, 1, 1},
		{"cut off", CategoryCutOff, false, 0, 1, 1},
		{"client error", CategoryClientError, false, 0, 1, 1},
		{"bad response", CategoryBadResponse, false, 0, 1, 1},
		{"unclassified, went out", CategoryUnknown, false, 0, 1, 1},
		{"aborted", CategoryAborted, false, 0, 0, 0},
		{"not sent, held by the generator", CategoryTimeout, true, BlockedOnGenerator, 0, 0},
		{"not sent, held by the stream", CategoryTimeout, true, BlockedOnStream, 0, 0},
		{"not sent, held by the connection", CategoryTimeout, true, BlockedOnConnection, 0, 0},
		{"not sent, cause unknown", CategoryTimeout, true, BlockedUnknown, 0, 0},
	} {
		start := time.Unix(1_000, 0)
		stats := reserved(start, 0)

		r := finishing(start, "a", 0, time.Second, tc.category)
		r.NotSent = tc.notSent
		r.NotSentOn = tc.cause
		stats.Record(r)
		stats.Record(finishing(start, "a", 0, time.Second, CategorySuccess))

		got := recentOf(t, stats, start, 3*time.Second, 5)

		var failed, total int
		for _, m := range got {
			failed += m.Failed
			total += m.Total
		}
		if failed != tc.failed || total != tc.total+1 {
			t.Errorf("%s: failed %d of %d, want %d of %d", tc.name, failed, total, tc.failed, tc.total+1)
		}
	}
}

// Ground: contract — the report's seconds are unchanged by keeping an unsent
// call of unknown cause apart from an unclassified one: both are still
// «Unclassified» in the export, and neither is left counted as in flight.
// (Green on the stub: guards the format of the report and of the JSON while
// the cell is split.)
func TestRecent_UnsentOfUnknownCauseStaysUnclassifiedInTheReport(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := reserved(start, 0)

	unsent := finishing(start, "a", 0, time.Second, CategoryTimeout)
	unsent.NotSent = true
	unsent.NotSentOn = BlockedUnknown
	stats.Record(unsent)
	stats.Record(finishing(start, "a", 0, time.Second, CategoryUnknown))

	secs := seconds(t, stats)

	var unclassified int
	for _, s := range secs {
		unclassified += s.Unclassified
	}
	if unclassified != 2 {
		t.Errorf("Unclassified over the report = %d, want 2: the unsent call and the unclassified one", unclassified)
	}
	if last := secs[len(secs)-1]; last.InFlight != 0 {
		t.Errorf("InFlight at the end = %d, want 0: both calls finished", last.InFlight)
	}
}

// Ground: boundary — the edges of the array the reader walks under the lock
// answer, never panic and never read what was not reserved: a now past the
// reserved span reads no more than was reserved; a now before the start, a
// Recent before Start, and a window of zero or less seconds give nothing; a
// window longer than the run is the run.
func TestRecent_TheEdgesOfTheTimeline(t *testing.T) {
	start := time.Unix(1_000, 0)

	t.Run("now past the reserved span", func(t *testing.T) {
		stats := reserved(start, 0)
		stats.Record(finishing(start, "a", 0, 59*time.Minute+58*time.Second, CategoryServerFault))
		stats.Record(finishing(start, "a", 0, 59*time.Minute+59*time.Second, CategorySuccess))

		// Two hours in the reserve is an hour: the last whole seconds of it are
		// not 5 s before «now», and nothing past the end exists to be read.
		got := recentOf(t, stats, start, 2*time.Hour, 5)

		for _, r := range got {
			if r.Total > 2 || r.Failed > r.Total {
				t.Errorf("read beyond what was recorded: %+v", r)
			}
		}
		if len(got) != 1 || got[0].Method != "a" || got[0].Total == 0 {
			t.Errorf("now two hours in on an hour reserve: %+v, want the method with the last seconds it holds", got)
		}
	})

	t.Run("now before the start", func(t *testing.T) {
		stats := reserved(start, 0)
		stats.Record(finishing(start, "a", 0, time.Second, CategoryServerFault))

		recentFor(t, recentOf(t, stats, start, 3*time.Second, 5), "a") // the call is there to be read

		got := recentOf(t, stats, start, -10*time.Second, 5)

		if len(got) != 0 {
			t.Errorf("a now before the start gives %+v, want nothing", got)
		}
	})

	t.Run("before Start", func(t *testing.T) {
		stats := NewStats()

		if got := stats.Recent(nil, start, 5); len(got) != 0 {
			t.Errorf("Recent on stats that never started gives %+v, want nothing", got)
		}
	})

	t.Run("before Start after Reserve", func(t *testing.T) {
		stats := NewStats()
		stats.Reserve(time.Hour)

		if got := stats.Recent(nil, start, 5); len(got) != 0 {
			t.Errorf("Recent on reserved stats that never started gives %+v, want nothing", got)
		}
	})

	for _, seconds := range []int{0, -1, -1 << 30} {
		t.Run(fmt.Sprintf("seconds %d", seconds), func(t *testing.T) {
			stats := reserved(start, 0)
			stats.Record(finishing(start, "a", 0, time.Second, CategoryServerFault))

			recentFor(t, recentOf(t, stats, start, 3*time.Second, 5), "a") // the call is there to be read

			if got := recentOf(t, stats, start, 3*time.Second, seconds); len(got) != 0 {
				t.Errorf("a window of %d seconds gives %+v, want nothing", seconds, got)
			}
		})
	}

	t.Run("a window longer than the run", func(t *testing.T) {
		stats := reserved(start, 0)
		stats.Record(finishing(start, "a", 0, time.Second, CategoryServerFault))
		stats.Record(finishing(start, "a", 0, 2*time.Second, CategorySuccess))

		got := recentFor(t, recentOf(t, stats, start, 3*time.Second, 1<<30), "a")

		if got.Failed != 1 || got.Total != 2 {
			t.Errorf("window of 2^30 seconds: failed %d of %d, want 1 of 2", got.Failed, got.Total)
		}
	})
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
// worth of time, so the window alone says nothing of them. Progress is what
// the stop line prints beside it: the number in flight, which is honest (a
// call leaves it before it is recorded, so it is a floor, never an excess),
// and the time since the start. There is no count of calls begun: the
// engine keeps none, and a send loop does not gain one for a line.
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
	if got.Elapsed <= 0 {
		t.Errorf("Elapsed = %v, want the time since the run started", got.Elapsed)
	}
	if after := eng.Progress(time.Now()); after.InFlight != 0 {
		t.Errorf("InFlight = %d after the run, want 0", after.InFlight)
	}
}
