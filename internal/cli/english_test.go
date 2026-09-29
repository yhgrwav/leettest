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
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// foreignMethod is a method name no ASCII console can be trusted to print.
const foreignMethod = "/пкг.Сервис/Метод"

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// liveGlyphs are the only non-ASCII characters the live view may draw: shades,
// full block, light and heavy horizontal, black circle, light vertical. Each
// was checked on a Windows console with its default font (2026-09-29); the
// rounded corners were not drawn right there and are not on the list.
var liveGlyphs = []rune{0x2591, 0x2592, 0x2593, 0x2588, 0x2500, 0x2501, 0x25cf, 0x2502}

// badRunes lists every character of screen outside ASCII and allowed, once
// each, with the first line it appeared on.
func badRunes(screen string, allowed []rune) []string {
	var bad []string
	seen := map[rune]bool{}

	for line := range strings.Lines(ansiEscape.ReplaceAllString(screen, "")) {
		for _, r := range line {
			if (r >= 0x20 && r < 0x7f) || r == '\n' || slices.Contains(allowed, r) || seen[r] {
				continue
			}
			seen[r] = true
			bad = append(bad, fmt.Sprintf("%q (U+%04X) in %q", r, r, strings.TrimSpace(line)))
		}
	}

	return bad
}

// escapesOf writes s as the report must: a backslash, u and four hex digits
// per character. Built, not typed, so no editor turns it back into letters.
func escapesOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		fmt.Fprintf(&b, "%cu%04x", '\\', r)
	}

	return b.String()
}

// reportScenarios are the text reports the ASCII rule is checked on: every
// kind of report the other tests build, plus foreign text from outside.
func reportScenarios() map[string]RunReport {
	foreign := engine.MethodReport{Method: foreignMethod, Sent: 10, Failed: 10, P50: exact(3)}

	return map[string]RunReport{
		"widest":     {Report: widestReport()},
		"categories": {Report: categoryReport()},
		"table":      {Report: tableReport()},
		"ledger":     {Report: ledgerShaped()},
		"stream":     {Report: oneStream()},
		"clock":      clockRun(502*time.Microsecond, time.Millisecond),
		"foreign": {
			Report:    engine.Report{Methods: []engine.MethodReport{foreign}},
			Unchecked: []Unchecked{{Method: foreignMethod, Err: errors.New("отказано: é")}},
		},
	}
}

// A: the screen speaks English only. A config written for another language
// still runs, and says once that the key is gone.
func TestEnglish_AnOldLangKeyWarnsOnce(t *testing.T) {
	for _, lang := range []string{"ru", "en", "zh"} {
		warnings := Settings{Lang: lang}.Deprecations()
		if len(warnings) != 1 || !strings.Contains(warnings[0], "lang") {
			t.Errorf("lang: %s gives %q, want one warning naming the key", lang, warnings)
		}
	}
	if warnings := (Settings{}).Deprecations(); len(warnings) != 0 {
		t.Errorf("no lang key gives %q, want nothing", warnings)
	}
}

func TestEnglish_ARussianSettingDrawsTheEnglishScreen(t *testing.T) {
	ru := testModel(t)
	ru.settings.Lang = string(LangRU)
	ru.applySettings()
	en := testModel(t)

	for tab := range en.tabs {
		ru.active, en.active = tab, tab
		ru.width, en.width = 120, 120
		ru.height, en.height = 40, 40
		if got, want := ansiEscape.ReplaceAllString(ru.View(), ""), ansiEscape.ReplaceAllString(en.View(), ""); got != want {
			t.Errorf("tab %d with lang: ru differs from English:\n%s", tab, got)
		}
	}
}

// B: counters are exact, grouped with commas; no k, M or G.
func TestEnglish_CountsAreExactWithCommas(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"}, {999, "999"}, {1_000, "1,000"}, {12_345, "12,345"},
		{999_950, "999,950"}, {1_234_567, "1,234,567"}, {-1_500, "-1,500"},
	}
	for _, c := range cases {
		if got := formatCount(c.n); got != c.want {
			t.Errorf("formatCount(%d) = %q, want %q", c.n, got, c.want)
		}
		if got := countCell(c.n); got != c.want {
			t.Errorf("countCell(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// C: the text report is ASCII whatever it prints, so a console on another
// code page shows it as written.
func TestEnglish_EveryTextReportIsASCII(t *testing.T) {
	for name, run := range reportScenarios() {
		var out bytes.Buffer
		PrintReport(&out, "localhost:50051", run)
		if bad := badRunes(out.String(), nil); len(bad) > 0 {
			t.Errorf("%s report prints non-ASCII:\n  %s", name, strings.Join(bad, "\n  "))
		}
	}
}

func TestEnglish_ForeignTextIsPrintedAsEscapes(t *testing.T) {
	var out bytes.Buffer
	PrintReport(&out, "localhost:50051", reportScenarios()["foreign"])

	if want := "/" + escapesOf("пкг") + "."; !strings.Contains(out.String(), want) {
		t.Errorf("report does not print the method as %s...:\n%s", want, out.String())
	}
}

func TestEnglish_JSONNotesCarryTheEscapedText(t *testing.T) {
	run := jsonRun(reportScenarios()["foreign"].Report)
	run.Run.Unchecked = reportScenarios()["foreign"].Unchecked
	notes, _ := writeJSON(t, run)["notes"].([]any)

	for _, n := range notes {
		if s, _ := n.(string); strings.Contains(s, escapesOf("пкг")) {
			return
		}
	}
	t.Errorf("no note carries the method as escapes: %q", notes)
}

// D: the live view draws only glyphs checked on cmd.exe, in every state.
func TestEnglish_TheLiveViewDrawsOnlyCheckedGlyphs(t *testing.T) {
	var bad []string
	collect := func(state string, m *model) {
		for tab := range m.tabs {
			m.active = tab
			for _, width := range []int{40, 80, 160} {
				m.width, m.height = width, 40
				for _, r := range badRunes(m.View(), liveGlyphs) {
					bad = append(bad, fmt.Sprintf("%s, tab %d, width %d: %s", state, tab, width, r))
				}
			}
		}
	}

	m := testModel(t)
	h := &history{}
	h.push(10, exact(10), exact(20), exact(95))
	h.push(12, exact(11), exact(22), exact(30))
	m.overall = *h
	for frame := range len(spinnerFrames) {
		m.frame = frame
		collect("running", m)
	}

	m.showHelp = true
	collect("help", m)
	m.showHelp = false

	m.snapshot.Methods = append(m.snapshot.Methods, engine.MethodSnapshot{Method: foreignMethod})
	collect("foreign method", m)

	press(m, "ы")
	collect("unknown key", m)

	m.stop()
	collect("stopping", m)

	m.report = widestReport()
	m.done = true
	collect("done", m)

	if len(bad) > 0 {
		t.Errorf("the live view draws unchecked characters:\n  %s", strings.Join(bad, "\n  "))
	}
}

func TestEnglish_TheSpinnerIsASCII(t *testing.T) {
	want := []string{"|", "/", "-", `\`}
	if !slices.Equal(spinnerFrames, want) {
		t.Errorf("spinner frames = %q, want %q", spinnerFrames, want)
	}
}

func TestEnglish_TheLatencyChartLabelsItsScaleMaximum(t *testing.T) {
	m := testModel(t)
	h := &history{}
	h.push(1, exact(10), exact(20), exact(95))
	h.push(1, exact(10), exact(20), exact(30))

	chart := ansiEscape.ReplaceAllString(m.latencyChart(h.points), "")
	if label := formatLatency(95 * time.Millisecond); !strings.Contains(chart, label) {
		t.Errorf("chart does not label its maximum %s: the cells alone have no scale\n%s", label, chart)
	}
}

// E: a key with nothing printable says nothing about the layout.
func TestEnglish_AKeyWithNoPrintableCharacterGivesNoLayoutHint(t *testing.T) {
	// Nothing, NUL, a zero-width space, ESC, a soft hyphen.
	for _, runes := range [][]rune{{}, {0}, {0x200b}, {0x1b}, {0xad}} {
		m := testModel(t)
		m.onKey(tea.KeyMsg(tea.Key{Type: tea.KeyRunes, Runes: runes}))
		if m.hintStage() != -1 {
			t.Errorf("runes %q start the layout hint %q", runes, m.hintKey)
		}
	}
}

func TestEnglish_APrintableUnknownKeyStillGetsTheHint(t *testing.T) {
	m := testModel(t)
	press(m, "Z")
	if m.hintStage() != 0 {
		t.Error("Z is no command and gave no hint")
	}
}
