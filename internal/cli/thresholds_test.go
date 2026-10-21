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
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

const thresholdMethod = "pkg.Svc/Do"

func exactQ(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Exact: true, Defined: true}
}

func passP99() threshold.Result {
	return threshold.Result{
		Method: thresholdMethod, Key: threshold.P99, Verdict: threshold.VerdictPass,
		Limit: 200 * time.Millisecond, Value: exactQ(180 * time.Millisecond),
	}
}

func failP99() threshold.Result {
	return threshold.Result{
		Method: thresholdMethod, Key: threshold.P99, Verdict: threshold.VerdictFail,
		Limit: 200 * time.Millisecond, Value: exactQ(312 * time.Millisecond),
	}
}

func passErrors() threshold.Result {
	return threshold.Result{
		Method: thresholdMethod, Key: threshold.Errors, Verdict: threshold.VerdictPass,
		LimitPercent: "1%", Bad: 12, Total: 6000,
	}
}

func unknownP99(reason threshold.Reason) threshold.Result {
	return threshold.Result{
		Method: thresholdMethod, Key: threshold.P99, Verdict: threshold.VerdictUnknown, Reason: reason,
		Limit: 200 * time.Millisecond, Value: metrics.Quantile{Value: 180 * time.Millisecond, Defined: true},
	}
}

// thresholdRun is a finished run of one method with the verdicts given.
func thresholdRun(results ...threshold.Result) RunReport {
	return RunReport{
		Report: engine.Report{
			Sent: 6000,
			Methods: []engine.MethodReport{{
				Method: thresholdMethod, Sent: 6000, Latencies: 6000, P99: exactQ(180 * time.Millisecond),
				P99WithoutClientWaits: exactQ(180 * time.Millisecond),
			}},
		},
		Thresholds: results,
	}
}

func printedWithLimits(run RunReport) string {
	var buf bytes.Buffer
	PrintReport(&buf, "t:1", run)

	return buf.String()
}

func decodeJSONObject(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, data)
	}

	return out
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

// Ground: contract — one shape for every threshold, so a script reads them
// with one path: the same twelve keys, the ones that do not belong to this
// key null. The numbers are what the text prints: whole microseconds,
// the percent as the config wrote it.
func TestThresholdsJSON_OneFormForEveryKey(t *testing.T) {
	out := writeJSON(t, func() JSONRun {
		run := jsonRun(thresholdRun().Report)
		run.Run.Thresholds = []threshold.Result{
			passP99(),
			{Method: thresholdMethod, Key: threshold.Errors, Verdict: threshold.VerdictFail, LimitPercent: "0.7%", Bad: 8, BadClient: 1, Total: 1000},
		}

		return run
	}())

	rows, _ := field(t, out, "thresholds").([]any)
	if len(rows) != 2 {
		t.Fatalf("thresholds = %v, want 2 entries in the config's order", rows)
	}

	want := []string{"bad", "bad_client", "held_rps", "key", "limit_percent", "limit_rps", "limit_us", "method", "reason", "total", "value", "verdict"}
	for i, row := range rows {
		if got := keysOf(row.(map[string]any)); !slices.Equal(got, want) {
			t.Errorf("thresholds[%d] keys %v, want %v", i, got, want)
		}
	}

	latency := rows[0].(map[string]any)
	if latency["method"] != thresholdMethod || latency["key"] != "p99" || latency["verdict"] != "pass" || latency["reason"] != nil {
		t.Errorf("latency threshold %v: want %s p99 pass without a reason", latency, thresholdMethod)
	}
	if latency["limit_us"] != 200000.0 {
		t.Errorf("limit_us = %v, want 200000", latency["limit_us"])
	}
	value, _ := latency["value"].(map[string]any)
	if value["us"] != 180000.0 || value["lower_bound"] != false {
		t.Errorf("value = %v, want {us 180000, lower_bound false}", latency["value"])
	}
	for _, k := range []string{"limit_percent", "bad", "bad_client", "total", "limit_rps", "held_rps"} {
		if latency[k] != nil {
			t.Errorf("latency threshold has %s = %v, want null", k, latency[k])
		}
	}

	errs := rows[1].(map[string]any)
	if errs["key"] != "errors" || errs["verdict"] != "fail" || errs["limit_percent"] != "0.7%" ||
		errs["bad"] != 8.0 || errs["bad_client"] != 1.0 || errs["total"] != 1000.0 {
		t.Errorf("errors threshold %v: want fail, 0.7%%, bad 8 (client 1) of 1000", errs)
	}
	for _, k := range []string{"limit_us", "value", "limit_rps", "held_rps", "reason"} {
		if errs[k] != nil {
			t.Errorf("errors threshold has %s = %v, want null", k, errs[k])
		}
	}
}

// Ground: contract — the reason is there for unknown and nowhere else; a
// percentile that was not measured is null, not a zero; one that is only a
// lower bound says so.
func TestThresholdsJSON_AnUnknownCarriesItsReason(t *testing.T) {
	run := jsonRun(thresholdRun().Report)
	run.Outcome = OutcomeInvalid
	run.Run.Thresholds = []threshold.Result{
		{Method: thresholdMethod, Key: threshold.P999, Verdict: threshold.VerdictUnknown, Reason: threshold.ReasonFewObservations, Limit: time.Second},
		unknownP99(threshold.ReasonCensored),
	}

	rows, _ := field(t, writeJSON(t, run), "thresholds").([]any)
	if len(rows) != 2 {
		t.Fatalf("thresholds = %v, want 2", rows)
	}

	few := rows[0].(map[string]any)
	if few["verdict"] != "unknown" || few["reason"] != "few_observations" || few["value"] != nil || few["limit_us"] != 1000000.0 {
		t.Errorf("p999 without observations: %v, want unknown, few_observations, no value, limit 1000000", few)
	}

	cut := rows[1].(map[string]any)
	value, _ := cut["value"].(map[string]any)
	if cut["reason"] != "censored" || value["us"] != 180000.0 || value["lower_bound"] != true {
		t.Errorf("p99 among timeouts: %v, want censored with the lower bound 180000", cut)
	}
}

// Ground: contract — `pass:` judges the value in nanoseconds, so a value 400ns
// over a 100us limit is a fail whose value.us (cut at the microsecond) equals
// limit_us, and value.ns is what shows it is over. The verdict is the real
// judge's, not a hand-made result.
func TestThresholdsJSON_AFailOverTheLimitByLessThanAMicrosecond(t *testing.T) {
	const limit = 100 * time.Microsecond

	report := thresholdRun().Report
	report.Methods[0].P99 = exactQ(limit + 400*time.Nanosecond)

	run := jsonRun(report)
	run.Run.Thresholds = threshold.Evaluate(report, threshold.Complete, []threshold.MethodLimits{
		{Name: thresholdMethod, Limits: threshold.Limits{P99: limit}},
	})

	rows, _ := field(t, writeJSON(t, run), "thresholds").([]any)
	if len(rows) != 1 {
		t.Fatalf("thresholds = %v, want the one p99 row", rows)
	}

	row, _ := rows[0].(map[string]any)
	value, _ := row["value"].(map[string]any)

	if row["verdict"] != "fail" || row["reason"] != nil {
		t.Errorf("verdict %v reason %v, want fail without a reason: 100.4us is over 100us", row["verdict"], row["reason"])
	}
	if row["limit_us"] != 100.0 || value["us"] != 100.0 {
		t.Errorf("limit_us %v value.us %v, want both 100 (the value cut at the microsecond)", row["limit_us"], value["us"])
	}
	if value["ns"] != 100400.0 || value["lower_bound"] != false {
		t.Errorf("value %v, want ns 100400 and no lower bound", value)
	}
}

// Ground: contract — a run without limits writes null, the same word as the
// other things the run did not produce; never an empty array that reads as
// "all limits pass".
func TestThresholdsJSON_NullWithoutLimits(t *testing.T) {
	out := writeJSON(t, jsonRun(thresholdRun().Report))

	if got := field(t, out, "thresholds"); got != nil {
		t.Errorf("thresholds = %v, want null for a run with no pass section", got)
	}
}

// Ground: contract — invalid_reasons names a threshold that could not be
// decided, so that a script reading only the reasons sees why the run is
// invalid. A run that was already invalid for another reason, or stopped, has
// its unknowns for that reason and does not get the extra one.
func TestThresholdsJSON_InvalidReasonsNameAnUndecidedThreshold(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome string
		capHit  bool
		results []threshold.Result
		want    []string
	}{
		{"decided by the target's side", OutcomeInvalid, false, []threshold.Result{passErrors(), unknownP99(threshold.ReasonGenerator)}, []string{"thresholds_undecided"}},
		{"few observations", OutcomeInvalid, false, []threshold.Result{unknownP99(threshold.ReasonFewObservations)}, []string{"thresholds_undecided"}},
		{"undecided beside another reason", OutcomeInvalid, true, []threshold.Result{unknownP99(threshold.ReasonInvalid)}, []string{"in_flight_cap"}},
		{"a stopped run", OutcomeIncomplete, false, []threshold.Result{unknownP99(threshold.ReasonIncomplete)}, []string{}},
		{"a failed limit is not undecided", OutcomeComplete, false, []threshold.Result{failP99()}, []string{}},
		{"all passed", OutcomeComplete, false, []threshold.Result{passP99(), passErrors()}, []string{}},
	} {
		run := jsonRun(thresholdRun().Report)
		run.Outcome = tc.outcome
		run.Run.Thresholds = tc.results
		if tc.capHit {
			run.Run.CapHit = &engine.CapHit{At: time.Second, Unsent: 1}
		}

		got, _ := field(t, writeJSON(t, run), "invalid_reasons").([]any)
		var reasons []string
		for _, r := range got {
			reasons = append(reasons, r.(string))
		}
		if len(reasons) == 0 {
			reasons = []string{}
		}
		if !slices.Equal(reasons, tc.want) {
			t.Errorf("%s: invalid_reasons = %v, want %v", tc.name, reasons, tc.want)
		}
	}
}

// Ground: contract — a search writes its verdicts at the top level, beside
// breakpoint, in the same form; the search's own outcome does not change
// because a limit was missed or undecided.
func TestSearchJSON_ThresholdsAreAtTheTopLevel(t *testing.T) {
	res := outcomes["held_all"].res

	run := BreakpointRun{Target: "t:1", Method: thresholdMethod, Plan: bpPlan, Result: res}
	for _, tc := range []struct {
		name    string
		result  threshold.Result
		verdict string
		reason  any
	}{
		{"held enough", threshold.Result{Key: threshold.HeldRPS, Verdict: threshold.VerdictPass, LimitRPS: 300, HeldRPS: 381}, "pass", nil},
		{"held too little", threshold.Result{Key: threshold.HeldRPS, Verdict: threshold.VerdictFail, LimitRPS: 500, HeldRPS: 381}, "fail", nil},
		{"undecided", threshold.Result{Key: threshold.HeldRPS, Verdict: threshold.VerdictUnknown, Reason: threshold.ReasonRunLimit, LimitRPS: 500, HeldRPS: 381}, "unknown", "run_limit"},
	} {
		run.Thresholds = []threshold.Result{tc.result}

		var buf bytes.Buffer
		if err := WriteBreakpointJSON(&buf, run); err != nil {
			t.Fatalf("%s: write: %v", tc.name, err)
		}
		out := decodeJSONObject(t, buf.Bytes())

		rows, _ := field(t, out, "thresholds").([]any)
		if len(rows) != 1 {
			t.Fatalf("%s: thresholds = %v, want 1", tc.name, rows)
		}
		row := rows[0].(map[string]any)
		if row["key"] != "held_rps" || row["verdict"] != tc.verdict || row["reason"] != tc.reason ||
			row["limit_rps"] != float64(tc.result.LimitRPS) || row["held_rps"] != 381.0 || row["method"] != nil {
			t.Errorf("%s: %v, want held_rps %s (%v) limit %d held 381 and no method", tc.name, row, tc.verdict, tc.reason, tc.result.LimitRPS)
		}
		for _, k := range []string{"limit_us", "value", "limit_percent", "bad", "bad_client", "total"} {
			if row[k] != nil {
				t.Errorf("%s: %s = %v, want null", tc.name, k, row[k])
			}
		}
		if got := field(t, out, "breakpoint", "outcome"); got != "held_all" {
			t.Errorf("%s: breakpoint.outcome = %v, want held_all unchanged", tc.name, got)
		}
	}

	run.Thresholds = nil
	var buf bytes.Buffer
	if err := WriteBreakpointJSON(&buf, run); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := field(t, decodeJSONObject(t, buf.Bytes()), "thresholds"); got != nil {
		t.Errorf("thresholds = %v, want null for a search with no pass section", got)
	}
}

// block is the text of the report from the Thresholds line to its end.
func block(t *testing.T, out string) string {
	t.Helper()

	i := strings.Index(out, "Thresholds:")
	if i < 0 {
		t.Fatalf("no Thresholds block in:\n%s", out)
	}

	return out[i:]
}

// Ground: contract — the block opens with the verdict of the run, and the
// verdict is the worst of the rows: a fail over an unknown over a pass; the
// counts are of the rows that are not passes.
func TestThresholdsText_TheHeaderIsTheWorstRow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []threshold.Result
		says    []string
		denies  []string
	}{
		{"all pass", []threshold.Result{passP99(), passErrors()}, []string{"passed"}, []string{"FAILED", "not decided"}},
		{"one fails", []threshold.Result{failP99(), passErrors(), unknownP99(threshold.ReasonGenerator)}, []string{"FAILED", "1 of 3"}, []string{"passed", "not decided"}},
		{"one unknown", []threshold.Result{passP99(), passErrors(), unknownP99(threshold.ReasonGenerator)}, []string{"not decided", "1 of 3"}, []string{"passed", "FAILED"}},
	} {
		out := printedWithLimits(thresholdRun(tc.results...))
		header, _, _ := strings.Cut(block(t, out), "\n")

		for _, s := range tc.says {
			if !strings.Contains(header, s) {
				t.Errorf("%s: header %q lacks %q", tc.name, header, s)
			}
		}
		for _, s := range tc.denies {
			if strings.Contains(header, s) {
				t.Errorf("%s: header %q has %q", tc.name, header, s)
			}
		}
	}
}

// rowOf is the one line of the block about the method and key.
func rowOf(t *testing.T, out, key string) string {
	t.Helper()

	var found []string
	for line := range strings.Lines(block(t, out)) {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == thresholdMethod && fields[1] == key {
			found = append(found, strings.TrimSpace(line))
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d rows for %s in the block, want 1:\n%s", len(found), key, block(t, out))
	}

	return found[0]
}

func hasAll(t *testing.T, what, text string, tokens ...string) {
	t.Helper()

	for _, s := range tokens {
		if !strings.Contains(text, s) {
			t.Errorf("%s %q lacks %q", what, text, s)
		}
	}
}

// Ground: contract — a row says the limit, the verdict and what was measured,
// and an unknown says why in words: the generator was late, the timeouts were
// in the tail, the stream waited. Tokens, not the layout.
func TestThresholdsText_ARowSaysLimitVerdictAndValue(t *testing.T) {
	out := printedWithLimits(thresholdRun(failP99(), passErrors()))

	hasAll(t, "p99 row", rowOf(t, out, "p99"), "<=", "200ms", "fail", "312ms")
	hasAll(t, "errors row", rowOf(t, out, "errors"), "<=", "1%", "pass", "0.2%", "12", "6000")

	for reason, word := range map[threshold.Reason]string{
		threshold.ReasonGenerator: "generator",
		threshold.ReasonStream:    "stream",
		threshold.ReasonCensored:  "timeouts in the tail",
	} {
		row := rowOf(t, printedWithLimits(thresholdRun(unknownP99(reason))), "p99")
		hasAll(t, string(reason)+" row", row, "unknown", word, "180ms")
	}
}

// Ground: contract — the share of errors shows what it is made of: calls that
// never got a connection count in it and are named, so that "10.3%" can be
// checked against the failed column; refusals of the config and the client
// are named too, since they are why a verdict may be undecided.
func TestThresholdsText_ErrorsShowTheirParts(t *testing.T) {
	run := thresholdRun(threshold.Result{
		Method: thresholdMethod, Key: threshold.Errors, Verdict: threshold.VerdictFail,
		LimitPercent: "1%", Bad: 3100, Total: 30000,
	})
	run.Methods[0].Sent, run.Methods[0].Failed, run.Methods[0].NotSent, run.Methods[0].NotSentConnection = 27000, 100, 3000, 3000
	run.NotSent, run.NotSentConnection = 3000, 3000

	hasAll(t, "errors row", rowOf(t, printedWithLimits(run), "errors"), "fail", "10.3%", "100", "3000", "no connection", "30000")

	run = thresholdRun(threshold.Result{
		Method: thresholdMethod, Key: threshold.Errors, Verdict: threshold.VerdictUnknown, Reason: threshold.ReasonClientErrors,
		LimitPercent: "1%", Bad: 20, BadClient: 20, Total: 1000,
	})
	hasAll(t, "errors row", rowOf(t, printedWithLimits(run), "errors"), "unknown", "2.0%", "20", "client", "1000")
}

// Ground: contract — the block is the last thing in the report, after the
// notes, and a run with no limits has no block at all: the report is the same
// bytes as before the section existed, an empty list included.
func TestThresholdsText_LastBlockAndNoneWithoutLimits(t *testing.T) {
	run := thresholdRun(passP99(), passErrors())
	run.Unchecked = []Unchecked{{Method: thresholdMethod, Err: errors.New("reflection is not used")}}

	out := printedWithLimits(run)
	rest := block(t, out)
	for line := range strings.Lines(rest) {
		if line != "\n" && !strings.HasPrefix(line, "Thresholds:") && !strings.HasPrefix(line, "  ") {
			t.Errorf("line %q after the Thresholds block starts: it is not the last block", line)
		}
	}
	if i := strings.Index(out, "not checked before the run"); i < 0 || i > strings.Index(out, "Thresholds:") {
		t.Errorf("the block is not after the other notes:\n%s", out)
	}

	none := printedWithLimits(thresholdRun())
	if strings.Contains(none, "Thresholds") {
		t.Errorf("a run with no limits prints a block:\n%s", none)
	}
	empty := thresholdRun()
	empty.Thresholds = []threshold.Result{}
	if got := printedWithLimits(empty); got != none {
		t.Errorf("an empty list changes the report:\n%s\n--- vs ---\n%s", got, none)
	}
}

// Ground: contract — a search's block is a row of the plan's own limit and
// what the search held, under the same header.
func TestThresholdsText_ASearchRowSaysLimitAndWhatWasHeld(t *testing.T) {
	for _, tc := range []struct {
		name   string
		res    threshold.Result
		header string
		row    []string
	}{
		{"pass", threshold.Result{Key: threshold.HeldRPS, Verdict: threshold.VerdictPass, LimitRPS: 300, HeldRPS: 381}, "passed", []string{"held_rps", ">=", "300", "pass", "held 381"}},
		{"fail", threshold.Result{Key: threshold.HeldRPS, Verdict: threshold.VerdictFail, LimitRPS: 500, HeldRPS: 381}, "FAILED", []string{"held_rps", ">=", "500", "fail", "held 381"}},
		{"unknown", threshold.Result{Key: threshold.HeldRPS, Verdict: threshold.VerdictUnknown, Reason: threshold.ReasonRunLimit, LimitRPS: 500, HeldRPS: 381}, "not decided", []string{"held_rps", ">=", "500", "unknown", "held 381"}},
	} {
		var buf bytes.Buffer
		PrintBreakpoint(&buf, BreakpointRun{
			Target: "t:1", Method: thresholdMethod, Plan: bpPlan, Result: outcomes["held_all"].res,
			Thresholds: []threshold.Result{tc.res},
		})

		rest := block(t, buf.String())
		header, rows, _ := strings.Cut(rest, "\n")
		hasAll(t, tc.name+" header", header, tc.header)
		hasAll(t, tc.name+" row", rows, tc.row...)
	}

	if out := printedSearch(outcomes["held_all"].res); strings.Contains(out, "Thresholds") {
		t.Errorf("a search with no pass section prints a block:\n%s", out)
	}
}
