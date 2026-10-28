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
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var inFlightInTheLine = regexp.MustCompile(`; (\d+) in flight\b`)

// stopLineOf is the line of a stop in a text: the one that starts «stopped at ».
func stopLineOf(t *testing.T, text string) string {
	t.Helper()

	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "stopped at "); i >= 0 {
			return line[i:]
		}
	}
	t.Fatalf("no stop line in:\n%s", text)

	return ""
}

// addPass appends a pass section to the (last) call of a config written by
// spikeConfig; lines are indented by the caller to sit under «pass:».
func addPass(t *testing.T, path, lines string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open config: %v", err)
	}
	defer f.Close()

	if _, err := f.WriteString("      pass:\n" + lines); err != nil {
		t.Fatalf("append to config: %v", err)
	}
}

const needsStopText = "app.mode: real needs load.stop_on_errors (the run stops when failures pass this share), e.g. load.stop_on_errors: 5%"

// setStop adds load.stop_on_errors to a config that has a «load:» line.
func setStop(t *testing.T, path, limit string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := strings.Replace(string(data), "load:\n", "load:\n  stop_on_errors: "+limit+"\n", 1)
	if got == string(data) {
		t.Fatalf("no load line to put the stop after:\n%s", data)
	}
	if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// spikeConfig is one call of the health check at 50 rps for duration, with the
// stop limit written ("" leaves it out) and the call's timeout.
func spikeConfig(t *testing.T, addr, limit, timeout, duration string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	stop := ""
	if limit != "" {
		stop = "  stop_on_errors: " + limit + "\n"
	}
	cfg := fmt.Sprintf("name: wallet\napp:\n  target:\n    ip: %s\n    port: %s\n  tls: false\nload:\n%s  calls:\n"+
		"    - method: %s\n      rps: 50\n      duration: %s\n      timeout: %s\n", host, port, stop, checkMethod, duration, timeout)

	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// Ground: signal — a target that fails every call ends the run by itself: soft
// stop, exit 3, the report written and saying why, the line on stderr and in
// the report, the JSON naming the method, the counts and the moment. The run
// is planned for 60 s; it must not get anywhere near that.
func TestRun_AnErrorSpikeStopsTheRunAndSaysWhy(t *testing.T) {
	cfg := spikeConfig(t, closedPort(t), "5%", "1s", "20s")
	path := newJSONPath(t)

	began := time.Now()
	res := runCLI(t.Context(), t, 40*time.Second, "-fake", "-fake-delay", "1ms", "-fake-fail-ratio", "1", "-json-file", path, "-c", cfg)
	took := time.Since(began)

	if !errors.Is(res.err, ErrIncomplete) || exitCode(res.err) != 3 {
		t.Fatalf("err = %v (exit %d), want ErrIncomplete, exit 3\nstderr:\n%s", res.err, exitCode(res.err), res.stderr)
	}
	if took > 12*time.Second {
		t.Errorf("the run took %v of its planned 20 s: the spike did not stop it", took)
	}
	for name, got := range map[string]string{"stderr": res.stderr, "the report": res.stdout} {
		if !strings.Contains(got, "stopped at ") || !strings.Contains(got, "over stop_on_errors 5%") {
			t.Errorf("%s has no stop line:\n%s", name, got)
		}
		// share.Format tells a share to a tenth: 100.0%, as the report tells all.
		if !strings.Contains(got, checkMethod+" failed 100.0%") {
			t.Errorf("%s does not name the method and its share:\n%s", name, got)
		}
	}

	js := decodeOnly(t, string(readFile(t, path)))
	if got := jsonAt(t, js, "stop_reason"); got != "error_spike" {
		t.Errorf("stop_reason = %v, want error_spike", got)
	}
	if got := jsonAt(t, js, "outcome"); got != "incomplete" {
		t.Errorf("outcome = %v, want incomplete", got)
	}
	// Ground: contract — stop.method is written exactly as methods[].method, so
	// a reader matches the two with no rule to learn (the brain's answer to the
	// leading slash); the text line names the method as the report's rows do.
	stopped := jsonAt(t, js, "stop", "method")
	matched := false

	methods, _ := jsonAt(t, js, "methods").([]any)
	for _, row := range methods {
		if m, ok := row.(map[string]any); ok && m["method"] == stopped {
			matched = true
		}
	}

	if !matched || !strings.HasSuffix(fmt.Sprint(stopped), checkMethod) {
		t.Errorf("stop.method = %v, want the method of a methods[] row, ending in %q: %v", stopped, checkMethod, methods)
	}
	if got := jsonAt(t, js, "stop", "limit"); got != "5%" {
		t.Errorf("stop.limit = %v, want 5%%", got)
	}
	failed, total := fieldNumber(t, js, "stop", "failed"), fieldNumber(t, js, "stop", "total")
	if failed < 2 || failed != total {
		t.Errorf("stop.failed = %v, stop.total = %v, want every call failed and at least 2", failed, total)
	}
	// The window holds whole seconds only: the first look that can see a second
	// is the one after it, and the run's own start is a second in.
	if at := fieldNumber(t, js, "stop", "at_us"); at < 1_000_000 || at > 6_000_000 {
		t.Errorf("stop.at_us = %v, want between 1 s and 6 s", at)
	}
	// What hung is told as a count, never as a send rate: the engine keeps no
	// count of calls begun. A stand that answers at once may hold none, and
	// the line says 0 then rather than leave the clause out.
	if n := fieldNumber(t, js, "stop", "in_flight"); n < 0 {
		t.Errorf("stop.in_flight = %v, want a count", n)
	}
	line := stopLineOf(t, res.stderr)
	if m := inFlightInTheLine.FindStringSubmatch(line); m == nil || !strings.HasSuffix(line, " in flight") {
		t.Errorf("the stop line does not end with «; N in flight»: %q", line)
	}
	if strings.Contains(line, "rps") {
		t.Errorf("the stop line tells a send rate: %q", line)
	}
}

// Ground: signal — a stand that hangs is the case the monitor exists for: no
// call has finished, so nothing but the timeout makes a failure. The stop comes
// no earlier than the first completed failure (the timeout) and no later than
// the timeout plus one check plus the window.
func TestRun_AHangingStandIsStoppedWithinTheTimeoutAndTheWindow(t *testing.T) {
	const timeout = time.Second

	target := startSlowTarget(t, 30*time.Second)
	cfg := spikeConfig(t, target.addr, "5%", timeout.String(), "20s")
	path := newJSONPath(t)

	began := time.Now()
	res := runCLI(t.Context(), t, 60*time.Second, "-json-file", path, "-c", cfg)
	took := time.Since(began)

	if !errors.Is(res.err, ErrIncomplete) {
		t.Fatalf("err = %v, want ErrIncomplete\nstderr:\n%s", res.err, res.stderr)
	}

	js := decodeOnly(t, string(readFile(t, path)))
	if got := jsonAt(t, js, "stop_reason"); got != "error_spike" {
		t.Fatalf("stop_reason = %v, want error_spike", got)
	}
	at := time.Duration(fieldNumber(t, js, "stop", "at_us")) * time.Microsecond
	if at < timeout {
		t.Errorf("stopped at %v, before the first call could have failed (%v)", at, timeout)
	}
	if latest := timeout + time.Second + 5*time.Second; at > latest {
		t.Errorf("stopped at %v, later than timeout + 1 s + window = %v", at, latest)
	}
	if took > 12*time.Second {
		t.Errorf("the run took %v of its planned 20 s: the hung calls were waited for or the stop never came", took)
	}

	// The calls that hung are the evidence of this case: at 50 rps with a
	// timeout of a second there are dozens in flight when the stop comes, and
	// the line and the JSON tell the same number.
	inJSON := fieldNumber(t, js, "stop", "in_flight")
	if inJSON <= 0 {
		t.Errorf("stop.in_flight = %v, want the hung calls counted", inJSON)
	}

	line := stopLineOf(t, res.stderr)
	m := inFlightInTheLine.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("the stop line has no «; N in flight»: %q", line)
	}
	if n, _ := strconv.Atoi(m[1]); n <= 0 || float64(n) != inJSON {
		t.Errorf("the line says %s in flight and the JSON %v, want the same positive number", m[1], inJSON)
	}
}

// Ground: signal — a ramp is part of the window: a run that starts failing in
// the first seconds of a 10 s ramp is stopped there, not at the hold, and the
// calls of the ramp stay in the warm-up column of the report like in any other
// stop: the measured totals are empty.
func TestRun_ARampedRunIsStoppedInsideTheRamp(t *testing.T) {
	cfg := spikeConfig(t, closedPort(t), "5%", "1s", "5s")
	setRamp(t, cfg, "10s")
	path := newJSONPath(t)

	began := time.Now()
	res := runCLI(t.Context(), t, 40*time.Second, "-fake", "-fake-delay", "1ms", "-fake-fail-ratio", "1", "-json-file", path, "-c", cfg)
	took := time.Since(began)

	wantExit(t, res, 3)
	if took > 12*time.Second {
		t.Errorf("the run took %v of its planned 15 s: the ramp was not watched", took)
	}

	js := decodeOnly(t, string(readFile(t, path)))
	if got := jsonAt(t, js, "stop_reason"); got != "error_spike" {
		t.Fatalf("stop_reason = %v, want error_spike", got)
	}
	if at := fieldNumber(t, js, "stop", "at_us"); at <= 0 || at >= 10_000_000 {
		t.Errorf("stop.at_us = %v, want inside the 10 s ramp", at)
	}
	if excluded := fieldNumber(t, js, "warmup_sent") + fieldNumber(t, js, "warmup_not_sent"); excluded <= 0 {
		t.Errorf("warmup_sent + warmup_not_sent = %v, want the ramp's calls there", excluded)
	}
	if measured := fieldNumber(t, js, "sent") + fieldNumber(t, js, "not_sent"); measured != 0 {
		t.Errorf("sent + not_sent = %v, want 0: the ramp's calls are not in the totals", measured)
	}
}

// Ground: contract — the stop is a stop: exit 3 and thresholds unknown for
// "the run was stopped", as for a Ctrl-C, whatever pass: would say. A limit of
// 1% on a target that fails everything would be exit 4 had pass: run on the
// part that was done; a stop outranks it (thresholds, «A stopped run checks
// nothing»).
func TestRun_AStopByErrorsOutranksPass(t *testing.T) {
	cfg := spikeConfig(t, closedPort(t), "5%", "1s", "20s")
	addPass(t, cfg, "        errors: 1%\n")
	path := newJSONPath(t)

	res := runCLI(t.Context(), t, 40*time.Second, "-fake", "-fake-delay", "1ms", "-fake-fail-ratio", "1", "-json-file", path, "-c", cfg)

	wantExit(t, res, 3)

	js := decodeOnly(t, string(readFile(t, path)))
	if got := jsonAt(t, js, "stop_reason"); got != "error_spike" {
		t.Fatalf("stop_reason = %v, want error_spike", got)
	}
	if got := jsonAt(t, js, "outcome"); got != "incomplete" {
		t.Errorf("outcome = %v, want incomplete", got)
	}

	rows := verdicts(t, js)
	wantVerdict(t, rows, "errors", "unknown")
	if rows["errors"]["reason"] != "incomplete" {
		t.Errorf("errors: reason = %v, want incomplete", rows["errors"]["reason"])
	}
}

// Ground: contract — 100% is off in effect and in the report: the same failing
// target runs to its planned end, nothing is stopped, the settings say so.
func TestRun_AHundredPercentNeverStops(t *testing.T) {
	cfg := spikeConfig(t, closedPort(t), "100%", "1s", "3s")
	path := newJSONPath(t)

	res := runCLI(t.Context(), t, 30*time.Second, "-fake", "-fake-delay", "1ms", "-fake-fail-ratio", "1", "-json-file", path, "-c", cfg)

	if errors.Is(res.err, ErrIncomplete) {
		t.Fatalf("err = %v: 100%% stopped the run", res.err)
	}
	if !strings.Contains(res.stdout, "\nstop_on_errors: off (100%)\n") {
		t.Errorf("the report does not say the stop is off:\n%s", res.stdout)
	}
	js := decodeOnly(t, string(readFile(t, path)))
	if got := jsonAt(t, js, "stop_reason"); got != nil {
		t.Errorf("stop_reason = %v, want null", got)
	}
}

// Ground: contract — a healthy run under a limit is not touched by it: exit 0,
// no stop line, both JSON keys null, the limit told in the settings. (Green on
// the stub for the keys; guards that a limit never ends a run that is fine.)
func TestRun_AHealthyRunUnderALimitRunsToTheEnd(t *testing.T) {
	cfg := spikeConfig(t, closedPort(t), "5%", "1s", "3s")
	path := newJSONPath(t)

	res := runCLI(t.Context(), t, 30*time.Second, "-fake", "-fake-delay", "1ms", "-json-file", path, "-c", cfg)

	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	if strings.Contains(res.stdout+res.stderr, "stopped at ") {
		t.Errorf("a fine run says it was stopped:\n%s%s", res.stdout, res.stderr)
	}
	js := decodeOnly(t, string(readFile(t, path)))
	for _, key := range []string{"stop_reason", "stop"} {
		if got, ok := js[key]; !ok || got != nil {
			t.Errorf("%s = %v (present %v), want null", key, got, ok)
		}
	}
}

// Ground: contract — the two config errors reach the user as exit 1 before
// anything is sent: a limit with a search (the search has its own, said by
// name) and real with no limit (the text says what to write).
func TestRun_StopConfigErrorsAreRefusedBeforeAnythingIsSent(t *testing.T) {
	target := startReflectingTarget(t)

	withSearch := writeSearch(t, target.addr, checkMethod)
	setStop(t, withSearch, "5%")

	realCfg := dryConfig(t, target.addr, "  mode: real\n", dryCall(unaryCallMethod, ""))
	setRamp(t, realCfg, "30s")

	// -dry-run on the real one: until the check exists the run is a dry run that
	// ends by itself, so the test fails on its assertion and not on a 30 s ramp.
	for _, tc := range []struct {
		name string
		cfg  string
		want string
		args []string
	}{
		{"a limit with a search", withSearch, "breakpoint.errors_limit", nil},
		{"real without a limit", realCfg, needsStopText, []string{"-dry-run"}},
	} {
		res := runCLI(t.Context(), t, 10*time.Second, append([]string{"-c", tc.cfg}, tc.args...)...)

		if got := exitCode(res.err); got != 1 {
			t.Errorf("%s: exit code %d, want 1\nerr %v\nstdout:\n%s", tc.name, got, res.err, res.stdout)
		}
		if all := fmt.Sprint(res.err) + res.stderr; !strings.Contains(all, tc.want) {
			t.Errorf("%s: no %q in:\n%s", tc.name, tc.want, all)
		}
		if res.stdout != "" {
			t.Errorf("%s: a refused config wrote a report:\n%s", tc.name, res.stdout)
		}
	}
	if n := len(target.recorder.allSimple()); n != 0 || target.health.calls.Load() != 0 {
		t.Errorf("the target got requests from configs that were refused")
	}
}
