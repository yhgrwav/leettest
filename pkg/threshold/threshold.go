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

package threshold

import (
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
	"github.com/yhgrwav/leettest/pkg/share"
)

// Key names a limit: a latency percentile, the share of failed calls, or the
// rate a search held.
type Key string

const (
	P50     Key = "p50"
	P90     Key = "p90"
	P95     Key = "p95"
	P99     Key = "p99"
	P999    Key = "p999"
	Errors  Key = "errors"
	HeldRPS Key = "held_rps"
)

// Percent is a share written in the config; see share.Percent.
type Percent = share.Percent

// Limits are one call's limits. A latency limit is whole microseconds, a
// zero one is not set; Errors is nil when not set.
type Limits struct {
	P50, P90, P95, P99, P999 time.Duration
	Errors                   *Percent
}

// SearchLimit is a search's limit: the rate it must hold at least; 0 is none.
type SearchLimit struct {
	HeldRPS int
}

// MethodLimits are the limits of the call to Name, as the report names it.
type MethodLimits struct {
	Name   string
	Limits Limits
}

// Verdict is what a limit came to.
type Verdict string

const (
	VerdictPass    Verdict = "pass"
	VerdictFail    Verdict = "fail"
	VerdictUnknown Verdict = "unknown"
)

// Reason says why a limit is unknown; empty for pass and fail.
type Reason string

const (
	ReasonIncomplete      Reason = "incomplete"
	ReasonInvalid         Reason = "invalid"
	ReasonFewObservations Reason = "few_observations"
	ReasonRunLimit        Reason = "run_limit"
	ReasonGenerator       Reason = "generator"
	ReasonStream          Reason = "stream"
	ReasonClientErrors    Reason = "client_errors"
	ReasonNothingSent     Reason = "nothing_sent"
	ReasonCensored        Reason = "censored"
)

// State is how the run ended, decided by the caller: limits are checked only
// on a Complete one.
type State int

const (
	Complete State = iota
	Incomplete
	Invalid
)

// Result is the verdict on one limit, with the numbers it was decided on.
type Result struct {
	// Method is empty for the search's limit.
	Method  string
	Key     Key
	Verdict Verdict
	Reason  Reason
	// Limit and Value are a latency limit and the percentile compared with
	// it; Value.Defined is false when there was none.
	Limit time.Duration
	Value metrics.Quantile
	// LimitPercent is the limit as written; Bad, BadClient and Total are the
	// share's parts: Bad counts every failure, BadClient those the target did
	// not decide (rejected requests, client errors, our bad responses).
	LimitPercent string
	Bad          int
	BadClient    int
	Total        int
	// LimitRPS and HeldRPS are the search's limit and what it held.
	LimitRPS int
	HeldRPS  int
}

// Evaluate checks each method's limits against the run's report, one result
// per limit set, in the order of limits: a method's p50 to p999, then errors.
// Limits are checked only when state is Complete; otherwise each is unknown.
func Evaluate(report engine.Report, state State, limits []MethodLimits) []Result {
	var results []Result

	for i := range limits {
		var m engine.MethodReport
		for j := range report.Methods {
			if report.Methods[j].Method == limits[i].Name {
				m = report.Methods[j]

				break
			}
		}

		results = appendMethod(results, report, &m, state, limits[i])
	}

	return results
}

func appendMethod(results []Result, report engine.Report, m *engine.MethodReport, state State, ml MethodLimits) []Result {
	lim := ml.Limits
	runSide, runReason := runSideOf(report, m)

	for _, l := range []struct {
		key   Key
		limit time.Duration
		value metrics.Quantile
	}{
		{P50, lim.P50, m.P50}, {P90, lim.P90, m.P90}, {P95, lim.P95, m.P95},
		{P99, lim.P99, m.P99}, {P999, lim.P999, m.P999},
	} {
		if l.limit <= 0 {
			continue
		}

		r := Result{Method: ml.Name, Key: l.key, Limit: l.limit, Value: l.value}

		switch {
		case state != Complete:
			r.Verdict, r.Reason = VerdictUnknown, notComplete(state)
		case runSide:
			r.Verdict, r.Reason = VerdictUnknown, runReason
		default:
			r.Verdict, r.Reason = latency(l.limit, l.value)
		}

		results = append(results, r)
	}

	if lim.Errors != nil {
		r := Result{Method: ml.Name, Key: Errors, LimitPercent: lim.Errors.Text}

		if state != Complete {
			r.Verdict, r.Reason = VerdictUnknown, notComplete(state)
		} else {
			judgeShare(&r, *lim.Errors, m, runSide, runReason)
		}

		results = append(results, r)
	}

	return results
}

func notComplete(state State) Reason {
	if state == Invalid {
		return ReasonInvalid
	}

	return ReasonIncomplete
}

// runSideOf says the run's own limit, not the target's, shapes the method's
// numbers: the generator or a stream held calls back, or calls went unsent for
// a reason other than the connection. The reason is always from the closed
// list: the wait verdict's when it names the run, else what the method lost.
func runSideOf(report engine.Report, m *engine.MethodReport) (bool, Reason) {
	cause, side, ok := report.WaitVerdict()
	runWait := ok && side == engine.SideRun

	if !runWait && m.NotSent-m.NotSentConnection <= 0 {
		return false, ""
	}

	switch {
	case runWait && cause == engine.WaitStream:
		return true, ReasonStream
	case runWait:
		return true, ReasonGenerator
	case m.NotSentGenerator > 0:
		return true, ReasonGenerator
	case m.NotSentStream > 0:
		return true, ReasonStream
	default:
		return true, ReasonGenerator
	}
}

// latency compares the whole microseconds the report prints with the limit.
// A value that is only a lower bound fails once the bound is over the limit
// and decides nothing otherwise.
func latency(limit time.Duration, q metrics.Quantile) (Verdict, Reason) {
	switch {
	case !q.Defined:
		return VerdictUnknown, ReasonFewObservations
	case q.Value.Microseconds() > limit.Microseconds():
		return VerdictFail, ""
	case !q.Exact:
		return VerdictUnknown, ReasonCensored
	default:
		return VerdictPass, ""
	}
}

// judgeShare decides the failure share (share.Decide). Every failure and every
// call lost to the connection counts for the pass; for the fail only those the
// target decided: requests the config or the client got wrong say nothing of
// the target.
func judgeShare(r *Result, limit Percent, m *engine.MethodReport, runSide bool, runReason Reason) {
	r.Total = m.Sent + m.NotSentConnection
	r.Bad = m.Failed + m.NotSentConnection
	r.BadClient = m.Rejected.Count + m.ClientError + m.BadResponse.Count

	if r.Total == 0 {
		r.Verdict, r.Reason = VerdictUnknown, ReasonNothingSent

		return
	}

	switch share.Decide(r.Bad, r.BadClient, r.Total, limit) {
	case share.Holds:
		r.Verdict = VerdictPass
		if runSide {
			r.Verdict, r.Reason = VerdictUnknown, runReason
		}
	case share.Breaks:
		r.Verdict = VerdictFail
	default:
		r.Verdict, r.Reason = VerdictUnknown, ReasonClientErrors
	}
}

// EvaluateSearch checks the search's result against its limit.
func EvaluateSearch(res breakpoint.Result, limit SearchLimit) Result {
	r := Result{Key: HeldRPS, LimitRPS: limit.HeldRPS, HeldRPS: res.Held}

	switch {
	case res.Outcome == breakpoint.Stopped:
		r.Verdict, r.Reason = VerdictUnknown, ReasonIncomplete
	case res.Outcome == breakpoint.Invalid:
		r.Verdict, r.Reason = VerdictUnknown, ReasonInvalid
	case res.Held >= limit.HeldRPS:
		r.Verdict = VerdictPass
	case res.Outcome == breakpoint.RunLimit:
		r.Verdict, r.Reason = VerdictUnknown, ReasonRunLimit
	default:
		r.Verdict = VerdictFail
	}

	return r
}

// Overall is fail if any result failed, else unknown if any is, else pass.
func Overall(results []Result) Verdict {
	overall := VerdictPass

	for i := range results {
		switch results[i].Verdict {
		case VerdictFail:
			return VerdictFail
		case VerdictUnknown:
			overall = VerdictUnknown
		case VerdictPass:
		}
	}

	return overall
}
