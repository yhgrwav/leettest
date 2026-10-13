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
	"bytes"
	"math"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// threeSecondConfig is writeConfig's run stretched from 300ms at 50 rps to 3s
// at 20000: long enough for the readings to tick, and with work to measure.
func threeSecondConfig(t *testing.T) string {
	t.Helper()

	// The timeout keeps rps x timeout under the in-flight cap.
	path := writeConfigWith(t, closedPort(t), checkMethod, plaintext, "      timeout: 100ms\n")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	longer := strings.Replace(strings.Replace(string(raw), "duration: 300ms", "duration: 3s", 1), "rps: 50", "rps: 20000", 1)
	if longer == string(raw) {
		t.Fatal("the config has no 300ms duration to stretch")
	}
	if err := os.WriteFile(path, []byte(longer), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// The whole path: the readings are taken beside the run, laid on the grid
// the run's seconds are on, and reach the JSON.
func TestRun_JSONCarriesTheGeneratorCPUOnTheGridOfTheRun(t *testing.T) {
	// The share is of all the cores the process may use: with 2 of them the
	// run's few percent of a machine is the same on a laptop and a 64-thread
	// runner, and the bounds below are in the units of those 2.
	const cores = 2
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(cores))

	res := runCLI(t.Context(), t, 30*time.Second, "-fake", "-output", "json", "-c", threeSecondConfig(t))
	if res.err != nil {
		t.Fatalf("run: %v\n%s", res.err, res.stderr)
	}
	out := decodeOnly(t, res.stdout)

	cpu, ok := out["generator_cpu"].(map[string]any)
	if !ok {
		t.Fatalf("generator_cpu = %v, want an object: the process's CPU time can be read here", out["generator_cpu"])
	}

	seconds, _ := cpu["seconds"].([]any)
	methods, _ := out["methods"].([]any)
	method, _ := methods[0].(map[string]any)
	grid, _ := method["seconds"].([]any)
	if len(seconds) == 0 || len(seconds) != len(grid) {
		t.Errorf("generator_cpu.seconds has %d entries, methods[0].seconds %d: one grid", len(seconds), len(grid))
	}
	if len(seconds) > 0 && seconds[0] == nil {
		t.Error("generator_cpu.seconds[0] is null: the readings start before the run")
	}

	// 20000 rps of the fake target takes at least 0.02 of a core, 0.01 of the 2;
	// a share can pass 1 a little (time in system calls outside the Ps), not
	// by half.
	share, _ := cpu["share"].(float64)
	if math.IsNaN(share) || share < 0.01 || share > 1.5 {
		t.Errorf("share = %v, want a CPU share from 0.01 to 1.5", cpu["share"])
	}
	if got, _ := cpu["available_cores"].(float64); got <= 0 || got > cores {
		t.Errorf("available_cores = %v, want above 0 and at most GOMAXPROCS %d", cpu["available_cores"], cores)
	}
}

// Every step of a search is a run of its own with its own readings, though
// each is under a second long: a share, and no busy second to name.
func TestRun_EveryStepOfASearchHasItsGeneratorCPU(t *testing.T) {
	res := runCLI(t.Context(), t, 60*time.Second, "-fake", "-output", "json", "-c", writeSearch(t, closedPort(t), checkMethod))
	if res.err != nil {
		t.Fatalf("run: %v\n%s", res.err, res.stderr)
	}
	out := decodeOnly(t, res.stdout)

	bp, _ := out["breakpoint"].(map[string]any)
	runs, _ := bp["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("the search lists no runs:\n%s", res.stdout)
	}
	for i, r := range runs {
		run, _ := r.(map[string]any)
		report, _ := run["report"].(map[string]any)
		cpu, ok := report["generator_cpu"].(map[string]any)
		if !ok {
			t.Errorf("runs[%d].report.generator_cpu = %v, want an object", i, report["generator_cpu"])

			continue
		}
		if share, _ := cpu["share"].(float64); math.IsNaN(share) || share < 0 {
			t.Errorf("runs[%d]: share = %v, want a number from 0", i, cpu["share"])
		}
		if cpu["busy_seconds"] != 0.0 || cpu["first_busy_s"] != nil {
			t.Errorf("runs[%d]: busy_seconds %v, first_busy_s %v; a step under a second has no busy second",
				i, cpu["busy_seconds"], cpu["first_busy_s"])
		}
		if seconds, _ := cpu["seconds"].([]any); len(seconds) == 0 || seconds[0] == nil {
			t.Errorf("runs[%d]: seconds = %v, want the first second measured", i, cpu["seconds"])
		}
	}
	if strings.Contains(res.stdout, "generator CPU:") || strings.Contains(res.stderr, "generator CPU:") {
		t.Error("a search that held every step printed the generator CPU line")
	}
}

// The line states a fact: the exit code is what it was without it.
func TestRunResult_TheGeneratorCPUChangesNoExitCode(t *testing.T) {
	first := 2
	busy := &cli.GeneratorCPU{AvailableCores: 1, Share: 0.97, BusySeconds: 18, FirstBusy: &first, Counted: 20}

	// The object is not ignored: the report with it says the line.
	var printed bytes.Buffer
	cli.PrintReport(&printed, "t:1", cli.RunReport{GeneratorCPU: busy})
	if !strings.Contains(printed.String(), "generator CPU:") {
		t.Fatalf("the report of a run with 18 busy seconds has no generator CPU line:\n%s", printed.String())
	}

	for name, report := range map[string]engine.Report{
		"complete":   {},
		"incomplete": {Incomplete: true},
		"cap hit":    {CapHit: &engine.CapHit{}},
		"rejected":   {RequestRejected: true},
	} {
		plain := runResult(cli.RunReport{Report: report}, nil)
		withCPU := runResult(cli.RunReport{Report: report, GeneratorCPU: busy}, nil)

		if exitCode(withCPU) != exitCode(plain) {
			t.Errorf("%s: exit code %d with the CPU object, %d without", name, exitCode(withCPU), exitCode(plain))
		}
	}
}
