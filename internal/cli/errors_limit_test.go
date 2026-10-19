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

package cli

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	shr "github.com/yhgrwav/leettest/pkg/share"
)

// Ground: contract — the JSON says the share of failed calls the search
// judged each step by, as written in the config, "1%" when the config set
// none.
func TestBreakpointJSON_SaysTheErrorsLimit(t *testing.T) {
	for name, tc := range map[string]struct {
		limit shr.Percent
		want  string
	}{
		"none set":      {shr.Percent{}, "1%"},
		"as written":    {shr.Percent{Thousandths: 125, Text: "0.125%"}, "0.125%"},
		"a whole limit": {shr.Percent{Thousandths: 100000, Text: "100%"}, "100%"},
	} {
		plan := bpPlan
		plan.ErrorsLimit = tc.limit

		var buf bytes.Buffer
		run := BreakpointRun{Target: "t:1", Method: "pkg.Svc/Do", Plan: plan, Result: outcomes["broke"].res}
		if err := WriteBreakpointJSON(&buf, run); err != nil {
			t.Fatalf("%s: write: %v", name, err)
		}
		var out map[string]any
		if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
			t.Fatalf("%s: json: %v", name, err)
		}
		bp, _ := out["breakpoint"].(map[string]any)
		if bp["errors_limit"] != tc.want {
			t.Errorf("%s: breakpoint.errors_limit = %v, want %q", name, bp["errors_limit"], tc.want)
		}
	}
}

// Ground: contract — a search ended by failures only the client had says
// client_errors, keeps what held and is no break.
func TestBreakpointJSON_ClientErrorsAreTheRunsLimit(t *testing.T) {
	res := breakpoint.Result{
		Outcome: breakpoint.RunLimit, Held: 125, Broke: 156, Cause: breakpoint.CauseClientErrors,
		Steps: []breakpoint.Step{
			held(100), held(125),
			bpRun(breakpoint.RateStep, 156, 624, 21*time.Millisecond, false, breakpoint.CauseClientErrors,
				"failed 20 of 624 calls (3.2%: 20 failed, 20 client side) over errors_limit 1%"),
		},
	}
	bp, _ := searchJSON(t, res)["breakpoint"].(map[string]any)
	if bp["outcome"] != "run_limit" || bp["why"] != "client_errors" || bp["held_rps"] != 125.0 || bp["broke_rps"] != 156.0 {
		t.Errorf("outcome %v why %v held %v broke %v, want run_limit client_errors 125 156",
			bp["outcome"], bp["why"], bp["held_rps"], bp["broke_rps"])
	}
}
