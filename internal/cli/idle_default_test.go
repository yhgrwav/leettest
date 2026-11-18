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
	"slices"
	"testing"
	"time"
)

// Ground: contract — with app.idle_timeout left out the sender's own 30m default closes idle
// connections, and the line names it as the default: "by app.idle_timeout 30m0s" would send the
// user looking for a key they never wrote. Mutation "the text ignores IdleDefault" turns it red.
func TestClosesNotes_OurDefaultIdleCloseNamesTheDefault(t *testing.T) {
	got := closedNotes(closesRun(&CloseStats{IdleOurs: 1, IdleTimeout: 30 * time.Minute, IdleDefault: true}))
	want := []string{"closed with no call on it: 1 by the default idle timeout 30m0s"}

	if !slices.Equal(got, want) {
		t.Errorf("notes\n got %q\nwant %q", got, want)
	}

	// Our timer at the default is below a long idle of the other side: no advice, our timer was first.
	got = closedNotes(closesRun(&CloseStats{
		IdleOurs: 2, IdlePeer: 1, PeerIdleMin: time.Hour, IdleTimeout: 30 * time.Minute, IdleDefault: true,
	}))
	want = []string{"closed with no call on it: 2 by the default idle timeout 30m0s, " +
		"1 by the other side or the network after 1h0m0s or more without calls"}

	if !slices.Equal(got, want) {
		t.Errorf("with the other side's close\n got %q\nwant %q", got, want)
	}
}

// Ground: contract — with the key set the text does not change by a byte, a value of 30m included:
// it is the user's own number, not the default. Guard, green before the change.
func TestClosesNotes_ConfiguredIdleTextIsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		idle time.Duration
		want string
	}{
		{50 * time.Second, "closed with no call on it: 12 by app.idle_timeout 50s"},
		{30 * time.Minute, "closed with no call on it: 12 by app.idle_timeout 30m0s"},
	} {
		got := closedNotes(closesRun(&CloseStats{IdleOurs: 12, IdleTimeout: tc.idle}))
		if !slices.Equal(got, []string{tc.want}) {
			t.Errorf("idle_timeout %v: notes %q, want %q", tc.idle, got, tc.want)
		}
	}
}
