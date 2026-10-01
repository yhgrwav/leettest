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
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Only characters a Windows console draws with its default font: the
	// braille spinner and the eighth-blocks came out as boxes there.
	spinnerFrames = []string{"|", "/", "-", `\`}
	sparkLevels   = []rune{0x2591, 0x2592, 0x2593, 0x2588} // light, medium, dark shade, full block
)

func spinner(frame int) string {
	return spinnerFrames[frame%len(spinnerFrames)]
}

func gauge(s styles, value, limit float64, width int) string {
	filled := 0
	if limit > 0 {
		filled = int(float64(width) * value / limit)
	}
	filled = min(max(filled, 0), width)

	return s.barOn.Render(strings.Repeat("█", filled)) +
		s.barOff.Render(strings.Repeat("░", width-filled))
}

func progress(s styles, elapsed, total time.Duration, width int) string {
	filled := 0
	if total > 0 {
		filled = int(float64(width) * float64(elapsed) / float64(total))
	}
	filled = min(max(filled, 0), width)

	bar := s.barOn.Render(strings.Repeat("━", filled))
	if filled < width {
		bar += s.barOff.Render(strings.Repeat("━", width-filled))
	}

	return bar
}

func sparkline(s styles, values []float64, width int) string {
	if len(values) == 0 {
		return s.faint.Render(strings.Repeat(".", width))
	}

	if len(values) > width {
		values = values[len(values)-width:]
	}

	// A NaN marks a tick with no measurement. It is drawn as a gap and kept out
	// of the scale: plotting it as zero would read as latency dropping to zero
	// exactly where nothing is known.
	low, high, known := 0.0, 0.0, false

	for _, v := range values {
		if math.IsNaN(v) {
			continue
		}
		if !known {
			low, high, known = v, v, true

			continue
		}

		low = min(low, v)
		high = max(high, v)
	}

	if !known {
		return s.faint.Render(strings.Repeat(".", width))
	}

	span := high - low
	flat := span < high/50 || span == 0

	var b strings.Builder

	for _, v := range values {
		if math.IsNaN(v) {
			b.WriteRune('.')

			continue
		}

		level := len(sparkLevels) / 2
		if !flat {
			level = int((v - low) / span * float64(len(sparkLevels)-1))
		}
		level = min(max(level, 0), len(sparkLevels)-1)
		b.WriteRune(sparkLevels[level])
	}

	line := s.spark.Render(b.String())

	if pad := width - len(values); pad > 0 {
		line = s.faint.Render(strings.Repeat(".", pad)) + line
	}

	return line
}

func sparkRange(s styles, values []float64, unit string, format func(float64) string) string {
	if len(values) == 0 {
		return ""
	}

	low, high := values[0], values[0]
	for _, v := range values {
		low = min(low, v)
		high = max(high, v)
	}

	return s.muted.Render(format(low) + unit + " - " + format(high) + unit)
}

func statLine(s styles, pairs ...[2]string) string {
	parts := make([]string, 0, len(pairs))

	for _, pair := range pairs {
		parts = append(parts, s.label.Render(pair[0]+" ")+s.value.Render(pair[1]))
	}

	return strings.Join(parts, s.faint.Render("  |  "))
}

func keyHint(s styles, hints ...string) string {
	parts := make([]string, 0, len(hints))

	for _, hint := range hints {
		parts = append(parts, s.muted.Render(hint))
	}

	return strings.Join(parts, s.faint.Render("   "))
}

// formatCount writes n exactly, digits grouped by three with commas.
func formatCount(n int) string {
	digits := strconv.Itoa(n)

	var b strings.Builder
	if n < 0 {
		b.WriteByte('-')
		digits = digits[1:]
	}

	for i := range len(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(digits[i])
	}

	return b.String()
}

// statField is one piece of a stat line. drop orders what goes when the line
// does not fit, lowest first, and 0 never goes. A value is never shortened:
// a count is exact or not on the line at all.
type statField struct {
	label, value string
	drop         int
}

// fitStatLine lays the fields out in width, without the droppable fields in
// their order until the line fits.
func fitStatLine(s styles, width int, fields ...statField) string {
	pairs := func(dropped int) [][2]string {
		out := make([][2]string, 0, len(fields))
		for _, f := range fields {
			if f.drop != 0 && f.drop <= dropped {
				continue
			}
			out = append(out, [2]string{f.label, f.value})
		}

		return out
	}

	for dropped := 0; ; dropped++ {
		line := statLine(s, pairs(dropped)...)
		if lipgloss.Width(line) <= width || dropped > len(fields) {
			return line
		}
	}
}

// countField is a count, exact. A negative count can only come from a bug,
// and is shown as it is.
func countField(label string, n, drop int) statField {
	if n < 0 {
		return statField{label: label, value: strconv.Itoa(n), drop: drop}
	}

	return statField{label: label, value: formatCount(n), drop: drop}
}
