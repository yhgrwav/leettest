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
	"errors"
	"io"
	"math"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// JSONSchemaVersion changes when a field is renamed, removed or changes type.
// Added fields and added enum values keep it. testdata/schema_v1.txt is the
// schema it names.
const JSONSchemaVersion = 1

// Outcomes of a run as JSON names them; each matches one exit code.
const (
	OutcomeComplete   = "complete"
	OutcomeInvalid    = "invalid"
	OutcomeIncomplete = "incomplete"
)

// reasonThresholdsUndecided is the invalid reason of a run whose own pass:
// limits could not be decided; leettest compare does not count it.
const reasonThresholdsUndecided = "thresholds_undecided"

// JSONRun is what the JSON report needs beyond the engine's report.
type JSONRun struct {
	Target    string
	Name      string
	Version   string
	Outcome   string
	StartedAt time.Time
	Run       RunReport
}

// JSONReport is the report as --output json writes it. A pointer is the only
// thing that may be null: a value the run did not produce, never a zero that
// could pass for a measurement. Durations are whole microseconds.
type JSONReport struct {
	SchemaVersion int `json:"schema_version"`
	// Mode is "run" here; a breaking-point search writes "breakpoint" and
	// another object (BreakpointReport).
	Mode            string `json:"mode"`
	LeetTestVersion string `json:"leettest_version"`
	Target          string `json:"target"`
	// Name is what the run is called: the config's name, else the one
	// service, else the config file's name (ServiceLabel). Always a string.
	Name string `json:"name"`
	// MethodsSource is where the methods came from: reflection, proto or
	// protoset; null when the run resolved none (-fake).
	MethodsSource *string `json:"methods_source"`
	Outcome       string  `json:"outcome"`
	// InvalidReasons are why the run is invalid: in_flight_cap, nothing_measured,
	// clock_step, thresholds_undecided.
	InvalidReasons []string `json:"invalid_reasons"`
	// TailWaitCause is the client-side wait that set the tail: generator, stream
	// or connection; null without that verdict.
	TailWaitCause *string `json:"tail_wait_cause"`
	// ClockStepNS is the host clock step: every latency and wait is +/- it.
	ClockStepNS int64  `json:"clock_step_ns"`
	StartedAt   string `json:"started_at"`
	DurationUS  int64  `json:"duration_us"`
	PlannedUS   int64  `json:"planned_us"`
	WarmupUS    int64  `json:"warmup_us"`
	// Settings echo what the run was started with, as configured.
	Settings jsonSettings `json:"settings"`
	// Passport is the config and the machine of the run, at the top of a run's
	// report; a step inside a search has none and the key is left out.
	Passport          *jsonPassport   `json:"passport,omitempty"`
	Sent              int             `json:"sent"`
	Failed            int             `json:"failed"`
	Aborted           int             `json:"aborted"`
	NotSent           int             `json:"not_sent"`
	NotSentGenerator  int             `json:"not_sent_generator"`
	NotSentStream     int             `json:"not_sent_stream"`
	NotSentConnection int             `json:"not_sent_connection"`
	WarmupSent        int             `json:"warmup_sent"`
	WarmupFailed      int             `json:"warmup_failed"`
	WarmupNotSent     int             `json:"warmup_not_sent"`
	CapHit            *jsonCapHit     `json:"cap_hit"`
	StartLag          jsonStartLag    `json:"start_lag"`
	Connections       *jsonConns      `json:"connections"`
	ClientWaits       jsonClientWaits `json:"client_waits"`
	GeneratorCPU      *jsonGenCPU     `json:"generator_cpu"`
	Methods           []jsonMethod    `json:"methods"`
	Unchecked         []jsonUnchecked `json:"unchecked"`
	// Thresholds are the verdicts on the limits the config set, in its order;
	// null when it set none.
	Thresholds *[]jsonThreshold `json:"thresholds"`
	// Notes are the text report's notes: for people, and reworded freely.
	Notes []string `json:"notes"`
}

// jsonSettings is a run's configured settings: the cap and the connections
// as set, the warm-up and the ramp in whole microseconds.
type jsonSettings struct {
	MaxInFlight int   `json:"max_in_flight"`
	Connections int   `json:"connections"`
	WarmupUS    int64 `json:"warmup_us"`
	RampUS      int64 `json:"ramp_us"`
	// Compression is "gzip" or null.
	Compression *string `json:"compression"`
}

// settingsOf is the echo as JSON has it; nil, which nobody told, gives zeros
// and is never filled in from what the run measured.
func settingsOf(s *RunSettings) jsonSettings {
	if s == nil {
		return jsonSettings{}
	}

	return jsonSettings{MaxInFlight: s.MaxInFlight, Connections: s.Connections, WarmupUS: micros(s.Warmup), RampUS: micros(s.Ramp), Compression: compressionOf(s.Compression)}
}

// compressionOf is the configured compression, null for none.
func compressionOf(name string) *string {
	if name == "" {
		return nil
	}

	return &name
}

// jsonRequestBytes is the mean request body in bytes: the message and what
// went on the wire.
type jsonRequestBytes struct {
	Raw  float64 `json:"raw"`
	Wire float64 `json:"wire"`
}

type jsonQuantile struct {
	US int64 `json:"us"`
	// NS is the value in whole nanoseconds, as the histogram holds it; US is
	// the same value truncated to microseconds.
	NS         int64 `json:"ns"`
	LowerBound bool  `json:"lower_bound"`
}

// UnmarshalJSON reads a quantile of a report written before `ns` existed as
// its microseconds in nanoseconds, never as 0 ns beside a nonzero `us`.
func (q *jsonQuantile) UnmarshalJSON(data []byte) error {
	var read struct {
		US         int64  `json:"us"`
		NS         *int64 `json:"ns"`
		LowerBound bool   `json:"lower_bound"`
	}
	if err := json.Unmarshal(data, &read); err != nil {
		return err
	}

	*q = jsonQuantile{US: read.US, LowerBound: read.LowerBound}
	if read.NS != nil {
		q.NS = *read.NS

		return nil
	}

	q.NS = min(read.US, math.MaxInt64/1000) * 1000

	return nil
}

type jsonCapHit struct {
	AtUS         int64 `json:"at_us"`
	Unsent       int   `json:"unsent"`
	OverDeadline int   `json:"over_deadline"`
}

type jsonStartLag struct {
	P99 *jsonQuantile `json:"p99"`
	Max *jsonQuantile `json:"max"`
}

type jsonConns struct {
	Open       int `json:"open"`
	Reconnects int `json:"reconnects"`
	// FirstLimit and LastLimit are the one connection's limits; with several
	// they are null, the limits are per connection.
	FirstLimit   *int `json:"first_limit"`
	LastLimit    *int `json:"last_limit"`
	LimitChanges int  `json:"limit_changes"`
	// Resolved is the addresses the target stands for; null with one
	// connection.
	Resolved *[]string `json:"resolved"`
	// InFlightLimit is the most calls the target lets the run have in flight:
	// the one connection's limit, or the sum of all of them; null while any
	// connection announced none.
	InFlightLimit *int `json:"in_flight_limit"`
	// PerConnection is one object per connection; null with one.
	PerConnection *[]jsonLink `json:"per_connection"`
}

// jsonLink is one connection of several. Failed counts the calls that failed
// on it and those that never went out because it was not ready;
// StreamWaited and NotSentStream are the calls that waited for a stream and
// those that expired waiting.
type jsonLink struct {
	Address       string        `json:"address"`
	Calls         int           `json:"calls"`
	Failed        int           `json:"failed"`
	StreamWaited  int           `json:"stream_waited"`
	NotSentStream int           `json:"not_sent_stream"`
	P99           *jsonQuantile `json:"p99"`
	FirstLimit    *int          `json:"first_limit"`
	LastLimit     *int          `json:"last_limit"`
	LimitChanges  int           `json:"limit_changes"`
}

// limitsOf is a connection's first and last limit as JSON tells them: both
// null when it never announced one, the last null when only a change was seen.
func limitsOf(announced bool, first, last uint32, changes int) (firstLimit, lastLimit *int) {
	// 0 is a limit a target may announce: unannounced is null, not 0.
	if announced || changes > 0 {
		f, l := int(first), int(last)
		firstLimit = &f
		if announced {
			lastLimit = &l
		}
	}

	return firstLimit, lastLimit
}

type jsonLatency struct {
	Min *jsonQuantile `json:"min"`
	P50 *jsonQuantile `json:"p50"`
	P90 *jsonQuantile `json:"p90"`
	P95 *jsonQuantile `json:"p95"`
	P99 *jsonQuantile `json:"p99"`
	// P999 is null while the method has fewer observations than
	// metrics.MinCount(0.999).
	P999 *jsonQuantile `json:"p999"`
	Max  *jsonQuantile `json:"max"`
}

type jsonAnswers struct {
	Count int           `json:"count"`
	P50   *jsonQuantile `json:"p50"`
	P90   *jsonQuantile `json:"p90"`
	P95   *jsonQuantile `json:"p95"`
	P99   *jsonQuantile `json:"p99"`
	Max   *jsonQuantile `json:"max"`
}

type jsonCode struct {
	Code       string `json:"code"`
	Count      int    `json:"count"`
	FromTarget bool   `json:"from_target"`
}

// jsonDataset is how a call used its dataset file; the file as the config
// wrote it, and the counts of engine.DatasetReport.
type jsonDataset struct {
	File string `json:"file"`
	// SHA256 is the sha256 of the file as it is on disk, hex: what sha256sum prints.
	SHA256  string `json:"sha256"`
	Records int    `json:"records"`
	Used    int    `json:"used"`
	UsedMax int    `json:"used_max"`
	// Order is "file" or "random"; Seed the seed of a random order, null for the file's.
	Order string  `json:"order"`
	Seed  *uint64 `json:"seed"`
	// Reuse is false when the call sends each record at most once.
	Reuse bool `json:"reuse"`
}

type jsonMethod struct {
	Method string `json:"method"`
	// InvalidReason says the method measured nothing about load: request_error,
	// client_error, bad_response or mixed; null when it did.
	InvalidReason     *string  `json:"invalid_reason"`
	Sent              int      `json:"sent"`
	Failed            int      `json:"failed"`
	Aborted           int      `json:"aborted"`
	NotSent           int      `json:"not_sent"`
	NotSentGenerator  int      `json:"not_sent_generator"`
	NotSentStream     int      `json:"not_sent_stream"`
	NotSentConnection int      `json:"not_sent_connection"`
	WarmupSent        int      `json:"warmup_sent"`
	WarmupFailed      int      `json:"warmup_failed"`
	WarmupNotSent     int      `json:"warmup_not_sent"`
	RPS               *float64 `json:"rps"`
	TimeoutUS         int64    `json:"timeout_us"`
	PlannedRPSLow     int      `json:"planned_rps_low"`
	PlannedRPSHigh    int      `json:"planned_rps_high"`
	PlannedDurationUS int64    `json:"planned_duration_us"`
	// RequestBytes is null without compression.
	RequestBytes          *jsonRequestBytes `json:"request_bytes"`
	Latency               jsonLatency       `json:"latency"`
	P99WithoutClientWaits *jsonQuantile     `json:"p99_without_client_waits"`
	Observations          int               `json:"observations"`
	Censored              int               `json:"censored"`
	InvalidLatencies      int               `json:"invalid_latencies"`
	TimedOut              int               `json:"timed_out"`
	TimedOutAfterWait     int               `json:"timed_out_after_wait"`
	CutOff                int               `json:"cut_off"`
	Unreachable           int               `json:"unreachable"`
	Unclassified          int               `json:"unclassified"`
	OutsideTimeline       int               `json:"outside_timeline"`
	RequestError          jsonAnswers       `json:"request_error"`
	Overload              jsonAnswers       `json:"overload"`
	Failure               jsonAnswers       `json:"failure"`
	BadResponse           jsonAnswers       `json:"bad_response"`
	ClientError           int               `json:"client_error"`
	FailureCodes          []jsonCode        `json:"failure_codes"`
	SilentFromS           *int              `json:"silent_from_s"`
	SilentSentRPS         *int              `json:"silent_sent_rps"`
	SilentPlannedRPSLow   *int              `json:"silent_planned_rps_low"`
	SilentPlannedRPSHigh  *int              `json:"silent_planned_rps_high"`
	LastAnswerAtUS        *int64            `json:"last_answer_at_us"`
	Dataset               *jsonDataset      `json:"dataset"`
	Seconds               []jsonSecond      `json:"seconds"`
}

// jsonSecond counts each category under its Category.Name, but for success.
type jsonSecond struct {
	Warmup             bool  `json:"warmup"`
	Begun              int   `json:"begun"`
	Succeeded          int   `json:"succeeded"`
	Overload           int   `json:"overload"`
	Failure            int   `json:"failure"`
	ClientError        int   `json:"client_error"`
	BadResponse        int   `json:"bad_response"`
	RequestError       int   `json:"request_error"`
	TimedOut           int   `json:"timed_out"`
	NotSentGenerator   int   `json:"not_sent_generator"`
	NotSentStream      int   `json:"not_sent_stream"`
	NotSentConnection  int   `json:"not_sent_connection"`
	Unreachable        int   `json:"unreachable"`
	CutOff             int   `json:"cut_off"`
	Aborted            int   `json:"aborted"`
	Unclassified       int   `json:"unclassified"`
	InFlight           int   `json:"in_flight"`
	LagCalls           int   `json:"lag_calls"`
	LagSumUS           int64 `json:"lag_sum_us"`
	LagMaxUS           int64 `json:"lag_max_us"`
	ObservedCalls      int   `json:"observed_calls"`
	ObservedLagSumUS   int64 `json:"observed_lag_sum_us"`
	TransportWaitSumUS int64 `json:"transport_wait_sum_us"`
	ServiceTimeSumUS   int64 `json:"service_time_sum_us"`
}

type jsonUnchecked struct {
	Method string `json:"method"`
	// Reason is for scripts: reflection_off or reflection_failed. Error is
	// for people and changes with grpc-go.
	Reason string `json:"reason"`
	Error  string `json:"error"`
}

// canonicalCodes are the gRPC codes as grpc/grpc doc/statuscodes.md names
// them, keyed by grpc-go's String.
var canonicalCodes = func() map[string]string {
	names := []string{
		"OK", "CANCELLED", "UNKNOWN", "INVALID_ARGUMENT", "DEADLINE_EXCEEDED", "NOT_FOUND",
		"ALREADY_EXISTS", "PERMISSION_DENIED", "RESOURCE_EXHAUSTED", "FAILED_PRECONDITION",
		"ABORTED", "OUT_OF_RANGE", "UNIMPLEMENTED", "INTERNAL", "UNAVAILABLE", "DATA_LOSS",
		"UNAUTHENTICATED",
	}
	m := make(map[string]string, len(names))
	for i, name := range names {
		m[codes.Code(i).String()] = name
	}

	return m
}()

func micros(d time.Duration) int64 { return d.Microseconds() }

func quantile(q metrics.Quantile) *jsonQuantile {
	if !q.Defined {
		return nil
	}

	return &jsonQuantile{US: micros(q.Value), NS: q.Value.Nanoseconds(), LowerBound: !q.Exact}
}

func answers(r engine.RefusalLatency) jsonAnswers {
	return jsonAnswers{
		Count: r.Count, P50: quantile(r.P50), P90: quantile(r.P90), P95: quantile(r.P95),
		P99: quantile(r.P99), Max: quantile(r.Max),
	}
}

// NewJSONReport builds the JSON report of a finished run.
func NewJSONReport(run JSONRun) JSONReport {
	run.Run.Target = run.Target
	r := &run.Run.Report

	out := jsonRunFacts(run)
	out.StartLag = startLagJSON(r)
	out.CapHit = capHitJSON(r.CapHit)
	out.Connections = connectionsOf(r.Connections)
	out.Methods = methodsJSON(r.Methods, r.Warmup, out.Settings.Compression)
	out.Notes = notesJSON(run.Run)
	out.InvalidReasons = invalidReasonsJSON(run.Run, out.Methods)
	out.Unchecked = uncheckedJSON(run.Run.Unchecked)

	return out
}

func jsonRunFacts(run JSONRun) JSONReport {
	r := &run.Run.Report

	return JSONReport{
		SchemaVersion:     JSONSchemaVersion,
		Mode:              ModeRun,
		LeetTestVersion:   run.Version,
		Target:            run.Target,
		Name:              run.Name,
		MethodsSource:     run.Run.MethodsSource.kind(),
		Outcome:           run.Outcome,
		StartedAt:         run.StartedAt.UTC().Format(time.RFC3339Nano),
		DurationUS:        micros(r.Duration),
		PlannedUS:         micros(r.Planned),
		WarmupUS:          micros(r.Warmup),
		Settings:          settingsOf(run.Run.Settings),
		Passport:          passportJSON(run.Run.Passport),
		Sent:              r.Sent,
		Failed:            r.Failed,
		Aborted:           r.Aborted,
		NotSent:           r.NotSent,
		NotSentGenerator:  r.NotSentGenerator,
		NotSentStream:     r.NotSentStream,
		NotSentConnection: r.NotSentConnection,
		WarmupSent:        r.WarmupSent,
		WarmupFailed:      r.WarmupFailed,
		WarmupNotSent:     r.WarmupNotSent,
		TailWaitCause:     tailWaitCause(*r),
		ClockStepNS:       int64(run.Run.ClockStep),
		ClientWaits: jsonClientWaits{
			GeneratorCalls: r.GeneratorCauseCalls, StreamCalls: r.StreamCauseCalls, ConnectionCalls: r.ConnectionCauseCalls,
			GeneratorTailCalls: r.GeneratorTailCalls, StreamTailCalls: r.StreamTailCalls, ConnectionTailCalls: r.ConnectionTailCalls,
		},
		GeneratorCPU: genCPUOf(run.Run.GeneratorCPU),
		Thresholds:   thresholdsJSON(run.Run.Thresholds),
	}
}

func startLagJSON(r *engine.Report) jsonStartLag {
	out := jsonStartLag{P99: quantile(r.StartLagP99)}
	// The maximum is exact, but there is none without a call.
	if r.StartLagP99.Defined {
		out.Max = &jsonQuantile{US: micros(r.StartLagMax), NS: r.StartLagMax.Nanoseconds()}
	}

	return out
}

func capHitJSON(c *engine.CapHit) *jsonCapHit {
	if c == nil {
		return nil
	}

	return &jsonCapHit{AtUS: micros(c.At), Unsent: c.Unsent, OverDeadline: c.OverDeadline}
}

func connectionsOf(c *engine.Connections) *jsonConns {
	if c == nil {
		return nil
	}

	conns := &jsonConns{Open: c.Open, Reconnects: c.Reconnects, LimitChanges: c.LimitChanges}

	if severalConnections(c) {
		resolved := append([]string{}, c.Resolved...)
		links := make([]jsonLink, len(c.Each))

		for i := range c.Each {
			l := &c.Each[i]
			links[i] = jsonLink{
				Address: l.Address, Calls: l.Calls, Failed: l.Failed, StreamWaited: l.StreamWaited,
				NotSentStream: l.NotSentStream, P99: quantile(l.P99), LimitChanges: l.LimitChanges,
			}
			links[i].FirstLimit, links[i].LastLimit = limitsOf(l.LimitAnnounced, l.FirstLimit, l.LastLimit, l.LimitChanges)
		}

		conns.Resolved, conns.PerConnection = &resolved, &links

		if c.InFlightAnnounced {
			limit := c.InFlightLimit
			conns.InFlightLimit = &limit
		}
	} else {
		conns.FirstLimit, conns.LastLimit = limitsOf(c.LimitAnnounced, c.FirstLimit, c.LastLimit, c.LimitChanges)
		conns.InFlightLimit = conns.LastLimit
	}

	return conns
}

func methodsJSON(methods []engine.MethodReport, warmup time.Duration, compression *string) []jsonMethod {
	out := make([]jsonMethod, 0, len(methods))
	for i := range methods {
		m := jsonMethodOf(&methods[i], warmup)
		// Without compression raw and wire are the same number: null.
		if b := methods[i].RequestBytes; b != nil && compression != nil {
			m.RequestBytes = &jsonRequestBytes{Raw: b.Raw, Wire: b.Wire}
		}
		out = append(out, m)
	}

	return out
}

func notesJSON(run RunReport) []string {
	notes := runNotes(run)
	if note := uncheckedNote(run.Unchecked); note != "" {
		notes = append(notes, note)
	}
	// Notes read as the text report prints them.
	for i := range notes {
		notes[i] = asciiText(notes[i])
	}

	return notes
}

func invalidReasonsJSON(run RunReport, methods []jsonMethod) []string {
	out := make([]string, 0, 2)
	if run.CapHit != nil {
		out = append(out, "in_flight_cap")
	}
	for i := range methods {
		if methods[i].InvalidReason != nil {
			out = append(out, "nothing_measured")

			break
		}
	}
	if ClockTooCoarse(run) {
		out = append(out, "clock_step")
	}
	if thresholdsUndecided(run.Thresholds) {
		out = append(out, reasonThresholdsUndecided)
	}

	return out
}

func uncheckedJSON(unchecked []Unchecked) []jsonUnchecked {
	out := make([]jsonUnchecked, 0, len(unchecked))
	for _, u := range unchecked {
		reason := "reflection_failed"
		if errors.Is(u.Err, descriptor.ErrReflectionUnsupported) {
			reason = "reflection_off"
		}
		msg := ""
		if u.Err != nil {
			msg = u.Err.Error()
		}
		out = append(out, jsonUnchecked{Method: u.Method, Reason: reason, Error: msg})
	}

	return out
}

func jsonMethodOf(m *engine.MethodReport, warmup time.Duration) jsonMethod {
	out := jsonMethod{
		Method: m.Method, InvalidReason: invalidReason(m), Sent: m.Sent, Failed: m.Failed, Aborted: m.Aborted,
		NotSent: m.NotSent, NotSentGenerator: m.NotSentGenerator, NotSentStream: m.NotSentStream,
		NotSentConnection: m.NotSentConnection,
		WarmupSent:        m.WarmupSent, WarmupFailed: m.WarmupFailed, WarmupNotSent: m.WarmupNotSent,
		TimeoutUS: micros(m.Timeout), PlannedRPSLow: m.RPSLow, PlannedRPSHigh: m.RPSHigh,
		PlannedDurationUS: micros(m.PlannedDuration),
		Latency: jsonLatency{
			Min: quantile(m.Min), P50: quantile(m.P50), P90: quantile(m.P90),
			P95: quantile(m.P95), P99: quantile(m.P99), P999: quantile(m.P999), Max: quantile(m.Max),
		},
		P99WithoutClientWaits: quantile(m.P99WithoutClientWaits),
		Observations:          m.Latencies, Censored: m.Censored, InvalidLatencies: m.Invalid,
		TimedOut: m.TimedOut, TimedOutAfterWait: m.TimedOutAfterWait,
		CutOff: m.CutOff, Unreachable: m.Unanswered, Unclassified: m.Unclassified,
		OutsideTimeline: m.OutsideTimeline,
		RequestError:    answers(m.Rejected), Overload: answers(m.Overload), Failure: answers(m.Failure),
		BadResponse: answers(m.BadResponse), ClientError: m.ClientError,
		FailureCodes: make([]jsonCode, 0, len(m.FailureCodes)),
		SilentFromS:  m.SilentFrom,
		Seconds:      make([]jsonSecond, 0, len(m.Seconds)),
	}
	// The rate belongs to a silence: none without one.
	if m.SilentFrom != nil {
		out.SilentSentRPS = &m.SentRPS
		// No stage in that second: unknown, which a 0 would hide.
		if m.SilentPlannedHigh > 0 {
			out.SilentPlannedRPSLow, out.SilentPlannedRPSHigh = &m.SilentPlannedLow, &m.SilentPlannedHigh
		}
	}
	// No call, no rate: a zero would read as a target that took none. NaN or
	// Inf would fail the whole encoding and leave stdout empty.
	if m.Sent > 0 && !math.IsNaN(m.RPS) && !math.IsInf(m.RPS, 0) {
		rps := m.RPS
		out.RPS = &rps
	}
	if m.LastAnswerAt != nil {
		at := micros(*m.LastAnswerAt)
		out.LastAnswerAtUS = &at
	}
	if d := m.Dataset; d != nil {
		out.Dataset = &jsonDataset{File: d.File, SHA256: d.SHA256, Records: d.Records, Used: d.Used, UsedMax: d.UsedMax, Order: "file", Reuse: !d.Once}
		if d.Random {
			out.Dataset.Order, out.Dataset.Seed = "random", &d.Seed
		}
	}
	for _, c := range m.FailureCodes {
		name, ok := canonicalCodes[c.Code]
		if !ok {
			name = c.Code
		}
		out.FailureCodes = append(out.FailureCodes, jsonCode{Code: name, Count: c.Count, FromTarget: c.FromTarget})
	}
	for i := range m.Seconds {
		s := &m.Seconds[i]
		out.Seconds = append(out.Seconds, jsonSecond{
			Warmup: time.Duration(i)*time.Second < warmup,
			Begun:  s.Begun, Succeeded: s.Succeeded, Overload: s.Overload, Failure: s.Failure,
			ClientError: s.ClientError, BadResponse: s.BadResponse, RequestError: s.RequestFailed,
			TimedOut: s.TimedOut, NotSentGenerator: s.NotSentGenerator, NotSentStream: s.NotSentStream,
			NotSentConnection: s.NotSentConnection, Unreachable: s.Unanswered, CutOff: s.CutOff,
			Aborted: s.Aborted, Unclassified: s.Unclassified, InFlight: s.InFlight,
			LagCalls: s.LagCalls, LagSumUS: micros(s.LagSum), LagMaxUS: micros(s.LagMax),
			ObservedCalls: s.ObservedCalls, ObservedLagSumUS: micros(s.ObservedLagSum),
			TransportWaitSumUS: micros(s.TransportWaitSum), ServiceTimeSumUS: micros(s.ServiceTimeSum),
		})
	}

	return out
}

// WriteJSON writes the report as one JSON object and a newline.
func WriteJSON(w io.Writer, run JSONRun) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	return enc.Encode(NewJSONReport(run))
}

// jsonGenCPU is how much of the CPU available to the process the generator
// used. Share and each of Seconds are of all the available cores and may pass
// 1: time in system calls runs outside the Ps. A second no reading covers is
// null, never 0.
type jsonGenCPU struct {
	AvailableCores float64    `json:"available_cores"`
	Share          float64    `json:"share"`
	BusySeconds    int        `json:"busy_seconds"`
	FirstBusy      *int       `json:"first_busy_s"`
	Seconds        []*float64 `json:"seconds"`
}

func genCPUOf(g *GeneratorCPU) *jsonGenCPU {
	if g == nil {
		return nil
	}

	return &jsonGenCPU{
		AvailableCores: g.AvailableCores, Share: g.Share, BusySeconds: g.BusySeconds,
		FirstBusy: g.FirstBusy, Seconds: g.Seconds,
	}
}

// jsonClientWaits count the calls that waited over the floor for each cause,
// in all and among those that set each method's p99: the numbers tail_wait_cause
// ranks.
type jsonClientWaits struct {
	GeneratorCalls      int `json:"generator_calls"`
	StreamCalls         int `json:"stream_calls"`
	ConnectionCalls     int `json:"connection_calls"`
	GeneratorTailCalls  int `json:"generator_tail_calls"`
	StreamTailCalls     int `json:"stream_tail_calls"`
	ConnectionTailCalls int `json:"connection_tail_calls"`
}

// tailWaitCause is engine.Report.WaitVerdict's cause, as JSON names it.
func tailWaitCause(report engine.Report) *string {
	c, _, ok := report.WaitVerdict()
	if !ok {
		return nil
	}
	name := string(c)

	return &name
}

// invalidReason is why a method measured nothing about load, by the rule of
// invalidNote, or nil when it did.
func invalidReason(m *engine.MethodReport) *string {
	if invalidNote(m, "") == "" {
		return nil
	}
	reason := "mixed"
	switch m.Sent {
	case m.Rejected.Count:
		reason = "request_error"
	case m.ClientError:
		reason = "client_error"
	case m.BadResponse.Count:
		reason = "bad_response"
	}

	return &reason
}
