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

package measure

import (
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/yhgrwav/leettest/test/stand"
)

// A target behind one stream, as in streams_test.go: it answers the first
// calls, then every call waits for the stream and times out. The silence is
// held against the stage of the second its sent rate counts; calls that
// waited go out late, and a silence past the plan has no stage to name.
func TestReport_AStreamLimitedSilenceNamesTheStageRate(t *testing.T) {
	const (
		rps = 50
		run = 3 * time.Second
	)
	target := stand.StartWith(stand.Constant(150*time.Millisecond), grpc.MaxConcurrentStreams(1))
	t.Cleanup(target.Stop)

	report, err := runOn(t, target, load(target.Method(), rps, run, 300*time.Millisecond), 1000, asIs)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	m := report.Methods[0]
	if m.SilentFrom == nil {
		t.Fatalf("no silence: timed out %d of %d", m.TimedOut, m.Sent)
	}
	want := rps
	if time.Duration(*m.SilentFrom-1)*time.Second >= run {
		want = 0
	}
	if m.SilentPlannedLow != want || m.SilentPlannedHigh != want {
		t.Errorf("silent from %d: planned %d-%d, want %d", *m.SilentFrom, m.SilentPlannedLow, m.SilentPlannedHigh, want)
	}
	if m.RPSLow != rps || m.RPSHigh != rps {
		t.Errorf("planned_rps %d-%d, want the whole plan %d", m.RPSLow, m.RPSHigh, rps)
	}
}
