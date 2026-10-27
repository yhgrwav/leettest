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
// whole seconds of every method, at the end of a run of each length. It must
// not depend on the length of the run, only on the methods, and allocate
// nothing: the lengths differ by 720 times and the times must agree to
// within noise.
// Ground: hot path — nanoseconds and allocations per read of the window.
func BenchmarkStatsRecent(b *testing.B) {
	for _, run := range []struct {
		name string
		span time.Duration
	}{{"run1m", time.Minute}, {"run30m", 30 * time.Minute}, {"run12h", 12 * time.Hour}} {
		for _, methods := range []int{1, 10, 100} {
			// A timeline is reserved for each method up front: 100 methods of
			// 12 hours is most of a gigabyte for a benchmark that reads five
			// seconds of it. The length of the run is shown with 1 and 10.
			if run.span >= 12*time.Hour && methods > 10 {
				continue
			}

			b.Run(fmt.Sprintf("%s/methods%d", run.name, methods), func(b *testing.B) {
				names := make([]string, methods)
				for m := range names {
					names[m] = fmt.Sprintf("pkg.Service/Method%d", m)
				}

				stats := NewStats()
				start := time.Now()
				stats.Reserve(run.span, names...)
				stats.Start(start, 0)

				// At the end of the run, with calls in each of the five whole
				// seconds before.
				now := start.Add(run.span - time.Second/2)

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
}

// BenchmarkStatsRecordWatched is BenchmarkStatsRecord with a reader beside the
// writers, at a thousand times the pace of a run (every millisecond, where a
// run reads once a second), in three forms:
//
//	off    no reader: the run as it is without the feature, which has no
//	       goroutine for it at all;
//	tick   a reader that wakes every millisecond and takes nothing: the cost
//	       of the timer and the scheduler, which is not the feature's;
//	reads  a reader that calls Recent every millisecond: the cost of the
//	       lock the window is read under.
//
// The price of the feature is reads against off; of the lock alone, reads
// against tick.
// Ground: hot path — nanoseconds and allocations per record with the watch on.
func BenchmarkStatsRecordWatched(b *testing.B) {
	for _, mode := range []string{"off", "tick", "reads"} {
		b.Run(mode, func(b *testing.B) {
			const writers = 100

			stats := NewStats()
			start := time.Now()
			stats.Reserve(time.Hour, "pkg.Service/Method")
			stats.Start(start, 0)

			stop := make(chan struct{})

			var reader sync.WaitGroup

			if mode != "off" {
				reader.Go(func() {
					var dst []Recent

					tick := time.NewTicker(time.Millisecond)
					defer tick.Stop()

					for {
						select {
						case <-stop:
							return
						case <-tick.C:
							if mode == "reads" {
								dst = stats.Recent(dst, start.Add(5*time.Second), 5)
							}
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
