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
	"strings"
	"testing"
	"time"
)

// errorsLimitSection is a short search on the fake target with the given
// lines added to the breakpoint section.
func errorsLimitSection(extra string) string {
	return "  breakpoint:\n    from: 50\n    to: 80\n    settle: 50ms\n    hold: 300ms\n" + extra
}

// Ground: contract — load.breakpoint.errors_limit reaches the search and its
// JSON, as written; left out it is the default 1%. The rule that judges a step
// by it is in pkg/breakpoint.
func TestErrorsLimit_TheKeyReachesTheReport(t *testing.T) {
	for name, tc := range map[string]struct{ line, want string }{
		"left out":  {"", "1%"},
		"a percent": {"    errors_limit: 0.125%\n", "0.125%"},
		"all":       {"    errors_limit: 100%\n", "100%"},
	} {
		res := runCLI(t.Context(), t, 60*time.Second, "-fake", "-output", "json",
			"-c", writeSearchOf(t, closedPort(t), errorsLimitSection(tc.line), "200ms", checkMethod))
		wantExit(t, res, 0)

		if got := jsonAt(t, decodeOnly(t, res.stdout), "breakpoint", "errors_limit"); got != tc.want {
			t.Errorf("%s: breakpoint.errors_limit = %v, want %q", name, got, tc.want)
		}
	}
}

// Ground: contract — an errors_limit that cannot be read is exit 1 before the
// run, naming the key; no report. An empty one is no "left out".
func TestErrorsLimit_ABadValueIsExit1(t *testing.T) {
	for name, line := range map[string]string{
		"no sign":        "    errors_limit: 1\n",
		"over 100":       "    errors_limit: 101%\n",
		"four digits":    "    errors_limit: 0.1255%\n",
		"a space":        "    errors_limit: \" 1%\"\n",
		"a null":         "    errors_limit: ~\n",
		"nothing":        "    errors_limit:\n",
		"also in a pass": "    errors_limit: 1\n    pass:\n      held_rps: 63\n",
	} {
		t.Run(name, func(t *testing.T) {
			res := runCLI(t.Context(), t, 10*time.Second, "-fake",
				"-c", writeSearchOf(t, closedPort(t), errorsLimitSection(line), "200ms", checkMethod))

			wantExit(t, res, 1)
			if res.err == nil || !strings.Contains(res.err.Error(), "breakpoint.errors_limit") || res.stdout != "" {
				t.Errorf("err %v stdout %q, want an error naming breakpoint.errors_limit and no report", res.err, res.stdout)
			}
		})
	}
}
