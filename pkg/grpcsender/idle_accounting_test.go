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
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/keepalive"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// --- a connection the test can hold ----------------------------------------

// writeGate holds every write the client makes once armed, until released, so
// the client's writer goroutine stands still while the rest of the transport
// goes on.
type writeGate struct {
	armed atomic.Bool
	// held gets a value when a write meets the gate.
	held chan struct{}
	open chan struct{}
	once sync.Once
}

func newWriteGate() *writeGate {
	return &writeGate{held: make(chan struct{}, 1), open: make(chan struct{})}
}

func (g *writeGate) release() { g.once.Do(func() { close(g.open) }) }

type gatedConn struct {
	net.Conn

	gate *writeGate
}

func (c gatedConn) Write(p []byte) (int, error) {
	if c.gate.armed.Load() {
		select {
		case c.gate.held <- struct{}{}:
		default:
		}

		<-c.gate.open
	}

	return c.Conn.Write(p)
}

// --- a call that ends late --------------------------------------------------

var lateEndSeq atomic.Int64

// lateEnd is a load balancing policy, pick_first with a Done on every pick that
// waits for release: grpc-go runs it in the call's own goroutine just before it
// tells the stats handler the call ended. So the call's End reaches the sender
// after everything the transport does on its own once the stream is over.
type lateEnd struct {
	name    string
	release chan struct{}
	ran     atomic.Bool
}

func newLateEnd() *lateEnd {
	l := &lateEnd{name: fmt.Sprintf("leettest_test_late_end_%d", lateEndSeq.Add(1)), release: make(chan struct{})}
	balancer.Register(l)

	return l
}

func (l *lateEnd) Name() string { return l.name }

func (l *lateEnd) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return balancer.Get("pick_first").Build(lateEndConn{ClientConn: cc, late: l}, opts)
}

func (l *lateEnd) dialOption() grpc.DialOption {
	return grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig":[{%q:{}}]}`, l.name))
}

type lateEndConn struct {
	balancer.ClientConn

	late *lateEnd
}

func (c lateEndConn) UpdateState(s balancer.State) {
	s.Picker = lateEndPicker{Picker: s.Picker, late: c.late}
	c.ClientConn.UpdateState(s)
}

type lateEndPicker struct {
	balancer.Picker

	late *lateEnd
}

func (p lateEndPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	res, err := p.Picker.Pick(info)
	if err != nil {
		return res, err
	}

	done := res.Done
	res.Done = func(d balancer.DoneInfo) {
		p.late.ran.Store(true)

		select {
		case <-p.late.release:
		case <-time.After(5 * time.Second):
		}

		if done != nil {
			done(d)
		}
	}

	return res, nil
}

// --- a drain ---------------------------------------------------------------

// drainRig is one connection to the backend a, which the test drains with one
// call on it. The next dials go to next when it is set.
type drainRig struct {
	a      *backend
	hold   *holdingTarget
	gate   *writeGate
	late   *lateEnd
	sender *Sender
}

// newDrainRig connects a sender to a backend that holds its calls. With
// endFirst the writer of the connection can be held (writeGate); without it
// the end of each call can (lateEnd). idle is Options.IdleTimeout.
func newDrainRig(t *testing.T, endFirst bool, next *backend, idle time.Duration) *drainRig {
	t.Helper()

	r := &drainRig{
		hold: &holdingTarget{entered: make(chan struct{}, 4), release: make(chan struct{})},
		gate: newWriteGate(),
	}
	r.a = newBackend(t, "solo", r.hold)

	var dials atomic.Int32

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		target := r.a
		if dials.Add(1) > 1 && next != nil {
			target = next
		}

		c, err := target.lis.DialContext(ctx)
		if err != nil {
			return nil, err
		}

		return gatedConn{Conn: c, gate: r.gate}, nil
	}

	opts := Options{
		Target: "passthrough:///solo", IdleTimeout: idle,
		DialOptions: []grpc.DialOption{grpc.WithContextDialer(dialer)},
	}

	if !endFirst {
		r.late = newLateEnd()
		opts.DialOptions = append(opts.DialOptions, r.late.dialOption())
	}

	r.sender = New(opts)
	t.Cleanup(func() {
		checkNoOpenStreams(t, r.sender)
		_ = r.sender.Close()
	})
	// After the sender's cleanup is registered, so it runs before it: a writer
	// held at the gate would hold the close.
	t.Cleanup(r.gate.release)

	limit := 5 * time.Second
	if idle > 0 {
		limit = idle
	}

	ctx, cancel := context.WithTimeout(t.Context(), limit)
	defer cancel()

	if err := r.sender.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	return r
}

// drain puts one call on the connection, makes the target drain it (GOAWAY)
// while the call is open, lets the call finish, and returns once the call has
// ended and the connection's writer has been let go. With endFirst the call
// ends while the writer is held, so its End reaches the sender before the
// transport's own close; without it the call's End is held until the close
// has been counted.
func (r *drainRig) drain(t *testing.T, endFirst bool) {
	t.Helper()

	done := make(chan engine.Outcome, 1)
	ctx := bounded(t)

	go func() {
		out, _ := r.sender.Send(ctx, request(time.Now()))
		done <- out
	}()

	select {
	case <-r.hold.entered:
	case <-ctx.Done():
		t.Fatal("the call never reached the target")
	}

	if endFirst {
		r.gate.armed.Store(true)
	}

	// GracefulStop sends the GOAWAY at once and a ping behind it: the client
	// answers the ping with a write, which the gate holds.
	go r.a.srv.GracefulStop()

	if endFirst {
		select {
		case <-r.gate.held:
		case <-time.After(closesWait):
			t.Fatal("the client wrote nothing after the drain began: no way to hold its writer")
		}
	}

	close(r.hold.release)

	if endFirst {
		select {
		case out := <-done:
			if out.Category != engine.CategorySuccess {
				t.Fatalf("category %v, want success: the drained call is served", out.Category)
			}
		case <-ctx.Done():
			t.Fatal("the call did not end with the writer held")
		}

		if got := r.sender.Closes(); counted(got) != 0 {
			t.Fatalf("closes %+v with the writer held, want none: the premise of this order is broken", got)
		}

		r.gate.armed.Store(false)
		r.gate.release()

		return
	}

	waitCloses(t, r.sender, "one close while the call's End is held", anyClose)

	select {
	case <-done:
		t.Fatal("the call ended before its End was let through: the premise of this order is broken")
	default:
	}

	close(r.late.release)

	select {
	case out := <-done:
		if out.Category != engine.CategorySuccess {
			t.Fatalf("category %v, want success: the drained call is served", out.Category)
		}
	case <-ctx.Done():
		t.Fatal("the call did not end after its End was let through")
	}

	if !r.late.ran.Load() {
		t.Fatal("the held End never ran: the premise of this order is broken")
	}
}

// waitClosesWithin is waitCloses with a ceiling of the test's own.
func waitClosesWithin(t *testing.T, s *Sender, limit time.Duration, want string, ok func(CloseStats) bool) CloseStats {
	t.Helper()

	deadline := time.Now().Add(limit)

	for {
		got := s.Closes()
		if ok(got) {
			return got
		}

		if time.Now().After(deadline) {
			t.Fatalf("closes %+v after %v, want %s", got, limit, want)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// --- (a) a transport that never heard the server ------------------------------

// Ground: contract — a transport that closes before one byte came from the server was never idle:
// it is a refusal (a proxy in front of a dead backend, an accept-then-close), and the run already
// shows it as failed calls. Counting it as "closed with no call on it" would tell the user to set
// app.idle_timeout below 30s, in the very run that shows the system failing.
func TestCloses_ATransportClosedBeforeTheServerSpokeIsNotIdle(t *testing.T) {
	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() { _ = lis.Close() })

	go func() {
		for {
			c, err := lis.Accept()
			if err != nil {
				return
			}

			_ = c.Close()
		}
	}()

	s := New(Options{Target: "passthrough:///" + lis.Addr().String()})
	t.Cleanup(func() { _ = s.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	if err := s.Connect(ctx); err == nil {
		t.Fatal("connect to a target that closes at once succeeded")
	}

	// The transport's reader meets the closed connection before Connect gives up; a moment more
	// for a close counted from the writer's side.
	time.Sleep(100 * time.Millisecond)

	if got := s.Closes(); got != (CloseStats{}) {
		t.Fatalf("closes %+v, want none: the target closed before it sent a byte, the connection was never idle", got)
	}
}

// Ground: contract — the same, by the other way out: a target that accepts and says nothing until
// the connect gives up (a proxy in front of a dead backend does this). The transport is then closed
// by grpc-go, not by the target, so a "heard the server" check kept to the read-error path passes
// the test above and still counts this as the other side's idle close. Mutation "readAny checked
// only when the read fails" turns it red.
func TestCloses_ATransportClosedByUsBeforeTheServerSpokeIsNotIdle(t *testing.T) {
	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var (
		mu    sync.Mutex
		conns []net.Conn
	)

	t.Cleanup(func() {
		_ = lis.Close()

		mu.Lock()
		defer mu.Unlock()

		for _, c := range conns {
			_ = c.Close()
		}
	})

	go func() {
		for {
			c, err := lis.Accept()
			if err != nil {
				return
			}

			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()

	s := New(Options{Target: "passthrough:///" + lis.Addr().String()})
	// Closed after the check, never before it: a closed sender counts nothing.
	t.Cleanup(func() { _ = s.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	if err := s.Connect(ctx); err == nil {
		t.Fatal("connect to a target that never speaks succeeded")
	}

	time.Sleep(100 * time.Millisecond)

	if got := s.Closes(); got != (CloseStats{}) {
		t.Fatalf("closes %+v, want none: we gave up on a target that never sent a byte, the connection was never idle", got)
	}
}

// --- (b) a drain is busy, whichever ends first --------------------------------

// Ground: contract — a connection told to drain (GOAWAY) while a call was open closes with that
// call, not for idleness: it is busy, whether the call's End or the transport's own close reaches
// the sender first. Read as idle it would be counted as the other side closing a connection that
// had been idle for no time at all.
func TestCloses_ADrainIsBusyWhicheverEndsFirst(t *testing.T) {
	for _, tc := range []struct {
		name     string
		endFirst bool
	}{
		{"the call ends before the transport closes", true},
		{"the transport closes before the call ends", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDrainRig(t, tc.endFirst, nil, 0)
			r.drain(t, tc.endFirst)

			waitCloses(t, r.sender, "one close", anyClose)
			// The read error and the writer's close of one transport are one close.
			time.Sleep(100 * time.Millisecond)

			got := r.sender.Closes()
			if want := (CloseStats{Busy: 1}); got != want {
				t.Fatalf("closes %+v, want %+v: the target drained the connection with a call on it", got, want)
			}
		})
	}
}

// Ground: contract — one drain must not put peer_idle_min_us near zero: the advice "set
// app.idle_timeout below G" is the report's way out of a target that really closes idle
// connections, and a drain earlier in the run must not silence it. The target's idle time here is
// 1.5s, not 1s: the advice is not given for 1s or less (the config accepts nothing lower), so the
// number must be above it to show.
func TestCloses_ADrainDoesNotSilenceTheAdvice(t *testing.T) {
	const targetIdle = 1500 * time.Millisecond

	idle := newBackend(t, "idle", slowTarget{},
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionIdle: targetIdle}))
	r := newDrainRig(t, true, idle, 5*time.Second)

	r.drain(t, true)
	sendOK(t, r.sender)

	got := waitClosesWithin(t, r.sender, 8*time.Second, "the drain and the target's idle close",
		func(c CloseStats) bool { return counted(c) >= 2 })

	if got.Busy != 1 || got.IdlePeer != 1 || got.IdleOurs != 0 {
		t.Errorf("closes %+v, want one Busy (the drain) and one IdlePeer (the idle close)", got)
	}

	// The client's idle clock starts at the last call's End, a little after the
	// target's; half the pause is the room for a loaded machine, a drain gives 0.
	if got.PeerIdleMin < targetIdle/2 || got.PeerIdleMin > 3*time.Second {
		t.Errorf("PeerIdleMin %v, want about %v: the drain's close is no idle time, and the advice needs the real one",
			got.PeerIdleMin, targetIdle)
	}
}

// --- (c) the idle timeout left out --------------------------------------------

// Ground: contract — with app.idle_timeout left out the sender says grpc-go's 30 minutes out loud,
// so a close by our own idle timer is told from the other side's: a number the user never set
// closing a connection is ours, not "the other side or the network". The constant is shortened
// here, so the test waits for milliseconds, not half an hour.
func TestIdleTimeout_UnsetIsGrpcsDefaultAndOurCloseIsOurs(t *testing.T) {
	if DefaultIdleTimeout != 30*time.Minute || defaultIdle != DefaultIdleTimeout {
		t.Fatalf("default idle timeout %v (read as %v), want 30m: grpc-go's own default, passed explicitly",
			DefaultIdleTimeout, defaultIdle)
	}

	const short = 400 * time.Millisecond

	defaultIdle = short

	t.Cleanup(func() { defaultIdle = DefaultIdleTimeout })

	b := newBackend(t, "solo", slowTarget{})

	s := New(Options{Target: "passthrough:///solo", DialOptions: []grpc.DialOption{dial(b)}})
	t.Cleanup(func() {
		checkNoOpenStreams(t, s)
		_ = s.Close()
	})

	// Under the shortened default, whichever way it is read.
	ctx, cancel := context.WithTimeout(t.Context(), short*3/4)
	defer cancel()

	if err := s.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	sendOK(t, s)

	got := waitCloses(t, s, "one close by our idle timer", anyClose)
	if want := (CloseStats{IdleOurs: 1}); got != want {
		t.Fatalf("closes %+v, want %+v: nobody set an idle timeout, so the close at %v is the sender's own", got, want, short)
	}
}
