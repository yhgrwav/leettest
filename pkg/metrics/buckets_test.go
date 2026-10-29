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

package metrics

import (
	"math"
	"slices"
	"sync"
	"testing"
	"time"
)

// exportEdges are the contract's 19 bucket edges of the Prometheus latency
// histogram, in seconds 0.0001 ... 60. Written out here on purpose: a change
// of the list in the code must be a change of this test.
var exportEdges = []time.Duration{
	100 * time.Microsecond, 250 * time.Microsecond, 500 * time.Microsecond,
	time.Millisecond, 2500 * time.Microsecond, 5 * time.Millisecond,
	10 * time.Millisecond, 25 * time.Millisecond, 50 * time.Millisecond,
	100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond,
	time.Second, 2500 * time.Millisecond, 5 * time.Second,
	10 * time.Second, 25 * time.Second, 50 * time.Second, 60 * time.Second,
}

// cumulativeOf fills a buffer from record and reads its cumulative buckets
// at exportEdges.
func cumulativeOf(record func(l *Latencies)) (counts []int64, total int64, sum time.Duration) {
	l := NewLatencies()
	record(l)

	b := NewBuffer()
	l.CopyInto(b)

	counts = make([]int64, len(exportEdges))
	total, sum = b.Cumulative(exportEdges, counts)

	return counts, total, sum
}

// Ground: strategy §7 / review verdict R2.6 — the five histograms of a method
// besides success are read only for how many calls they hold, on every
// scrape; copying them would be the scrape storm the verdict forbids.
func TestLatenciesCount_CountsMeasuredAndCensoredAlike(t *testing.T) {
	l := NewLatencies()
	for range 3 {
		l.Record(time.Millisecond)
	}
	for range 2 {
		l.RecordCensored(time.Second)
	}

	if got := l.Count(); got != 5 {
		t.Errorf("Count() = %d, want 5 (3 measured + 2 censored)", got)
	}
}

// Ground: same — a value past the histogram's hour goes to censored in Record;
// it is still an observation of the method.
func TestLatenciesCount_AValuePastTheRangeIsCounted(t *testing.T) {
	l := NewLatencies()
	l.Record(2 * time.Hour)

	if got := l.Count(); got != 1 {
		t.Errorf("Count() after one value past the range = %d, want 1", got)
	}
}

// Ground: same — a negative duration is an invalid record, not an observation.
func TestLatenciesCount_ANegativeDurationIsNotCounted(t *testing.T) {
	l := NewLatencies()
	l.Record(-time.Millisecond)
	l.Record(time.Millisecond)

	if got := l.Count(); got != 1 {
		t.Errorf("Count() = %d, want 1: the negative record is counted apart", got)
	}
}

// Ground: edge — an empty distribution is zero, not a panic.
// Green on the stub (it returns 0): it holds the line for the code.
func TestLatenciesCount_EmptyIsZero(t *testing.T) {
	if got := NewLatencies().Count(); got != 0 {
		t.Errorf("Count() of an empty distribution = %d, want 0", got)
	}
}

// Ground: review verdict R2.6 — O(1), no allocation, no copy of the buckets.
// Green on the stub: it holds the line for the code (a Count that copies the
// histogram allocates and turns this red).
func TestLatenciesCount_DoesNotAllocate(t *testing.T) {
	l := NewLatencies()
	for i := range 1000 {
		l.Record(time.Duration(i+1) * time.Microsecond)
	}

	var sink int64
	allocs := testing.AllocsPerRun(100, func() { sink += l.Count() })

	if allocs != 0 {
		t.Errorf("Count() allocates %v per call, want 0 (sink %d)", allocs, sink)
	}
}

// Ground: review verdict R2.5 — the scrape reads while calls are recorded;
// the count taken after the writers are done is exact. Run with -race.
func TestLatenciesCount_NextToRecordingIsRaceFreeAndExactAtTheEnd(t *testing.T) {
	const (
		writers = 4
		each    = 2500
	)

	l := NewLatencies()
	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
				_ = l.Count()
			}
		}
	}()

	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			for range each {
				l.Record(time.Millisecond)
			}
		})
	}
	wg.Wait()
	close(stop)
	<-readerDone

	if got := l.Count(); got != writers*each {
		t.Errorf("Count() = %d, want %d", got, writers*each)
	}
}

// Ground: decisions R3 — HDR sub-buckets go by their lower value, so an
// observation exactly on an edge is in that edge's le (Prometheus le is <=).
func TestBufferCumulative_AValueExactlyOnEachEdgeIsInThatEdge(t *testing.T) {
	for i, edge := range exportEdges {
		t.Run(edge.String(), func(t *testing.T) {
			counts, total, _ := cumulativeOf(func(l *Latencies) { l.Record(edge) })

			want := make([]int64, len(exportEdges))
			for j := i; j < len(want); j++ {
				want[j] = 1
			}
			if !slices.Equal(counts, want) || total != 1 {
				t.Errorf("one value on %v: counts %v total %d, want counts %v total 1", edge, counts, total, want)
			}
		})
	}
}

// Ground: decisions R3 — the lower-value rule means a value 1% above an edge
// (ten times the sub-bucket) is never in that edge: the error is 0.1%, not more.
func TestBufferCumulative_AValueOnePercentAboveAnEdgeIsNotInThatEdge(t *testing.T) {
	for i, edge := range exportEdges {
		t.Run(edge.String(), func(t *testing.T) {
			above := edge + edge/100
			counts, total, _ := cumulativeOf(func(l *Latencies) { l.Record(above) })

			want := make([]int64, len(exportEdges))
			for j := i + 1; j < len(want); j++ {
				want[j] = 1
			}
			if !slices.Equal(counts, want) || total != 1 {
				t.Errorf("one value at %v: counts %v total %d, want counts %v total 1", above, counts, total, want)
			}
		})
	}
}

// Ground: Brief test 2 — a value above 60 s is only in +Inf, which the caller
// reads as the total.
func TestBufferCumulative_AValueAboveTheLastEdgeIsOnlyInTheTotal(t *testing.T) {
	counts, total, _ := cumulativeOf(func(l *Latencies) { l.Record(61 * time.Second) })

	want := make([]int64, len(exportEdges))
	if !slices.Equal(counts, want) || total != 1 {
		t.Errorf("one value at 61s: counts %v total %d, want all zero and total 1", counts, total)
	}
}

// Ground: edge — a zero latency is in the first bucket, not lost below it.
func TestBufferCumulative_AZeroLatencyIsInTheFirstBucket(t *testing.T) {
	counts, total, _ := cumulativeOf(func(l *Latencies) { l.Record(0) })

	if counts[0] != 1 || total != 1 {
		t.Errorf("one zero latency: first bucket %d total %d, want 1 and 1", counts[0], total)
	}
}

// Ground: Brief test 2 — counts are cumulative over a spread of values; none
// of them is within 1% of an edge, so the rule of the lower value cannot move one.
func TestBufferCumulative_CountsAreCumulativeOverASpread(t *testing.T) {
	values := []time.Duration{
		50 * time.Microsecond, 300 * time.Microsecond, 300 * time.Microsecond, 2 * time.Millisecond,
		70 * time.Millisecond, 3 * time.Second, 90 * time.Second,
	}

	counts, total, _ := cumulativeOf(func(l *Latencies) {
		for _, v := range values {
			l.Record(v)
		}
	})

	want := make([]int64, len(exportEdges))
	for i, edge := range exportEdges {
		for _, v := range values {
			if v <= edge {
				want[i]++
			}
		}
	}
	if !slices.Equal(counts, want) || total != int64(len(values)) {
		t.Errorf("counts %v total %d, want counts %v total %d", counts, total, want, len(values))
	}
}

// Ground: Brief test 3 — a censored observation is a lower bound, never a
// measured one: it is in no bucket and not in the total.
func TestBufferCumulative_CensoredObservationsAreInNoBucketAndNotInTheTotal(t *testing.T) {
	counts, total, _ := cumulativeOf(func(l *Latencies) {
		l.Record(2 * time.Millisecond)
		l.Record(2 * time.Millisecond)
		l.RecordCensored(time.Second)
		l.RecordCensored(time.Second)
		l.RecordCensored(time.Second)
	})

	if counts[len(counts)-1] != 2 || total != 2 {
		t.Errorf("2 measured + 3 censored: last bucket %d total %d, want 2 and 2", counts[len(counts)-1], total)
	}
}

// Ground: edge — an empty buffer: zero everywhere, no division, no panic.
// Green on the stub: it holds the line for the code.
func TestBufferCumulative_EmptyIsAllZero(t *testing.T) {
	counts, total, sum := cumulativeOf(func(*Latencies) {})

	if !slices.Equal(counts, make([]int64, len(exportEdges))) || total != 0 || sum != 0 {
		t.Errorf("empty: counts %v total %d sum %v, want all zero", counts, total, sum)
	}
}

// Ground: contract — counts is overwritten, not added to: the exporter reuses
// one slice for every scrape.
func TestBufferCumulative_OverwritesWhatCountsHeld(t *testing.T) {
	b := NewBuffer()
	NewLatencies().CopyInto(b)

	counts := make([]int64, len(exportEdges))
	for i := range counts {
		counts[i] = 7
	}
	b.Cumulative(exportEdges, counts)

	if !slices.Equal(counts, make([]int64, len(exportEdges))) {
		t.Errorf("counts of an empty buffer after Cumulative = %v, want all zero", counts)
	}
}

// Ground: edge (impossible input) — a counts slice of another length than the
// bounds is a caller bug and is loud, not a quiet short read.
func TestBufferCumulative_AMismatchedLengthPanics(t *testing.T) {
	b := NewBuffer()
	NewLatencies().CopyInto(b)

	defer func() {
		if recover() == nil {
			t.Error("Cumulative with len(counts) != len(bounds) did not panic")
		}
	}()
	b.Cumulative(exportEdges, make([]int64, len(exportEdges)-1))
}

// Ground: decisions R3 — _sum comes from the HDR buckets, within 0.1% of the
// exact sum of what was recorded (3 significant digits).
func TestBufferCumulative_SumIsWithinTenthOfAPercentOfTheExactSum(t *testing.T) {
	var exact time.Duration
	_, _, sum := cumulativeOf(func(l *Latencies) {
		// A deterministic spread over six decades: 1, 1.37, 1.37^2 ... µs.
		for v := 1000.0; v < 4e10; v *= 1.37 {
			d := time.Duration(v)
			l.Record(d)
			exact += d
		}
	})

	if rel := math.Abs(float64(sum-exact)) / float64(exact); rel > 0.001 {
		t.Errorf("sum = %v, exact %v: off by %.4f%%, want within 0.1%%", sum, exact, rel*100)
	}
}

// Ground: decisions R3 — the sum covers the values past the last edge too:
// they are in +Inf, so they are in _sum.
func TestBufferCumulative_SumIncludesValuesPastTheLastEdge(t *testing.T) {
	_, _, sum := cumulativeOf(func(l *Latencies) {
		for range 3 {
			l.Record(90 * time.Second)
		}
		l.Record(time.Millisecond)
	})

	want := 270*time.Second + time.Millisecond
	if rel := math.Abs(float64(sum-want)) / float64(want); rel > 0.001 {
		t.Errorf("sum = %v, want %v within 0.1%%", sum, want)
	}
}

// Ground: Brief test 3 — a censored observation adds nothing to _sum.
func TestBufferCumulative_SumLeavesCensoredObservationsOut(t *testing.T) {
	_, _, sum := cumulativeOf(func(l *Latencies) {
		l.Record(10 * time.Millisecond)
		l.RecordCensored(30 * time.Second)
	})

	want := 10 * time.Millisecond
	if rel := math.Abs(float64(sum-want)) / float64(want); rel > 0.001 {
		t.Errorf("sum = %v, want %v: the censored 30s is not in it", sum, want)
	}
}

// Ground: contract — the buffer is refilled again and again; Cumulative reads
// what it holds now, not what an earlier fill left.
func TestBufferCumulative_ReadsTheLatestFill(t *testing.T) {
	first := NewLatencies()
	for range 5 {
		first.Record(time.Millisecond)
	}
	second := NewLatencies()
	second.Record(time.Second)

	b := NewBuffer()
	first.CopyInto(b)
	second.CopyInto(b)

	counts := make([]int64, len(exportEdges))
	total, _ := b.Cumulative(exportEdges, counts)

	if total != 1 || counts[3] != 0 {
		t.Errorf("after refilling with one 1s value: total %d, le 1ms %d, want 1 and 0", total, counts[3])
	}
}
