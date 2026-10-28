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
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// Ground: contract — the report names what stopped the run, and the first
// cause wins. A person who presses Ctrl-C stops the run; the calls that hang
// then run into their timeouts while they are drained, the window fills and
// the monitor decides — too late to have stopped anything. The report of that
// run says nothing of an error spike: no «stopped at» on stderr, the text
// report or in the JSON, stop_reason and stop null. Exit 3 as for any soft
// stop.
func TestRun_ACtrlCInTheDrainIsNotTheMonitorsStop(t *testing.T) {
	target := startSlowTarget(t, 30*time.Second)
	// Plan 20 s, timeout 5 s: the first call times out at 5 s, the monitor
	// decides a tick or two later. The press is at 3 s, well before.
	cfg := spikeConfig(t, target.addr, "5%", "5s", "20s")
	path := newJSONPath(t)

	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	var stdout, stderr bytes.Buffer

	stops, aborts := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- run(t.Context(), stops, aborts, []string{"-json-file", path, "-c", cfg}, &stdout, &stderr)
	}()

	time.Sleep(3 * time.Second)
	stops <- struct{}{}

	var err error

	select {
	case err = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the run did not return after the stop")
	}

	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v, want ErrIncomplete: a run stopped by a person is an incomplete one\nstderr:\n%s",
			err, stderr.String())
	}
	for name, got := range map[string]string{"stderr": stderr.String(), "the report": stdout.String()} {
		if strings.Contains(got, "stopped at ") {
			t.Errorf("%s names the monitor for a run the person stopped:\n%s", name, got)
		}
	}

	js := decodeOnly(t, string(readFile(t, path)))
	for _, key := range []string{"stop_reason", "stop"} {
		if got, ok := js[key]; !ok || got != nil {
			t.Errorf("%s = %v (present %v), want null", key, got, ok)
		}
	}
	if got := jsonAt(t, js, "outcome"); got != "incomplete" {
		t.Errorf("outcome = %v, want incomplete: the run was stopped", got)
	}
}

// Ground: contract — the box keeps the first cause: a trip after another
// stop is not the run's stop, and another stop after a trip does not take it
// back. A trip is told only for a report that is incomplete (the engine's).
func TestTripBox_TheFirstCauseWins(t *testing.T) {
	cut := engine.Report{Incomplete: true}

	var after tripBox
	after.Other()
	after.Trip(aTrip)

	if got := after.Of(cut); got != nil {
		t.Errorf("a trip after another stop is the run's stop: %+v", got)
	}

	var before tripBox
	before.Trip(aTrip)
	before.Other()

	if got := before.Of(cut); got == nil || got.Method != aTrip.Method {
		t.Errorf("another stop after a trip took the trip back: %+v", got)
	}
}
