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
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
)

// burn starts n goroutines that spin until the returned stop is called, and
// waits in stop for all of them: beside a single P they take it from the
// generator. stop may be called twice.
func burn(n int) (stop func()) {
	var (
		quit atomic.Bool
		wg   sync.WaitGroup
		once sync.Once
	)
	for range n {
		wg.Go(func() {
			for !quit.Load() {
			}
		})
	}

	return func() {
		once.Do(func() {
			quit.Store(true)
			wg.Wait()
		})
	}
}

// starvedRun is a real run on the fake target at 4000 rps for 3s with one P
// shared with four spinning goroutines: the generator wakes up late, 100 ms
// and more, whatever the machine. The 1s timeout keeps the late calls from
// being cut off at the deadline, where they would read as the target's. Linux
// only: the other systems name another limit (the schedule line) or none.
func starvedRun(t *testing.T, args ...string) result {
	t.Helper()

	if runtime.GOOS != "linux" {
		t.Skip("the limit names of a starved generator are those of Linux: cgroup quota, GOMAXPROCS and the CPU readings")
	}

	path := writeConfigWith(t, closedPort(t), checkMethod, plaintext, "      timeout: 1s\n")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	longer := strings.Replace(strings.Replace(string(raw), "duration: 300ms", "duration: 3s", 1), "rps: 50", "rps: 4000", 1)
	if longer == string(raw) {
		t.Fatal("the config has no 300ms duration to stretch")
	}
	if err := os.WriteFile(path, []byte(longer), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	stop := burn(4)
	defer stop()

	res := runCLI(t.Context(), t, 60*time.Second,
		append([]string{"-fake", "-fake-delay", "1ms", "-fake-jitter", "0", "-c", path}, args...)...)
	stop()

	return res
}

// starvedLimit is the limit the generator names for GOMAXPROCS 1 with its CPU
// saturated, from the CPUs and the cgroup quota of this host: GOMAXPROCS is
// the limit while more than one P could run (under a quota of 2, or none, on a
// machine of several CPUs); a quota of one CPU is the limit when it is below
// the CPUs of the machine; on a one-CPU machine nothing is below, the CPU is.
func starvedLimit() string {
	reach := runtime.NumCPU()
	quota := cli.HostCPUQuota()
	if quota != nil {
		reach = min(reach, int(math.Ceil(*quota)))
	}

	switch {
	case reach > 1:
		return "gomaxprocs"
	case quota != nil && *quota < float64(runtime.NumCPU()):
		return "cpu_quota"
	default:
		return "cpu"
	}
}

// starvedLine is a phrase of the verdict's last line, the one that says which
// limit it is.
func starvedLine(limit string) string {
	switch limit {
	case "gomaxprocs":
		return fmt.Sprintf("The generator ran on GOMAXPROCS 1 of %d CPUs", runtime.NumCPU())
	case "cpu_quota":
		return "The generator used 90% or more of its CPU quota"
	default:
		return "The generator used 90% or more of the"
	}
}

// The whole path from the config to the exit code: a generator that cannot
// keep its schedule ends the run invalid (exit 2, generator_late) and the JSON
// names the limit it hit.
func TestRun_AStarvedGeneratorEndsInvalidAndNamesItsLimitInJSON(t *testing.T) {
	res := starvedRun(t, "-output", "json")

	if !errors.Is(res.err, ErrInvalidRun) || exitCode(res.err) != 2 {
		t.Errorf("run error %v (exit code %d), want an invalid run, exit code 2\n%s", res.err, exitCode(res.err), res.stderr)
	}

	out := decodeOnly(t, res.stdout)
	if got := out["outcome"]; got != "invalid" {
		t.Errorf("outcome = %v, want invalid", got)
	}
	if got := fmt.Sprint(out["invalid_reasons"]); got != "[generator_late]" {
		t.Errorf("invalid_reasons = %s, want [generator_late]", got)
	}
	passport, _ := out["passport"].(map[string]any)
	if got, _ := passport["gomaxprocs"].(float64); got != 1 {
		t.Errorf("passport.gomaxprocs = %v, want the 1 the test set", passport["gomaxprocs"])
	}
	if got, want := out["generator_limit"], starvedLimit(); got != want {
		t.Errorf("generator_limit = %v, want %q", got, want)
	}
}

// The same run in the text report: the verdict says the generator decided the
// tail and its last line says the limit.
func TestRun_AStarvedGeneratorNamesItsLimitInTheText(t *testing.T) {
	res := starvedRun(t)

	if !errors.Is(res.err, ErrInvalidRun) || exitCode(res.err) != 2 {
		t.Errorf("run error %v (exit code %d), want an invalid run, exit code 2\n%s", res.err, exitCode(res.err), res.stderr)
	}

	const heading = "invalid run: the generator fell behind its schedule and decided the p99 tail of"
	if !strings.Contains(res.stdout, heading) {
		t.Errorf("the report has no %q:\n%s", heading, res.stdout)
	}
	want := starvedLine(starvedLimit())
	if !strings.Contains(res.stdout, want) {
		t.Errorf("the report has no limit line starting %q:\n%s", want, res.stdout)
	}
}
