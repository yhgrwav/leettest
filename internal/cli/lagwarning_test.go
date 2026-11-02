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
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yhgrwav/leettest/pkg/engine"
)

const lagTail = "If this holds, the latency measures this machine, not the target, and the run may end invalid " +
	"(generator_late). Give the generator more CPU, lower the rate, or split the rate over several machines."

// lateWindow is the window of the line in the design: 2140 of 60000 calls
// over the floor in five seconds, at 12000 rps scheduled.
func lateWindow() engine.LagWindow {
	return engine.LagWindow{
		Seconds: 5, End: 3 * time.Second, Warmup: true, Floor: time.Millisecond,
		Calls: 60000, Starts: 60000, Late: 2140, Scheduled: 12000,
	}
}

// unsentWindow is a window the generator left 37 calls unsent in, 1:42 into
// the run.
func unsentWindow() engine.LagWindow {
	return engine.LagWindow{
		Seconds: 5, End: 102 * time.Second, Floor: time.Millisecond,
		Calls: 60000, Starts: 60000, NotSentGenerator: 37, Scheduled: 12000,
	}
}

// The text of the warning, word for word, for a window in the warm-up. The
// floor is written as the report writes a wait ("1.00ms"), not "1 ms".
//
// Ground: contract — the text users read and the docs quote.
func TestLagWarningLine_LateVariantInTheWarmup(t *testing.T) {
	want := "warning: the generator is behind its schedule: 2140 of 60000 calls in the last 5 s (warm-up) " +
		"started more than 1.00ms late, at 12000 rps scheduled. " + lagTail

	if got := LagWarningLine(lateWindow()); got != want {
		t.Errorf("line =\n%q\nwant\n%q", got, want)
	}
}

// Past the warm-up the window is named by the moment it ends, M:SS from the
// start.
//
// Ground: boundary — the minute turns over in the label.
func TestLagWarningLine_LateVariantPastTheWarmupNamesTheMoment(t *testing.T) {
	for _, c := range []struct {
		end  time.Duration
		want string
	}{
		{102 * time.Second, "(at 1:42)"},
		{9 * time.Second, "(at 0:09)"},
		{60 * time.Second, "(at 1:00)"},
	} {
		w := lateWindow()
		w.Warmup, w.End = false, c.end

		got := LagWarningLine(w)
		if !strings.Contains(got, "in the last 5 s "+c.want+" started more than") {
			t.Errorf("window ending at %v: line %q lacks %q", c.end, got, c.want)
		}
		if strings.Contains(got, "warm-up") {
			t.Errorf("window ending at %v past the warm-up names the warm-up: %q", c.end, got)
		}
	}
}

// The unsent variant: the count of calls the generator left unsent, the same
// tail.
//
// Ground: contract — the text users read; the rate clause follows the late
// variant's.
func TestLagWarningLine_UnsentVariant(t *testing.T) {
	want := "warning: the generator is behind its schedule: the generator left 37 calls unsent in the last 5 s " +
		"(at 1:42), at 12000 rps scheduled. " + lagTail

	if got := LagWarningLine(unsentWindow()); got != want {
		t.Errorf("line =\n%q\nwant\n%q", got, want)
	}

	w := unsentWindow()
	w.Warmup, w.End = true, 4*time.Second

	if got := LagWarningLine(w); !strings.Contains(got, "left 37 calls unsent in the last 5 s (warm-up), at 12000") {
		t.Errorf("unsent line in the warm-up = %q", got)
	}
}

// A window with late and unsent calls gets the unsent line: it is the one
// that ends a run as generator_late with no floor under it.
//
// Ground: contract — the choice the line makes when both hold.
func TestLagWarningLine_UnsentOutranksLate(t *testing.T) {
	w := lateWindow()
	w.NotSentGenerator = 5

	got := LagWarningLine(w)

	if !strings.Contains(got, "the generator left 5 calls unsent") || strings.Contains(got, "started more than") {
		t.Errorf("window with both late and unsent calls: line %q, want the unsent variant alone", got)
	}
}

// The floor in the line is the run's own, written as the report writes a wait:
// the default 1 ms, and 4 ms where a 1 ms clock step raised it.
//
// Ground: contract — one formatting function for the floor.
func TestLagWarningLine_FloorIsTheRunsFloor(t *testing.T) {
	for _, c := range []struct {
		floor time.Duration
		want  string
	}{
		{time.Millisecond, "started more than " + formatLatency(time.Millisecond) + " late"},
		{4 * time.Millisecond, "started more than " + formatLatency(4*time.Millisecond) + " late"},
		{4 * time.Millisecond, "started more than 4.00ms late"},
	} {
		w := lateWindow()
		w.Floor = c.floor

		if got := LagWarningLine(w); !strings.Contains(got, c.want) {
			t.Errorf("floor %v: line %q lacks %q", c.floor, got, c.want)
		}
	}
}

// Early in the run the window is shorter than five seconds, and the line says
// how many seconds it covers.
//
// Ground: boundary — a clipped window must not claim five seconds.
func TestLagWarningLine_AClippedWindowSaysItsSeconds(t *testing.T) {
	w := lateWindow()
	w.Seconds = 2

	if got := LagWarningLine(w); !strings.Contains(got, "calls in the last 2 s (warm-up)") {
		t.Errorf("clipped window: line %q lacks \"in the last 2 s\"", got)
	}
}

// A window the generator kept up in has no line, and the line is one line.
//
// Ground: contract — "" is no warning, and a line never wraps itself.
func TestLagWarningLine_NothingForAHealthyWindowAndOneLineOtherwise(t *testing.T) {
	healthy := lateWindow()
	healthy.Late = 600

	if got := LagWarningLine(healthy); got != "" {
		t.Errorf("line for a window at exactly 1%% late = %q, want none", got)
	}

	for _, w := range []engine.LagWindow{lateWindow(), unsentWindow()} {
		if got := LagWarningLine(w); got == "" || strings.Contains(got, "\n") {
			t.Errorf("line = %q, want one non-empty line", got)
		}
	}
}

// scriptedLag answers each read with the next window of its script, the last
// one for every read after, and records what it was asked.
type scriptedLag struct {
	windows []engine.LagWindow
	nows    []time.Time
	seconds []int
}

func (s *scriptedLag) LagWindow(now time.Time, seconds int) engine.LagWindow {
	i := len(s.nows)
	s.nows = append(s.nows, now)
	s.seconds = append(s.seconds, seconds)

	return s.windows[min(i, len(s.windows)-1)]
}

// The watch speaks once per run, on the first window the generator was behind
// in, however many ticks it is behind on after that.
//
// Ground: contract — one line per run.
func TestLagWatch_FiresOnceAtTheFirstWindowBehind(t *testing.T) {
	src := &scriptedLag{windows: []engine.LagWindow{{Seconds: 1}, lateWindow(), unsentWindow(), lateWindow()}}
	watch := NewLagWatch(src)
	now := time.Unix(1_000, 0)

	var lines []string

	for i := range 6 {
		if line, ok := watch.Tick(now.Add(time.Duration(i) * time.Second)); ok {
			lines = append(lines, line)
		}
	}

	if len(lines) != 1 {
		t.Fatalf("watch fired %d times over six ticks, want once: %q", len(lines), lines)
	}

	if want := LagWarningLine(lateWindow()); lines[0] != want || want == "" {
		t.Errorf("line = %q, want the line of the first window behind (%q)", lines[0], want)
	}
}

// A run the generator kept up in never gets a line.
//
// Ground: contract — silence on a clean run.
func TestLagWatch_StaysSilentOnACleanRun(t *testing.T) {
	clean := lateWindow()
	clean.Late = 0

	src := &scriptedLag{windows: []engine.LagWindow{{}, clean}}
	watch := NewLagWatch(src)
	now := time.Unix(1_000, 0)

	for i := range 10 {
		if line, ok := watch.Tick(now.Add(time.Duration(i) * time.Second)); ok || line != "" {
			t.Fatalf("tick %d fired on a clean run: %q", i, line)
		}
	}
}

// The watch reads the window at the tick's own moment and asks for five whole
// seconds.
//
// Ground: contract — the window length of the warning.
func TestLagWatch_ReadsFiveSecondsAtTheTick(t *testing.T) {
	src := &scriptedLag{windows: []engine.LagWindow{{}}}
	watch := NewLagWatch(src)
	now := time.Unix(1_000, 0)

	watch.Tick(now)
	watch.Tick(now.Add(time.Second))

	if len(src.nows) != 2 || !src.nows[0].Equal(now) || !src.nows[1].Equal(now.Add(time.Second)) {
		t.Errorf("source read at %v, want the two tick moments", src.nows)
	}

	for _, seconds := range src.seconds {
		if seconds != 5 || LagWindowSeconds != 5 {
			t.Errorf("window asked for %d seconds (LagWindowSeconds %d), want 5", seconds, LagWindowSeconds)
		}
	}
}

// In the live view the line sits in the view until the run ends, and later
// frames do not wipe it.
//
// Ground: contract — "in the view itself until the end of the run".
func TestModel_TheLagWarningStaysInTheLiveView(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})

	const fragment = "warning: the generator is behind its schedule"

	if strings.Contains(m.View(), fragment) {
		t.Fatalf("view shows the warning before it fired:\n%s", m.View())
	}

	m.Update(LagWarningMsg(fragment + ": 2140 of 60000 calls in the last 5 s (warm-up) started more than 1.00ms late."))

	if !strings.Contains(m.View(), fragment) {
		t.Errorf("view lacks the warning after it fired:\n%s", m.View())
	}

	for range 3 {
		m.Update(tickMsg(time.Now()))
	}

	if !strings.Contains(m.View(), fragment) {
		t.Errorf("view lost the warning after later frames:\n%s", m.View())
	}
}
