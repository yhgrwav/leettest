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

package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rampSearch is a breakpoint section from 100 to 160 rps whose hold is a second
// and a half: the first step's second 2 (after a ramp of 2s) is a whole second
// at the rate, every step has a whole first second to count, and the first
// measures 145 calls, over the 100 a p99 needs.
const rampSearch = `  breakpoint:
    from: 100
    to: 160
    settle: 50ms
    hold: 1500ms
`

const needsSearchRampText = "app.mode: real needs load.ramp (a ramp from 0 to breakpoint.from), e.g. load.ramp: 30s"

// searchRunsOf runs the search of the config on the fake target and returns the
// decoded document and its runs (at least two: -fake never breaks, so each of
// the steps 100, 125 and 156 is held).
func searchRunsOf(t *testing.T, cfg string) (doc map[string]any, runs []any) {
	t.Helper()

	res := runCLI(t.Context(), t, 90*time.Second, "-fake", "-output", "json", "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\n%s", res.err, res.stderr)
	}
	doc = decodeOnly(t, res.stdout)
	runs, _ = jsonAt(t, doc, "breakpoint", "runs").([]any)
	if len(runs) < 2 {
		t.Fatalf("the search has %d runs, want 2 or more (the fake target never breaks)", len(runs))
	}

	return doc, runs
}

// begunBySecond is the calls that began in each second of the run, the seconds
// of the warm-up included, and whether each second is a warm-up one.
func begunBySecond(t *testing.T, run any) (begun []float64, warmup []bool) {
	t.Helper()

	methods, _ := jsonAt(t, run, "report", "methods").([]any)
	if len(methods) == 0 {
		t.Fatalf("the run has no methods")
	}
	seconds, _ := jsonAt(t, methods[0], "seconds").([]any)
	for _, s := range seconds {
		begun = append(begun, fieldNumber(t, s, "begun"))
		w, _ := jsonAt(t, s, "warmup").(bool)
		warmup = append(warmup, w)
	}

	return begun, warmup
}

// runNotes is report.notes of a run of the search's JSON.
func runNotes(t *testing.T, run any) []string {
	t.Helper()

	raw, _ := jsonAt(t, run, "report", "notes").([]any)
	notes := make([]string, 0, len(raw))
	for _, n := range raw {
		s, _ := n.(string)
		notes = append(notes, s)
	}

	return notes
}

// noteBetween is the first note that starts with prefix and ends with suffix.
func noteBetween(notes []string, prefix, suffix string) (string, bool) {
	for _, n := range notes {
		if strings.HasPrefix(n, prefix) && strings.HasSuffix(n, suffix) {
			return n, true
		}
	}

	return "", false
}

// Ground: contract — the whole path of the ramp of a search: config to JSON. A
// search from 100 rps with a ramp of 2s rises from 0 to 100 in the first step's
// excluded window and starts its steps there. The first step only: its first
// second holds the ramp's first half (25 or so calls, not 100), its second the
// second half (75 or so), and its third second the rate; the next step is a
// constant stage and counts its rate from the first second. The window the
// statistics leave out is ramp and settle (2.05s) for the first step and the
// settle (50ms) for the others, and the ramp is in the settings of the first
// step and of the search, never of the later steps.
//
// The counts are judged by margins (the ramp's first second under half of
// from, the rate's second at least 0.8 of it), not exact numbers: the clock of
// the machine is not ours.
func TestRun_ASearchStartsWithItsRamp(t *testing.T) {
	cfg := writeSearchOf(t, closedPort(t), rampSearch, "200ms", checkMethod)
	setRamp(t, cfg, "2s")

	doc, runs := searchRunsOf(t, cfg)

	if got := fieldNumber(t, doc, "settings", "ramp_us"); got != 2_000_000 {
		t.Errorf("settings.ramp_us = %v, want 2000000: the search's ramp", got)
	}

	begun, warmup := begunBySecond(t, runs[0])
	if len(begun) < 3 {
		t.Fatalf("runs[0] has %d seconds %v, want 3 or more (2s of ramp and the hold)", len(begun), begun)
	}
	if begun[0] >= 50 {
		t.Errorf("runs[0] began %v calls in its first second, want under 50 (half of from): the ramp rises from 0", begun[0])
	}
	if begun[1] <= begun[0] {
		t.Errorf("runs[0] began %v calls in its first second and %v in its second, want a rise through the ramp", begun[0], begun[1])
	}
	if begun[2] < 80 {
		t.Errorf("runs[0] began %v calls in its third second, want 80 or more (0.8 of from): the ramp is over", begun[2])
	}
	if !warmup[0] || !warmup[1] {
		t.Errorf("runs[0] seconds 0 and 1 are warm-up: %v, want both true: the ramp is outside the statistics", warmup)
	}

	for key, want := range map[string]float64{"settings.ramp_us": 2_000_000, "warmup_us": 2_050_000} {
		path := append([]string{"report"}, strings.Split(key, ".")...)
		if got := fieldNumber(t, runs[0], path...); got != want {
			t.Errorf("runs[0].report.%s = %v, want %v (the ramp, and the ramp with the settle)", key, got, want)
		}
	}

	// The step's report shows the ramp as a plain run's warm-up line does: one
	// note, naming the ramp with the warm-up (the window is the ramp and the
	// settle, longer than the ramp), and no word of a ramp on the later steps.
	if _, ok := noteBetween(runNotes(t, runs[0]), "ramp and warm-up ", ", excluded from stats"); !ok {
		t.Errorf("runs[0].report.notes has no %q ... %q line: %q", "ramp and warm-up ", ", excluded from stats", runNotes(t, runs[0]))
	}

	for i := 1; i < len(runs); i++ {
		notes := runNotes(t, runs[i])
		if _, ok := noteBetween(notes, "warm-up ", ", excluded from stats"); !ok {
			t.Errorf("runs[%d].report.notes has no %q ... %q line: %q", i, "warm-up ", ", excluded from stats", notes)
		}
		for _, n := range notes {
			if strings.HasPrefix(n, "ramp") {
				t.Errorf("runs[%d].report.notes names a ramp: %q, want none: only the first step ramps", i, n)
			}
		}
		if got := fieldNumber(t, runs[i], "report", "settings", "ramp_us"); got != 0 {
			t.Errorf("runs[%d].report.settings.ramp_us = %v, want 0: only the first step ramps", i, got)
		}
		if got := fieldNumber(t, runs[i], "report", "warmup_us"); got != 50_000 {
			t.Errorf("runs[%d].report.warmup_us = %v, want 50000: the settle alone", i, got)
		}
	}

	rps := fieldNumber(t, runs[1], "planned_rps")
	next, _ := begunBySecond(t, runs[1])
	if len(next) == 0 || next[0] < 0.8*rps {
		t.Errorf("runs[1] began %v calls in its first second, want %v or more (0.8 of its %v rps): no ramp", next, 0.8*rps, rps)
	}
}

// Ground: contract — without load.ramp a search is today's, in local too: the
// first step starts at its rate (its first second holds 0.8 of from or more),
// every ramp in the JSON is 0 and present, the window the statistics leave out
// is the settle alone. This pins «byte for byte» in spirit: the ramp stage is
// not built, and the key is there for the reader.
func TestRun_ASearchWithoutARampIsTodays(t *testing.T) {
	for _, ramp := range []string{"", "0s"} {
		cfg := writeSearchOf(t, closedPort(t), rampSearch, "200ms", checkMethod)
		if ramp != "" {
			setRamp(t, cfg, ramp)
		}

		doc, runs := searchRunsOf(t, cfg)

		if got := fieldNumber(t, doc, "settings", "ramp_us"); got != 0 {
			t.Errorf("ramp %q: settings.ramp_us = %v, want 0 (and present)", ramp, got)
		}
		begun, _ := begunBySecond(t, runs[0])
		if len(begun) == 0 || begun[0] < 80 {
			t.Errorf("ramp %q: runs[0] began %v calls in its first second, want 80 or more (0.8 of from)", ramp, begun)
		}
		for i := range runs {
			if got := fieldNumber(t, runs[i], "report", "settings", "ramp_us"); got != 0 {
				t.Errorf("ramp %q: runs[%d].report.settings.ramp_us = %v, want 0", ramp, i, got)
			}
			if got := fieldNumber(t, runs[i], "report", "warmup_us"); got != 50_000 {
				t.Errorf("ramp %q: runs[%d].report.warmup_us = %v, want 50000: the settle alone", ramp, i, got)
			}
		}
	}
}

// Ground: contract — the text report says the ramp of a search on its settings
// line (the line proposed in internal/cli) and, with no ramp, the release's
// line: a text report and a JSON that disagree about the ramp would be a lie
// of one of them.
func TestRun_ASearchTextNamesTheRampOfItsConfig(t *testing.T) {
	cfg := writeSearchOf(t, closedPort(t), rampSearch, "200ms", checkMethod)
	setRamp(t, cfg, "2s")

	text, _ := echoRun(t, 90*time.Second, "-c", cfg)

	want := "settings: settle 50ms, hold 1.5s, first step after a 2s ramp, max in flight per step, connections 1\n"
	if !strings.Contains(text, want) {
		t.Errorf("the search's text lacks %q:\n%s", want, text)
	}
}

// Ground: contract — a search whose first step ramps shows that ramp by second
// in its text, as a plain run does: a failure on the way to `from` is visible
// there and not only in the JSON. One block, directly after the runs table,
// headed like the plain run's («ramp by second, <method>:», or «ramp and
// warm-up by second» when a settle follows the ramp, as it does here) and ended
// by its footnote. The 2s ramp and the 50ms settle give two whole seconds,
// the least a block has.
func TestRun_ARampedSearchPrintsItsRampBySecond(t *testing.T) {
	cfg := writeSearchOf(t, closedPort(t), rampSearch, "200ms", checkMethod)
	setRamp(t, cfg, "2s")

	text, _ := echoRun(t, 90*time.Second, "-output", "text", "-c", cfg)

	table := strings.Index(text, "  kind")
	if table < 0 {
		t.Fatalf("no runs table in the search's text:\n%s", text)
	}

	// Nothing after the table (today's text, when the search has no notes) is no block.
	var after string
	if end := strings.Index(text[table:], "\n\n"); end >= 0 {
		after = text[table+end+2:]
	}

	if !regexp.MustCompile(`^ramp( and warm-up)? by second, \S+:\n`).MatchString(after) {
		t.Errorf("the runs table is not followed by a ramp block:\n%s", after)
	}
	if n := strings.Count(text, "by second"); n != 1 {
		t.Errorf("%d blocks by second in the search's text, want 1 (only the first step ramps):\n%s", n, text)
	}
	if !strings.Contains(after, "calls still in flight when the run ended are not counted as failed") {
		t.Errorf("the block lacks its footnote:\n%s", after)
	}
}

// Ground: contract — a search in real without a ramp is a config error before
// anything is asked or sent: exit 1, the text for the search (it names
// breakpoint.from), not the plain run's. The question of the confirmation is not
// reached, and the target hears nothing.
func TestRun_ARealSearchWithoutARampIsRefused(t *testing.T) {
	for _, ramp := range []string{"", "0s"} {
		addr, seen := countingTarget(t)
		cfg := writeSearch(t, addr, checkMethod)
		setMode(t, cfg, "real")
		if ramp != "" {
			setRamp(t, cfg, ramp)
		}
		withTerminals(t, true, true, strings.NewReader("y\n"))

		res := runCLI(t.Context(), t, 10*time.Second, "-plain", "-c", cfg)

		wantExit(t, res, 1)
		if all := fmt.Sprint(res.err) + res.stderr; !strings.Contains(all, needsSearchRampText) {
			t.Errorf("ramp %q: no %q in:\n%s", ramp, needsSearchRampText, all)
		}
		if strings.Contains(res.stderr, askText) {
			t.Errorf("ramp %q: a refused config reached the question:\n%s", ramp, res.stderr)
		}
		if res.stdout != "" {
			t.Errorf("ramp %q: stdout is not empty, a refused config writes no report:\n%s", ramp, res.stdout)
		}
		if n := seen(); n != 0 {
			t.Errorf("ramp %q: the target saw %d connections from a config that was refused", ramp, n)
		}
	}
}

// Ground: contract — a first step the cap was too low for never ran, and still
// says what it was tried with, the ramp included: runs[0] is the step that
// would have ramped, and its settings say so (the existing test of a refused
// step has no ramp). Reading «runs[0].report.settings.ramp_us = the ramp»
// literally; a step after the first that was refused would say 0.
func TestRun_ARefusedFirstStepOfARampedSearchEchoesItsRamp(t *testing.T) {
	cfg := writeSearchOf(t, closedPort(t), rampSearch, "200ms", checkMethod)
	setRamp(t, cfg, "2s")

	_, js := echoRun(t, 60*time.Second, "-max-in-flight", "1", "-c", cfg)

	runs := js["breakpoint"].(map[string]any)["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("the search reports no runs")
	}
	if why := runs[0].(map[string]any)["why"]; why != "in_flight_cap" {
		t.Fatalf("runs[0].why = %v, want in_flight_cap: the step did not run", why)
	}
	if got := fieldNumber(t, runs[0], "report", "settings", "ramp_us"); got != 2_000_000 {
		t.Errorf("runs[0].report.settings.ramp_us = %v, want 2000000 for the step that never ran", got)
	}
}

// Ground: contract — a search with a ramp is checked against its first run as
// that run will be: the ramp's calls use records of a dataset_reuse: false
// file too. A file that covers the hold of the first step (120 requests at 400
// rps for 300ms) and not the ramp before it is a config error, before anything
// is sent, and the message says the ramp is in the count, as a plain run's does.
// The count is the engine's own (the 2s ramp from 0 to 400 hands out about 400),
// so it is judged by margins.
func TestRun_OnceSearchCountsTheRampInItsFirstRun(t *testing.T) {
	cfg := writeSearchWith(t, closedPort(t), quickSearch, "200ms",
		"      dataset: '"+writeDataset(t, lines(120))+"'\n      dataset_reuse: false\n", unaryCallMethod)
	setRamp(t, cfg, "2s")

	res := runCLI(t.Context(), t, 20*time.Second, "-fake", "-c", cfg)

	if exitCode(res.err) != 1 {
		t.Fatalf("exit code %d, want 1\nerr %v\nstderr %s", exitCode(res.err), res.err, res.stderr)
	}
	all := fmt.Sprint(res.err) + res.stderr
	m := regexp.MustCompile(`has 120 records, the run hands out (\d+) \(ramp and warm-up included\)`).FindStringSubmatch(all)
	if m == nil {
		t.Fatalf("the error does not give 120 records and the ramp in the count:\n%s", all)
	}
	if need, _ := strconv.Atoi(m[1]); need < 480 || need > 560 {
		t.Errorf("the first run hands out %d, want 480 to 560: the ramp's 400 and the hold's 120", need)
	}
	if strings.Contains(res.stderr, "rps: sent") {
		t.Errorf("a run was made before the error:\n%s", res.stderr)
	}
}

// Ground: contract — the ramp is before the first step only, and the first
// step's repeat after a cooldown is a run like the others: the same rate, held
// from its first second, no ramp in its settings and its warm-up the settle
// alone. The target here fails every call, so the first rate breaks and is
// repeated (outcome broke_at_first); a runner that tells the first step by its
// rate would ramp the repeat too. The delay is 20ms: with 1ms the clock step of
// a Windows host passes a quarter of the median and the repeat is an invalid run.
func TestRun_TheRepeatOfARampedFirstStepHasNoRamp(t *testing.T) {
	cfg := writeSearchOf(t, closedPort(t), rampSearch, "200ms", checkMethod)
	setRamp(t, cfg, "2s")

	res := runCLI(t.Context(), t, 90*time.Second, "-fake", "-fake-delay", "20ms", "-fake-fail-ratio", "1", "-output", "json", "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\n%s", res.err, res.stderr)
	}
	doc := decodeOnly(t, res.stdout)

	if got := jsonAt(t, doc, "breakpoint", "outcome"); got != "broke_at_first" {
		t.Fatalf("outcome = %v, want broke_at_first: the premise is a first rate that breaks and is repeated", got)
	}
	runs, _ := jsonAt(t, doc, "breakpoint", "runs").([]any)
	if len(runs) < 2 {
		t.Fatalf("the search has %d runs, want 2 (the step and its repeat)", len(runs))
	}

	if got := jsonAt(t, runs[1], "kind"); got != "repeat" {
		t.Fatalf("runs[1].kind = %v, want repeat", got)
	}
	rps := fieldNumber(t, runs[0], "planned_rps")
	if got := fieldNumber(t, runs[1], "planned_rps"); got != rps {
		t.Fatalf("runs[1].planned_rps = %v, want %v: the repeat is of the first rate", got, rps)
	}
	if got := fieldNumber(t, runs[0], "report", "settings", "ramp_us"); got != 2_000_000 {
		t.Errorf("runs[0].report.settings.ramp_us = %v, want 2000000: the first step ramps", got)
	}
	if got := fieldNumber(t, runs[1], "report", "settings", "ramp_us"); got != 0 {
		t.Errorf("runs[1].report.settings.ramp_us = %v, want 0: the repeat has no second ramp", got)
	}
	if got := fieldNumber(t, runs[1], "report", "warmup_us"); got != 50_000 {
		t.Errorf("runs[1].report.warmup_us = %v, want 50000: the settle alone", got)
	}
	begun, _ := begunBySecond(t, runs[1])
	if len(begun) == 0 || begun[0] < 0.8*rps {
		t.Errorf("runs[1] began %v calls in its first second, want %v or more (0.8 of its %v rps): no ramp", begun, 0.8*rps, rps)
	}
}

// planLineSeconds is the seconds the plan line of the search's stderr promises
// ("at most 1m13s").
func planLineSeconds(t *testing.T, stderr string) time.Duration {
	t.Helper()

	m := regexp.MustCompile(`breakpoint: up to \d+ steps, at most (\S+)`).FindStringSubmatch(stderr)
	if m == nil {
		t.Fatalf("stderr has no plan line:\n%s", stderr)
	}
	d, err := time.ParseDuration(m[1])
	if err != nil {
		t.Fatalf("the plan line's %q is not a duration: %v", m[1], err)
	}

	return d
}

// Ground: contract — "at most" stays an upper bound with a ramp: the plan line
// of a search with `ramp: 2s` promises at least 2s more than the same search
// without it (the ramp is added once, in front of the first step).
func TestRun_ThePlanLineOfARampedSearchCountsTheRamp(t *testing.T) {
	plain := runCLI(t.Context(), t, 60*time.Second, "-fake", "-c", writeSearch(t, closedPort(t), checkMethod))
	if plain.err != nil {
		t.Fatalf("run without a ramp: %v\n%s", plain.err, plain.stderr)
	}
	cfg := writeSearch(t, closedPort(t), checkMethod)
	setRamp(t, cfg, "2s")
	ramped := runCLI(t.Context(), t, 60*time.Second, "-fake", "-c", cfg)
	if ramped.err != nil {
		t.Fatalf("run with a ramp: %v\n%s", ramped.err, ramped.stderr)
	}

	without, with := planLineSeconds(t, plain.stderr), planLineSeconds(t, ramped.stderr)
	if with < without+2*time.Second {
		t.Errorf("the plan line says at most %v with a ramp of 2s and %v without: the promise is %v short of the run", with, without,
			without+2*time.Second-with)
	}
}
