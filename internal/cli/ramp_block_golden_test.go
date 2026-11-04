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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// blockText is the lines of the block under header, the header and the
// footnote included, exactly as printed.
func blockText(t *testing.T, run RunReport, header string) []string {
	t.Helper()

	lines := echoLines(run)
	at := slices.Index(lines, header)
	if at < 0 {
		t.Fatalf("no line %q in:\n%s", header, strings.Join(lines, "\n"))
	}

	end := slices.Index(lines[at:], rampFootnote)
	if end < 0 {
		t.Fatalf("the block under %q has no footnote:\n%s", header, strings.Join(lines, "\n"))
	}

	return lines[at : at+end+1]
}

// Ground: contract — the block is a table: the label left-aligned, every
// number right-aligned under its name. Tests that split a line into fields
// cannot see a broken alignment, this one compares the text.
func TestReport_TheRampBlockIsAlignedWordForWord(t *testing.T) {
	short := secondsOf(4, func(i int) engine.Second {
		return []engine.Second{
			blockSecond(100, 98, 0),
			blockSecond(300, 301, 0),
			blockSecond(500, 500, 150),
			blockSecond(700, 640, 0),
		}[i]
	})
	long := secondsOf(61, func(i int) engine.Second {
		rate := float64(i+1) * 1000 / 61
		failed := 0
		if i >= 50 {
			failed = 30 * (i - 49)
		}

		return blockSecond(rate, int(rate), failed)
	})

	for _, tc := range []struct {
		name string
		run  RunReport
		want []string
	}{
		{"4 s", blockRun(4*time.Second, 0, short...), []string{
			"ramp by second, a.B/One:",
			"at    planned rps  sent  failed",
			"0-1s          100    98      0%",
			"1-2s          300   301      0%",
			"2-3s          500   500     30%",
			"3-4s          700   640      0%",
			rampFootnote,
		}},
		{"61 s", blockRun(61*time.Second, 0, long...), []string{
			"ramp by second, a.B/One:",
			"at      planned rps  sent  failed",
			"0-4s             41   162      0%",
			"4-8s            107   424      0%",
			"8-12s           172   686      0%",
			"12-16s          238   949      0%",
			"16-20s          303  1211      0%",
			"20-24s          369  1474      0%",
			"24-28s          434  1736      0%",
			"28-32s          500  1998      0%",
			"32-36s          566  2260      0%",
			"36-40s          631  2522      0%",
			"40-44s          697  2785      0%",
			"44-48s          762  3047      0%",
			"48-52s          828  3310      2%",
			"52-56s          893  3572     15%",
			"56-60s          959  3834     26%",
			"60-61s         1000  1000     33%",
			rampFootnote,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := blockText(t, tc.run, tc.want[0])
			if !slices.Equal(got, tc.want) {
				t.Errorf("block:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}
