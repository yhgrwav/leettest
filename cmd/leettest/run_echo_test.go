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
)

// writeEchoConfig writes a one-call config with the settings the echo tells:
// connections and warm-up as written, a call of 2 s at 50 rps.
func writeEchoConfig(t *testing.T, connections, warmup string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(closedPort(t))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	cfg := fmt.Sprintf("name: wallet\napp:\n  target:\n    ip: %s\n    port: %s\n  tls: false\n%sload:\n%s  calls:\n"+
		"    - method: %s\n      rps: 50\n      duration: 2s\n", host, port, connections, warmup, checkMethod)

	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// setConnections adds app.connections to a config written by writeSearch.
func setConnections(t *testing.T, path string, n int) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := strings.Replace(string(data), "  tls: false\n", fmt.Sprintf("  tls: false\n  connections: %d\n", n), 1)
	if got == string(data) {
		t.Fatalf("no tls line to put connections after:\n%s", data)
	}
	if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// echoRun runs args once with the text report on stdout and the JSON report
// in a file, so both describe one run.
func echoRun(t *testing.T, limit time.Duration, args ...string) (text string, js map[string]any) {
	t.Helper()

	path := newJSONPath(t)
	res := runCLI(t.Context(), t, limit, append([]string{"-fake", "-json-file", path}, args...)...)
	if res.err != nil {
		t.Fatalf("run: %v\n%s", res.err, res.stderr)
	}

	return res.stdout, decodeOnly(t, string(readFile(t, path)))
}

// fieldNumber reads a number at path of a decoded JSON report.
func fieldNumber(t *testing.T, v any, path ...string) float64 {
	t.Helper()

	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%v: not an object at %q", path, p)
		}
		if v, ok = m[p]; !ok {
			t.Fatalf("%v: no field %q", path, p)
		}
	}
	n, ok := v.(float64)
	if !ok {
		t.Fatalf("%v = %#v, want a number", path, v)
	}

	return n
}

// Ground: contract — a report pasted into a ticket says which run and with
// what: the text and the JSON of one run carry the same name, and the
// settings are what was configured. Defaults: the cap 5000 and one
// connection, also with -fake, where the report has no connections of its own.
func TestRun_TheReportEchoesTheDefaults(t *testing.T) {
	text, js := echoRun(t, 20*time.Second, "-c", writeEchoConfig(t, "", ""))

	if got := js["name"]; got != "wallet" {
		t.Fatalf("JSON name = %v, want wallet", got)
	}
	if !strings.Contains(text, "\nname: wallet\nmode: local\nsettings: warm-up 0s, max in flight 5000, connections 1\nsent ") {
		t.Errorf("the text report lacks the name and settings lines right before the sent line:\n%s", text)
	}
	for key, want := range map[string]float64{"max_in_flight": 5000, "connections": 1, "warmup_us": 0} {
		if got := fieldNumber(t, js, "settings", key); got != want {
			t.Errorf("settings.%s = %v, want %v", key, got, want)
		}
	}
	if !strings.Contains(text, "\n  plan: 50 rps for 2s\n") {
		t.Errorf("no plan line under the method:\n%s", text)
	}
	method := js["methods"].([]any)[0].(map[string]any)
	if got := method["planned_duration_us"]; got != 2000000.0 {
		t.Errorf("methods[0].planned_duration_us = %v, want 2000000", got)
	}
}

// Ground: contract — what the flag and the config set is what is echoed: not
// the default, and a warm-up of 1.25 s as 1.25s in the text and 1250000 in
// the JSON, while warmup_us stays where it was.
func TestRun_TheReportEchoesWhatWasSet(t *testing.T) {
	cfg := writeEchoConfig(t, "  connections: 4\n", "  warmup: 1250ms\n")
	text, js := echoRun(t, 20*time.Second, "-max-in-flight", "1234", "-c", cfg)

	if !strings.Contains(text, "\nname: wallet\nmode: local\nsettings: warm-up 1.25s, max in flight 1234, connections 4\nsent ") {
		t.Errorf("the text report lacks the settings as set:\n%s", text)
	}
	for key, want := range map[string]float64{"max_in_flight": 1234, "connections": 4, "warmup_us": 1250000} {
		if got := fieldNumber(t, js, "settings", key); got != want {
			t.Errorf("settings.%s = %v, want %v", key, got, want)
		}
	}
	if got := fieldNumber(t, js, "warmup_us"); got != 1250000 {
		t.Errorf("warmup_us = %v, want 1250000: it stays at the top as well", got)
	}
}

// Ground: contract — a search echoes its own settings: the cap is null (and
// "per step" in the text) without the flag, each run's report has the cap of
// that run, not the plain run's default; the settle is the warm-up.
func TestRun_ASearchEchoesItsOwnSettings(t *testing.T) {
	cfg := writeSearch(t, closedPort(t), checkMethod)
	withName(t, cfg, "name: wallet\n")
	setConnections(t, cfg, 3)

	text, js := echoRun(t, 60*time.Second, "-c", cfg)

	if !strings.Contains(text, "\nname: wallet\nmode: local\nsettings: settle 50ms, hold 300ms, max in flight per step, connections 3\n\n  kind") {
		t.Errorf("the search's text lacks the name and settings above the runs:\n%s", text)
	}
	settings, _ := js["settings"].(map[string]any)
	if v, ok := settings["max_in_flight"]; !ok || v != nil {
		t.Errorf("settings.max_in_flight = %v (present %v), want null without the flag", v, ok)
	}
	for key, want := range map[string]float64{"connections": 3, "warmup_us": 50000} {
		if got := fieldNumber(t, js, "settings", key); got != want {
			t.Errorf("settings.%s = %v, want %v", key, got, want)
		}
	}

	runs := js["breakpoint"].(map[string]any)["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("the search reports no runs")
	}
	for i, r := range runs {
		got := fieldNumber(t, r, "report", "settings", "max_in_flight")
		if got <= 0 || got == defaultMaxInFlight {
			t.Errorf("runs[%d].report.settings.max_in_flight = %v, want that run's own cap: positive, not the default %d", i, got, defaultMaxInFlight)
		}
		if w := fieldNumber(t, r, "report", "settings", "warmup_us"); w != 50000 {
			t.Errorf("runs[%d].report.settings.warmup_us = %v, want 50000: the settle", i, w)
		}
		if c := fieldNumber(t, r, "report", "settings", "connections"); c != 3 {
			t.Errorf("runs[%d].report.settings.connections = %v, want 3: app.connections", i, c)
		}
		method := r.(map[string]any)["report"].(map[string]any)["methods"].([]any)[0]
		if d := fieldNumber(t, method, "planned_duration_us"); d != 300000 {
			t.Errorf("runs[%d].report.methods[0].planned_duration_us = %v, want 300000: the step's hold", i, d)
		}
	}
}

// Ground: contract — a step the cap was too low for never ran, and still
// says what it was tried with: the flag's cap, the configured connections
// and the settle, not zeros.
func TestRun_ARefusedSearchStepEchoesWhatItWasTriedWith(t *testing.T) {
	cfg := writeSearch(t, closedPort(t), checkMethod)
	setConnections(t, cfg, 2)

	_, js := echoRun(t, 60*time.Second, "-max-in-flight", "1", "-c", cfg)

	runs := js["breakpoint"].(map[string]any)["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("the search reports no runs")
	}
	for key, want := range map[string]float64{"max_in_flight": 1, "connections": 2, "warmup_us": 50000} {
		if got := fieldNumber(t, runs[0], "report", "settings", key); got != want {
			t.Errorf("runs[0].report.settings.%s = %v, want %v for the step that never ran", key, got, want)
		}
	}
	if why := runs[0].(map[string]any)["why"]; why != "in_flight_cap" {
		t.Fatalf("runs[0].why = %v, want in_flight_cap: the step did not run", why)
	}
}

// Ground: contract — without name in the config the text and the JSON still
// agree: both are ServiceLabel, here the one service.
func TestRun_TheNameWithoutAConfigNameIsTheSameInTextAndJSON(t *testing.T) {
	cfg := writeNamedConfig(t, "leettest.yaml", "", checkMethod)
	text, js := echoRun(t, 20*time.Second, "-c", cfg)

	if js["name"] != "Health" {
		t.Fatalf("JSON name = %v, want Health", js["name"])
	}
	if !strings.Contains(text, "\nname: Health\n") {
		t.Errorf("the text lacks the same name:\n%s", text)
	}
}

// Ground: contract — with the flag set the search echoes it, on top and in
// each run.
func TestRun_ASearchEchoesTheFlag(t *testing.T) {
	cfg := writeSearch(t, closedPort(t), checkMethod)

	text, js := echoRun(t, 60*time.Second, "-max-in-flight", "777", "-c", cfg)

	if !strings.Contains(text, "settings: settle 50ms, hold 300ms, max in flight 777, connections 1\n") {
		t.Errorf("the search's text lacks the flag's cap:\n%s", text)
	}
	if got := fieldNumber(t, js, "settings", "max_in_flight"); got != 777 {
		t.Errorf("settings.max_in_flight = %v, want 777", got)
	}
	runs := js["breakpoint"].(map[string]any)["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("the search reports no runs")
	}
	for i, r := range runs {
		if got := fieldNumber(t, r, "report", "settings", "max_in_flight"); got != 777 {
			t.Errorf("runs[%d].report.settings.max_in_flight = %v, want 777: the flag holds for every run", i, got)
		}
	}
}
