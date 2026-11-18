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

package cli

import (
	"strings"
	"testing"
	"time"
)

// Ground: contract (spec §2.6) — the settings line tells app.idle_timeout when
// the config set it, after the compression; left out, the line is what it was.
func TestIdleTimeout_TheSettingsLineEchoesItOnlyWhenSet(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings RunSettings
		want     string
	}{
		{"set", RunSettings{MaxInFlight: 5000, Connections: 1, IdleTimeout: 50 * time.Second},
			"settings: warm-up 0s, max in flight 5000, connections 1, idle_timeout 50s"},
		{"after compression", RunSettings{MaxInFlight: 5000, Connections: 1, Compression: "gzip", IdleTimeout: 90 * time.Second},
			"settings: warm-up 0s, max in flight 5000, connections 1, compression gzip, idle_timeout 1m30s"},
		{"left out", RunSettings{MaxInFlight: 5000, Connections: 1},
			"settings: warm-up 0s, max in flight 5000, connections 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := echoLines(RunReport{Report: echoReport(), Name: "x", Settings: &tc.settings})

			if i := indexOfPrefix(lines, "settings:"); i < 0 || lines[i] != tc.want {
				t.Errorf("settings line:\n%s\nwant %q", strings.Join(lines, "\n"), tc.want)
			}
		})
	}
}

// Ground: contract (spec §2.6) — JSON settings.idle_timeout_us is whole
// microseconds when the config set the key and null when it did not: 0 would
// read as «a timeout of 0».
func TestIdleTimeout_JSONSettings(t *testing.T) {
	for _, tc := range []struct {
		name string
		idle time.Duration
		want any
	}{
		{"set", 50 * time.Second, 50000000.0},
		{"a day", 24 * time.Hour, 86400000000.0},
		{"left out", 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := jsonRun(echoReport())
			run.Run.Settings = &RunSettings{MaxInFlight: 5000, Connections: 1, IdleTimeout: tc.idle}

			settings, ok := writeJSON(t, run)["settings"].(map[string]any)
			if !ok {
				t.Fatal("settings is not an object")
			}

			got, present := settings["idle_timeout_us"]
			if !present || got != tc.want {
				t.Errorf("settings.idle_timeout_us = %#v (present %v), want %#v", got, present, tc.want)
			}
		})
	}
}
