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
	"os"
	"reflect"
	"strings"
	"testing"
)

const ciYML = `
jobs:
  test:
    strategy:
      matrix:
        go: ["1.26", "1.27.1"]
`

const releaseYML = `
jobs:
  release:
    steps:
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version: "1.27.1"
`

func TestParseList(t *testing.T) {
	got, err := ParseList([]byte("# branches under the count\nmain\n\n  release/v0.1  \n# release/v0.0\nmain\n"))
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}
	if want := []string{"main", "release/v0.1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseList = %q, want %q", got, want)
	}

	for _, bad := range []string{"main release/v0.1\n", "release/v0.1 # until the tag\n"} {
		if _, err := ParseList([]byte(bad)); err == nil {
			t.Errorf("ParseList(%q): no error", bad)
		}
	}
}

// An empty list, or one of comments only, leaves nothing to stress: the plan
// says so instead of handing GitHub an empty matrix.
func TestPlan_NothingToStressIsAnError(t *testing.T) {
	for _, raw := range []string{"", "# none yet\n\n"} {
		names, err := ParseList([]byte(raw))
		if err != nil {
			t.Fatalf("ParseList(%q): %v", raw, err)
		}
		var branches []Branch
		for _, n := range names {
			branches = append(branches, Branch{Name: n})
		}
		if _, err := Plan(branches); err == nil {
			t.Errorf("Plan of list %q: no error, want one: nothing would be stressed", raw)
		}
	}
}

// Each branch is checked on its own commit with its own Go versions, and the
// job name carries both, so a branch's count reads its own commits.
func TestPlan_EachBranchOnItsOwnCommitAndGo(t *testing.T) {
	oldCI := strings.Replace(ciYML, `["1.26", "1.27.1"]`, `["1.25", "1.26.3"]`, 1)
	oldRelease := strings.Replace(releaseYML, `"1.27.1"`, `"1.26.3"`, 1)

	jobs, err := Plan([]Branch{
		{Name: "main", SHA: "9a088764ab1365dd", CI: []byte(ciYML), Release: []byte(releaseYML)},
		{Name: "release/v0.1", SHA: "d5bfed53fa113fbe", CI: []byte(oldCI), Release: []byte(oldRelease)},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	var names []string
	for _, j := range jobs {
		names = append(names, j.Name())
	}
	want := []string{
		"stress main@9a08876 go1.26",
		"stress main@9a08876 go1.27.1",
		"stress release/v0.1@d5bfed5 go1.25",
		"stress release/v0.1@d5bfed5 go1.26.3",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("jobs %q, want %q", names, want)
	}
}

// A job name read back gives the same branch, commit and Go — a branch with
// a slash in its name included.
func TestParseJobName_RoundTrip(t *testing.T) {
	for _, j := range []Job{
		{Branch: "main", SHA: "9a08876", Go: "1.26"},
		{Branch: "release/v0.1", SHA: "d5bfed5", Go: "1.27.1"},
	} {
		got, ok := ParseJobName(j.Name())
		if !ok || got != j {
			t.Errorf("ParseJobName(%q) = %+v, %v, want %+v", j.Name(), got, ok, j)
		}
	}
	for _, other := range []string{"", "stress (go 1.26)", "report", "stress main go1.26"} {
		if _, ok := ParseJobName(other); ok {
			t.Errorf("ParseJobName(%q) ok, want not a stress job", other)
		}
	}
}

// The binary is built with release.yml's Go: a branch whose test matrix
// moved past it is still stressed with it.
func TestPlan_TheReleaseGoIsAlwaysIn(t *testing.T) {
	ci := strings.Replace(ciYML, `["1.26", "1.27.1"]`, `["1.26", "1.28"]`, 1)

	jobs, err := Plan([]Branch{{Name: "release/v0.2", SHA: "abcdef0123", CI: []byte(ci), Release: []byte(releaseYML)}})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	var gos []string
	for _, j := range jobs {
		gos = append(gos, j.Go)
	}
	if want := []string{"1.26", "1.27.1", "1.28"}; !reflect.DeepEqual(gos, want) {
		t.Errorf("Go versions %q, want %q", gos, want)
	}
}

// A branch whose files lack a Go version is red for that branch: an error
// naming it. The other branches still get their jobs, so one broken branch
// does not stop another's count.
func TestPlan_ABrokenBranchIsItsOwnError(t *testing.T) {
	noRelease := strings.Replace(releaseYML, `go-version: "1.27.1"`, `go-version-file: go.mod`, 1)

	for _, broken := range []Branch{
		{Name: "release/v0.3", SHA: "1234567890", CI: []byte("jobs: {}\n"), Release: []byte("jobs: {}\n")},
		// The matrix is there, the release Go is not: its builds would go unstressed.
		{Name: "release/v0.3", SHA: "1234567890", CI: []byte(ciYML), Release: []byte(noRelease)},
	} {
		jobs, err := Plan([]Branch{
			{Name: "main", SHA: "9a088764ab", CI: []byte(ciYML), Release: []byte(releaseYML)},
			broken,
		})
		if err == nil || !strings.Contains(err.Error(), "release/v0.3") {
			t.Errorf("Plan = %v, want an error naming release/v0.3", err)
		}
		if len(jobs) != 2 || jobs[0].Branch != "main" || jobs[1].Branch != "main" {
			t.Errorf("jobs %+v, want main's two jobs planned anyway", jobs)
		}
	}
}

// The workflow files this repository has now give exactly its Go versions:
// the parser reads the real shape, the matrix included.
func TestPlan_ReadsThisRepositorysWorkflows(t *testing.T) {
	ci, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	release, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := Plan([]Branch{{Name: "main", SHA: "0000000000", CI: ci, Release: release}})
	if err != nil {
		t.Fatalf("Plan of this repository: %v", err)
	}
	var gos []string
	for _, j := range jobs {
		gos = append(gos, j.Go)
	}
	// Update with the Go floor and GO_LATEST in ci.yml.
	if want := []string{"1.26", "1.27.1"}; !reflect.DeepEqual(gos, want) {
		t.Errorf("Go versions of this repository %q, want %q", gos, want)
	}
}

const sha = "9a088764ab1365dd"

func nameOf(branch, goVer string) string { return Job{Branch: branch, SHA: sha, Go: goVer}.Name() }

func plannedFor(branches ...string) []Job {
	var jobs []Job
	for _, b := range branches {
		for _, v := range []string{"1.26", "1.27.1"} {
			jobs = append(jobs, Job{Branch: b, SHA: sha, Go: v})
		}
	}
	return jobs
}

// A red job counts against its own branch only: main red, the release branch
// green — the run counts for the release branch. Anything but success is red;
// a branch with no planned job is not green.
func TestVerdicts(t *testing.T) {
	name := nameOf

	got := Verdicts([]string{"main", "release/v0.1", "release/v0.2", "release/v0.3"}, plannedFor("main", "release/v0.1", "release/v0.2"), []Result{
		{Name: name("main", "1.26"), Conclusion: "failure"},
		{Name: name("main", "1.27.1"), Conclusion: "success"},
		{Name: name("release/v0.1", "1.26"), Conclusion: "success"},
		{Name: name("release/v0.1", "1.27.1"), Conclusion: "success"},
		{Name: name("release/v0.2", "1.26"), Conclusion: "success"},
		{Name: name("release/v0.2", "1.27.1"), Conclusion: "cancelled"},
		{Name: "report", Conclusion: "skipped"},
	})
	want := map[string]bool{"main": false, "release/v0.1": true, "release/v0.2": false, "release/v0.3": false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Verdicts = %v, want %v", got, want)
	}
}

// A planned job that wrote no result did not finish: its branch is red, which
// is also what a result the jobs API had not recorded yet used to look like.
// The other branch, whose jobs all reported, stays green.
func TestVerdicts_APlannedJobWithoutAResultIsRed(t *testing.T) {
	got := Verdicts([]string{"main", "release/v0.1"}, plannedFor("main", "release/v0.1"), []Result{
		{Name: nameOf("main", "1.26"), Conclusion: "success"},
		{Name: nameOf("release/v0.1", "1.26"), Conclusion: "success"},
		{Name: nameOf("release/v0.1", "1.27.1"), Conclusion: "success"},
	})
	want := map[string]bool{"main": false, "release/v0.1": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Verdicts = %v, want %v", got, want)
	}
}

// Only the planned jobs are read: a failure written for a commit that was not
// planned does not turn main red.
func TestVerdicts_AResultOutsideThePlanIsIgnored(t *testing.T) {
	other := Job{Branch: "main", SHA: "1111111222222", Go: "1.26"}.Name()
	got := Verdicts([]string{"main"}, plannedFor("main"), []Result{
		{Name: nameOf("main", "1.26"), Conclusion: "success"},
		{Name: nameOf("main", "1.27.1"), Conclusion: "success"},
		{Name: other, Conclusion: "failure"},
	})
	if want := map[string]bool{"main": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("Verdicts = %v, want %v", got, want)
	}
}

func TestParseResult(t *testing.T) {
	got, err := ParseResult([]byte("stress main@9a08876 go1.26\tsuccess\n"))
	if err != nil {
		t.Fatalf("ParseResult: %v", err)
	}
	if want := (Result{Name: "stress main@9a08876 go1.26", Conclusion: "success"}); got != want {
		t.Errorf("ParseResult = %+v, want %+v", got, want)
	}

	for _, bad := range []string{
		"stress main@9a08876 go1.26\tsuccess\textra\n",
		"stress main@9a08876 go1.26\t\n",
		"\tsuccess\n",
		"stress main@9a08876 go1.26 success\n",
		"",
	} {
		if _, err := ParseResult([]byte(bad)); err == nil {
			t.Errorf("ParseResult(%q): no error", bad)
		}
	}
}

func TestIssueTitle_NamesTheBranch(t *testing.T) {
	if got, want := IssueTitle("release/v0.1"), "Stress run failed on release/v0.1"; got != want {
		t.Errorf("IssueTitle = %q, want %q", got, want)
	}
}
