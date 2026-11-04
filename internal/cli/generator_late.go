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
	"strings"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// reasonGeneratorLate is the invalid_reasons name of a run whose p99 tail the
// generator's lag decided.
const reasonGeneratorLate = "generator_late"

// GeneratorLate says the generator's lag decided the run's p99 tail
// (engine.Report.GeneratorDecidedTail): the wait verdict names the generator
// and a method with enough observations for its p99 to be more than the
// maximum was moved by it, or the generator left calls unsent. Such latencies
// include the delay of this machine, so the run is invalid. A step of a
// breaking-point search is never late in this sense: the search ends on the
// generator with its own cause.
func GeneratorLate(run RunReport) bool {
	return !run.searchStep && run.GeneratorDecidedTail()
}

// GeneratorLimit names the limit of its own the generator hit in a
// generator_late run: gomaxprocs, cpu_quota, cpu, schedule or unknown. ok is
// false when the run is not generator_late.
func GeneratorLimit(_ RunReport) (limit string, ok bool) {
	return "", false
}

func generatorLimitJSON(run RunReport) *string {
	limit, ok := GeneratorLimit(run)
	if !ok {
		return nil
	}

	return &limit
}

// generatorLateHeading is the verdict sentence of a late generator, up to the
// advice. It names the methods engine.MethodReport.LateByGenerator picks: those
// the lag moved with enough observations and those that lost calls to the
// generator, in the report's order.
func generatorLateHeading(report engine.Report) string {
	var names []string
	for i := range report.Methods {
		if m := &report.Methods[i]; m.LateByGenerator() {
			names = append(names, displayMethod(m.Method))
		}
	}
	if len(names) == 0 {
		// Not reachable from an engine report: a verdict on the generator has a
		// moved method or a method that lost calls. An empty list would read as
		// no one.
		for i := range report.Methods {
			names = append(names, displayMethod(report.Methods[i].Method))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "invalid run: the generator fell behind its schedule and decided the p99 tail of %s: start lag p99 %s, max %s",
		strings.Join(names, ", "), formatQuantile(report.StartLagP99), formatDuration(report.StartLagMax))
	if n := report.NotSentGenerator; n > 0 {
		fmt.Fprintf(&b, ", %d calls never sent", n)
	}
	b.WriteString(".\nThe latencies include this machine's delay and do not measure the target.\n" +
		"Give the generator more CPU, lower the rate, or split it over several machines.")

	return b.String()
}
