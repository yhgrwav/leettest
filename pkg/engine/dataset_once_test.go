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
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// onceCall is a dataset call of n records, none handed out twice, whose plan
// has count ticks at 200 rps.
func onceCall(counter *RecordCounter, n, count int) Call {
	call := datasetCall(n, counter, atRate(200, count))
	call.Dataset.Once = true

	return call
}

// fourLinks is a sender with four connections as far as the engine can tell,
// answering after a pause so that several calls are in flight at once.
type fourLinks struct {
	*recordingSender

	pause time.Duration
}

func (fourLinks) Links() []string { return []string{"a:1", "b:1", "c:1", "d:1"} }

func (s fourLinks) Send(ctx context.Context, req Request) (Outcome, error) {
	time.Sleep(s.pause)

	return s.recordingSender.Send(ctx, req)
}

// countPerRecord is how many times each of the n records was sent.
func countPerRecord(sent []int, n int) []int {
	counts := make([]int, n)
	for _, r := range sent {
		if r >= 0 && r < n {
			counts[r]++
		}
	}

	return counts
}

// Every record goes out exactly once whatever the order, over four connections
// with calls in flight together, and a plan that wants more than the file ends
// there. The count is the call's, not a connection's.
// Ground: contract — "at most once" is exact: one counter per call in the
// dispatcher; a repeat is a duplicate key on the target, not its limit.
func TestEngine_OnceHandsOutEachRecordExactlyOnce(t *testing.T) {
	const n = 20

	for name, counter := range map[string]func() *RecordCounter{
		"file order":   NewRecordCounter,
		"random order": func() *RecordCounter { return NewShuffledCounter(n, 5) },
	} {
		t.Run(name, func(t *testing.T) {
			sender := fourLinks{recordingSender: answering(), pause: 5 * time.Millisecond}

			runEngine(t, onceCall(counter(), n, 2*n), 0, sender)

			want := slices.Repeat([]int{1}, n)
			if got := countPerRecord(sender.records(), n); !slices.Equal(got, want) {
				t.Errorf("sends per record = %v, want each exactly once", got)
			}
			if got := len(sender.records()); got != n {
				t.Errorf("%d requests sent, want %d: no request past the file", got, n)
			}
		})
	}
}

// The run ends gently at the tick that needs a record the file does not have:
// the calls already in flight finish and are recorded, none is cut off, and the
// report names the call and the file.
// Ground: contract — the run is incomplete and says why; nothing is aborted.
func TestEngine_OnceEndsTheRunGentlyWhenTheFileRunsOut(t *testing.T) {
	const n = 10

	sender := fourLinks{recordingSender: answering(), pause: 60 * time.Millisecond}
	report := runEngine(t, onceCall(NewRecordCounter(), n, 3*n), 0, sender)

	if !report.Incomplete {
		t.Errorf("Incomplete = false, want true: the plan was cut short")
	}
	if got := report.DatasetExhausted; got == nil || got.Method != "a.B/One" || got.File != "data/users.jsonl" {
		t.Errorf("DatasetExhausted = %+v, want the call a.B/One and the file data/users.jsonl", got)
	}

	m := report.Methods[0]
	if m.Sent != n || m.Failed != 0 {
		t.Errorf("sent %d failed %d, want %d and 0: the calls in flight finish", m.Sent, m.Failed, n)
	}
	// The sending ended at the tick that found no record (10 requests in 50ms,
	// 200 per second), not when the last call came back 60ms later (about 95).
	if m.RPS < 150 {
		t.Errorf("sent/s = %.0f, want about 200: the sending ends where the file does", m.RPS)
	}
	if got := len(sender.records()); got != n {
		t.Errorf("the sender saw %d requests, want %d", got, n)
	}
	if d := m.Dataset; d == nil || d.Used != n || d.UsedMax != 1 || !d.Once {
		t.Errorf("dataset report = %+v, want used %d, used_max 1, Once", d, n)
	}
}

// A plan that takes exactly the file is not exhaustion: the end is lazy, a
// record is missing only when a tick needs it.
// Ground: contract — the inverse of the test above; a run that used the whole
// file and no more is complete and says nothing of exhaustion.
func TestEngine_OnceWithAPlanOfExactlyTheFileIsComplete(t *testing.T) {
	const n = 10

	sender := answering()
	report := runEngine(t, onceCall(NewRecordCounter(), n, n), 0, sender)

	if report.Incomplete {
		t.Errorf("Incomplete = true, want false: the plan ran to its end")
	}
	if report.DatasetExhausted != nil {
		t.Errorf("DatasetExhausted = %+v, want nil: no tick was left without a record", report.DatasetExhausted)
	}
	if got := countPerRecord(sender.records(), n); !slices.Equal(got, slices.Repeat([]int{1}, n)) {
		t.Errorf("sends per record = %v, want each exactly once", got)
	}
}

// The second run on the same counter has nothing to hand out: it ends at its
// first tick with nothing sent, the first run having used the file.
// Ground: contract — S is the call's for the process (a search's runs share it).
func TestEngine_OnceSecondRunOnASpentCounterEndsAtItsFirstTick(t *testing.T) {
	const n = 10

	counter := NewRecordCounter()
	first := runEngine(t, onceCall(counter, n, n), 0, answering())
	if first.DatasetExhausted != nil || first.Incomplete {
		t.Fatalf("the first run: Incomplete %v exhausted %+v, want a complete run", first.Incomplete, first.DatasetExhausted)
	}

	sender := answering()
	second := runEngine(t, onceCall(counter, n, n), 0, sender)

	if second.DatasetExhausted == nil || !second.Incomplete {
		t.Errorf("the second run: Incomplete %v exhausted %+v, want both set", second.Incomplete, second.DatasetExhausted)
	}
	if got := len(sender.records()); got != 0 {
		t.Errorf("the second run sent %d requests, want 0", got)
	}
}

// The inverse: without Once the file wraps, as it did, and the run is whole.
// Ground: contract — the default is unchanged.
func TestEngine_ReuseWrapsPastTheFileAndNeverExhausts(t *testing.T) {
	const n = 5

	sender := answering()
	report := runEngine(t, datasetCall(n, NewRecordCounter(), atRate(200, 3*n)), 0, sender)

	if report.Incomplete || report.DatasetExhausted != nil {
		t.Errorf("Incomplete %v exhausted %+v, want a complete run that wrapped", report.Incomplete, report.DatasetExhausted)
	}
	if got := countPerRecord(sender.records(), n); !slices.Equal(got, slices.Repeat([]int{3}, n)) {
		t.Errorf("sends per record = %v, want each 3 times", got)
	}
	if d := report.Methods[0].Dataset; d == nil || d.Once {
		t.Errorf("dataset report = %+v, want Once false", d)
	}
}

// Warm-up uses records like any other tick: the file is spent by it too.
// Ground: contract — "no record twice in the whole process, warm-up included".
func TestEngine_OnceCountsTheWarmup(t *testing.T) {
	const n = 10

	sender := answering()
	// The first ticks are warm-up: the 10 records go out, the 11th tick has none.
	report := runEngine(t, onceCall(NewRecordCounter(), n, n+1), 20*time.Millisecond, sender)

	if got := countPerRecord(sender.records(), n); !slices.Equal(got, slices.Repeat([]int{1}, n)) {
		t.Errorf("sends per record = %v, want each exactly once, warm-up included", got)
	}
	if report.DatasetExhausted == nil {
		t.Errorf("DatasetExhausted = nil, want set: the 11th tick had no record")
	}
}

func runScheduler(t *testing.T, call Call) (int, error) {
	t.Helper()

	out := make(chan Request, 64)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := (&Scheduler{Call: call}).Run(ctx, out)

	return len(out), err
}

func TestSchedulerRefusesOnce(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		call Call
	}{
		{"records", onceCall(nil, 3, 3)},
		{"no records", func() Call {
			c := onceCall(nil, 3, 3)
			c.Payloads = nil

			return c
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			queued, err := runScheduler(t, tc.call)
			if !errors.Is(err, ErrOnceNeedsEngine) {
				t.Fatalf("err = %v, want ErrOnceNeedsEngine", err)
			}
			if !strings.Contains(err.Error(), tc.call.Method) {
				t.Fatalf("err = %q does not name the call %q", err, tc.call.Method)
			}
			if queued != 0 {
				t.Fatalf("%d requests handed out before the refusal, want none", queued)
			}
		})
	}
}

func TestSchedulerWithoutOnceRunsAsBefore(t *testing.T) {
	t.Parallel()

	queued, err := runScheduler(t, datasetCall(3, nil, atRate(200, 3)))
	if err != nil {
		t.Fatalf("err = %v, want none", err)
	}
	if queued != 3 {
		t.Fatalf("%d requests, want 3", queued)
	}
}
