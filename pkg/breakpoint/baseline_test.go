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

package breakpoint

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// noBaselineNote is the line a probe judged against no baseline leaves.
const noBaselineNote = "no baseline: the first step measured too few calls for a p99, the knee was not checked"

// undefinedP99 is a step whose p99 is not defined: too few calls measured.
func undefinedP99(sent int) engine.Report {
	return engine.Report{Sent: sent, Methods: []engine.MethodReport{{Method: "a.B/C", Sent: sent, Latencies: sent}}}
}

func didNotRecoverNotes(res Result) []string {
	return slices.DeleteFunc(slices.Clone(res.Notes), func(n string) bool { return !strings.Contains(n, "did not recover") })
}

func recoveredProbes(res Result) (probes, recovered int) {
	for i := range res.Steps {
		if s := &res.Steps[i]; s.Kind == Probe {
			probes++
			if s.Recovered {
				recovered++
			}
		}
	}

	return probes, recovered
}

// Ground: contract — the steps below the break measured too few calls for a
// p99, so there is no baseline; the probe at the first rate has its 500 calls
// and a p99 of its own. Judging it against a baseline of 0 ("did not recover",
// five probes, the break named CauseNoRecovery) is a verdict on nothing: the
// probe recovers, the repeat runs, and one line says the knee was not checked.
func TestSearch_AProbeWithNoBaselineRecoversAndSaysSo(t *testing.T) {
	p := plan
	p.To = 156
	var asked []int
	target := func(rps int) engine.Report {
		switch {
		case rps == 100 && len(asked) > 3:
			return report(1000, 0, 20*time.Millisecond, 20*time.Millisecond)
		case rps < 156:
			return undefinedP99(rps * 10)
		default:
			return report(rps*10, rps*5, 500*time.Millisecond, 500*time.Millisecond)
		}
	}
	res, err := Search(t.Context(), p, fake(target, &asked))
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if want := []int{100, 125, 156, 100, 156}; !slices.Equal(asked, want) {
		t.Errorf("ran %v, want %v: one probe that recovered, then the repeat", asked, want)
	}
	if probes, recovered := recoveredProbes(res); probes == 0 || recovered != probes {
		t.Errorf("%d probes, %d of them recovered, want all: nothing to be far from", probes, recovered)
	}
	if notes := didNotRecoverNotes(res); len(notes) > 0 {
		t.Errorf("notes %q say the target did not recover from a baseline that does not exist", notes)
	}
	if res.Cause == CauseNoRecovery {
		t.Errorf("cause %v: no_recovery with no baseline", res.Cause)
	}
	if res.Outcome != BrokeBetween || res.Held != 125 || res.Broke != 156 || res.Cause != CauseErrors {
		t.Errorf("%v held %d broke %d cause %v, want BrokeBetween 125 156 by errors", res.Outcome, res.Held, res.Broke, res.Cause)
	}
	if !slices.Contains(res.Notes, noBaselineNote) {
		t.Errorf("notes %q, want the line %q", res.Notes, noBaselineNote)
	}
}

// Ground: contract — a step that held with an undefined p99 does not touch the
// baseline the earlier steps set: it is not a p99 of 0. The probe is then
// judged against 20ms as always, and no line about a missing baseline is
// written because there is one.
func TestSearch_AStepWithNoP99LeavesTheBaselineAlone(t *testing.T) {
	p := plan
	p.To = 156
	var asked []int
	target := func(rps int) engine.Report {
		switch {
		case rps == 100 && len(asked) > 3:
			return report(1000, 0, 25*time.Millisecond, 25*time.Millisecond)
		case rps == 100:
			return report(1000, 0, 20*time.Millisecond, 20*time.Millisecond)
		case rps == 125:
			return undefinedP99(rps * 10)
		default:
			return report(rps*10, rps*5, 500*time.Millisecond, 500*time.Millisecond)
		}
	}
	res, err := Search(t.Context(), p, fake(target, &asked))
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if want := []int{100, 125, 156, 100, 156}; !slices.Equal(asked, want) {
		t.Errorf("ran %v, want %v: 25ms is within 1.5x of the 20ms baseline", asked, want)
	}
	if probes, recovered := recoveredProbes(res); probes != 1 || recovered != 1 {
		t.Errorf("%d probes, %d recovered, want 1 and 1", probes, recovered)
	}
	if notes := didNotRecoverNotes(res); len(notes) > 0 {
		t.Errorf("notes %q, want none: the baseline is 20ms and the probe is 25ms", notes)
	}
	if slices.Contains(res.Notes, noBaselineNote) {
		t.Errorf("notes %q carry the no-baseline line, but a step set a baseline", res.Notes)
	}
}

// Guard (green before and after): steps that define a p99 behave exactly as
// they did. The whole sequence of a plain search is pinned: kinds, rates,
// verdicts, causes and no notes.
func TestSearch_StepsWithAP99AreJudgedAsBefore(t *testing.T) {
	var asked []int
	res, err := Search(t.Context(), plan, fake(capacity(270), &asked))
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	var got []string
	for _, s := range res.Steps {
		got = append(got, fmt.Sprintf("%v %d broken=%v recovered=%v cause=%d", s.Kind, s.RPS, s.Broken, s.Recovered, s.Cause))
	}
	want := []string{
		"step 100 broken=false recovered=false cause=0",
		"step 125 broken=false recovered=false cause=0",
		"step 156 broken=false recovered=false cause=0",
		"step 195 broken=false recovered=false cause=0",
		"step 244 broken=false recovered=false cause=0",
		"step 305 broken=true recovered=false cause=1",
		"probe 100 broken=false recovered=true cause=0",
		"repeat 305 broken=true recovered=false cause=1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("steps\n%q\nwant\n%q", got, want)
	}
	if res.Outcome != BrokeBetween || res.Held != 244 || res.Broke != 305 || res.Cause != CauseErrors || len(res.Notes) != 0 {
		t.Errorf("%v held %d broke %d cause %v notes %q, want BrokeBetween 244 305 by errors, no notes",
			res.Outcome, res.Held, res.Broke, res.Cause, res.Notes)
	}
}
