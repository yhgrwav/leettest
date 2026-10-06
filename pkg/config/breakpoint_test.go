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

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/config"
)

// searchYAML is a search over one call: no rps, no duration, no warm-up.
const searchYAML = `
app:
  target:
    ip: localhost
    port: 50051
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      timeout: 500ms
  breakpoint:
    from: 100
    to: 2000
    settle: 5s
    hold: 30s
    p99_limit: 200ms
`

// Ground: contract — the YAML keys of the spec become the search's plan, the
// call's timeout and the cap from the CLI included.
func TestBreakpoint_TheSectionIsThePlan(t *testing.T) {
	cfg, err := config.Parse([]byte(searchYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Load.Breakpoint == nil {
		t.Fatalf("no breakpoint section parsed")
	}
	got := cfg.Load.Breakpoint.Plan(cfg.Load.Calls[0].Timeout, 80)
	want := breakpoint.Plan{
		From: 100, To: 2000, Settle: 5 * time.Second, Hold: 30 * time.Second,
		Timeout: 500 * time.Millisecond, MaxInFlight: 80, P99Limit: 200 * time.Millisecond,
	}
	if got != want {
		t.Errorf("plan %+v, want %+v", got, want)
	}
}

// Ground: boundary — a search that cannot run as written is a config error
// before any load, named by its YAML key.
func TestBreakpoint_RefusesWhatCannotRun(t *testing.T) {
	for _, tc := range []struct {
		name, from, to, want string
	}{
		{"two calls", "      timeout: 500ms\n", "      timeout: 500ms\n    - method: wallet.v1.WalletService/Other\n", "one call"},
		{"rps on the call", "      timeout: 500ms\n", "      timeout: 500ms\n      rps: 100\n", "rps"},
		{"duration on the call", "      timeout: 500ms\n", "      timeout: 500ms\n      duration: 1m\n", "duration"},
		{"a warm-up", "load:\n", "load:\n  warmup: 5s\n", "warmup"},
		{"settle at half the hold", "    settle: 5s\n", "    settle: 15s\n", "breakpoint.settle"},
		{"top below the start", "    to: 2000\n", "    to: 50\n", "breakpoint.to"},
		{"factor and step", "    from: 100\n", "    from: 100\n    factor: 2\n    step: 50\n", "breakpoint.factor"},
	} {
		_, err := config.Parse([]byte(strings.Replace(searchYAML, tc.from, tc.to, 1)))
		if !errors.Is(err, config.ErrBreakpoint) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want ErrBreakpoint naming %q", tc.name, err, tc.want)
		}
	}
}
