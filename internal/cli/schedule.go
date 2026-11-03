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
	"strings"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// scheduleOrder is the order the engine names the reasons in, and the line
// does.
var scheduleOrder = []string{engine.ScheduleNoPreciseSleep, engine.ScheduleClockStep, engine.ScheduleGOMAXPROCS}

// scheduleClaim is what a schedule that was not exact does to the numbers: the
// p99 without the client-side waits takes the whole start lag off, the
// latencies do not.
const scheduleClaim = ": calls may start late, and latencies include that delay (p99 without client-side waits does not)."

// scheduleWords is a reason in the line's words; goos names the host in the
// reason that depends on it.
func scheduleWords(reason, goos string) string {
	switch reason {
	case engine.ScheduleNoPreciseSleep:
		return "no microsecond sleep on " + goos
	case engine.ScheduleClockStep:
		return "clock step 1us or coarser"
	case engine.ScheduleGOMAXPROCS:
		return "GOMAXPROCS below 2"
	}

	return reason
}

// scheduleHost is the OS the passport names, "this host" without one.
func scheduleHost(p *Passport) string {
	if p == nil || p.OS == "" {
		return "this host"
	}

	return p.OS
}

// printScheduleLine writes the schedule line for parts, the reasons in words;
// nothing for no reasons. tail is the sentence of the run's own numbers, or "".
func printScheduleLine(w io.Writer, parts []string, tail string) {
	if len(parts) == 0 {
		return
	}

	fmt.Fprintf(w, "schedule: not exact (%s)%s%s\n", strings.Join(parts, ", "), scheduleClaim, tail)
}

// printRunSchedule writes the line of a run: the reasons its engine named and,
// when the run has a start lag to show, the lag.
func printRunSchedule(w io.Writer, run RunReport) {
	host := scheduleHost(run.Passport)

	var parts []string

	for _, reason := range scheduleOrder {
		if slices.Contains(run.ScheduleNotExact, reason) {
			parts = append(parts, scheduleWords(reason, host))
		}
	}

	tail := ""
	if run.StartLagP99.Defined || run.StartLagMax > 0 {
		tail = fmt.Sprintf(" Start lag in this run: p99 %s, max %s.", formatQuantile(run.StartLagP99), formatLatency(run.StartLagMax))
	}

	printScheduleLine(w, parts, tail)
}

// printSearchSchedule writes the one line of a search: the union of the
// reasons of its steps, each step having a clock of its own. A reason that was
// not in every step says in how many it was.
func printSearchSchedule(w io.Writer, p *Passport, steps []breakpoint.Step) {
	host := scheduleHost(p)

	var parts []string

	for _, reason := range scheduleOrder {
		n := 0

		for i := range steps {
			if slices.Contains(steps[i].Report.ScheduleNotExact, reason) {
				n++
			}
		}

		switch {
		case n == 0:
		case n == len(steps):
			parts = append(parts, scheduleWords(reason, host))
		default:
			parts = append(parts, fmt.Sprintf("%s in %d of %d steps", scheduleWords(reason, host), n, len(steps)))
		}
	}

	printScheduleLine(w, parts, "")
}

// scheduleText is how the compare report names a schedule: exact, or not exact
// with the engine's reasons as the JSON has them; none for a report written
// before the field (its list is nil, the writer's is never).
func scheduleText(s jsonSchedule) string {
	switch {
	case s.NotExactReasons == nil:
		return "none"
	case len(s.NotExactReasons) == 0:
		return "exact"
	}

	return "not exact (" + strings.Join(s.NotExactReasons, ", ") + ")"
}

// scheduleJSON is the schedule of a run report; the list is never nil.
func scheduleJSON(reasons []string) jsonSchedule {
	return jsonSchedule{Exact: len(reasons) == 0, NotExactReasons: append([]string{}, reasons...)}
}
