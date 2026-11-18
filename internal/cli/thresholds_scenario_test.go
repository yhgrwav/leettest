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
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

// Test 12 (reviewer round 0, scenario-pass, unit U2). A repeated method's step
// is one name on the screen: the cell in "Thresholds:" is the cell of the same
// row in the table of methods, "<method> (<scenario>, step <n>)", and the
// plain row of the same method stays the bare name in both.
// Ground: contract — thresholds[].method is the key of the judged row, in text
// and in JSON alike.
func TestThresholds_ARepeatedStepIsNamedAsInTheTableOfMethods(t *testing.T) {
	const step, plain = "/svc.S/Get (s, step 2)", "/svc.S/Get"

	run := RunReport{
		Report: engine.Report{
			Sent: 200,
			Methods: []engine.MethodReport{
				{Method: plain, Sent: 100},
				{Method: step, Sent: 100, Failed: 50},
			},
		},
		Thresholds: []threshold.Result{
			{Method: plain, Key: threshold.Errors, Verdict: threshold.VerdictPass, LimitPercent: "1%", Total: 100},
			{Method: step, Key: threshold.Errors, Verdict: threshold.VerdictFail, LimitPercent: "1%", Bad: 50, Total: 100},
		},
	}

	text := printedWithLimits(run)

	table, block, found := strings.Cut(text, "Thresholds:")
	if !found {
		t.Fatalf("no Thresholds block:\n%s", text)
	}

	cell := displayMethod(step)
	for name, part := range map[string]string{"table of methods": table, "thresholds block": block} {
		if !strings.Contains(part, cell+" ") {
			t.Errorf("the %s has no %q:\n%s", name, cell, text)
		}
	}

	var bare []string
	for _, line := range strings.Split(block, "\n") {
		// The columns are two spaces apart; a step's key goes on with one space and a bracket.
		if strings.HasPrefix(strings.TrimSpace(line), displayMethod(plain)+"  ") {
			bare = append(bare, line)
		}
	}
	if len(bare) != 1 {
		t.Errorf("%d rows in the thresholds block for the bare %q, want 1:\n%s", len(bare), displayMethod(plain), block)
	}
}
