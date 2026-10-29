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

import "time"

// subBucketHalfCount is the geometry of every histogram newHistogram makes:
// a lowest trackable value of 1 and 3 significant digits need values below
// 2000 told apart, so a bucket holds 2048 sub-buckets and the upper half of
// each bucket after the first is 1024 of them. The flat index of Export's
// Counts runs 0..2047 for bucket 0 (the sub-bucket is the value) and then
// 1024 per bucket, each bucket twice as wide as the one before.
const subBucketHalfCount = 1 << 10

// Count reports every observation, measured and censored alike, without
// copying anything: O(1), under the lock Record takes, no allocation.
func (l *Latencies) Count() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	n := l.measured.TotalCount()
	if l.censored != nil {
		n += l.censored.TotalCount()
	}

	return n
}

// lowestValueAt is the smallest value stored in the sub-bucket at flat index
// idx of Export().Counts: hdrhistogram-go v1.4.0 valueFromFlatIndex for
// subBucketHalfCount 1024 and unit 1. TestLowestValueAtMatchesTheLibrary pins it
// to Distribution() on every index.
func lowestValueAt(idx int) int64 {
	if idx < 2*subBucketHalfCount {
		return int64(idx)
	}

	return int64(idx%subBucketHalfCount+subBucketHalfCount) << subBucketShift(idx)
}

// subBucketWidthAt is how many values the sub-bucket at flat index idx holds.
func subBucketWidthAt(idx int) int64 {
	if idx < 2*subBucketHalfCount {
		return 1
	}

	return 1 << subBucketShift(idx)
}

func subBucketShift(idx int) uint {
	return uint(idx/subBucketHalfCount - 1)
}

// Cumulative fills counts[i] with the measured observations whose HDR
// sub-bucket starts at or below bounds[i], for ascending bounds; the
// sub-bucket is assigned by its lower value, so an observation exactly on a
// bound is in that bound and one up to 0.1% above it may be in the one below.
// It returns the measured total (what a +Inf bucket holds, a value past the
// last bound included) and the sum of the measured values, read from the
// buckets (each at its middle), within 0.05% of the exact sum. Censored
// observations are in none of them. len(counts) must equal len(bounds), else
// Cumulative panics.
//
// One pass over Export().Counts: one allocation of about 270 KB per call,
// where Distribution() allocates a Bar per index, 3.3 MB and 23 allocations at
// the benchmark's spread, and takes ten times as long (B/op of
// BenchmarkBufferCumulative against BenchmarkBufferCumulativeReference, gated
// in CI at a third). The library gives no count by index, so the flat index is
// turned into a value here.
func (b *Buffer) Cumulative(bounds []time.Duration, counts []int64) (total int64, sum time.Duration) {
	if len(counts) != len(bounds) {
		panic("metrics: Buffer.Cumulative: len(counts) != len(bounds)")
	}

	var sumNanos int64
	next := 0

	for idx, c := range b.measured.Export().Counts {
		if c == 0 {
			continue
		}

		low := lowestValueAt(idx)
		for next < len(bounds) && low > int64(bounds[next]) {
			counts[next] = total
			next++
		}

		total += c
		sumNanos += (low + subBucketWidthAt(idx)/2) * c
	}

	for ; next < len(bounds); next++ {
		counts[next] = total
	}

	return total, time.Duration(sumNanos)
}
