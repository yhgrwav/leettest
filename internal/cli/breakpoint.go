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
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
)

// Modes of the JSON report.
const (
	ModeRun        = "run"
	ModeBreakpoint = "breakpoint"
)

// BreakpointRun is what the breaking-point report needs: the search's plan
// and result, and the run's header.
type BreakpointRun struct {
	Target    string
	Version   string
	Method    string
	StartedAt time.Time
	Plan      breakpoint.Plan
	Result    breakpoint.Result
	// Runs, when it has one per Result.Steps, are the runs with what only
	// the CLI knows (the clock step); otherwise each run is its engine report.
	Runs []RunReport
}

// PlanLine is the line printed before the first step: how many steps and the
// longest the search can take, connecting within connect included, rounded
// up to a second.
func PlanLine(p breakpoint.Plan, connect time.Duration) string {
	rates, err := p.Rates()
	if err != nil {
		return ""
	}
	worst, _ := p.Worst()
	at := (worst + connect + time.Second - 1).Truncate(time.Second)

	return fmt.Sprintf("breakpoint: up to %d steps, at most %v", len(rates), at)
}

// outcomeNames are the JSON names of the outcomes: a closed list.
var outcomeNames = map[breakpoint.Outcome]string{
	breakpoint.BrokeBetween:   "broke",
	breakpoint.BrokeAtFirst:   "broke_at_first",
	breakpoint.HeldThroughout: "held_all",
	breakpoint.RunLimit:       "run_limit",
	breakpoint.Stopped:        "stopped",
	breakpoint.Invalid:        "invalid",
}

// causeNames are the JSON names of the causes: a closed list.
var causeNames = map[breakpoint.Cause]string{
	breakpoint.CauseErrors:        "errors",
	breakpoint.CauseP99Limit:      "p99_limit",
	breakpoint.CauseP99VsBase:     "p99_vs_base",
	breakpoint.CauseConnection:    "connection",
	breakpoint.CauseNoRecovery:    "no_recovery",
	breakpoint.CauseGenerator:     "generator",
	breakpoint.CauseInFlightCap:   "in_flight_cap",
	breakpoint.CauseStreamLimit:   "stream_limit",
	breakpoint.CauseStreamWait:    "stream_wait",
	breakpoint.CauseClockStep:     "clock_step",
	breakpoint.CauseRequestErrors: "request_errors",
}

func causeName(c breakpoint.Cause) *string {
	if name, ok := causeNames[c]; ok {
		return &name
	}

	return nil
}

// window is the measured part of a run of kind at rps: its hold less the
// settle.
func window(p breakpoint.Plan, step *breakpoint.Step) time.Duration {
	hold := p.Hold
	if step.Kind == breakpoint.Probe {
		hold = breakpoint.ProbeHold(step.RPS)
	}

	return hold - p.Settle
}

// sentRPS is the rate the generator sent in the measured window, rounded
// down: a short run never prints as full.
func sentRPS(p breakpoint.Plan, step *breakpoint.Step) int {
	w := window(p, step)
	if w <= 0 {
		return 0
	}

	return int(time.Duration(step.Report.Sent) * time.Second / w)
}

// lastWhy is the why of the last run, the one that ended the search.
func lastWhy(res breakpoint.Result) (breakpoint.Step, bool) {
	if len(res.Steps) == 0 {
		return breakpoint.Step{}, false
	}

	return res.Steps[len(res.Steps)-1], true
}

// headline is the search's answer in one line, and a second line with the
// criterion where the outcome has one.
func headline(res breakpoint.Result) (head, criterion string) {
	last, _ := lastWhy(res)
	switch res.Outcome {
	case breakpoint.BrokeBetween:
		return fmt.Sprintf("held %d rps, broke at %d rps", res.Held, res.Broke), brokeLine(res)
	case breakpoint.BrokeAtFirst:
		return fmt.Sprintf("broke at the first step, %d rps: the limit is at or below it", res.Broke), brokeLine(res)
	case breakpoint.HeldThroughout:
		return fmt.Sprintf("held every step up to %d rps: the limit is above it", res.Held), ""
	case breakpoint.RunLimit:
		if res.Held == 0 {
			return fmt.Sprintf("the run gave out at the first step (%d rps); nothing was learned about the target", res.Broke), last.Why
		}

		return fmt.Sprintf("the run gave out at %d rps, not the target: %s; the target held %d rps", res.Broke, last.Why, res.Held), ""
	case breakpoint.Stopped:
		switch {
		case len(res.Steps) <= 1:
			return "stopped at the first step; nothing was learned about the target", ""
		case res.Held == 0:
			return fmt.Sprintf("stopped at %d rps (%v); nothing held so far", last.RPS, last.Kind), ""
		}

		return fmt.Sprintf("stopped at %d rps (%v); held %d rps so far", last.RPS, last.Kind, res.Held), ""
	case breakpoint.Invalid:
		reason := "every call a request error"
		if res.Cause == breakpoint.CauseClockStep {
			reason = "clock step"
		}

		return fmt.Sprintf("invalid search: %s at %d rps; no breaking point is reported", reason, last.RPS), last.Why
	}

	return "no outcome", ""
}

// brokeLine names the criterion of the run that confirmed the break.
func brokeLine(res breakpoint.Result) string {
	for i := len(res.Steps) - 1; i >= 0; i-- {
		if s := res.Steps[i]; s.Broken && s.RPS == res.Broke {
			return fmt.Sprintf("%d rps: %s", s.RPS, s.Why)
		}
	}

	return ""
}

// verdict is a run's last column, read off its judgement.
func verdict(res *breakpoint.Result, i int) string {
	s := res.Steps[i]
	switch {
	case res.Outcome == breakpoint.Stopped && i == len(res.Steps)-1:
		return "stopped"
	case s.Broken:
		return "broke: " + s.Why
	case s.Cause == breakpoint.CauseClockStep || s.Cause == breakpoint.CauseRequestErrors:
		return "invalid: " + s.Why
	case s.Why != "":
		return "run limit: " + s.Why
	}

	return "held"
}

// PrintBreakpoint writes the search's text report: the answer, every run a
// row, the notes.
func PrintBreakpoint(w io.Writer, run BreakpointRun) {
	w = asciiWriter{w: w}
	res := run.Result

	fmt.Fprintf(w, "breaking point: %s\n", run.Method)
	head, why := headline(res)
	fmt.Fprintf(w, "  %s\n", head)
	if why != "" {
		fmt.Fprintf(w, "  %s\n", why)
	}
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  kind\tplanned\tsent\tfailed\tp99\tverdict")
	for i := range res.Steps {
		s := &res.Steps[i]
		p99 := "-"
		if len(s.Report.Methods) > 0 && s.Report.Methods[0].P99.Defined {
			p99 = formatQuantile(s.Report.Methods[0].P99)
		}
		fmt.Fprintf(tw, "  %v\t%d\t%d\t%d\t%s\t%s\n", s.Kind, s.RPS, sentRPS(run.Plan, s), s.Report.Failed, p99, verdict(&res, i))
	}
	_ = tw.Flush()

	if len(res.Notes) > 0 {
		fmt.Fprintln(w, "\nnotes:")
		for _, n := range res.Notes {
			fmt.Fprintf(w, "  - %s\n", n)
		}
	}
}

// BreakpointReport is the search as --output json writes it: mode
// "breakpoint", none of a plain run's top-level fields.
type BreakpointReport struct {
	SchemaVersion   int            `json:"schema_version"`
	Mode            string         `json:"mode"`
	LeetTestVersion string         `json:"leettest_version"`
	Target          string         `json:"target"`
	Method          string         `json:"method"`
	StartedAt       string         `json:"started_at"`
	Breakpoint      jsonBreakpoint `json:"breakpoint"`
}

type jsonBreakpoint struct {
	// Outcome: broke, broke_at_first, held_all, run_limit, stopped, invalid.
	Outcome  string `json:"outcome"`
	HeldRPS  *int   `json:"held_rps"`
	BrokeRPS *int   `json:"broke_rps"`
	// Why is the cause of the run that ended the search, from a closed list;
	// the numbers are in notes.
	Why   *string     `json:"why"`
	Notes []string    `json:"notes"`
	Runs  []jsonRunBP `json:"runs"`
}

type jsonRunBP struct {
	Kind       string     `json:"kind"`
	PlannedRPS int        `json:"planned_rps"`
	SentRPS    int        `json:"sent_rps"`
	Broken     bool       `json:"broken"`
	Why        *string    `json:"why"`
	Report     JSONReport `json:"report"`
}

func rate(n int) *int {
	if n == 0 {
		return nil
	}

	return &n
}

// NewBreakpointReport builds the search's JSON report.
func NewBreakpointReport(run BreakpointRun) BreakpointReport {
	res := run.Result
	bp := jsonBreakpoint{
		Outcome: outcomeNames[res.Outcome],
		Why:     causeName(res.Cause),
		Notes:   append([]string{}, res.Notes...),
		Runs:    make([]jsonRunBP, 0, len(res.Steps)),
	}
	switch res.Outcome {
	case breakpoint.BrokeBetween, breakpoint.RunLimit:
		bp.HeldRPS, bp.BrokeRPS = rate(res.Held), rate(res.Broke)
	case breakpoint.BrokeAtFirst:
		bp.BrokeRPS = rate(res.Broke)
	case breakpoint.HeldThroughout, breakpoint.Stopped:
		bp.HeldRPS = rate(res.Held)
	}
	if line := brokeLine(res); line != "" && res.Outcome != breakpoint.Invalid {
		bp.Notes = append([]string{line}, bp.Notes...)
	}

	for i := range res.Steps {
		s := &res.Steps[i]
		r := RunReport{Report: s.Report}
		if len(run.Runs) == len(res.Steps) {
			r = run.Runs[i]
		}
		outcome := OutcomeComplete
		switch {
		case s.Cause == breakpoint.CauseClockStep || s.Cause == breakpoint.CauseRequestErrors:
			outcome = OutcomeInvalid
		case s.Report.Incomplete:
			outcome = OutcomeIncomplete
		}
		bp.Runs = append(bp.Runs, jsonRunBP{
			Kind: s.Kind.String(), PlannedRPS: s.RPS, SentRPS: sentRPS(run.Plan, s),
			Broken: s.Broken, Why: causeName(s.Cause),
			Report: NewJSONReport(JSONRun{Target: run.Target, Version: run.Version, Outcome: outcome, StartedAt: s.Report.StartedAt, Run: r}),
		})
	}

	return BreakpointReport{
		SchemaVersion: JSONSchemaVersion, Mode: ModeBreakpoint,
		LeetTestVersion: run.Version, Target: run.Target, Method: run.Method,
		StartedAt:  run.StartedAt.UTC().Format(time.RFC3339Nano),
		Breakpoint: bp,
	}
}

// WriteBreakpointJSON writes the search's report as one JSON object and a
// newline.
func WriteBreakpointJSON(w io.Writer, run BreakpointRun) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	return enc.Encode(NewBreakpointReport(run))
}
