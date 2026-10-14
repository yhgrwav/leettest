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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// lines is n records of the form {"responseSize":i}, i from 1.
func lines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "{\"responseSize\":%d}\n", i)
	}

	return b.String()
}

// onceCalls is the call of 10 requests (10 rps for a second) on the file, none
// handed out twice.
func onceCalls(dataset, extra string) string {
	return "    - method: " + unaryCallMethod + "\n      rps: 10\n      duration: 1s\n      dataset: '" + dataset +
		"'\n      dataset_reuse: false\n" + extra
}

// A plain run is checked before anything is sent: the file is shorter than the
// 10 requests the plan hands out, the error names the call, the file and both
// numbers, exit 1, and the target never sees a half run.
// Ground: contract — config errors before the run; "numbers don't lie".
func TestRun_OnceWithAFileShorterThanThePlanIsAConfigError(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dataConfig(t, target.addr, onceCalls(writeDataset(t, lines(5)), ""))

	res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)

	if exitCode(res.err) != 1 {
		t.Fatalf("exit code %d, want 1: a config error\nerr %v\nstderr %s", exitCode(res.err), res.err, res.stderr)
	}
	all := res.err.Error() + res.stderr
	for _, want := range []string{unaryCallMethod, "users.jsonl", "has 5 records", "hands out 10", "dataset_reuse: false"} {
		if !strings.Contains(all, want) {
			t.Errorf("the error lacks %q:\n%s", want, all)
		}
	}
	if got := len(target.recorder.allSimple()); got != 0 {
		t.Errorf("the target saw %d requests: the run must not start", got)
	}
}

// The boundary: a file of exactly the plan's size is enough. Every record
// reaches the target once, in the file's order, and the report says so.
// Ground: contract — "equal is OK"; the end to end order on the wire.
func TestRun_OnceWithAFileOfExactlyThePlanSendsEachRecordOnce(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dataConfig(t, target.addr, onceCalls(writeDataset(t, lines(10)), ""))

	res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	var got []int32
	for _, req := range target.recorder.allSimple() {
		got = append(got, req.GetResponseSize())
	}
	if want := []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}; !slices.Equal(got, want) {
		t.Errorf("the target saw response_size %v, want %v", got, want)
	}
	if want := "  data: 10 of 10 requests from users.jsonl, each used up to 1 time, each at most once\n"; !strings.Contains(res.stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
	}
}

// Warm-up uses records: the plan hands out 10 requests in all, so a file of 8
// is short whatever the warm-up leaves out of the statistics.
// Ground: contract — "no record twice in the process, warm-up included".
func TestRun_OnceCountsTheWarmupAmongTheRecordsTheRunNeeds(t *testing.T) {
	dataset := writeDataset(t, lines(8))
	cfg := filepath.Join(t.TempDir(), "leettest.yaml")
	body := "app:\n  target:\n    ip: localhost\n    port: 1\n  tls: false\nload:\n  warmup: 300ms\n  calls:\n" + onceCalls(dataset, "")
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-c", cfg)

	if exitCode(res.err) != 1 {
		t.Fatalf("exit code %d, want 1\nerr %v", exitCode(res.err), res.err)
	}
	if all := res.err.Error() + res.stderr; !strings.Contains(all, "has 8 records") || !strings.Contains(all, "hands out 10") {
		t.Errorf("the error does not give 8 and 10 (warm-up included):\n%s", all)
	}
}

// The reuse that is explicit and true is the old wrap: more requests than records, no error.
// Ground: contract — the default is unchanged; the inverse of the tests above.
func TestRun_ReuseTrueWrapsThePlanLongerThanTheFile(t *testing.T) {
	cfg := dataConfig(t, closedPort(t), onceCalls(writeDataset(t, lines(5)), ""))
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if err := os.WriteFile(cfg, []byte(strings.Replace(string(data), "dataset_reuse: false", "dataset_reuse: true", 1)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	if want := "  data: 5 of 5 requests from users.jsonl, each used up to 2 times\n"; !strings.Contains(res.stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
	}
	if strings.Contains(res.stdout, "at most once") {
		t.Errorf("a wrapping call says it sends each at most once:\n%s", res.stdout)
	}
}

// onceSearch is a search of steps 50, 63, 79 rps, each held 300ms, on a file
// of n records none handed out twice.
func onceSearch(t *testing.T, n int) string {
	t.Helper()

	return writeSearchWith(t, closedPort(t), quickSearch, "200ms",
		"      dataset: '"+writeDataset(t, lines(n))+"'\n      dataset_reuse: false\n", unaryCallMethod)
}

// A search is checked against its first run: 50 rps for 300ms hands out 15
// requests, and a file of 14 cannot give even that. Nothing is run.
// Ground: contract — the first run of a search is known before it starts
// (the warm-up is inside the hold).
func TestRun_OnceSearchWithAFileShorterThanItsFirstRunIsAConfigError(t *testing.T) {
	res := runCLI(t.Context(), t, 20*time.Second, "-fake", "-c", onceSearch(t, 14))

	if exitCode(res.err) != 1 {
		t.Fatalf("exit code %d, want 1\nerr %v\nstderr %s", exitCode(res.err), res.err, res.stderr)
	}
	if all := res.err.Error() + res.stderr; !strings.Contains(all, "has 14 records") || !strings.Contains(all, "hands out 15") {
		t.Errorf("the error does not give 14 and 15:\n%s", all)
	}
	if strings.Contains(res.stderr, "rps: sent") {
		t.Errorf("a run was made before the error:\n%s", res.stderr)
	}
}

// A file of exactly the first run: that run is whole and judged, the next one
// finds no record at its first tick, ends there unjudged, and the search is
// stopped (exit 3) with the reason in the text and in the JSON. The first run
// is not lost to "stopped at the first step".
// Ground: contract — exhaustion is lazy: a record is missing when a tick needs it.
func TestRun_OnceSearchStopsWhenTheFileRunsOut(t *testing.T) {
	res := runCLI(t.Context(), t, 30*time.Second, "-fake", "-c", onceSearch(t, 15))

	if exitCode(res.err) != 3 {
		t.Fatalf("exit code %d, want 3: a soft stop\nerr %v\nstderr %s", exitCode(res.err), res.err, res.stderr)
	}
	for _, want := range []string{
		"stopped at 63 rps", "held 50 rps so far",
		"dataset users.jsonl (call " + unaryCallMethod + ") ran out at 63 rps",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
		}
	}
	if strings.Contains(res.stdout, "nothing was learned") {
		t.Errorf("the first run, whole and judged, is reported as nothing learned:\n%s", res.stdout)
	}

	res = runCLI(t.Context(), t, 30*time.Second, "-fake", "-output", "json", "-c", onceSearch(t, 15))
	if exitCode(res.err) != 3 {
		t.Fatalf("json: exit code %d, want 3\nerr %v", exitCode(res.err), res.err)
	}
	bp := jsonAt(t, decodeOnly(t, res.stdout), "breakpoint").(map[string]any)
	if bp["outcome"] != "stopped" || bp["stop_reason"] != "dataset_exhausted" || bp["held_rps"] != 50.0 {
		t.Errorf("breakpoint = outcome %v stop_reason %v held_rps %v, want stopped, dataset_exhausted, 50", bp["outcome"], bp["stop_reason"], bp["held_rps"])
	}
	if runs, _ := bp["runs"].([]any); len(runs) != 2 {
		t.Errorf("%d runs listed, want 2: the whole one and the one that found no record", len(runs))
	}
}

// The inverse: a file that covers every step, or one that wraps, ends the
// search itself and stop_reason is null, the key present.
// Ground: contract — null when the search was not stopped.
func TestRun_SearchThatEndsItselfHasNoStopReason(t *testing.T) {
	// 15 + 19 + 24 requests over the three steps.
	for name, cfg := range map[string]string{
		"a file that covers it": onceSearch(t, 58),
		"a file that wraps": writeSearchWith(t, closedPort(t), quickSearch, "200ms",
			"      dataset: '"+writeDataset(t, lines(3))+"'\n      dataset_reuse: true\n", unaryCallMethod),
		"no dataset": writeSearch(t, closedPort(t), checkMethod),
	} {
		t.Run(name, func(t *testing.T) {
			res := runCLI(t.Context(), t, 60*time.Second, "-fake", "-output", "json", "-c", cfg)
			if res.err != nil {
				t.Fatalf("run: %v\n%s", res.err, res.stderr)
			}

			bp := jsonAt(t, decodeOnly(t, res.stdout), "breakpoint").(map[string]any)
			if v, ok := bp["stop_reason"]; !ok || v != nil {
				t.Errorf("stop_reason = %v (present %v), want null with the key present", v, ok)
			}
		})
	}
}

// Ctrl+C is "requested", not an exhaustion and not a signal: in a terminal it
// is a key.
// Ground: contract — the closed list of stop_reason.
func TestRun_ASearchStoppedByCtrlCSaysRequested(t *testing.T) {
	path := writeSearchOf(t, "127.0.0.1:1", `  breakpoint:
    from: 50
    to: 80
    settle: 100ms
    hold: 5s
`, "300ms", checkMethod)

	res := runSignalledOn(t, path, func(stops, _ chan<- struct{}) { stops <- struct{}{} }, "-output", "json")

	if exitCode(res.err) != 3 {
		t.Fatalf("exit code %d, want 3\n%s", exitCode(res.err), res.stderr)
	}
	bp := jsonAt(t, decodeOnly(t, res.stdout), "breakpoint").(map[string]any)
	if bp["outcome"] != "stopped" || bp["stop_reason"] != "requested" {
		t.Errorf("breakpoint = outcome %v stop_reason %v, want stopped, requested", bp["outcome"], bp["stop_reason"])
	}
}
