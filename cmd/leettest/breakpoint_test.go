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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/test/stand"
)

// quickSearch is a breakpoint section from 50 to 80 rps: steps 50, 63, 79.
const quickSearch = `  breakpoint:
    from: 50
    to: 80
    settle: 50ms
    hold: 300ms
`

// writeSearch writes a config of the calls (each a method) with the quick
// breakpoint section.
func writeSearch(t *testing.T, addr string, methods ...string) string {
	t.Helper()

	return writeSearchOf(t, addr, quickSearch, "200ms", methods...)
}

// writeSearchOf writes a config of the calls with timeout and the breakpoint
// section as written.
func writeSearchOf(t *testing.T, addr, section, timeout string, methods ...string) string {
	t.Helper()

	return writeSearchWith(t, addr, section, timeout, "", methods...)
}

// writeSearchWith is writeSearchOf with callLines, indented as fields of a
// call, added to each call.
func writeSearchWith(t *testing.T, addr, section, timeout, callLines string, methods ...string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	var calls strings.Builder
	for _, m := range methods {
		fmt.Fprintf(&calls, "    - method: %s\n      timeout: %s\n%s", m, timeout, callLines)
	}
	cfg := fmt.Sprintf(`app:
  target:
    ip: %s
    port: %s
  tls: false
load:
  calls:
%s%s`, host, port, calls.String(), section)

	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// The quickstart's tour of the search: -fake never breaks, so the search
// holds every step, says so under the method, exits 0, and tells before the
// first step how long it can take.
//
// The call has a dataset of 3 records: every step is a run of its own, and the
// count of the call goes on through all of them, never back to record 1 (the
// counter is made once for the process, in the wiring, not by a run). The text
// says the line once for the search; each run of the JSON carries the count to
// its own end.
func TestRun_ASearchOnTheFakeTarget(t *testing.T) {
	dataset := writeDataset(t, "{\"service\":\"a\"}\n{\"service\":\"b\"}\n{\"service\":\"c\"}\n")
	cfg := func() string {
		return writeSearchWith(t, closedPort(t), quickSearch, "200ms", "      dataset: '"+dataset+"'\n", checkMethod)
	}

	res := runCLI(t.Context(), t, 60*time.Second, "-fake", "-c", cfg())
	if res.err != nil {
		t.Fatalf("run: %v\n%s", res.err, res.stderr)
	}
	if want := "breaking point: " + checkMethod + "\n  held every step up to 79 rps: the limit is above it\n"; !strings.Contains(res.stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
	}
	if !strings.Contains(res.stderr, "breakpoint: up to 3 steps, at most ") {
		t.Errorf("stderr lacks the plan line:\n%s", res.stderr)
	}
	// 15 + 19 + 24 requests handed out over the steps of 50, 63 and 79 rps held
	// for 300ms (ticks i/rps below 300ms): 58, over 3 records is 20 at most.
	if want := "  held every step up to 79 rps: the limit is above it\n" +
		"  data: 3 of 3 requests from users.jsonl, each used up to 20 times\n\n"; !strings.Contains(res.stdout, want) {
		t.Errorf("stdout lacks the data line under the headline, with the count of the whole search:\n%s", res.stdout)
	}

	res = runCLI(t.Context(), t, 60*time.Second, "-fake", "-output", "json", "-c", cfg())
	if res.err != nil {
		t.Fatalf("json run: %v", res.err)
	}
	out := decodeOnly(t, res.stdout)
	bp, _ := out["breakpoint"].(map[string]any)
	if out["mode"] != "breakpoint" || bp == nil || bp["outcome"] != "held_all" || bp["held_rps"] != 79.0 {
		t.Errorf("mode %v, breakpoint %v; want breakpoint, held_all at 79", out["mode"], bp)
	}

	runs, _ := bp["runs"].([]any)
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3 steps", len(runs))
	}
	handed := 0
	for k, r := range runs {
		m := jsonAt(t, r, "report", "methods").([]any)[0].(map[string]any)
		// Warm-up is part of what the dispatcher handed out; no call is left
		// unsent on the fake target, but the count is of requests handed out.
		for _, key := range []string{"sent", "not_sent", "warmup_sent", "warmup_not_sent"} {
			handed += int(m[key].(float64))
		}

		d, ok := m["dataset"].(map[string]any)
		if !ok {
			t.Fatalf("run %d: dataset = %v, want an object", k, m["dataset"])
		}
		if d["file"] != dataset || d["records"] != 3.0 {
			t.Errorf("run %d: dataset file %v, records %v; want %q and 3", k, d["file"], d["records"], dataset)
		}
		if want := float64(min(handed, 3)); d["used"] != want {
			t.Errorf("run %d: used = %v, want %v after %d requests in all", k, d["used"], want, handed)
		}
		if want := float64((handed + 2) / 3); d["used_max"] != want {
			t.Errorf("run %d: used_max = %v, want %v after %d requests in all: each run goes on where the last ended", k, d["used_max"], want, handed)
		}
	}
}

// The CLI path against a target of known capacity, the outside source for
// "breaks at": the stand serves 270 calls a second; the steps of ×1.25 from
// 100 are 100 125 156 195 244 305, so the search holds 244 and breaks at 305
// exactly (test/measure TestBreakpoint_FindsTheStandsCapacity has the
// arithmetic).
func TestRun_ASearchNamesTheStandsCapacity(t *testing.T) {
	if testing.Short() {
		t.Skip("a search of ~30s")
	}
	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	target := stand.StartOn(lis, stand.Capacity(270, 20*time.Millisecond))
	t.Cleanup(target.Stop)

	section := `  breakpoint:
    from: 100
    to: 400
    settle: 500ms
    hold: 2500ms
`
	res := runCLI(t.Context(), t, 3*time.Minute, "-output", "json",
		"-c", writeSearchOf(t, target.Target(), section, "500ms", checkMethod))
	if res.err != nil {
		t.Fatalf("run: %v\n%s", res.err, res.stderr)
	}
	bp, _ := decodeOnly(t, res.stdout)["breakpoint"].(map[string]any)
	if bp == nil || bp["outcome"] != "broke" || bp["held_rps"] != 244.0 || bp["broke_rps"] != 305.0 {
		t.Errorf("breakpoint %v, want broke, held 244, broke 305", bp)
	}
}

// A stop during a search follows a plain run's: one Ctrl+C ends the current
// run gently, the report of what was found is printed with the stopped run
// in its table, exit 3; SIGTERM cuts the run's calls off and still prints
// the report (3), as a plain run does — 143 is only the exit without one.
func TestRun_ASearchStoppedBySignal(t *testing.T) {
	path := writeSearchOf(t, "127.0.0.1:1", `  breakpoint:
    from: 50
    to: 80
    settle: 100ms
    hold: 5s
`, "300ms", checkMethod)

	for name, signal := range map[string]func(stops, aborts chan<- struct{}){
		"ctrl+c":  func(stops, _ chan<- struct{}) { stops <- struct{}{} },
		"sigterm": func(_, aborts chan<- struct{}) { aborts <- struct{}{} },
	} {
		res := runSignalledOn(t, path, signal)
		if exitCode(res.err) != 3 {
			t.Errorf("%s: exit code %d, want 3\n%s", name, exitCode(res.err), res.stderr)
		}
		if !strings.Contains(res.stdout, "  stopped at the first step; nothing was learned about the target\n") {
			t.Errorf("%s: no stopped headline:\n%s", name, res.stdout)
		}
		if !regexp.MustCompile(`(?m)^\s+step\s+50\s.*\sstopped$`).MatchString(res.stdout) {
			t.Errorf("%s: the stopped run is not in the table:\n%s", name, res.stdout)
		}
	}
}

// With -output json stdout is the one JSON document; the progress goes to
// stderr.
func TestRun_ASearchWritesOnlyJSONToStdout(t *testing.T) {
	res := runCLI(t.Context(), t, 60*time.Second, "-fake", "-output", "json", "-c", writeSearch(t, closedPort(t), checkMethod))
	if res.err != nil {
		t.Fatalf("run: %v", res.err)
	}
	out := decodeOnly(t, res.stdout)
	if !strings.Contains(res.stderr, "breakpoint: up to 3 steps") {
		t.Errorf("stderr lacks the plan line:\n%s", res.stderr)
	}

	// How many steps the search ran is the target's and the machine's to
	// decide; the claim is that each run the report names had its line on
	// stderr.
	bp, _ := out["breakpoint"].(map[string]any)
	runs, _ := bp["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("the report lists no runs:\n%s", res.stdout)
	}
	progress := map[int]int{}
	for _, line := range strings.Split(res.stderr, "\n") {
		var rps int
		if _, err := fmt.Sscanf(line, "%d rps: sent", &rps); err == nil {
			progress[rps]++
		}
	}
	listed := map[int]int{}
	for i, r := range runs {
		run, _ := r.(map[string]any)
		planned, ok := run["planned_rps"].(float64)
		if !ok {
			t.Fatalf("run %d has no planned_rps: %v", i, r)
		}
		listed[int(planned)]++
	}
	for rps, n := range listed {
		if progress[rps] < n {
			t.Errorf("the report lists %d run(s) at %d rps, stderr has %d \"%d rps: sent\" line(s):\n%s",
				n, rps, progress[rps], rps, res.stderr)
		}
	}
}

// A search is of one method: a second call is a config error naming the key,
// exit 1 (the run did not happen, as every config error) before any load.
func TestRun_ASearchOfTwoCallsIsRefused(t *testing.T) {
	res := runCLI(t.Context(), t, 10*time.Second, "-fake",
		"-c", writeSearch(t, closedPort(t), checkMethod, "grpc.health.v1.Health/Watch"))
	if exitCode(res.err) != 1 {
		t.Fatalf("exit code %d, want 1: a config error, the run did not happen", exitCode(res.err))
	}
	if !strings.Contains(res.err.Error(), "load.breakpoint") || res.stdout != "" {
		t.Errorf("want an error naming load.breakpoint and no report:\nerr %v\nstdout %s", res.err, res.stdout)
	}
}

// The screen only in a terminal and without -output json; the stderr lines
// otherwise — as in #153.
func TestSearchScreenOnlyInATerminalWithText(t *testing.T) {
	for _, tc := range []struct {
		interactive, json, want bool
	}{{true, false, true}, {true, true, false}, {false, false, false}, {false, true, false}} {
		if got := searchScreen(tc.interactive, tc.json); got != tc.want {
			t.Errorf("terminal %v, json %v: screen %v, want %v", tc.interactive, tc.json, got, tc.want)
		}
	}
}

// The exit code of each outcome: findings are 0, invalid is 2 as an invalid
// run, a soft stop 3.
func TestSearchExitCodes(t *testing.T) {
	for outcome, want := range map[breakpoint.Outcome]int{
		breakpoint.BrokeBetween:   0,
		breakpoint.BrokeAtFirst:   0,
		breakpoint.HeldThroughout: 0,
		breakpoint.RunLimit:       0,
		breakpoint.Invalid:        2,
		breakpoint.Stopped:        3,
	} {
		if got := searchExitCode(outcome); got != want {
			t.Errorf("outcome %d: exit %d, want %d", outcome, got, want)
		}
	}
}
