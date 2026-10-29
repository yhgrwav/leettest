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
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

// referenceCumulative is the simple version Cumulative is checked against:
// the same buckets read through Distribution(), which allocates a Bar per
// index. Bar.From is the lowest value of a sub-bucket, Bar.To the highest; the
// sum takes the middle of each, as Cumulative does.
func referenceCumulative(b *Buffer, bounds []time.Duration) (counts []int64, total int64, sum time.Duration) {
	counts = make([]int64, len(bounds))

	for _, bar := range b.measured.Distribution() {
		if bar.Count == 0 {
			continue
		}

		for i, bound := range bounds {
			if bar.From <= int64(bound) {
				counts[i] += bar.Count
			}
		}

		total += bar.Count
		sum += time.Duration((bar.From + (bar.To-bar.From+1)/2) * bar.Count)
	}

	return counts, total, sum
}

func filled(record func(l *Latencies)) *Buffer {
	l := NewLatencies()
	record(l)

	b := NewBuffer()
	l.CopyInto(b)

	return b
}

// Ground: CLAUDE.md «Complex for speed» — the fast version is compared with the
// simple one on the same inputs, exactly: counts, total and sum bit for bit.
func TestBufferCumulative_EqualsTheDistributionReference(t *testing.T) {
	logUniform := func(n int) func(l *Latencies) {
		return func(l *Latencies) {
			rng := rand.New(rand.NewPCG(1, 2))
			top := math.Log(float64(time.Hour))
			for range n {
				l.Record(time.Duration(math.Exp(rng.Float64() * top)))
			}
		}
	}

	inputs := map[string]func(l *Latencies){
		"empty": func(*Latencies) {},
		"every edge and one nanosecond either side": func(l *Latencies) {
			for _, e := range exportEdges {
				l.Record(e - 1)
				l.Record(e)
				l.Record(e + 1)
			}
		},
		"every edge, one percent either side": func(l *Latencies) {
			for _, e := range exportEdges {
				l.Record(e - e/100)
				l.Record(e + e/100)
			}
		},
		"1 to 4100 ns one by one, past the width-one range": func(l *Latencies) {
			for v := time.Duration(1); v <= 4100; v++ {
				l.Record(v)
			}
		},
		"1 ns to 1 h, log uniform":    logUniform(20000),
		"the same twice over":         func(l *Latencies) { logUniform(5000)(l); logUniform(5000)(l) },
		"past the last edge":          func(l *Latencies) { l.Record(61 * time.Second); l.Record(time.Hour); l.Record(time.Minute) },
		"past the range, censored":    func(l *Latencies) { l.Record(2 * time.Hour); l.Record(time.Millisecond) },
		"censored beside measured":    func(l *Latencies) { l.RecordCensored(time.Second); l.Record(time.Second) },
		"many on one value":           func(l *Latencies) { l.Record(3 * time.Millisecond); l.Record(3 * time.Millisecond) },
		"a zero and the largest kept": func(l *Latencies) { l.Record(0); l.Record(time.Hour) },
	}

	for name, record := range inputs {
		t.Run(name, func(t *testing.T) {
			b := filled(record)

			wantCounts, wantTotal, wantSum := referenceCumulative(b, exportEdges)

			counts := make([]int64, len(exportEdges))
			total, sum := b.Cumulative(exportEdges, counts)

			if !slices.Equal(counts, wantCounts) || total != wantTotal || sum != wantSum {
				t.Errorf("Cumulative: counts %v total %d sum %v\nreference:  counts %v total %d sum %v",
					counts, total, sum, wantCounts, wantTotal, wantSum)
			}
		})
	}
}

// Ground: the same, for a different list of bounds than the export's: the
// walk must not depend on the 19 edges.
func TestBufferCumulative_EqualsTheReferenceForOtherBounds(t *testing.T) {
	b := filled(func(l *Latencies) {
		// Every nanosecond to 9000, so bounds on the lowest value of a
		// sub-bucket (2048, 4096, 4100) meet observations in it.
		for v := time.Duration(0); v <= 9000; v++ {
			l.Record(v)
		}
		rng := rand.New(rand.NewPCG(3, 4))
		for range 5000 {
			l.Record(time.Duration(rng.Int64N(int64(2 * time.Second))))
		}
	})

	for _, bounds := range [][]time.Duration{
		nil,
		{0},
		{2047, 2048, 2049, 4095, 4096, 4097, 4100, 8192, 8200},
		{time.Nanosecond, time.Microsecond, time.Millisecond, time.Hour, 2 * time.Hour},
	} {
		wantCounts, wantTotal, wantSum := referenceCumulative(b, bounds)

		counts := make([]int64, len(bounds))
		total, sum := b.Cumulative(bounds, counts)

		if !slices.Equal(counts, wantCounts) || total != wantTotal || sum != wantSum {
			t.Errorf("bounds %v: counts %v total %d sum %v, reference counts %v total %d sum %v",
				bounds, counts, total, sum, wantCounts, wantTotal, wantSum)
		}
	}
}

// Ground: the flat-index formula is the library's. A histogram with one
// observation on every sub-bucket's lowest value, as lowestValueAt says it: if
// the formula were off at any index, that observation would land in another
// sub-bucket and the bars would not read one, one, one. Also fails when
// lowestTrackableNanos or significantFigures change without the formula.
func TestLowestValueAtMatchesTheLibrary(t *testing.T) {
	h := newHistogram()

	n := 0
	for ; lowestValueAt(n) <= highestTrackableNanos; n++ {
		if err := h.RecordValue(lowestValueAt(n)); err != nil {
			t.Fatalf("recording the lowest value of index %d (%d): %v", n, lowestValueAt(n), err)
		}
	}

	bars := h.Distribution()
	if len(bars) != n {
		t.Fatalf("%d sub-buckets walked, Distribution() has %d bars", n, len(bars))
	}

	for i, bar := range bars {
		if bar.Count != 1 || bar.From != lowestValueAt(i) || bar.To-bar.From+1 != subBucketWidthAt(i) {
			t.Fatalf("index %d: formula says [%d, +%d), the library has count %d from %d to %d",
				i, lowestValueAt(i), subBucketWidthAt(i), bar.Count, bar.From, bar.To)
		}
	}

	// Export().Counts is as long as the flat index the formula assumes: the
	// last index reaches the highest trackable value, the one after it would
	// be a bucket more.
	last := len(h.Export().Counts) - 1
	if lowestValueAt(last)+subBucketWidthAt(last) <= highestTrackableNanos || lowestValueAt(last-subBucketHalfCount)+subBucketWidthAt(last-subBucketHalfCount) > highestTrackableNanos {
		t.Errorf("Export().Counts has %d entries; the formula puts the end of the range elsewhere", last+1)
	}
}

func benchmarkBuffer() (*Buffer, []time.Duration, []int64) {
	l := NewLatencies()
	rng := rand.New(rand.NewPCG(5, 6))
	top := math.Log(float64(time.Minute))
	for range 50000 {
		l.Record(time.Duration(math.Exp(rng.Float64() * top)))
	}

	b := NewBuffer()
	l.CopyInto(b)

	return b, exportEdges, make([]int64, len(exportEdges))
}

// BenchmarkBufferCumulative against BenchmarkBufferCumulativeReference is the
// "why" of Cumulative: B/op is gated in CI (the code at most a third of the
// reference), ns/op is only printed.
func BenchmarkBufferCumulative(b *testing.B) {
	buf, bounds, counts := benchmarkBuffer()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		buf.Cumulative(bounds, counts)
	}
}

func BenchmarkBufferCumulativeReference(b *testing.B) {
	buf, bounds, _ := benchmarkBuffer()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		referenceCumulative(buf, bounds)
	}
}
