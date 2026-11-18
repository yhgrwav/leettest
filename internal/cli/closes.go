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

import "time"

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
}

// closesNotes is the lines under the connections line; none when no close
// was counted.
func closesNotes(c *CloseStats) []string {
	return nil
}
