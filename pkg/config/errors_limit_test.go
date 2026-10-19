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
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/share"
)

// Ground: contract — breakpoint.errors_limit is read by the grammar of
// pass.errors and becomes the plan's limit as written; left out, the plan
// carries none (the search then holds the default), and 0% is a limit.
func TestBreakpoint_ErrorsLimitBecomesThePlansLimit(t *testing.T) {
	for _, tc := range []struct {
		line string
		want share.Percent
	}{
		{"", share.Percent{}},
		{"    errors_limit: 1%\n", share.Percent{Thousandths: 1000, Text: "1%"}},
		{"    errors_limit: 0.125%\n", share.Percent{Thousandths: 125, Text: "0.125%"}},
		{"    errors_limit: 100%\n", share.Percent{Thousandths: 100000, Text: "100%"}},
		{"    errors_limit: 0%\n", share.Percent{Thousandths: 0, Text: "0%"}},
		{"    errors_limit: \"5%\"\n", share.Percent{Thousandths: 5000, Text: "5%"}},
	} {
		cfg, err := config.Parse([]byte(searchYAML + tc.line))
		if err != nil {
			t.Fatalf("%q: parse: %v", tc.line, err)
		}
		b := cfg.Load.Breakpoint
		if b.ErrorsLimit != tc.want {
			t.Errorf("%q: ErrorsLimit %+v, want %+v", tc.line, b.ErrorsLimit, tc.want)
		}
		if got := b.Plan(500*time.Millisecond, 0).ErrorsLimit; got != tc.want {
			t.Errorf("%q: plan's ErrorsLimit %+v, want %+v", tc.line, got, tc.want)
		}
	}
}

// Ground: boundary — a limit that cannot be read is a config error before any
// load, naming breakpoint.errors_limit. A key written with no value, as `~`
// or nothing, is refused too: reading it as "left out" would run the search
// at the default without a word.
func TestBreakpoint_ErrorsLimitRefusesWhatItCannotRead(t *testing.T) {
	for _, line := range []string{
		"    errors_limit: 0.1255%\n",
		"    errors_limit: 1\n",
		"    errors_limit: 0.5\n",
		"    errors_limit: 101%\n",
		"    errors_limit: \" 1%\"\n",
		"    errors_limit: \"1% \"\n",
		"    errors_limit: -1%\n",
		"    errors_limit: ~\n",
		"    errors_limit: null\n",
		"    errors_limit:\n",
		"    errors_limit: \"\"\n",
		"    errors_limit: [1%]\n",
	} {
		_, err := config.Parse([]byte(searchYAML + line))
		if err == nil || !strings.Contains(err.Error(), "breakpoint.errors_limit") {
			t.Errorf("%q: err %v, want a refusal naming breakpoint.errors_limit", line, err)
		}
	}
}
