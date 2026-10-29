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

// Count reports every observation, measured and censored alike, without
// copying anything: O(1), under the lock Record takes, no allocation.
//
// STUB (prometheus-export, layer 1): returns 0.
func (l *Latencies) Count() int64 {
	return 0
}

// Cumulative fills counts[i] with the measured observations whose HDR
// sub-bucket starts at or below bounds[i], for ascending bounds; the
// sub-bucket is assigned by its lower value, so an observation exactly on a
// bound is in that bound and one up to 0.1% above it may be in the one below.
// It returns the measured total (what a +Inf bucket holds, a value past the
// last bound included) and the sum of the measured values, read from the
// buckets, within 0.1% of the exact sum. Censored observations are in none of
// them. len(counts) must equal len(bounds).
//
// STUB (prometheus-export, layer 1): returns 0, 0 and leaves counts alone.
func (b *Buffer) Cumulative(bounds []time.Duration, counts []int64) (total int64, sum time.Duration) {
	return 0, 0
}
