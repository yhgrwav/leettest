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
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The timing job is a separate job of the stress run: its name is not a stress
// job's name, so the count of 8 green runs never reads it.
func TestTimingName_IsNotAPlannedStressJobAndIsNotParsed(t *testing.T) {
	for _, j := range plannedFor("main", "release/v0.3") {
		name := j.TimingName()
		if name == j.Name() {
			t.Errorf("TimingName %q equals the stress job's name", name)
		}
		for _, other := range plannedFor("main", "release/v0.3") {
			if name == other.Name() {
				t.Errorf("TimingName %q is the name of a planned stress job", name)
			}
		}
		if want := "timing " + j.Branch + "@" + sha[:7] + " go" + j.Go; name != want {
			t.Errorf("TimingName = %q, want %q", name, want)
		}
		if _, ok := ParseJobName(name); ok {
			t.Errorf("ParseJobName accepted %q", name)
		}
	}
}

// A red timing job leaves the count alone; a red stress job still resets it.
func TestVerdicts_ATimingJobDoesNotTouchTheCount(t *testing.T) {
	planned := plannedFor("main")
	stress := func(conclusion string) []Result {
		var rs []Result
		for _, j := range planned {
			rs = append(rs, Result{Name: j.Name(), Conclusion: conclusion})
		}
		return rs
	}
	timingFailed := func() []Result {
		var rs []Result
		for _, j := range planned {
			rs = append(rs, Result{Name: j.TimingName(), Conclusion: "failure"})
		}
		return rs
	}

	got := Verdicts([]string{"main"}, planned, append(stress("success"), timingFailed()...))
	if want := map[string]bool{"main": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("stress green, timing red: Verdicts = %v, want %v", got, want)
	}
	got = Verdicts([]string{"main"}, planned, append(stress("failure"), timingFailed()...))
	if want := map[string]bool{"main": false}; !reflect.DeepEqual(got, want) {
		t.Errorf("stress red: Verdicts = %v, want %v", got, want)
	}
}

func TestTimingIssueTitle_NamesTheBranchAndIsNotTheStressIssue(t *testing.T) {
	if got, want := TimingIssueTitle("release/v0.3"), "Timing tests red on release/v0.3"; got != want {
		t.Errorf("TimingIssueTitle = %q, want %q", got, want)
	}
	if TimingIssueTitle("main") == IssueTitle("main") {
		t.Error("the timing issue shares the stress issue's title: the flaky triage would read it")
	}
}

const makefileSkip = `
# a comment
STRESS_SKIP := ^TestOne$$|^TestTwo_Sub$$|^TestThree$$

stress:
	go test
`

func TestParseSkipList(t *testing.T) {
	got, err := ParseSkipList([]byte(makefileSkip))
	if err != nil {
		t.Fatalf("ParseSkipList: %v", err)
	}
	if want := []string{"TestOne", "TestTwo_Sub", "TestThree"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseSkipList = %q, want %q", got, want)
	}

	for name, bad := range map[string]string{
		"no variable":    "stress:\n\tgo test\n",
		"empty value":    "STRESS_SKIP :=\n",
		"a regexp":       "STRESS_SKIP := ^TestOne.*$$\n",
		"no anchors":     "STRESS_SKIP := TestOne|TestTwo\n",
		"an empty entry": "STRESS_SKIP := ^TestOne$$||^TestTwo$$\n",
	} {
		if got, err := ParseSkipList([]byte(bad)); err == nil {
			t.Errorf("%s: ParseSkipList = %q, want an error", name, got)
		}
	}
}

// The Makefile this repository has now gives its six tests: the parser reads
// the real shape, the doubled dollars included.
func TestParseSkipList_ReadsThisRepositorysMakefile(t *testing.T) {
	raw, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseSkipList(raw)
	if err != nil {
		t.Fatalf("ParseSkipList of the Makefile: %v", err)
	}
	for _, want := range []string{"TestRun_ASearchOnTheFakeTarget", "TestBreakpoint_FindsTheStandsCapacity"} {
		found := false
		for _, n := range got {
			found = found || n == want
		}
		if !found {
			t.Errorf("%q is not among %q", want, got)
		}
	}
}

// The judge counts TimingRepeats runs, so the recipe must ask for them, as
// JSON, without the race detector (it is what slows the tests down), over the
// tests of the same list.
func TestMakefile_StressTimingRunsWhatTheJudgeExpects(t *testing.T) {
	raw, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	var recipe string
	lines := strings.Split(string(raw), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "stress-timing:") && i+1 < len(lines) {
			recipe = lines[i+1]
		}
	}
	if recipe == "" {
		t.Fatal("the Makefile has no stress-timing target")
	}
	for _, want := range []string{
		fmt.Sprintf("-count=%d", TimingRepeats), "-json", `-run "$(STRESS_SKIP)"`, "-timeout 90m", "./...",
	} {
		if !strings.Contains(recipe, want) {
			t.Errorf("stress-timing recipe %q lacks %q", recipe, want)
		}
	}
	if strings.Contains(recipe, "-race") {
		t.Errorf("stress-timing recipe %q has -race: the detector's slowdown is what this job must not measure", recipe)
	}
}

// ---- the judge ----

// goEvents writes the `go test -json` lines of one test run n times over: pass
// passes, fail fails, skip skips.
func goEvents(pkg, test string, pass, fail, skip int) string {
	var b strings.Builder
	line := func(action string) {
		fmt.Fprintf(&b, `{"Time":"2026-10-23T10:00:00Z","Action":%q,"Package":%q,"Test":%q}`+"\n", action, pkg, test)
	}
	for range pass {
		line("run")
		line("pass")
	}
	for range fail {
		line("run")
		line("fail")
	}
	for range skip {
		line("run")
		line("skip")
	}
	return b.String()
}

func goOutput(pkg, text string) string {
	return fmt.Sprintf(`{"Time":"2026-10-23T10:00:00Z","Action":"output","Package":%q,"Output":%q}`+"\n", pkg, text)
}

func goPkg(pkg, action string) string {
	return fmt.Sprintf(`{"Time":"2026-10-23T10:00:00Z","Action":%q,"Package":%q}`+"\n", action, pkg)
}

var twoTests = []string{"TestA", "TestB"}

func requireRed(t *testing.T, r TimingReport, mention string) {
	t.Helper()
	if r.Green() {
		t.Fatalf("report is green, want red (mentioning %q):\n%s", mention, r.Table())
	}
	if !strings.Contains(strings.Join(r.Red, "\n"), mention) {
		t.Errorf("red reasons %q do not mention %q", r.Red, mention)
	}
}

func TestTiming_EveryTestTwentyTimesWithoutFailIsGreen(t *testing.T) {
	ev := goEvents("p", "TestA", 20, 0, 0) + goEvents("q", "TestB", 20, 0, 0) + goPkg("p", "pass") + goPkg("q", "pass")
	r := Timing(twoTests, []byte(ev), 0)
	if !r.Green() {
		t.Errorf("report is red: %q\n%s", r.Red, r.Table())
	}
	want := []TimingRow{{Name: "TestA", Runs: 20}, {Name: "TestB", Runs: 20}}
	if !reflect.DeepEqual(r.Rows, want) {
		t.Errorf("Rows = %+v, want %+v", r.Rows, want)
	}
}

// The threshold is 5 of 20: five fails are noise on a starved runner, six are
// not.
func TestTiming_TheThresholdIsFiveFailsOfTwenty(t *testing.T) {
	for _, tc := range []struct {
		fails int
		green bool
	}{{1, true}, {5, true}, {6, false}, {20, false}} {
		ev := goEvents("p", "TestA", 20-tc.fails, tc.fails, 0) + goEvents("p", "TestB", 20, 0, 0)
		r := Timing(twoTests, []byte(ev), 1)
		if r.Green() != tc.green {
			t.Errorf("%d fails of 20: green = %v, want %v\n%s", tc.fails, r.Green(), tc.green, r.Table())
		}
		if !tc.green {
			requireRed(t, r, "TestA")
		}
	}
}

// A run cut short must not pass for a green one: seven repeats with one fail
// is under the threshold and still thirteen repeats short.
func TestTiming_ARunThatEndedEarlyIsRed(t *testing.T) {
	for _, runs := range []int{19, 7, 21} {
		ev := goEvents("p", "TestA", runs, 0, 0) + goEvents("p", "TestB", 20, 0, 0)
		requireRed(t, Timing(twoTests, []byte(ev), 0), "TestA")
	}
	ev := goEvents("p", "TestA", 6, 1, 0) + goEvents("p", "TestB", 20, 0, 0)
	requireRed(t, Timing(twoTests, []byte(ev), 1), "TestA")
}

// A skip tests nothing, however many repeats were counted around it.
func TestTiming_ASkipIsRed(t *testing.T) {
	for _, tc := range []struct{ pass, skip int }{{20, 1}, {19, 1}, {0, 20}} {
		ev := goEvents("p", "TestA", tc.pass, 0, tc.skip) + goEvents("p", "TestB", 20, 0, 0)
		requireRed(t, Timing(twoTests, []byte(ev), 0), "TestA")
	}
}

// A renamed test is not found by -run: both runs are green with «no tests to
// run», and nothing runs the test anywhere.
func TestTiming_ATestThatNeverRanIsRed(t *testing.T) {
	ev := goEvents("p", "TestA", 20, 0, 0) + goPkg("p", "pass")
	r := Timing(twoTests, []byte(ev), 0)
	requireRed(t, r, "TestB")
	if len(r.Rows) != 2 || r.Rows[1] != (TimingRow{Name: "TestB"}) {
		t.Errorf("Rows = %+v, want a zero row for TestB", r.Rows)
	}
	requireRed(t, Timing(twoTests, nil, 0), "TestA")
}

func TestTiming_APackageThatDoesNotBuildIsRed(t *testing.T) {
	// Nothing ran at all.
	broken := goOutput("p", "FAIL\tp [build failed]\n") + goPkg("p", "fail")
	requireRed(t, Timing(twoTests, []byte(broken), 1), "TestA")

	// The listed tests ran 20 times each; another package did not build.
	ev := goEvents("p", "TestA", 20, 0, 0) + goEvents("p", "TestB", 20, 0, 0) + goPkg("p", "pass")
	requireRed(t, Timing(twoTests, []byte(ev+broken), 1), "build")
	requireRed(t, Timing(twoTests, []byte(ev+`{"Action":"build-fail","ImportPath":"q"}`+"\n"), 1), "build")
	// Even when the exit code was lost on the way.
	requireRed(t, Timing(twoTests, []byte(ev+broken), 0), "build")
}

func TestTiming_APanicOrATimeoutIsRed(t *testing.T) {
	// The test that panicked is counted as one fail among its repeats.
	last := goEvents("p", "TestA", 19, 1, 0) + goOutput("p", "panic: boom\n") + goEvents("p", "TestB", 20, 0, 0)
	requireRed(t, Timing(twoTests, []byte(last), 1), "panic")

	timeout := goEvents("p", "TestA", 20, 0, 0) + goEvents("p", "TestB", 20, 0, 0) +
		goOutput("q", "panic: test timed out after 1h30m0s\n") + goPkg("q", "fail")
	requireRed(t, Timing(twoTests, []byte(timeout), 1), "panic")

	// A panic line that came as plain text, not as an event.
	raw := goEvents("p", "TestA", 20, 0, 0) + goEvents("p", "TestB", 20, 0, 0) + "panic: runtime error\n"
	requireRed(t, Timing(twoTests, []byte(raw), 1), "panic")
}

// Exit 1 is explained by a listed test that failed within the threshold and
// by nothing else.
func TestTiming_TheExitCodeNeedsAListedTestThatFailed(t *testing.T) {
	ev := goEvents("p", "TestA", 17, 3, 0) + goEvents("p", "TestB", 20, 0, 0) + goPkg("p", "fail")
	if r := Timing(twoTests, []byte(ev), 1); !r.Green() {
		t.Errorf("3 fails and exit 1: red %q", r.Red)
	}

	clean := goEvents("p", "TestA", 20, 0, 0) + goEvents("p", "TestB", 20, 0, 0)
	requireRed(t, Timing(twoTests, []byte(clean), 1), "exit")

	other := clean + goEvents("p", "TestZ", 0, 3, 0)
	requireRed(t, Timing(twoTests, []byte(other), 1), "exit")
}

// Only the listed names and only whole tests: a subtest is not a repeat.
func TestTiming_OnlyTopLevelListedTestsAreCounted(t *testing.T) {
	ev := goEvents("p", "TestA", 20, 0, 0) + goEvents("p", "TestA/sub", 20, 10, 0) +
		goEvents("p", "TestB", 20, 0, 0) + goEvents("p", "TestZ", 30, 0, 0)
	r := Timing(twoTests, []byte(ev), 0)
	want := []TimingRow{{Name: "TestA", Runs: 20}, {Name: "TestB", Runs: 20}}
	if !r.Green() || !reflect.DeepEqual(r.Rows, want) {
		t.Errorf("red %q, Rows = %+v, want green %+v", r.Red, r.Rows, want)
	}
}

func TestTiming_AStrayLineIsNotAnError(t *testing.T) {
	ev := "make: Entering directory\n" + goEvents("p", "TestA", 20, 0, 0) + "\n" + goEvents("p", "TestB", 20, 0, 0)
	if r := Timing(twoTests, []byte(ev), 0); !r.Green() {
		t.Errorf("red %q", r.Red)
	}
}

func TestTiming_TheTableNamesEveryTestAndEveryReason(t *testing.T) {
	ev := goEvents("p", "TestA", 17, 3, 0) + goEvents("p", "TestB", 19, 0, 1)
	r := Timing(twoTests, []byte(ev), 1)
	table := r.Table()
	for _, want := range []string{"| test | runs | fails | skips |", "| TestA | 20 | 3 | 0 |", "| TestB | 19 | 0 | 1 |"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	for _, reason := range r.Red {
		if !strings.Contains(table, reason) {
			t.Errorf("table lacks the reason %q:\n%s", reason, table)
		}
	}
	if len(r.Red) == 0 {
		t.Error("a skip and 19 runs made no red reason")
	}
}
