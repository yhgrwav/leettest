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

// Ground: contract (spec §2.1) — app.idle_timeout is a duration written as the
// other durations of the config are, and it is kept as written.
func TestIdleTimeout_AcceptedAndRead(t *testing.T) {
	for _, tc := range []struct {
		text string
		want time.Duration
	}{
		{"1s", time.Second},
		{"50s", 50 * time.Second},
		{"1m30s", 90 * time.Second},
		{"24h", 24 * time.Hour},
	} {
		t.Run(tc.text, func(t *testing.T) {
			cfg := parseApp(t, "  idle_timeout: "+tc.text+"\n")

			if cfg.App.IdleTimeout != tc.want {
				t.Errorf("IdleTimeout = %s, want %s", cfg.App.IdleTimeout, tc.want)
			}
			if cfg.App.RawIdleTimeout == nil || *cfg.App.RawIdleTimeout != tc.want {
				t.Errorf("RawIdleTimeout = %v, want it as written, %s", cfg.App.RawIdleTimeout, tc.want)
			}
		})
	}
}

// Ground: guard (spec §2.2, green before the code) — the key left out is nil and
// 0: grpc-go's own 30 minutes, nothing printed.
func TestIdleTimeout_LeftOutIsNone(t *testing.T) {
	cfg := parseApp(t, "")

	if cfg.App.RawIdleTimeout != nil || cfg.App.IdleTimeout != 0 {
		t.Errorf("idle_timeout left out: Raw %v, IdleTimeout %s, want nil and 0", cfg.App.RawIdleTimeout, cfg.App.IdleTimeout)
	}
}

// Ground: contract (spec §2.2) — below 1s is refused, 0s and a negative one too:
// there is no 0 that means off (leave the key out). The value is printed as a
// duration is.
func TestIdleTimeout_BelowTheMinimumIsRefused(t *testing.T) {
	for _, text := range []string{"999ms", "500ms", "1ms", "0s", "-1s"} {
		t.Run(text, func(t *testing.T) {
			_, err := config.Parse(withApp("  idle_timeout: " + text + "\n"))
			if !errors.Is(err, config.ErrInvalidIdleTimeout) {
				t.Fatalf("error = %v, want ErrInvalidIdleTimeout", err)
			}

			d, perr := time.ParseDuration(text)
			if perr != nil {
				t.Fatalf("test input %q: %v", text, perr)
			}
			if want := "invalid app.idle_timeout: " + d.String() + " is below the minimum of 1s"; !strings.Contains(err.Error(), want) {
				t.Errorf("error %q, want it to say %q", err, want)
			}
		})
	}
}

// Ground: guard (spec §2.1, green before the code) — a bare number is refused
// the way load.ramp refuses it: the decoder will not read it as a duration, with
// or without quotes, and says so as a config that cannot be read.
func TestIdleTimeout_ABareNumberIsRefusedLikeRamp(t *testing.T) {
	for _, literal := range []string{"30", `"30"`, "1.5"} {
		t.Run(literal, func(t *testing.T) {
			_, errRamp := config.Parse([]byte(strings.Replace(rampYAML, "RAMP", "  ramp: "+literal+"\n", 1)))
			if !errors.Is(errRamp, config.ErrReadConfig) {
				t.Fatalf("ramp: %s gives %v, the premise of this test is gone", literal, errRamp)
			}

			_, err := config.Parse(withApp("  idle_timeout: " + literal + "\n"))
			if !errors.Is(err, config.ErrReadConfig) {
				t.Errorf("idle_timeout: %s gives %v, want the error ramp gives (ErrReadConfig)", literal, err)
			}
		})
	}
}

// Ground: contract (R3 F-A, the shape of metadata_refresh) — a key with no value
// (`idle_timeout:`, `~`, `null`) is refused with the line and the key, because the decoder would read it as left out and the
// connections would keep grpc-go's 30 minutes without a word. The first inserted
// line of withApp is line 3 of the file.
// Mutation (drop the app.idle_timeout entry of checkTree) dies on every row.
func TestIdleTimeout_NoValueIsRefusedWithTheLine(t *testing.T) {
	const want = "line 3: invalid app.idle_timeout: has no value: write a duration such as 5m, or leave the key out"

	for _, tc := range []struct{ name, line string }{
		{"empty", "  idle_timeout:\n"},
		{"tilde", "  idle_timeout: ~\n"},
		{"null", "  idle_timeout: null\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse(withApp(tc.line))
			if !errors.Is(err, config.ErrInvalidIdleTimeout) {
				t.Fatalf("error = %v, want ErrInvalidIdleTimeout", err)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q, want it to say %q", err, want)
			}
		})
	}
}

// Ground: guard (R3 F-A, green before the code) — a value is still read after
// the refusal of no value; the key left out is TestIdleTimeout_LeftOutIsNone.
func TestIdleTimeout_AValueSurvivesTheNullCheck(t *testing.T) {
	if cfg := parseApp(t, "  idle_timeout: 30s\n"); cfg.App.IdleTimeout != 30*time.Second {
		t.Errorf("idle_timeout: 30s read as %s, want 30s", cfg.App.IdleTimeout)
	}
}
