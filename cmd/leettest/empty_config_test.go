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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runConfigFile runs `leettest -c <file>` and prints the error the way main
// does. A panic is a failure of the test, not the end of the test binary.
func runConfigFile(t *testing.T, body string) (stdout, stderr string, code int) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	var out, errOut bytes.Buffer

	// run is called on this goroutine: runCLI's own goroutine cannot be
	// recovered from here, and a panic would end the whole test binary.
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("run with config %q panicked: %v", body, r)
				err = errPanicked
			}
		}()

		err = run(t.Context(), nil, nil, []string{"-c", path}, &out, &errOut)
	}()

	if err != nil {
		printError(&errOut, err)
	}

	return out.String(), errOut.String(), exitCode(err)
}

var errPanicked = errors.New("run panicked")

// A file the YAML library cannot decode is refused like a YAML syntax error:
// exit code 1, the reason on stderr, no stack trace.
func TestRun_UndecodableConfigFile(t *testing.T) {
	_, _, syntaxCode := runConfigFile(t, "app: [\n")
	if syntaxCode != 1 {
		t.Fatalf("a YAML syntax error exits %d, want 1: the reference", syntaxCode)
	}

	for name, body := range map[string]string{
		"the fuzz input": "00A:00000000:\n    0A: 00000000A\n0000000A: 10000\nload:\n  00000A: 0A\n  calls:    ! ",
		"shortest known": "load:\n  calls: !\n",
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, code := runConfigFile(t, body)

			if !strings.Contains(stderr, "the YAML library could not decode this file") {
				t.Errorf("stderr = %q, want the reason", stderr)
			}
			if strings.Contains(stderr, "goroutine") {
				t.Errorf("stderr holds a stack trace:\n%s", stderr)
			}
			if code != syntaxCode {
				t.Errorf("exit code %d, want %d as for a YAML syntax error", code, syntaxCode)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want it empty", stdout)
			}
		})
	}
}

// A config file with no document in it is refused like a YAML syntax error:
// the same exit code, the reason on stderr, nothing on stdout, no stack trace.
func TestRun_EmptyConfigFile(t *testing.T) {
	_, _, syntaxCode := runConfigFile(t, "app: [\n")
	if syntaxCode != 1 {
		t.Fatalf("a YAML syntax error exits %d, want 1: the reference for an empty file", syntaxCode)
	}

	for name, body := range map[string]string{
		"nothing":         "",
		"a newline":       "\n",
		"spaces":          "   ",
		"only a comment":  "# only a comment\n",
		"null":            "null\n",
		"a tab":           "\t",
		"document marker": "---\n",
	} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, code := runConfigFile(t, body)

			if !strings.Contains(stderr, "config is empty") {
				t.Errorf("stderr = %q, want it to say the config is empty", stderr)
			}
			if strings.Contains(stderr, "goroutine") {
				t.Errorf("stderr holds a stack trace:\n%s", stderr)
			}
			if code != syntaxCode {
				t.Errorf("exit code %d, want %d as for a YAML syntax error", code, syntaxCode)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want it empty", stdout)
			}
		})
	}
}
