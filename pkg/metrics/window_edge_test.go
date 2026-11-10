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
	"testing"
	"time"
)

// windowBound100ms is the lower bound a window of two significant digits
// reads for a p99 that lies in the censored mass at 100ms: the bottom of the
// 100ms bucket of that histogram, 99 614 720 ns (99.61ms). The bucket is
// 524 288 ns wide, eight times the three-digit one, and runs to 100 139 007 ns.
const windowBound100ms = 99_614_720 * time.Nanosecond

// cutOffCalls records 90 calls at 10ms and ten cut off, nine at 100ms and one
// at 50ms: two thresholds, so the p99 (rank 99) is the bottom of the bucket
// where the censored values put it, not a threshold itself.
func cutOffCalls(l *Latencies) *Latencies {
	record(l, 90, 10*time.Millisecond)
	recordCensored(l, 9, 100*time.Millisecond)
	recordCensored(l, 1, 50*time.Millisecond)

	return l
}

// Ground: contract (audit item 4) — a window keeps two significant digits, so
// the lower bound of a p99 in the censored mass is the bottom of a two-digit
// bucket: 99.61ms for 100ms. The fixed 11-bit edges applied to the window's
// bucket top read 100.07ms, above the bucket's own bottom, and claim the true
// value is higher than the data shows.
func TestWindowLowerBoundIsTheBottomOfTheWindowsOwnBucket(t *testing.T) {
	l := cutOffCalls(NewWindowLatencies())

	q := l.Snapshot().Percentile(0.99)
	if !q.Defined || q.Exact || q.Value != windowBound100ms {
		t.Errorf("window p99 = %+v, want the lower bound %v", q, windowBound100ms)
	}
}

// Ground: contract — windows folded into one row keep their digits: the
// merged snapshot reads the same bound as the window it was made of.
func TestMergedWindowsKeepTheWindowsBucketEdges(t *testing.T) {
	merged := Merge(cutOffCalls(NewWindowLatencies()).Snapshot(), NewWindowLatencies().Snapshot())

	q := merged.Percentile(0.99)
	if !q.Defined || q.Exact || q.Value != windowBound100ms {
		t.Errorf("merged p99 = %+v, want the lower bound %v", q, windowBound100ms)
	}
}

// Ground: contract — the run's own three-digit histogram is unchanged: the
// same observations read the bottom of the 100ms bucket, 99.94ms (the bucket
// is 65 536 ns wide). Green before the change: it keeps the line for the code.
func TestRunLowerBoundIsStillTheBottomOfTheThreeDigitBucket(t *testing.T) {
	l := cutOffCalls(NewLatencies())

	q := l.Snapshot().Percentile(0.99)
	if want := 99_942_400 * time.Nanosecond; !q.Defined || q.Exact || q.Value != want {
		t.Errorf("run p99 = %+v, want the lower bound %v", q, want)
	}
}
