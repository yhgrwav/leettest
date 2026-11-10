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
	"encoding/json"
	"errors"
	"os"
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

	// The window needs 100 starts to judge the tail, and a second of this run
	// holds 50: three whole seconds is the least that lets the rule speak, so the
	// run lasts four and the real window is read on every tick.
	t.Run("the engine at 50 rps", func(t *testing.T) {
		savedEvery := lagTickEvery
		t.Cleanup(func() { lagTickEvery = savedEvery })

		lagTickEvery = 20 * time.Millisecond

		res := runCLI(t.Context(), t, 30*time.Second, "-fake", "-output", "json", "-c", writeRunOf(t, "4s"))
		skipIfTheRunShowsALateGenerator(t, res)

		if res.err != nil {
			t.Fatalf("run: %v\n%s", res.err, res.stderr)
		}

		if strings.Contains(res.stderr, "generator is behind") {
			t.Errorf("stderr has a warning on a 50 rps run:\n%s", res.stderr)
		}
	})
}

// skipIfTheRunShowsALateGenerator skips the test when the run's own JSON report
// shows a generator that did not keep the schedule, so the premise of a run with
// no lag did not hold (issue #406, a loaded runner). Two cases, nothing wider:
//   - the run ended invalid and every invalid_reasons entry is generator_late or
//     clock_step (starvedReasons), the right verdict on a starved generator;
//   - the run ended valid, stderr carries the warning and the report backs it:
//     the slowest start is over the wait floor. The warning needs at least two
//     starts over the floor, so a report that holds none contradicts it and the
//     test fails on the warning as before.
//
// Any other result, a run invalid for another reason or a stdout that is not a
// report included, returns and the asserts after it run.
func skipIfTheRunShowsALateGenerator(t skipper, res result) {
	t.Helper()

	if res.err != nil {
		if !errors.Is(res.err, ErrInvalidRun) {
			return
		}

		if reasons, ok := starvedReasons([]byte(res.stdout)); ok {
			t.Skipf("the generator was late, the run ended invalid (%s)", strings.Join(reasons, ","))
		}

		return
	}

	if !strings.Contains(res.stderr, "generator is behind") {
		return
	}

	var report map[string]any
	if json.Unmarshal([]byte(res.stdout), &report) != nil {
		return
	}

	if worst, floor, ok := startLagOverTheFloor(report); ok {
		t.Skipf("the generator was late, the run had no lag to keep silent about: slowest start %s over the wait floor %s",
			worst, floor)
	}
}

// startLagOverTheFloor reads the slowest start lag of a run's JSON report and
// the wait floor it is judged by, max(1 ms, 4 clock steps), and says whether
// the lag is over the floor. A report without the number says no.
func startLagOverTheFloor(report map[string]any) (worst, floor time.Duration, ok bool) {
	lag, _ := report["start_lag"].(map[string]any)
	slowest, _ := lag["max"].(map[string]any)

	ns, has := slowest["ns"].(float64)
	if !has {
		return 0, 0, false
	}

	step, _ := report["clock_step_ns"].(float64)
	worst = time.Duration(ns)
	floor = max(time.Millisecond, 4*time.Duration(step))

	return worst, floor, worst > floor
}

// The skip of the 50 rps subtest is as narrow as the warning: a slowest start
// strictly over max(1 ms, 4 clock steps) is lag; one at the floor, under it, or
// a report without the number is not, and the subtest then fails on the warning.
//
// Ground: contract — a wait counts from the floor (rule of the wait floor).
func TestStartLagOverTheFloor(t *testing.T) {
	report := func(maxNS, stepNS any) map[string]any {
		return map[string]any{
			"start_lag":     map[string]any{"max": map[string]any{"ns": maxNS}},
			"clock_step_ns": stepNS,
		}
	}

	for name, tc := range map[string]struct {
		report map[string]any
		want   bool
	}{
		"over the 1 ms floor":           {report(float64(1_000_001), float64(100)), true},
		"at the floor":                  {report(float64(1_000_000), float64(100)), false},
		"under the floor":               {report(float64(900_000), float64(100)), false},
		"over 1 ms, under 4 steps":      {report(float64(1_500_000), float64(500_000)), false},
		"over 4 steps":                  {report(float64(2_100_000), float64(500_000)), true},
		"no clock step, over the floor": {map[string]any{"start_lag": map[string]any{"max": map[string]any{"ns": float64(5_000_000)}}}, true},
		"no start lag":                  {map[string]any{"clock_step_ns": float64(100)}, false},
		"no number":                     {report(nil, float64(100)), false},
		"empty report":                  {map[string]any{}, false},
	} {
		if _, _, got := startLagOverTheFloor(tc.report); got != tc.want {
			t.Errorf("%s: over the floor = %v, want %v", name, got, tc.want)
		}
	}
}

// The skip is only for a run whose own report shows the generator late: invalid
// for starvation alone, or valid with the warning on stderr and a slowest start
// over the floor. Everything else is left to the subtest's asserts.
//
// Ground: contract — a verdict on the target, or a warning the report does not
// back, is not starvation.
func TestSkipIfTheRunShowsALateGenerator(t *testing.T) {
	invalid := func(reasons string) result {
		return result{stdout: string(reportWith(reasons)), err: errInvalidRun}
	}
	warned := func(stdout string) result {
		return result{stdout: stdout, stderr: lagFragment + ": 3 of 150 calls\n"}
	}
	lagReport := func(maxNS string) string {
		return `{"clock_step_ns":100,"start_lag":{"max":{"ns":` + maxNS + `}}}`
	}

	for name, tc := range map[string]struct {
		res  result
		want bool
	}{
		"invalid, generator late":         {invalid(`["generator_late"]`), true},
		"invalid, coarse clock":           {invalid(`["clock_step"]`), true},
		"invalid, late and in flight cap": {invalid(`["generator_late","in_flight_cap"]`), false},
		"invalid, nothing measured":       {invalid(`["nothing_measured"]`), false},
		"invalid, no reasons":             {invalid(`[]`), false},
		"invalid, stdout not a report":    {result{stdout: "not json", err: errInvalidRun}, false},
		"another error":                   {result{stdout: string(reportWith(`["generator_late"]`)), err: errors.New("dial: refused")}, false},
		"valid, warned, start over floor": {warned(lagReport("2000000")), true},
		"valid, warned, start at floor":   {warned(lagReport("1000000")), false},
		"valid, warned, no start lag":     {warned(`{"clock_step_ns":100}`), false},
		"valid, warned, stdout not JSON":  {warned("a text report"), false},
		"valid, silent, start over floor": {result{stdout: lagReport("2000000")}, false},
	} {
		r := &skipRecorder{name: "TestX"}

		skipIfTheRunShowsALateGenerator(r, tc.res)

		if r.skipped != tc.want {
			t.Errorf("%s: skipped = %v (%s), want %v", name, r.skipped, r.message, tc.want)
		}
	}
}

// writeRunOf is the config of the other runs with its stage lasting dur.
func writeRunOf(t *testing.T, dur string) string {
	t.Helper()

	path := writeConfig(t, closedPort(t), checkMethod, plaintext)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	longer := strings.Replace(string(raw), "duration: 300ms", "duration: "+dur, 1)
	if longer == string(raw) {
		t.Fatal("the config has no duration to change")
	}

	if err := os.WriteFile(path, []byte(longer), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// With a live view the line goes into the view when it fires and is repeated on
// stderr once the view is gone, once however many times it is asked.
//
// Ground: contract — the view takes the screen with it when it leaves, so the
// warning would be lost with it.
func TestLagNotice_LiveViewRepeatsTheLineOnceAfterTheViewExits(t *testing.T) {
	var (
		stderr strings.Builder
		sent   []string
	)

	n := &lagNotice{stderr: &stderr, send: func(line string) { sent = append(sent, line) }}

	n.deliver("warning: first")
	n.deliver("warning: second")

	if len(sent) != 1 || sent[0] != "warning: first" {
		t.Errorf("sent into the view = %q, want the first line once", sent)
	}

	if stderr.Len() != 0 {
		t.Errorf("stderr before the view exits = %q, want nothing: the view shows it", stderr.String())
	}

	n.afterView()
	n.afterView()

	if got, want := stderr.String(), "warning: first\n"; got != want {
		t.Errorf("stderr after the view exits = %q, want %q once", got, want)
	}
}

// A run the generator kept up in leaves nothing on stderr when the view exits.
//
// Ground: contract — silence on a clean run.
func TestLagNotice_NoWarningNothingAfterTheView(t *testing.T) {
	var stderr strings.Builder

	n := &lagNotice{stderr: &stderr, send: func(string) { t.Error("sent a line nobody delivered") }}
	n.afterView()

	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", stderr.String())
	}
}

// Without a view the line is one stderr line at once, and afterView adds none; a
// line that arrives after the view is gone goes to stderr and is not repeated.
//
// Ground: boundary — the order of the watch and the view's exit is not fixed.
func TestLagNotice_PlainAndLateDelivery(t *testing.T) {
	t.Run("no view", func(t *testing.T) {
		var stderr strings.Builder

		n := &lagNotice{stderr: &stderr}
		n.deliver("warning: a")
		n.deliver("warning: b")
		n.afterView()

		if got, want := stderr.String(), "warning: a\n"; got != want {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	})

	t.Run("delivered after the view exited", func(t *testing.T) {
		var stderr strings.Builder

		n := &lagNotice{stderr: &stderr, send: func(string) { t.Error("sent into a view that is gone") }}
		n.afterView()
		n.deliver("warning: late")
		n.afterView()

		if got, want := stderr.String(), "warning: late\n"; got != want {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	})
}
