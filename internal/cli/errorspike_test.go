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

package cli

import (
	"strings"
	"testing"
	"time"

	shr "github.com/yhgrwav/leettest/pkg/share"
	"github.com/yhgrwav/leettest/pkg/spike"
)

func spikeLimit(t *testing.T, written string) shr.Percent {
	t.Helper()

	p, err := shr.Parse(written, "test")
	if err != nil {
		t.Fatalf("parse %q: %v", written, err)
	}

	return p
}

func spikeTrip(t *testing.T, failed, total int) spike.Trip {
	t.Helper()

	return spike.Trip{
		Method: "a.S/One", Failed: failed, Total: total, Limit: spikeLimit(t, "5%"),
		At: 12300 * time.Millisecond, SentRPS: 1204, InFlight: 800,
	}
}

// Ground: contract — the stop line is the one place a user reads why the run
// ended: when (from the start), which method, its share, the counts it comes
// from, the limit, what the generator was sending and how many calls hung.
// The text is the contract; the words are the reviewer's to change in this
// test.
func TestStopLine_SaysWhenWhoHowMuchAndAgainstWhat(t *testing.T) {
	const want = "stopped at 12.3s: a.S/One failed 87.5% (35 of 40 calls in the last 5s), over stop_on_errors 5%; " +
		"1204 rps sent in the last second, 800 in flight"

	if got := StopLine(spikeTrip(t, 35, 40)); got != want {
		t.Errorf("StopLine =\n%q\nwant\n%q", got, want)
	}
}

// Ground: boundary — the share is printed by the one rule of share.Format: a
// share a hair over the limit is never rounded down onto it. 5001 of 100000
// is 5.001%, which "5.0%" would show as being at the limit it has passed.
func TestStopLine_ShareJustOverTheLimitIsNotRoundedOntoIt(t *testing.T) {
	got := StopLine(spikeTrip(t, 5001, 100000))

	if !strings.Contains(got, "failed 5.001% (") {
		t.Errorf("StopLine = %q, want the share as 5.001%%", got)
	}
}

// Ground: contract — the report that follows a stop carries the same line in
// its notes, so a saved report says why it is incomplete; a run that nobody
// stopped has no such line.
func TestPrintReport_AStoppedRunCarriesTheStopLine(t *testing.T) {
	report := echoReport()
	report.Incomplete = true
	report.Planned = 40 * time.Second

	trip := spikeTrip(t, 35, 40)

	var stopped, plain strings.Builder
	PrintReport(&stopped, "localhost:50051", RunReport{Report: report, Stop: &trip})
	PrintReport(&plain, "localhost:50051", RunReport{Report: report})

	if line := StopLine(trip); !strings.Contains(stopped.String(), "stopped at 12.3s") || !strings.Contains(stopped.String(), line) {
		t.Errorf("the report of a stopped run has no stop line:\n%s", stopped.String())
	}
	if strings.Contains(plain.String(), "stopped at") {
		t.Errorf("a run nobody stopped says it was stopped:\n%s", plain.String())
	}
}

// Ground: contract — the settings echo tells the limit as written on a line
// of its own under the settings, so a pasted report shows what the run was
// guarded by; 100% is told as off, because that is what it does; a limit
// nobody wrote prints no line.
func TestPrintReport_TheSettingsTellTheStopLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit string
		want  string
	}{
		{"a limit", "5%", "stop_on_errors: 5%"},
		{"a fraction", "0.5%", "stop_on_errors: 0.5%"},
		{"a hundred is off", "100%", "stop_on_errors: off (100%)"},
		{"left out", "", ""},
	} {
		lines := echoLines(RunReport{
			Report: echoReport(), Name: "wallet",
			Settings: &RunSettings{MaxInFlight: 5000, Connections: 1, Mode: "local", StopOnErrors: tc.limit},
		})

		i := indexOfPrefix(lines, "stop_on_errors:")
		if tc.want == "" {
			if i >= 0 {
				t.Errorf("%s: a line %q for a limit nobody wrote", tc.name, lines[i])
			}

			continue
		}
		if i < 0 || lines[i] != tc.want {
			t.Errorf("%s: no line %q:\n%s", tc.name, tc.want, strings.Join(lines, "\n"))

			continue
		}
		if j := indexOfPrefix(lines, "settings:"); j < 0 || i != j+1 {
			t.Errorf("%s: the line is at %d, want right under the settings line at %d", tc.name, i, j)
		}
	}
}

// Ground: contract — JSON: a run the spike ended says so at the top level and
// where, on what numbers, with the limit as written and the time in whole
// microseconds. The names are an interface (schema_v1.txt).
func TestJSON_AStoppedRunSaysWhyAndWhere(t *testing.T) {
	report := echoReport()
	report.Incomplete = true

	trip := spikeTrip(t, 35, 40)
	run := jsonRun(report)
	run.Outcome = OutcomeIncomplete
	run.Run.Stop = &trip

	out := writeJSON(t, run)

	if got := out["stop_reason"]; got != StopErrorSpike {
		t.Errorf("stop_reason = %v, want %q", got, StopErrorSpike)
	}

	stop, ok := out["stop"].(map[string]any)
	if !ok {
		t.Fatalf("stop = %#v, want an object", out["stop"])
	}

	want := map[string]any{"method": "a.S/One", "failed": 35.0, "total": 40.0, "limit": "5%", "at_us": 12_300_000.0}
	for key, w := range want {
		if stop[key] != w {
			t.Errorf("stop.%s = %#v, want %#v", key, stop[key], w)
		}
	}
	if len(stop) != len(want) {
		t.Errorf("stop has %d keys, want %d: %v", len(stop), len(want), stop)
	}
}

// Ground: contract — a run nobody stopped has both keys, null: a key that is
// missing is not a value a reader can test for.
func TestJSON_ARunNobodyStoppedHasNullStopFields(t *testing.T) {
	out := writeJSON(t, jsonRun(echoReport()))

	for _, key := range []string{"stop_reason", "stop"} {
		v, present := out[key]
		if !present || v != nil {
			t.Errorf("%s = %#v (present %v), want null", key, v, present)
		}
	}
}
