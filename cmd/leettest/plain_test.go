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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
)

var errSetupAsked = errors.New("the first-run setup was asked")

// onATerminal makes run take its stderr for a terminal and records the asks
// for the first-run setup, which fail with errSetupAsked. The settings live in
// a fresh directory, so the run is unconfigured. Without -plain such a run
// reaches the setup and stops there, which keeps a test from entering the live
// screen.
func onATerminal(t *testing.T) (asked *int, settingsFile string) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	settingsFile = filepath.Join(dir, "leettest", "settings.yaml")

	savedTerminal, savedSetup := stderrIsTerminal, runSetup
	t.Cleanup(func() { stderrIsTerminal, runSetup = savedTerminal, savedSetup })

	asked = new(int)
	stderrIsTerminal = func(io.Writer) bool { return true }
	runSetup = func(*cli.Settings) error {
		*asked++

		return errSetupAsked
	}

	return asked, settingsFile
}

// runOnATerminal is run with fresh writers and a limit.
func runOnATerminal(t *testing.T, limit time.Duration, args ...string) result {
	t.Helper()

	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- run(t.Context(), nil, nil, args, &stdout, &stderr) }()

	select {
	case err := <-done:
		return result{stdout: stdout.String(), stderr: stderr.String(), err: err}
	case <-time.After(limit):
		t.Fatalf("run has not returned after %v", limit)

		return result{}
	}
}

// writeLongerRun is the config of a run of 1500ms, long enough for one
// progress line.
func writeLongerRun(t *testing.T) string {
	t.Helper()

	path := writeConfig(t, closedPort(t), checkMethod, plaintext)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	longer := strings.Replace(string(raw), "duration: 300ms", "duration: 1500ms", 1)
	if longer == string(raw) {
		t.Fatal("the config has no duration to lengthen")
	}
	if err := os.WriteFile(path, []byte(longer), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// -plain is the non-terminal path on demand: the screen is drawn only on a
// terminal without the flag.
func TestRun_PlainForcesTheProgressLines(t *testing.T) {
	t.Run("the view is live only on a terminal without the flag", func(t *testing.T) {
		for _, c := range []struct {
			plain, terminal, live bool
		}{
			{false, true, true},
			{true, true, false},
			{false, false, false},
			{true, false, false},
		} {
			if got := liveView(c.plain, c.terminal); got != c.live {
				t.Errorf("liveView(plain=%v, terminal=%v) = %v, want %v", c.plain, c.terminal, got, c.live)
			}
		}
	})

	t.Run("a run on a terminal prints running against and a line a second", func(t *testing.T) {
		onATerminal(t)

		res := runOnATerminal(t, 20*time.Second, "-plain", "-fake", "-c", writeLongerRun(t))
		if res.err != nil {
			t.Fatalf("run: %v\n%s", res.err, res.stderr)
		}
		if !strings.Contains(res.stderr, "running against ") {
			t.Errorf("stderr lacks the running against line:\n%s", res.stderr)
		}
		if !strings.Contains(res.stderr, "in-flight") {
			t.Errorf("stderr has no progress line:\n%s", res.stderr)
		}
		if strings.Contains(res.stdout, "running against") || strings.Contains(res.stdout, "in-flight") {
			t.Errorf("stdout carries progress:\n%s", res.stdout)
		}
		if header, _, _ := strings.Cut(res.stdout, "\n"); !strings.Contains(strings.ToLower(header), "fake") {
			t.Errorf("stdout is not the usual report, header %q:\n%s", header, res.stdout)
		}
		if strings.Contains(res.stderr, "\x1b[") {
			t.Errorf("stderr carries escape sequences:\n%q", res.stderr)
		}
	})

	t.Run("with -output json stdout is the one JSON object", func(t *testing.T) {
		onATerminal(t)

		res := runOnATerminal(t, 20*time.Second, "-plain", "-fake", "-output", "json", "-c", writeLongerRun(t))
		if res.err != nil {
			t.Fatalf("run: %v\n%s", res.err, res.stderr)
		}
		decodeOnly(t, res.stdout)
		if !strings.Contains(res.stderr, "in-flight") {
			t.Errorf("stderr has no progress line:\n%s", res.stderr)
		}
	})
}

// -plain needs no screen settings: the defaults stay in memory, as for a
// stderr that is not a terminal, and nothing is written.
func TestRun_PlainSkipsTheSetup(t *testing.T) {
	for _, kind := range []struct {
		name string
		cfg  func(t *testing.T) string
	}{
		{"a run", writeLongerRun},
		{"a search", func(t *testing.T) string {
			return writeSearchOf(t, closedPort(t), quickSearch, "200ms", checkMethod)
		}},
	} {
		t.Run(kind.name, func(t *testing.T) {
			asked, settingsFile := onATerminal(t)

			res := runOnATerminal(t, 60*time.Second, "-plain", "-fake", "-c", kind.cfg(t))
			if res.err != nil {
				t.Fatalf("run: %v\n%s", res.err, res.stderr)
			}
			if *asked != 0 {
				t.Errorf("the setup was asked %d times with -plain, want none", *asked)
			}
			if _, err := os.Stat(settingsFile); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the settings file after -plain: stat error %v, want it not written", err)
			}
		})

		// Without the flag the same terminal does ask: the seam is what the
		// two cases above are about.
		t.Run(kind.name+" without the flag asks", func(t *testing.T) {
			asked, _ := onATerminal(t)

			res := runOnATerminal(t, 60*time.Second, "-fake", "-c", kind.cfg(t))
			if !errors.Is(res.err, errSetupAsked) {
				t.Errorf("run error %v, want the setup to be asked on a terminal without -plain", res.err)
			}
			if *asked != 1 {
				t.Errorf("the setup was asked %d times, want once", *asked)
			}
		})
	}
}

// -plain is a flag of any run, the fake target's included.
func TestRun_PlainIsNotAFakeOnlyFlag(t *testing.T) {
	res := runCLI(t.Context(), t, 10*time.Second,
		"-plain", "-c", writeConfig(t, closedPort(t), checkMethod, plaintext))
	if res.err != nil && strings.Contains(res.err.Error(), "-plain") {
		t.Errorf("run: %v, want -plain accepted without -fake", res.err)
	}
}
