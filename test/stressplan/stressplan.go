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
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

const shortSHA = 7

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
func (j Job) Name() string {
	return "stress " + j.Branch + "@" + short(j.SHA) + " go" + j.Go
}

func short(sha string) string {
	if len(sha) > shortSHA {
		return sha[:shortSHA]
	}
	return sha
}

// ParseJobName reads a job name written by Name; SHA comes back as its seven
// characters. ok is false for any other name.
func ParseJobName(name string) (j Job, ok bool) {
	fields := strings.Fields(name)
	if len(fields) != 3 || fields[0] != "stress" || !strings.HasPrefix(fields[2], "go") {
		return Job{}, false
	}
	at := strings.LastIndex(fields[1], "@")
	if at <= 0 || len(fields[1])-at-1 != shortSHA {
		return Job{}, false
	}
	goVer := strings.TrimPrefix(fields[2], "go")
	if goVer == "" {
		return Job{}, false
	}
	return Job{Branch: fields[1][:at], SHA: fields[1][at+1:], Go: goVer}, true
}

// ParseList reads .github/stress-branches: one branch per line; blank lines
// and lines starting with # are skipped, a repeated branch counts once. A
// line with a space inside or a trailing comment is an error, not a guess.
func ParseList(raw []byte) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsAny(line, " \t") {
			return nil, fmt.Errorf("stress-branches line %d: %q is more than one branch name", i+1, line)
		}
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	return out, nil
}

// Plan is the jobs for the branches: each branch with every Go version of its
// own ci.yml test matrix and the Go its release.yml builds with. A branch
// whose files lack either is an error naming it; the other branches still get
// their jobs. No branches at all is an error: an empty matrix checks nothing.
func Plan(branches []Branch) ([]Job, error) {
	if len(branches) == 0 {
		return nil, errors.New("no branches under the stress count: an empty matrix checks nothing")
	}
	var jobs []Job
	var errs []error
	for _, b := range branches {
		versions, err := goVersions(b)
		if err != nil {
			errs = append(errs, fmt.Errorf("branch %s: %w", b.Name, err))
			continue
		}
		for _, v := range versions {
			jobs = append(jobs, Job{Branch: b.Name, SHA: b.SHA, Go: v})
		}
	}
	return jobs, errors.Join(errs...)
}

type ciFile struct {
	Jobs struct {
		Test struct {
			Strategy struct {
				Matrix struct {
					Go []string `yaml:"go"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"test"`
	} `yaml:"jobs"`
}

type releaseFile struct {
	Jobs struct {
		Release struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"release"`
	} `yaml:"jobs"`
}

// goVersions is the branch's test matrix plus the release Go, each once,
// oldest first.
func goVersions(b Branch) ([]string, error) {
	var ci ciFile
	if err := yaml.Unmarshal(b.CI, &ci); err != nil {
		return nil, fmt.Errorf("ci.yml: %w", err)
	}
	versions := ci.Jobs.Test.Strategy.Matrix.Go
	if len(versions) == 0 {
		return nil, errors.New("ci.yml has no jobs.test.strategy.matrix.go")
	}

	var rel releaseFile
	if err := yaml.Unmarshal(b.Release, &rel); err != nil {
		return nil, fmt.Errorf("release.yml: %w", err)
	}
	releaseGo := ""
	for _, s := range rel.Jobs.Release.Steps {
		if strings.HasPrefix(s.Uses, "actions/setup-go@") && s.With["go-version"] != "" {
			releaseGo = s.With["go-version"]
			break
		}
	}
	if releaseGo == "" {
		return nil, errors.New("release.yml has no setup-go with a go-version: the release build's Go would go unstressed")
	}

	seen := map[string]bool{}
	var out []string
	for _, v := range append(append([]string{}, versions...), releaseGo) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return versionLess(out[i], out[j]) })
	return out, nil
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		x, y := part(pa, i), part(pb, i)
		if x != y {
			return x < y
		}
	}
	return false
}

func part(p []string, i int) int {
	if i >= len(p) {
		return 0
	}
	n, _ := strconv.Atoi(p[i])
	return n
}

// Result is what a job wrote about itself: its name and its status
// (success, failure or cancelled), not what the jobs API says later.
type Result struct {
	Name       string
	Conclusion string
}

// ParseResult reads a result file: one line "<job name>\t<status>". A wrong
// field count, an empty name or an empty status is an error.
func ParseResult(raw []byte) (Result, error) {
	line := strings.TrimRight(string(raw), "\r\n")
	fields := strings.Split(line, "\t")
	if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
		return Result{}, fmt.Errorf("result %q: want \"<job name>\\t<status>\"", line)
	}
	return Result{Name: fields[0], Conclusion: fields[1]}, nil
}

// Verdicts says, for each branch under the count, whether the run is green
// for it: it has at least one planned job and every planned job of it has a
// result with the status "success". A planned job without a result is red
// (the job never wrote it: it did not finish), so is failure, cancelled (a
// job past timeout-minutes too) or anything else. A result whose name is not
// planned is ignored. A branch with no planned job is not green.
func Verdicts(branches []string, planned []Job, results []Result) map[string]bool {
	status := map[string][]string{}
	for _, r := range results {
		status[r.Name] = append(status[r.Name], r.Conclusion)
	}

	green := make(map[string]bool, len(branches))
	for _, b := range branches {
		green[b] = false
	}
	red := map[string]bool{}
	for _, j := range planned {
		if _, listed := green[j.Branch]; !listed {
			continue
		}
		got := status[j.Name()]
		if len(got) == 0 || slices.ContainsFunc(got, func(s string) bool { return s != "success" }) {
			red[j.Branch] = true
			continue
		}
		green[j.Branch] = true
	}
	for b := range red {
		green[b] = false
	}
	return green
}

// IssueTitle is the title of the failure issue for a branch.
func IssueTitle(branch string) string { return "Stress run failed on " + branch }
