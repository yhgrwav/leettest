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
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// Messages the search sends its screen, between runs.
type (
	searchRunMsg struct {
		eng *engine.Engine
		run breakpoint.Run
	}
	searchStepMsg     struct{ step breakpoint.Step }
	searchCooldownMsg struct {
		d   time.Duration
		rps int
	}
	searchDoneMsg struct {
		res breakpoint.Result
	}
)

// searchModel is the full-screen view of a breaking-point search: where the
// search is, the current run's panel, and the runs so far. The current run
// is drawn by a plain run's model, which knows nothing of the search: the
// search sets its engine and panel per run and draws around it.
type searchModel struct {
	base   *model
	method string
	plan   breakpoint.Plan
	// worst is the plan line's "at most", connecting included.
	worst string
	now   func() time.Time
	start time.Time

	run     breakpoint.Run
	running bool
	// coolUntil and coolRPS are the cooldown under way; zero when none.
	coolUntil time.Time
	coolRPS   int
	rows      []breakpoint.Step
	result    *breakpoint.Result
}

func newSearchModel(target, method string, plan breakpoint.Plan, connect time.Duration, settings *Settings, stopper *Stopper) *searchModel {
	base := &model{target: target, stopper: stopper, runPanel: newRunPanel(plan.Settle), settings: settings}
	base.applySettings()
	base.tabs = []string{base.text.Summary(), base.text.Settings()}

	_, worst, _ := strings.Cut(PlanLine(plan, connect), "at most ")

	return &searchModel{base: base, method: method, plan: plan, worst: worst, now: time.Now, start: time.Now()}
}

// NewSearchProgram builds the full-screen view of a search and the feed that
// drives it.
func NewSearchProgram(target, method string, plan breakpoint.Plan, connect time.Duration, settings *Settings,
	stopper *Stopper,
) (*tea.Program, *SearchFeed) {
	m := newSearchModel(target, method, plan, connect, settings, stopper)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(os.Stderr))

	return p, NewSearchFeed(func(msg any) { p.Send(msg) })
}

// SearchFeed carries a search's progress to its screen: the Observer for
// SearchWith, and Starting, which the RunStep calls with each run's engine
// before it runs — never inside a run.
type SearchFeed struct {
	send func(any)
	mu   sync.Mutex
	run  breakpoint.Run
}

// NewSearchFeed sends the screen's messages through send (tea.Program.Send).
func NewSearchFeed(send func(any)) *SearchFeed {
	return &SearchFeed{send: send}
}

// Observer is what SearchWith tells.
func (f *SearchFeed) Observer() breakpoint.Observer {
	return breakpoint.Observer{
		Started: func(r breakpoint.Run) {
			f.mu.Lock()
			f.run = r
			f.mu.Unlock()
		},
		Finished: func(s breakpoint.Step) { f.send(searchStepMsg{step: s}) },
		Cooldown: func(d time.Duration, rps int) { f.send(searchCooldownMsg{d: d, rps: rps}) },
	}
}

// Starting hands the screen the engine of the run about to start.
func (f *SearchFeed) Starting(eng *engine.Engine) {
	f.mu.Lock()
	r := f.run
	f.mu.Unlock()
	f.send(searchRunMsg{eng: eng, run: r})
}

// Done hands the screen the search's result.
func (f *SearchFeed) Done(res breakpoint.Result) {
	f.send(searchDoneMsg{res: res})
}

func (m *searchModel) Init() tea.Cmd { return tick() }

func (m *searchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case searchRunMsg:
		// A fresh panel per run: nothing of the last run is drawn on this one.
		panel := newRunPanel(m.plan.Settle)
		panel.text, panel.styles, panel.width = m.base.text, m.base.styles, m.base.width
		m.base.runPanel = panel
		m.base.engine = msg.eng
		m.run, m.running = msg.run, true
		m.coolUntil, m.coolRPS = time.Time{}, 0

		return m, nil

	case searchStepMsg:
		m.rows = append(m.rows, msg.step)
		m.running = false

		return m, nil

	case searchCooldownMsg:
		m.coolUntil, m.coolRPS = m.now().Add(msg.d), msg.rps

		return m, nil

	case searchDoneMsg:
		res := msg.res
		m.result = &res
		m.base.done = true

		return m, nil

	case doneMsg:
		// A stop asked to leave once the search ended.
		if m.base.stopper.Stopping() {
			return m, tea.Quit
		}

		return m, nil

	case tickMsg:
		m.base.frame++
		if m.running && m.base.engine != nil && !m.base.done {
			m.base.refresh(m.base.engine)
		}

		return m, tick()

	case tea.WindowSizeMsg:
		m.base.width, m.base.height = msg.Width, msg.Height

		return m, nil

	case tea.KeyMsg:
		_, cmd := m.base.onKey(msg)

		return m, cmd
	}

	return m, nil
}

func (m *searchModel) View() string {
	b := m.base
	if b.width > 0 && b.width < minWidth {
		return truncate(b.text.TooNarrow(minWidth), b.width)
	}

	width := b.viewWidth()
	frame := b.styles.frame.Width(width - 4)
	if b.height > 6 {
		frame = frame.Height(b.height - 4)
	}

	return frame.Render(m.body(contentWidth(width)))
}

func (m *searchModel) body(inner int) string {
	b := m.base

	var out strings.Builder
	out.WriteString(m.header(inner))
	out.WriteString("\n\n")

	if m.result != nil {
		var report bytes.Buffer
		PrintBreakpoint(&report, BreakpointRun{Method: m.method, Plan: m.plan, Result: *m.result})
		out.WriteString(strings.TrimRight(report.String(), "\n"))
		out.WriteString("\n\n")
		out.WriteString(b.footer())

		return out.String()
	}

	var top strings.Builder
	switch {
	case !m.coolUntil.IsZero():
		left := max(0, m.coolUntil.Sub(m.now())).Round(time.Second)
		top.WriteString(b.styles.value.Render(fmt.Sprintf("cooldown %s before the repeat of %d rps", formatDuration(left), m.coolRPS)))
	case b.showHelp, b.active == b.settingsTab():
		top.WriteString(b.content(inner))
	case b.engine == nil:
		top.WriteString(b.styles.muted.Render(ellipsis))
	default:
		top.WriteString(b.summary(inner))
	}

	used := strings.Count(out.String(), "\n") + strings.Count(top.String(), "\n") + 1
	out.WriteString(top.String())
	out.WriteString("\n\n")

	room := 0
	if b.height > 0 {
		// The frame, the blank lines around the table, the table heading and
		// the footer.
		room = b.height - frameHeight - used - 2 - 1 - 2
	}
	out.WriteString(m.table(room))
	out.WriteString("\n\n")
	out.WriteString(b.footer())

	return out.String()
}

// header is the status line and the search's place: which run of which
// step, at what rate, and the time against the plan line's worst case.
func (m *searchModel) header(width int) string {
	b := m.base
	status, glyph := b.text.Running(), spinner(b.frame/2)
	switch {
	case m.result != nil:
		status, glyph = b.text.Finished(), "●"
	case b.stopper.Stopping():
		status = "stopping: the search ends with this run"
	}

	target := b.target
	if target == FakeTarget {
		target = b.text.FakeTarget()
	}
	line := newHeaderLine(b.styles, width, b.styles.shimmer(glyph+" "+status, b.frame))
	line.add(b.styles.warn, b.stopAction())
	line.add(b.styles.value, m.method)
	line.add(b.styles.muted, target)

	clock := formatDuration(m.now().Sub(m.start)) + " of at most " + m.worst
	place := m.place()
	gap := max(1, width-lipgloss.Width(place)-lipgloss.Width(clock))
	if lipgloss.Width(place)+lipgloss.Width(clock)+1 > width {
		return line.text + "\n" + truncate(place, width) + "\n" + truncate(clock, width)
	}

	return line.text + "\n" + b.styles.value.Render(place) + strings.Repeat(" ", gap) + b.styles.muted.Render(clock)
}

// place says where the search is without promising what may not come.
func (m *searchModel) place() string {
	r := m.run
	switch {
	case m.result != nil || r.RPS == 0:
		return "breaking-point search"
	case r.Kind == breakpoint.Repeat:
		return fmt.Sprintf("repeat of step %d, %d rps", r.Step, r.RPS)
	case r.Kind == breakpoint.Probe:
		return fmt.Sprintf("probe %d of at most %d, %d rps", r.Probe, breakpoint.MaxProbes, r.RPS)
	}

	return fmt.Sprintf("step %d of at most %d, %d rps", r.Step, r.Steps, r.RPS)
}

// table is the runs so far in at most room lines below the heading (no
// limit at 0): the newest kept, the oldest counted.
func (m *searchModel) table(room int) string {
	rows := m.rows
	hidden := 0
	if room > 0 && len(rows) > room {
		keep := max(room-1, 1)
		hidden = len(rows) - keep
		rows = rows[hidden:]
	}
	verdicts := make([]string, len(rows))
	for i := range rows {
		verdicts[i] = stepVerdict(&rows[i])
	}

	table := strings.TrimRight(runsTable(m.plan, rows, verdicts, ""), "\n")
	if hidden == 0 {
		return table
	}
	head, rest, _ := strings.Cut(table, "\n")

	return head + "\n" + m.base.styles.muted.Render(fmt.Sprintf("%s %d earlier runs", ellipsis, hidden)) + "\n" + rest
}
