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
// The count reads the branch and the commit back from it with ParseJobName.
func (j Job) Name() string { return "" }

// ParseJobName reads a job name written by Name; SHA comes back as its seven
// characters. ok is false for any other name.
func ParseJobName(name string) (j Job, ok bool) { return Job{}, false }

// ParseList reads .github/stress-branches: one branch per line; blank lines
// and lines starting with # are skipped, a repeated branch counts once. A
// line with a space inside or a trailing comment is an error, not a guess.
func ParseList(raw []byte) ([]string, error) { return nil, nil }

// Plan is the jobs for the branches: each branch with every Go version of its
// own ci.yml test matrix and the Go its release.yml builds with. A branch
// whose files lack either is an error naming it; the other branches still get
// their jobs. No branches at all is an error: an empty matrix checks nothing.
func Plan(branches []Branch) ([]Job, error) { return nil, nil }

// Result is a finished job as the run's job list reports it.
type Result struct {
	Name       string
	Conclusion string
}

// Verdicts says, for each branch under the count, whether the run is green
// for it: it has jobs, and every one concluded "success". Anything else —
// failure, cancelled (a job past timeout-minutes too), skipped — is red for
// its own branch only. A branch with no job is not green.
func Verdicts(branches []string, results []Result) map[string]bool { return nil }

// IssueTitle is the title of the failure issue for a branch.
func IssueTitle(branch string) string { return "" }
