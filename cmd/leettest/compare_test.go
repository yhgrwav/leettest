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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/share"
)

// reportSpec is a breakpoint search to write as a JSON report, with only the
// fields leettest compare reads varying.
type reportSpec struct {
	outcome     breakpoint.Outcome
	held, broke int
	// first is the rate of the first step, runs[0].planned_rps.
	first                   int
	name                    string
	startedAt               time.Time
	target, method, version string
	// passport nil: the report has none.
	passport *cli.Passport
	// datasetSHA, when set, is the dataset hash of the searched method.
	datasetSHA string
}

const (
	cmpMethod  = "pkg.Svc/Do"
	cmpTarget  = "10.0.0.1:50051"
	cmpVersion = "v0.3.0"
)

// cmpPassport is a passport that does not depend on the machine the test runs on.
func cmpPassport() *cli.Passport {
	quota := 4.0
	p := cli.NewPassport(strings.Repeat("a", 64), nil, &quota)
	p.OS, p.Arch, p.NumCPU, p.GOMAXPROCS, p.GoVersion = "linux", "amd64", 8, 8, "go1.27.1"

	return p
}

// brokeAt is a search that held `held` rps and broke above it.
func brokeAt(held int) reportSpec {
	return reportSpec{
		outcome: breakpoint.BrokeBetween, held: held, broke: held + held/4, first: 100, name: "svc",
		startedAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
		target:    cmpTarget, method: cmpMethod, version: cmpVersion, passport: cmpPassport(),
	}
}

// brokeAtFirst is a search that broke at its first step, 100 rps.
func brokeAtFirst() reportSpec { return brokeAtFirstOf(100) }

// brokeAtFirstOf is a search that broke at its first step, which ran at first rps.
func brokeAtFirstOf(first int) reportSpec {
	s := brokeAt(0)
	s.outcome, s.first, s.broke = breakpoint.BrokeAtFirst, first, first

	return s
}

// endedAt is a search of the outcome that held `held` rps and never broke.
func endedAt(outcome breakpoint.Outcome, held int) reportSpec {
	s := brokeAt(held)
	s.outcome, s.broke = outcome, 0

	return s
}

func (s reportSpec) run() cli.BreakpointRun {
	step := func(rps int, broken bool) breakpoint.Step {
		var dataset *engine.DatasetReport
		if s.datasetSHA != "" {
			dataset = &engine.DatasetReport{File: "data.jsonl", SHA256: s.datasetSHA, Records: 10}
		}

		return breakpoint.Step{
			RPS: rps, Broken: broken,
			Report: engine.Report{Sent: rps * 4, Methods: []engine.MethodReport{{Method: s.method, Sent: rps * 4, Dataset: dataset}}},
		}
	}

	res := breakpoint.Result{Outcome: s.outcome, Held: s.held, Broke: s.broke, Steps: []breakpoint.Step{step(s.first, s.outcome == breakpoint.BrokeAtFirst)}}
	if s.outcome == breakpoint.BrokeBetween {
		res.Steps = append(res.Steps, step(s.broke, true))
		res.Cause = breakpoint.CauseErrors
	}

	return cli.BreakpointRun{
		Target: s.target, Version: s.version, Method: s.method, Name: s.name, StartedAt: s.startedAt,
		Plan:   breakpoint.Plan{From: s.first, To: 100000, Settle: time.Second, Hold: 5 * time.Second, Timeout: 500 * time.Millisecond},
		Result: res, Passport: s.passport,
	}
}

// write puts the spec's report, as the CLI writes it, in dir/name.
func (s reportSpec) write(t *testing.T, dir, name string) string {
	t.Helper()

	return writeReportFile(t, dir, name, cli.NewBreakpointReport(s.run()))
}

func writeReportFile(t *testing.T, dir, name string, v any) string {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}

// compared runs leettest compare on the two specs, with flags before the
// files; it returns the result and the two paths.
func compared(t *testing.T, base, nw reportSpec, flags ...string) (res result, basePath, newPath string) {
	t.Helper()

	dir := t.TempDir()
	basePath, newPath = base.write(t, dir, "base.json"), nw.write(t, dir, "new.json")

	return runCLI(t.Context(), t, 10*time.Second, append(append([]string{"compare"}, flags...), basePath, newPath)...), basePath, newPath
}

// lastLine is the last line of out.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")

	return lines[len(lines)-1]
}

// lineStarting is the one line of out that starts with prefix.
func lineStarting(t *testing.T, out, prefix string) string {
	t.Helper()

	var found []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one line starting %q, got %d in\n%s", prefix, len(found), out)
	}

	return found[0]
}

// Ground: contract — a fall past the limit is a fail: exit 4, and the report
// is exactly the two header lines and the capacity line.
func TestCompare_ADropPastTheLimitExits4(t *testing.T) {
	base, nw := brokeAt(1000), brokeAt(800)
	base.passport.Labels = map[string]string{"region": "eu", "env": "prod"}
	nw.passport.Labels = map[string]string{"env": "prod"}
	nw.startedAt = time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)

	res, basePath, newPath := compared(t, base, nw, "-max-capacity-drop", "10%")

	wantExit(t, res, 4)
	want := "base: " + basePath + "  svc  2026-10-01T10:00:00Z  labels: env=prod, region=eu\n" +
		"new:  " + newPath + "  svc  2026-10-02T10:00:00Z  labels: env=prod\n" +
		"capacity: 1000 rps -> 800 rps, drop 20.0%, limit 10%: FAIL\n"
	if res.stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", res.stdout, want)
	}
}

// Ground: contract — a fall within the limit passes: exit 0, the capacity
// line says pass and the fall as it is.
func TestCompare_WithinTheLimitExits0(t *testing.T) {
	res, _, _ := compared(t, brokeAt(1000), brokeAt(950), "-max-capacity-drop", "10%")

	wantExit(t, res, 0)
	if got, want := lineStarting(t, res.stdout, "capacity:"), "capacity: 1000 rps -> 950 rps, drop 5.0%, limit 10%: pass"; got != want {
		t.Errorf("capacity line %q, want %q", got, want)
	}
}

// Ground: contract — with no limit the fall is printed and nothing is judged,
// even a fall of half: exit 0 and the last line says so. A report without a
// name prints "-", without labels no labels part.
func TestCompare_NoLimitJudgesNothing(t *testing.T) {
	base, nw := brokeAt(1000), brokeAt(500)
	nw.name, nw.startedAt = "", time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)

	res, basePath, newPath := compared(t, base, nw)

	wantExit(t, res, 0)
	want := "base: " + basePath + "  svc  2026-10-01T10:00:00Z\n" +
		"new:  " + newPath + "  -  2026-10-02T10:00:00Z\n" +
		"capacity: 1000 rps -> 500 rps, drop 50.0%\n" +
		"no limit given: nothing judged (-max-capacity-drop)\n"
	if res.stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", res.stdout, want)
	}
}

// Ground: contract — a rise is printed as a rise, never as a drop; no change
// says so.
func TestCompare_ARiseAndNoChangeAreNotDrops(t *testing.T) {
	res, _, _ := compared(t, brokeAt(1000), brokeAt(1200), "-max-capacity-drop", "10%")

	wantExit(t, res, 0)
	if got, want := lineStarting(t, res.stdout, "capacity:"), "capacity: 1000 rps -> 1200 rps, rise 20.0%, limit 10%: pass"; got != want {
		t.Errorf("capacity line %q, want %q", got, want)
	}

	res, _, _ = compared(t, brokeAt(1000), brokeAt(1000), "-max-capacity-drop", "10%")

	wantExit(t, res, 0)
	if got := lineStarting(t, res.stdout, "capacity:"); !strings.Contains(got, "no change") || strings.Contains(got, "drop") || strings.Contains(got, "rise") {
		t.Errorf("capacity line %q, want it to say no change", got)
	}
}

// Ground: contract — the capacity of each outcome is printed by what it is:
// exact, below the first step, a lower bound, not measured; the verdict
// follows the bounds, the fall carries `>=` / `<=` when it is a bound, and
// `-` when it cannot be worked out.
func TestCompare_CapacityIsPrintedAndJudgedByItsBound(t *testing.T) {
	for _, tc := range []struct {
		name     string
		base, nw reportSpec
		line     string
		reason   string
		exit     int
	}{
		{"new is a lower bound within the limit", brokeAt(1000), endedAt(breakpoint.HeldThroughout, 950),
			"capacity: 1000 rps -> >= 950 rps, drop <= 5.0%, limit 10%: pass", "", 0},
		{"new is a lower bound past the limit", brokeAt(1000), endedAt(breakpoint.HeldThroughout, 800),
			"capacity: 1000 rps -> >= 800 rps, drop <= 20.0%, limit 10%: unknown", "new run ended before its capacity (held_all)", 2},
		{"base is a lower bound, the fall past the limit", endedAt(breakpoint.HeldThroughout, 1000), brokeAt(800),
			"capacity: >= 1000 rps -> 800 rps, drop >= 20.0%, limit 10%: FAIL", "", 4},
		{"base is a lower bound, the fall within the limit", endedAt(breakpoint.HeldThroughout, 1000), brokeAt(950),
			"capacity: >= 1000 rps -> 950 rps, drop >= 5.0%, limit 10%: unknown", "base capacity is only a lower bound (held_all)", 2},
		{"two lower bounds", endedAt(breakpoint.HeldThroughout, 1000), endedAt(breakpoint.HeldThroughout, 900),
			"capacity: >= 1000 rps -> >= 900 rps, drop -, limit 10%: unknown", "both capacities are lower bounds", 2},
		{"base is a lower bound, new the same", endedAt(breakpoint.HeldThroughout, 1000), brokeAt(1000),
			"capacity: >= 1000 rps -> 1000 rps, drop >= 0.0%, limit 10%: unknown", "base capacity is only a lower bound (held_all)", 2},
		{"base is a lower bound, new above", endedAt(breakpoint.HeldThroughout, 1000), brokeAt(1200),
			"capacity: >= 1000 rps -> 1200 rps, rise <= 20.0%, limit 10%: unknown", "base capacity is only a lower bound (held_all)", 2},
		{"new is a lower bound above the base", brokeAt(1000), endedAt(breakpoint.HeldThroughout, 1100),
			"capacity: 1000 rps -> >= 1100 rps, rise >= 10.0%, limit 10%: pass", "", 0},
		{"new is a lower bound the same as the base", brokeAt(1000), endedAt(breakpoint.HeldThroughout, 1000),
			"capacity: 1000 rps -> >= 1000 rps, rise >= 0.0%, limit 10%: pass", "", 0},
		{"a stopped search is a lower bound", brokeAt(400), endedAt(breakpoint.Stopped, 300),
			"capacity: 400 rps -> >= 300 rps, drop <= 25.0%, limit 10%: unknown", "new run ended before its capacity (stopped)", 2},
		{"a run limit with a held rate is a lower bound", brokeAt(400), endedAt(breakpoint.RunLimit, 380),
			"capacity: 400 rps -> >= 380 rps, drop <= 5.0%, limit 10%: pass", "", 0},
		{"base broke at the first step", brokeAtFirst(), brokeAt(800),
			"capacity: below 100 rps -> 800 rps, drop -, limit 10%: unknown", "base broke at the first step: lower the plan to see a drop", 2},
		{"new broke at the first step, far below the base", brokeAt(1000), brokeAtFirst(),
			"capacity: 1000 rps -> below 100 rps, drop >= 90.0%, limit 10%: FAIL", "", 4},
		{"new broke at the first step, close to the base", brokeAt(1000), brokeAtFirstOf(950),
			"capacity: 1000 rps -> below 950 rps, drop >= 5.0%, limit 10%: unknown", "new broke at the first step: lower the plan to see the drop", 2},
		{"base is invalid", endedAt(breakpoint.Invalid, 244), brokeAt(800),
			"capacity: not measured (invalid) -> 800 rps, drop -, limit 10%: unknown", "base not measured (invalid)", 2},
		{"new is invalid", brokeAt(1000), endedAt(breakpoint.Invalid, 244),
			"capacity: 1000 rps -> not measured (invalid), drop -, limit 10%: unknown", "new not measured (invalid)", 2},
		{"a run limit without a held rate is not measured", brokeAt(1000), func() reportSpec { s := endedAt(breakpoint.RunLimit, 0); s.broke = 100; return s }(),
			"capacity: 1000 rps -> not measured (run_limit), drop -, limit 10%: unknown", "new not measured (run_limit)", 2},
	} {
		res, _, _ := compared(t, tc.base, tc.nw, "-max-capacity-drop", "10%")

		wantExit(t, res, tc.exit)
		got := lineStarting(t, res.stdout, "capacity:")
		if tc.reason == "" && got != tc.line || tc.reason != "" && (!strings.HasPrefix(got, tc.line) || !strings.Contains(got, tc.reason)) {
			t.Errorf("%s:\n  got  %q\n  want %q (reason %q)", tc.name, got, tc.line, tc.reason)
		}
	}
}

// Ground: contract — a printed bound never claims more than is known: an
// upper bound of the fall rounds up, a lower bound rounds down (a third of 3
// rps is 33.33%), so neither can be read as better or worse than the data.
func TestCompare_ABoundOfTheFallIsRoundedTowardWhatIsKnown(t *testing.T) {
	res, _, _ := compared(t, brokeAt(3), endedAt(breakpoint.HeldThroughout, 2))

	wantExit(t, res, 0)
	if got, want := lineStarting(t, res.stdout, "capacity:"), "capacity: 3 rps -> >= 2 rps, drop <= 33.4%"; got != want {
		t.Errorf("capacity line %q, want %q", got, want)
	}

	res, _, _ = compared(t, endedAt(breakpoint.HeldThroughout, 3), brokeAt(2))

	wantExit(t, res, 0)
	if got, want := lineStarting(t, res.stdout, "capacity:"), "capacity: >= 3 rps -> 2 rps, drop >= 33.3%"; got != want {
		t.Errorf("capacity line %q, want %q", got, want)
	}
}

// Ground: contract — each field that makes two reports incomparable is named
// on one `differs:` line with both values, and the check is undecidable
// (exit 2) with a limit or without one: never a fail, though the fall is half,
// and never a pass.
func TestCompare_EachPassportFieldMakesItUndecidable(t *testing.T) {
	quota := 2.0

	for _, tc := range []struct {
		field string
		edit  func(*reportSpec)
		line  string
	}{
		{"target", func(s *reportSpec) { s.target = "10.0.0.2:50051" }, "differs: target: 10.0.0.1:50051 -> 10.0.0.2:50051"},
		{"method", func(s *reportSpec) { s.method = "pkg.Svc/Other" }, "differs: method: pkg.Svc/Do -> pkg.Svc/Other"},
		{"leettest_version", func(s *reportSpec) { s.version = "v0.3.1" }, "differs: leettest_version: v0.3.0 -> v0.3.1"},
		{"passport.config_sha256", func(s *reportSpec) { s.passport.ConfigSHA256 = strings.Repeat("b", 64) },
			"differs: passport.config_sha256: " + strings.Repeat("a", 64) + " -> " + strings.Repeat("b", 64) +
				" (config bytes differ: a comment or line endings count too)"},
		{"passport.os", func(s *reportSpec) { s.passport.OS = "darwin" }, "differs: passport.os: linux -> darwin"},
		{"passport.arch", func(s *reportSpec) { s.passport.Arch = "arm64" }, "differs: passport.arch: amd64 -> arm64"},
		{"passport.num_cpu", func(s *reportSpec) { s.passport.NumCPU = 4 }, "differs: passport.num_cpu: 8 -> 4"},
		{"passport.gomaxprocs", func(s *reportSpec) { s.passport.GOMAXPROCS = 4 }, "differs: passport.gomaxprocs: 8 -> 4"},
		{"passport.cpu_quota", func(s *reportSpec) { s.passport.CPUQuota = &quota }, "differs: passport.cpu_quota: 4 -> 2"},
		{"passport.cpu_quota to none", func(s *reportSpec) { s.passport.CPUQuota = nil }, "differs: passport.cpu_quota: 4 -> none"},
		{"passport.go_version", func(s *reportSpec) { s.passport.GoVersion = "go1.26.9" }, "differs: passport.go_version: go1.27.1 -> go1.26.9"},
		{"dataset", func(s *reportSpec) { s.datasetSHA = strings.Repeat("d", 64) },
			"differs: dataset: " + strings.Repeat("c", 64) + " -> " + strings.Repeat("d", 64)},
		{"dataset missing on the new side", func(s *reportSpec) { s.datasetSHA = "" },
			"differs: dataset: " + strings.Repeat("c", 64) + " -> none"},
	} {
		for _, flags := range [][]string{nil, {"-max-capacity-drop", "10%"}} {
			base, nw := brokeAt(1000), brokeAt(500)
			base.datasetSHA, nw.datasetSHA = strings.Repeat("c", 64), strings.Repeat("c", 64)
			tc.edit(&nw)

			res, _, _ := compared(t, base, nw, flags...)

			wantExit(t, res, 2)
			if got := lineStarting(t, res.stdout, "differs:"); got != tc.line {
				t.Errorf("%s %v: differs line %q, want %q", tc.field, flags, got, tc.line)
			}
			if len(flags) > 0 {
				capacity := lineStarting(t, res.stdout, "capacity:")
				if !strings.Contains(capacity, "unknown") || !strings.Contains(capacity, "reports are not comparable") || strings.Contains(capacity, "FAIL") {
					t.Errorf("%s: capacity line %q, want unknown, reports are not comparable", tc.field, capacity)
				}
			} else if got, want := lastLine(res.stdout), "reports are not comparable: nothing judged"; got != want {
				t.Errorf("%s: last line %q, want %q", tc.field, got, want)
			}
		}
	}
}

// Ground: contract — the dataset hash is compared with a side that has none:
// a dataset on the new side only differs too.
func TestCompare_ADatasetOnTheNewSideOnlyDiffers(t *testing.T) {
	base, nw := brokeAt(1000), brokeAt(950)
	nw.datasetSHA = strings.Repeat("d", 64)

	res, _, _ := compared(t, base, nw)

	wantExit(t, res, 2)
	if got, want := lineStarting(t, res.stdout, "differs:"), "differs: dataset: none -> "+strings.Repeat("d", 64); got != want {
		t.Errorf("differs line %q, want %q", got, want)
	}
}

// Ground: contract — the dataset hash is compared on both sides: two
// different hashes differ, none on both sides is equal (every other test),
// and the same hash on both is equal.
func TestCompare_TheSameDatasetIsComparable(t *testing.T) {
	base, nw := brokeAt(1000), brokeAt(950)
	base.datasetSHA, nw.datasetSHA = strings.Repeat("c", 64), strings.Repeat("c", 64)

	res, _, _ := compared(t, base, nw, "-max-capacity-drop", "10%")

	wantExit(t, res, 0)
	if strings.Contains(res.stdout, "differs:") {
		t.Errorf("stdout names a difference between equal datasets:\n%s", res.stdout)
	}
	if got, want := lineStarting(t, res.stdout, "capacity:"), "capacity: 1000 rps -> 950 rps, drop 5.0%, limit 10%: pass"; got != want {
		t.Errorf("capacity line %q, want %q", got, want)
	}
}

// Ground: contract — labels are what differs by design: printed, never
// compared. The inverse is the labels on the header lines: a run that merely
// printed nothing would pass the exit code.
func TestCompare_LabelsAreNotCompared(t *testing.T) {
	base, nw := brokeAt(1000), brokeAt(950)
	base.passport.Labels = map[string]string{"env": "prod", "build": "41"}
	nw.passport.Labels = map[string]string{"env": "staging"}

	res, _, _ := compared(t, base, nw, "-max-capacity-drop", "10%")

	wantExit(t, res, 0)
	if strings.Contains(res.stdout, "differs:") {
		t.Errorf("stdout names a difference, labels are not compared:\n%s", res.stdout)
	}
	if got := lineStarting(t, res.stdout, "base:"); !strings.HasSuffix(got, "labels: build=41, env=prod") {
		t.Errorf("base line %q, want the labels printed", got)
	}
	if got := lineStarting(t, res.stdout, "new:"); !strings.HasSuffix(got, "labels: env=staging") {
		t.Errorf("new line %q, want the labels printed", got)
	}
	if got := lineStarting(t, res.stdout, "capacity:"); !strings.HasSuffix(got, "limit 10%: pass") {
		t.Errorf("capacity line %q, want a verdict of pass", got)
	}
}

// Ground: contract — a report without a passport (before v0.3) cannot be
// shown to be comparable: undecidable, naming the file, with a limit or
// without one.
func TestCompare_NoPassportIsUndecidable(t *testing.T) {
	for _, tc := range []struct {
		name             string
		noBase, noNew    bool
		wantBase, wantNw bool
	}{
		{"base only", true, false, true, false},
		{"new only", false, true, false, true},
		{"both", true, true, true, true},
	} {
		for _, flags := range [][]string{nil, {"-max-capacity-drop", "10%"}} {
			base, nw := brokeAt(1000), brokeAt(500)
			if tc.noBase {
				base.passport = nil
			}
			if tc.noNew {
				nw.passport = nil
			}

			res, basePath, newPath := compared(t, base, nw, flags...)

			wantExit(t, res, 2)
			const tail = ": no passport (report from leettest before v0.3)"
			if got := strings.Contains(res.stdout, basePath+tail); got != tc.wantBase {
				t.Errorf("%s %v: line for the base = %v, want %v\n%s", tc.name, flags, got, tc.wantBase, res.stdout)
			}
			if got := strings.Contains(res.stdout, newPath+tail); got != tc.wantNw {
				t.Errorf("%s %v: line for the new = %v, want %v\n%s", tc.name, flags, got, tc.wantNw, res.stdout)
			}
			if strings.Contains(res.stdout, "FAIL") {
				t.Errorf("%s %v: a report without a passport failed a check:\n%s", tc.name, flags, res.stdout)
			}
			if len(flags) == 0 {
				if got, want := lastLine(res.stdout), "reports are not comparable: nothing judged"; got != want {
					t.Errorf("%s: last line %q, want %q", tc.name, got, want)
				}
			}
		}
	}
}

// Ground: contract — no comparison can be made when the command is misused or
// a file is not a report this leettest reads: exit 1, a message naming what is
// wrong, nothing on stdout.
func TestCompare_InputErrorsExit1(t *testing.T) {
	dir := t.TempDir()
	good := brokeAt(1000).write(t, dir, "good.json")
	other := brokeAt(900).write(t, dir, "other.json")
	notJSON := filepath.Join(dir, "notjson.json")
	if err := os.WriteFile(notJSON, []byte("this is not a report\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	schema2 := cli.NewBreakpointReport(brokeAt(1000).run())
	schema2.SchemaVersion = 2
	badSchema := writeReportFile(t, dir, "schema2.json", schema2)

	plain := writeReportFile(t, dir, "plain.json", cli.NewJSONReport(cli.JSONRun{
		Target: cmpTarget, Name: "svc", Version: cmpVersion, Outcome: cli.OutcomeComplete, Run: cli.RunReport{},
	}))

	missing := filepath.Join(dir, "missing.json")
	_, badPercent := share.Parse("10", "-max-capacity-drop")
	if badPercent == nil {
		t.Fatal("share.Parse accepted 10")
	}

	for _, tc := range []struct {
		name string
		args []string
		// exact is the whole message; otherwise each of has is in it.
		exact string
		has   []string
	}{
		{"no files", nil, "compare: want two report files, base then new", nil},
		{"one file", []string{good}, "compare: want two report files, base then new", nil},
		{"three files", []string{good, other, good}, "compare: want two report files, base then new", nil},
		{"a missing file", []string{good, missing}, "", []string{missing}},
		{"a file that is not JSON", []string{notJSON, good}, "", []string{notJSON}},
		{"schema 2", []string{good, badSchema}, "compare: " + badSchema + ": schema_version 2, this leettest reads 1", nil},
		{"a plain run as base", []string{plain, good}, "compare: " + plain + " is a plain run; compare reads two breakpoint search reports for now", nil},
		{"a plain run as new", []string{good, plain}, "compare: " + plain + " is a plain run; compare reads two breakpoint search reports for now", nil},
		{"a percent without a sign", []string{"-max-capacity-drop", "10", good, other}, "", []string{badPercent.Error()}},
		{"a percent over 100", []string{"-max-capacity-drop", "101%", good, other}, "", []string{"-max-capacity-drop", "over 100%"}},
		{"an unknown flag", []string{"-max-capacity-fall", "10%", good, other}, "", []string{"max-capacity-fall"}},
	} {
		res := runCLI(t.Context(), t, 10*time.Second, append([]string{"compare"}, tc.args...)...)

		wantExit(t, res, 1)
		if res.stdout != "" {
			t.Errorf("%s: stdout %q, want nothing", tc.name, res.stdout)
		}
		msg := res.err.Error()
		if tc.exact != "" && msg != tc.exact {
			t.Errorf("%s: error %q, want %q", tc.name, msg, tc.exact)
		}
		for _, has := range tc.has {
			if !strings.Contains(msg+res.stderr, has) {
				t.Errorf("%s: error %q (stderr %q), want it to say %q", tc.name, msg, res.stderr, has)
			}
		}
	}
}

// Ground: contract — a field this leettest does not know is ignored, so a
// later leettest that adds one to schema 1 does not break the comparison: the
// report reads and is judged as without it.
func TestCompare_AFieldItDoesNotKnowIsIgnored(t *testing.T) {
	dir := t.TempDir()

	raw, err := json.Marshal(cli.NewBreakpointReport(brokeAt(1000).run()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fields["added_in_a_later_leettest"] = map[string]any{"anything": []int{1, 2, 3}}
	base := writeReportFile(t, dir, "base.json", fields)
	nw := brokeAt(950).write(t, dir, "new.json")

	res := runCLI(t.Context(), t, 10*time.Second, "compare", "-max-capacity-drop", "10%", base, nw)

	wantExit(t, res, 0)
	if got, want := lineStarting(t, res.stdout, "capacity:"), "capacity: 1000 rps -> 950 rps, drop 5.0%, limit 10%: pass"; got != want {
		t.Errorf("capacity line %q, want %q", got, want)
	}
}

// Ground: boundary — the fall is printed so that it never lands on the wrong
// side of the limit the verdict used: on the limit it prints the limit, one
// rps past it prints more than the limit, with the digits it takes.
func TestCompare_PrintedDropNeverArguesWithTheVerdict(t *testing.T) {
	res, _, _ := compared(t, brokeAt(1000), brokeAt(900), "-max-capacity-drop", "10%")

	wantExit(t, res, 0)
	if got, want := lineStarting(t, res.stdout, "capacity:"), "capacity: 1000 rps -> 900 rps, drop 10.0%, limit 10%: pass"; got != want {
		t.Errorf("capacity line %q, want %q", got, want)
	}

	res, _, _ = compared(t, brokeAt(100000), brokeAt(89999), "-max-capacity-drop", "10%")

	wantExit(t, res, 4)
	if got, want := lineStarting(t, res.stdout, "capacity:"), "capacity: 100000 rps -> 89999 rps, drop 10.1%, limit 10%: FAIL"; got != want {
		t.Errorf("capacity line %q, want %q", got, want)
	}
}

// Ground: contract — compare is dispatched only when it is the first
// argument: a run with -c is as before, even when the config file is called
// compare.
func TestCompare_ConfigFlagStillRuns(t *testing.T) {
	cfg := passportCfg(t)
	_, res := fakeRun(t, "-c", cfg)
	if res.stdout == "" {
		t.Errorf("a run with -c printed nothing, stderr: %s", res.stderr)
	}

	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	named := filepath.Join(t.TempDir(), "compare")
	if err := os.WriteFile(named, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, res = fakeRun(t, "-c", named); res.stdout == "" {
		t.Errorf("a run with -c compare printed nothing, stderr: %s", res.stderr)
	}
}
