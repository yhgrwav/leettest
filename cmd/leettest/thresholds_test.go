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

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

// writePassConfig writes a one-call config for the fake target: rps for
// duration, with the call's own lines (timeout, pass) as given.
func writePassConfig(t *testing.T, rps int, duration, callLines string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(closedPort(t))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	cfg := fmt.Sprintf(`app:
  target:
    ip: %s
    port: %s
  tls: false
load:
  calls:
    - method: %s
      rps: %d
      duration: %s
%s`, host, port, checkMethod, rps, duration, callLines)

	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// runFake runs the config against the fake target with a 200 ms answer and no
// jitter, so the p99 is 200 ms and a bit on any machine. The flags after are
// the test's own and win.
func runFake(t *testing.T, cfg string, flags ...string) (res result, report map[string]any) {
	t.Helper()

	args := append([]string{"-fake", "-fake-delay", "200ms", "-fake-jitter", "0", "-output", "json"}, flags...)
	res = runCLI(t.Context(), t, 30*time.Second, append(args, "-c", cfg)...)
	if res.stdout == "" {
		return res, nil
	}

	return res, decodeOnly(t, res.stdout)
}

// verdicts are the thresholds of the JSON report by key.
func verdicts(t *testing.T, out map[string]any) map[string]map[string]any {
	t.Helper()

	rows, ok := jsonAt(t, out, "thresholds").([]any)
	if !ok {
		t.Fatalf("thresholds = %v, want an array", out["thresholds"])
	}

	byKey := map[string]map[string]any{}
	for _, r := range rows {
		row := r.(map[string]any)
		byKey[row["key"].(string)] = row
	}

	return byKey
}

func wantExit(t *testing.T, res result, code int) {
	t.Helper()

	if got := exitCode(res.err); got != code {
		t.Fatalf("exit code %d (%v), want %d\nstderr: %s", got, res.err, code, res.stderr)
	}
}

func wantVerdict(t *testing.T, rows map[string]map[string]any, key, verdict string) {
	t.Helper()

	row, ok := rows[key]
	if !ok {
		t.Fatalf("no %s threshold in %v", key, rows)
	}
	if row["verdict"] != verdict {
		t.Errorf("%s: verdict %v (reason %v), want %s", key, row["verdict"], row["reason"], verdict)
	}
}

const (
	p99Within = "      timeout: 2s\n      pass:\n        p99: 1s\n        errors: 1%\n"
	p99Over   = "      timeout: 2s\n      pass:\n        p99: 50ms\n        errors: 1%\n"
)

// Ground: contract — the whole path on the fake target, a p99 of 200 ms and a
// bit: a limit above it passes and the code is 0, with the verdicts in the
// report; a limit below it is exit 4 and the run is still a complete one.
func TestThresholds_AP99WithinTheLimitExits0AndOverItExits4(t *testing.T) {
	res, out := runFake(t, writePassConfig(t, 250, "600ms", p99Within))
	wantExit(t, res, 0)
	rows := verdicts(t, out)
	wantVerdict(t, rows, "p99", "pass")
	wantVerdict(t, rows, "errors", "pass")
	if out["outcome"] != "complete" {
		t.Errorf("outcome = %v, want complete", out["outcome"])
	}
	if us, _ := rows["p99"]["limit_us"].(float64); us != 1e6 {
		t.Errorf("limit_us = %v, want 1000000", rows["p99"]["limit_us"])
	}

	res, out = runFake(t, writePassConfig(t, 250, "600ms", p99Over))
	wantExit(t, res, 4)
	if !errors.Is(res.err, ErrThresholdsFailed) {
		t.Errorf("err = %v, want ErrThresholdsFailed", res.err)
	}
	rows = verdicts(t, out)
	wantVerdict(t, rows, "p99", "fail")
	wantVerdict(t, rows, "errors", "pass")
	if out["outcome"] != "complete" {
		t.Errorf("outcome = %v, want complete: the run finished, the limit was missed", out["outcome"])
	}
	if got := jsonAt(t, out, "invalid_reasons").([]any); len(got) != 0 {
		t.Errorf("invalid_reasons = %v, want none for a missed limit", got)
	}
}

// Ground: contract — the text report ends with the block, with the verdict
// the code stands for, and the error line says which kind of failure it was.
func TestThresholds_TheTextReportEndsWithTheBlock(t *testing.T) {
	cfg := writePassConfig(t, 250, "600ms", "      timeout: 2s\n      pass:\n        errors: 50%\n")
	res := runCLI(t.Context(), t, 30*time.Second, "-fake", "-fake-delay", "50ms", "-fake-fail-ratio", "1", "-c", cfg)

	wantExit(t, res, 4)
	i := strings.Index(res.stdout, "Thresholds:")
	if i < 0 || !strings.Contains(res.stdout[i:], "FAILED") || !strings.Contains(res.stdout[i:], "errors") {
		t.Errorf("stdout lacks the failed Thresholds block:\n%s", res.stdout)
	}
	if i >= 0 && strings.Contains(res.stdout[:i], "FAILED") {
		t.Errorf("FAILED before the block: it is the last thing in the report")
	}
}

// Ground: boundary — a share of failures exactly at the limit passes, one
// over it fails. All the calls fail on the fake target (the share is 100%),
// and none (0%): the limits 100% and 0% are exactly on them, 99.9% is under
// the first.
func TestThresholds_ErrorsExactlyOnTheLimitPass(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fail  string
		limit string
		code  int
		want  string
	}{
		{"all failed, limit 100%", "1", "100%", 0, "pass"},
		{"all failed, limit 99.9%", "1", "99.9%", 4, "fail"},
		{"none failed, limit 0%", "0", "0%", 0, "pass"},
	} {
		cfg := writePassConfig(t, 250, "600ms", "      timeout: 2s\n      pass:\n        errors: "+tc.limit+"\n")
		res, out := runFake(t, cfg, "-fake-delay", "20ms", "-fake-fail-ratio", tc.fail)

		wantExit(t, res, tc.code)
		wantVerdict(t, verdicts(t, out), "errors", tc.want)
		if row := verdicts(t, out)["errors"]; row["limit_percent"] != tc.limit {
			t.Errorf("%s: limit_percent = %v, want %q as written", tc.name, row["limit_percent"], tc.limit)
		}
	}
}

// Ground: contract — calls that timed out are in the tail with a lower bound
// of the timeout: over a 50 ms limit, so the limit is missed (4), not
// undecided; the bound is a lower bound in the report. Every call times out
// here, so errors: 100% is the limit that lets only the latency decide.
func TestThresholds_TimeoutsInTheTailMissTheLimit(t *testing.T) {
	cfg := writePassConfig(t, 250, "600ms", "      timeout: 100ms\n      pass:\n        p99: 50ms\n        errors: 100%\n")
	res, out := runFake(t, cfg, "-fake-delay", "3s")

	wantExit(t, res, 4)
	rows := verdicts(t, out)
	wantVerdict(t, rows, "p99", "fail")
	wantVerdict(t, rows, "errors", "pass")
	if value, _ := rows["p99"]["value"].(map[string]any); value["lower_bound"] != true {
		t.Errorf("value = %v, want a lower bound", rows["p99"]["value"])
	}
}

// Ground: contract — a limit that cannot be decided makes the run invalid,
// exit 2, and the JSON says which: p999 needs 1000 answers, and half the calls
// fail, so a 1500-call run holds about 750 of them. A limit that is missed in
// the same run takes the code to 4: a miss is a finding, an unknown is not.
func TestThresholds_AnUndecidedLimitIsExit2AndAMissOutranksIt(t *testing.T) {
	cfg := writePassConfig(t, 1000, "1500ms", "      timeout: 3s\n      pass:\n        p999: 2s\n        errors: 100%\n")
	res, out := runFake(t, cfg, "-fake-fail-ratio", "0.5")

	wantExit(t, res, 2)
	rows := verdicts(t, out)
	wantVerdict(t, rows, "p999", "unknown")
	if rows["p999"]["reason"] != "few_observations" {
		t.Errorf("reason = %v, want few_observations", rows["p999"]["reason"])
	}
	if out["outcome"] != "invalid" {
		t.Errorf("outcome = %v, want invalid", out["outcome"])
	}
	if got := jsonAt(t, out, "invalid_reasons").([]any); !slices.Equal(got, []any{"thresholds_undecided"}) {
		t.Errorf("invalid_reasons = %v, want [thresholds_undecided]", got)
	}

	cfg = writePassConfig(t, 1000, "1500ms", "      timeout: 3s\n      pass:\n        p999: 2s\n        errors: 10%\n")
	res, out = runFake(t, cfg, "-fake-fail-ratio", "0.5")

	wantExit(t, res, 4)
	rows = verdicts(t, out)
	wantVerdict(t, rows, "errors", "fail")
	wantVerdict(t, rows, "p999", "unknown")
	if out["outcome"] != "complete" {
		t.Errorf("outcome = %v, want complete", out["outcome"])
	}
}

// Ground: contract — a run stopped by Ctrl-C is 3 whatever the limits would
// say: they are not checked, each is unknown for that reason, and the run
// is not invalid for it.
func TestThresholds_AStoppedRunChecksNothing(t *testing.T) {
	res := runSignalled(t, "      timeout: 300ms\n      pass:\n        p99: 200ms\n        errors: 100%\n",
		func(stops, _ chan<- struct{}) { stops <- struct{}{} }, "-output", "json")

	wantExit(t, res, 3)
	out := decodeOnly(t, res.stdout)
	rows := verdicts(t, out)
	for _, key := range []string{"p99", "errors"} {
		wantVerdict(t, rows, key, "unknown")
		if rows[key]["reason"] != "incomplete" {
			t.Errorf("%s: reason = %v, want incomplete", key, rows[key]["reason"])
		}
	}
	if out["outcome"] != "incomplete" {
		t.Errorf("outcome = %v, want incomplete", out["outcome"])
	}
	if got := jsonAt(t, out, "invalid_reasons").([]any); len(got) != 0 {
		t.Errorf("invalid_reasons = %v, want none for a stopped run", got)
	}
}

// Ground: contract — a run with no pass section is as it was: no block in
// the text, and `thresholds` null in the JSON, the same word as for what the
// run did not produce, with exit 0 however the target answered.
func TestThresholds_WithoutPassNothingChanges(t *testing.T) {
	cfg := writePassConfig(t, 250, "600ms", "      timeout: 2s\n")

	res, out := runFake(t, cfg, "-fake-fail-ratio", "1")
	wantExit(t, res, 0)
	if got := jsonAt(t, out, "thresholds"); got != nil {
		t.Errorf("thresholds = %v, want null", got)
	}

	res = runCLI(t.Context(), t, 30*time.Second, "-fake", "-fake-fail-ratio", "1", "-c", cfg)
	wantExit(t, res, 0)
	if strings.Contains(res.stdout, "Thresholds") {
		t.Errorf("a run with no pass section prints a block:\n%s", res.stdout)
	}
}

// Ground: contract — a config that cannot be checked is exit 1 before the run,
// naming the key; no report.
func TestThresholds_ABadPassSectionIsExit1(t *testing.T) {
	for name, tc := range map[string]struct{ lines, word string }{
		"latency without errors": {"      timeout: 2s\n      pass:\n        p99: 200ms\n", "errors"},
		"unknown key":            {"      timeout: 2s\n      pass:\n        p98: 200ms\n        errors: 1%\n", "p98"},
		"limit over the timeout": {"      timeout: 100ms\n      pass:\n        p99: 200ms\n        errors: 1%\n", "timeout"},
		"percent without sign":   {"      timeout: 2s\n      pass:\n        errors: 0.5\n", "errors"},
	} {
		res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-c", writePassConfig(t, 250, "600ms", tc.lines))

		wantExit(t, res, 1)
		if res.err == nil || !strings.Contains(res.err.Error(), tc.word) || res.stdout != "" {
			t.Errorf("%s: err %v stdout %q, want an error naming %q and no report", name, res.err, res.stdout, tc.word)
		}
	}
}

// Ground: contract — in a search the limit is on the rate held. The fake
// target never breaks, so the search holds every step up to 79: a limit at 63
// passes (0); a p99_limit under the fake's latency breaks the first step,
// nothing is held, and the same limit is missed (4). The search's own outcome
// stays what it was.
func TestThresholds_ASearchIsDecidedByWhatItHeld(t *testing.T) {
	section := func(extra string) string {
		return "  breakpoint:\n    from: 50\n    to: 80\n    settle: 50ms\n    hold: 300ms\n" + extra + "    pass:\n      held_rps: 63\n"
	}

	res := runCLI(t.Context(), t, 60*time.Second, "-fake", "-output", "json",
		"-c", writeSearchOf(t, closedPort(t), section(""), "200ms", checkMethod))
	wantExit(t, res, 0)
	out := decodeOnly(t, res.stdout)
	rows := verdicts(t, out)
	wantVerdict(t, rows, "held_rps", "pass")
	if rows["held_rps"]["limit_rps"] != 63.0 || rows["held_rps"]["held_rps"] != 79.0 {
		t.Errorf("held_rps threshold %v, want limit 63, held 79", rows["held_rps"])
	}
	if got := jsonAt(t, out, "breakpoint", "outcome"); got != "held_all" {
		t.Errorf("breakpoint.outcome = %v, want held_all", got)
	}

	res = runCLI(t.Context(), t, 60*time.Second, "-fake", "-fake-delay", "30ms", "-fake-jitter", "0", "-output", "json",
		"-c", writeSearchOf(t, closedPort(t), section("    p99_limit: 5ms\n"), "200ms", checkMethod))
	wantExit(t, res, 4)
	out = decodeOnly(t, res.stdout)
	wantVerdict(t, verdicts(t, out), "held_rps", "fail")
	if got := jsonAt(t, out, "breakpoint", "outcome"); got != "broke_at_first" {
		t.Errorf("breakpoint.outcome = %v, want broke_at_first", got)
	}
}

// Ground: contract — the code of a plain run is its own result first (an error,
// invalid, stopped), then the limits: a miss is 4 even beside an undecided
// one, an undecided one alone is 2, and a run that is already invalid or
// short is not made 4 by a limit.
func TestRunResult_TheLimitsDecideTheCodeOfAFinishedRun(t *testing.T) {
	res := func(v threshold.Verdict, reason threshold.Reason) threshold.Result {
		return threshold.Result{Method: checkMethod, Key: threshold.P99, Verdict: v, Reason: reason}
	}
	pass := res(threshold.VerdictPass, "")
	miss := res(threshold.VerdictFail, "")
	unknown := res(threshold.VerdictUnknown, threshold.ReasonGenerator)

	for _, tc := range []struct {
		name   string
		report engine.Report
		limits []threshold.Result
		want   error
	}{
		{"no limits", engine.Report{}, nil, nil},
		{"all pass", engine.Report{}, []threshold.Result{pass, pass}, nil},
		{"a miss", engine.Report{}, []threshold.Result{pass, miss}, ErrThresholdsFailed},
		{"a miss beside an undecided", engine.Report{}, []threshold.Result{unknown, miss}, ErrThresholdsFailed},
		{"undecided", engine.Report{}, []threshold.Result{pass, unknown}, ErrInvalidRun},
		{"undecided few observations", engine.Report{}, []threshold.Result{res(threshold.VerdictUnknown, threshold.ReasonFewObservations)}, ErrInvalidRun},
		{"stopped", engine.Report{Incomplete: true}, []threshold.Result{res(threshold.VerdictUnknown, threshold.ReasonIncomplete)}, ErrIncomplete},
		{"invalid", engine.Report{CapHit: &engine.CapHit{Unsent: 1}}, []threshold.Result{res(threshold.VerdictUnknown, threshold.ReasonInvalid)}, ErrInvalidRun},
		{"invalid outranks a miss", engine.Report{CapHit: &engine.CapHit{Unsent: 1}}, []threshold.Result{miss}, ErrInvalidRun},
	} {
		got := runResult(cli.RunReport{Report: tc.report, Thresholds: tc.limits}, nil)
		if !errors.Is(got, tc.want) || (tc.want == nil && got != nil) {
			t.Errorf("%s: err = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Ground: contract — the same for a search, with its outcome in place of the
// report's flags: findings and a run_limit that held enough are decided by the
// limit; a run_limit that held too little is undecided (2); invalid and stopped
// stay 2 and 3.
func TestSearchResult_TheLimitDecidesTheCodeOfAFinishedSearch(t *testing.T) {
	res := func(v threshold.Verdict, reason threshold.Reason) []threshold.Result {
		return []threshold.Result{{Key: threshold.HeldRPS, Verdict: v, Reason: reason}}
	}

	for _, tc := range []struct {
		name    string
		outcome breakpoint.Outcome
		limits  []threshold.Result
		want    error
	}{
		{"held all, no limits", breakpoint.HeldThroughout, nil, nil},
		{"held enough", breakpoint.BrokeBetween, res(threshold.VerdictPass, ""), nil},
		{"held too little", breakpoint.BrokeBetween, res(threshold.VerdictFail, ""), ErrThresholdsFailed},
		{"broke at the first step", breakpoint.BrokeAtFirst, res(threshold.VerdictFail, ""), ErrThresholdsFailed},
		{"run limit, held enough", breakpoint.RunLimit, res(threshold.VerdictPass, ""), nil},
		{"run limit, held too little", breakpoint.RunLimit, res(threshold.VerdictUnknown, threshold.ReasonRunLimit), ErrInvalidRun},
		{"stopped", breakpoint.Stopped, res(threshold.VerdictUnknown, threshold.ReasonIncomplete), ErrIncomplete},
		{"invalid", breakpoint.Invalid, res(threshold.VerdictUnknown, threshold.ReasonInvalid), ErrInvalidRun},
	} {
		got := searchResult(tc.outcome, tc.limits)
		if !errors.Is(got, tc.want) || (tc.want == nil && got != nil) {
			t.Errorf("%s: err = %v, want %v", tc.name, got, tc.want)
		}
	}

	// Without limits the search is what it was.
	for _, outcome := range []breakpoint.Outcome{
		breakpoint.BrokeBetween, breakpoint.BrokeAtFirst, breakpoint.HeldThroughout, breakpoint.RunLimit,
		breakpoint.Invalid, breakpoint.Stopped,
	} {
		if got, want := exitCode(searchResult(outcome, nil)), searchExitCode(outcome); got != want {
			t.Errorf("outcome %d with no limits: exit %d, want %d", outcome, got, want)
		}
	}
}

// Ground: contract — 4 is a finished run that missed a limit, and its JSON
// outcome is complete: the exit code and the outcome stay one fact.
func TestExitCode_AMissedLimitIs4(t *testing.T) {
	for _, err := range []error{ErrThresholdsFailed, fmt.Errorf("run: %w", ErrThresholdsFailed)} {
		if got := exitCode(err); got != 4 {
			t.Errorf("exitCode(%v) = %d, want 4", err, got)
		}
		if outcome, ok := outcomeOf(err); outcome != "complete" || !ok {
			t.Errorf("outcomeOf(%v) = %q (%v), want complete", err, outcome, ok)
		}
	}
	// The codes of the other outcomes stay.
	for err, want := range map[error]int{ErrInvalidRun: 2, ErrIncomplete: 3, errors.New("config"): 1} {
		if got := exitCode(err); got != want {
			t.Errorf("exitCode(%v) = %d, want %d", err, got, want)
		}
	}
}
