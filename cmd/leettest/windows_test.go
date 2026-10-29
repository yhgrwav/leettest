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
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
)

func optionsOf(t *testing.T, path string) (engine.Options, bool) {
	t.Helper()

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	opts, _, search, err := engineOptions(cfg, engine.FakeSender{}, defaultMaxInFlight, 0)
	if err != nil {
		t.Fatalf("engineOptions: %v", err)
	}

	return opts, search
}

// Ground: wiring (A6, T9) — the switch is set where the options are made: on
// for a plain run, off for the steps of a breakpoint search.
func TestEngineOptions_WindowsAreAskedForByAPlainRunAndNotByASearch(t *testing.T) {
	plain, search := optionsOf(t, writeConfig(t, closedPort(t), checkMethod, plaintext))
	if search || !plain.Windows {
		t.Errorf("a plain run: search %v, Windows %v; want false and true", search, plain.Windows)
	}

	stepped, search := optionsOf(t, writeSearch(t, closedPort(t), checkMethod))
	if !search || stepped.Windows {
		t.Errorf("a search: search %v, Windows %v; want true and false", search, stepped.Windows)
	}
}

// Ground: end to end (A2, A5) — the windows reach the JSON of a real command
// line run, and add up to the method's sent.
func TestRun_APlainRunPutsItsWindowsInTheJSON(t *testing.T) {
	res := runCLI(t.Context(), t, 30*time.Second,
		"-fake", "-output", "json", "-c", writeConfig(t, closedPort(t), checkMethod, plaintext))
	if res.err != nil {
		t.Fatalf("run: %v", res.err)
	}

	method := jsonAt(t, decodeOnly(t, res.stdout), "methods").([]any)[0]
	windows, _ := jsonAt(t, method, "windows").([]any)
	if len(windows) == 0 {
		t.Fatal("methods[0].windows is empty: a 300 ms run has one window of at most a second")
	}

	var sent float64
	for _, w := range windows {
		sent += jsonAt(t, w, "sent").(float64)
	}
	if want := jsonAt(t, method, "sent").(float64); sent != want {
		t.Errorf("windows hold %v sent, want the method's %v", sent, want)
	}
	if start := jsonAt(t, windows[0], "start_us"); start != 0.0 {
		t.Errorf("window 0 start_us = %v, want 0: the axis starts where the warm-up ends", start)
	}
	if width := jsonAt(t, windows[0], "width_us").(float64); width <= 0 || width > 1_000_000 {
		t.Errorf("window 0 width_us = %v, want in (0, 1 s]: the 300 ms that were covered", width)
	}
}

// Ground: end to end (A6) — a step of a search is already a slice of time and
// holds no windows. Green on the stub.
func TestRun_ABreakpointSearchStepsHaveNoWindows(t *testing.T) {
	res := runCLI(t.Context(), t, 60*time.Second,
		"-fake", "-output", "json", "-c", writeSearch(t, closedPort(t), checkMethod))
	if res.err != nil {
		t.Fatalf("run: %v", res.err)
	}

	bp := jsonAt(t, decodeOnly(t, res.stdout), "breakpoint").(map[string]any)
	runs, _ := bp["runs"].([]any)
	if len(runs) == 0 {
		t.Fatal("the search made no runs")
	}
	for k, r := range runs {
		method := jsonAt(t, r, "report", "methods").([]any)[0]
		if windows, ok := jsonAt(t, method, "windows").([]any); !ok || len(windows) != 0 {
			t.Errorf("step %d: windows = %v, want []", k, windows)
		}
	}
}
