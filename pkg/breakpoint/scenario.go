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

package breakpoint

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/share"
)

// The search of a scenario steps the rate of the head, in iterations per
// second, and judges every row of the report (the head and each step) on its
// own, as a plain call is judged as one row. See docs/decisions.md, «Search
// with a scenario».

// scenarioRun says the report is a run of a scenario: its rows are steps.
func scenarioRun(r engine.Report) bool {
	return len(r.Scenarios) > 0
}

// rate is rps as the texts of a search print it: a scenario's step is
// iterations per second.
func rate(rps int, scenario bool) string {
	if scenario {
		return fmt.Sprintf("%d iterations/s", rps)
	}

	return fmt.Sprintf("%d rps", rps)
}

// rate is rate for the unit of this search: known from the runs seen, or, before
// the first, from the plan's steps.
func (s *search) rate(rps int) string {
	return rate(rps, s.scenario || len(s.plan.StepTimeouts) > 0)
}

// rowsOf are the rows of a scenario's report in the order of the steps, the
// head first; Methods is sorted by key.
func rowsOf(r engine.Report) []*engine.MethodReport {
	rows := make([]*engine.MethodReport, len(r.Methods))
	for i := range r.Methods {
		rows[i] = &r.Methods[i]
	}
	slices.SortStableFunc(rows, func(a, b *engine.MethodReport) int { return a.Step - b.Step })

	return rows
}

// stepsOf is how many steps the scenario has, the head included.
func stepsOf(r engine.Report, rows []*engine.MethodReport) int {
	n := 0
	for i := range r.Scenarios {
		n = max(n, len(r.Scenarios[i].Steps))
	}
	for _, m := range rows {
		n = max(n, m.Step)
	}

	return n
}

// rowName names a row as the reasons print it: its step of n and its key.
func rowName(m *engine.MethodReport, n int) string {
	return fmt.Sprintf("step %d of %d, %s", m.Step, n, m.Method)
}

// rowFailures counts a row's calls as failures() counts a run's: over the calls
// that reached the row, those lost to the connection among them.
func rowFailures(m *engine.MethodReport) (bad, client, total int) {
	client = m.Rejected.Count + m.ClientError + m.BadResponse.Count

	return m.Failed + m.NotSentConnection, client, m.Sent + m.NotSentConnection
}

// undersentRows names a scenario's run whose generator or streams did not
// deliver its schedule: the head's Sent short of the scheduled iterations by
// over 0.1% (at least one), or a later step that lost over 0.1% of the calls
// that reached it (at least one) to something other than the connection.
func undersentRows(rps int, r engine.Report) (cause Cause, why string) {
	rows := rowsOf(r)
	n := stepsOf(r, rows)

	var headSent int
	for _, m := range rows {
		if m.Step == 1 {
			headSent = m.Sent
		}
	}
	if headSent < r.Scheduled-max(1, r.Scheduled/1000) {
		return CauseGenerator, fmt.Sprintf("the generator sent %d of %d scheduled iterations at %s; the target above that is untested",
			headSent, r.Scheduled, rate(rps, true))
	}

	for _, m := range rows {
		lost, reached := m.NotSent-m.NotSentConnection, m.Sent+m.NotSent
		if m.Step <= 1 || lost <= max(1, reached/1000) {
			continue
		}

		// The reason as threshold's runSideOf names it.
		reason, by := CauseGenerator, "the generator"
		if m.NotSentGenerator == 0 && m.NotSentStream > 0 {
			reason, by = CauseStreamWait, "a stream"
		}

		return reason, fmt.Sprintf("%s: %d of %d calls were not sent for %s at %s; the target above that is untested",
			rowName(m, n), lost, reached, by, rate(rps, true))
	}

	return NoCause, ""
}

// rowCall is one row's verdicts of a step: the text of each criterion that
// fired, empty when it did not.
type rowCall struct {
	row       *engine.MethodReport
	name      string
	errs      string
	undecided string
	knee      string
}

// brokenRows judges the rows of a scenario's run the held step of which was
// not cut short by the run's side. Tiers: a failure share that breaks on any
// row, then one that is over the limit only with the client's own failures (the
// run's limit, not a break), then the knee of any row against its own baseline.
// A broken step is named by its first broken row, in the order of the steps,
// whatever the tier, and says every broken row. A row nobody reached gives no
// verdict.
func (s *search) brokenRows(r engine.Report, kind Kind) (broken bool, cause Cause, why string, row *Row) {
	limit := s.plan.EffectiveErrorsLimit()
	rows := rowsOf(r)
	n := stepsOf(r, rows)

	var calls []rowCall
	for _, m := range rows {
		bad, client, total := rowFailures(m)
		if total == 0 {
			continue
		}

		c := rowCall{row: m, name: rowName(m, n)}
		switch verdict := share.Decide(bad, client, total, limit); {
		case verdict == share.Breaks:
			c.errs = fmt.Sprintf("%s over errors_limit %s", failedCounts(bad, client, m.NotSentConnection, total, limit, verdict), limit.Text)
		case verdict == share.Undecided && kind != Probe:
			c.undecided = fmt.Sprintf("%s over errors_limit %s only with the client's; the target's own are within it, the target above that is untested",
				failedCounts(bad, client, m.NotSentConnection, total, limit, verdict), limit.Text)
		}

		// The printed p99 is the bucket's top; the knee is crossed only by what
		// is certainly over, the bucket's bottom.
		if base := s.rowBase[m.Step]; base > 0 && m.P99.Defined && m.P99.Floor() > KneeRatio*base {
			c.knee = fmt.Sprintf("p99 %v = %.1fx baseline %v", short(m.P99.Value), float64(m.P99.Value)/float64(base), short(base))
		}

		calls = append(calls, c)
	}

	var anyErrs, anyKnee bool
	var undecided []string
	for _, c := range calls {
		anyErrs = anyErrs || c.errs != ""
		anyKnee = anyKnee || c.knee != ""

		if c.undecided != "" {
			undecided = append(undecided, c.name+": "+c.undecided)
		}
	}

	switch {
	case anyErrs:
		cause = CauseErrors
	case len(undecided) > 0:
		return false, CauseClientErrors, strings.Join(undecided, "; "), nil
	case anyKnee:
		cause = CauseP99VsBase
	default:
		return false, NoCause, "", nil
	}

	var texts []string
	for _, c := range calls {
		var parts []string
		for _, part := range []string{c.errs, c.knee} {
			if part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			continue
		}
		if row == nil {
			row = &Row{Step: c.row.Step, Method: c.row.Method}
		}
		texts = append(texts, c.name+": "+strings.Join(parts, ", "))
	}

	return true, cause, "first to break: " + strings.Join(texts, "; "), row
}

// holdRows lowers each row's baseline by a held step: only a defined p99 does,
// a row too short for one says nothing, and zero would be the lowest.
func (s *search) holdRows(r engine.Report) {
	if s.rowBase == nil {
		s.rowBase = make(map[int]time.Duration)
	}
	for i := range r.Methods {
		m := &r.Methods[i]
		if base := s.rowBase[m.Step]; m.P99.Defined && (base == 0 || m.P99.Value < base) {
			s.rowBase[m.Step] = m.P99.Value
		}
	}
}

// noBaseline says nothing held yet measured a p99 to be far from.
func (s *search) noBaseline() bool {
	if s.scenario {
		return len(s.rowBase) == 0
	}

	return s.baseline == 0
}

// recovered says a probe's p99 is within Recovered times the baseline: of the
// run, or of each row of a scenario that has one. A row with no p99 in the
// probe says nothing.
func (s *search) recovered(r engine.Report) bool {
	if !scenarioRun(r) {
		return s.baseline == 0 || float64(p99(r)) <= Recovered*float64(s.baseline)
	}

	for i := range r.Methods {
		m := &r.Methods[i]
		if base := s.rowBase[m.Step]; base > 0 && m.P99.Defined && float64(m.P99.Value) > Recovered*float64(base) {
			return false
		}
	}

	return true
}
