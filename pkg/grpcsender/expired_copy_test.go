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

package grpcsender

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
)

// Ground: boundary — a DEADLINE_EXCEEDED is the target's copy of our deadline
// from 90% of the call's budget on, not a nanosecond before. The number is
// written here on purpose: moving expiredCopyPercent must turn this red.
func TestExpiredCopy_StartsAtNinetyPercentOfTheBudget(t *testing.T) {
	sent := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	const budget = time.Second
	deadline := sent.Add(budget)

	for _, tc := range []struct {
		name   string
		code   codes.Code
		answer time.Duration
		want   bool
	}{
		{"one nanosecond before 90%", codes.DeadlineExceeded, 900*time.Millisecond - 1, false},
		{"exactly 90%", codes.DeadlineExceeded, 900 * time.Millisecond, true},
		{"95%", codes.DeadlineExceeded, 950 * time.Millisecond, true},
		{"at the deadline", codes.DeadlineExceeded, budget, true},
		{"half of the budget", codes.DeadlineExceeded, 500 * time.Millisecond, false},
		{"another code at 95%", codes.Unavailable, 950 * time.Millisecond, false},
	} {
		times := callTimes{sentAt: sent, answeredAt: sent.Add(tc.answer)}
		if got := expiredCopy(tc.code, times, deadline); got != tc.want {
			t.Errorf("%s: expiredCopy = %v, want %v", tc.name, got, tc.want)
		}
	}
}
