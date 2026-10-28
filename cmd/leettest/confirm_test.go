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
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
)

const (
	askText    = "Start? [y/N]"
	needsYes   = "add -yes"
	notDoneErr = "not confirmed"
)

// readWatcher is an input that counts the reads made of it, so that a test can
// say «nothing was asked».
type readWatcher struct {
	r     io.Reader
	reads atomic.Int32
}

func (w *readWatcher) Read(p []byte) (int, error) {
	w.reads.Add(1)

	return w.r.Read(p)
}

// syncBuffer is a writer a test may read while the run still writes to it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.String()
}

// withTerminals makes run take stdin and stderr for terminals or not, and
// reads the answer from input. The returned watcher says whether anything was
// read from it.
func withTerminals(t *testing.T, stdinTerminal, stderrTerminal bool, input io.Reader) *readWatcher {
	t.Helper()

	savedErr, savedIn, savedInput := stderrIsTerminal, stdinIsTerminal, confirmInput
	t.Cleanup(func() { stderrIsTerminal, stdinIsTerminal, confirmInput = savedErr, savedIn, savedInput })

	watcher := &readWatcher{r: input}
	stderrIsTerminal = func(io.Writer) bool { return stderrTerminal }
	stdinIsTerminal = func() bool { return stdinTerminal }
	confirmInput = watcher

	return watcher
}

// countingTarget accepts TCP connections and counts them: a run that was
// refused, or not confirmed, must not have touched the target at all, the
// connection and the reflection call included.
func countingTarget(t *testing.T) (addr string, seen func() int) {
	t.Helper()

	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	var n atomic.Int32

	go func() {
		for {
			conn, acceptErr := lis.Accept()
			if acceptErr != nil {
				return
			}
			n.Add(1)
			_ = conn.Close()
		}
	}()

	return lis.Addr().String(), func() int { return int(n.Load()) }
}

const aShortCall = "    - method: " + checkMethod + "\n      rps: 20\n      duration: 1s\n"

// realConfig is a real-mode config of the target with the ramp and the stop
// that real needs, so that only the confirmation stands between it and a run.
func realConfig(t *testing.T, addr, calls string) string {
	t.Helper()

	cfg := dryConfig(t, addr, "  mode: real\n", calls)
	setRamp(t, cfg, "1s")
	setStop(t, cfg, "5%")

	return cfg
}

// Ground: contract — on a terminal (stdin and stderr both) a real run prints
// its plan and asks, and starts on y or yes in any case. The question goes to
// stderr, so the report on stdout is the report alone, in JSON as in text. The
// target of a -fake run is the fake one: -fake changes where the calls go, not
// what the config means, so it asks as well.
func TestRun_ARealRunAsksOnATerminalAndStartsOnYes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
		extra  []string
	}{
		{"y, text", "y\n", nil},
		{"Yes, json", "Yes\r\n", []string{"-output", "json"}},
	} {
		addr, _ := countingTarget(t)
		cfg := realConfig(t, addr, aShortCall)
		in := withTerminals(t, true, true, strings.NewReader(tc.answer))

		res := runCLI(t.Context(), t, 30*time.Second, append([]string{"-fake", "-plain", "-c", cfg}, tc.extra...)...)

		if res.err != nil {
			t.Fatalf("%s: run: %v\nstderr:\n%s", tc.name, res.err, res.stderr)
		}
		if in.reads.Load() == 0 {
			t.Errorf("%s: the answer was never read", tc.name)
		}
		const plan = "real mode: fake target, planned peak 20 rps, planned duration 2s, ramp 1s included"
		if !strings.Contains(res.stderr, plan) || !strings.Contains(res.stderr, askText) {
			t.Errorf("%s: stderr lacks the plan %q and %q:\n%s", tc.name, plan, askText, res.stderr)
		}
		if strings.Contains(res.stdout, askText) || strings.Contains(res.stdout, "real mode:") {
			t.Errorf("%s: the question is on stdout, which is the report's:\n%s", tc.name, res.stdout)
		}
		if len(tc.extra) > 0 {
			if got := jsonAt(t, decodeOnly(t, res.stdout), "settings", "mode"); got != "real" {
				t.Errorf("%s: settings.mode = %v, want real", tc.name, got)
			}
		} else if !strings.Contains(res.stdout, "mode: real") {
			t.Errorf("%s: no report on stdout:\n%s", tc.name, res.stdout)
		}
	}
}

// Ground: contract — anything but y or yes, the end of input included, ends the
// command with exit 1 and a message that nothing was sent: no report, no
// connection to the target, no reflection call, and the file asked for with
// -json-file is not left behind.
func TestRun_NotConfirmedSendsNothing(t *testing.T) {
	for _, answer := range []string{"n\n", "\n", "", "yes please\n", "ye\n"} {
		addr, seen := countingTarget(t)
		cfg := realConfig(t, addr, aShortCall)
		withTerminals(t, true, true, strings.NewReader(answer))
		path := newJSONPath(t)

		res := runCLI(t.Context(), t, 10*time.Second, "-plain", "-c", cfg, "-json-file", path)

		wantExit(t, res, 1)
		if all := fmt.Sprint(res.err) + res.stderr; !strings.Contains(all, notDoneErr) || !strings.Contains(all, "nothing was sent") {
			t.Errorf("%q: no %q in:\n%s", answer, notDoneErr, all)
		}
		if !strings.Contains(res.stderr, askText) {
			t.Errorf("%q: the question was not asked:\n%s", answer, res.stderr)
		}
		if res.stdout != "" {
			t.Errorf("%q: stdout is not empty, a refused run has no report:\n%s", answer, res.stdout)
		}
		if n := seen(); n != 0 {
			t.Errorf("%q: the target saw %d connections from a run that was not confirmed", answer, n)
		}
		if !fileMissing(t, path) {
			t.Errorf("%q: the JSON file is left behind", answer)
		}
	}
}

// Ground: contract — when stdin or stderr is not a terminal nobody can answer
// (the question goes to stderr, so a redirected stderr hides it and the run
// would hang): a real run without -yes is exit 1 with the words to add, before
// anything is read or sent. A script whose input holds a «y» is not an answer:
// the input is not read.
func TestRun_WithoutATerminalARealRunNeedsYes(t *testing.T) {
	for _, tc := range []struct {
		name          string
		stdin, stderr bool
	}{
		{"stdin is not a terminal", false, true},
		{"stderr is not a terminal", true, false},
		{"neither is", false, false},
	} {
		addr, seen := countingTarget(t)
		cfg := realConfig(t, addr, aShortCall)
		in := withTerminals(t, tc.stdin, tc.stderr, strings.NewReader("y\n"))

		res := runCLI(t.Context(), t, 10*time.Second, "-plain", "-c", cfg)

		wantExit(t, res, 1)
		if all := fmt.Sprint(res.err) + res.stderr; !strings.Contains(all, needsYes) {
			t.Errorf("%s: no %q in:\n%s", tc.name, needsYes, all)
		}
		if in.reads.Load() != 0 {
			t.Errorf("%s: the input was read; a script's input is not an answer", tc.name)
		}
		if res.stdout != "" {
			t.Errorf("%s: stdout is not empty:\n%s", tc.name, res.stdout)
		}
		if n := seen(); n != 0 {
			t.Errorf("%s: the target saw %d connections from a run that needs -yes", tc.name, n)
		}
	}
}

// Ground: contract — -yes starts a real run without a question, on a terminal
// or off it, and the plan still goes to stderr so that the log of the run
// says what was started. The input is not read.
func TestRun_YesStartsARealRunWithoutAsking(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		addr, _ := countingTarget(t)
		cfg := realConfig(t, addr, aShortCall)
		in := withTerminals(t, terminal, terminal, strings.NewReader("n\n"))

		res := runCLI(t.Context(), t, 30*time.Second, "-fake", "-plain", "-yes", "-c", cfg)

		if res.err != nil {
			t.Fatalf("terminal %v: run: %v\nstderr:\n%s", terminal, res.err, res.stderr)
		}
		if strings.Contains(res.stderr, askText) {
			t.Errorf("terminal %v: -yes asked:\n%s", terminal, res.stderr)
		}
		if in.reads.Load() != 0 {
			t.Errorf("terminal %v: -yes read the input", terminal)
		}
		const plan = "real mode: fake target, planned peak 20 rps, planned duration 2s, ramp 1s included"
		if !strings.Contains(res.stderr, plan) {
			t.Errorf("terminal %v: stderr lacks the plan %q:\n%s", terminal, plan, res.stderr)
		}
		if !strings.Contains(res.stdout, "mode: real") {
			t.Errorf("terminal %v: no report on stdout:\n%s", terminal, res.stdout)
		}
	}
}

// Ground: contract — the numbers under the question are the config's: the peak
// is the sum of the calls' rps (they run in parallel) and the duration is the
// ramp together with the longest call, and the target is the address as the
// config has it.
func TestRun_TheQuestionStatesThePlannedPeakAndDuration(t *testing.T) {
	addr, seen := countingTarget(t)
	calls := "    - method: " + checkMethod + "\n      rps: 100\n      duration: 2s\n" +
		"    - method: " + emptyCallMethod + "\n      rps: 50\n      duration: 3s\n"
	cfg := realConfig(t, addr, calls)
	withTerminals(t, true, true, strings.NewReader("n\n"))

	res := runCLI(t.Context(), t, 10*time.Second, "-plain", "-c", cfg)

	want := "real mode: " + addr + ", planned peak 150 rps, planned duration 4s, ramp 1s included"
	if !strings.Contains(res.stderr, want) {
		t.Errorf("stderr lacks %q:\n%s", want, res.stderr)
	}
	if n := seen(); n != 0 {
		t.Errorf("the target saw %d connections before the answer", n)
	}
}

// Ground: contract — a search in real is asked too, and its line gives the
// range and says it goes on until the system breaks: a search has no duration
// to promise.
func TestRun_ASearchInRealStatesItsRangeAndAsks(t *testing.T) {
	addr, seen := countingTarget(t)
	cfg := writeSearch(t, addr, checkMethod)
	setMode(t, cfg, "real")
	withTerminals(t, true, true, strings.NewReader("n\n"))

	res := runCLI(t.Context(), t, 10*time.Second, "-plain", "-c", cfg)

	wantExit(t, res, 1)
	want := "real mode: " + addr + ", search from 50 to 80 rps until it breaks, duration not promised"
	if !strings.Contains(res.stderr, want) || !strings.Contains(res.stderr, askText) {
		t.Errorf("stderr lacks %q and %q:\n%s", want, askText, res.stderr)
	}
	if strings.Contains(res.stderr, "planned") {
		t.Errorf("a search promised planned numbers:\n%s", res.stderr)
	}
	if n := seen(); n != 0 {
		t.Errorf("the target saw %d connections from a search that was not confirmed", n)
	}
}

// Ground: contract — a search in real needs -yes off a terminal like a run.
func TestRun_ASearchInRealNeedsYesOffATerminal(t *testing.T) {
	addr, seen := countingTarget(t)
	cfg := writeSearch(t, addr, checkMethod)
	setMode(t, cfg, "real")
	withTerminals(t, false, false, strings.NewReader(""))

	res := runCLI(t.Context(), t, 10*time.Second, "-plain", "-c", cfg)

	wantExit(t, res, 1)
	if all := fmt.Sprint(res.err) + res.stderr; !strings.Contains(all, needsYes) {
		t.Errorf("no %q in:\n%s", needsYes, all)
	}
	if n := seen(); n != 0 {
		t.Errorf("the target saw %d connections", n)
	}
}

// Ground: contract — -yes in a search starts it without a question and still
// prints the search's line, as a run does: the log says what range was started.
// The input is not read, on a terminal or off it.
func TestRun_YesInASearchPrintsItsLineAndDoesNotAsk(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		addr := closedPort(t)
		cfg := writeSearch(t, addr, checkMethod)
		setMode(t, cfg, "real")
		in := withTerminals(t, terminal, terminal, strings.NewReader("n\n"))

		res := runCLI(t.Context(), t, 60*time.Second, "-plain", "-yes", "-c", cfg)

		want := "real mode: " + addr + ", search from 50 to 80 rps until it breaks, duration not promised"
		if !strings.Contains(res.stderr, want) {
			t.Errorf("terminal %v: stderr lacks %q:\n%s", terminal, want, res.stderr)
		}
		if strings.Contains(res.stderr, askText) {
			t.Errorf("terminal %v: -yes asked:\n%s", terminal, res.stderr)
		}
		if in.reads.Load() != 0 {
			t.Errorf("terminal %v: -yes read the input", terminal)
		}
	}
}

// Ground: contract — the question is for real alone: a run in local, or with
// the mode left out, asks nothing on a terminal and never reads the input, with
// -yes or without it. (Green on the stub: guards the requirement that the
// implementation asks only in real.)
func TestRun_LocalNeverAsks(t *testing.T) {
	for _, mode := range []string{"", "  mode: local\n"} {
		cfg := writeEchoConfig(t, mode, "")
		in := withTerminals(t, true, true, strings.NewReader("n\n"))

		res := runCLI(t.Context(), t, 30*time.Second, "-fake", "-plain", "-c", cfg)

		if res.err != nil {
			t.Fatalf("mode %q: run: %v\nstderr:\n%s", mode, res.err, res.stderr)
		}
		if in.reads.Load() != 0 || strings.Contains(res.stderr, askText) || strings.Contains(res.stderr, "real mode:") {
			t.Errorf("mode %q: a local run asked or told of a real one (reads %d):\n%s", mode, in.reads.Load(), res.stderr)
		}
	}
}

// Ground: contract — a dry run in real never asks, on a terminal as off it:
// it sends one request per call. (Green on the stub: guards the requirement.)
func TestRun_DryRunInRealNeverAsksOnATerminal(t *testing.T) {
	for _, extra := range [][]string{nil, {"-yes"}} {
		target := startReflectingTarget(t)
		cfg := dryConfig(t, target.addr, "  mode: real\n", dryCall(unaryCallMethod, ""))
		setRamp(t, cfg, "30s")
		setStop(t, cfg, "5%")
		in := withTerminals(t, true, true, strings.NewReader("n\n"))

		res := runCLI(t.Context(), t, 10*time.Second, append([]string{"-dry-run", "-c", cfg}, extra...)...)

		if res.err != nil {
			t.Fatalf("%v: run: %v\nstderr:\n%s", extra, res.err, res.stderr)
		}
		if in.reads.Load() != 0 || strings.Contains(res.stdout+res.stderr, askText) {
			t.Errorf("%v: the dry run asked (reads %d):\n%s%s", extra, in.reads.Load(), res.stdout, res.stderr)
		}
	}
}

// Ground: contract — an unusable -json-file is refused before the question:
// the user is not asked to confirm a run that cannot start, and a «yes» is not
// wasted on it. The answer is not read. (Green on the stub: guards the place
// of the question among the checks.)
func TestRun_AnUnusableJSONFileIsRefusedBeforeTheQuestion(t *testing.T) {
	addr, seen := countingTarget(t)
	cfg := realConfig(t, addr, aShortCall)
	in := withTerminals(t, true, true, strings.NewReader("y\n"))

	taken := newJSONPath(t)
	if err := os.WriteFile(taken, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	res := runCLI(t.Context(), t, 10*time.Second, "-plain", "-c", cfg, "-json-file", taken)

	wantExit(t, res, 1)
	if !strings.Contains(fmt.Sprint(res.err), "-json-file") {
		t.Errorf("the error is not about the file: %v", res.err)
	}
	if in.reads.Load() != 0 || strings.Contains(res.stderr, askText) {
		t.Errorf("the question was asked of a run that cannot start (reads %d):\n%s", in.reads.Load(), res.stderr)
	}
	if n := seen(); n != 0 {
		t.Errorf("the target saw %d connections", n)
	}
}

// Ground: signal — Ctrl+C (the soft stop) or SIGTERM while the question waits
// ends the command the way a cancel before the run always did: exit 1 through
// the cancelled context, no report, nothing sent. The reader of the input
// never returns; the command must not wait for it.
func TestRun_ASignalAtTheQuestionEndsBeforeAnythingIsSent(t *testing.T) {
	for name, press := range map[string]func(stops, aborts chan<- struct{}){
		"ctrl+c":  func(stops, _ chan<- struct{}) { stops <- struct{}{} },
		"sigterm": func(_, aborts chan<- struct{}) { aborts <- struct{}{} },
	} {
		addr, seen := countingTarget(t)
		cfg := realConfig(t, addr, aShortCall)

		in, out := io.Pipe()
		t.Cleanup(func() { _ = out.Close() })
		withTerminals(t, true, true, in)

		dir := t.TempDir()
		t.Setenv("APPDATA", dir)
		t.Setenv("XDG_CONFIG_HOME", dir)

		var (
			stdout bytes.Buffer
			stderr syncBuffer
		)

		stops, aborts := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)

		go func() {
			done <- run(t.Context(), stops, aborts, []string{"-plain", "-c", cfg}, &stdout, &stderr)
		}()

		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(stderr.String(), askText) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: the question never appeared:\n%s", name, stderr.String())
			}
			time.Sleep(10 * time.Millisecond)
		}

		go press(stops, aborts)

		select {
		case err := <-done:
			if got := exitCode(err); got != 1 || !errors.Is(err, context.Canceled) {
				t.Errorf("%s: exit %d, err %v: want 1 through context.Canceled", name, got, err)
			}
			if err == nil || !strings.Contains(err.Error(), "nothing was sent") || errors.Is(err, cli.ErrNotConfirmed) {
				t.Errorf("%s: err = %v, want «nothing was sent» and not ErrNotConfirmed", name, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: the command is still waiting for an answer after the signal", name)
		}

		if stdout.Len() != 0 {
			t.Errorf("%s: stdout is not empty:\n%s", name, stdout.String())
		}
		if n := seen(); n != 0 {
			t.Errorf("%s: the target saw %d connections", name, n)
		}
	}
}
