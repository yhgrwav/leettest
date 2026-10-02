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

// The scheduled count is the dispatcher's ticks, i × 1s / rps within each
// stage, counted from the warmup on: a tick exactly at the warmup's end is
// measured, as Stats.Record files it.
func TestScheduledAfter_CountsTheTicksOfTheMeasuredWindow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stages []Stage
		warmup time.Duration
		want   int
	}{
		{"no warmup", []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Second}}, 0, 100},
		{"a tick on the warmup's end", []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Second}}, 100 * time.Millisecond, 90},
		{"ticks off the millisecond", []Stage{{StartRPS: 300, TargetRPS: 300, Duration: time.Second}}, 333 * time.Millisecond, 200},
		{"a duration not a whole tick", []Stage{{StartRPS: 3, TargetRPS: 3, Duration: 1100 * time.Millisecond}}, 0, 4},
		{"two stages, warmup in the first", []Stage{
			{StartRPS: 10, TargetRPS: 10, Duration: time.Second},
			{StartRPS: 20, TargetRPS: 20, Duration: time.Second},
		}, 500 * time.Millisecond, 5 + 20},
		{"warmup past the first stage", []Stage{
			{StartRPS: 10, TargetRPS: 10, Duration: time.Second},
			{StartRPS: 20, TargetRPS: 20, Duration: time.Second},
		}, 1500 * time.Millisecond, 10},
	} {
		if got := ScheduledAfter(tc.stages, tc.warmup); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

// Sent and Scheduled are of one window: on a sender that answers every call,
// every scheduled call after the warmup is sent, none of the warmup's is.
func TestReport_ScheduledIsTheWindowSentCountsIn(t *testing.T) {
	stages := []Stage{{StartRPS: 100, TargetRPS: 100, Duration: 200 * time.Millisecond}}
	eng, err := New(Options{
		Calls:       []Call{{Method: "a.B/One", Timeout: 100 * time.Millisecond, Stages: stages}},
		Sender:      FakeSender{Delay: time.Millisecond},
		MaxInFlight: 64,
		Warmup:      50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	r := eng.Report()
	if r.Scheduled != 15 || r.Sent+r.NotSent != r.Scheduled || r.WarmupSent != 5 {
		t.Errorf("scheduled %d, sent %d + not sent %d, warmup sent %d; want 15 = 15 + 0, 5",
			r.Scheduled, r.Sent, r.NotSent, r.WarmupSent)
	}
}
