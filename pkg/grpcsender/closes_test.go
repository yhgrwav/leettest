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

package grpcsender

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// closesWait bounds every wait for a close to be counted.
const closesWait = 3 * time.Second

// soloOptions is Options for one connection to the backend "solo".
func soloOptions(idle time.Duration) Options {
	return Options{Target: "passthrough:///solo", IdleTimeout: idle}
}

// connectIdle connects n connections to the backends with a Connect deadline
// the idle timeout allows: a longer one is refused (checkIdle). Not
// connectTo, whose deadline is 5s.
func connectIdle(t *testing.T, n int, backends []*backend, opts Options) *Sender {
	t.Helper()

	opts.Connections = n
	if n > 1 {
		opts.Target = name
		opts.Lookup = lookup(addrsOf(backends)...)
	}

	opts.DialOptions = append(opts.DialOptions, dial(backends...))

	sender := New(opts)
	t.Cleanup(func() {
		checkNoOpenStreams(t, sender)
		_ = sender.Close()
	})

	limit := 5 * time.Second
	if opts.IdleTimeout > 0 {
		limit = opts.IdleTimeout
	}

	ctx, cancel := context.WithTimeout(t.Context(), limit)
	defer cancel()

	if err := sender.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	return sender
}

func sendOK(t *testing.T, s *Sender) {
	t.Helper()

	out, err := s.Send(bounded(t), request(time.Now()))
	if err != nil || out.Category != engine.CategorySuccess {
		t.Fatalf("send: category %v, error %v, want success", out.Category, err)
	}
}

func counted(c CloseStats) int { return c.IdleOurs + c.IdlePeer + c.Busy }

// waitCloses returns the first count of closes that ok accepts, and fails the
// test with what was counted if none does within closesWait.
func waitCloses(t *testing.T, s *Sender, want string, ok func(CloseStats) bool) CloseStats {
	t.Helper()

	deadline := time.Now().Add(closesWait)

	for {
		got := s.Closes()
		if ok(got) {
			return got
		}

		if time.Now().After(deadline) {
			t.Fatalf("closes %+v after %v, want %s", got, closesWait, want)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func anyClose(c CloseStats) bool { return counted(c) > 0 }

// Ground: contract — a connection our own idle timer closes is IdleOurs, never the other side's:
// the report would blame the network for what the user's own key did. The next call reconnects.
func TestCloses_OurIdleTimerClosesAConnectionAndTheNextCallReconnects(t *testing.T) {
	b := newBackend(t, "solo", slowTarget{})
	s := connectIdle(t, 1, []*backend{b}, soloOptions(200*time.Millisecond))

	sendOK(t, s)

	got := waitCloses(t, s, "one close by our idle timer", anyClose)
	if want := (CloseStats{IdleOurs: 1}); got != want {
		t.Fatalf("closes %+v, want %+v: closed with no call on it, after our idle timeout", got, want)
	}

	sendOK(t, s)

	if c, _ := s.Connections(); c.Reconnects != 1 {
		t.Errorf("reconnects %d, want 1: the second call dialled again", c.Reconnects)
	}

	if after := s.Closes(); after != got {
		t.Errorf("closes %+v after the reconnect, want %+v: dialling again closes nothing", after, got)
	}
}

// Ground: contract — a connection the target closes for idleness (GOAWAY, we answer with our
// Close) is the other side's, with the time it had gone without a call; the client has no idle
// timeout here. The channel reconnects with the next call.
func TestCloses_AGoawayWithNoCallIsThePeersAndKeepsTheIdleTime(t *testing.T) {
	b := newBackend(t, "solo", slowTarget{},
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: 100 * time.Millisecond}))
	s := connectIdle(t, 1, []*backend{b}, soloOptions(0))

	sendOK(t, s)

	got := waitCloses(t, s, "one close by the other side", anyClose)
	if got.IdlePeer != 1 || got.IdleOurs != 0 || got.Busy != 0 {
		t.Fatalf("closes %+v, want one IdlePeer: the target closed it, no call on it", got)
	}

	// 100ms of idleness on the server; a client sees its last call end a little before the
	// server's, and the close a little after.
	if got.PeerIdleMin < 50*time.Millisecond || got.PeerIdleMin > 400*time.Millisecond {
		t.Errorf("PeerIdleMin %v, want about 100ms", got.PeerIdleMin)
	}

	sendOK(t, s)

	if c, _ := s.Connections(); c.Reconnects != 1 {
		t.Errorf("reconnects %d, want 1: the channel dials again with the next call", c.Reconnects)
	}
}

// Ground: contract — the idle time counts from the last call, not from the connection coming up:
// a connection kept busy for 800ms and then left is closed after 250ms without calls, and says so.
func TestCloses_TheIdleTimeCountsFromTheLastCall(t *testing.T) {
	b := newBackend(t, "solo", slowTarget{},
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: 250 * time.Millisecond}))
	s := connectIdle(t, 1, []*backend{b}, soloOptions(0))

	for end := time.Now().Add(800 * time.Millisecond); time.Now().Before(end); time.Sleep(40 * time.Millisecond) {
		sendOK(t, s)
	}

	if got := s.Closes(); counted(got) != 0 {
		t.Fatalf("closes %+v while calls came every 40ms, want none: the target's idle timer was 250ms", got)
	}

	got := waitCloses(t, s, "one close by the other side", anyClose)
	if got.IdlePeer != 1 {
		t.Fatalf("closes %+v, want one IdlePeer", got)
	}

	if got.PeerIdleMin < 125*time.Millisecond || got.PeerIdleMin > 700*time.Millisecond {
		t.Errorf("PeerIdleMin %v, want about 250ms: from the last call, not from the connection coming up 800ms before", got.PeerIdleMin)
	}
}

// Ground: contract — the target closing first, while our idle timeout is longer, is the
// target's: our idle timeout is no reason to call it ours.
func TestCloses_APeerThatClosesBeforeOurIdleTimeoutIsThePeers(t *testing.T) {
	b := newBackend(t, "solo", slowTarget{},
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: 100 * time.Millisecond}))
	s := connectIdle(t, 1, []*backend{b}, soloOptions(time.Second))

	sendOK(t, s)

	got := waitCloses(t, s, "one close by the other side", anyClose)
	if got.IdlePeer != 1 || got.IdleOurs != 0 || got.Busy != 0 {
		t.Fatalf("closes %+v, want one IdlePeer and no IdleOurs: our timeout was 1s, the close came at 100ms", got)
	}

	if got.PeerIdleMin < 50*time.Millisecond || got.PeerIdleMin > 400*time.Millisecond {
		t.Errorf("PeerIdleMin %v, want about 100ms", got.PeerIdleMin)
	}
}

// Ground: contract — a reset of the connection from the target with no call on it is the
// other side's, whoever it was (a proxy, a firewall): counted at the read, not at a close of ours.
func TestCloses_ACutConnectionWithNoCallOnItIsThePeers(t *testing.T) {
	b := newBackend(t, "solo", slowTarget{})
	s := connectIdle(t, 1, []*backend{b}, soloOptions(0))

	sendOK(t, s)
	time.Sleep(50 * time.Millisecond)
	b.lis.cut()

	got := waitCloses(t, s, "one close by the other side", anyClose)
	if got.IdlePeer != 1 || got.IdleOurs != 0 || got.Busy != 0 {
		t.Fatalf("closes %+v, want one IdlePeer", got)
	}

	if got.PeerIdleMin < 20*time.Millisecond || got.PeerIdleMin > 2*time.Second {
		t.Errorf("PeerIdleMin %v, want the 50ms or so since the call ended", got.PeerIdleMin)
	}

	// The read error and the transport's own Close that follows are one connection.
	time.Sleep(100 * time.Millisecond)

	if after := s.Closes(); after != got {
		t.Errorf("closes %+v a moment later, want %+v: one connection closed once", after, got)
	}
}

// Ground: contract — a cut with a call on the connection is Busy, and the call keeps the
// category it has always had: it went out and no status came back. Nothing about the call changes.
func TestCloses_ACutConnectionWithACallOnItIsBusyAndTheCallIsStillCutOff(t *testing.T) {
	hold := &holdingTarget{entered: make(chan struct{}, 4), release: make(chan struct{})}
	b := newBackend(t, "solo", hold)
	s := connectIdle(t, 1, []*backend{b}, soloOptions(0))

	done := make(chan engine.Outcome, 1)
	ctx := bounded(t)

	go func() {
		out, _ := s.Send(ctx, request(time.Now()))
		done <- out
	}()

	select {
	case <-hold.entered:
	case <-bounded(t).Done():
		t.Fatal("the call never reached the target")
	}

	b.lis.cut()

	var out engine.Outcome

	select {
	case out = <-done:
	case <-bounded(t).Done():
		t.Fatal("the call did not end after the cut")
	}

	if out.Category != engine.CategoryCutOff {
		t.Errorf("category %v, want cut_off: the request went out, no status came back", out.Category)
	}

	got := waitCloses(t, s, "one close with a call on it", anyClose)
	if want := (CloseStats{Busy: 1}); got != want {
		t.Errorf("closes %+v, want %+v", got, want)
	}
}

// Ground: contract — the run's own Close is not a connection closing: every counter stays zero,
// at one connection and at three.
func TestCloses_TheRunsOwnCloseCountsNothing(t *testing.T) {
	for _, n := range []int{1, 3} {
		bs := pair(t, slowTarget{}, nil, nil)
		opts := Options{}

		if n == 1 {
			bs, opts = []*backend{newBackend(t, "solo", slowTarget{})}, soloOptions(0)
		}

		s := connectIdle(t, n, bs, opts)

		for range n {
			sendOK(t, s)
		}

		if err := s.Close(); err != nil {
			t.Fatalf("%d connections: close: %v", n, err)
		}

		// The transport's reader meets the closed connection a moment after Close returns.
		time.Sleep(100 * time.Millisecond)

		if got := s.Closes(); got != (CloseStats{}) {
			t.Errorf("%d connections: closes %+v after the run's own Close, want none", n, got)
		}
	}
}

// Ground: contract — with several connections every one has its own idle timer and its own
// count, and the sender reports them added: three connections, three closes by our timer.
func TestCloses_AreAddedOverTheConnections(t *testing.T) {
	bs := pair(t, slowTarget{}, nil, nil)
	s := connectIdle(t, 3, bs, Options{IdleTimeout: 200 * time.Millisecond})

	for range 3 {
		sendOK(t, s)
	}

	got := waitCloses(t, s, "three closes", func(c CloseStats) bool { return counted(c) >= 3 })
	if want := (CloseStats{IdleOurs: 3}); got != want {
		t.Errorf("closes %+v, want %+v: one for each connection", got, want)
	}
}

// Ground: boundary — a connection that never carried a call, closed by our own idle timer, is
// ours: the timer of grpc-go starts when the channel connects, before our idle clock does.
func TestCloses_AConnectionNeverCalledClosedByOurTimerIsOurs(t *testing.T) {
	b := newBackend(t, "solo", slowTarget{})
	s := connectIdle(t, 1, []*backend{b}, soloOptions(200*time.Millisecond))

	got := waitCloses(t, s, "one close by our idle timer", anyClose)
	if want := (CloseStats{IdleOurs: 1}); got != want {
		t.Errorf("closes %+v, want %+v: PeerIdleMin is how long the connection had been idle when it closed", got, want)
	}
}

// Ground: concurrency — calls, cuts and reads of the counters at once: the race detector and the
// count of open streams are what is checked.
func TestCloses_ReadWhileConnectionsClose(t *testing.T) {
	b := newBackend(t, "solo", slowTarget{})
	s := connectIdle(t, 1, []*backend{b}, soloOptions(0))

	stop := make(chan struct{})
	ctx := bounded(t)

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = s.Send(ctx, request(time.Now()))
				}
			}
		})
	}

	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.Closes()
			}
		}
	})

	for range 5 {
		time.Sleep(30 * time.Millisecond)
		b.lis.cut()
	}

	close(stop)
	wg.Wait()

	if got := s.Closes(); counted(got) == 0 {
		t.Errorf("closes %+v after five cuts, want some", got)
	}
}
