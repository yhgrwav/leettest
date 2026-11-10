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

// Ground: boundary — with a valid factor set, the error names the key that is
// refused (hold, settle), never `factor`: the key follows the order in which
// breakpoint.Plan.Rates checks the plan.
func TestBreakpoint_TheKeyRefusedIsNamed(t *testing.T) {
	const withFactor = "    from: 100\n    factor: 2\n"
	for _, tc := range []struct {
		name, from, to, want string
	}{
		{"settle at half the hold", "    settle: 5s\n", "    settle: 15s\n", "breakpoint.settle"},
		{"settle over the hold", "    settle: 5s\n", "    settle: 40s\n", "breakpoint.settle"},
		{"a negative hold", "    hold: 30s\n", "    hold: -1s\n", "breakpoint.hold"},
		{"a zero hold", "    hold: 30s\n", "    hold: 0s\n", "breakpoint.hold"},
	} {
		yaml := strings.Replace(searchYAML, "    from: 100\n", withFactor, 1)
		yaml = strings.Replace(yaml, tc.from, tc.to, 1)
		_, err := config.Parse([]byte(yaml))
		if !errors.Is(err, config.ErrBreakpoint) {
			t.Errorf("%s: %v, want ErrBreakpoint", tc.name, err)

			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want it to name %q", tc.name, err, tc.want)
		}
		if strings.Contains(err.Error(), "breakpoint.factor") {
			t.Errorf("%s: %v, the factor is valid and must not be named", tc.name, err)
		}
	}
}

// Ground: boundary — the factor is still named when it is the key refused.
func TestBreakpoint_TheFactorIsNamedWhenItIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, add string
	}{
		{"a factor of 1", "    from: 100\n    factor: 1\n"},
		{"factor and step", "    from: 100\n    factor: 2\n    step: 50\n"},
	} {
		_, err := config.Parse([]byte(strings.Replace(searchYAML, "    from: 100\n", tc.add, 1)))
		if !errors.Is(err, config.ErrBreakpoint) || !strings.Contains(err.Error(), "breakpoint.factor") {
			t.Errorf("%s: %v, want ErrBreakpoint naming breakpoint.factor", tc.name, err)
		}
	}
}
