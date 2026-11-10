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

package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// twoCalls is a valid config of two calls, the second carrying timeoutLine, so
// that a refusal has to name the call it is about.
func twoCalls(timeoutLine string) []byte {
	return []byte(`app:
  target:
    ip: localhost
    port: 50051
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 10
      duration: 1s
    - method: wallet.v1.WalletService/Transfer
      rps: 10
      duration: 1s
` + timeoutLine)
}

// searchWithTimeout is a breakpoint search over one call with the given timeout.
func searchWithTimeout(timeout string) []byte {
	return []byte(`app:
  target:
    ip: localhost
    port: 50051
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      timeout: ` + timeout + `
  breakpoint:
    from: 100
    to: 2000
    settle: 5s
    hold: 30s
    p99_limit: 200ms
`)
}

// Ground: boundary — the histogram keeps no latency of an hour or more, so a
// call allowed that long could finish in success and still be counted as
// censored. The config is the only gate: a timeout is refused from 1h, however
// it is written, for a plain call, for the second of two calls, and under a search.
func TestTimeoutAtOrOverAnHourIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  []byte
	}{
		{"call 1h", withTimeout("      timeout: 1h\n")},
		{"call 90m", withTimeout("      timeout: 90m\n")},
		{"call 3600s", withTimeout("      timeout: 3600s\n")},
		{"call 3600000ms", withTimeout("      timeout: 3600000ms\n")},
		{"call 2h", withTimeout("      timeout: 2h\n")},
		{"call 1h and one nanosecond", withTimeout("      timeout: 3600000000001ns\n")},
		{"second of two calls", twoCalls("      timeout: 1h\n")},
		{"search, 1h", searchWithTimeout("1h")},
		{"search, 2h", searchWithTimeout("2h")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.cfg)
			if !errors.Is(err, ErrTimeoutTooLong) {
				t.Errorf("err = %v, want ErrTimeoutTooLong", err)
			}
		})
	}
}

// Ground: boundary — the line is strictly 1h: one nanosecond under it still
// runs, and the refusal does not touch a short timeout or the default.
func TestTimeoutJustUnderAnHourIsAccepted(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  []byte
		want time.Duration
	}{
		{"call 59m59s", withTimeout("      timeout: 59m59s\n"), 59*time.Minute + 59*time.Second},
		{"call 3599999999999ns", withTimeout("      timeout: 3599999999999ns\n"), time.Hour - 1},
		{"second of two calls, 59m59s", twoCalls("      timeout: 59m59s\n"), 59*time.Minute + 59*time.Second},
		{"search, 59m59s", searchWithTimeout("59m59s"), 59*time.Minute + 59*time.Second},
		{"call 150ms", withTimeout("      timeout: 150ms\n"), 150 * time.Millisecond},
		{"no timeout", withTimeout(""), DefaultTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse(tc.cfg)
			if err != nil {
				t.Fatalf("parse: %v, want accepted", err)
			}
			if got := cfg.Load.Calls[len(cfg.Load.Calls)-1].Timeout; got != tc.want {
				t.Errorf("timeout = %v, want %v", got, tc.want)
			}
		})
	}
}

// Ground: contract — the text is what the user fixes the config by: the call
// it is about, the key, the value and the limit.
func TestTimeoutErrorNamesKeyValueAndLimit(t *testing.T) {
	_, err := Parse(twoCalls("      timeout: 90m\n"))
	if !errors.Is(err, ErrTimeoutTooLong) {
		t.Fatalf("err = %v, want ErrTimeoutTooLong", err)
	}

	for _, want := range []string{
		"call 1 (wallet.v1.WalletService/Transfer)", // the same place text as ErrInvalidTimeout
		"timeout",
		(90 * time.Minute).String(),
		"is not below 1h",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not name %q", err, want)
		}
	}

	// The place text is the one a too-short timeout already carries.
	_, short := Parse(twoCalls("      timeout: 0s\n"))
	if short == nil || !strings.Contains(short.Error(), "call 1 (wallet.v1.WalletService/Transfer)") {
		t.Errorf("premise: ErrInvalidTimeout carries the call's place, got %v", short)
	}
}
