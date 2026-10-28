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

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

var (
	aRunPlan = RealPlan{
		Target: "10.0.0.5:50051", PeakRPS: 800, Ramp: 30 * time.Second, Duration: 90 * time.Second,
	}
	aSearchPlan = RealPlan{Target: "10.0.0.5:50051", Search: true, From: 50, To: 800}
)

// Ground: contract — the line under the question tells the target, the peak
// and the duration, and calls them planned: a run that is stopped, or whose
// calls come late, does not do exactly this. The ramp is inside the duration
// and the line says so. The numbers are the plan's, printed as they are.
func TestConfirmText_ARunStatesItsPlannedNumbers(t *testing.T) {
	const want = "real mode: 10.0.0.5:50051, planned peak 800 rps, planned duration 1m30s, ramp 30s included"

	if got := ConfirmText(aRunPlan); got != want {
		t.Errorf("ConfirmText =\n%q\nwant\n%q", got, want)
	}
}

// Ground: contract — a search has no duration to promise and no single peak:
// the line gives the range it steps through and says it goes on until the
// system breaks, and does not borrow the numbers of a plain run.
func TestConfirmText_ASearchDoesNotPromiseADuration(t *testing.T) {
	const want = "real mode: 10.0.0.5:50051, search from 50 to 800 rps until it breaks, duration not promised"

	got := ConfirmText(aSearchPlan)
	if got != want {
		t.Errorf("ConfirmText =\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(got, "planned") {
		t.Errorf("a search's line has planned numbers: %q", got)
	}
}

// Ground: contract — only y and yes, in any case and with the spaces and the
// carriage return a console adds, start the run. Anything else, an empty line
// and the end of input included, does not: the default of [y/N] is no. The
// prompt goes to the writer with the plan above it.
func TestConfirm_OnlyAYesStarts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
		yes    bool
	}{
		{"y", "y\n", true},
		{"Y", "Y\n", true},
		{"yes", "yes\n", true},
		{"YES", "YES\n", true},
		{"spaces", "  yes  \n", true},
		{"console line end", "y\r\n", true},
		{"a pipe that ended after y", "y", true},
		{"n", "n\n", false},
		{"no", "no\n", false},
		{"empty line", "\n", false},
		{"end of input", "", false},
		{"yy", "yy\n", false},
		{"yes please", "yes please\n", false},
		{"ye", "ye\n", false},
		{"y after a no", "n\ny\n", false},
	} {
		var out bytes.Buffer

		err := Confirm(t.Context(), strings.NewReader(tc.answer), &out, aRunPlan)

		switch {
		case tc.yes && err != nil:
			t.Errorf("%s: %q refused: %v", tc.name, tc.answer, err)
		case !tc.yes && !errors.Is(err, ErrNotConfirmed):
			t.Errorf("%s: %q: err = %v, want ErrNotConfirmed", tc.name, tc.answer, err)
		}

		if got := out.String(); !strings.Contains(got, ConfirmText(aRunPlan)) || !strings.HasSuffix(got, "Start? [y/N] ") {
			t.Errorf("%s: the prompt is\n%q\nwant the plan line, then «Start? [y/N] » with no line end", tc.name, got)
		}
	}
}

// Ground: contract — the plan is asked before anything is read, and the
// refusal says what happened and that nothing was sent; the text is the
// message the user reads.
func TestConfirm_TheRefusalSaysNothingWasSent(t *testing.T) {
	err := Confirm(t.Context(), strings.NewReader("n\n"), io.Discard, aRunPlan)

	if err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Errorf("err = %v, want a refusal that says nothing was sent", err)
	}
}

// Ground: signal — an answer that never comes does not hold the process once
// the run is cancelled (Ctrl+C, SIGTERM): Confirm returns the context's error
// at once and not ErrNotConfirmed, so the caller ends the way a cancelled
// connection does.
func TestConfirm_ACancelEndsAQuestionNobodyAnswers(t *testing.T) {
	in, out := io.Pipe()
	t.Cleanup(func() { _ = out.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- Confirm(ctx, in, io.Discard, aRunPlan) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrNotConfirmed) {
			t.Errorf("err = %v, want context.Canceled and not ErrNotConfirmed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Confirm is still waiting for an answer after the cancel")
	}
}
