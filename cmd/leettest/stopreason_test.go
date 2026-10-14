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

package main

import (
	"testing"

	"github.com/yhgrwav/leettest/internal/cli"
)

func TestStopReasonFirstWins(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		first, second string
	}{
		{"requested then exhausted", cli.StopRequested, cli.StopDatasetExhausted},
		{"exhausted then requested", cli.StopDatasetExhausted, cli.StopRequested},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var r stopReason
			r.stopFor(tc.first)
			r.stopFor(tc.second)

			if got := r.get(); got != tc.first {
				t.Fatalf("reason = %q, want the first stop %q", got, tc.first)
			}
		})
	}
}

func TestStopReasonEmptyUntilStopped(t *testing.T) {
	t.Parallel()

	var r stopReason
	if got := r.get(); got != "" {
		t.Fatalf("reason = %q before any stop, want empty", got)
	}
}
