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

// stepYAML is searchYAML with the first step's rate, settle and hold set.
func stepYAML(from, settle, hold string) string {
	return strings.NewReplacer(
		"    from: 100\n", "    from: "+from+"\n",
		"    settle: 5s\n", "    settle: "+settle+"\n",
		"    hold: 30s\n", "    hold: "+hold+"\n",
	).Replace(searchYAML)
}

// Ground: boundary — a step measures from × (hold − settle) calls (the settle
// is out of the verdict); a p99 needs metrics.MinCount(0.99) = 100 of them.
// A search whose first step measures fewer is refused before any load: the
// baseline of every later judgement would not exist. 100 exactly is enough, 99
// is not.
func TestBreakpoint_TheFirstStepMustMeasureEnoughCallsForAP99(t *testing.T) {
	const tail = " calls, a p99 needs 100: raise from, hold or lower settle"

	for _, tc := range []struct {
		name               string
		from, settle, hold string
		// refused is the number the error names; empty means accepted.
		refused string
	}{
		{"5 rps for 8 s measured", "5", "2s", "10s", "40"},
		{"13 rps for 8 s measured", "13", "2s", "10s", ""},
		{"100 calls exactly", "10", "2s", "12s", ""},
		{"one call short of 100", "11", "1s", "10s", "99"},
		// The count is floor(from × window): 9.1 s × 11 = 100.1 holds, and
		// 9.95 s × 10 = 99.5 is 99, not a rounded 100 or a window cut to 9 s.
		{"a fractional window, 100.1 calls", "11", "900ms", "10s", ""},
		{"a fractional window, 99.5 calls", "10", "2050ms", "12s", "99"},
	} {
		_, err := config.Parse([]byte(stepYAML(tc.from, tc.settle, tc.hold)))
		if tc.refused == "" {
			if err != nil {
				t.Errorf("%s: %v, want accepted", tc.name, err)
			}

			continue
		}
		if !errors.Is(err, config.ErrShortFirstStep) || !errors.Is(err, config.ErrBreakpoint) {
			t.Errorf("%s: %v, want ErrShortFirstStep and ErrBreakpoint", tc.name, err)

			continue
		}
		if want := "the first step measures " + tc.refused + tail; !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %q, want it to contain %q", tc.name, err, want)
		}
	}
}

// Ground: boundary — a plan Rates refuses (settle at the hold) is one error, the
// settle's; there is no window to count, and no negative "N calls" is written.
func TestBreakpoint_ASettleOverTheHoldIsOnlyTheSettleError(t *testing.T) {
	_, err := config.Parse([]byte(stepYAML("5", "10s", "10s")))
	if err == nil || !strings.Contains(err.Error(), "breakpoint.settle") {
		t.Fatalf("%v, want the settle refusal", err)
	}
	if errors.Is(err, config.ErrShortFirstStep) || strings.Contains(err.Error(), "first step measures") {
		t.Errorf("%v names the first step's calls beside a window that does not exist", err)
	}
}

// Ground: contract — the refusal is joined with the section's other errors, as
// its neighbours are: a search with a warm-up and a short first step names both.
func TestBreakpoint_AShortFirstStepIsJoinedWithTheOtherBreakpointErrors(t *testing.T) {
	raw := strings.Replace(stepYAML("5", "2s", "10s"), "load:\n", "load:\n  warmup: 5s\n", 1)
	_, err := config.Parse([]byte(raw))
	if !errors.Is(err, config.ErrShortFirstStep) {
		t.Errorf("%v, want ErrShortFirstStep among the errors", err)
	}
	if err == nil || !strings.Contains(err.Error(), "warmup") {
		t.Errorf("%v, want the warm-up refusal beside it", err)
	}
}
