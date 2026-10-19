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

package config

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/yhgrwav/leettest/pkg/threshold"
)

// maxLatencyLimit is the longest latency the histogram keeps; a limit must be
// below it, since longer values are cut to it.
const maxLatencyLimit = time.Hour

// minP999Calls is how many observations p99.9 is printed from (#267).
const minP999Calls = 1000

// percentText is a share as written: up to three digits before and after the
// point, and the sign.
var percentText = regexp.MustCompile(`^(\d{1,3})(?:\.(\d{1,3}))?%$`)

// maxPercent is 100% in thousandths of a percent.
const maxPercent = 100 * 1000

var latencyKeys = []threshold.Key{threshold.P50, threshold.P90, threshold.P95, threshold.P99, threshold.P999}

func passErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrThreshold, fmt.Sprintf(format, args...))
}

// resolvePass reads the pass sections into the limits the run is judged by.
// It runs after Validate, so the rates, durations and timeouts it compares
// with are valid.
func (m *MasterConfig) resolvePass() error {
	var errs []error

	search := m.Load.Breakpoint != nil

	for i := range m.Load.Calls {
		call := &m.Load.Calls[i]
		if call.RawPass == nil {
			continue
		}
		if search {
			errs = append(errs, fmt.Errorf("%s: %w", call.where(i),
				passErr("pass with load.breakpoint: a search has no single p99, use breakpoint.pass.held_rps")))

			continue
		}

		limits, err := call.resolvePass(m.Load.Warmup)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", call.where(i), err))

			continue
		}
		call.Pass = limits
	}

	if b := m.Load.Breakpoint; b != nil && b.RawPass != nil {
		limit, err := b.resolvePass(m.Load.Calls[0].Timeout)
		if err != nil {
			errs = append(errs, err)
		} else {
			b.Pass = limit
		}
	}

	return errors.Join(errs...)
}

func sortedKeys(raw map[string]any) []string {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

func (c *Call) resolvePass(warmup time.Duration) (threshold.Limits, error) {
	var (
		limits threshold.Limits
		errs   []error
	)

	raw := map[string]any(*c.RawPass)
	if len(raw) == 0 {
		return limits, passErr("pass has no limits: set p50, p90, p95, p99, p999 or errors, or leave pass out")
	}

	for _, key := range sortedKeys(raw) {
		if key != string(threshold.Errors) && !slices.Contains(latencyKeys, threshold.Key(key)) {
			errs = append(errs, passErr("pass.%s is not a limit; the keys are p50, p90, p95, p99, p999 and errors", key))
		}
	}
	if len(errs) > 0 {
		return limits, errors.Join(errs...)
	}

	dest := map[threshold.Key]*time.Duration{
		threshold.P50: &limits.P50, threshold.P90: &limits.P90, threshold.P95: &limits.P95,
		threshold.P99: &limits.P99, threshold.P999: &limits.P999,
	}
	for _, key := range latencyKeys {
		v, set := raw[string(key)]
		if !set {
			continue
		}

		d, err := c.latencyLimit(key, v, warmup)
		if err != nil {
			errs = append(errs, err)

			continue
		}
		*dest[key] = d
	}

	if v, set := raw[string(threshold.Errors)]; set {
		p, err := percentLimit(v)
		if err != nil {
			errs = append(errs, err)
		} else {
			limits.Errors = &p
		}
	} else {
		for _, key := range latencyKeys {
			if _, has := raw[string(key)]; has {
				errs = append(errs, passErr("pass.%s needs pass.errors next to it: a target that rejects calls fast passes a latency limit", key))
			}
		}
	}

	return limits, errors.Join(errs...)
}

func (c *Call) latencyLimit(key threshold.Key, v any, warmup time.Duration) (time.Duration, error) {
	text, ok := v.(string)
	if !ok {
		return 0, passErr("pass.%s is %v, want a duration such as 200ms", key, v)
	}

	d, err := time.ParseDuration(text)
	switch {
	case err != nil:
		return 0, passErr("pass.%s %q is not a duration such as 200ms", key, text)
	case d <= 0:
		return 0, passErr("pass.%s %s is not above zero", key, text)
	case d%time.Microsecond != 0:
		return 0, passErr("pass.%s %s is not whole microseconds: the report prints microseconds, a finer limit cannot be read from it", key, text)
	case d >= c.Timeout:
		return 0, passErr("pass.%s %s is not below the call's timeout %s: a call that timed out is known only as at least that, so the limit would check nothing",
			key, text, c.Timeout)
	case d >= maxLatencyLimit:
		return 0, passErr("pass.%s %s is not below %s: the histogram keeps no longer latency", key, text, maxLatencyLimit)
	}

	if key == threshold.P999 {
		// ceil(1000s / rps) in whole nanoseconds: rps x duration would
		// overflow int64 for a long run at a high rate.
		rps := time.Duration(c.RPS)
		need := (minP999Calls*time.Second + rps - 1) / rps
		if c.Duration-warmup < need {
			return 0, passErr("pass.p999 needs %d calls measured: at %d rps that is %s after the warm-up, the call runs %s",
				minP999Calls, c.RPS, need, c.Duration-warmup)
		}
	}

	return d, nil
}

func percentLimit(v any) (threshold.Percent, error) {
	text, ok := v.(string)
	groups := percentText.FindStringSubmatch(text)
	if !ok || groups == nil {
		return threshold.Percent{}, passErr("pass.errors is %v, want a percent such as 1%% or 0.5%%: up to 3 digits after the point, at most 100%%; a number without %% is not guessed", v)
	}

	whole, _ := strconv.Atoi(groups[1])
	fraction, _ := strconv.Atoi((groups[2] + "000")[:3])

	thousandths := int64(whole)*1000 + int64(fraction)
	if thousandths > maxPercent {
		return threshold.Percent{}, passErr("pass.errors %s is over 100%%", text)
	}

	return threshold.Percent{Thousandths: thousandths, Text: text}, nil
}

func (b *Breakpoint) resolvePass(timeout time.Duration) (threshold.SearchLimit, error) {
	raw := map[string]any(*b.RawPass)

	var errs []error

	for _, key := range sortedKeys(raw) {
		if key != "held_rps" {
			errs = append(errs, passErr("breakpoint.pass.%s is not a limit of a search; the only one is held_rps", key))
		}
	}
	if len(errs) > 0 {
		return threshold.SearchLimit{}, errors.Join(errs...)
	}

	v, set := raw["held_rps"]
	if !set {
		return threshold.SearchLimit{}, passErr("breakpoint.pass has no held_rps: set it or leave pass out")
	}

	held, err := wholeRate(v)
	if err != nil {
		return threshold.SearchLimit{}, err
	}

	rates, err := b.Plan(timeout, 0).Rates()
	if err != nil {
		return threshold.SearchLimit{}, fmt.Errorf("%w: breakpoint.pass.held_rps: %w", ErrBreakpoint, err)
	}

	below, above := 0, 0

	for _, rate := range rates {
		switch {
		case rate == held:
			return threshold.SearchLimit{HeldRPS: held}, nil
		case rate < held:
			below = rate
		case above == 0:
			above = rate
		}
	}

	switch {
	case below == 0:
		return threshold.SearchLimit{}, passErr("breakpoint.pass.held_rps %d is not a step of the plan: %d is the first", held, above)
	case above == 0:
		return threshold.SearchLimit{}, passErr("breakpoint.pass.held_rps %d is not a step of the plan: %d is the last", held, below)
	default:
		return threshold.SearchLimit{}, passErr("breakpoint.pass.held_rps %d is not a step of the plan: %d or %d", held, below, above)
	}
}

// wholeRate reads held_rps: a whole number as YAML wrote it, never a float or
// a string.
func wholeRate(v any) (int, error) {
	var n int64

	switch x := v.(type) {
	case int:
		n = int64(x)
	case int64:
		n = x
	case uint64:
		if x > math.MaxInt32 {
			return 0, passErr("breakpoint.pass.held_rps %d is too large", x)
		}

		n = int64(x)
	default:
		return 0, passErr("breakpoint.pass.held_rps is %v, want a whole number of requests per second", v)
	}

	if n < 1 || n > math.MaxInt32 {
		return 0, passErr("breakpoint.pass.held_rps %d is not a positive whole number of requests per second", n)
	}

	return int(n), nil
}
