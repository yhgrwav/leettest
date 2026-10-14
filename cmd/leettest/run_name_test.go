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
	"testing"
	"time"
)

// writeNamedConfig writes a one-call-per-method config called file, with
// nameLine (a whole top-level line, or empty) first.
func writeNamedConfig(t *testing.T, file, nameLine string, methods ...string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(closedPort(t))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	cfg := fmt.Sprintf("%sapp:\n  target:\n    ip: %s\n    port: %s\n  tls: false\nload:\n  calls:\n", nameLine, host, port)
	for _, m := range methods {
		cfg += fmt.Sprintf("    - method: %s\n      rps: 50\n      duration: 300ms\n", m)
	}

	path := filepath.Join(t.TempDir(), file)
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// withName puts nameLine in front of the config at path.
func withName(t *testing.T, path, nameLine string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if err := os.WriteFile(path, append([]byte(nameLine), data...), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// The JSON report says which run it is, with the label the live screen shows
// (ServiceLabel: the config's name, else the one service, else the config
// file's name), so a pile of reports from CI does not get mixed up. One
// -fake run per case checks the wiring from the config to the JSON.
func TestRun_JSONNamesTheRun(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		nameLine string
		methods  []string
		want     string
	}{
		{"explicit name", "leettest.yaml", "name: wallet\n", []string{checkMethod}, "wallet"},
		{"explicit name wins over two services", "checkout.yaml", "name: wallet\n", []string{checkMethod, "other.v1.Svc/Do"}, "wallet"},
		{"one service gives its short name", "leettest.yaml", "", []string{checkMethod}, "Health"},
		{"two services give the config file name", "checkout.yaml", "", []string{checkMethod, "other.v1.Svc/Do"}, "checkout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeNamedConfig(t, tt.file, tt.nameLine, tt.methods...)
			res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-output", "json", "-c", path)
			if res.err != nil {
				t.Fatalf("run: %v\n%s", res.err, res.stderr)
			}

			out := decodeOnly(t, res.stdout)
			if got, ok := out["name"].(string); !ok || got != tt.want {
				t.Errorf("name = %#v, want %q", out["name"], tt.want)
			}
		})
	}
}

// A search names itself at the top and in each run's own report, where the
// plain run's object sits whole.
func TestRun_ASearchJSONNamesTheRun(t *testing.T) {
	tests := []struct {
		name     string
		nameLine string
		want     string
	}{
		{"explicit name", "name: wallet\n", "wallet"},
		{"the service", "", "Health"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeSearch(t, closedPort(t), checkMethod)
			withName(t, path, tt.nameLine)
			res := runCLI(t.Context(), t, 60*time.Second, "-fake", "-output", "json", "-c", path)
			if res.err != nil {
				t.Fatalf("run: %v\n%s", res.err, res.stderr)
			}

			out := decodeOnly(t, res.stdout)
			if got, ok := out["name"].(string); !ok || got != tt.want {
				t.Errorf("name = %#v, want %q", out["name"], tt.want)
			}
			bp, _ := out["breakpoint"].(map[string]any)
			runs, _ := bp["runs"].([]any)
			if len(runs) == 0 {
				t.Fatalf("the report lists no runs:\n%s", res.stdout)
			}
			for i, r := range runs {
				run, _ := r.(map[string]any)
				report, _ := run["report"].(map[string]any)
				if got, ok := report["name"].(string); !ok || got != tt.want {
					t.Errorf("runs[%d].report.name = %#v, want %q", i, report["name"], tt.want)
				}
			}
		})
	}
}
