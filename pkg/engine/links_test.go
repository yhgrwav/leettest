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
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/metrics"
)

// linkedSender is a sender with several connections as far as the engine can tell.
type linkedSender struct {
	FakeSender

	addrs []string
}

func (l linkedSender) Links() []string { return l.addrs }

// linkedReporter also says what it knows about the connections.
type linkedReporter struct {
	linkedSender

	conns Connections
	known bool
}

func (l linkedReporter) Connections() (Connections, bool) { return l.conns, l.known }

// linkedEngine is an engine over sender whose statistics are started, so a
// test can record results into it without a run. The first second is warmup.
func linkedEngine(t *testing.T, sender Sender) (*Engine, time.Time) {
	t.Helper()

	eng, err := New(Options{
		Calls:       []Call{{Method: "a", Timeout: time.Second, Stages: []Stage{{StartRPS: 10, TargetRPS: 10, Duration: time.Second}}}},
		Sender:      sender,
		MaxInFlight: 100,
	})
	if err != nil {
		t.Fatalf("build the engine: %v", err)
	}

	start := time.Now()
	eng.stats.Start(start, time.Second)
	eng.stats.Reserve(time.Minute, "a")

	return eng, start
}

// onLink is a measured call of the run (scheduled after the warmup) on a connection.
func onLink(link int, start time.Time, o Outcome) Result {
	at := start.Add(2 * time.Second)
	o.Link = link

	if o.SentAt.IsZero() {
		o.SentAt = at
	}
	if o.DoneAt.IsZero() {
		o.DoneAt = at.Add(10 * time.Millisecond)
	}

	return Result{Method: "a", ScheduledAt: at, BegunAt: at, Deadline: at.Add(time.Second), Outcome: o}
}

func addrs3() []string { return []string{"10.0.0.1:443", "10.0.0.2:443", "10.0.0.3:443"} }

func finish(eng *Engine, start time.Time) Report {
	eng.stats.EndSending(start.Add(3 * time.Second))
	eng.stats.Finish(start.Add(4 * time.Second))

	return eng.Report()
}

func eachOf(t *testing.T, r Report) []LinkReport {
	t.Helper()

	if r.Connections == nil || len(r.Connections.Each) == 0 {
		t.Fatalf("connections %+v, want one entry per connection", r.Connections)
	}

	return r.Connections.Each
}

// Ground: contract — what a connection's row says is the library's API: a call counts for the
// connection it was assigned to, sent or not, warmup excluded; a failure is anything but a
// success and an abort, as Report.Failed counts it, and an unsent call that waited for the
// connection itself. An unsent call held back by the generator or by a full stream is a call of
// the connection, not its failure. Mutations "Failed counts any unsent call" and "Failed counts
// no unsent call" turn it red.
func TestStats_EachLinkCountsTheCallsAssignedToIt(t *testing.T) {
	eng, start := linkedEngine(t, linkedSender{addrs: addrs3()[:2]})

	record := func(r Result) { eng.stats.Record(r) }

	for range 3 {
		record(onLink(0, start, Outcome{Category: CategorySuccess}))
	}
	record(onLink(1, start, Outcome{Category: CategorySuccess}))
	record(onLink(1, start, Outcome{Category: CategorySuccess}))
	record(onLink(1, start, Outcome{Category: CategoryUnreachable}))
	record(onLink(1, start, Outcome{Category: CategoryTimeout, DoneAt: start.Add(3 * time.Second)}))
	// A call that never went out: not in Report.Failed, but a call of the connection that failed it.
	unsent := onLink(1, start, Outcome{Category: CategoryTimeout, NotSent: true, NotSentOn: BlockedOnConnection})
	unsent.DoneAt = unsent.Deadline
	record(unsent)
	// Held back by the generator, or by a stream the target had none free: calls of the
	// connection, no failure of it.
	lagged := onLink(1, start, Outcome{Category: CategoryTimeout, NotSent: true, NotSentOn: BlockedOnGenerator})
	lagged.DoneAt = lagged.Deadline
	record(lagged)
	full := onLink(1, start, Outcome{Category: CategoryTimeout, NotSent: true, NotSentOn: BlockedOnStream})
	full.DoneAt = full.Deadline
	record(full)
	// The run's abort is nobody's fault: counted as a call, not as a failure.
	record(onLink(1, start, Outcome{Category: CategoryAborted}))
	// The warmup is on neither connection, a failure of it included.
	warm := onLink(0, start, Outcome{Category: CategoryServerFault})
	warm.ScheduledAt = start
	record(warm)

	report := finish(eng, start)

	got := eachOf(t, report)
	if len(got) != 2 {
		t.Fatalf("%d entries, want 2", len(got))
	}

	for i, want := range []struct {
		addr          string
		calls, failed int
	}{{"10.0.0.1:443", 3, 0}, {"10.0.0.2:443", 8, 3}} {
		if got[i].Address != want.addr || got[i].Calls != want.calls || got[i].Failed != want.failed {
			t.Errorf("connection %d: %s, %d calls, %d failed; want %s, %d, %d",
				i+1, got[i].Address, got[i].Calls, got[i].Failed, want.addr, want.calls, want.failed)
		}
	}

	// The rows add up to the run: every measured call is on one connection, and the failures are
	// the run's plus the unsent calls that waited for a connection.
	if calls := got[0].Calls + got[1].Calls; calls != report.Sent+report.NotSent {
		t.Errorf("connections add up to %d calls, the run sent %d and left %d unsent", calls, report.Sent, report.NotSent)
	}
	if failed := got[0].Failed + got[1].Failed; failed != report.Failed+report.NotSentConnection {
		t.Errorf("connections add up to %d failed, the run has %d failed and %d unsent for a connection",
			failed, report.Failed, report.NotSentConnection)
	}
}

// Ground: contract — StreamWaited is Report.StreamWaited's rule per connection: a call that went
// out after waiting over the floor; one that never went out is a call, not a wait that was served.
func TestStats_EachLinkCountsItsStreamWaitsOverTheFloor(t *testing.T) {
	eng, start := linkedEngine(t, linkedSender{addrs: addrs3()[:2]})

	record := func(r Result) { eng.stats.Record(r) }

	record(onLink(0, start, Outcome{Category: CategorySuccess, StreamWait: StreamWaitFloor}))
	record(onLink(0, start, Outcome{Category: CategorySuccess, StreamWait: StreamWaitFloor + time.Nanosecond}))
	record(onLink(1, start, Outcome{Category: CategorySuccess, StreamWait: 50 * time.Millisecond}))
	record(onLink(1, start, Outcome{Category: CategorySuccess, StreamWait: 70 * time.Millisecond}))
	stuck := onLink(1, start, Outcome{Category: CategoryTimeout, NotSent: true, NotSentOn: BlockedOnStream})
	stuck.DoneAt = stuck.Deadline
	record(stuck)

	report := finish(eng, start)
	got := eachOf(t, report)

	if got[0].StreamWaited != 1 || got[1].StreamWaited != 2 {
		t.Errorf("stream waited %d and %d, want 1 (at the floor is not over it) and 2 (the unsent one is not a wait that was served)",
			got[0].StreamWaited, got[1].StreamWaited)
	}
	if got[0].StreamWaited+got[1].StreamWaited != report.StreamWaited {
		t.Errorf("connections add up to %d waited, the report says %d", got[0].StreamWaited+got[1].StreamWaited, report.StreamWaited)
	}
}

// Ground: contract — the connection's p99 is the run's percentile rule over the connection's own
// calls that have a latency: successes, and timeouts and aborts as lower bounds; a refused
// connection has none. The expected values come from metrics, not from the engine.
func TestStats_EachLinkHasTheRunsP99OverItsOwnCalls(t *testing.T) {
	eng, start := linkedEngine(t, linkedSender{addrs: addrs3()})

	record := func(r Result) { eng.stats.Record(r) }

	fast := metrics.NewLatencies()
	for i := 1; i <= 100; i++ {
		took := time.Duration(i) * time.Millisecond
		fast.Record(took)
		record(onLink(0, start, Outcome{Category: CategorySuccess, DoneAt: start.Add(2*time.Second + took)}))
	}

	// Connection 2 never answers once: a refusal has no latency, a timeout is a lower bound.
	slow := metrics.NewLatencies()
	slow.RecordCensored(time.Second)
	record(onLink(1, start, Outcome{Category: CategoryUnreachable}))
	record(onLink(1, start, Outcome{Category: CategoryTimeout, DoneAt: start.Add(3 * time.Second)}))

	// Connection 3 only refuses.
	record(onLink(2, start, Outcome{Category: CategoryUnreachable}))

	got := eachOf(t, finish(eng, start))
	if len(got) != 3 {
		t.Fatalf("%d entries, want 3", len(got))
	}

	if want := fast.Snapshot().Percentile(0.99); got[0].P99 != want {
		t.Errorf("connection 1 p99 %+v, want %+v", got[0].P99, want)
	}
	if want := slow.Snapshot().Percentile(0.99); got[1].P99 != want || !got[1].P99.Defined || got[1].P99.Exact {
		t.Errorf("connection 2 p99 %+v, want %+v: a lower bound, not exact", got[1].P99, want)
	}
	if got[2].P99.Defined {
		t.Errorf("connection 3 p99 %+v, want none: nothing answered", got[2].P99)
	}
}

// Ground: contract — what the sender knows of each connection (address, limits) and what the
// engine counted of each meet in one entry, and asking twice does not count twice.
func TestEngine_ReportJoinsTheSendersConnectionsWithTheCounts(t *testing.T) {
	sender := linkedReporter{
		linkedSender: linkedSender{addrs: addrs3()},
		known:        true,
		conns: Connections{
			Open: 3, Resolved: addrs3()[:2], InFlightLimit: 9, InFlightAnnounced: true, LimitAnnounced: true,
			Each: []LinkReport{
				{Address: addrs3()[0], LimitAnnounced: true, FirstLimit: 4, LastLimit: 4},
				{Address: addrs3()[1], LimitAnnounced: true, FirstLimit: 1, LastLimit: 1},
				{Address: addrs3()[2], LimitAnnounced: true, FirstLimit: 4, LastLimit: 4},
			},
		},
	}
	eng, start := linkedEngine(t, sender)

	for link, n := range []int{3, 2, 1} {
		for range n {
			eng.stats.Record(onLink(link, start, Outcome{Category: CategorySuccess}))
		}
	}

	want := []int{3, 2, 1}
	for range 2 {
		report := finish(eng, start)
		got := eachOf(t, report)

		for i := range got {
			if got[i].Calls != want[i] || got[i].Address != addrs3()[i] || got[i].LastLimit != sender.conns.Each[i].LastLimit {
				t.Errorf("entry %d: %+v, want %d calls at %s with the sender's limit %d",
					i, got[i], want[i], addrs3()[i], sender.conns.Each[i].LastLimit)
			}
		}

		c := *report.Connections
		if c.Open != 3 || c.InFlightLimit != 9 || !c.InFlightAnnounced || !reflect.DeepEqual(c.Resolved, addrs3()[:2]) {
			t.Errorf("connections %+v: the sender's own fields must pass through", c)
		}
	}

	if sender.conns.Each[0].Calls != 0 {
		t.Errorf("the engine wrote %d calls into the sender's own slice", sender.conns.Each[0].Calls)
	}
}

// Ground: contract — a sender that cannot vouch for its handshakes (custom credentials) still has
// connections and addresses, and the report must show them, with no limit it did not read.
func TestEngine_ReportHasTheLinksOfASenderThatKnowsNoHandshake(t *testing.T) {
	eng, start := linkedEngine(t, linkedReporter{linkedSender: linkedSender{addrs: addrs3()}, known: false})

	for link := range 3 {
		eng.stats.Record(onLink(link, start, Outcome{Category: CategorySuccess}))
	}
	eng.stats.Record(onLink(2, start, Outcome{Category: CategorySuccess}))

	got := finish(eng, start).Connections
	if got == nil {
		t.Fatal("connections nil, want the links: more than one connection is worth reporting")
	}

	if got.Open != 3 || !reflect.DeepEqual(got.Resolved, addrs3()) {
		t.Errorf("open %d resolved %v, want 3 connections to %v", got.Open, got.Resolved, addrs3())
	}
	if got.LimitAnnounced || got.InFlightAnnounced || got.InFlightLimit != 0 || got.FirstLimit != 0 || got.LastLimit != 0 {
		t.Errorf("connections %+v: no limit was read, none may be reported", *got)
	}

	for i, e := range got.Each {
		wantCalls := 1
		if i == 2 {
			wantCalls = 2
		}
		if e.Address != addrs3()[i] || e.Calls != wantCalls || e.LimitAnnounced || e.LastLimit != 0 || e.FirstLimit != 0 {
			t.Errorf("entry %d: %+v, want %s with %d calls and no limit", i, e, addrs3()[i], wantCalls)
		}
	}
}

// Ground: contract — with fewer than two addresses there are no per-connection entries, and a
// sender with one connection and one that tells nothing stay as they were.
func TestEngine_OneLinkIsNoPerLinkReport(t *testing.T) {
	for _, tt := range []struct {
		name  string
		addrs []string
	}{{"none", nil}, {"one", addrs3()[:1]}} {
		t.Run(tt.name, func(t *testing.T) {
			eng, start := linkedEngine(t, linkedReporter{linkedSender: linkedSender{addrs: tt.addrs}, known: false})
			eng.stats.Record(onLink(0, start, Outcome{Category: CategorySuccess}))

			if got := finish(eng, start).Connections; got != nil {
				t.Errorf("connections %+v, want none: the sender knows nothing and has no second connection", *got)
			}
		})
	}

	eng, start := linkedEngine(t, linkedReporter{
		linkedSender: linkedSender{addrs: addrs3()[:1]}, known: true, conns: Connections{Open: 1},
	})
	eng.stats.Record(onLink(0, start, Outcome{Category: CategorySuccess}))

	got := finish(eng, start).Connections
	if got == nil || got.Each != nil || got.Resolved != nil {
		t.Errorf("connections %+v, want the sender's own, with no entries", got)
	}
}

// Ground: contract — off is absent: with no link addresses a result's Link, whatever it says,
// lands nowhere and nothing is reported per connection. Mutation "record per link even without
// links" turns it red. Green on the stub by nature: the stub never counts per link.
func TestStats_NoLinksTouchesNothing(t *testing.T) {
	eng, start := linkedEngine(t, FakeSender{})

	for _, link := range []int{0, 1, 7} {
		eng.stats.Record(onLink(link, start, Outcome{Category: CategorySuccess}))
	}

	report := finish(eng, start)
	if report.Sent != 3 {
		t.Fatalf("sent %d, want 3: the calls must still be counted", report.Sent)
	}
	if report.Connections != nil {
		t.Errorf("connections %+v, want none: nobody reported connections", *report.Connections)
	}
}

// Ground: hot path — a call is recorded under the lock every worker shares; a per-connection
// count must cost it no allocation. The two runs see the same number of allocations whatever the
// connection; the count of the second proves the per-connection state was live.
func TestStats_PerLinkRecordDoesNotAllocate(t *testing.T) {
	const runs = 200

	allocs := func(sender Sender, link int) (float64, *Engine, time.Time) {
		eng, start := linkedEngine(t, sender)
		r := onLink(link, start, Outcome{Category: CategorySuccess})

		return testing.AllocsPerRun(runs, func() { eng.stats.Record(r) }), eng, start
	}

	plain, _, _ := allocs(FakeSender{}, 0)
	linked, eng, start := allocs(linkedSender{addrs: addrs3()[:2]}, 1)

	// AllocsPerRun runs the function once more than runs to warm up.
	got := eachOf(t, finish(eng, start))
	if got[1].Calls != runs+1 || got[0].Calls != 0 {
		t.Fatalf("calls per connection %d and %d, want 0 and %d: the records were not counted per connection", got[0].Calls, got[1].Calls, runs+1)
	}

	if linked != plain {
		t.Errorf("%v allocations per record over two connections, %v over one", linked, plain)
	}
}

// linkedHolding holds each call until the run is aborted, on connections taken in turn, and
// returns the abort with the connection it had picked, as the real sender does.
type linkedHolding struct {
	addrs   []string
	entered chan struct{}
	calls   atomic.Int64
}

func (l *linkedHolding) Links() []string { return l.addrs }

func (l *linkedHolding) Send(ctx context.Context, _ Request) (Outcome, error) {
	link := int(l.calls.Add(1)-1) % len(l.addrs)
	l.entered <- struct{}{}

	<-ctx.Done()

	return Outcome{Link: link}, ctx.Err()
}

// Ground: concurrency — an abort ends every call in flight at once, in the worker that rebuilds
// the outcome of a sender's error; the connection the call was on must survive that, or the
// whole of an aborted run lands on connection 1. Mutation "the rebuilt outcome drops Link" turns
// it red.
func TestAbort_AnAbortedCallKeepsItsLink(t *testing.T) {
	sender := &linkedHolding{addrs: addrs3()[:2], entered: make(chan struct{}, 1024)}
	eng := stopEngine(t, sender, time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	done := runAsync(ctx, eng)

	limit := time.After(5 * time.Second)
	for range 10 {
		select {
		case <-sender.entered:
		case <-limit:
			t.Fatal("fewer than 10 calls reached the sender")
		}
	}

	cancel()

	if err := waitRun(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run after the abort = %v, want context.Canceled", err)
	}

	n := int(sender.calls.Load())
	got := eachOf(t, eng.Report())

	if got[0].Calls != (n+1)/2 || got[1].Calls != n/2 {
		t.Errorf("%d and %d aborted calls on the connections, want %d and %d of the %d the sender saw",
			got[0].Calls, got[1].Calls, (n+1)/2, n/2, n)
	}
	if got[0].Failed != 0 || got[1].Failed != 0 {
		t.Errorf("failed %d and %d, want 0 and 0: an abort is not a failure of a connection", got[0].Failed, got[1].Failed)
	}
}
