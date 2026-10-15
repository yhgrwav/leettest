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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
)

// Ground: contract — app.compression reaches the sender as it was written.
func TestCompression_TheConfigReachesTheSender(t *testing.T) {
	opts, err := senderOptions(&config.App{Compression: "gzip"})
	if err != nil {
		t.Fatalf("senderOptions: %v", err)
	}
	if opts.Compression != "gzip" {
		t.Errorf("Options.Compression = %q, want gzip", opts.Compression)
	}
}

// Ground: contract — a run's text and JSON echo app.compression.
func TestCompression_ARunEchoesIt(t *testing.T) {
	text, js := echoRun(t, 20*time.Second, "-c", writeEchoConfig(t, "  compression: gzip\n", ""))

	if !strings.Contains(text, "\nsettings: warm-up 0s, max in flight 5000, connections 1, compression gzip\n") {
		t.Errorf("the text report lacks compression in the settings line:\n%s", text)
	}
	if got := js["settings"].(map[string]any)["compression"]; got != "gzip" {
		t.Errorf("settings.compression = %v, want gzip", got)
	}
}

// Ground: contract — the search echoes it in its text and JSON too, and the
// settings of each of its runs.
func TestCompression_ASearchEchoesIt(t *testing.T) {
	cfg := writeSearch(t, closedPort(t), checkMethod)
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := strings.Replace(string(data), "  tls: false\n", "  tls: false\n  compression: gzip\n", 1)
	if got == string(data) {
		t.Fatalf("no tls line to put compression after:\n%s", data)
	}
	if err := os.WriteFile(cfg, []byte(got), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	text, js := echoRun(t, 60*time.Second, "-c", cfg)

	if !strings.Contains(text, ", compression gzip\n") {
		t.Errorf("the search's settings line lacks compression:\n%s", text)
	}
	if v := js["settings"].(map[string]any)["compression"]; v != "gzip" {
		t.Errorf("settings.compression = %v, want gzip", v)
	}
	runs := js["breakpoint"].(map[string]any)["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("the search reports no runs")
	}
	for i, r := range runs {
		settings := r.(map[string]any)["report"].(map[string]any)["settings"].(map[string]any)
		if settings["compression"] != "gzip" {
			t.Errorf("runs[%d].report.settings.compression = %v, want gzip", i, settings["compression"])
		}
	}
}
