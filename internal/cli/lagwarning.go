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
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// LagWindowSeconds is how many whole seconds the early warning looks back.
const LagWindowSeconds = 5

// lagWarningTail is what the user does with the warning; it is the same for
// both variants.
const lagWarningTail = "If this holds, the latency measures this machine, not the target, and the run may end invalid " +
	"(generator_late). Give the generator more CPU, lower the rate, or split the rate over several machines."

// LagWindowSource is what the warning reads: the engine of the run.
type LagWindowSource interface {
	LagWindow(now time.Time, seconds int) engine.LagWindow
}

// LagWarningLine is the one-line warning for a window the generator was behind
// in; "" for a window it was not. A call left unsent by the generator is the
// line when there is one: it ends the run as generator_late with no floor under
// it.
func LagWarningLine(w engine.LagWindow) string {
	if !w.Behind() {
		return ""
	}

	span := fmt.Sprintf("in the last %d s %s", w.Seconds, lagWindowLabel(w))

	var detail string

	if w.NotSentGenerator > 0 {
		calls := "calls"
		if w.NotSentGenerator == 1 {
			calls = "call"
		}

		detail = fmt.Sprintf("the generator left %d %s unsent %s", w.NotSentGenerator, calls, span)
	} else {
		detail = fmt.Sprintf("%d of %d calls %s started more than %s late", w.Late, w.Starts, span, formatLatency(w.Floor))
	}

	if w.Scheduled > 0 {
		detail += fmt.Sprintf(", at %d rps scheduled", w.Scheduled)
	}

	return "warning: the generator is behind its schedule: " + detail + ". " + lagWarningTail
}

// lagWindowLabel names where the window is: the warm-up, or the moment it ends,
// M:SS from the start.
func lagWindowLabel(w engine.LagWindow) string {
	if w.Warmup {
		return "(warm-up)"
	}

	secs := int(w.End / time.Second)

	return fmt.Sprintf("(at %d:%02d)", secs/60, secs%60)
}

// LagWatch looks at the window on every tick and fires once per run, at the
// first window the generator was behind in.
type LagWatch struct {
	src   LagWindowSource
	fired bool
}

// NewLagWatch watches src.
func NewLagWatch(src LagWindowSource) *LagWatch {
	return &LagWatch{src: src}
}

// Tick reads the window at now. It returns the warning line and true the first
// time the generator was behind, and "", false on every tick after that; it
// reads the window on each, one cheap read a second. It is for one goroutine.
func (l *LagWatch) Tick(now time.Time) (string, bool) {
	line := LagWarningLine(l.src.LagWindow(now, LagWindowSeconds))
	if line == "" || l.fired {
		return "", false
	}

	l.fired = true

	return line, true
}

// LagWarningMsg is the message that hands the warning line to the live view.
func LagWarningMsg(line string) tea.Msg {
	return lagWarningMsg{line: line}
}

type lagWarningMsg struct{ line string }
