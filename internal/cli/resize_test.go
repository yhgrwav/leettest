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
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// After a resize the renderer rewrites every line of the frame but erases
// nothing outside it: rows below a frame that did not get shorter, and the
// rest of a full-width line, keep what the terminal left there. Clearing the
// screen on the size message (the workaround in charmbracelet/bubbletea#573)
// removes them; on a tick it would flicker. Every screen that takes the size
// must return it.
func TestView_AResizeRepaintsTheWholeScreen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update func(t *testing.T, msg tea.Msg) tea.Cmd
	}{
		{"the run view", func(t *testing.T, msg tea.Msg) tea.Cmd {
			_, cmd := testModel(t).Update(msg)

			return cmd
		}},
		{"the search view", func(t *testing.T, msg tea.Msg) tea.Cmd {
			_, cmd := testSearchModel(t).Update(msg)

			return cmd
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.update(t, tea.WindowSizeMsg{Width: 132, Height: 32})
			if cmd == nil {
				t.Fatal("a resize returns no command: the old frame's lines stay on the screen")
			}
			if got, want := cmd(), tea.ClearScreen(); got != want {
				t.Errorf("a resize returns %#v, want bubbletea's clear-screen message %#v", got, want)
			}
		})
	}
}

// The clear must not become part of every frame: on a tick the screen is
// redrawn by the renderer's own diff, or it would flicker.
func TestView_ATickDoesNotClearTheScreen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update func(t *testing.T) tea.Cmd
	}{
		{"the run view", func(t *testing.T) tea.Cmd {
			_, cmd := testModel(t).Update(tickMsg{})

			return cmd
		}},
		{"the search view", func(t *testing.T) tea.Cmd {
			_, cmd := testSearchModel(t).Update(tickMsg{})

			return cmd
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.update(t)
			if cmd == nil {
				t.Fatal("a tick returns no command: the screen would stop refreshing")
			}
			if got := cmd(); got == tea.ClearScreen() {
				t.Error("a tick clears the whole screen: it would flicker every frame")
			}
		})
	}
}
