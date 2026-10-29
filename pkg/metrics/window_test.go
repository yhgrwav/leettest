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

// Ground: contract (A6, T10) — 60 windows of 50 methods are 3000 pairs; at
// three digits a pair is 540 KB and the run would hold 1.6 GB. The bound is
// measured, not guessed: a two-digit histogram of the full range is 36.9 KB.
func TestNewWindowLatencies_AWindowsPairIsUnderSeventyFourKilobytes(t *testing.T) {
	const (
		most  = 74_000
		least = 60_000 // both histograms are allocated, not one
	)

	l := NewWindowLatencies()
	if got := l.ByteSize(); got > most || got < least {
		t.Errorf("a window's pair of histograms = %d bytes, want %d..%d", got, least, most)
	}
}

// Ground: contract (A7) — two digits are coarser, never wrong by more than
// the 1% the text promises, over the whole range a call can have.
// Green on the stub (three digits are finer): it holds the line for the code.
func TestNewWindowLatencies_AnyLatencyFromAMicrosecondToHalfAMinuteReadsWithinAPercent(t *testing.T) {
	for _, d := range []time.Duration{
		time.Microsecond, 17 * time.Microsecond, 999 * time.Microsecond, 1234 * time.Microsecond,
		12345 * time.Microsecond, 100 * time.Millisecond, 987 * time.Millisecond, 4321 * time.Millisecond,
		29 * time.Second, 30 * time.Second,
	} {
		l := NewWindowLatencies()
		for range 10 {
			l.Record(d)
		}

		got := l.Snapshot().Percentile(0.5)
		if !got.Defined || !got.Exact || max(got.Value-d, d-got.Value) > d/100 {
			t.Errorf("p50 of %s read as %+v, want within 1%% and exact", d, got)
		}
	}
}

// Ground: contract (A3, T2) — a row of the text report is a fold of windows;
// folding must be the same as having recorded everything in one histogram,
// the cut-off calls included, or a merged row would show a false p99.
// Green on the stub.
func TestNewWindowLatencies_FoldedWindowsEqualOneHistogramOfAllTheCalls(t *testing.T) {
	first, second, all := NewWindowLatencies(), NewWindowLatencies(), NewWindowLatencies()
	for i := range 300 {
		d := time.Duration(i%37+1) * time.Millisecond
		if i%2 == 0 {
			first.Record(d)
		} else {
			second.Record(d)
		}
		all.Record(d)
	}
	second.RecordCensored(900 * time.Millisecond)
	all.RecordCensored(900 * time.Millisecond)

	folded, want := Merge(first.Snapshot(), second.Snapshot()), all.Snapshot()
	if folded.Count() != want.Count() || folded.CensoredCount() != want.CensoredCount() {
		t.Fatalf("folded %d observations (%d censored), want %d (%d)",
			folded.Count(), folded.CensoredCount(), want.Count(), want.CensoredCount())
	}
	for _, p := range []float64{0.5, 0.9, 0.99, 1} {
		if got, w := folded.Percentile(p), want.Percentile(p); got != w {
			t.Errorf("p%v of the fold = %+v, want %+v from one histogram", p*100, got, w)
		}
	}
}
