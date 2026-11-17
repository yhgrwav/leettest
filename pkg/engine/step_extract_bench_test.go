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

// replySender answers every call with a success and a reply of a few bytes,
// kept only when the request asks for it, as the gRPC sender does.
type replySender struct{ reply []byte }

func (s replySender) Send(_ context.Context, req Request) (Outcome, error) {
	out := Outcome{Category: CategorySuccess, Code: "OK", CodeFromTarget: true}
	if req.KeepResponse {
		out.Response = s.reply
	}

	return out, nil
}

// benchIterate runs b.N iterations of call through the pool: the engine's cost
// of a scenario iteration, the sender answering at once.
func benchIterate(b *testing.B, call *Call) {
	b.Helper()

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

	pool := NewWorkerPool(replySender{reply: []byte("w-0123456789abcdef")}, 64)
	runDone := make(chan struct{})

	go func() {
		defer close(runDone)

		_ = pool.Run(context.Background(), in, out)
	}()

	b.ReportAllocs()
	b.ResetTimer()

	for i := range b.N {
		in <- Request{
			Method: call.Method, scenario: call, ScheduledAt: time.Now(), Seq: uint64(i) + 1, //nolint:gosec // i >= 0
			KeepResponse: call.KeepResponse,
		}
	}
	close(in)
	<-runDone
	close(out)
	<-done
}

// BenchmarkIterate_Plain is a scenario iteration of two steps that extracts
// nothing: the baseline of BenchmarkIterate_Extract, and the gate of a scenario
// with no extract (benchstat against main: p<0.05 and >5% is a regression).
func BenchmarkIterate_Plain(b *testing.B) {
	benchIterate(b, &Call{
		Method: "a/One", Scenario: "flow",
		Next: []Step{{Method: "a/Two"}},
	})
}

// BenchmarkIterate_Extract is the same iteration with a value taken from step
// 1's reply and written into step 2's body. The functions are text only (the
// engine knows no proto); what the CLI's decode and build cost on top is
// BenchmarkExtract_DecodeAndBuild in internal/cli. A number for the report,
// not a gate.
func BenchmarkIterate_Extract(b *testing.B) {
	benchIterate(b, &Call{
		Method: "a/One", Scenario: "flow", KeepResponse: true, Values: 1,
		Extract: func(resp []byte, vals []string) error {
			vals[0] = string(resp)

			return nil
		},
		Next: []Step{{
			Method: "a/Two",
			Build:  func(_ uint64, vals []string) []byte { return []byte("from:" + vals[0]) },
		}},
	})
}
