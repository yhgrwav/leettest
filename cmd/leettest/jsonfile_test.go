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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newJSONPath is a path in a fresh directory where nothing exists yet.
func newJSONPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "report.json")
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return data
}

// fileMissing says whether nothing is at path.
func fileMissing(t *testing.T, path string) bool {
	t.Helper()

	_, err := os.Stat(path)
	if err == nil {
		return false
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s: %v", path, err)
	}

	return true
}

// shape is the names a JSON value is made of: the same keys at every depth,
// the first element standing for an array. Two runs differ in their numbers,
// never in their shape.
func shape(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, x := range v {
			out[k] = shape(x)
		}

		return out
	case []any:
		if len(v) == 0 {
			return []any{}
		}

		return []any{shape(v[0])}
	default:
		return nil
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)

	return out
}

// With -output json the one document goes twice, from one encoding: the bytes
// of stdout and of the file are the same.
func TestRun_JSONFileHoldsTheBytesOfStdoutJSON(t *testing.T) {
	path := newJSONPath(t)

	res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-output", "json", "-json-file", path,
		"-c", writeConfig(t, closedPort(t), checkMethod, plaintext))
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	decodeOnly(t, res.stdout)

	if got := readFile(t, path); !bytes.Equal(got, []byte(res.stdout)) {
		t.Errorf("the file differs from stdout:\nfile   %q\nstdout %q", got, res.stdout)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the directory holds %d entries, want only the report: no temporary file is left", len(entries))
	}
}

// With -output text stdout keeps the text and the file holds the JSON that
// -output json prints: the same object, the numbers of two runs aside.
func TestRun_JSONFileBesideTheTextReport(t *testing.T) {
	cfg := writeConfig(t, closedPort(t), checkMethod, plaintext)
	path := newJSONPath(t)

	res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-json-file", path, "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	if json.Valid([]byte(res.stdout)) || !strings.Contains(res.stdout, checkMethod) {
		t.Errorf("stdout is not the text report:\n%s", res.stdout)
	}

	var inFile map[string]any
	if err := json.Unmarshal(readFile(t, path), &inFile); err != nil {
		t.Fatalf("the file is not JSON: %v", err)
	}
	printed := decodeOnly(t, runCLI(t.Context(), t, 10*time.Second, "-fake", "-output", "json", "-c", cfg).stdout)

	if !reflect.DeepEqual(shape(inFile), shape(printed)) {
		t.Errorf("the file and -output json differ in shape:\nfile    %v\nstdout  %v", keys(inFile), keys(printed))
	}
	for _, k := range []string{"schema_version", "outcome", "target", "version"} {
		if inFile[k] != printed[k] || inFile[k] == nil {
			t.Errorf("%s: file %v, -output json %v", k, inFile[k], printed[k])
		}
	}
}

// A path that exists is never overwritten, empty or not, and the target does
// not see a call.
func TestRun_JSONFileThatExistsFailsBeforeAnyCall(t *testing.T) {
	target := startTarget(t)
	path := newJSONPath(t)
	if err := os.WriteFile(path, []byte("earlier report"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := runCLI(t.Context(), t, 10*time.Second, "-json-file", path, "-c", writeConfig(t, target.addr, checkMethod, plaintext))

	if exitCode(res.err) != 1 || res.err == nil || !strings.Contains(res.err.Error(), path) ||
		!strings.Contains(res.err.Error(), "exists") {
		t.Errorf("err = %v, want exit code 1 naming %s as existing", res.err, path)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want no report", res.stdout)
	}
	if got := string(readFile(t, path)); got != "earlier report" {
		t.Errorf("the existing file was changed: %q", got)
	}
	if n := target.health.calls.Load(); n != 0 {
		t.Errorf("the target received %d calls, want none", n)
	}
}

// The temporary file has no fixed name: a leftover <path>.tmp from an earlier
// failure is neither used nor touched.
func TestRun_JSONFileLeavesAnotherFilesTmpAlone(t *testing.T) {
	path := newJSONPath(t)
	leftover := path + ".tmp"
	if err := os.WriteFile(leftover, []byte("leftover"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-json-file", path,
		"-c", writeConfig(t, closedPort(t), checkMethod, plaintext))
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	var report map[string]any
	if err := json.Unmarshal(readFile(t, path), &report); err != nil {
		t.Fatalf("the file is not JSON: %v", err)
	}
	if got := string(readFile(t, leftover)); got != "leftover" {
		t.Errorf("%s was changed: %q", leftover, got)
	}
}

// A directory that is not there is a config error like any other: exit 1
// naming the path, nothing sent, nothing created.
func TestRun_JSONFileInAMissingDirectoryFailsBeforeAnyCall(t *testing.T) {
	target := startTarget(t)
	dir := filepath.Join(t.TempDir(), "no-such-dir")
	path := filepath.Join(dir, "report.json")

	res := runCLI(t.Context(), t, 10*time.Second, "-json-file", path, "-c", writeConfig(t, target.addr, checkMethod, plaintext))

	if exitCode(res.err) != 1 || res.err == nil || !strings.Contains(res.err.Error(), path) {
		t.Errorf("err = %v, want exit code 1 naming %s", res.err, path)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want no report", res.stdout)
	}
	if n := target.health.calls.Load(); n != 0 {
		t.Errorf("the target received %d calls, want none", n)
	}
	if !fileMissing(t, dir) {
		t.Errorf("%s was created", dir)
	}
}

// A search writes its own object, the one -output json prints.
func TestRun_JSONFileOfASearchHoldsTheBreakpointObject(t *testing.T) {
	cfg := writeSearch(t, closedPort(t), checkMethod)

	path := newJSONPath(t)
	res := runCLI(t.Context(), t, 60*time.Second, "-fake", "-json-file", path, "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	if json.Valid([]byte(res.stdout)) {
		t.Errorf("stdout is JSON, want the text report of the search:\n%s", res.stdout)
	}
	var inFile map[string]any
	if err := json.Unmarshal(readFile(t, path), &inFile); err != nil {
		t.Fatalf("the file is not JSON: %v", err)
	}
	if _, ok := inFile["breakpoint"].(map[string]any); !ok || inFile["schema_version"] != float64(1) {
		t.Errorf("the file lacks the breakpoint object or schema_version 1: keys %v", keys(inFile))
	}

	both := newJSONPath(t)
	res = runCLI(t.Context(), t, 60*time.Second, "-fake", "-output", "json", "-json-file", both, "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	decodeOnly(t, res.stdout)
	if got := readFile(t, both); !bytes.Equal(got, []byte(res.stdout)) {
		t.Errorf("the search file differs from stdout:\nfile   %q\nstdout %q", got, res.stdout)
	}
}

// A soft stop has a report, so it has the file: exit 3, outcome incomplete.
func TestRun_JSONFileOfAStoppedRun(t *testing.T) {
	path := newJSONPath(t)

	res := runSignalled(t, "      timeout: 300ms\n", func(stops, _ chan<- struct{}) { stops <- struct{}{} },
		"-json-file", path)

	if exitCode(res.err) != 3 {
		t.Fatalf("exit code %d, want 3", exitCode(res.err))
	}
	var report map[string]any
	if err := json.Unmarshal(readFile(t, path), &report); err != nil {
		t.Fatalf("the file is not JSON: %v", err)
	}
	if report["outcome"] != "incomplete" {
		t.Errorf("outcome = %v, want incomplete", report["outcome"])
	}
}

// No report, no file: a run that fails after the file was created leaves no
// empty file behind, or the same command fails on "exists" and a thresholds
// step reads an empty file as broken JSON. The file is already there while
// the run connects: the check is before the network.
func TestRun_JSONFileIsRemovedWhenTheRunEndsWithoutAReport(t *testing.T) {
	path := newJSONPath(t)
	addr := silentTarget(t)

	var seen atomic.Bool
	stop := make(chan struct{})
	go func() {
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				if _, err := os.Stat(path); err == nil {
					seen.Store(true)
				}
			}
		}
	}()
	defer close(stop)

	res := runCLI(t.Context(), t, 5*time.Second, "-connect-timeout", "500ms", "-json-file", path,
		"-c", writeConfig(t, addr, checkMethod, plaintext))

	if exitCode(res.err) != 1 || res.err == nil || !strings.Contains(res.err.Error(), addr) {
		t.Fatalf("err = %v, want exit code 1 from the connection to %s", res.err, addr)
	}
	if !seen.Load() {
		t.Errorf("the file was not there while the run connected")
	}
	if !fileMissing(t, path) {
		t.Errorf("the empty file is left after a run with no report")
	}
}

// An abort that exits at once (os.Exit skips every defer) removes the file in
// exitNow itself.
func TestExitNow_RemovesTheJSONFileOfTheRunInProgress(t *testing.T) {
	path := newJSONPath(t)

	var exitedWith atomic.Int64
	exitedWith.Store(-1)
	exitProcess = func(code int) { exitedWith.Store(int64(code)) }
	t.Cleanup(func() { exitProcess = os.Exit })

	var existedBefore, missingAfter bool
	runSignalled(t, "      timeout: 300ms\n", func(stops, _ chan<- struct{}) {
		_, err := os.Stat(path)
		existedBefore = err == nil
		exitNow()
		missingAfter = fileMissing(t, path)
		// The run goes on to its own end once the test has looked.
		stops <- struct{}{}
	}, "-json-file", path)

	if exitedWith.Load() < 0 {
		t.Fatal("exitNow did not exit")
	}
	if !existedBefore {
		t.Errorf("the file was not there while the run was going")
	}
	if !missingAfter {
		t.Errorf("exitNow left the file: an exit without a report must leave none")
	}
}

// The write after the run fails: the text report is already printed, stderr
// names the path and the error, and the code is 1 whatever the run's own was.
func TestRun_JSONFileWriteFailureIsExitOne(t *testing.T) {
	boom := errors.New("disk is full")

	for _, c := range []struct {
		name    string
		args    func(t *testing.T) []string
		want    string
		wantRaw bool
	}{
		{"complete run", func(t *testing.T) []string {
			return []string{"-fake", "-c", writeConfig(t, closedPort(t), checkMethod, plaintext)}
		}, checkMethod, false},
		{"invalid run", func(t *testing.T) []string {
			return []string{"-c", writeConfig(t, startTarget(t).addr, missingMethod, plaintext)}
		}, missingMethod, false},
		{"complete run with -output json", func(t *testing.T) []string {
			return []string{"-fake", "-output", "json", "-c", writeConfig(t, closedPort(t), checkMethod, plaintext)}
		}, checkMethod, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var tried atomic.Bool
			writeJSONFile = func(string, []byte) error {
				tried.Store(true)

				return boom
			}
			t.Cleanup(func() { writeJSONFile = func(string, []byte) error { return nil } })

			path := newJSONPath(t)
			res := runCLI(t.Context(), t, 10*time.Second, append([]string{"-json-file", path}, c.args(t)...)...)

			if !tried.Load() {
				t.Fatalf("the file was never written\nerr %v\nstderr:\n%s", res.err, res.stderr)
			}
			if res.err == nil || exitCode(res.err) != 1 || errors.Is(res.err, ErrInvalidRun) || errors.Is(res.err, ErrIncomplete) {
				t.Fatalf("err = %v, want exit code 1: the report asked for was not written", res.err)
			}
			if !strings.Contains(res.err.Error(), path) || !strings.Contains(res.err.Error(), boom.Error()) {
				t.Errorf("err = %v, want the path and the write error", res.err)
			}
			if !strings.Contains(res.stdout, c.want) {
				t.Errorf("stdout lacks the report that was printed:\n%s", res.stdout)
			}
			if c.wantRaw {
				if got := decodeOnly(t, res.stdout)["outcome"]; got != "complete" {
					t.Errorf("stdout outcome = %v, want complete: stdout keeps the run's own verdict", got)
				}
			}
		})
	}
}
