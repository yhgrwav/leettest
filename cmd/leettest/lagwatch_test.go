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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/engine"
)

const lagFragment = "warning: the generator is behind its schedule"

// behindWindow is a window 2140 of 60000 calls over the floor in.
func behindWindow() engine.LagWindow {
	return engine.LagWindow{
		Seconds: 5, End: 8 * time.Second, Floor: time.Millisecond,
		Calls: 60000, Starts: 60000, Late: 2140, Scheduled: 12000,
	}
}

// lagScript is a source that answers by the number of the read, 1 for the
// first, and signals every read on reads once someone listens.
type lagScript struct {
	window func(read int) engine.LagWindow
	n      atomic.Int64
	reads  chan int
}

func newLagScript(window func(read int) engine.LagWindow) *lagScript {
	return &lagScript{window: window, reads: make(chan int, 1024)}
}

func (s *lagScript) LagWindow(time.Time, int) engine.LagWindow {
	n := int(s.n.Add(1))
	select {
	case s.reads <- n:
	default:
	}

	return s.window(n)
}

// seam replaces the window source and the tick of the warning for one test.
func seam(t *testing.T, src cli.LagWindowSource, every time.Duration) {
	t.Helper()

	savedSource, savedEvery := lagSource, lagTickEvery
	t.Cleanup(func() { lagSource, lagTickEvery = savedSource, savedEvery })

	lagSource = func(*engine.Engine) cli.LagWindowSource { return src }
	lagTickEvery = every
}

// awaitReads waits until the source has been read n times, ceiling 5 s.
func awaitReads(t *testing.T, s *lagScript, n int) {
	t.Helper()

	deadline := time.After(5 * time.Second)

	for {
		select {
		case got := <-s.reads:
			if got >= n {
				return
			}
		case <-deadline:
			t.Fatalf("source read %d times in 5 s, want %d", s.n.Load(), n)
		}
	}
}

// The watch hands over one line at the first window behind, and not another
// one however long the generator stays behind.
//
// Ground: concurrency — a ticker goroutine and the caller; the count of lines
// is read after stop, which waits for it.
func TestStartLagWatch_DeliversOnceAtTwoFirings(t *testing.T) {
	src := newLagScript(func(read int) engine.LagWindow {
		if read < 3 {
			return engine.LagWindow{Seconds: read}
		}

		return behindWindow()
	})

	var (
		mu    sync.Mutex
		lines []string
	)

	stop := startLagWatch(t.Context(), src, 5*time.Millisecond, func(line string) {
		mu.Lock()
		defer mu.Unlock()

		lines = append(lines, line)
	})

	awaitReads(t, src, 8)
	stop()

	mu.Lock()
	defer mu.Unlock()

	if len(lines) != 1 {
		t.Fatalf("watch delivered %d lines over %d reads, want 1: %q", len(lines), src.n.Load(), lines)
	}

	if want := cli.LagWarningLine(behindWindow()); lines[0] != want || want == "" {
		t.Errorf("line = %q, want the line of the window (%q)", lines[0], want)
	}
}

// A generator that kept up leaves the watch silent.
//
// Ground: contract — silence on a clean run.
func TestStartLagWatch_StaysSilentWhenTheGeneratorKeepsUp(t *testing.T) {
	src := newLagScript(func(int) engine.LagWindow {
		w := behindWindow()
		w.Late = 600

		return w
	})

	var delivered atomic.Int64

	stop := startLagWatch(t.Context(), src, 5*time.Millisecond, func(string) { delivered.Add(1) })

	awaitReads(t, src, 5)
	stop()

	if n := delivered.Load(); n != 0 {
		t.Errorf("watch delivered %d lines for a window at exactly 1%% late, want none", n)
	}
}

// With the source behind from the first tick, the run prints exactly one
// warning on stderr, with no terminal; stdout is the usual report.
//
// Ground: contract — the text-mode surface of the warning.
func TestRun_LagWarningIsOneStderrLineInTextMode(t *testing.T) {
	src := newLagScript(func(int) engine.LagWindow { return behindWindow() })
	seam(t, src, 20*time.Millisecond)

	res := runCLI(t.Context(), t, 20*time.Second, "-fake", "-c", writeLongerRun(t))
	if res.err != nil {
		t.Fatalf("run: %v\n%s", res.err, res.stderr)
	}

	if n := strings.Count(res.stderr, lagFragment); n != 1 {
		t.Errorf("stderr has the warning %d times, want once:\n%s", n, res.stderr)
	}

	if want := cli.LagWarningLine(behindWindow()); want == "" || !strings.Contains(res.stderr, want+"\n") {
		t.Errorf("stderr lacks the whole line %q:\n%s", want, res.stderr)
	}

	if strings.Contains(res.stdout, "generator is behind") {
		t.Errorf("stdout carries the warning, it is the report's:\n%s", res.stdout)
	}

	if header, _, _ := strings.Cut(res.stdout, "\n"); !strings.Contains(strings.ToLower(header), "fake") {
		t.Errorf("stdout is not the usual report, header %q:\n%s", header, res.stdout)
	}
}

// In JSON mode stdout is the one JSON object and the warning is on stderr.
//
// Ground: contract — stdout stays machine-readable.
func TestRun_LagWarningLeavesJSONStdoutAlone(t *testing.T) {
	src := newLagScript(func(int) engine.LagWindow { return behindWindow() })
	seam(t, src, 20*time.Millisecond)

	res := runCLI(t.Context(), t, 20*time.Second, "-fake", "-output", "json", "-c", writeLongerRun(t))
	if res.err != nil {
		t.Fatalf("run: %v\n%s", res.err, res.stderr)
	}

	decodeOnly(t, res.stdout)

	if strings.Contains(res.stdout, "generator is behind") {
		t.Errorf("stdout carries the warning:\n%s", res.stdout)
	}

	if n := strings.Count(res.stderr, lagFragment); n != 1 {
		t.Errorf("stderr has the warning %d times, want once:\n%s", n, res.stderr)
	}
}

// A run whose windows are all clean prints no warning, and a real low-rate
// run against the fake sender, read through the engine itself, none either.
//
// Ground: contract — silence where the generator kept up.
func TestRun_NoLagNoWarning(t *testing.T) {
	t.Run("a scripted clean source", func(t *testing.T) {
		src := newLagScript(func(int) engine.LagWindow {
			return engine.LagWindow{Seconds: 5, Floor: time.Millisecond, Calls: 60000, Starts: 60000}
		})
		seam(t, src, 20*time.Millisecond)

		res := runCLI(t.Context(), t, 20*time.Second, "-fake", "-c", writeLongerRun(t))
		if res.err != nil {
			t.Fatalf("run: %v\n%s", res.err, res.stderr)
		}

		if src.n.Load() < 2 {
			t.Fatalf("the source was read %d times; the premise needs the watch to look at least twice", src.n.Load())
		}

		if strings.Contains(res.stderr, "generator is behind") {
			t.Errorf("stderr has a warning on a clean run:\n%s", res.stderr)
		}
	})

	t.Run("the engine at 50 rps", func(t *testing.T) {
		savedEvery := lagTickEvery
		t.Cleanup(func() { lagTickEvery = savedEvery })

		lagTickEvery = 20 * time.Millisecond

		res := runCLI(t.Context(), t, 20*time.Second, "-fake", "-c", writeLongerRun(t))
		if res.err != nil {
			t.Fatalf("run: %v\n%s", res.err, res.stderr)
		}

		if strings.Contains(res.stderr, "generator is behind") {
			t.Errorf("stderr has a warning on a 50 rps run:\n%s", res.stderr)
		}
	})
}
