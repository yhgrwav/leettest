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

// modeYAML is one call with a line for app.mode and one for load.ramp: MODE
// and RAMP are put in place of the markers, each a whole line or nothing.
const modeYAML = `
app:
  target:
    ip: localhost
    port: 50051
MODE
load:
  stop_on_errors: 5%
RAMP  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 20s
`

func modeConfig(mode, ramp string) []byte {
	text := strings.Replace(modeYAML, "MODE\n", mode, 1)

	return []byte(strings.Replace(text, "RAMP", ramp, 1))
}

// Ground: contract — app.mode is local or real, optional; left out it is
// local, so a config from before the field runs as it did.
func TestLoad_Mode(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode string
		ramp string
		want string
	}{
		{"left out", "", "", config.ModeLocal},
		{"local", "  mode: local\n", "", config.ModeLocal},
		{"local with a ramp", "  mode: local\n", "  ramp: 30s\n", config.ModeLocal},
		{"real", "  mode: real\n", "  ramp: 30s\n", config.ModeReal},
	} {
		cfg, err := config.Parse(modeConfig(tc.mode, tc.ramp))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)

			continue
		}
		if cfg.App.Mode != tc.want {
			t.Errorf("%s: Mode = %q, want %q", tc.name, cfg.App.Mode, tc.want)
		}
	}
}

// Ground: contract — the value is exact: the two names, in lowercase. Anything
// else is refused with the value as written; a value taken for local would
// run a staging config with no protection (Real, REAL) and a mistyped name
// would pass for a choice. A key written with no value is refused too:
// read as left out it would turn `mode: real` into local when the word is
// deleted.
func TestLoad_ModeOtherValuesAreRefused(t *testing.T) {
	for _, written := range []string{"Real", "REAL", "Local", "staging", "prod", "true", "1", `""`, `" real"`, `"real "`} {
		_, err := config.Parse(modeConfig("  mode: "+written+"\n", "  ramp: 30s\n"))
		if !errors.Is(err, config.ErrInvalidMode) {
			t.Errorf("mode: %s: error %v, want ErrInvalidMode", written, err)

			continue
		}
		if shown := strings.Trim(written, `"`); !strings.Contains(err.Error(), shown) {
			t.Errorf("mode: %s: error %q, want it to name the value as written", written, err)
		}
	}

	for name, line := range map[string]string{
		"nothing after the colon": "  mode:\n",
		"a tilde":                 "  mode: ~\n",
		"the word null":           "  mode: null\n",
	} {
		_, err := config.Parse(modeConfig(line, "  ramp: 30s\n"))
		if !errors.Is(err, config.ErrInvalidMode) {
			t.Errorf("%s: error %v, want ErrInvalidMode", name, err)

			continue
		}
		if errors.Is(err, config.ErrReadConfig) {
			t.Errorf("%s: error %v is a read error, the category is the mode", name, err)
		}
	}
}

// Ground: contract — real asks for a ramp and says what to write. A ramp of
// 0 is no ramp, as everywhere else. The text is the message of the brief,
// word for word: a user reads it and fixes the file. Local asks for nothing.
func TestLoad_RealNeedsARamp(t *testing.T) {
	const want = "app.mode: real needs load.ramp (a ramp from 0 to rps), e.g. load.ramp: 30s"

	for _, tc := range []struct {
		name string
		mode string
		ramp string
		fail bool
	}{
		{"real, no ramp", "  mode: real\n", "", true},
		{"real, a ramp of 0", "  mode: real\n", "  ramp: 0s\n", true},
		{"real, a ramp of a second", "  mode: real\n", "  ramp: 1s\n", false},
		{"local, no ramp", "  mode: local\n", "", false},
		{"left out, no ramp", "", "", false},
	} {
		_, err := config.Parse(modeConfig(tc.mode, tc.ramp))
		if got := errors.Is(err, config.ErrRealNeedsRamp); got != tc.fail {
			t.Errorf("%s: error %v, want ErrRealNeedsRamp: %v", tc.name, err, tc.fail)

			continue
		}
		if tc.fail && !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %q, want it to say %q", tc.name, err, want)
		}
		if !tc.fail && err != nil {
			t.Errorf("%s: %v, want none", tc.name, err)
		}
	}
}

// Ground: boundary — a negative ramp is the user writing a ramp, wrongly: the
// error is that of the ramp, and real does not add «needs a ramp» to it.
func TestLoad_RealWithANegativeRampSaysTheRampIsWrong(t *testing.T) {
	_, err := config.Parse(modeConfig("  mode: real\n", "  ramp: -1s\n"))

	if !errors.Is(err, config.ErrInvalidRamp) {
		t.Errorf("error %v, want ErrInvalidRamp", err)
	}
	if errors.Is(err, config.ErrRealNeedsRamp) {
		t.Errorf("error %v says real needs a ramp, the config has one", err)
	}
}

// Ground: contract — Parse reports all the problems of a config at once, as
// it does for the other rules: a missing ramp does not hide a rate of 0, and
// the other way round.
func TestLoad_RealWithoutARampIsReportedWithTheRestOfTheErrors(t *testing.T) {
	raw := strings.Replace(string(modeConfig("  mode: real\n", "")), "rps: 800", "rps: 0", 1)

	_, err := config.Parse([]byte(raw))

	if !errors.Is(err, config.ErrRealNeedsRamp) || !errors.Is(err, config.ErrInvalidRPS) {
		t.Errorf("error %v, want both ErrRealNeedsRamp and ErrInvalidRPS", err)
	}
}

// Ground: boundary — a mode that is not a mode is one error: whether it needs
// a ramp is not known, and a second message about the ramp would be a guess.
func TestLoad_AnInvalidModeDoesNotAlsoAskForARamp(t *testing.T) {
	_, err := config.Parse(modeConfig("  mode: staging\n", ""))

	if !errors.Is(err, config.ErrInvalidMode) {
		t.Fatalf("error %v, want ErrInvalidMode", err)
	}
	if errors.Is(err, config.ErrRealNeedsRamp) {
		t.Errorf("error %v also asks for a ramp", err)
	}
}

// Ground: contract — a search in real asks for a ramp as a run does (it hits a
// live system with breakpoint.from rps in its first second otherwise), and says
// what to write: the ramp goes from 0 to breakpoint.from. A ramp of 0 is no
// ramp. The text is the brief's, word for word; the plain run's text is
// unchanged. Local asks for nothing, with a search too. The search stays free of
// load.stop_on_errors (its errors_limit is its own limit): a search in real
// without a ramp is the ramp's error alone.
func TestLoad_RealWithASearchNeedsARamp(t *testing.T) {
	const (
		searchWant = "app.mode: real needs load.ramp (a ramp from 0 to breakpoint.from), e.g. load.ramp: 30s"
		plainWant  = "app.mode: real needs load.ramp (a ramp from 0 to rps), e.g. load.ramp: 30s"
	)

	withMode := func(mode, ramp string) []byte {
		text := strings.Replace(searchYAML, "app:\n", "app:\n"+mode, 1)

		return []byte(strings.Replace(text, "load:\n", "load:\n"+ramp, 1))
	}

	for _, ramp := range []string{"", "  ramp: 0s\n"} {
		_, err := config.Parse(withMode("  mode: real\n", ramp))
		if !errors.Is(err, config.ErrRealNeedsRamp) {
			t.Errorf("a search in real, ramp %q: error %v, want ErrRealNeedsRamp", ramp, err)

			continue
		}
		if !strings.Contains(err.Error(), searchWant) {
			t.Errorf("a search in real, ramp %q: error %q, want it to say %q", ramp, err, searchWant)
		}
		if strings.Contains(err.Error(), plainWant) {
			t.Errorf("a search in real, ramp %q: error %q has the plain run's text, which names rps", ramp, err)
		}
		if errors.Is(err, config.ErrRealNeedsStop) {
			t.Errorf("a search in real, ramp %q: error %v also asks for a stop, a search has its own limit", ramp, err)
		}
	}

	cfg, err := config.Parse(withMode("  mode: real\n", "  ramp: 30s\n"))
	if err != nil {
		t.Fatalf("a search in real with a ramp: %v, want none", err)
	}
	if cfg.App.Mode != config.ModeReal || cfg.Load.Ramp != 30*time.Second {
		t.Errorf("a search in real: Mode %q, Ramp %s; want real and 30s", cfg.App.Mode, cfg.Load.Ramp)
	}

	for _, mode := range []string{"", "  mode: local\n"} {
		if _, perr := config.Parse(withMode(mode, "")); perr != nil {
			t.Errorf("a search in local (mode %q), no ramp: %v, want none", mode, perr)
		}
	}

	_, err = config.Parse(modeConfig("  mode: real\n", ""))
	if !errors.Is(err, config.ErrRealNeedsRamp) || !strings.Contains(err.Error(), plainWant) {
		t.Errorf("a plain run in real, no ramp: error %v, want the text %q", err, plainWant)
	}
}
