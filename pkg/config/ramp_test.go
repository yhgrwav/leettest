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

package config_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
)

// rampYAML is one call with a ramp line in the load section; %s of the table
// is put in place of the marker.
const rampYAML = `
app:
  target:
    ip: localhost
    port: 50051
load:
  warmup: 2s
RAMP  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 20s
`

// Ground: contract — load.ramp is a duration, optional, never negative, and
// the search sets the rates itself so a ramp with it is refused by the key's
// name (the text follows load.warmup with load.breakpoint). The rows other than
// the negative ramp are green on the stub (the field exists so that the test
// builds); the search is the next test.
func TestLoad_Ramp(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ramp    string
		want    time.Duration
		wantErr error
		text    string
	}{
		{name: "a ramp", ramp: "  ramp: 30s\n", want: 30 * time.Second},
		{name: "no ramp", ramp: "", want: 0},
		{name: "a ramp of zero", ramp: "  ramp: 0s\n", want: 0},
		{
			name: "a negative ramp", ramp: "  ramp: -1s\n",
			wantErr: config.ErrInvalidRamp, text: "ramp must not be negative: -1s",
		},
		{
			name: "a ramp longer than the call", ramp: "  ramp: 5m\n",
			want: 5 * time.Minute,
		},
	} {
		cfg, err := config.Parse([]byte(strings.Replace(rampYAML, "RAMP", tc.ramp, 1)))
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: error %v, want %v", tc.name, err, tc.wantErr)

			continue
		}
		if tc.wantErr != nil {
			if !strings.Contains(err.Error(), tc.text) {
				t.Errorf("%s: error %q, want it to say %q", tc.name, err, tc.text)
			}

			continue
		}
		if cfg.Load.Ramp != tc.want {
			t.Errorf("%s: Ramp = %s, want %s", tc.name, cfg.Load.Ramp, tc.want)
		}
	}
}

// Ground: contract — a ramp is time before duration, not inside it: the checks
// on duration and warmup read the config as they did (warmup 2s of a 20s
// duration), whatever the ramp. A warm-up as long as the duration is refused
// with a ramp of any length, since the ramp does not stretch duration.
func TestLoad_ARampDoesNotStretchTheDuration(t *testing.T) {
	yaml := strings.Replace(rampYAML, "RAMP", "  ramp: 30s\n", 1)
	yaml = strings.Replace(yaml, "warmup: 2s", "warmup: 20s", 1)
	_, err := config.Parse([]byte(yaml))
	if !errors.Is(err, config.ErrWarmupCoversTheCall) {
		t.Errorf("a warm-up of 20s in a duration of 20s with a ramp of 30s: error %v, want %v", err, config.ErrWarmupCoversTheCall)
	}
}

// Ground: boundary — the search sets the rate of every step, and a ramp would
// move it from under the search. The refusal is ErrBreakpoint, by the key's
// name, in the words of the neighbouring load.warmup with load.breakpoint. A
// ramp of 0 is no ramp and is as acceptable as a warm-up of 0.
func TestLoad_RampWithABreakpointIsRefused(t *testing.T) {
	_, err := config.Parse([]byte(strings.Replace(searchYAML, "load:\n", "load:\n  ramp: 30s\n", 1)))
	if !errors.Is(err, config.ErrBreakpoint) {
		t.Fatalf("a ramp with a search: error %v, want ErrBreakpoint", err)
	}
	if want := "load.ramp with load.breakpoint: the search sets the rates"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q, want it to say %q", err, want)
	}

	if _, err := config.Parse([]byte(strings.Replace(searchYAML, "load:\n", "load:\n  ramp: 0s\n", 1))); err != nil {
		t.Errorf("a ramp of 0 with a search: %v, want none", err)
	}
}
