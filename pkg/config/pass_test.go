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

package config_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
)

// passYAML is a plain run of one call whose rps, duration and extra lines are
// given, with the pass: section made of passLines (each indented 8 spaces by
// the caller's strings, one per line).
func passYAML(rps, duration, extra string, passLines ...string) []byte {
	var b strings.Builder
	b.WriteString("app:\n  target:\n    ip: localhost\n    port: 50051\nload:\n")
	b.WriteString(extra)
	b.WriteString("  calls:\n    - method: shop.v1.Catalog/Get\n")
	b.WriteString("      rps: " + rps + "\n      duration: " + duration + "\n")
	if len(passLines) > 0 {
		b.WriteString("      pass:\n")
		for _, l := range passLines {
			b.WriteString("        " + l + "\n")
		}
	}

	return []byte(b.String())
}

// withTimeout is a plain run of 100 rps for 60s with the call's timeout and a
// pass: of the given latency line beside errors: 1%.
func withTimeout(timeout, latency string) []byte {
	return []byte(strings.Replace(string(passYAML("100", "60s", "", latency, "errors: 1%")),
		"      rps:", "      timeout: "+timeout+"\n      rps:", 1))
}

// refuses fails the test unless Parse refuses cfg with ErrThreshold and a text
// that names every one of the words.
func refuses(t *testing.T, name string, cfg []byte, words ...string) {
	t.Helper()

	_, err := config.Parse(cfg)
	if err == nil {
		t.Errorf("%s: accepted, want an error naming %q", name, words)

		return
	}
	if !errors.Is(err, config.ErrThreshold) {
		t.Errorf("%s: %v, want ErrThreshold", name, err)
	}
	for _, w := range words {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("%s: %q does not name %q", name, err, w)
		}
	}
}

// Ground: contract — pkg/config is a library API: the YAML keys of pass become
// the limits the run is judged by, latencies in time and the share in
// thousandths of a percent.
func TestPass_KeysBecomeLimits(t *testing.T) {
	cfg, err := config.Parse(passYAML("100", "60s", "",
		"p50: 20ms", "p90: 50ms", "p95: 80ms", "p99: 200ms", "p999: 1s", "errors: 0.7%"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := cfg.Load.Calls[0].Pass
	if got.P50 != 20*time.Millisecond || got.P90 != 50*time.Millisecond || got.P95 != 80*time.Millisecond ||
		got.P99 != 200*time.Millisecond || got.P999 != time.Second {
		t.Errorf("latency limits %v %v %v %v %v, want 20ms 50ms 80ms 200ms 1s", got.P50, got.P90, got.P95, got.P99, got.P999)
	}
	if got.Errors == nil || got.Errors.Thousandths != 700 || got.Errors.Text != "0.7%" {
		t.Errorf("errors limit %+v, want 700 thousandths of a percent written 0.7%%", got.Errors)
	}
}

// Ground: contract — a call without pass: has no limits at all, so a run of it
// is judged by nothing.
func TestPass_LeftOutMeansNoLimits(t *testing.T) {
	cfg, err := config.Parse(passYAML("100", "60s", ""))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := cfg.Load.Calls[0]
	if got.RawPass != nil || got.Pass.P99 != 0 || got.Pass.Errors != nil {
		t.Errorf("pass %+v / %+v, want none", got.RawPass, got.Pass)
	}
}

// Ground: contract — errors is a percent string: N%, up to three digits after
// the point, from 0% to 100% (decisions on exact integer comparison). The
// accepted ones keep their exact value; everything else is refused before the
// run, by the key.
func TestPass_ErrorsPercentGrammar(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int64
	}{
		{"1%", 1000}, {"0%", 0}, {"100%", 100000}, {"0.125%", 125}, {"99.999%", 99999},
		{"0.7%", 700}, {"12.5%", 12500}, {"100.0%", 100000}, {"007%", 7000},
	} {
		cfg, err := config.Parse(passYAML("100", "60s", "", `errors: "`+tc.text+`"`))
		if err != nil {
			t.Errorf("%s: %v, want accepted", tc.text, err)

			continue
		}
		if got := cfg.Load.Calls[0].Pass.Errors; got == nil || got.Thousandths != tc.want || got.Text != tc.text {
			t.Errorf("%s: %+v, want %d thousandths written %s", tc.text, got, tc.want, tc.text)
		}
	}

	for _, text := range []string{
		"0.1255%", "100.001%", "101%", "1000%", "-1%", "+1%", "1e1%", "1E-1%", " 1%", "1% ", "1 %",
		"1", "0.01", ".5%", "1.%", "%", "", "1,5%", "0x10%", "NaN%", "1%%", "0.0001%",
	} {
		refuses(t, "errors "+text, passYAML("100", "60s", "", `errors: "`+text+`"`), "pass.errors")
	}

	// Unquoted, YAML reads these as numbers: a number without % is refused
	// whatever its value, because 0.01 is 1% or 0.01% and cannot be guessed.
	for _, text := range []string{"0.01", "1", "0"} {
		refuses(t, "errors "+text+" unquoted", passYAML("100", "60s", "", "errors: "+text), "pass.errors")
	}
}

// Ground: boundary — a latency limit is whole microseconds, the unit the JSON
// and the text report print; a finer one cannot be compared to what is printed.
func TestPass_LatencyIsWholeMicroseconds(t *testing.T) {
	for _, text := range []string{"1500ns", "200.0155ms", "1us500ns", "0.5us"} {
		refuses(t, "p99 "+text, passYAML("100", "60s", "", "p99: "+text, "errors: 1%"), "pass.p99")
	}
	for _, text := range []string{"0s", "-1ms"} {
		refuses(t, "p99 "+text, passYAML("100", "60s", "", "p99: "+text, "errors: 1%"), "pass.p99")
	}

	for _, tc := range []struct {
		text string
		want time.Duration
	}{
		{"1500us", 1500 * time.Microsecond}, {"1.5ms", 1500 * time.Microsecond}, {"200.015ms", 200015 * time.Microsecond},
		{"1us", time.Microsecond},
	} {
		cfg, err := config.Parse(passYAML("100", "60s", "", "p99: "+tc.text, "errors: 1%"))
		if err != nil {
			t.Errorf("p99 %s: %v, want accepted", tc.text, err)

			continue
		}
		if got := cfg.Load.Calls[0].Pass.P99; got != tc.want {
			t.Errorf("p99 %s = %v, want %v", tc.text, got, tc.want)
		}
	}
}

// Ground: contract — a key that is not a limit is refused, and the error lists
// the ones that are.
func TestPass_UnknownKeyListsTheAllowedOnes(t *testing.T) {
	_, err := config.Parse(passYAML("100", "60s", "", "p98: 1s", "errors: 1%"))
	if err == nil {
		t.Fatalf("accepted pass.p98")
	}
	for _, want := range []string{"p98", "p50", "p90", "p95", "p99", "p999", "errors"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not name %q", err, want)
		}
	}
}

// Ground: boundary — a target that rejects calls fast passes any latency
// limit (decisions: service time and failure time), so every latency key needs
// errors beside it; errors alone is fine.
func TestPass_LatencyNeedsErrorsNextToIt(t *testing.T) {
	for _, key := range []string{"p50", "p90", "p95", "p99", "p999"} {
		refuses(t, key+" alone", passYAML("100", "60s", "", key+": 200ms"), "pass."+key, "pass.errors")
	}

	if _, err := config.Parse(passYAML("100", "60s", "", "errors: 1%")); err != nil {
		t.Errorf("errors alone: %v, want accepted", err)
	}
	if _, err := config.Parse(passYAML("100", "60s", "", "p99: 200ms", "errors: 100%")); err != nil {
		t.Errorf("p99 beside errors: 100%%: %v, want accepted", err)
	}
}

// Ground: boundary — a call that timed out is known only as "at least the
// timeout", so a latency limit at or above it checks nothing; and a value
// above 1h is clamped by the histogram, so a limit must be below that too.
func TestPass_LatencyLimitIsBelowTheTimeoutAndAnHour(t *testing.T) {
	refuses(t, "above the default 2s", passYAML("100", "60s", "", "p99: 3s", "errors: 1%"), "pass.p99", "timeout")
	refuses(t, "at the timeout", withTimeout("500ms", "p99: 500ms"), "pass.p99", "timeout")
	refuses(t, "above a set timeout", withTimeout("500ms", "p50: 600ms"), "pass.p50", "timeout")

	if _, err := config.Parse(passYAML("100", "60s", "", "p99: 1999ms", "errors: 1%")); err != nil {
		t.Errorf("just under the timeout: %v, want accepted", err)
	}

	long := func(limit string) []byte { return withTimeout("3h", "p99: "+limit) }
	refuses(t, "an hour", long("1h"), "pass.p99")
	refuses(t, "two hours under a 3h timeout", long("2h"), "pass.p99")
	if _, err := config.Parse(long("59m59s")); err != nil {
		t.Errorf("just under an hour: %v, want accepted", err)
	}
}

// Ground: boundary — p99.9 is printed from 1000 observations (#267), and the
// warm-up is measured by nothing, so the call must run duration − warmup ≥
// ceil(1000s / rps). The check is on durations, not rps × duration, which
// overflows int64 at 1e7 rps for an hour.
func TestPass_P999NeedsAThousandCalls(t *testing.T) {
	for _, tc := range []struct {
		name, rps, duration, warmup string
		ok                          bool
	}{
		{"exactly 1000 calls", "10", "100s", "", true},
		{"one second short", "10", "99s", "", false},
		{"warm-up eats the margin", "20", "60s", "30s", false},
		{"warm-up leaves enough", "20", "80s", "30s", true},
		{"one rps", "1", "1000s", "", true},
		{"one rps, one second short", "1", "999s", "", false},
		{"fraction rounds up: 333s", "3", "333s", "", false},
		{"fraction rounds up: 334s", "3", "334s", "", true},
		{"rps 7, a nanosecond short", "7", "142857142857ns", "", false},
		{"rps 7, ceil exactly", "7", "142857142858ns", "", true},
		{"rps 1e7 for an hour does not overflow", "10000000", "1h", "", true},
		{"rps 1e7 for an hour, a long warm-up", "10000000", "1h", "30m", true},
	} {
		extra := ""
		if tc.warmup != "" {
			extra = "  warmup: " + tc.warmup + "\n"
		}
		cfg := passYAML(tc.rps, tc.duration, extra, "p999: 500ms", "errors: 1%")
		if tc.ok {
			if _, err := config.Parse(cfg); err != nil {
				t.Errorf("%s: %v, want accepted", tc.name, err)
			}

			continue
		}
		refuses(t, tc.name, cfg, "pass.p999")
	}

	// The other percentiles have no such floor.
	if _, err := config.Parse(passYAML("1", "1s", "", "p99: 500ms", "errors: 1%")); err != nil {
		t.Errorf("p99 on a one-call run: %v, want accepted", err)
	}
}

// Ground: boundary — a search has no single p99, so calls[].pass is refused in
// one and the error points to the search's own key.
func TestPass_ACallsPassInASearchIsRefused(t *testing.T) {
	cfg := strings.Replace(searchYAML, "      timeout: 500ms\n", "      timeout: 500ms\n      pass:\n        errors: 1%\n", 1)
	refuses(t, "calls[].pass in a search", []byte(cfg), "breakpoint.pass.held_rps")
}

func searchWithHeld(held string) []byte {
	return []byte(searchYAML + "    pass:\n      held_rps: " + held + "\n")
}

// Ground: contract — breakpoint.pass.held_rps is a step of the plan: the
// search never measures between steps, so 800 would be answered without a
// measurement. Plan from 100 to 2000 at the default factor 1.25: 100 125 156
// 195 244 305 381 476 595 744 930 1163 1454 1818.
func TestPass_HeldRPSIsAStepOfThePlan(t *testing.T) {
	for _, held := range []int{100, 744, 930, 1818} {
		cfg, err := config.Parse(searchWithHeld(strconv.Itoa(held)))
		if err != nil {
			t.Errorf("held_rps %d: %v, want accepted", held, err)

			continue
		}
		if got := cfg.Load.Breakpoint.Pass.HeldRPS; got != held {
			t.Errorf("held_rps %d resolved as %d", held, got)
		}
	}

	refuses(t, "between steps", searchWithHeld("800"), "breakpoint.pass.held_rps", "744", "930")
	refuses(t, "above the last step, below to", searchWithHeld("1900"), "breakpoint.pass.held_rps", "1818")
	refuses(t, "above to", searchWithHeld("2100"), "breakpoint.pass.held_rps")
	refuses(t, "below from", searchWithHeld("50"), "breakpoint.pass.held_rps", "100")
	refuses(t, "zero", searchWithHeld("0"), "breakpoint.pass.held_rps")
	refuses(t, "negative", searchWithHeld("-5"), "breakpoint.pass.held_rps")

	// A whole number: not rounded to a step.
	if _, err := config.Parse(searchWithHeld("800.5")); err == nil || !strings.Contains(err.Error(), "held_rps") {
		t.Errorf("fractional held_rps: %v, want an error naming held_rps", err)
	}
}

// Ground: contract — breakpoint.pass has the one key held_rps; a latency or
// error limit there is refused with that key named.
func TestPass_BreakpointPassHasOnlyHeldRPS(t *testing.T) {
	for _, line := range []string{"p99: 200ms", "errors: 1%"} {
		_, err := config.Parse([]byte(searchYAML + "    pass:\n      " + line + "\n"))
		if err == nil || !strings.Contains(err.Error(), "held_rps") {
			t.Errorf("breakpoint.pass %s: %v, want an error naming held_rps", line, err)
		}
	}
}

// Ground: boundary — an empty pass: {} judges nothing, so it is refused in a
// call and in a search rather than read as "no limits".
func TestPass_AnEmptyPassIsRefused(t *testing.T) {
	refuses(t, "calls[].pass: {}", passYAML("100", "60s", "", "{}"), "pass has no limits")
	refuses(t, "breakpoint.pass: {}", []byte(searchYAML+"    pass: {}\n"), "breakpoint.pass", "held_rps")
}
