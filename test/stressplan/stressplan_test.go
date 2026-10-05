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

func TestParseList_SkipsBlanksAndComments(t *testing.T) {
	got := ParseList([]byte("# branches under the count\nmain\n\n  release/v0.1  \n# release/v0.0\n"))
	if want := []string{"main", "release/v0.1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseList = %q, want %q", got, want)
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

// A branch whose files name no Go version is an error naming it, not a branch
// that silently gets no jobs and so never turns red.
func TestPlan_NoGoVersionIsAnError(t *testing.T) {
	_, err := Plan([]Branch{
		{Name: "main", SHA: "9a088764ab", CI: []byte(ciYML), Release: []byte(releaseYML)},
		{Name: "release/v0.3", SHA: "1234567890", CI: []byte("jobs: {}\n"), Release: []byte("jobs: {}\n")},
	})
	if err == nil || !strings.Contains(err.Error(), "release/v0.3") {
		t.Errorf("Plan = %v, want an error naming release/v0.3", err)
	}
}

// The workflow files this repository has now give a plan: the parser reads
// the real shape, not only the fixtures above.
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
	if err != nil || len(jobs) == 0 {
		t.Fatalf("Plan of this repository: %d jobs, %v", len(jobs), err)
	}
}

// A red job counts against its own branch only: main red, the release branch
// green — the run counts for the release branch.
func TestVerdicts_ARedJobOfAnotherBranchDoesNotCount(t *testing.T) {
	got := Verdicts([]Result{
		{Job: Job{Branch: "main", Go: "1.26"}, Green: false},
		{Job: Job{Branch: "main", Go: "1.27.1"}, Green: true},
		{Job: Job{Branch: "release/v0.1", Go: "1.26"}, Green: true},
		{Job: Job{Branch: "release/v0.1", Go: "1.27.1"}, Green: true},
	})
	if want := map[string]bool{"main": false, "release/v0.1": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("Verdicts = %v, want %v", got, want)
	}
}

func TestIssueTitle_NamesTheBranch(t *testing.T) {
	if got, want := IssueTitle("release/v0.1"), "Stress run failed on release/v0.1"; got != want {
		t.Errorf("IssueTitle = %q, want %q", got, want)
	}
}
