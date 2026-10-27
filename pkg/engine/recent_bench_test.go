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
	"fmt"
	"sync"
	"testing"
	"time"
)

// BenchmarkStatsRecent measures one read of the stop monitor: the last five
// whole seconds of every method, from a run an hour long. It must not depend
// on the length of the run, only on the methods, and allocate nothing.
// Ground: hot path — nanoseconds and allocations per read of the window.
func BenchmarkStatsRecent(b *testing.B) {
	for _, methods := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("methods%d", methods), func(b *testing.B) {
			names := make([]string, methods)
			for m := range names {
				names[m] = fmt.Sprintf("pkg.Service/Method%d", m)
			}

			stats := NewStats()
			start := time.Now()
			stats.Reserve(time.Hour, names...)
			stats.Start(start, 0)

			// Half an hour in, with calls in each of the five seconds before.
			now := start.Add(time.Hour / 2)

			for _, name := range names {
				for i := range 50 {
					done := now.Add(-time.Duration(i%5+1) * time.Second)
					category := CategorySuccess
					if i%10 == 0 {
						category = CategoryServerFault
					}
					stats.Record(Result{
						Method: name, ScheduledAt: start, BegunAt: start,
						Outcome: Outcome{SentAt: start, DoneAt: done, Category: category},
					})
				}
			}

			dst := make([]Recent, 0, methods)

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				dst = stats.Recent(dst, now, 5)
			}
		})
	}
}

// BenchmarkStatsRecordWatched is BenchmarkStatsRecord with the monitor on, and
// at a thousand times its pace: it reads the window every millisecond where a
// run reads it once a second. The cost of the feature to a recording call is
// the difference between this and the same writers with no monitor; off, there
// is no monitor at all and the two are one.
// Ground: hot path — nanoseconds and allocations per record with the watch on.
func BenchmarkStatsRecordWatched(b *testing.B) {
	for _, watched := range []bool{false, true} {
		b.Run(fmt.Sprintf("watched%v", watched), func(b *testing.B) {
			const writers = 100

			stats := NewStats()
			start := time.Now()
			stats.Reserve(time.Hour, "pkg.Service/Method")
			stats.Start(start, 0)

			stop := make(chan struct{})

			var reader sync.WaitGroup

			if watched {
				reader.Go(func() {
					var dst []Recent

					tick := time.NewTicker(time.Millisecond)
					defer tick.Stop()

					for {
						select {
						case <-stop:
							return
						case <-tick.C:
							dst = stats.Recent(dst, start.Add(5*time.Second), 5)
						}
					}
				})
			}

			var wg sync.WaitGroup

			each := max(b.N/writers, 1)

			b.ReportAllocs()
			b.ResetTimer()

			for range writers {
				wg.Go(func() {
					for range each {
						stats.Record(Result{
							Method:      "pkg.Service/Method",
							ScheduledAt: start,
							Outcome:     Outcome{DoneAt: start.Add(12 * time.Millisecond), Category: CategorySuccess},
						})
					}
				})
			}

			wg.Wait()
			b.StopTimer()
			close(stop)
			reader.Wait()
		})
	}
}
