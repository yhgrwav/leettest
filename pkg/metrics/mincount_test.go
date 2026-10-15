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
