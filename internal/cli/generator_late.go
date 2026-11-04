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
	"math"
	"slices"
	"strconv"
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

// The names of the limits in GeneratorLimit and in the JSON.
const (
	limitGOMAXPROCS = "gomaxprocs"
	limitCPUQuota   = "cpu_quota"
	limitCPU        = "cpu"
	limitSchedule   = "schedule"
	limitUnknown    = "unknown"
)

const limitAdvice = "lower the rate, or split it over several machines."

// cpuNotMeasuredLine is the verdict's last line when the generator's CPU is not
// known: nothing can be said of what limited it.
const cpuNotMeasuredLine = "The generator's CPU was not measured for a whole second and the report cannot name the limit: " + limitAdvice

// GeneratorLimit names the limit of its own the generator hit in a
// generator_late run: gomaxprocs, cpu_quota, cpu, schedule or unknown. ok is
// false when the run is not generator_late. Every limit but unknown needs the
// generator's CPU measured for at least one whole second.
func GeneratorLimit(run RunReport) (limit string, ok bool) {
	limit, _, ok = generatorLimitLine(run)

	return limit, ok
}

// generatorLimitLine is GeneratorLimit and the last line of the verdict for
// it. The first match wins: the user lowered GOMAXPROCS and the CPU was busy;
// the cgroup quota is below the CPUs and the CPU was busy; the CPU was busy;
// the CPU was quiet and the schedule not exact; else unknown. A lowered
// GOMAXPROCS with a quiet CPU is no limit: the Ps that were there stood idle.
func generatorLimitLine(run RunReport) (limit, line string, ok bool) {
	if !GeneratorLate(run) {
		return "", "", false
	}

	cpu := run.GeneratorCPU
	if cpu == nil || cpu.Counted == 0 {
		return limitUnknown, cpuNotMeasuredLine, true
	}

	busy := cpu.BusySeconds > 0

	if p := run.Passport; p != nil {
		reach := p.NumCPU
		belowCPUs := false

		var rounded int
		if q := p.CPUQuota; q != nil {
			rounded = int(math.Ceil(*q))
			belowCPUs = rounded < p.NumCPU
			reach = min(reach, rounded)
		}

		if busy && p.GOMAXPROCS < reach {
			return limitGOMAXPROCS, fmt.Sprintf("The generator ran on GOMAXPROCS %d of %d CPUs: raise GOMAXPROCS, %s",
				p.GOMAXPROCS, p.NumCPU, limitAdvice), true
		}

		if busy && belowCPUs {
			return limitCPUQuota, fmt.Sprintf("The generator used 90%% or more of its CPU quota (%s of %d CPUs) in %d of %d s: raise the quota, %s",
				strconv.FormatFloat(*p.CPUQuota, 'f', -1, 64), p.NumCPU, cpu.BusySeconds, cpu.Counted, limitAdvice), true
		}
	}

	if busy {
		return limitCPU, fmt.Sprintf("The generator used 90%% or more of the %s available to it in %d of %d s: %s",
			coresText(cpu.AvailableCores), cpu.BusySeconds, cpu.Counted, limitAdvice), true
	}

	if reasons, fixes := scheduleAdvice(run); reasons != "" {
		return limitSchedule, "The generator's CPU was not busy, and the schedule is not exact here (" + reasons + "): " +
			fixes + ", or lower the rate.", true
	}

	return limitUnknown, "The generator's CPU was not busy and the report cannot name the limit: " + limitAdvice, true
}

// scheduleAdvice is the reasons the run's schedule was not exact in the words
// of the schedule line, and what to change for them, once for each fix, both
// in the order of the line; "" for an exact schedule.
func scheduleAdvice(run RunReport) (reasons, fixes string) {
	host := scheduleHost(run.Passport)

	var words, fix []string

	for _, reason := range scheduleOrder {
		if !slices.Contains(run.ScheduleNotExact, reason) {
			continue
		}

		words = append(words, scheduleWords(reason, host))

		text := "run from Linux"
		if reason == engine.ScheduleGOMAXPROCS {
			text = "set GOMAXPROCS to 2 or more"
		}
		if !slices.Contains(fix, text) {
			fix = append(fix, text)
		}
	}

	return strings.Join(words, ", "), strings.Join(fix, ", ")
}

// generatorLimitJSON is the generator_limit key: nil when the run is not
// generator_late.
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
// generator, in the report's order. limitLine is the last line, the limit the
// generator hit (generatorLimitLine).
func generatorLateHeading(report engine.Report, limitLine string) string {
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
		limitLine)

	return b.String()
}
