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

// Branch is a branch under the stress count and the workflow files as they
// are on that branch.
type Branch struct {
	Name, SHA   string
	CI, Release []byte
}

// Job is one stress job: a branch at a commit, built with one Go version.
type Job struct {
	Branch, SHA, Go string
}

// Name is the job's name as the run shows it: «stress <branch>@<sha7> go<ver>».
// The count reads the branch and the commit back from it.
func (j Job) Name() string { return "" }

// ParseList reads .github/stress-branches: one branch per line; blank lines
// and lines starting with # are skipped.
func ParseList(raw []byte) []string { return nil }

// Plan is the jobs for the branches: each branch with every Go version of its
// own ci.yml test matrix and the Go its release.yml builds with. A branch whose
// files name no Go version is an error, not a branch with no jobs.
func Plan(branches []Branch) ([]Job, error) { return nil, nil }

// Result is a finished job.
type Result struct {
	Job   Job
	Green bool
}

// Verdicts says, per branch, whether the run is green for it: every job of
// that branch is green. Another branch's red job does not touch it.
func Verdicts(results []Result) map[string]bool { return nil }

// IssueTitle is the title of the failure issue for a branch.
func IssueTitle(branch string) string { return "" }
