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
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// runPanel is one run's live numbers and their drawing: the summary and the
// per-method panels, fed from snapshots of one engine. The plain run's view
// has one; a search's view has one per run.
type runPanel struct {
	text   Text
	styles styles
	width  int

	snapshot engine.Snapshot
	live     *engine.LiveBuffer
	// percentilesAt is when the snapshot's percentiles were last recomputed.
	// Copying a distribution holds its lock while recording waits, so it is
	// done once a second; a person cannot tell that from every frame.
	percentilesAt time.Time
	warmup        time.Duration
	// search is a run inside a breaking-point search: it has no per-method tab.
	search    bool
	overall   history
	perMethod map[string]*history
}

func newRunPanel(warmup time.Duration) runPanel {
	return runPanel{warmup: warmup, perMethod: make(map[string]*history), live: engine.NewLiveBuffer()}
}

// refresh takes a snapshot of eng and adds it to the histories.
func (m *runPanel) refresh(eng *engine.Engine) {
	fresh := time.Since(m.percentilesAt) >= percentileEvery
	eng.SnapshotInto(&m.snapshot, m.live, fresh)
	if fresh {
		m.percentilesAt = time.Now()
	}

	m.overall.push(m.snapshot.RPS, m.snapshot.P50, m.snapshot.P90, m.snapshot.P99)

	for _, method := range m.snapshot.Methods {
		h, ok := m.perMethod[method.Method]
		if !ok {
			h = &history{}
			m.perMethod[method.Method] = h
		}
		h.push(method.RPS, method.P50, method.P90, method.P99)
	}
}

func (m *runPanel) viewWidth() int {
	if m.width < minWidth {
		return 72
	}

	return m.width
}

// notSentLine is its own line so no width drops it, and absent when every
// call went out.
func (m *runPanel) notSentLine(width, n int) string {
	if n == 0 {
		return ""
	}

	return fitStatLine(m.styles, width, countField(m.text.NotSent(), n, 0)) + "\n"
}

func (m *runPanel) summary(width int) string {
	s := m.snapshot

	var b strings.Builder

	b.WriteString(fitStatLine(m.styles, width,
		countField(m.text.Sent(), s.Sent, 1),
		statField{label: "rps", value: fmt.Sprintf("%.0f", s.RPS), drop: 2},
		countField(m.text.InFlight(), s.InFlight, 0),
		statField{label: m.text.Errors(), value: m.errorShare(s.Sent, s.Failed)},
	))
	b.WriteString("\n")
	b.WriteString(statLine(m.styles,
		[2]string{"p50", formatQuantile(s.P50)},
		[2]string{"p90", formatQuantile(s.P90)},
		[2]string{"p99", formatQuantile(s.P99)},
	))
	b.WriteString("\n" + m.notSentLine(width, s.NotSent) + "\n")

	b.WriteString(m.gaugeRow("rps", s.RPS, m.totalTarget(), fmt.Sprintf("%.0f", s.RPS)))
	b.WriteString("\n")
	b.WriteString(m.gaugeRow(m.text.InFlight(), float64(s.InFlight), float64(max(s.InFlight, 1)*2), formatCount(s.InFlight)))
	b.WriteString("\n\n")

	b.WriteString(m.sparkRow(m.text.Rate(), m.overall.rps, "", func(v float64) string {
		return fmt.Sprintf("%.0f", v)
	}))
	b.WriteString("\n")
	b.WriteString("\n")
	b.WriteString(m.latencyChart(m.overall.points))

	if full, short := m.note(); full != "" {
		note := "> " + full
		if lipgloss.Width(note) > width {
			note = "> " + short
		}
		b.WriteString("\n\n")
		b.WriteString(m.styles.note.Render(note))
	}

	return b.String()
}

func (m *runPanel) method(width, index int) string {
	if index >= len(m.snapshot.Methods) {
		return m.styles.muted.Render(ellipsis)
	}

	method := m.snapshot.Methods[index]

	var b strings.Builder

	b.WriteString(m.styles.value.Render(displayMethod(method.Method)))
	b.WriteString("\n\n")

	b.WriteString(fitStatLine(m.styles, width,
		countField(m.text.Sent(), method.Sent, 1),
		statField{label: "rps", value: fmt.Sprintf("%.0f", method.RPS), drop: 2},
		statField{label: m.text.Errors(), value: m.errorShare(method.Sent, method.Failed)},
	))
	b.WriteString("\n")
	b.WriteString(statLine(m.styles,
		[2]string{"p50", formatQuantile(method.P50)},
		[2]string{"p90", formatQuantile(method.P90)},
		[2]string{"p99", formatQuantile(method.P99)},
	))
	b.WriteString("\n\n")

	b.WriteString(m.gaugeRow("rps", method.RPS, float64(method.TargetRPS), fmt.Sprintf("%.0f / %d", method.RPS, method.TargetRPS)))
	b.WriteString("\n\n")

	h := m.perMethod[method.Method]
	if h == nil {
		h = &history{}
	}

	b.WriteString(m.sparkRow(m.text.Rate(), h.rps, "", func(v float64) string {
		return fmt.Sprintf("%.0f", v)
	}))
	b.WriteString("\n")
	b.WriteString("\n")
	b.WriteString(m.latencyChart(h.points))

	return b.String()
}

// latencyChart is three sparkline rows, p50 to p99, on one shared scale: the
// height of a cell means the same value in every row, so the gap between p50
// and p99 is visible at a glance. The scale is written once, above the rows.
func (m *runPanel) latencyChart(points []point) string {
	series := latencySeriesOf(points)
	low, high, ok := latencyScale(series)
	width := m.sparkCells()

	scale := "-"
	if ok {
		scale = formatDuration(millis(low)) + " - " + formatDuration(millis(high))
	}

	var b strings.Builder

	b.WriteString(m.styles.label.Render(padRight(m.text.Latency(), sparkLabelWidth)))
	b.WriteString(m.styles.muted.Render(scale))

	for _, s := range series {
		b.WriteString("\n")
		b.WriteString(m.styles.label.Render(fmt.Sprintf("  %-8s", s.label)))
		b.WriteString(latencyCells(m.styles, s.values, s.bounds, low, high, width))
		b.WriteString(m.styles.pad(2))
		b.WriteString(m.styles.value.Render(latencyValue(s.values, s.bounds)))
	}

	return b.String()
}

// Columns a spark row keeps beside its cells: the label and the value after.
const (
	sparkLabelWidth = 10
	sparkValueWidth = 18
)

// sparkCells is how many cells a spark row gets: as many as fit beside its
// label and value, up to the length of the history, and never so few the line
// means nothing.
func (m *runPanel) sparkCells() int {
	room := contentWidth(m.viewWidth()) - sparkLabelWidth - 2 - sparkValueWidth

	return min(max(room, 8), historyLimit)
}

func (m *runPanel) sparkRow(label string, values []float64, unit string, format func(float64) string) string {
	return m.styles.label.Render(padRight(label, sparkLabelWidth)) +
		sparkline(m.styles, values, m.sparkCells()) +
		m.styles.pad(2) +
		sparkRange(m.styles, values, unit, format)
}

func (m *runPanel) gaugeRow(label string, value, limit float64, text string) string {
	return m.styles.label.Render(padRight(label, sparkLabelWidth)) +
		gauge(m.styles, value, limit, min(max(gaugeWidth, m.sparkCells()/2), m.sparkCells())) + m.styles.pad(2) +
		m.styles.value.Render(text)
}

// note returns the live view's note in full and in the short form a narrow
// frame takes instead of wrapping it.
func (m *runPanel) note() (full, short string) {
	s := m.snapshot

	if m.warmup > 0 && s.Elapsed < m.warmup {
		return m.text.WarmupNote(formatDuration(m.warmup-s.Elapsed), s.WarmupSent), m.text.WarmupNoteShort(s.WarmupSent)
	}
	if s.Sent > 0 && float64(s.Failed)/float64(s.Sent) > 0.05 {
		if m.search {
			return m.text.SearchErrorsNote(), m.text.SearchErrorsNoteShort()
		}

		return m.text.ErrorsNote(), m.text.ErrorsNoteShort()
	}
	if s.InFlight > 0 && float64(s.InFlight) > m.totalTarget() {
		return m.text.InFlightNote(), m.text.InFlightNoteShort()
	}

	return "", ""
}

func (m *runPanel) errorShare(sent, failed int) string {
	if sent == 0 {
		return "0%"
	}

	return fmt.Sprintf("%.1f%%", float64(failed)/float64(sent)*100)
}

func (m *runPanel) totalTarget() float64 {
	var total float64

	for _, method := range m.snapshot.Methods {
		total += float64(method.TargetRPS)
	}

	if total == 0 {
		return 1
	}

	return total
}
