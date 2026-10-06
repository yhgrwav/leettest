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

// Command stressplan is what the stress and release workflows call: the
// rules are in package stressplan, this only reads files and git and prints.
//
//	stressplan plan <list-file>                   matrix JSON for the stress job
//	stressplan verdicts <list-file> <plan.json> <results-dir>
//	                                                 "<branch> green|red" per branch
//	stressplan title <branch>                     the failure issue's title
//	stressplan tag <tag> <points-at-file>         exit 1 unless the tag is a branch tip
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yhgrwav/leettest/test/stressplan"
)

const usage = `usage:
  stressplan plan <list-file>
  stressplan verdicts <list-file> <plan.json> <results-dir>
  stressplan title <branch>
  stressplan tag <tag> <points-at-file>`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	var err error
	switch cmd, rest := args[0], args[1:]; {
	case cmd == "plan" && len(rest) == 1:
		err = plan(rest[0], stdout, stderr)
	case cmd == "verdicts" && len(rest) == 3:
		err = verdicts(rest[0], rest[1], rest[2], stdout, stderr)
	case cmd == "title" && len(rest) == 1:
		fmt.Fprintln(stdout, stressplan.IssueTitle(rest[0]))
	case cmd == "tag" && len(rest) == 2:
		err = tag(rest[0], rest[1])
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "stressplan:", err)
		return 1
	}
	return 0
}

func readList(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return stressplan.ParseList(raw)
}

type matrixEntry struct {
	Branch string `json:"branch"`
	SHA    string `json:"sha"`
	Go     string `json:"go"`
	Name   string `json:"name"`
}

// plan prints the matrix. A branch whose files are broken goes to stderr
// and the others are still planned; nothing planned at all is an error.
func plan(listPath string, stdout, stderr io.Writer) error {
	names, err := readList(listPath)
	if err != nil {
		return err
	}
	branches := make([]stressplan.Branch, 0, len(names))
	for _, n := range names {
		branches = append(branches, fromGit(n, stderr))
	}
	jobs, planErr := stressplan.Plan(branches)
	if len(jobs) == 0 {
		return errors.Join(errors.New("no job planned"), planErr)
	}
	if planErr != nil {
		fmt.Fprintln(stderr, "stressplan:", planErr)
	}
	entries := make([]matrixEntry, 0, len(jobs))
	for _, j := range jobs {
		entries = append(entries, matrixEntry{Branch: j.Branch, SHA: j.SHA, Go: j.Go, Name: j.Name()})
	}
	out, err := json.Marshal(map[string]any{"include": entries})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(out))
	return err
}

// fromGit reads a branch's tip and its two workflow files from origin. What
// git cannot give stays empty and Plan names the branch.
func fromGit(name string, stderr io.Writer) stressplan.Branch {
	b := stressplan.Branch{Name: name}
	sha, err := git("rev-parse", "origin/"+name)
	if err != nil {
		fmt.Fprintf(stderr, "stressplan: branch %s: %v\n", name, err)
		return b
	}
	b.SHA = strings.TrimSpace(string(sha))
	b.CI, _ = git("show", "origin/"+name+":.github/workflows/ci.yml")
	b.Release, _ = git("show", "origin/"+name+":.github/workflows/release.yml")
	return b
}

func git(args ...string) ([]byte, error) {
	out, err := exec.CommandContext(context.Background(), "git", args...).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// verdicts reads the matrix the plan job wrote and the result file each stress
// job wrote about itself. A file that cannot be read or parsed is named on
// stderr and counts as no result: its job is red.
func verdicts(listPath, planPath, resultsDir string, stdout, stderr io.Writer) error {
	names, err := readList(listPath)
	if err != nil {
		return err
	}
	rawPlan, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	var matrix struct {
		Include []matrixEntry `json:"include"`
	}
	if err = json.Unmarshal(rawPlan, &matrix); err != nil {
		// The plan job failed and wrote nothing: no job was planned, so every
		// branch is red, and the report must still get to open its issues.
		fmt.Fprintf(stderr, "stressplan: %s: %v\n", planPath, err)
		matrix.Include = nil
	}
	if len(matrix.Include) == 0 {
		fmt.Fprintf(stderr, "stressplan: %s: no job planned\n", planPath)
	}
	planned := make([]stressplan.Job, 0, len(matrix.Include))
	for _, e := range matrix.Include {
		planned = append(planned, stressplan.Job{Branch: e.Branch, SHA: e.SHA, Go: e.Go})
	}

	files, err := os.ReadDir(resultsDir)
	if err != nil {
		return err
	}
	var results []stressplan.Result
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		path := filepath.Join(resultsDir, f.Name())
		raw, err := os.ReadFile(path)
		if err == nil {
			var r stressplan.Result
			if r, err = stressplan.ParseResult(raw); err == nil {
				results = append(results, r)
				continue
			}
		}
		fmt.Fprintf(stderr, "stressplan: %s: %v\n", path, err)
	}

	green := stressplan.Verdicts(names, planned, results)
	for _, n := range names {
		verdict := "red"
		if green[n] {
			verdict = "green"
		}
		fmt.Fprintf(stdout, "%s %s\n", n, verdict)
	}
	return nil
}

func tag(name, pointsAtPath string) error {
	raw, err := os.ReadFile(pointsAtPath)
	if err != nil {
		return err
	}
	return stressplan.TagOnReleaseBranch(name, string(raw))
}
