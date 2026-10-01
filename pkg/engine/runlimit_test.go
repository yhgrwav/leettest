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
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/metrics"
)

// Ground: contract — one rule for the report's verdict and the search's
// "run's limit" (#140, moved from internal/cli): a tenth of p99 on the
// histogram's values, or unsent calls, and a wait in the tail.
func TestReport_RunLimitIsTheVerdictRule(t *testing.T) {
	q := func(ms float64) metrics.Quantile {
		return metrics.Quantile{Value: time.Duration(ms * float64(time.Millisecond)), Exact: true, Defined: true}
	}
	for _, tc := range []struct {
		name    string
		report  Report
		cause   WaitCause
		limited bool
	}{
		{"1.1ms of 21.9ms", Report{GeneratorTailCalls: 100, Methods: []MethodReport{{P99: q(21.9), P99WithoutClientWaits: q(20.8)}}}, "", false},
		{"exactly a tenth", Report{GeneratorTailCalls: 100, Methods: []MethodReport{{P99: q(20), P99WithoutClientWaits: q(18)}}}, WaitGenerator, true},
		{"unsent for a stream", Report{NotSent: 2, NotSentStream: 2, StreamTailCalls: 5, Methods: []MethodReport{{P99: q(20), P99WithoutClientWaits: q(20)}}}, WaitStream, true},
		{"moved, nobody waited in the tail", Report{Methods: []MethodReport{{P99: q(20), P99WithoutClientWaits: q(10)}}}, "", false},
		{"the larger cause in the tail", Report{GeneratorTailCalls: 3, StreamTailCalls: 9, Methods: []MethodReport{{P99: q(20), P99WithoutClientWaits: q(10)}}}, WaitStream, true},
	} {
		cause, limited := tc.report.RunLimit()
		if cause != tc.cause || limited != tc.limited {
			t.Errorf("%s: %q %v, want %q %v", tc.name, cause, limited, tc.cause, tc.limited)
		}
	}
}
