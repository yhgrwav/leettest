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

	"github.com/yhgrwav/leettest/pkg/errstop"
	shr "github.com/yhgrwav/leettest/pkg/share"
)

func spikeLimit(t *testing.T, written string) shr.Percent {
	t.Helper()

	p, err := shr.Parse(written, "test")
	if err != nil {
		t.Fatalf("parse %q: %v", written, err)
	}

	return p
}

func spikeTrip(t *testing.T, failed, total int) errstop.Trip {
	t.Helper()

	return errstop.Trip{
		Method: "a.S/One", Failed: failed, Total: total, Limit: spikeLimit(t, "5%"),
		At: 12300 * time.Millisecond, InFlight: 800,
	}
}

// offered is a planned rate for a stop line.
func offered(v float64) *float64 { return &v }

// Ground: contract — the stop line is the one place a user reads why the run
// ended: when (from the start), which method, its share, the counts it comes
// from, the limit, the rate the plan offered at that moment and how many calls
// hung. There is no send rate in it: the engine keeps no count of calls begun,
// and the rate is the plan's, not what went out. The text is the contract; the
// words are the reviewer's to change in this test.
func TestStopLine_SaysWhenWhoHowMuchAndAgainstWhat(t *testing.T) {
	const want = "stopped at 12.3s: a.S/One failed 87.5% (35 of 40 calls in the last 5s), over stop_on_errors 5%; " +
		"the plan offered 1700 rps at that moment; 800 in flight"

	if got := StopLine(spikeTrip(t, 35, 40), offered(1700)); got != want {
		t.Errorf("StopLine =\n%q\nwant\n%q", got, want)
	}
}

// Ground: boundary — one rule for the time: seconds with one decimal, whether
// the run is short or long, so a line read in a log of a 40-minute run is
// told like one of a 12-second run.
func TestStopLine_TheTimeHasOneFormat(t *testing.T) {
	for _, tc := range []struct {
		at   time.Duration
		want string
	}{
		{12300 * time.Millisecond, "stopped at 12.3s:"},
		{2 * time.Second, "stopped at 2.0s:"},
		{1949 * time.Millisecond, "stopped at 1.9s:"},
		{1960 * time.Millisecond, "stopped at 2.0s:"},
		{42*time.Minute + 5300*time.Millisecond, "stopped at 2525.3s:"},
	} {
		trip := spikeTrip(t, 35, 40)
		trip.At = tc.at

		if got := StopLine(trip, offered(1700)); !strings.HasPrefix(got, tc.want) {
			t.Errorf("At %v: StopLine = %q, want it to start with %q", tc.at, got, tc.want)
		}
	}
}

// Ground: boundary — the share is printed by the one rule of share.Format: a
// share a hair over the limit is never rounded down onto it. 5001 of 100000
// is 5.001%, which "5.0%" would show as being at the limit it has passed.
func TestStopLine_ShareJustOverTheLimitIsNotRoundedOntoIt(t *testing.T) {
	got := StopLine(spikeTrip(t, 5001, 100000), offered(1700))

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
	PrintReport(&stopped, "localhost:50051", RunReport{Report: report, Stop: &trip, StopPlanned: offered(1700)})
	PrintReport(&plain, "localhost:50051", RunReport{Report: report})

	// The note is the line on stderr word for word, the planned rate included.
	if line := StopLine(trip, offered(1700)); !strings.Contains(stopped.String(), "stopped at 12.3s") || !strings.Contains(stopped.String(), line) {
		t.Errorf("the report of a stopped run has no stop line:\n%s", stopped.String())
	}
	if !strings.Contains(stopped.String(), "; the plan offered 1700 rps at that moment; ") {
		t.Errorf("the stop line in the report's notes does not name the planned rate:\n%s", stopped.String())
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
	run.Run.StopPlanned = offered(1700)

	out := writeJSON(t, run)

	if got := out["stop_reason"]; got != StopErrorSpike {
		t.Errorf("stop_reason = %v, want %q", got, StopErrorSpike)
	}

	stop, ok := out["stop"].(map[string]any)
	if !ok {
		t.Fatalf("stop = %#v, want an object", out["stop"])
	}

	want := map[string]any{
		"method": "a.S/One", "failed": 35.0, "total": 40.0, "limit": "5%", "at_us": 12_300_000.0, "in_flight": 800.0,
		"planned_rps": 1700.0,
	}
	for key, w := range want {
		if stop[key] != w {
			t.Errorf("stop.%s = %#v, want %#v", key, stop[key], w)
		}
	}
	if len(stop) != len(want) {
		t.Errorf("stop has %d keys, want %d: %v", len(stop), len(want), stop)
	}
}

// Ground: contract — settings.stop_on_errors echoes the config as written, like
// the other settings: "5%" as "5%", "100%" as "100%" (the report calls it off,
// the echo says what the config says), and null when the key was left out.
func TestJSON_SettingsEchoTheStopLimitAsWritten(t *testing.T) {
	for _, tc := range []struct {
		written string
		want    any
	}{
		{"5%", "5%"},
		{"0.5%", "0.5%"},
		{"100%", "100%"},
		{"", nil},
	} {
		run := jsonRun(echoReport())
		run.Run.Settings = &RunSettings{MaxInFlight: 5000, Connections: 1, Mode: "real", StopOnErrors: tc.written}

		out := writeJSON(t, run)

		settings, ok := out["settings"].(map[string]any)
		if !ok {
			t.Fatalf("%q: settings = %#v, want an object", tc.written, out["settings"])
		}
		got, present := settings["stop_on_errors"]
		if !present || got != tc.want {
			t.Errorf("%q: settings.stop_on_errors = %#v (present %v), want %#v", tc.written, got, present, tc.want)
		}
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

// Ground: contract — the stop line names the rate the plan offered at the
// moment the watch judged, in whole calls per second, by the one rule of the
// settings line's "planned rps" (rounded to the nearest, as %.0f). The words
// say "the plan offered ... at that moment", not "sent" and not "broke at": the
// failed calls may have gone out a timeout earlier, and the watch judges by
// the calls that ended. The clause sits between the limit and the calls in
// flight. No value ends in .5: %.0f rounds a tie to even, and a test of it
// would pin that, not the rule. Mutations "truncate", "%d" and "%.1f" turn it
// red (18, 2999; "18" is not "19").
func TestStopLine_NamesThePlannedRate(t *testing.T) {
	for _, tc := range []struct {
		planned float64
		want    string
	}{
		{1700, "; the plan offered 1700 rps at that moment; "},
		{18.75, "; the plan offered 19 rps at that moment; "},
		{6.25, "; the plan offered 6 rps at that moment; "},
		{2999.6, "; the plan offered 3000 rps at that moment; "},
		{50, "; the plan offered 50 rps at that moment; "},
	} {
		got := StopLine(spikeTrip(t, 35, 40), offered(tc.planned))

		if !strings.Contains(got, tc.want) {
			t.Errorf("planned %v: StopLine = %q, want it to contain %q", tc.planned, got, tc.want)
		}

		limit := strings.Index(got, "over stop_on_errors 5%")
		planned := strings.Index(got, "the plan offered")
		inFlight := strings.Index(got, "800 in flight")
		if limit < 0 || planned < limit || inFlight < planned {
			t.Errorf("planned %v: clauses out of order in %q, want limit, planned rate, calls in flight", tc.planned, got)
		}
	}
}

// Ground: contract — the text rounds the rate itself, once, to whole calls; the
// JSON's three places are for the output only and never feed the text. 1.4996
// is "1" in the line and 1.5 in the JSON; a text made from the JSON's rounded
// number would say "2". Mutation "text from the 3-place value" turns it red.
func TestStopLine_RoundsTheRateOnceNotTwice(t *testing.T) {
	trip := spikeTrip(t, 35, 40)
	planned := offered(1.4996)

	if got := StopLine(trip, planned); !strings.Contains(got, "the plan offered 1 rps at that moment") {
		t.Errorf("StopLine = %q, want it to say 1 rps for 1.4996", got)
	}

	report := echoReport()
	report.Incomplete = true

	run := jsonRun(report)
	run.Outcome = OutcomeIncomplete
	run.Run.Stop = &trip
	run.Run.StopPlanned = planned

	stop, ok := writeJSON(t, run)["stop"].(map[string]any)
	if !ok {
		t.Fatal("stop is not an object")
	}
	if got := stop["planned_rps"]; got != 1.5 {
		t.Errorf("stop.planned_rps = %#v, want 1.5", got)
	}
}

// Ground: boundary — a stop after the plan ended (the timeouts of the tail
// come then) has no rate to name, and "the plan offered 0 rps" would say the
// opposite of what happened. The line says the plan had ended, and no number.
func TestStopLine_AfterThePlanEndedSaysSo(t *testing.T) {
	const want = "stopped at 12.3s: a.S/One failed 87.5% (35 of 40 calls in the last 5s), over stop_on_errors 5%; " +
		"the plan had ended; 800 in flight"

	got := StopLine(spikeTrip(t, 35, 40), nil)
	if got != want {
		t.Errorf("StopLine =\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(got, "rps") || strings.Contains(got, "offered") {
		t.Errorf("a plan that had ended is told as a rate: %q", got)
	}

	// The same through the report's notes.
	report := echoReport()
	report.Incomplete = true

	trip := spikeTrip(t, 35, 40)

	var out strings.Builder
	PrintReport(&out, "localhost:50051", RunReport{Report: report, Stop: &trip})

	if !strings.Contains(out.String(), "over stop_on_errors 5%; the plan had ended; 800 in flight") {
		t.Errorf("the notes of a stop with no planned rate do not say the plan had ended:\n%s", out.String())
	}
}

// Ground: contract — JSON: stop.planned_rps is the rate the plan of the stopped
// method offered at the moment of the stop, a float rounded to three places at
// the output (the same number on every platform), additive within schema 1. A
// stop after the plan ended writes null, never 0: the key is there, the value
// says "no rate". Mutations "0 for null" and "no rounding" turn it red.
func TestJSON_TheStopCarriesItsPlannedRate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		planned *float64
		want    any
	}{
		{"a rate", offered(1700), 1700.0},
		{"rounded to three places", offered(123.4567), 123.457},
		{"a fraction", offered(2.0 / 3.0), 0.667},
		{"the plan had ended", nil, nil},
	} {
		report := echoReport()
		report.Incomplete = true

		trip := spikeTrip(t, 35, 40)
		run := jsonRun(report)
		run.Outcome = OutcomeIncomplete
		run.Run.Stop = &trip
		run.Run.StopPlanned = tc.planned

		stop, ok := writeJSON(t, run)["stop"].(map[string]any)
		if !ok {
			t.Fatalf("%s: stop is not an object", tc.name)
		}

		got, present := stop["planned_rps"]
		if !present {
			t.Errorf("%s: stop has no planned_rps key: %v", tc.name, stop)

			continue
		}
		if got != tc.want {
			t.Errorf("%s: stop.planned_rps = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}
