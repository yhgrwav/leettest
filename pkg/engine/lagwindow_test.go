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
	"sync"
	"testing"
	"time"
)

// lwCall is a call of method a scheduled at an offset from start and begun lag
// after that, finished a millisecond later and answered.
func lwCall(start time.Time, at, lag time.Duration) Result {
	scheduled := start.Add(at)
	begun := scheduled.Add(lag)

	return Result{
		Method:      "a",
		ScheduledAt: scheduled,
		BegunAt:     begun,
		Outcome:     Outcome{SentAt: begun, DoneAt: begun.Add(time.Millisecond), Category: CategorySuccess},
	}
}

// lwUnsent is a call that never went out: its deadline came budget after the
// schedule and it is done at that deadline, with the blocker the sender named.
func lwUnsent(start time.Time, at, lag, budget time.Duration, blocker Blocker) Result {
	scheduled := start.Add(at)
	begun := scheduled.Add(lag)
	deadline := scheduled.Add(budget)

	return Result{
		Method:      "a",
		ScheduledAt: scheduled,
		BegunAt:     begun,
		Deadline:    deadline,
		Outcome: Outcome{
			SentAt: begun, DoneAt: deadline, Category: CategoryTimeout,
			NotSent: true, NotSentOn: blocker,
		},
	}
}

// lwStats is a run of a minute started at start, with the given warm-up.
func lwStats(start time.Time, warmup time.Duration) *Stats {
	stats := NewStats()
	stats.Reserve(time.Minute, "a")
	stats.Start(start, warmup)

	return stats
}

func lwRead(stats *Stats, start time.Time, at time.Duration, seconds int) LagWindow {
	return stats.LagWindow(start.Add(at), seconds)
}

// The rule is stated on its own, not through the rank formula of the percentile
// rule (#73): p99 of the window's start lag lies above the floor when more than
// 1% of the starts were over it (late*100 > starts), and a call the generator
// left unsent counts with no floor under it.
//
// Ground: boundary — the floor of 100 starts (metrics.MinCount(0.99)), the rank
// of p99 (1 late of 100 is the maximum's business, 2 are p99's), and an unsent
// call among ten; a run of this size is not driven to the call by an
// end-to-end test.
func TestLagWindow_BehindTable(t *testing.T) {
	for _, c := range []struct {
		name                 string
		starts, late, unsent int
		want                 bool
	}{
		{"99 starts, all late: under the floor of 100", 99, 99, 0, false},
		{"100 starts, all late", 100, 100, 0, true},
		{"100 starts, 2 late: p99 is over the floor", 100, 2, 0, true},
		{"100 starts, 1 late: the maximum only", 100, 1, 0, false},
		{"100 starts, none late", 100, 0, 0, false},
		{"101 starts, 1 late", 101, 1, 0, false},
		{"101 starts, 2 late", 101, 2, 0, true},
		{"200 starts, 2 late: rank 198 of 200 needs 3", 200, 2, 0, false},
		{"200 starts, 3 late", 200, 3, 0, true},
		{"1000 starts, 10 late: exactly 1%", 1000, 10, 0, false},
		{"1000 starts, 11 late", 1000, 11, 0, true},
		{"60000 starts, 600 late: exactly 1%", 60000, 600, 0, false},
		{"60000 starts, 601 late", 60000, 601, 0, true},
		{"the 2140 of 60000 of the line", 60000, 2140, 0, true},
		{"no lag at all", 60000, 0, 0, false},
		{"one unsent by the generator among ten, no late", 10, 0, 1, true},
		{"one unsent by the generator among a million, no late", 1_000_000, 0, 1, true},
		{"one unsent and nothing else", 0, 0, 1, true},
		{"unsent and under the floor of starts", 99, 99, 1, true},
		{"nothing at all", 0, 0, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := LagWindow{Seconds: 5, Floor: time.Millisecond, Starts: c.starts, Late: c.late, NotSentGenerator: c.unsent}
			if got := w.Behind(); got != c.want {
				t.Errorf("Behind() of %d starts, %d late, %d unsent = %v, want %v", c.starts, c.late, c.unsent, got, c.want)
			}
		})
	}
}

// A warm-up call is counted: the record of lag returns before the measured
// counters for it, and the window must not.
//
// Ground: contract — LagWindow is the public reader of the counters.
func TestLagWindow_WarmupCallsAreCounted(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := lwStats(start, 2*time.Second)

	stats.Record(lwCall(start, 500*time.Millisecond, 5*time.Millisecond))
	stats.Record(lwUnsent(start, 1500*time.Millisecond, 5*time.Millisecond, 2*time.Second, BlockedOnGenerator))

	// The unsent call is done at its deadline, 3.5 s: second 3 of the window.
	w := lwRead(stats, start, 4500*time.Millisecond, 5)

	if w.Starts != 2 || w.Late != 2 || w.NotSentGenerator != 1 {
		t.Errorf("window of the warm-up: %d starts, %d late, %d unsent by the generator; want 2, 2, 1", w.Starts, w.Late, w.NotSentGenerator)
	}
}

// The window flag says where the window is. It is the warm-up while the window
// ends inside it; the first window that ends past it is the measured part,
// with the warm-up seconds still in its counts.
//
// Ground: boundary — the second the warm-up ends in, on explicit moments.
func TestLagWindow_WarmupFlagFollowsTheEndOfTheWindow(t *testing.T) {
	start := time.Unix(1_000, 0)

	for _, c := range []struct {
		name   string
		warmup time.Duration
		at     time.Duration
		want   bool
	}{
		{"window ends in the first second of 2 s", 2 * time.Second, 1500 * time.Millisecond, true},
		{"window ends exactly where the warm-up does", 2 * time.Second, 2500 * time.Millisecond, true},
		{"window ends one second past", 2 * time.Second, 3500 * time.Millisecond, false},
		{"warm-up of 1.5 s, window ends at 1 s", 1500 * time.Millisecond, 1900 * time.Millisecond, true},
		{"warm-up of 1.5 s, window ends at 2 s", 1500 * time.Millisecond, 2100 * time.Millisecond, false},
		{"no warm-up, first second", 0, 1500 * time.Millisecond, false},
		{"no warm-up, later", 0, 30 * time.Second, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			stats := lwStats(start, c.warmup)
			stats.Record(lwCall(start, 100*time.Millisecond, 5*time.Millisecond))

			if got := lwRead(stats, start, c.at, 5).Warmup; got != c.want {
				t.Errorf("Warmup at %v of a %v warm-up = %v, want %v", c.at, c.warmup, got, c.want)
			}
		})
	}
}

// The first window past the warm-up still holds its calls: the counts are the
// last five seconds of the run, not the measured part.
//
// Ground: contract — the window reads seconds, whatever the report leaves out.
func TestLagWindow_APastWarmupWindowKeepsTheWarmupSeconds(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := lwStats(start, 2*time.Second)

	stats.Record(lwCall(start, 500*time.Millisecond, 5*time.Millisecond))
	stats.Record(lwCall(start, 3500*time.Millisecond, 0))

	w := lwRead(stats, start, 4500*time.Millisecond, 5)

	if w.Starts != 2 || w.Late != 1 || w.Warmup {
		t.Errorf("window at 4.5 s: %d starts, %d late, warm-up %v; want 2, 1, false", w.Starts, w.Late, w.Warmup)
	}
}

// The window is the last whole seconds before now, by the second a call was
// done in. A call of second 1 is in a window read in second 6 and out of one
// read in second 7; the second now is in is not whole and is left out.
//
// Ground: boundary — a second slides out of a window; no end-to-end test hits
// the second to the millisecond.
func TestLagWindow_AStartLeavesTheWindowFiveSecondsLater(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := lwStats(start, 0)

	stats.Record(lwCall(start, 1200*time.Millisecond, 5*time.Millisecond))
	stats.Record(lwCall(start, 6200*time.Millisecond, 5*time.Millisecond))

	for _, c := range []struct {
		at         time.Duration
		wantStarts int
	}{
		{1500 * time.Millisecond, 0},
		{2500 * time.Millisecond, 1},
		{6500 * time.Millisecond, 1},
		{7500 * time.Millisecond, 1},
		{11500 * time.Millisecond, 1},
		{12500 * time.Millisecond, 0},
	} {
		w := lwRead(stats, start, c.at, 5)
		if w.Starts != c.wantStarts {
			t.Errorf("read at %v: %d starts in the window, want %d", c.at, w.Starts, c.wantStarts)
		}
	}

	if w := lwRead(stats, start, 6500*time.Millisecond, 5); w.Seconds != 5 || w.End != 6*time.Second {
		t.Errorf("window at 6.5 s covers %d s ending at %v, want 5 s ending at 6s", w.Seconds, w.End)
	}
}

// With no warm-up the window starts with the run: early on it is the seconds
// there are, and the count of seconds says how many that is.
//
// Ground: boundary — fewer seconds into the run than the window is long.
func TestLagWindow_WithNoWarmupTheWindowStartsWithTheRun(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := lwStats(start, 0)

	stats.Record(lwCall(start, 200*time.Millisecond, 5*time.Millisecond))
	stats.Record(lwCall(start, 1200*time.Millisecond, 5*time.Millisecond))
	stats.Record(lwCall(start, 2200*time.Millisecond, 5*time.Millisecond))

	for _, c := range []struct {
		at                   time.Duration
		wantSeconds, wantAll int
	}{
		{1500 * time.Millisecond, 1, 1},
		{2500 * time.Millisecond, 2, 2},
		{3500 * time.Millisecond, 3, 3},
	} {
		w := lwRead(stats, start, c.at, 5)
		if w.Seconds != c.wantSeconds || w.Starts != c.wantAll || w.Warmup {
			t.Errorf("read at %v: %d s, %d starts, warm-up %v; want %d s, %d starts, false",
				c.at, w.Seconds, w.Starts, w.Warmup, c.wantSeconds, c.wantAll)
		}
	}
}

// A call is late when its start lag is over the floor of the run, the one the
// call is told to have waited by; a wait of exactly the floor is not.
//
// Ground: boundary — T-1ns, T and T+1ns of the floor, as for the waits.
func TestLagWindow_LateIsOverTheFloorOfTheRun(t *testing.T) {
	start := time.Unix(1_000, 0)

	for _, c := range []struct {
		name     string
		floor    time.Duration
		lag      time.Duration
		wantLate int
	}{
		{"default floor, lag a nanosecond under", 0, time.Millisecond - time.Nanosecond, 0},
		{"default floor, lag exactly the floor", 0, time.Millisecond, 0},
		{"default floor, lag a nanosecond over", 0, time.Millisecond + time.Nanosecond, 1},
		{"floor of 4 ms, lag 3 ms", 4 * time.Millisecond, 3 * time.Millisecond, 0},
		{"floor of 4 ms, lag exactly 4 ms", 4 * time.Millisecond, 4 * time.Millisecond, 0},
		{"floor of 4 ms, lag 5 ms", 4 * time.Millisecond, 5 * time.Millisecond, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			stats := lwStats(start, 0)
			if c.floor > 0 {
				stats.SetWaitFloor(c.floor)
			}
			stats.Record(lwCall(start, 500*time.Millisecond, c.lag))

			w := lwRead(stats, start, 1500*time.Millisecond, 5)

			wantFloor := c.floor
			if wantFloor == 0 {
				wantFloor = StreamWaitFloor
			}
			if w.Late != c.wantLate || w.Starts != 1 || w.Floor != wantFloor {
				t.Errorf("window: %d starts, %d late, floor %v; want 1, %d, %v", w.Starts, w.Late, w.Floor, c.wantLate, wantFloor)
			}
		})
	}
}

// Only the generator's own unsent calls count as unsent in the window; one the
// stream or the connection held back says nothing of the generator. Every call
// with a start on record is a start, unsent or not, as the start lag of the
// report is.
//
// Ground: contract — the same cause as the report's NotSentGenerator.
func TestLagWindow_UnsentCountsOnlyWhatTheGeneratorHeldBack(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := lwStats(start, 0)

	// Each unsent call is done at its deadline a second after the schedule, in
	// second 1; a sixth, unsent by the stream, begun 600 ms late against a
	// budget of 1 s, was held back by the generator for more than it had left
	// (the timing of notSentCause), whatever the sender named.
	stats.Record(lwUnsent(start, 100*time.Millisecond, 0, time.Second, BlockedOnGenerator))
	stats.Record(lwUnsent(start, 200*time.Millisecond, 0, time.Second, BlockedOnStream))
	stats.Record(lwUnsent(start, 300*time.Millisecond, 0, time.Second, BlockedOnConnection))
	stats.Record(lwUnsent(start, 400*time.Millisecond, 0, time.Second, BlockedUnknown))
	stats.Record(lwCall(start, 500*time.Millisecond, 0))
	stats.Record(lwUnsent(start, 700*time.Millisecond, 600*time.Millisecond, time.Second, BlockedOnStream))

	w := lwRead(stats, start, 3500*time.Millisecond, 5)

	if w.NotSentGenerator != 2 || w.Calls != 6 {
		t.Errorf("unsent by the generator = %d of %d calls, want 2 of 6: the one it named and the one its timing names", w.NotSentGenerator, w.Calls)
	}
	if w.Starts != 6 {
		t.Errorf("starts = %d, want 6: an unsent call has a start on record too", w.Starts)
	}
	if w.Late != 1 {
		t.Errorf("late = %d, want 1: only the call begun 600 ms late was over the floor", w.Late)
	}
}

// The window counts a call in the second it was done in, not the second it was
// scheduled in: an unsent call is recorded at its deadline, which may be many
// seconds after the schedule, and the line must find it then.
//
// Ground: boundary — an unsent call with a long timeout, and a slow answer,
// between the second of the schedule and the second of the end.
func TestLagWindow_TheAxisIsTheSecondACallIsDoneIn(t *testing.T) {
	start := time.Unix(1_000, 0)

	t.Run("an unsent call with a timeout of 10 s", func(t *testing.T) {
		stats := lwStats(start, 0)
		stats.Record(lwUnsent(start, 1500*time.Millisecond, 0, 10*time.Second, BlockedOnGenerator))

		if w := lwRead(stats, start, 6500*time.Millisecond, 5); w.NotSentGenerator != 0 || w.Behind() {
			t.Errorf("at 6.5 s: %d unsent, behind %v; want none: it is not done yet", w.NotSentGenerator, w.Behind())
		}

		if w := lwRead(stats, start, 12500*time.Millisecond, 5); w.NotSentGenerator != 1 || !w.Behind() {
			t.Errorf("at 12.5 s: %d unsent, behind %v; want 1, true: it was done in second 11", w.NotSentGenerator, w.Behind())
		}
	})

	t.Run("an answer after 6 s", func(t *testing.T) {
		stats := lwStats(start, 0)
		slow := lwCall(start, 500*time.Millisecond, 5*time.Millisecond)
		slow.DoneAt = start.Add(6500 * time.Millisecond)
		stats.Record(slow)

		for _, c := range []struct {
			at         time.Duration
			wantStarts int
		}{
			{1500 * time.Millisecond, 0},
			{6900 * time.Millisecond, 0},
			{7500 * time.Millisecond, 1},
		} {
			w := lwRead(stats, start, c.at, 5)
			if w.Starts != c.wantStarts || w.Late != c.wantStarts {
				t.Errorf("read at %v: %d starts, %d late; want %d of each", c.at, w.Starts, w.Late, c.wantStarts)
			}
		}
	})
}

// A call that began before it was scheduled has no start lag to speak of; the
// report's start lag leaves it out, and so does the window.
//
// Ground: boundary — a negative lag is not a start.
func TestLagWindow_ACallAheadOfItsScheduleIsNotAStart(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := lwStats(start, 0)

	stats.Record(lwCall(start, time.Second, -5*time.Millisecond))

	if w := lwRead(stats, start, 3500*time.Millisecond, 5); w.Starts != 0 || w.Late != 0 || w.Calls != 1 {
		t.Errorf("window: %d starts, %d late, %d calls; want 0 and 0 and the 1 call", w.Starts, w.Late, w.Calls)
	}
}

// The window is the run's, not a method's: calls of every method add up.
//
// Ground: contract — one window for the run.
func TestLagWindow_IsTheRunsWhateverTheMethod(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := NewStats()
	stats.Reserve(time.Minute, "a", "b")
	stats.Start(start, 0)

	for _, method := range []string{"a", "b", "c"} {
		r := lwCall(start, 500*time.Millisecond, 5*time.Millisecond)
		r.Method = method
		stats.Record(r)
	}

	if w := lwRead(stats, start, 1500*time.Millisecond, 5); w.Starts != 3 || w.Late != 3 || w.Calls != 3 {
		t.Errorf("window: %d calls, %d starts, %d late, want 3 of each over three methods", w.Calls, w.Starts, w.Late)
	}
}

// The edges of the window, as for Recent: a run that has not started, a window
// of no seconds, a now before the start, a call before the start or past the
// reserved span, a window wholly past the span.
//
// Ground: boundary — nothing here may panic or count.
func TestLagWindow_TheEdges(t *testing.T) {
	start := time.Unix(1_000, 0)

	t.Run("a run that has not started", func(t *testing.T) {
		stats := NewStats()
		stats.Reserve(time.Minute, "a")

		if w := stats.LagWindow(start, 5); w != (LagWindow{}) {
			t.Errorf("window of a run not started = %+v, want zero", w)
		}
	})

	t.Run("seconds of zero or less", func(t *testing.T) {
		stats := lwStats(start, 0)
		stats.Record(lwCall(start, 500*time.Millisecond, 5*time.Millisecond))

		for _, seconds := range []int{0, -1, -1 << 62} {
			if w := lwRead(stats, start, 1500*time.Millisecond, seconds); w.Starts != 0 || w.Seconds != 0 {
				t.Errorf("window of %d seconds = %+v, want nothing", seconds, w)
			}
		}
	})

	t.Run("a now before the start", func(t *testing.T) {
		stats := lwStats(start, 0)
		stats.Record(lwCall(start, 500*time.Millisecond, 5*time.Millisecond))

		if w := lwRead(stats, start, -3*time.Second, 5); w.Starts != 0 {
			t.Errorf("window before the start has %d starts, want 0", w.Starts)
		}
	})

	t.Run("a call scheduled before the start is not counted", func(t *testing.T) {
		stats := lwStats(start, 0)
		stats.Record(lwCall(start, -2*time.Second, 5*time.Millisecond))

		if w := lwRead(stats, start, 3500*time.Millisecond, 5); w.Starts != 0 {
			t.Errorf("window has %d starts, want 0: the call was scheduled before the run", w.Starts)
		}
	})

	t.Run("a call past the reserved span is not counted and not fatal", func(t *testing.T) {
		stats := NewStats()
		stats.Reserve(3*time.Second, "a")
		stats.Start(start, 0)
		stats.Record(lwCall(start, 10*time.Second, 5*time.Millisecond))
		stats.Record(lwCall(start, 500*time.Millisecond, 5*time.Millisecond))

		if w := lwRead(stats, start, 12500*time.Millisecond, 5); w.Starts != 0 {
			t.Errorf("window past the span has %d starts, want 0", w.Starts)
		}
		if w := lwRead(stats, start, 1500*time.Millisecond, 5); w.Starts != 1 {
			t.Errorf("window inside the span has %d starts, want the 1 inside it", w.Starts)
		}
	})

	t.Run("a window as long as an int", func(t *testing.T) {
		stats := lwStats(start, 0)
		stats.Record(lwCall(start, 500*time.Millisecond, 5*time.Millisecond))

		if w := lwRead(stats, start, 1500*time.Millisecond, int(^uint(0)>>1)); w.Starts != 1 || w.Seconds != 1 {
			t.Errorf("window of the largest int = %+v, want the 1 second there is", w)
		}
	})
}

// The counters sit in the recording path, which is the hot path: a record
// allocates nothing and the read allocates nothing.
//
// Ground: hot path — zero allocations per record with the lag counters on.
func TestLagWindow_RecordAndReadAllocateNothing(t *testing.T) {
	start := time.Unix(1_000, 0)
	stats := lwStats(start, time.Second)

	late := lwCall(start, 3500*time.Millisecond, 5*time.Millisecond)
	warm := lwCall(start, 500*time.Millisecond, 5*time.Millisecond)
	unsent := lwUnsent(start, 3500*time.Millisecond, 5*time.Millisecond, time.Second, BlockedOnGenerator)
	stats.Record(late)

	if allocs := testing.AllocsPerRun(200, func() {
		stats.Record(late)
		stats.Record(warm)
		stats.Record(unsent)
	}); allocs != 0 {
		t.Errorf("Record allocates %v times per three calls, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(200, func() {
		_ = stats.LagWindow(start.Add(4500*time.Millisecond), 5)
	}); allocs != 0 {
		t.Errorf("LagWindow allocates %v times per read, want 0", allocs)
	}
}

// Workers record while the CLI's ticker reads, under -race; once they are done
// every call is in the window, none twice, none lost.
//
// Ground: concurrency — the counters are written under the lock of Record and
// read under the same one.
func TestLagWindow_ReadsWhileWorkersRecord(t *testing.T) {
	const workers, each = 8, 500

	start := time.Unix(1_000, 0)
	stats := lwStats(start, 0)

	stop := make(chan struct{})
	readerDone := make(chan struct{})

	go func() {
		defer close(readerDone)

		for {
			select {
			case <-stop:
				return
			default:
				_ = lwRead(stats, start, 4500*time.Millisecond, 5)
			}
		}
	}()

	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			for i := range each {
				lag := time.Duration(0)
				if i%2 == 0 {
					lag = 5 * time.Millisecond
				}
				stats.Record(lwCall(start, time.Duration(i%4)*time.Second+100*time.Millisecond, lag))
			}
		})
	}

	wg.Wait()
	close(stop)
	<-readerDone

	w := lwRead(stats, start, 4500*time.Millisecond, 5)
	if w.Starts != workers*each || w.Late != workers*each/2 {
		t.Errorf("window: %d starts, %d late, want %d and %d", w.Starts, w.Late, workers*each, workers*each/2)
	}
}

// The same reader on the engine reaches the stats of its run, and adds what
// only the plan knows: the rate it asked for in the window.
//
// Ground: contract — Engine.LagWindow is Stats.LagWindow of the run plus the
// schedule.
func TestEngine_LagWindowIsTheStatsWindow(t *testing.T) {
	start := time.Unix(1_000, 0)
	eng := &Engine{stats: lwStats(start, 0)}

	eng.stats.Record(lwCall(start, 500*time.Millisecond, 5*time.Millisecond))

	want := eng.stats.LagWindow(start.Add(1500*time.Millisecond), 5)
	got := eng.LagWindow(start.Add(1500*time.Millisecond), 5)

	if got != want || got.Starts != 1 || got.Late != 1 {
		t.Errorf("Engine.LagWindow = %+v, want the stats' window %+v with 1 start, 1 late", got, want)
	}
}

// The rate in the line is the schedule's of the window, not what was reached:
// the highest stage rate the window overlaps, summed over the methods; on a
// ramp, the rate at the far end of the window.
//
// Ground: boundary — a window inside one stage, across two, wholly in the
// second, and on a ramp (a stage with no StartRPS ramps from 0, so the constant
// ones state StartRPS).
func TestEngine_LagWindowCarriesTheScheduledRate(t *testing.T) {
	start := time.Unix(1_000, 0)
	stages := []Stage{
		{StartRPS: 100, TargetRPS: 100, Duration: 3 * time.Second},
		{StartRPS: 400, TargetRPS: 400, Duration: 7 * time.Second},
	}
	ramp := []Stage{
		{StartRPS: 100, TargetRPS: 100, Duration: 3 * time.Second},
		{StartRPS: 100, TargetRPS: 400, Duration: 6 * time.Second},
	}
	constant := []Stage{{StartRPS: 50, TargetRPS: 50, Duration: 10 * time.Second}}

	eng := &Engine{
		opts: Options{Calls: []Call{
			{Method: "a", Stages: stages},
			{Method: "b", Stages: constant},
		}},
		stats: lwStats(start, 0),
	}

	rampEng := &Engine{
		opts: Options{Calls: []Call{
			{Method: "a", Stages: ramp},
			{Method: "b", Stages: constant},
		}},
		stats: lwStats(start, 0),
	}

	if got := rampEng.LagWindow(start.Add(6500*time.Millisecond), 3).Scheduled; got != 250+50 {
		t.Errorf("on a ramp 100 to 400 over 6 s, window [3 s, 6 s): scheduled = %d, want %d: the rate at the window's far end, 250, and 50", got, 250+50)
	}

	for _, c := range []struct {
		name string
		at   time.Duration
		want int
	}{
		{"inside the first stage", 3500 * time.Millisecond, 100 + 50},
		{"across both stages", 5500 * time.Millisecond, 400 + 50},
		{"wholly in the second", 9500 * time.Millisecond, 400 + 50},
	} {
		if got := eng.LagWindow(start.Add(c.at), 3).Scheduled; got != c.want {
			t.Errorf("%s: scheduled = %d, want %d", c.name, got, c.want)
		}
	}
}
