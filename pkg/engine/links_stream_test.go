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

package engine

import (
	"testing"
	"time"
)

// unsentOn is a measured call that expired unsent on a connection, held back by blocker.
func unsentOn(link int, start time.Time, blocker Blocker) Result {
	r := onLink(link, start, Outcome{Category: CategoryTimeout, NotSent: true, NotSentOn: blocker})
	r.DoneAt = r.Deadline

	return r
}

// Ground: contract — NotSentStream of a connection is Report.NotSentStream's rule over the calls
// assigned to it: only the unsent calls a full stream held back, as notSentCause tells them
// apart, so a call blamed on the generator by its lag is not one, whatever the sender saw. The
// connections add up to the run's count. Mutations "counts any unsent call" (6 instead of 3 on
// connection 2), "counts by the sender's NotSentOn alone" (4) and "counts the warmup" (2 on
// connection 1) turn it red.
func TestStats_NotSentStreamPerLink(t *testing.T) {
	eng, start := linkedEngine(t, linkedSender{addrs: addrs3()[:2]})

	record := func(r Result) { eng.stats.Record(r) }

	// Connection 2: three calls a full stream held back, and everything else that can go unsent.
	for range 3 {
		record(unsentOn(1, start, BlockedOnStream))
	}

	lateOnStream := unsentOn(1, start, BlockedOnStream)
	lateOnStream.BegunAt = lateOnStream.Deadline // the generator ate the whole budget: its fault, not the stream's
	record(lateOnStream)

	record(unsentOn(1, start, BlockedOnGenerator))
	record(unsentOn(1, start, BlockedOnConnection))
	// A call that went out and waited for a stream is not an unsent one.
	record(onLink(1, start, Outcome{Category: CategorySuccess, StreamWait: 70 * time.Millisecond}))

	// Connection 1: one.
	record(unsentOn(0, start, BlockedOnStream))
	record(onLink(0, start, Outcome{Category: CategorySuccess}))

	// The warmup is on no connection.
	warm := unsentOn(0, start, BlockedOnStream)
	warm.ScheduledAt = start
	record(warm)

	report := finish(eng, start)
	got := eachOf(t, report)

	if got[0].NotSentStream != 1 || got[1].NotSentStream != 3 {
		t.Errorf("not sent for a stream %d on connection 1 and %d on connection 2, want 1 and 3",
			got[0].NotSentStream, got[1].NotSentStream)
	}
	if sum := got[0].NotSentStream + got[1].NotSentStream; sum != report.NotSentStream {
		t.Errorf("connections add up to %d not sent for a stream, the run says %d", sum, report.NotSentStream)
	}
}

// Ground: contract — a connection that never held a call back for a stream says 0, not a count
// of its unsent calls: the field is "waited for a stream", and a connection that failed its
// calls another way did not.
func TestStats_NotSentStreamIsZeroForAConnectionThatWasHeldBackOtherwise(t *testing.T) {
	eng, start := linkedEngine(t, linkedSender{addrs: addrs3()[:2]})

	eng.stats.Record(unsentOn(0, start, BlockedOnConnection))
	eng.stats.Record(unsentOn(0, start, BlockedOnGenerator))
	eng.stats.Record(unsentOn(1, start, BlockedOnStream))

	got := eachOf(t, finish(eng, start))

	if got[0].NotSentStream != 0 || got[1].NotSentStream != 1 {
		t.Errorf("not sent for a stream %d and %d, want 0 and 1", got[0].NotSentStream, got[1].NotSentStream)
	}
}
