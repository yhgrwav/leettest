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
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// mrSearchConfig writes a plaintext config of a breaking-point search over the
// health call into dir, next to the token file: two steps of 2s (100 and 125
// rps; a search of 400 rps can end at its first step in 1.5s on Windows, with one
// poll of the watch) so the search outlasts several refresh reads of 1s, and
// metadata_refresh: 1s over the file.
func mrSearchConfig(t *testing.T, dir, addr string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}

	cfg := fmt.Sprintf("app:\n  target:\n    ip: %s\n    port: %s\n  tls: false\n%s  metadata_refresh: 1s\n"+
		"load:\n  calls:\n    - method: %s\n      timeout: 200ms\n"+
		"  breakpoint:\n    from: 100\n    to: 130\n    settle: 50ms\n    hold: 2000ms\n",
		host, port, mrFileApp, checkMethod)

	path := filepath.Join(dir, "leettest.yaml")
	if err = os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// refreshLines are the stderr lines that tell a failed read of the metadata files.
func refreshLines(stderr string) []string {
	var lines []string

	for l := range strings.SplitSeq(stderr, "\n") {
		if strings.HasPrefix(l, "metadata refresh failed: ") {
			lines = append(lines, l)
		}
	}

	return lines
}

// Ground: contract (R3 F-C, spec §4.1 of metadata-refresh) — a search reads the
// files as a plain run does, so a failed read is the same one stderr line, at the
// first failure and once for the whole search, and the report counts every
// failed read. The text is the one a plain run prints (mrStderr).
// Mutation (no watch in the search path) dies on the count; the line printed
// unconditionally dies on the working-source test below.
func TestSearch_AFailedRefreshIsOneLineOnStderr(t *testing.T) {
	dir := mrDir(t, "tok-one\n")
	target := startMRTarget(t)
	cfg := mrSearchConfig(t, dir, target.addr)

	go func() {
		// Half way between the start and the first read of the refresh at 1s.
		time.Sleep(500 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "token.txt"), nil, 0o600)
	}()

	res := runCLI(t.Context(), t, 30*time.Second, "-c", cfg)
	if res.err != nil && !errors.Is(res.err, ErrInvalidRun) {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	if lines := refreshLines(res.stderr); len(lines) != 1 || lines[0] != mrStderr {
		t.Errorf("stderr lines %q, want exactly one: %q\nstderr:\n%s", lines, mrStderr, res.stderr)
	}

	// The report is unchanged: it carries the refresh line with the failures.
	report := regexp.MustCompile(`(?m)^metadata refresh: every 1s, \d+ reads, 0 changed, [1-9]\d* failed \(failing at the end\); ` +
		`the last good value stayed in use; last error: ` + regexp.QuoteMeta(mrLastError) + `$`)
	if !report.MatchString(res.stdout) {
		t.Errorf("no refresh line with the failure in the report:\n%s", res.stdout)
	}
}

// Ground: contract (R3 F-C) — a search whose files are read well says nothing on
// stderr about the refresh, and the report says no read failed.
// Mutation (the line printed unconditionally) dies here.
func TestSearch_AWorkingRefreshSaysNothingOnStderr(t *testing.T) {
	dir := mrDir(t, "tok-one\n")
	target := startMRTarget(t)
	cfg := mrSearchConfig(t, dir, target.addr)

	res := runCLI(t.Context(), t, 30*time.Second, "-c", cfg)
	if res.err != nil && !errors.Is(res.err, ErrInvalidRun) {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	if strings.Contains(res.stderr, "metadata refresh") {
		t.Errorf("stderr mentions the refresh although every read worked:\n%s", res.stderr)
	}

	// The premise: the refresh did run, so the silence is not an absent watch.
	if !regexp.MustCompile(`(?m)^metadata refresh: every 1s, [1-9]\d* reads, 0 changed, 0 failed`).MatchString(res.stdout) {
		t.Errorf("the report has no refresh line with reads and 0 failed:\n%s", res.stdout)
	}
}
