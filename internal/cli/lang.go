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
	"strconv"
)

// Text holds every string the interface shows. The screen is English only:
// other languages drew glyphs a Windows console could not show.
type Text struct{}

// NewText builds the interface text.
func NewText() Text {
	return Text{}
}

func (Text) Summary() string  { return "summary" }
func (Text) Running() string  { return "load test running" }
func (Text) Stopping() string { return "stopping" }
func (Text) Finished() string { return "load test finished" }
func (Text) Sent() string     { return "sent" }
func (Text) Errors() string   { return "errors" }
func (Text) InFlight() string { return "in flight" }

// InFlightCount is the header's count of calls still waiting for an answer;
// n is already formatted.
func (Text) InFlightCount(n string) string { return n + " in flight" }

// StopAgainAborts says what another press of the stop key does while the
// calls in flight drain.
func (Text) StopAgainAborts() string { return "q again cuts them off" }

// StopAgainExits is the way out once the abort is under way: it records the
// results and builds the report, and if that hangs only this leaves.
func (Text) StopAgainExits() string { return "q again exits without a report" }

func (Text) Target() string  { return "target" }
func (Text) Latency() string { return "latency" }

// Duration is how long the run went on, as a label for the run itself: the
// same number under "latency" would read as a measurement of the target.
func (Text) Duration() string { return "duration" }

func (Text) Rate() string     { return "rate" }
func (Text) HintTabs() string { return "<- -> tabs" }
func (Text) HintHelp() string { return "? help" }
func (Text) HintQuit() string { return "q quit" }

// UnknownKey is the hint for a key that is no command. It is short enough for
// the narrowest view, and points at the layout: a letter from another layout
// is the usual reason.
func (Text) UnknownKey(key string) string {
	return `"` + key + `" is not a key here - check the layout | ? help`
}

// FakeTarget names the built-in fake target in the header.
func (Text) FakeTarget() string { return "fake target (-fake)" }

func (Text) WarmupNote(left string, sent int) string {
	return "warming up (" + left + "): " + strconv.Itoa(sent) +
		" sent, these requests stay out of the percentiles, so p99 is still empty"
}

func (Text) InFlightNote() string {
	return "requests are piling up: the service answers slower than we send"
}

func (Text) ErrorsNote() string { return "the error share is growing - see the per-method tab" }

func (Text) HelpTitle() string { return "Keys" }

func (Text) HelpEscape() string {
	return "leave the settings, otherwise go back to the summary"
}

func (Text) HelpTabs() string     { return "switch tab" }
func (Text) HelpHelp() string     { return "show and hide this help" }
func (Text) HelpQuit() string     { return "stop the run and print the report" }
func (Text) HelpSettings() string { return "open the settings: mode, palette" }
func (Text) Settings() string     { return "settings" }
func (Text) ModeRow() string      { return "Mode" }
func (Text) PaletteRow() string   { return "Palette" }
func (Text) ModeDark() string     { return "dark" }
func (Text) ModeLight() string    { return "light" }

func (Text) SettingsHint() string {
	return "up/down row   <- -> value   esc back to the tabs   saved immediately"
}

func (Text) SettingsLocked() string { return "enter to edit   <- -> tabs   esc to the summary" }
func (Text) HintBack() string       { return "esc back" }
func (Text) ReportTitle() string    { return "Run finished" }
func (Text) ReportStopped() string  { return "Run stopped" }

func (Text) ReportStoppedNote() string {
	return "the run was cut short, so the numbers below describe only the part that ran"
}

func (Text) ColumnMethod() string      { return "method" }
func (Text) PressToExit() string       { return "enter to exit" }
func (Text) PickTheme() string         { return "Colour theme" }
func (Text) Saved(path string) string  { return "settings saved to " + path }
func (Text) PickHint() string          { return "up/down move   enter confirm" }
func (Text) ErrorsNoteShort() string   { return "error share growing - see method tab" }
func (Text) InFlightNoteShort() string { return "requests piling up: service slower than send rate" }
func (Text) NotSent() string           { return "not sent" }
func (Text) SentColumn() string        { return "sent" }
func (Text) TooShort() string          { return "terminal too small, the full report is printed after exit" }
func (Text) Failed() string            { return "failed" }
func (Text) MoreLines(n int) string {
	return fmt.Sprintf("%d more lines, the full report is printed after exit", n)
}
func (Text) TooNarrow(minimum int) string {
	return "window narrower than " + strconv.Itoa(minimum) + " columns - widen it"
}

// WarmupNoteShort is WarmupNote for a narrow frame. It leaves out the time
// left, which the header shows.
func (Text) WarmupNoteShort(sent int) string {
	return "warming up: " + strconv.Itoa(sent) + " sent, no p99"
}

// MoreMethods counts the methods a short terminal left out of the table.
func (Text) MoreMethods(n int) string {
	if n == 1 {
		return "1 more method"
	}

	return fmt.Sprintf("%d more methods", n)
}
