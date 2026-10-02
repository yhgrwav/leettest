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

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	var calls strings.Builder
	for _, m := range methods {
		fmt.Fprintf(&calls, "    - method: %s\n      timeout: %s\n", m, timeout)
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
func TestRun_ASearchOnTheFakeTarget(t *testing.T) {
	res := runCLI(t.Context(), t, 60*time.Second, "-fake", "-c", writeSearch(t, closedPort(t), checkMethod))
	if res.err != nil {
		t.Fatalf("run: %v\n%s", res.err, res.stderr)
	}
	if want := "breaking point: " + checkMethod + "\n  held every step up to 79 rps: the limit is above it\n"; !strings.Contains(res.stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
	}
	if !strings.Contains(res.stderr, "breakpoint: up to 3 steps, at most ") {
		t.Errorf("stderr lacks the plan line:\n%s", res.stderr)
	}

	res = runCLI(t.Context(), t, 60*time.Second, "-fake", "-output", "json", "-c", writeSearch(t, closedPort(t), checkMethod))
	if res.err != nil {
		t.Fatalf("json run: %v", res.err)
	}
	out := decodeOnly(t, res.stdout)
	bp, _ := out["breakpoint"].(map[string]any)
	if out["mode"] != "breakpoint" || bp == nil || bp["outcome"] != "held_all" || bp["held_rps"] != 79.0 {
		t.Errorf("mode %v, breakpoint %v; want breakpoint, held_all at 79", out["mode"], bp)
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

// A search is of one method: a second call is a config error naming the key,
// exit 2 before any load.
func TestRun_ASearchOfTwoCallsIsRefused(t *testing.T) {
	res := runCLI(t.Context(), t, 10*time.Second, "-fake",
		"-c", writeSearch(t, closedPort(t), checkMethod, "grpc.health.v1.Health/Watch"))
	if exitCode(res.err) != 2 {
		t.Fatalf("exit code %d, want 2", exitCode(res.err))
	}
	if !strings.Contains(res.stderr, "load.breakpoint") || strings.Contains(res.stdout, "breaking point") {
		t.Errorf("want an error naming load.breakpoint and no report:\nstdout %s\nstderr %s", res.stdout, res.stderr)
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
