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
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// scenarioBlockNote is the one line under every scenario block.
const scenarioBlockNote = "  step latency is measured from the end of the previous step; " +
	"the rows above count calls, the iteration counts runs"

// categoryOrder is the contract order of the categories: success first, then
// the order of the names in the JSON.
var categoryOrder = [...]engine.Category{
	engine.CategorySuccess, engine.CategoryClientFault, engine.CategoryOverload, engine.CategoryServerFault,
	engine.CategoryTimeout, engine.CategoryCutOff, engine.CategoryUnreachable, engine.CategoryClientError,
	engine.CategoryBadResponse, engine.CategoryAborted, engine.CategoryUnknown,
}

// printScenarioBlocks prints one block per scenario of the run, after the
// method table: the iteration as a unit, and how far the iterations got at
// each step. The calls of the steps are rows of the method table.
func printScenarioBlocks(w io.Writer, run RunReport) {
	for i := range run.Scenarios {
		printScenarioBlock(w, run, &run.Scenarios[i])
	}
}

func printScenarioBlock(w io.Writer, run RunReport, s *engine.ScenarioReport) {
	fmt.Fprintf(w, "\nscenario %s\n", s.Name)

	noun := "iterations"
	if s.Iterations == 1 {
		noun = "iteration"
	}
	line := fmt.Sprintf("  %d %s", s.Iterations, noun)
	if rate := scenarioRate(run, s); rate != "" {
		line += ", started at " + rate + "/s"
	}
	if s.Iterations > 0 {
		var parts []string
		for _, c := range categoryOrder {
			if n := s.ByCategory[c]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", c.Name(), n))
			}
		}
		line += ": " + strings.Join(parts, ", ")
	}
	fmt.Fprintln(w, line)

	p99 := formatQuantile(s.P99)
	if !s.P99.Defined {
		p99 = fmt.Sprintf("too few observations (%d of %d)", s.Latencies, metrics.MinCount(0.99))
	}
	fmt.Fprintf(w, "  iteration latency (tick to end of the last step): p50 %s, p99 %s\n", formatQuantile(s.P50), p99)

	const row = "  %-4s %-44s %8s %11s %12s %8s %11s\n"
	fmt.Fprintf(w, row, "step", "method", "reached", "failed here", "aborted here", "not sent", "not reached")
	for k, st := range s.Steps {
		fmt.Fprintf(w, row, strconv.Itoa(k+1), displayMethod(st.Method), strconv.Itoa(st.Reached),
			strconv.Itoa(st.FailedHere), strconv.Itoa(st.AbortedHere), strconv.Itoa(st.NotSentHere),
			strconv.Itoa(st.NotReached))
	}

	fmt.Fprintln(w, scenarioBlockNote)
}

// scenarioRate is the rate the iterations were started at: the plan of the
// head call, the first step, in the form of its plan line. Empty when the
// report has no row for the head call.
func scenarioRate(run RunReport, s *engine.ScenarioReport) string {
	if len(s.Steps) == 0 {
		return ""
	}

	for i := range run.Methods {
		if m := &run.Methods[i]; m.Method == s.Steps[0].Method {
			if m.RPSHigh != m.RPSLow {
				return fmt.Sprintf("%d-%d", m.RPSLow, m.RPSHigh)
			}

			return strconv.Itoa(m.RPSLow)
		}
	}

	return ""
}

// isStepMethod says the method is a step of some scenario with no plan of its
// own: its calls are started by the head call's tick, so there is no rate the
// user set to show beside them.
func isStepMethod(report engine.Report, m *engine.MethodReport) bool {
	if m.PlannedDuration > 0 {
		return false
	}

	return slices.ContainsFunc(report.Scenarios, func(s engine.ScenarioReport) bool {
		return slices.ContainsFunc(s.Steps[min(1, len(s.Steps)):], func(st engine.ScenarioStepReport) bool {
			return st.Method == m.Method
		})
	})
}
