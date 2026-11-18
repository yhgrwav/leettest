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
	"fmt"
	"strings"
	"time"
)

// CloseStats is how the connections of a run or a search were closed, as the
// sender counted it. A report whose sender does not count carries nil.
type CloseStats struct {
	// IdleOurs are the closes with no call on the connection made by LeetTest's
	// own app.idle_timeout; IdlePeer those by the other side or the network
	// after PeerIdleMin or more without calls; Busy those with calls on it.
	IdleOurs, IdlePeer, Busy int
	PeerIdleMin              time.Duration
	// IdleTimeout is app.idle_timeout as configured, 0 when it was left out.
	IdleTimeout time.Duration
	// IdleDefault is true when the key was left out; IdleTimeout is then the
	// default the sender used. Compile stub of the red tests: nothing reads it yet.
	IdleDefault bool
}

// minIdleTimeout is the shortest app.idle_timeout the config accepts: an
// advice to set a value below it cannot be followed.
const minIdleTimeout = time.Second

// closesNotes are the lines about how the connections were closed: one for the
// closes with no call on the connection, one for those with calls on it; a
// part with a zero count is left out, and nothing is said when nothing was
// counted.
func closesNotes(c *CloseStats) []string {
	if c == nil {
		return nil
	}

	var notes []string

	if c.IdleOurs > 0 || c.IdlePeer > 0 {
		var parts []string

		if c.IdleOurs > 0 {
			parts = append(parts, fmt.Sprintf("%d by app.idle_timeout %s", c.IdleOurs, c.IdleTimeout))
		}

		if c.IdlePeer > 0 {
			parts = append(parts, fmt.Sprintf("%d by the other side or the network after %s or more without calls",
				c.IdlePeer, floorIdle(c.PeerIdleMin)))
		}

		line := "closed with no call on it: " + strings.Join(parts, ", ")
		if advice := idleAdvice(c); advice != "" {
			line += "; " + advice
		}

		notes = append(notes, line)
	}

	if c.Busy > 0 {
		notes = append(notes, fmt.Sprintf("closed while calls were on it: %d", c.Busy))
	}

	return notes
}

// idleAdvice is the way out of closes by the other side: an app.idle_timeout
// below the shortest time one of them waited. Not given when our timer is
// already below it (it would have closed first, so the other side's closes
// came from something else), nor when no value could follow it.
func idleAdvice(c *CloseStats) string {
	if c.IdlePeer == 0 || c.IdleTimeout != 0 && c.IdleTimeout < c.PeerIdleMin {
		return ""
	}

	below := floorIdle(c.PeerIdleMin)
	if below <= minIdleTimeout {
		return ""
	}

	return "set app.idle_timeout below " + below.String()
}

// floorIdle rounds down: to whole seconds from 10s, to milliseconds below it.
// "Below X" must not overstate X, so never up.
func floorIdle(d time.Duration) time.Duration {
	if d >= 10*time.Second {
		return d.Truncate(time.Second)
	}

	return d.Truncate(time.Millisecond)
}

// closesFields are the closes as the JSON has them: the counts, and the
// shortest idle time of the other side's closes in microseconds, null while
// there was none. All null when the sender did not count: not counted is not
// none closed.
func closesFields(c *CloseStats) (ours, peer, busy *int, peerIdleMinUS *int64) {
	if c == nil {
		return nil, nil, nil, nil
	}

	if c.IdlePeer > 0 {
		us := micros(c.PeerIdleMin)
		peerIdleMinUS = &us
	}

	o, p, b := c.IdleOurs, c.IdlePeer, c.Busy

	return &o, &p, &b, peerIdleMinUS
}

// jsonCloses is the closes of a search: one sender serves all its steps, so
// the counts are the search's, once. Null when the sender did not count.
type jsonCloses struct {
	ClosedIdleOurs *int   `json:"closed_idle_ours"`
	ClosedIdlePeer *int   `json:"closed_idle_peer"`
	ClosedBusy     *int   `json:"closed_busy"`
	PeerIdleMinUS  *int64 `json:"peer_idle_min_us"`
}

func closesJSONOf(c *CloseStats) *jsonCloses {
	if c == nil {
		return nil
	}

	out := new(jsonCloses)
	out.ClosedIdleOurs, out.ClosedIdlePeer, out.ClosedBusy, out.PeerIdleMinUS = closesFields(c)

	return out
}
