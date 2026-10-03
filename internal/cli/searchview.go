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
// search is, the current run's panel, and the runs so far.
type searchModel struct {
	base  *model
	now   func() time.Time
	start time.Time
}

func newSearchModel(target, method string, plan breakpoint.Plan, connect time.Duration, settings *Settings, stopper *Stopper) *searchModel {
	base := &model{target: target, stopper: stopper, runPanel: newRunPanel(plan.Settle), settings: settings}
	base.applySettings()

	return &searchModel{base: base, now: time.Now}
}

// SearchFeed carries a search's progress to its screen: the Observer for
// SearchWith, and Starting, which the RunStep calls with each run's engine
// before it runs.
type SearchFeed struct {
	send func(any)
}

// NewSearchFeed sends the screen's messages through send (tea.Program.Send).
func NewSearchFeed(send func(any)) *SearchFeed {
	return &SearchFeed{send: send}
}

// Observer is what SearchWith tells.
func (f *SearchFeed) Observer() breakpoint.Observer {
	return breakpoint.Observer{}
}

// Starting hands the screen the engine of the run about to start.
func (f *SearchFeed) Starting(_ *engine.Engine) {}

func (m *searchModel) Init() tea.Cmd { return nil }

func (m *searchModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *searchModel) View() string { return "" }
