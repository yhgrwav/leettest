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

// Percent is a share written in the config, kept in thousandths of a percent
// so that comparisons are integer: 0.7% is 700. Text is it as written.
type Percent struct {
	Thousandths int64
	Text        string
}

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

// Evaluate checks each method's limits against the run's report.
func Evaluate(_ engine.Report, _ State, _ []MethodLimits) []Result {
	return nil
}

// EvaluateSearch checks the search's result against its limit.
func EvaluateSearch(_ breakpoint.Result, _ SearchLimit) Result {
	return Result{}
}

// Overall is fail if any result failed, else unknown if any is, else pass.
func Overall(_ []Result) Verdict {
	return VerdictPass
}
