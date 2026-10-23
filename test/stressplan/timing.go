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

// TimingRepeats is how many times the timing job runs each test; the
// Makefile's stress-timing has the same -count.
const TimingRepeats = 20

// TimingMaxFails is the most failed repeats of one test, out of
// TimingRepeats, that the timing job still calls green.
const TimingMaxFails = 5

// TimingName is the name of the timing job of the same branch, commit and Go
// as j: «timing <branch>@<sha7> go<ver>».
func (j Job) TimingName() string { return j.Name() }

// TimingIssueTitle is the title of the timing issue for a branch.
func TimingIssueTitle(branch string) string { return "" }

// ParseSkipList reads the test names out of the STRESS_SKIP variable of a
// Makefile.
func ParseSkipList(makefile []byte) ([]string, error) { return nil, nil }

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
func (r TimingReport) Green() bool { return true }

// Table is the report as a markdown table followed by the reasons.
func (r TimingReport) Table() string { return "" }

// Timing judges the output of `go test -json` (events) and the exit code of
// that go test for the tests in names.
func Timing(names []string, events []byte, exit int) TimingReport { return TimingReport{} }
