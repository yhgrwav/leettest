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

package stressplan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// TimingRepeats is how many times the timing job runs each test; the
// Makefile's stress-timing has the same -count.
const TimingRepeats = 20

// TimingMaxFails is the most failed repeats of one test, out of
// TimingRepeats, that the timing job still calls green: the worst flake of
// the search test was 8 of 120 (6.7 %) on a starved runner, and 6 of 20 is
// reached by chance 0.15 % of the time at that rate.
const TimingMaxFails = 5

// TimingName is the name of the timing job of the same branch, commit and Go
// as j: «timing <branch>@<sha7> go<ver>». It is not a name Name gives, so the
// count of green stress runs never reads it.
func (j Job) TimingName() string {
	return "timing " + strings.TrimPrefix(j.Name(), "stress ")
}

// TimingIssueTitle is the title of the timing issue for a branch; it is not
// the stress issue's, which the flaky-test triage reads.
func TimingIssueTitle(branch string) string { return "Timing tests red on " + branch }

var skipEntry = regexp.MustCompile(`^\^(\w+)\$$`)

// ParseSkipList reads the test names out of the STRESS_SKIP variable of a
// Makefile: «STRESS_SKIP := ^TestA$$|^TestB$$». An entry that is not a plain
// anchored name, or no variable at all, is an error: the timing job would
// run less than it should and say nothing.
func ParseSkipList(makefile []byte) ([]string, error) {
	for _, line := range strings.Split(string(makefile), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "STRESS_SKIP")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(rest, ":"), "="))
		rest = strings.ReplaceAll(rest, "$$", "$")
		if rest == "" {
			return nil, errors.New("STRESS_SKIP is empty")
		}
		var names []string
		for _, entry := range strings.Split(rest, "|") {
			m := skipEntry.FindStringSubmatch(entry)
			if m == nil {
				return nil, fmt.Errorf("STRESS_SKIP entry %q is not ^TestName$", entry)
			}
			names = append(names, m[1])
		}
		return names, nil
	}
	return nil, errors.New("the Makefile has no STRESS_SKIP")
}

// TimingRow is one test of the timing job: the repeats that ended in a pass
// or a fail, the fails among them, the skips.
type TimingRow struct {
	Name               string
	Runs, Fails, Skips int
}

// TimingReport is the timing job's table and why it is red.
type TimingReport struct {
	Rows []TimingRow
	Red  []string
}

// Green says the timing job passes.
func (r TimingReport) Green() bool { return len(r.Red) == 0 }

// Table is the report as a markdown table followed by the reasons.
func (r TimingReport) Table() string {
	var b strings.Builder
	b.WriteString("| test | runs | fails | skips |\n|---|---|---|---|\n")
	for _, row := range r.Rows {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", row.Name, row.Runs, row.Fails, row.Skips)
	}
	if r.Green() {
		fmt.Fprintf(&b, "\nGreen: every test ran %d times with at most %d fails.\n", TimingRepeats, TimingMaxFails)
		return b.String()
	}
	b.WriteString("\nRed:\n")
	for _, reason := range r.Red {
		fmt.Fprintf(&b, "- %s\n", reason)
	}
	return b.String()
}

type goEvent struct {
	Action, Package, Test, Output, ImportPath string
}

// Timing judges the output of `go test -json` (events) and the exit code of
// that go test for the tests in names. It is red when a test did not run
// exactly TimingRepeats times, ran any skipped, failed in more than
// TimingMaxFails repeats, when the run hit a panic, a timeout or a build
// failure, or when go test failed and no listed test failed to explain it.
// Lines that are not JSON are read as plain text, not skipped.
func Timing(names []string, events []byte, exit int) TimingReport {
	rows := make([]TimingRow, len(names))
	index := make(map[string]int, len(names))
	for i, n := range names {
		rows[i].Name = n
		index[n] = i
	}

	var panicked, built bool
	text := func(s string) {
		for _, l := range strings.Split(s, "\n") {
			l = strings.TrimSpace(l)
			panicked = panicked || strings.HasPrefix(l, "panic:")
			built = built || strings.Contains(l, "[build failed]") || strings.Contains(l, "[setup failed]")
		}
	}
	for _, line := range bytes.Split(events, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev goEvent
		if line[0] != '{' || json.Unmarshal(line, &ev) != nil {
			text(string(line))
			continue
		}
		text(ev.Output)
		if ev.Action == "build-fail" {
			built = true
		}
		i, listed := index[ev.Test]
		if !listed {
			continue
		}
		switch ev.Action {
		case "pass":
			rows[i].Runs++
		case "fail":
			rows[i].Runs++
			rows[i].Fails++
		case "skip":
			rows[i].Skips++
		}
	}

	r := TimingReport{Rows: rows}
	anyFailed := false
	for _, row := range rows {
		anyFailed = anyFailed || row.Fails > 0
		if row.Skips > 0 {
			r.Red = append(r.Red, fmt.Sprintf("%s skipped %d repeats: nothing was checked", row.Name, row.Skips))
		}
		if row.Runs != TimingRepeats {
			r.Red = append(r.Red, fmt.Sprintf("%s ran %d of %d repeats (a rename, a cut-short run?)", row.Name, row.Runs, TimingRepeats))
		}
		if row.Fails > TimingMaxFails {
			r.Red = append(r.Red, fmt.Sprintf("%s failed %d of %d repeats, more than %d", row.Name, row.Fails, row.Runs, TimingMaxFails))
		}
	}
	if panicked {
		r.Red = append(r.Red, "the run has a panic or a timeout")
	}
	if built {
		r.Red = append(r.Red, "a package did not build")
	}
	if exit != 0 && !anyFailed {
		r.Red = append(r.Red, fmt.Sprintf("go test exited %d and no listed test failed to explain it (exit code)", exit))
	}
	return r
}
