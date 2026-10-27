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
	"strings"
	"testing"
	"time"
)

// Ground: contract — the whole path, config to both reports. A call of 50 rps
// for 2s after a ramp of 1s with a warm-up of 200ms: the excluded window is
// 1.2s, which holds the ramp's 25 calls and the hold's first 10 (35); what is
// measured is the hold's other 90, from the tick at exactly 1.2s. The text and
// the JSON say it in the same words: the plan is the user's 2s and then the
// ramp, planned_us is the sum (3s), warmup_us on top is the window (1.2s),
// settings.warmup_us the configured 200ms and settings.ramp_us the ramp.
func TestRun_ARampedRunOnTheFakeTarget(t *testing.T) {
	cfg := writeEchoConfig(t, "", "  warmup: 200ms\n  ramp: 1s\n")
	text, js := echoRun(t, 30*time.Second, "-c", cfg)

	for key, want := range map[string]float64{"ramp_us": 1_000_000, "warmup_us": 200_000} {
		if got := fieldNumber(t, js, "settings", key); got != want {
			t.Errorf("settings.%s = %v, want %v", key, got, want)
		}
	}
	for key, want := range map[string]float64{"warmup_us": 1_200_000, "planned_us": 3_000_000} {
		if got := fieldNumber(t, js, key); got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}

	excluded := fieldNumber(t, js, "warmup_sent") + fieldNumber(t, js, "warmup_not_sent")
	measured := fieldNumber(t, js, "sent") + fieldNumber(t, js, "not_sent")
	if excluded != 35 || measured != 90 {
		t.Errorf("excluded %v and measured %v calls, want 35 and 90: the ramp's 25, the hold's 100", excluded, measured)
	}

	method := js["methods"].([]any)[0]
	if got := fieldNumber(t, method, "planned_duration_us"); got != 3_000_000 {
		t.Errorf("methods[0].planned_duration_us = %v, want 3000000, the stages' sum", got)
	}
	if low, high := fieldNumber(t, method, "planned_rps_low"), fieldNumber(t, method, "planned_rps_high"); low != 50 || high != 50 {
		t.Errorf("planned rps = %v-%v, want 50-50: the measured part holds the rate", low, high)
	}

	sentNote := fmt.Sprintf("ramp and warm-up %d sent, excluded from stats", int(fieldNumber(t, js, "warmup_sent")))
	for _, want := range []string{
		"\nname: wallet\nmode: local\nsettings: warm-up 200ms, ramp 1s, max in flight 5000, connections 1\nsent ",
		"\n  plan: 50 rps for 2s, after a 1s ramp\n",
		sentNote,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the text report lacks %q:\n%s", want, text)
		}
	}
}

// Ground: contract — the brief's own run: ramp 1s, 100 rps for 1s, no warm-up.
// The ramp's 50 calls are the excluded ones (the hold's tick at exactly 1s is
// measured), the hold's 100 are measured, and the note says ramp alone.
func TestRun_ARampWithoutAWarmUpNamesTheRampAlone(t *testing.T) {
	cfg := writeEchoConfig(t, "", "  ramp: 1s\n")
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	faster := strings.Replace(string(raw), "rps: 50\n      duration: 2s", "rps: 100\n      duration: 1s", 1)
	if faster == string(raw) {
		t.Fatalf("no 50 rps for 2s to change:\n%s", raw)
	}
	if err := os.WriteFile(cfg, []byte(faster), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	text, js := echoRun(t, 30*time.Second, "-c", cfg)

	if got := fieldNumber(t, js, "warmup_sent") + fieldNumber(t, js, "warmup_not_sent"); got != 50 {
		t.Errorf("excluded calls = %v, want 50: the ramp's", got)
	}
	if got := fieldNumber(t, js, "sent") + fieldNumber(t, js, "not_sent"); got != 100 {
		t.Errorf("measured calls = %v, want 100: the hold's", got)
	}
	for _, want := range []string{
		"settings: warm-up 0s, ramp 1s, max in flight 5000, connections 1\n",
		"\nramp 50 sent, excluded from stats\n",
		"\n  plan: 100 rps for 1s, after a 1s ramp\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the text report lacks %q:\n%s", want, text)
		}
	}
}

// Ground: contract — without load.ramp the same run is the release's: the
// settings carry ramp_us 0 (always there) and the text never says ramp.
func TestRun_ARunWithoutARampSaysNone(t *testing.T) {
	text, js := echoRun(t, 20*time.Second, "-c", writeEchoConfig(t, "", "  warmup: 200ms\n"))

	if got := fieldNumber(t, js, "settings", "ramp_us"); got != 0 {
		t.Errorf("settings.ramp_us = %v, want 0", got)
	}
	if got := fieldNumber(t, js, "warmup_us"); got != 200_000 {
		t.Errorf("warmup_us = %v, want 200000: the warm-up alone", got)
	}
	if strings.Contains(text, "ramp") {
		t.Errorf("the text says ramp on a run that has none:\n%s", text)
	}
}
