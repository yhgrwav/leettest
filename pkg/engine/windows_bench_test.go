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
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkStatsRecordParallel is the cost of Record from many goroutines at
// once, as the workers call it: off, with the windows on but every call in one
// window, and with them on and the calls spread over all 60. The point is the
// per-(method,window) lock under contention, which a serial benchmark does not
// show. The accepted cost of "on" over "off" is fixed at the review of the code.
// Ground: hot path — nanoseconds and allocations per record, in parallel.
func BenchmarkStatsRecordParallel(b *testing.B) {
	const measured = time.Minute

	cases := []struct {
		name    string
		windows bool
		spread  int
	}{
		{"off", false, 1},
		{"on-one-window", true, 1},
		{"on-spread-over-60", true, 60},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			start := time.Now()
			stats := NewStats()
			if c.windows {
				stats.SetWindows(measured)
			}
			stats.Reserve(2*measured, "a")
			stats.Start(start, 0)
			stats.EndSending(start.Add(measured))

			var next atomic.Int64
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					at := time.Duration(next.Add(1)%int64(c.spread)) * time.Second
					stats.Record(call(start, at, at+time.Millisecond, CategorySuccess))
				}
			})
		})
	}
}
