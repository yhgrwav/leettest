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
	"slices"
	"testing"
	"time"
)

// The order of seed 42 over five records, three passes: the literal list the
// fixed mapping seed -> PCG(seed, 0x6c65657474657374) and a step-wise
// Fisher-Yates (position i swaps with i + IntN(n-i), the draw made for the last
// position too) give. A seed written in an old report must give this order in
// every later version; a changed algorithm, or a changed IntN or PCG in the
// standard library, turns this red.
var (
	golden42 = []int{0, 3, 4, 1, 2, 4, 2, 1, 3, 0, 3, 1, 4, 2, 0}
	golden7  = []int{0, 2, 3, 4, 1, 0, 4, 3, 1, 2, 3, 2, 1, 4, 0}
)

const goldenSeed = 42

func randomCall(n int, seed uint64, count int) Call {
	return datasetCall(n, NewShuffledCounter(n, seed), atRate(100, count))
}

// Ground: contract — random order is a fresh permutation per pass: each record
// once per pass, which keeps the report's used and used_max exact.
func TestDispatcher_RandomDatasetEveryPassIsAPermutation(t *testing.T) {
	got := handOut(t, randomCall(5, goldenSeed, 15))["a.B/One"]
	if len(got) != 15 {
		t.Fatalf("handed out %d, want 15", len(got))
	}

	for pass := 0; pass < 3; pass++ {
		seen := slices.Clone(got[pass*5 : pass*5+5])
		slices.Sort(seen)
		if want := []int{0, 1, 2, 3, 4}; !slices.Equal(seen, want) {
			t.Errorf("pass %d = %v (sorted %v), want each of 0..4 exactly once", pass, got[pass*5:pass*5+5], seen)
		}
	}
}

// Ground: contract — the same seed, file and count give the same sequence, and
// it is the sequence of the fixed algorithm, not only equal to itself.
func TestDispatcher_RandomDatasetSameSeedSameSequence(t *testing.T) {
	first := handOut(t, randomCall(5, goldenSeed, 15))["a.B/One"]
	second := handOut(t, randomCall(5, goldenSeed, 15))["a.B/One"]

	if !slices.Equal(first, second) {
		t.Errorf("two runs with seed %d: %v and %v, want the same", goldenSeed, first, second)
	}
	if !slices.Equal(first, golden42) {
		t.Errorf("seed %d, n = 5: %v, want the golden %v", goldenSeed, first, golden42)
	}
}

// Ground: contract — the seed decides the order.
func TestDispatcher_RandomDatasetOtherSeedOtherSequence(t *testing.T) {
	got := handOut(t, randomCall(5, 7, 15))["a.B/One"]

	if slices.Equal(got, golden42) {
		t.Errorf("seeds 7 and %d gave the same sequence %v", goldenSeed, got)
	}
	if !slices.Equal(got, golden7) {
		t.Errorf("seed 7, n = 5: %v, want the golden %v", got, golden7)
	}
}

// Ground: contract — pass k+1 is a new permutation of the same generator, not
// the first one again.
func TestDispatcher_RandomDatasetPassesDiffer(t *testing.T) {
	got := handOut(t, randomCall(5, goldenSeed, 15))["a.B/One"]

	if len(got) == 15 && (slices.Equal(got[0:5], got[5:10]) || slices.Equal(got[5:10], got[10:15])) {
		t.Errorf("two passes alike in %v: one permutation reused", got)
	}
}

// The library path hands out by the same rule as the dispatcher.
// Ground: contract — pkg/engine is a library API; Run is part of it.
func TestScheduler_RunRandomDatasetFollowsTheSeed(t *testing.T) {
	out := make(chan Request, 32)
	if err := NewScheduler(randomCall(5, goldenSeed, 15)).Run(t.Context(), out); err != nil {
		t.Fatalf("run: %v", err)
	}
	close(out)

	var got []int
	for req := range out {
		got = append(got, recordNumber(req))
	}
	if !slices.Equal(got, golden42) {
		t.Errorf("records handed out = %v, want %v", got, golden42)
	}
}

// The counter is the call's, so a second run goes on in the same permutation:
// its first record is the next one, not the first of a new shuffle.
// Ground: contract — D3: the order lives with the call's counter.
func TestEngine_RandomDatasetContinuesAcrossRuns(t *testing.T) {
	call := randomCall(5, goldenSeed, 8)
	first, second := answering(), answering()

	runEngine(t, call, 0, first)
	call.Stages = []Stage{atRate(100, 7)}
	runEngine(t, call, 0, second)

	got := append(first.records(), second.records()...)
	if !slices.Equal(got, golden42) {
		t.Errorf("records over two runs = %v, want %v: the second run went on from the first", got, golden42)
	}
	if got, want := call.Dataset.Counter.Handed(), 15; got != want {
		t.Errorf("handed out = %d, want %d", got, want)
	}
}

// Warm-up requests are requests of the call: the order goes on through them.
// Ground: contract — D3, as for file order.
func TestEngine_RandomDatasetOrderSurvivesWarmup(t *testing.T) {
	sender := answering()
	runEngine(t, randomCall(5, goldenSeed, 15), 50*time.Millisecond, sender)

	if !slices.Equal(sender.records(), golden42) {
		t.Errorf("records sent = %v, want %v: warm-up included", sender.records(), golden42)
	}
}

// The report says the order and the seed of a random call, and keeps the
// numbers exact: used = min(S, n), used_max = ceil(S / n).
// Ground: contract — the seed in the report is what repeats the run.
func TestEngine_RandomDatasetReportCarriesOrderAndSeed(t *testing.T) {
	report := runEngine(t, randomCall(5, goldenSeed, 12), 0, answering())

	want := &DatasetReport{File: "data/users.jsonl", Records: 5, Used: 5, UsedMax: 3, Random: true, Seed: goldenSeed}
	if got := report.Methods[0].Dataset; got == nil || *got != *want {
		t.Errorf("dataset report = %+v, want %+v", got, want)
	}
}

// Ground: contract — file order says so: not random, no seed.
func TestEngine_FileDatasetReportIsNotRandom(t *testing.T) {
	report := runEngine(t, datasetCall(5, NewRecordCounter(), atRate(100, 12)), 0, answering())

	if got := report.Methods[0].Dataset; got == nil || got.Random || got.Seed != 0 {
		t.Errorf("dataset report = %+v, want file order: Random false, Seed 0", got)
	}
}

// Ground: contract — building a request uses up no record, in random order too.
func TestScheduler_BuildingARandomRequestDoesNotUseUpItsRecord(t *testing.T) {
	counter := NewShuffledCounter(5, 1) // seed 1 starts with record 1
	s := NewScheduler(datasetCall(5, counter, atRate(100, 1)))
	at := time.Now()

	first, second := s.newRequest(at), s.newRequest(at)

	if recordNumber(first) != 1 || recordNumber(second) != 1 {
		t.Errorf("records = %d and %d, want 1 twice: the first of the permutation", recordNumber(first), recordNumber(second))
	}
	if n := counter.Handed(); n != 0 {
		t.Errorf("handed out = %d, want 0", n)
	}
}

// Ground: hot path — a random request allocates no more than one without a dataset.
func TestScheduler_NewRequestWithARandomDatasetAllocatesNoMore(t *testing.T) {
	at := time.Now()
	with := NewScheduler(datasetCall(5, NewShuffledCounter(5, goldenSeed), atRate(100, 1)))
	without := NewScheduler(Call{Method: "a.B/One", Payload: []byte{9}, Timeout: time.Second, Stages: []Stage{atRate(100, 1)}})

	withAllocs := testing.AllocsPerRun(1000, func() { sinkRequest = with.newRequest(at) })
	withoutAllocs := testing.AllocsPerRun(1000, func() { sinkRequest = without.newRequest(at) })

	if withAllocs > withoutAllocs {
		t.Errorf("allocations per request: %v with a random dataset, %v without", withAllocs, withoutAllocs)
	}
}
