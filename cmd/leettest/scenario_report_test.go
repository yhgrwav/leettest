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
	"strconv"
	"strings"
	"testing"
	"time"
)

// scenarioE2EConfig is a two-step scenario against the stub: Health/Check as
// step 1, UnaryCall as step 2, 40 iterations a second for a second.
func scenarioE2EConfig(t *testing.T, addr string) string {
	t.Helper()

	return dryConfig(t, addr, "", scenarioCalls("pay", "40", "1s", stepUnary))
}

// A scenario runs end to end: the iteration is the unit of schedule, both
// steps are sent, the text report has the block and the numbers printed in it
// are consistent. The run is clean, so the exit code is 0 (A7 i).
// Ground: contract — the whole path: config, engine, report; the counter
// invariant checked on the printed numbers (spec 8).
func TestRun_ATwoStepScenarioRunsAndIsReportedAsText(t *testing.T) {
	target := startReflectingTarget(t)

	res := runCLI(t.Context(), t, 30*time.Second, "-c", scenarioE2EConfig(t, target.addr))

	if res.err != nil {
		t.Fatalf("run failed: %v\nstdout:\n%s", res.err, res.stdout)
	}

	lines := strings.Split(res.stdout, "\n")
	start := -1
	for i, l := range lines {
		if l == "scenario pay" {
			start = i
		}
	}
	if start < 0 || start+7 > len(lines) {
		t.Fatalf("no scenario block in the report:\n%s", res.stdout)
	}

	iterations, err := strconv.Atoi(strings.Fields(lines[start+1])[0])
	if err != nil || iterations <= 0 {
		t.Fatalf("first line %q: want the iterations first, more than 0 (%v)", lines[start+1], err)
	}

	step1 := strings.Fields(lines[start+4])
	step2 := strings.Fields(lines[start+5])
	if len(step1) != 7 || len(step2) != 7 {
		t.Fatalf("step rows %q and %q, want 7 fields each", lines[start+4], lines[start+5])
	}
	if step1[1] != checkMethod || step2[1] != unaryCallMethod {
		t.Errorf("step rows name %q and %q, want %q and %q", step1[1], step2[1], checkMethod, unaryCallMethod)
	}

	num := func(row []string, col int) int {
		v, err := strconv.Atoi(row[col])
		if err != nil {
			t.Fatalf("row %v column %d: %v", row, col, err)
		}

		return v
	}
	if got := num(step1, 2); got != iterations {
		t.Errorf("step 1 reached %d, want every iteration (%d)", got, iterations)
	}
	for k, row := range [][]string{step1, step2} {
		if num(row, 2)+num(row, 6) != iterations {
			t.Errorf("step %d: reached %d + not reached %d != iterations %d", k+1, num(row, 2), num(row, 6), iterations)
		}
	}
	if want := num(step1, 2) - num(step1, 3) - num(step1, 4) - num(step1, 5); num(step2, 2) != want {
		t.Errorf("step 2 reached %d, want step 1's reached - failed - aborted - not sent = %d", num(step2, 2), want)
	}

	// The two methods are ordinary rows of the table, and the stub saw both.
	for _, method := range []string{checkMethod, unaryCallMethod} {
		if !strings.Contains(res.stdout, "\n"+method+" ") {
			t.Errorf("no method row for %s:\n%s", method, res.stdout)
		}
	}
	if got := target.health.calls.Load(); got < int64(iterations) {
		t.Errorf("Health/Check got %d requests, want at least %d", got, iterations)
	}
	if got := len(target.recorder.allSimple()); got == 0 {
		t.Error("UnaryCall got no request: step 2 was never sent")
	}
}
