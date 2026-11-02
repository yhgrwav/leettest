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

package measure

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/test/stand"
)

// The whole way from a generator that started its calls late to the line the
// user reads: the real pool, sender and stand hand every call over late, the
// stats of the run are read through the window the CLI's ticker reads, and the
// watch prints its warning on the writer standing for stderr, once, however
// many ticks look at the window. The same run ends invalid for generator_late,
// so the warning names what the report then says.
//
// Ground: signal — the warning must come from calls a real generator
// started late, not from a window built by hand.
func TestEarlyWarning_ALateStarterIsWarnedOfOnce(t *testing.T) {
	target := stand.Start(stand.Constant(time.Millisecond))
	t.Cleanup(target.Stop)

	const late = 150 * time.Millisecond

	stats, start, results := lateStarterStats(t, target, lateCalls, late)
	report := stats.Report()
	checkLateStarter(t, report, results, late)

	if !cli.GeneratorLate(cli.RunReport{Report: report}) {
		t.Fatal("GeneratorLate = false: the run is not the late one the warning is tested against")
	}

	// The calls are scheduled over the first second; two seconds in, the window
	// is seconds 0 and 1 of the run.
	now := start.Add(2500 * time.Millisecond)

	window := stats.LagWindow(now, cli.LagWindowSeconds)
	if window.Starts != lateCalls || window.Late != lateCalls || window.Warmup || !window.Behind() {
		t.Fatalf("window = %+v, want %d starts, all %d late, past no warm-up, and behind", window, lateCalls, lateCalls)
	}

	var stderr bytes.Buffer

	watch := cli.NewLagWatch(stats)
	for i := range 3 {
		if line, ok := watch.Tick(now.Add(time.Duration(i) * time.Second)); ok {
			fmt.Fprintln(&stderr, line)
		}
	}

	out := stderr.String()
	if n := strings.Count(out, "warning: the generator is behind its schedule"); n != 1 {
		t.Fatalf("stderr has the warning %d times over three ticks, want once:\n%s", n, out)
	}

	const want = "200 of 200 calls in the last 2 s (at 0:02) started more than 1.00ms late"
	if !strings.Contains(out, want) {
		t.Errorf("stderr lacks %q:\n%s", want, out)
	}

	if !strings.Contains(out, "(generator_late)") {
		t.Errorf("stderr does not name generator_late:\n%s", out)
	}
}

// The same pool and stand, every call on time: the window is not behind and
// the watch prints nothing. It guards the warning against firing on a
// generator that kept up; the window it reads must hold the run's calls, which
// the empty stub does not, so it is red until the counters exist.
//
// Ground: signal — the false alarm the 1% rule exists to prevent.
func TestEarlyWarning_AnOnTimeStarterIsNotWarnedOf(t *testing.T) {
	target := stand.Start(stand.Constant(time.Millisecond))
	t.Cleanup(target.Stop)

	stats, start, results := lateStarterStats(t, target, lateCalls, 0)
	checkLateStarter(t, stats.Report(), results, 0)

	now := start.Add(2500 * time.Millisecond)

	window := stats.LagWindow(now, cli.LagWindowSeconds)
	if window.Starts != lateCalls {
		t.Fatalf("window has %d starts, want %d: the premise of the test is a window of the run's calls", window.Starts, lateCalls)
	}

	if window.Behind() {
		t.Errorf("window = %+v is behind for a run of on-time starts", window)
	}

	if line, ok := cli.NewLagWatch(stats).Tick(now); ok || line != "" {
		t.Errorf("watch fired on an on-time run: %q", line)
	}
}
