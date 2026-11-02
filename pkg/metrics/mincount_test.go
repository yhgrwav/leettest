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

import "testing"

// Ground: boundary — the named thresholds: p99 needs 100 observations, p99.9 needs 1000
// (999 * 1.001 float error must not push it to 1001).
func TestMinCountNamesTheThresholds(t *testing.T) {
	for _, tt := range []struct {
		p    float64
		want int64
	}{{0.99, 100}, {0.999, 1000}} {
		if got := MinCount(tt.p); got != tt.want {
			t.Errorf("MinCount(%v) = %d, want %d", tt.p, got, tt.want)
		}
	}
}

// Ground: boundary — the counts the early warning relies on: with p99, 2 of 100 above a
// threshold put p99 above it and 1 does not; 200 needs 3, 1000 needs 11, 60000 needs 601
// (600 of 60000 is exactly 1%); fewer than one observation needs 1.
func TestMinAboveNamesTheThresholds(t *testing.T) {
	for _, tt := range []struct {
		n    int64
		want int64
	}{{0, 1}, {-3, 1}, {1, 1}, {100, 2}, {101, 2}, {200, 3}, {1000, 11}, {60000, 601}} {
		if got := MinAbove(0.99, tt.n); got != tt.want {
			t.Errorf("MinAbove(0.99, %d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

// Ground: boundary — MinAbove is the rank rule's own count: with k of n observations above
// a threshold and the rest at or below it, p lies above the threshold exactly when the
// ones at or below it do not reach the rank, for every k and n.
func TestMinAboveIsWhereThePercentileLeavesTheThreshold(t *testing.T) {
	for _, p := range []float64{0.5, 0.9, 0.99, 0.999} {
		for n := int64(1); n <= 400; n++ {
			least := MinAbove(p, n)
			for k := int64(0); k <= n; k++ {
				if above := n-k < rankFor(p, n); above != (k >= least) {
					t.Fatalf("p=%v n=%d k=%d: p above the threshold = %v, but MinAbove = %d", p, n, k, above, least)
				}
			}
		}
	}
}

// Ground: boundary — MinCount is the rank rule's own threshold: p is below the max exactly
// when n >= MinCount(p), for every n.
func TestMinCountIsWhereTheRankLeavesTheMax(t *testing.T) {
	for _, p := range []float64{0.5, 0.9, 0.99, 0.999} {
		least := MinCount(p)
		for n := int64(1); n <= 3000; n++ {
			if below := rankFor(p, n) < n; below != (n >= least) {
				t.Fatalf("p=%v n=%d: rank below n = %v, but MinCount = %d", p, n, below, least)
			}
		}
	}
}
