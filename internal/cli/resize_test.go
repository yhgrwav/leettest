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

// A resize makes the terminal reflow the alternate screen, and bubbletea's
// renderer repaints only the lines it believes changed, so lines of the old
// frame stay. Its maintainers' advice for this (charmbracelet/bubbletea#573)
// is a clear-screen command on the size message: every screen that takes the
// size must return it, and only then, not on every tick.
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
