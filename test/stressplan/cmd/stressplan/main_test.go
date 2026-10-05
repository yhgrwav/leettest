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
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runCmd(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The jobs of a run come from `gh api --paginate`: one JSON object per page,
// back to back. A red job on page two still counts against its branch.
func TestVerdicts_ReadsEveryPageOfTheJobList(t *testing.T) {
	list := write(t, "stress-branches", "main\nrelease/v0.1\n")
	jobs := write(t, "jobs.json", `{"total_count":3,"jobs":[
  {"name":"plan","conclusion":"success"},
  {"name":"stress main@9a08876 go1.26","conclusion":"success"}]}
{"total_count":3,"jobs":[
  {"name":"stress release/v0.1@d5bfed5 go1.26","conclusion":"failure"},
  {"name":"stress release/v0.1@d5bfed5 go1.27.1","conclusion":"success"}]}`)

	code, out, errOut := runCmd(t, "verdicts", list, jobs)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if want := "main green\nrelease/v0.1 red\n"; out != want {
		t.Errorf("verdicts:\n%s\nwant:\n%s", out, want)
	}
}

func TestTag_ExitCodeFollowsTheBranchTip(t *testing.T) {
	tipOnly := write(t, "pointsat.txt", "  origin/release/v0.1\n")
	mainOnly := write(t, "pointsat.txt", "  origin/main\n")

	if code, _, errOut := runCmd(t, "tag", "v0.1.0", tipOnly); code != 0 {
		t.Errorf("v0.1.0 at the tip of release/v0.1: exit %d: %s", code, errOut)
	}
	code, _, errOut := runCmd(t, "tag", "v0.1.0", mainOnly)
	if code != 1 || !strings.Contains(errOut, "origin/release/v0.1") {
		t.Errorf("v0.1.0 on main only: exit %d, stderr %q, want 1 naming origin/release/v0.1", code, errOut)
	}
}

func TestTitle_IsThePackages(t *testing.T) {
	if _, out, _ := runCmd(t, "title", "release/v0.1"); out != "Stress run failed on release/v0.1\n" {
		t.Errorf("title = %q", out)
	}
}

func TestUsage_UnknownCommandExitsTwo(t *testing.T) {
	if code, _, errOut := runCmd(t, "nope"); code != 2 || !strings.Contains(errOut, "usage") {
		t.Errorf("exit %d, stderr %q, want 2 with usage", code, errOut)
	}
}

const ciYML = "jobs:\n  test:\n    strategy:\n      matrix:\n        go: [\"1.26\", \"1.27.1\"]\n"
const releaseYML = "jobs:\n  release:\n    steps:\n      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e\n        with:\n          go-version: \"1.27.1\"\n"

// repoWithBranches makes a repository whose origin/<branch> refs hold the
// workflow files given, the way a fetched clone has them.
func repoWithBranches(t *testing.T, files map[string][2]string) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	for branch, pair := range files {
		wf := filepath.Join(dir, ".github", "workflows")
		if err := os.MkdirAll(wf, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wf, "ci.yml"), []byte(pair[0]), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wf, "release.yml"), []byte(pair[1]), 0o600); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", "-A")
		gitIn(t, dir, "commit", "-q", "-m", branch)
		gitIn(t, dir, "update-ref", "refs/remotes/origin/"+branch, "HEAD")
	}
	return dir
}

type matrix struct {
	Include []struct{ Branch, SHA, Go, Name string }
}

// A broken branch goes to stderr and the others are still planned; nothing
// plannable is an error.
func TestPlan_ABrokenBranchDoesNotStopTheOthers(t *testing.T) {
	dir := repoWithBranches(t, map[string][2]string{
		"main":         {ciYML, releaseYML},
		"release/v0.3": {"jobs: {}\n", "jobs: {}\n"},
	})
	t.Chdir(dir)
	list := write(t, "stress-branches", "main\nrelease/v0.3\nrelease/v0.9\n")

	code, out, errOut := runCmd(t, "plan", list)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var m matrix
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("stdout is not the matrix JSON: %v\n%s", err, out)
	}
	if len(m.Include) != 2 || m.Include[0].Branch != "main" || m.Include[1].Branch != "main" {
		t.Errorf("matrix %+v, want main's two jobs only", m.Include)
	}
	if len(m.Include[0].SHA) != 40 || !strings.HasPrefix(m.Include[0].Name, "stress main@"+m.Include[0].SHA[:7]+" go") {
		t.Errorf("entry %+v: want the full sha and the job name carrying its seven characters", m.Include[0])
	}
	for _, branch := range []string{"release/v0.3", "release/v0.9"} {
		if !strings.Contains(errOut, branch) {
			t.Errorf("stderr %q does not name %s", errOut, branch)
		}
	}

	only := write(t, "stress-branches", "release/v0.3\n")
	if code, _, errOut := runCmd(t, "plan", only); code != 1 {
		t.Errorf("plan of a broken branch only: exit %d (%s), want 1", code, errOut)
	}
}
