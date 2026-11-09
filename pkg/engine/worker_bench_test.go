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
	"context"
	"testing"
	"time"
)

type instantSender struct{}

func (instantSender) Send(context.Context, Request) (Outcome, error) {
	return Outcome{Category: CategorySuccess}, nil
}

// BenchmarkWorkerPoolPath is the result path of a call without Next: request
// in, worker, result out, Stats.Record. A call without a scenario must pay for
// scenarios with one length check and nothing else (docs/decisions.md, hot path).
func BenchmarkWorkerPoolPath(b *testing.B) {
	stats := NewStats()
	stats.Start(time.Now(), 0)

	in := make(chan Request)
	out := make(chan Result, 64)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for r := range out {
			stats.Record(r)
		}
	}()

	pool := NewWorkerPool(instantSender{}, 64)
	runDone := make(chan struct{})

	go func() {
		defer close(runDone)

		_ = pool.Run(context.Background(), in, out)
	}()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		in <- Request{Method: "a/One", ScheduledAt: time.Now()}
	}
	close(in)
	<-runDone
	close(out)
	<-done
}
