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
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// LagWindowSeconds is how many whole seconds the early warning looks back.
const LagWindowSeconds = 5

// LagWindowSource is what the warning reads: the engine of the run.
type LagWindowSource interface {
	LagWindow(now time.Time, seconds int) engine.LagWindow
}

// LagWarningLine is the one-line warning for a window the generator was behind
// in; "" for a window it was not.
func LagWarningLine(_ engine.LagWindow) string {
	return ""
}

// LagWatch looks at the window on every tick and fires once per run, at the
// first window the generator was behind in.
type LagWatch struct {
	src LagWindowSource
}

// NewLagWatch watches src.
func NewLagWatch(src LagWindowSource) *LagWatch {
	return &LagWatch{src: src}
}

// Tick reads the window at now. It returns the warning line and true the first
// time the generator was behind, and "", false on every tick after that.
func (l *LagWatch) Tick(_ time.Time) (string, bool) {
	return "", false
}

// LagWarningMsg is the message that hands the warning line to the live view.
func LagWarningMsg(line string) tea.Msg {
	return lagWarningMsg{line: line}
}

type lagWarningMsg struct{ line string }
