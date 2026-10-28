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

	"github.com/yhgrwav/leettest/pkg/config"
)

// stopYAML is one call with a line for app.mode and the lines of load put in
// place of LOAD: ramp, stop_on_errors, breakpoint, each whole or nothing.
const stopYAML = `
app:
  target:
    ip: localhost
    port: 50051
MODE
load:
LOAD  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 20s
`

func stopConfig(mode, load string) []byte {
	text := strings.Replace(stopYAML, "MODE\n", mode, 1)

	return []byte(strings.Replace(text, "LOAD", load, 1))
}

// Ground: contract — load.stop_on_errors is a percent by the grammar of
// breakpoint.errors_limit (up to 3 digits after the point, at most 100%),
// kept in thousandths of a percent so the comparison is integer. Left out it
// is the zero Percent: no stop. Text is what the user wrote, for the report.
func TestLoad_StopOnErrorsIsRead(t *testing.T) {
	for _, tc := range []struct {
		name        string
		line        string
		thousandths int64
		text        string
	}{
		{"left out", "", 0, ""},
		{"a whole percent", "  stop_on_errors: 5%\n", 5000, "5%"},
		{"a fraction", "  stop_on_errors: 0.5%\n", 500, "0.5%"},
		{"the finest fraction", "  stop_on_errors: 0.001%\n", 1, "0.001%"},
		{"a quoted percent", "  stop_on_errors: \"5%\"\n", 5000, "5%"},
		{"a hundred", "  stop_on_errors: 100%\n", 100000, "100%"},
	} {
		cfg, err := config.Parse(stopConfig("", tc.line))
		if err != nil {
			t.Errorf("%s: %v, want none", tc.name, err)

			continue
		}
		if got := cfg.Load.StopOnErrors.Thousandths; got != tc.thousandths {
			t.Errorf("%s: StopOnErrors = %d thousandths, want %d", tc.name, got, tc.thousandths)
		}
		if got := cfg.Load.StopOnErrors.Text; got != tc.text {
			t.Errorf("%s: StopOnErrors.Text = %q, want %q", tc.name, got, tc.text)
		}
	}
}

// Ground: boundary — a limit that is not a percent above 0 is one error that
// names the key. 0% would stop a run on the first failure of two and is
// «off» written the wrong way round (100% is off); a number without % is not
// guessed, as in errors_limit; the grammar's other refusals carry over.
func TestLoad_StopOnErrorsRefusesWhatIsNotAPercentAboveZero(t *testing.T) {
	for _, written := range []string{
		"0%", "0.0%", "0.000%", "100.001%", "101%", "5", "0.5", "five", "1.2345%", "-1%", "5 %", "[5%]", "true",
	} {
		_, err := config.Parse(stopConfig("", "  stop_on_errors: "+written+"\n"))

		if !errors.Is(err, config.ErrInvalidStopOnErrors) {
			t.Errorf("stop_on_errors: %s: error %v, want ErrInvalidStopOnErrors", written, err)

			continue
		}
		if !strings.Contains(err.Error(), "load.stop_on_errors") {
			t.Errorf("stop_on_errors: %s: error %q does not name load.stop_on_errors", written, err)
		}
		if errors.Is(err, config.ErrReadConfig) {
			t.Errorf("stop_on_errors: %s: error %v is a read error, the category is the limit", written, err)
		}
	}
}

// Ground: contract — a key written with no value is refused, not read as left
// out: a stop the user wrote and lost would let a run that was to be stopped
// go on, without a word. The same rule as errors_limit and the dataset keys.
func TestLoad_StopOnErrorsWithNoValueIsRefused(t *testing.T) {
	for _, line := range []string{
		"  stop_on_errors:\n", "  stop_on_errors: ~\n", "  stop_on_errors: null\n", "  stop_on_errors: &a ~\n",
	} {
		_, err := config.Parse(stopConfig("", line))

		if !errors.Is(err, config.ErrInvalidStopOnErrors) {
			t.Errorf("%q: error %v, want ErrInvalidStopOnErrors", line, err)

			continue
		}
		if !strings.Contains(err.Error(), "line") {
			t.Errorf("%q: error %q does not name the line", line, err)
		}
	}
}

// searchConfig is searchYAML with a line for app.mode and the lines of load
// put in front of the call.
func searchConfig(mode, load string) []byte {
	text := strings.Replace(searchYAML, "app:\n", "app:\n"+mode, 1)

	return []byte(strings.Replace(text, "load:\n", "load:\n"+load, 1))
}

// Ground: contract — the search stops by breakpoint.errors_limit and
// load.stop_on_errors beside it would be a second, silent limit: a config
// error in any mode, and the text points at the one that works. It is the
// user's mistake, so it is not a read error either.
func TestLoad_StopOnErrorsWithASearchIsAConfigError(t *testing.T) {
	const pointer = "breakpoint.errors_limit"

	for _, tc := range []struct{ name, mode, load string }{
		{"local", "", "  stop_on_errors: 5%\n"},
		{"real", "  mode: real\n", "  stop_on_errors: 5%\n"},
		{"a hundred", "", "  stop_on_errors: 100%\n"},
	} {
		_, err := config.Parse(searchConfig(tc.mode, tc.load))

		if !errors.Is(err, config.ErrStopWithSearch) {
			t.Errorf("%s: error %v, want ErrStopWithSearch", tc.name, err)

			continue
		}
		if !strings.Contains(err.Error(), pointer) {
			t.Errorf("%s: error %q does not point at %s", tc.name, err, pointer)
		}
		if errors.Is(err, config.ErrReadConfig) {
			t.Errorf("%s: error %v is a read error, the category is the pair", tc.name, err)
		}
	}
}

// Ground: contract — the two keys beside each other are one error about the
// pair, not that plus a second about the value of the one: a valid stop beside
// a search says «do not go together» only.
func TestLoad_StopOnErrorsWithASearchSaysNothingOfRealNeedingIt(t *testing.T) {
	_, err := config.Parse(searchConfig("  mode: real\n", "  stop_on_errors: 5%\n"))

	if errors.Is(err, config.ErrRealNeedsStop) {
		t.Errorf("error %v also says real needs a stop, the config has one", err)
	}
}

// Ground: contract — real asks for a stop and says what to write, in the
// words of the brief: a user reads it and fixes the file. 100% is a stop the
// user chose to be off, written down, and passes. Local asks for nothing.
func TestLoad_RealNeedsAStopOnErrors(t *testing.T) {
	const want = "app.mode: real needs load.stop_on_errors (the run stops when failures pass this share), e.g. load.stop_on_errors: 5%"

	for _, tc := range []struct {
		name string
		mode string
		load string
		fail bool
	}{
		{"real, no stop", "  mode: real\n", "  ramp: 30s\n", true},
		{"real, a stop", "  mode: real\n", "  ramp: 30s\n  stop_on_errors: 5%\n", false},
		{"real, a stop of a hundred", "  mode: real\n", "  ramp: 30s\n  stop_on_errors: 100%\n", false},
		{"local, no stop", "  mode: local\n", "  ramp: 30s\n", false},
		{"left out, no stop", "", "", false},
	} {
		_, err := config.Parse(stopConfig(tc.mode, tc.load))

		if got := errors.Is(err, config.ErrRealNeedsStop); got != tc.fail {
			t.Errorf("%s: error %v, want ErrRealNeedsStop: %v", tc.name, err, tc.fail)

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

// Ground: contract — Parse reports all the problems at once: a missing stop
// does not hide a missing ramp or a rate of 0, and the other way round.
func TestLoad_RealWithoutAStopIsReportedWithTheRestOfTheErrors(t *testing.T) {
	raw := strings.Replace(string(stopConfig("  mode: real\n", "")), "rps: 800", "rps: 0", 1)

	_, err := config.Parse([]byte(raw))

	for _, want := range []error{config.ErrRealNeedsStop, config.ErrRealNeedsRamp, config.ErrInvalidRPS} {
		if !errors.Is(err, want) {
			t.Errorf("error %v, want %v among them", err, want)
		}
	}
}

// Ground: boundary — a stop that is not a stop is the error of the stop, and
// real does not add «needs a stop» to it: the user wrote one, wrongly.
func TestLoad_RealWithAnInvalidStopSaysTheStopIsWrong(t *testing.T) {
	_, err := config.Parse(stopConfig("  mode: real\n", "  ramp: 30s\n  stop_on_errors: 0%\n"))

	if !errors.Is(err, config.ErrInvalidStopOnErrors) {
		t.Errorf("error %v, want ErrInvalidStopOnErrors", err)
	}
	if errors.Is(err, config.ErrRealNeedsStop) {
		t.Errorf("error %v says real needs a stop, the config has one", err)
	}
}

// Ground: contract — a search in real asks for no stop: its own errors_limit
// is the limit (the default 1% when left out), so a real search config
// written for unit 1 runs as before. GUARD: green before the implementation
// too; it is here so that the rule «real needs a stop» is never applied to a
// search.
func TestLoad_RealWithASearchDoesNotRequireAStopOnErrors(t *testing.T) {
	for _, load := range []string{"", "  ramp: 0s\n"} {
		cfg, err := config.Parse(searchConfig("  mode: real\n", load))
		if err != nil {
			t.Errorf("a search in real, load %q: %v, want none", load, err)

			continue
		}
		if cfg.App.Mode != config.ModeReal {
			t.Errorf("a search in real: Mode = %q, want real", cfg.App.Mode)
		}
		if cfg.Load.StopOnErrors.Thousandths != 0 {
			t.Errorf("a search in real: StopOnErrors = %d, want none", cfg.Load.StopOnErrors.Thousandths)
		}
	}
}
