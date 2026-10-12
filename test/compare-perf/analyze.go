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
	"bufio"
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// The definition of "held" is this harness's, not a tool's: the first headSkip
// of every run is left out for all tools alike (connecting, starting virtual
// users, dialling), and a point holds when the stand received at least
// heldShare of the planned calls of the rest and no steady second of it fell
// below steadyShare of the planned rate. Both shares are hypotheses until the
// final run shows they separate a tool that keeps up from one that does not.
const (
	heldShare   = 0.999
	steadyShare = 0.99
	// headSkip is the start of a run, counted from the first arrival at the
	// stand, that no judgement looks at; the skipped arrivals are printed.
	headSkip = 2 * time.Second
	// minDuration leaves one steady second between the skipped head and the
	// last second, which is skipped too.
	minDuration = 4 * time.Second
)

// allowanceFor is how long after the nominal end an arrival still counts as
// within the run: the share of the run that heldShare lets be missing anyway.
// Offsets run from the first arrival, which is later than the tool's own
// start, so a call on schedule has an offset below the duration and needs no
// more. A longer allowance would credit a tool that sends late calls after the
// end (LeetTest does, until a call's deadline) with capacity that ghz and k6,
// which stop issuing calls at the end, do not get.
func allowanceFor(dur time.Duration) time.Duration {
	return time.Duration((1 - heldShare) * float64(dur))
}

// readArrivals reads the stand's -arrivals file and sorts it: arrival order
// can differ from time order by microseconds.
func readArrivals(path string) ([]time.Duration, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var offs []time.Duration
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		ns, err := strconv.ParseInt(sc.Text(), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		offs = append(offs, time.Duration(ns))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	slices.Sort(offs)

	return offs, nil
}

// steady is the arrivals of the seconds that count, [headSkip, whole
// seconds−1), and how many seconds that is. The head holds the tools' different
// start-up, the last second their different stops.
func steady(offs []time.Duration, dur time.Duration) (window []time.Duration, seconds int) {
	whole := int(dur / time.Second)
	first := int(headSkip / time.Second)
	if whole < first+2 {
		return nil, 0
	}
	from, to := headSkip, time.Duration(whole-1)*time.Second
	for _, o := range offs {
		if o >= from && o < to {
			window = append(window, o)
		}
	}

	return window, whole - 1 - first
}

func perSecond(window []time.Duration, seconds int) []int {
	counts := make([]int, seconds)
	for _, o := range window {
		counts[int(o/time.Second)-int(headSkip/time.Second)]++
	}

	return counts
}

// verdict is row 1's judgement of one run. Planned and Arrived count the run
// after the skipped head.
type verdict struct {
	Planned, Arrived int
	// Head is the arrivals in the skipped first headSkip, of rate × headSkip
	// planned.
	Head int
	// Share is Arrived/Planned.
	Share float64
	// MinSecond is the fewest arrivals in a steady second, MinShare that over
	// the planned rate.
	MinSecond int
	MinShare  float64
	Held      bool
}

func judge(offs []time.Duration, rate int, dur time.Duration) verdict {
	v := verdict{Planned: int(float64(rate) * (dur - headSkip).Seconds())}
	for _, o := range offs {
		switch {
		case o < headSkip:
			v.Head++
		case o < dur+allowanceFor(dur):
			v.Arrived++
		}
	}
	v.Share = float64(v.Arrived) / float64(v.Planned)

	window, seconds := steady(offs, dur)
	if seconds > 0 {
		v.MinSecond = slices.Min(perSecond(window, seconds))
		v.MinShare = float64(v.MinSecond) / float64(rate)
	}
	v.Held = v.Share >= heldShare && seconds > 0 && v.MinShare >= steadyShare

	return v
}

// spacing is row 2's measure of one run: how far the arrivals at the stand are
// from an even schedule at the planned rate, over the steady seconds.
type spacing struct {
	// Ideal is the gap an even schedule has.
	Ideal                           time.Duration
	GapP50, GapP99, GapP999, GapMax time.Duration
	SecMin, SecMax                  int
	SecStdDev                       float64
	Gaps                            int
}

func measureSpacing(offs []time.Duration, rate int, dur time.Duration) (spacing, error) {
	window, seconds := steady(offs, dur)
	if len(window) < 2 {
		return spacing{}, fmt.Errorf("%d arrivals in the steady seconds, want at least 2", len(window))
	}

	gaps := make([]time.Duration, 0, len(window)-1)
	for i := 1; i < len(window); i++ {
		gaps = append(gaps, window[i]-window[i-1])
	}
	slices.Sort(gaps)

	counts := perSecond(window, seconds)
	var sum float64
	for _, c := range counts {
		sum += float64(c)
	}
	mean := sum / float64(len(counts))
	var sq float64
	for _, c := range counts {
		sq += (float64(c) - mean) * (float64(c) - mean)
	}

	return spacing{
		Ideal:     time.Second / time.Duration(rate),
		GapP50:    nearestRank(gaps, 0.50),
		GapP99:    nearestRank(gaps, 0.99),
		GapP999:   nearestRank(gaps, 0.999),
		GapMax:    gaps[len(gaps)-1],
		SecMin:    slices.Min(counts),
		SecMax:    slices.Max(counts),
		SecStdDev: math.Sqrt(sq / float64(len(counts))),
		Gaps:      len(gaps),
	}, nil
}

// nearestRank is the smallest value with at least p of the sample at or below
// it, the stand's own rule for its held-time percentiles.
func nearestRank(sorted []time.Duration, p float64) time.Duration {
	return sorted[max(0, int(float64(len(sorted))*p+0.999999)-1)]
}

// standSummary is what the stand prints on exit.
type standSummary struct {
	Arrivals, Answered int
	// Held percentiles are zero when nothing was answered.
	HeldP50, HeldP99 time.Duration
}

var (
	arrivalsLine = regexp.MustCompile(`(?m)^arrivals (\d+), answered (\d+)$`)
	heldLine     = regexp.MustCompile(`(?m)^held, answered calls: p50 (\S+)\s+p99 (\S+)\s+max (\S+)$`)
)

func parseStandSummary(text string) (standSummary, error) {
	var s standSummary
	m := arrivalsLine.FindStringSubmatch(text)
	if m == nil {
		return s, fmt.Errorf("no arrivals line in the stand's output:\n%s", text)
	}
	s.Arrivals, _ = strconv.Atoi(m[1])
	s.Answered, _ = strconv.Atoi(m[2])

	if h := heldLine.FindStringSubmatch(text); h != nil {
		var err error
		if s.HeldP50, err = time.ParseDuration(h[1]); err != nil {
			return s, err
		}
		if s.HeldP99, err = time.ParseDuration(h[2]); err != nil {
			return s, err
		}
	}

	return s, nil
}
