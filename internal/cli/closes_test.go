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
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// closedNotes are the notes of a run that begin with "closed ": the lines
// about how the connections were closed.
func closedNotes(run RunReport) []string {
	var out []string

	for _, n := range runNotes(run) {
		if strings.HasPrefix(n, "closed ") {
			out = append(out, n)
		}
	}

	return out
}

func closesRun(c *CloseStats) RunReport {
	return RunReport{Report: echoReport(), Closes: c}
}

// Ground: contract (spec §4.4, §6 U3) — the golden frames of the text: each
// part is printed only when its count is not zero, the advice only when it can
// be followed, and a run with nothing counted says nothing. The numbers are
// not round on purpose: 12, 3 and 1 would pass a swapped field in a frame with
// all three equal.
// Mutations "print the advice always" (the frame with idle_timeout below G),
// "the advice for idle_timeout at G or above only" and "swap ours and peer"
// turn it red.
func TestCloses_TheFramesOfTheText(t *testing.T) {
	const (
		oursPart = "12 by app.idle_timeout 50s"
		peerPart = "3 by the other side or the network after 58s or more without calls"
	)

	tests := []struct {
		name   string
		closes *CloseStats
		want   []string
	}{
		{"not counted", nil, nil},
		{"all zero", &CloseStats{IdleTimeout: 50 * time.Second}, nil},
		{
			"ours only", &CloseStats{IdleOurs: 12, IdleTimeout: 50 * time.Second},
			[]string{"closed with no call on it: " + oursPart},
		},
		{
			"peer only, no idle_timeout: the advice", &CloseStats{IdlePeer: 3, PeerIdleMin: 58300 * time.Millisecond},
			[]string{"closed with no call on it: " + peerPart + "; set app.idle_timeout below 58s"},
		},
		{
			"peer, idle_timeout above G: the advice",
			&CloseStats{IdlePeer: 3, PeerIdleMin: 58300 * time.Millisecond, IdleTimeout: 90 * time.Second},
			[]string{"closed with no call on it: " + peerPart + "; set app.idle_timeout below 58s"},
		},
		{
			"peer, idle_timeout equal to G: the advice",
			&CloseStats{IdlePeer: 3, PeerIdleMin: 58300 * time.Millisecond, IdleTimeout: 58300 * time.Millisecond},
			[]string{"closed with no call on it: " + peerPart + "; set app.idle_timeout below 58s"},
		},
		{
			"peer, idle_timeout a hair below G: no advice, our timer was first",
			&CloseStats{IdlePeer: 3, PeerIdleMin: 58300 * time.Millisecond, IdleTimeout: 58299 * time.Millisecond},
			[]string{"closed with no call on it: " + peerPart},
		},
		{
			"peer, idle_timeout below G: no advice",
			&CloseStats{IdlePeer: 3, PeerIdleMin: 58300 * time.Millisecond, IdleTimeout: 50 * time.Second},
			[]string{"closed with no call on it: " + peerPart},
		},
		{
			"ours and peer, idle_timeout below G",
			&CloseStats{IdleOurs: 12, IdlePeer: 3, PeerIdleMin: 58300 * time.Millisecond, IdleTimeout: 50 * time.Second},
			[]string{"closed with no call on it: " + oursPart + ", " + peerPart},
		},
		{
			"busy only", &CloseStats{Busy: 1},
			[]string{"closed while calls were on it: 1"},
		},
		{
			"ours and busy", &CloseStats{IdleOurs: 12, Busy: 7, IdleTimeout: 50 * time.Second},
			[]string{"closed with no call on it: " + oursPart, "closed while calls were on it: 7"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := closedNotes(closesRun(tt.closes)); !slices.Equal(got, tt.want) {
				t.Errorf("notes\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// Ground: contract (spec §4.4) — the advice names the time rounded down, never
// up: "below X" must not overstate X. Whole seconds from 10s, milliseconds
// below it. 59.9s reads 59s and 1.2345s reads 1.234s; 10s stays 10s and 9.9999s
// reads 9.999s. Mutation "round up" and "round to the nearest" turn it red.
func TestCloses_TheAdviceRoundsDown(t *testing.T) {
	tests := []struct {
		min  time.Duration
		want string
	}{
		{59900 * time.Millisecond, "59s"},
		{58300 * time.Millisecond, "58s"},
		{10 * time.Second, "10s"},
		{10999 * time.Millisecond, "10s"},
		{9999900 * time.Microsecond, "9.999s"},
		{1234500 * time.Microsecond, "1.234s"},
		{1500 * time.Millisecond, "1.5s"},
		{65 * time.Second, "1m5s"},
	}

	for _, tt := range tests {
		got := closedNotes(closesRun(&CloseStats{IdlePeer: 1, PeerIdleMin: tt.min}))
		if len(got) != 1 {
			t.Errorf("G %v: notes %q, want one", tt.min, got)

			continue
		}

		if want := "; set app.idle_timeout below " + tt.want; !strings.HasSuffix(got[0], want) {
			t.Errorf("G %v: %q does not end with %q", tt.min, got[0], want)
		}
		// The number of the line itself says "after <G>" in the same rounding.
		if want := "after " + tt.want + " or more without calls"; !strings.Contains(got[0], want) {
			t.Errorf("G %v: %q does not contain %q", tt.min, got[0], want)
		}
	}
}

// Ground: boundary — the advice has to be followable: app.idle_timeout is at
// least 1s, so when G rounded down is not above 1s there is nothing "below"
// to set, and the line gives no advice. 1.001s is the first that gets one.
// Mutation "advise at any G" turns it red.
func TestCloses_NoAdviceThatCannotBeFollowed(t *testing.T) {
	for _, tt := range []struct {
		min    time.Duration
		advice bool
	}{
		{200 * time.Millisecond, false},
		{400 * time.Microsecond, false},
		{time.Second, false},
		{1000900 * time.Microsecond, false},
		{1001 * time.Millisecond, true},
	} {
		got := closedNotes(closesRun(&CloseStats{IdlePeer: 1, PeerIdleMin: tt.min}))
		if len(got) != 1 {
			t.Errorf("G %v: notes %q, want one", tt.min, got)

			continue
		}

		if has := strings.Contains(got[0], "set app.idle_timeout"); has != tt.advice {
			t.Errorf("G %v: advice %v in %q, want %v", tt.min, has, got[0], tt.advice)
		}
	}
}

// Ground: contract (spec §4.4) — the lines sit under the connections line: the
// first note after it, nothing from the verdict between them.
func TestCloses_TheLineFollowsTheConnectionsLine(t *testing.T) {
	run := closesRun(&CloseStats{IdleOurs: 4, IdleTimeout: 30 * time.Second})
	run.Connections = &engine.Connections{Open: 1}

	notes := runNotes(run)
	at := slices.IndexFunc(notes, func(n string) bool { return strings.HasPrefix(n, "connections: ") })

	if at < 0 || at+1 >= len(notes) {
		t.Fatalf("no connections line followed by a note in %q", notes)
	}

	if want := "closed with no call on it: 4 by app.idle_timeout 30s"; notes[at+1] != want {
		t.Errorf("the note after the connections line is %q, want %q", notes[at+1], want)
	}

	var out bytes.Buffer
	PrintReport(&out, "localhost:50051", run)

	if !strings.Contains(out.String(), "\n"+notes[at+1]+"\n") {
		t.Errorf("the text report lacks the line:\n%s", out.String())
	}
}

// Ground: contract — the JSON report carries the same lines as the text, in
// notes, in the same order: both read runNotes.
func TestCloses_TheJSONNotesCarryTheLines(t *testing.T) {
	run := jsonRun(echoReport())
	run.Run.Closes = &CloseStats{IdleOurs: 12, Busy: 1, IdleTimeout: 50 * time.Second}

	notes, _ := writeJSON(t, run)["notes"].([]any)

	var got []string

	for _, n := range notes {
		if s, _ := n.(string); strings.HasPrefix(s, "closed ") {
			got = append(got, s)
		}
	}

	want := []string{"closed with no call on it: 12 by app.idle_timeout 50s", "closed while calls were on it: 1"}
	if !slices.Equal(got, want) {
		t.Errorf("notes %q, want %q", got, want)
	}
}

// closesJSON is the connections object of a run over one connection with the
// closes.
func closesJSON(t *testing.T, closes *CloseStats) map[string]any {
	t.Helper()

	run := jsonRun(engine.Report{Connections: &engine.Connections{Open: 1}})
	run.Run.Closes = closes

	obj, ok := writeJSON(t, run)["connections"].(map[string]any)
	if !ok {
		t.Fatalf("connections is not an object")
	}

	return obj
}

// Ground: contract (spec §4.5) — the object connections has the four fields
// of the closes: the counts as numbers and the shortest idle time of the other
// side's closes in microseconds. The numbers differ from each other.
// Mutations "swap ours and peer in the JSON" and "ns for us" turn it red.
func TestCloses_TheJSONFields(t *testing.T) {
	obj := closesJSON(t, &CloseStats{IdleOurs: 12, IdlePeer: 3, Busy: 7, PeerIdleMin: 58300 * time.Millisecond})

	for key, want := range map[string]float64{
		"closed_idle_ours": 12, "closed_idle_peer": 3, "closed_busy": 7, "peer_idle_min_us": 58300000,
	} {
		if got := obj[key]; got != want {
			t.Errorf("connections.%s = %#v, want %v", key, got, want)
		}
	}
}

// Ground: contract (spec §4.5) — peer_idle_min_us is null when nothing was
// closed by the other side: 0 would be a shortest idle time of 0, a number that
// lies. The counts stay numbers, 0 among them. Mutation "0 instead of null"
// turns it red.
func TestCloses_NoPeerClosesIsNullNotZero(t *testing.T) {
	obj := closesJSON(t, &CloseStats{IdleOurs: 2, IdleTimeout: time.Minute})

	if v, ok := obj["peer_idle_min_us"]; !ok || v != nil {
		t.Errorf("connections.peer_idle_min_us = %#v (present %v), want null", v, ok)
	}

	for key, want := range map[string]float64{"closed_idle_ours": 2, "closed_idle_peer": 0, "closed_busy": 0} {
		if got := obj[key]; got != want {
			t.Errorf("connections.%s = %#v, want %v", key, got, want)
		}
	}
}

// Ground: contract — a sender that does not count says null, not 0: a run the
// closes were not counted for is not one with none closed. The keys are there.
func TestCloses_NotCountedIsNull(t *testing.T) {
	obj := closesJSON(t, nil)

	for _, key := range []string{"closed_idle_ours", "closed_idle_peer", "closed_busy", "peer_idle_min_us"} {
		if v, ok := obj[key]; !ok || v != nil {
			t.Errorf("connections.%s = %#v (present %v), want null", key, v, ok)
		}
	}
}

func decodeSearchJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, data)
	}

	return out
}

// ranSearch is a search held through every step with the closes.
func ranSearch(closes *CloseStats) BreakpointRun {
	run := echoSearch(bpPlan, 1)
	run.Closes = closes

	return run
}

// Ground: contract (spec §6 U3, §8.4) — a search carries the same: the sender
// is one for the whole search, so the counts are the search's, once. In the
// text they are lines of its notes; in the JSON the same lines are
// breakpoint.notes and the four fields are the top-level object closes (not
// connections: a search has none of a plain run's top-level fields, and an
// object of that name without open would be read as one step's).
// Mutation "no lines in the search" turns it red.
func TestCloses_ASearchCarriesThem(t *testing.T) {
	run := ranSearch(&CloseStats{IdleOurs: 12, IdlePeer: 3, PeerIdleMin: 58300 * time.Millisecond, Busy: 1, IdleTimeout: 50 * time.Second})

	text := echoSearchText(run)
	for _, line := range []string{
		"  - closed with no call on it: 12 by app.idle_timeout 50s, 3 by the other side or the network after 58s or more without calls\n",
		"  - closed while calls were on it: 1\n",
	} {
		if !strings.Contains(text, line) {
			t.Errorf("the search's text lacks %q:\n%s", line, text)
		}
	}

	if strings.Count(text, "closed with no call on it") != 1 {
		t.Errorf("the line is not once in the search's text:\n%s", text)
	}

	var buf bytes.Buffer
	if err := WriteBreakpointJSON(&buf, run); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := decodeSearchJSON(t, buf.Bytes())
	obj, ok := out["closes"].(map[string]any)
	if !ok {
		t.Fatalf("closes = %#v, want an object", out["closes"])
	}

	for key, want := range map[string]float64{
		"closed_idle_ours": 12, "closed_idle_peer": 3, "closed_busy": 1, "peer_idle_min_us": 58300000,
	} {
		if got := obj[key]; got != want {
			t.Errorf("closes.%s = %#v, want %v", key, got, want)
		}
	}

	if _, ok := out["connections"]; ok {
		t.Errorf("a top-level connections in the search's JSON")
	}

	notes, _ := field(t, out, "breakpoint", "notes").([]any)
	if len(notes) < 2 {
		t.Errorf("breakpoint.notes = %v, want the two lines", notes)
	}
}

// Ground: boundary — a search whose sender did not count prints no line and
// writes closes as null; one that counted none closed prints no line and
// writes zeros.
func TestCloses_ASearchWithNoneOrNotCounted(t *testing.T) {
	for _, tt := range []struct {
		name   string
		closes *CloseStats
		null   bool
	}{
		{"not counted", nil, true},
		{"none closed", &CloseStats{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			run := ranSearch(tt.closes)

			if text := echoSearchText(run); strings.Contains(text, "closed ") {
				t.Errorf("a line about closes in the text:\n%s", text)
			}

			var buf bytes.Buffer
			if err := WriteBreakpointJSON(&buf, run); err != nil {
				t.Fatalf("write: %v", err)
			}

			v, present := decodeSearchJSON(t, buf.Bytes())["closes"]
			if !present {
				t.Fatalf("no closes key")
			}

			if (v == nil) != tt.null {
				t.Errorf("closes = %#v, null want %v", v, tt.null)
			}
		})
	}
}
