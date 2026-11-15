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
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

// One run of a two-step scenario against the stub, its text on stdout and its
// JSON in the file asked for with -json-file: the two reports are of the same
// run, so the numbers a person reads and the numbers a script reads must be
// the same, and the run is clean (A7 i): exit 0, no invalid reason.
// Ground: contract — spec 7 and 8 end to end, the counters of #8 on both forms.
func TestRun_AScenarioHasTheSameNumbersInTextAndJSON(t *testing.T) {
	target := startReflectingTarget(t)
	path := newJSONPath(t)

	res := runCLI(t.Context(), t, 30*time.Second, "-c", scenarioE2EConfig(t, target.addr), "-json-file", path)

	if res.err != nil {
		t.Fatalf("run failed: %v\nstdout:\n%s", res.err, res.stdout)
	}

	var report struct {
		SchemaVersion  int      `json:"schema_version"`
		Outcome        string   `json:"outcome"`
		InvalidReasons []string `json:"invalid_reasons"`
		Methods        []struct {
			Method            string `json:"method"`
			PlannedRPSLow     int    `json:"planned_rps_low"`
			PlannedDurationUS int64  `json:"planned_duration_us"`
		} `json:"methods"`
		Scenarios []struct {
			Name         string         `json:"name"`
			Iterations   int            `json:"iterations"`
			Categories   map[string]int `json:"categories"`
			Observations int            `json:"observations"`
			Steps        []struct {
				Method      string `json:"method"`
				Reached     int    `json:"reached"`
				FailedHere  int    `json:"failed_here"`
				AbortedHere int    `json:"aborted_here"`
				NotSentHere int    `json:"not_sent_here"`
				NotReached  int    `json:"not_reached"`
			} `json:"steps"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(readFile(t, path), &report); err != nil {
		t.Fatalf("the JSON file is not a report: %v", err)
	}

	if report.SchemaVersion != 1 || report.Outcome != "complete" || len(report.InvalidReasons) != 0 {
		t.Errorf("schema %d, outcome %q, invalid reasons %v, want 1, complete, none",
			report.SchemaVersion, report.Outcome, report.InvalidReasons)
	}
	if len(report.Scenarios) != 1 || report.Scenarios[0].Name != "pay" || len(report.Scenarios[0].Steps) != 2 {
		t.Fatalf("scenarios = %+v, want one scenario \"pay\" of two steps", report.Scenarios)
	}

	sc := report.Scenarios[0]
	sum := 0
	for _, n := range sc.Categories {
		sum += n
	}
	if len(sc.Categories) != 11 || sum != sc.Iterations || sc.Iterations <= 0 {
		t.Errorf("categories %v add up to %d over %d iterations, want 11 names adding up to the iterations (more than 0)",
			sc.Categories, sum, sc.Iterations)
	}
	if sc.Observations > sc.Iterations {
		t.Errorf("%d observations over %d iterations", sc.Observations, sc.Iterations)
	}

	for k, s := range sc.Steps {
		if s.Reached+s.NotReached != sc.Iterations {
			t.Errorf("step %d: reached %d + not reached %d != iterations %d", k+1, s.Reached, s.NotReached, sc.Iterations)
		}
	}
	if s := sc.Steps[0]; sc.Steps[1].Reached != s.Reached-s.FailedHere-s.AbortedHere-s.NotSentHere {
		t.Errorf("step 2 reached %d, want step 1's reached - failed - aborted - not sent", sc.Steps[1].Reached)
	}

	// The step methods are ordinary rows; the step has no plan of its own.
	byName := map[string]int{}
	for i, m := range report.Methods {
		byName[strings.TrimPrefix(m.Method, "/")] = i
	}
	head, step := byName[checkMethod], byName[unaryCallMethod]
	if _, ok := byName[checkMethod]; !ok || report.Methods[head].PlannedRPSLow != 40 {
		t.Errorf("methods = %+v, want %s planned at 40", report.Methods, checkMethod)
	}
	if m := report.Methods[step]; m.PlannedRPSLow != 0 || m.PlannedDurationUS != 0 {
		t.Errorf("%s plans %d rps for %d us, want 0 and 0: a step has no plan of its own", unaryCallMethod, m.PlannedRPSLow, m.PlannedDurationUS)
	}

	// The same numbers printed: the block's first line starts with the
	// iterations, and the two step rows with the reached counts.
	lines := strings.Split(res.stdout, "\n")
	start := -1
	for i, l := range lines {
		if l == "scenario pay" {
			start = i
		}
	}
	if start < 0 || start+6 > len(lines) {
		t.Fatalf("no scenario block in the text report:\n%s", res.stdout)
	}
	if got := strings.Fields(lines[start+1])[0]; got != strconv.Itoa(sc.Iterations) {
		t.Errorf("text says %s iterations, JSON %d", got, sc.Iterations)
	}
	for k, s := range sc.Steps {
		row := strings.Fields(lines[start+4+k])
		want := []string{strconv.Itoa(s.Reached), strconv.Itoa(s.FailedHere), strconv.Itoa(s.AbortedHere), strconv.Itoa(s.NotSentHere), strconv.Itoa(s.NotReached)}
		if len(row) != 7 || strings.Join(row[2:], " ") != strings.Join(want, " ") {
			t.Errorf("step %d: text row %q, JSON counters %v", k+1, lines[start+4+k], want)
		}
	}
}
