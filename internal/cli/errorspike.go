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
	"fmt"
	"math"

	"github.com/yhgrwav/leettest/pkg/errstop"
	shr "github.com/yhgrwav/leettest/pkg/share"
)

// StopErrorSpike is the JSON stop_reason of a run that load.stop_on_errors
// ended.
const StopErrorSpike = "error_spike"

// jsonStop is where load.stop_on_errors ended a run, on whose numbers: the
// method, its failed and total calls over the last five seconds, the limit as
// written, the time since the start in whole microseconds and the calls in
// flight at that moment, and the rate the method's plan offered then (calls
// per second, three places); null when the plan had already ended.
type jsonStop struct {
	Method     string   `json:"method"`
	Failed     int      `json:"failed"`
	Total      int      `json:"total"`
	Limit      string   `json:"limit"`
	AtUS       int64    `json:"at_us"`
	InFlight   int      `json:"in_flight"`
	PlannedRPS *float64 `json:"planned_rps"`
}

// StopLine is the line printed when the run is stopped, and again in the
// report's notes: when, which method, its share to the digit that puts it
// over the limit, the counts, the rate the plan offered at that moment and
// the number in flight. The time is told by one rule, in seconds with one
// decimal place. planned is the rate of the method's plan at the moment of
// the stop, to whole calls; nil says the plan had ended (the trip is the
// moment of judging, not of sending: the failed calls may have left earlier).
func StopLine(t errstop.Trip, planned *float64) string {
	plan := "the plan had ended"
	if planned != nil {
		plan = fmt.Sprintf("the plan offered %.0f rps at that moment", *planned)
	}

	return fmt.Sprintf("stopped at %.1fs: %s failed %s (%d of %d calls in the last %ds), over stop_on_errors %s; %s; %d in flight",
		t.At.Seconds(), displayMethod(t.Method), shr.Format(t.Failed, t.Total, t.Limit, shr.Breaks),
		t.Failed, t.Total, errstop.Window, t.Limit.Text, plan, t.InFlight)
}

// stopNotes is the stop line as a note of the report, none for a run nobody
// stopped.
func stopNotes(run RunReport) []string {
	if run.Stop == nil {
		return nil
	}

	return []string{StopLine(*run.Stop, run.StopPlanned)}
}

// stopLimitLine is the settings echo of the limit; empty when nobody wrote
// one. 100% is told as off, because that is what it does.
func stopLimitLine(written string) string {
	switch written {
	case "":
		return ""
	case "100%":
		return "stop_on_errors: off (100%)"
	default:
		return "stop_on_errors: " + written
	}
}

// stopJSON is the stop of the report; nil for a run nobody stopped. The planned
// rate is rounded here, at the output, to three places like the seconds' one.
func stopJSON(t *errstop.Trip, planned *float64) *jsonStop {
	if t == nil {
		return nil
	}

	var rate *float64
	if planned != nil {
		rounded := math.Round(*planned*1000) / 1000
		rate = &rounded
	}

	return &jsonStop{
		Method: t.Method, Failed: t.Failed, Total: t.Total, Limit: t.Limit.Text,
		AtUS: micros(t.At), InFlight: t.InFlight, PlannedRPS: rate,
	}
}

// StopSeqOverflow is the JSON stop_reason of a run that a seq which did not fit
// its field ended.
const StopSeqOverflow = "seq_overflow"

// stopReasonJSON is the stop_reason of the report.
func stopReasonJSON(run RunReport) *string {
	var reason string

	switch {
	case run.Stop != nil:
		reason = StopErrorSpike
	case run.SeqOverflow != nil:
		reason = StopSeqOverflow
	default:
		return nil
	}

	return &reason
}

// writtenLimit is the limit for the JSON echo: null when left out.
func writtenLimit(written string) *string {
	if written == "" {
		return nil
	}

	return &written
}
