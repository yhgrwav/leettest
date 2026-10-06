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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/yhgrwav/leettest/pkg/engine"
)

var (
	_ engine.LinkReporter       = (*Sender)(nil)
	_ engine.ConnectionReporter = (*Sender)(nil)
)

// name is the Target the tests give: it resolves to nothing, Lookup says where it goes.
const name = "backends.test:443"

// --- backends -------------------------------------------------------------

// backend is one address a name resolves to: a health server in process that
// the test can watch at the level of connections and of what a call carried.
type backend struct {
	addr  string
	lis   *watchedListener
	srv   *grpc.Server
	calls atomic.Int64

	mu          sync.Mutex
	authorities []string
}

// newBackend serves target as addr. The dialer in dial reaches it by that name.
func newBackend(t *testing.T, addr string, target grpc_health_v1.HealthServer, opts ...grpc.ServerOption) *backend {
	t.Helper()

	b := &backend{addr: addr, lis: &watchedListener{Listener: bufconn.Listen(1024 * 1024)}}
	opts = append([]grpc.ServerOption{grpc.UnaryInterceptor(b.note)}, opts...)
	b.srv = grpc.NewServer(opts...)
	grpc_health_v1.RegisterHealthServer(b.srv, target)

	go func() { _ = b.srv.Serve(b.lis) }()

	t.Cleanup(b.srv.Stop)

	return b
}

// note records every call: how many, and the :authority each came with.
func (b *backend) note(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
	b.calls.Add(1)

	if md, ok := metadata.FromIncomingContext(ctx); ok {
		b.mu.Lock()
		b.authorities = append(b.authorities, md.Get(":authority")...)
		b.mu.Unlock()
	}

	return next(ctx, req)
}

func (b *backend) seenAuthorities() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return slices.Clone(b.authorities)
}

// watchedListener remembers the connections it accepted, to see them closed.
type watchedListener struct {
	*bufconn.Listener

	mu    sync.Mutex
	conns []*watchedConn
}

func (l *watchedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	w := &watchedConn{Conn: c, closed: make(chan struct{})}

	l.mu.Lock()
	l.conns = append(l.conns, w)
	l.mu.Unlock()

	return w, nil
}

func (l *watchedListener) accepted() []*watchedConn {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.conns)
}

// cut closes every connection accepted so far, as a target that drops them.
func (l *watchedListener) cut() {
	for _, c := range l.accepted() {
		_ = c.Close()
	}
}

type watchedConn struct {
	net.Conn

	once   sync.Once
	closed chan struct{}
}

func (c *watchedConn) Close() error {
	c.once.Do(func() { close(c.closed) })

	return c.Conn.Close()
}

// allClosed fails the test unless every connection the backend accepted ends
// by the ceiling, and returns how many there were.
func (b *backend) allClosed(t *testing.T) int {
	t.Helper()

	conns := b.lis.accepted()
	for _, c := range conns {
		select {
		case <-c.closed:
		case <-bounded(t).Done():
			t.Fatalf("a connection to %s is still open", b.addr)
		}
	}

	return len(conns)
}

// dial reaches the backend a connection was told to dial by its address.
func dial(backends ...*backend) grpc.DialOption {
	return grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		for _, b := range backends {
			if b.addr == addr {
				return b.lis.DialContext(ctx)
			}
		}

		return nil, fmt.Errorf("nothing at %s: refused by the test", addr)
	})
}

// lookup is a resolver that answers the same addresses to every name.
func lookup(addrs ...string) func(context.Context, string) ([]string, error) {
	return func(context.Context, string) ([]string, error) { return slices.Clone(addrs), nil }
}

func addrsOf(backends []*backend) []string {
	var out []string
	for _, b := range backends {
		out = append(out, b.addr)
	}

	return out
}

// pair is two backends of one name, serving target.
func pair(t *testing.T, target grpc_health_v1.HealthServer, opts0, opts1 []grpc.ServerOption) []*backend {
	t.Helper()

	return []*backend{
		newBackend(t, "10.0.0.1:443", target, opts0...),
		newBackend(t, "10.0.0.2:443", target, opts1...),
	}
}

// connectTo builds a sender of n connections to the name, resolved to the backends.
func connectTo(t *testing.T, n int, backends []*backend, opts Options) (*Sender, error) {
	t.Helper()

	opts.Connections = n
	if opts.Target == "" {
		opts.Target = name
	}
	if opts.Lookup == nil {
		opts.Lookup = lookup(addrsOf(backends)...)
	}

	opts.DialOptions = append(opts.DialOptions, dial(backends...))

	sender := New(opts)
	t.Cleanup(func() {
		checkNoOpenStreams(t, sender)
		_ = sender.Close()
	})

	err := sender.Connect(bounded(t))

	return sender, err
}

func mustConnect(t *testing.T, n int, backends []*backend, opts Options) *Sender {
	t.Helper()

	sender, err := connectTo(t, n, backends, opts)
	if err != nil {
		t.Fatalf("connect %d connections: %v", n, err)
	}

	return sender
}

// load builds an engine over sender: rps calls a second for d, each with timeout.
func load(t *testing.T, sender engine.Sender, rps int, d, timeout time.Duration) *engine.Engine {
	t.Helper()

	eng, err := engine.New(engine.Options{
		Calls: []engine.Call{{
			Method: checkMethod, Timeout: timeout,
			Stages: []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: d}},
		}},
		Sender:      sender,
		MaxInFlight: 1000,
	})
	if err != nil {
		t.Fatalf("build the engine: %v", err)
	}

	return eng
}

func eachOf(t *testing.T, r engine.Report, n int) []engine.LinkReport {
	t.Helper()

	if r.Connections == nil || len(r.Connections.Each) != n {
		t.Fatalf("connections %+v, want %d entries, one per connection", r.Connections, n)
	}

	return r.Connections.Each
}

// --- connecting -----------------------------------------------------------

// Ground: contract — Connect resolves the target once, by Options.Lookup, and opens one
// connection per link, the addresses taken in turn: three over two addresses is 2 + 1.
func TestConnect_OpensOneConnectionPerLinkTheAddressesTakenInTurn(t *testing.T) {
	var asked []string

	bs := pair(t, slowTarget{}, nil, nil)
	sender := mustConnect(t, 3, bs, Options{Lookup: func(_ context.Context, target string) ([]string, error) {
		asked = append(asked, target)

		return addrsOf(bs), nil
	}})

	if !slices.Equal(asked, []string{name}) {
		t.Errorf("lookup asked for %q, want the target once: %q", asked, name)
	}

	want := []string{bs[0].addr, bs[1].addr, bs[0].addr}
	if got := sender.Links(); !slices.Equal(got, want) {
		t.Errorf("links %v, want %v", got, want)
	}

	if n0, n1 := len(bs[0].lis.accepted()), len(bs[1].lis.accepted()); n0 != 2 || n1 != 1 {
		t.Errorf("%d connections to %s and %d to %s, want 2 and 1", n0, bs[0].addr, n1, bs[1].addr)
	}

	conns, ok := sender.Connections()
	if !ok || conns.Open != 3 || !slices.Equal(conns.Resolved, addrsOf(bs)) {
		t.Errorf("connections %+v (known %v), want 3 open over the two distinct addresses", conns, ok)
	}
}

// Ground: contract — N = 1 is today's path: no resolution of ours, no links, no per-connection
// entries, and the in-flight bound is the one connection's limit.
func TestConnect_OneConnectionResolvesNothingAndReportsNoLinks(t *testing.T) {
	for _, n := range []int{0, 1} {
		t.Run(fmt.Sprintf("connections %d", n), func(t *testing.T) {
			bs := []*backend{newBackend(t, "solo", slowTarget{}, grpc.MaxConcurrentStreams(7))}

			sender := mustConnect(t, n, bs, Options{
				Target: "passthrough:///solo",
				Lookup: func(context.Context, string) ([]string, error) {
					t.Error("lookup called: one connection is grpc-go's own resolver, not ours")

					return nil, errors.New("not at one connection")
				},
			})

			if _, err := sender.Send(bounded(t), request(time.Now())); err != nil {
				t.Fatalf("send: %v", err)
			}

			if links := sender.Links(); links != nil {
				t.Errorf("links %v, want none", links)
			}

			want := engine.Connections{
				Open: 1, LimitAnnounced: true, FirstLimit: 7, LastLimit: 7,
				InFlightLimit: 7, InFlightAnnounced: true,
			}
			if got, ok := sender.Connections(); !ok || !reflect.DeepEqual(got, want) {
				t.Errorf("connections %+v (known %v), want %+v with no entries", got, ok, want)
			}
		})
	}
}

// Ground: contract — a Target with a scheme is the caller's own resolver's: not resolved by us,
// all links dial it as given, and no authority of ours replaces the one it would have at N = 1.
func TestConnect_ASchemeTargetIsNotResolvedAndKeepsItsAuthority(t *testing.T) {
	const target = "passthrough:///bufnet"

	lookupFailed := func(context.Context, string) ([]string, error) {
		t.Error("lookup called for a target with a scheme")

		return nil, errors.New("not for a scheme")
	}

	b := newBackend(t, "bufnet", slowTarget{})

	one := mustConnect(t, 1, []*backend{b}, Options{Target: target, Lookup: lookupFailed})
	if _, err := one.Send(bounded(t), request(time.Now())); err != nil {
		t.Fatalf("send at one connection: %v", err)
	}

	two := mustConnect(t, 2, []*backend{b}, Options{Target: target, Lookup: lookupFailed})
	for range 2 {
		if _, err := two.Send(bounded(t), request(time.Now())); err != nil {
			t.Fatalf("send at two connections: %v", err)
		}
	}

	if got := two.Links(); !slices.Equal(got, []string{target, target}) {
		t.Errorf("links %v, want the target twice, as given", got)
	}

	seen := b.seenAuthorities()
	if len(seen) != 3 || seen[1] != seen[0] || seen[2] != seen[0] {
		t.Errorf("authorities %q, want the one connection's authority on every call of two", seen)
	}
}

// Ground: contract — an IP literal needs no lookup: every link dials it, and it is the one
// resolved address. The CLI's own targets are ip + port.
func TestConnect_AnIPLiteralIsOneAddressAndNotLookedUp(t *testing.T) {
	const addr = "127.0.0.1:7001"

	b := newBackend(t, addr, slowTarget{})

	sender := mustConnect(t, 2, []*backend{b}, Options{
		Target: addr,
		Lookup: func(context.Context, string) ([]string, error) {
			t.Error("lookup called for an IP literal")

			return nil, errors.New("not for an IP")
		},
	})

	if got := sender.Links(); !slices.Equal(got, []string{addr, addr}) {
		t.Errorf("links %v, want the address twice", got)
	}
	if n := len(b.lis.accepted()); n != 2 {
		t.Errorf("%d connections to the one address, want 2", n)
	}
	if conns, _ := sender.Connections(); !slices.Equal(conns.Resolved, []string{addr}) {
		t.Errorf("resolved %v, want the one address", conns.Resolved)
	}
}

// Ground: contract — a name that gives no address is a failed start that says which name.
func TestConnect_ANameWithNoAddressNamesItself(t *testing.T) {
	for _, tt := range []struct {
		name   string
		lookup func(context.Context, string) ([]string, error)
		cause  string
	}{
		{"error", func(context.Context, string) ([]string, error) { return nil, errors.New("no such host") }, "no such host"},
		{"none", func(context.Context, string) ([]string, error) { return nil, nil }, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := connectTo(t, 2, nil, Options{Lookup: tt.lookup})
			if err == nil {
				t.Fatal("connect succeeded, want a resolve error")
			}

			if want := "resolve backends.test"; !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), tt.cause) {
				t.Errorf("error %q, want it to contain %q and %q", err, want, tt.cause)
			}
		})
	}
}

// Ground: contract — one address refusing fails the start and names which connection of how
// many to which address; the error says the cause as at one connection.
func TestConnect_NamesTheConnectionThatFailed(t *testing.T) {
	const dead = "10.0.0.9:443"

	live := newBackend(t, "10.0.0.1:443", slowTarget{})

	_, err := connectTo(t, 2, []*backend{live}, Options{Lookup: lookup(live.addr, dead)})
	if err == nil {
		t.Fatal("connect succeeded with a dead address")
	}

	for _, want := range []string{"connect to " + name, "connection 2 of 2 to " + dead, "refused by the test"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q, want it to contain %q", err, want)
		}
	}
}

// Ground: contract — a start that fails leaves nothing open: the connections that did come up
// are closed, or a failed start leaks a connection per attempt.
func TestConnect_AFailedStartClosesTheConnectionsThatCameUp(t *testing.T) {
	live := newBackend(t, "10.0.0.1:443", slowTarget{})

	if _, err := connectTo(t, 2, []*backend{live}, Options{Lookup: lookup(live.addr, "10.0.0.9:443")}); err == nil {
		t.Fatal("connect succeeded with a dead address")
	}

	if n := live.allClosed(t); n != 1 {
		t.Errorf("%d connections to the live address, want the one that came up, closed", n)
	}
}

// Ground: contract — Close ends every connection, not the first, and the sender is closed after.
func TestClose_EndsEveryConnection(t *testing.T) {
	bs := pair(t, slowTarget{}, nil, nil)
	sender := mustConnect(t, 2, bs, Options{})

	if err := sender.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for _, b := range bs {
		if n := b.allClosed(t); n != 1 {
			t.Errorf("%d connections to %s, want one, closed", n, b.addr)
		}
	}

	if _, err := sender.Send(bounded(t), request(time.Now())); !errors.Is(err, ErrClosed) {
		t.Errorf("send after close: %v, want ErrClosed", err)
	}
}

// Ground: contract — Conn serves the reflection that resolves the methods before the run: it is
// the first connection's. Green on the stub by nature; "Conn is the last link's" turns it red.
func TestConn_AtSeveralConnectionsIsTheFirstOne(t *testing.T) {
	bs := pair(t, slowTarget{}, nil, nil)
	sender := mustConnect(t, 2, bs, Options{})

	conn := sender.Conn()
	if conn == nil {
		t.Fatal("conn nil after connect")
	}

	if err := conn.Invoke(bounded(t), checkMethod, &grpc_health_v1.HealthCheckRequest{}, &grpc_health_v1.HealthCheckResponse{}); err != nil {
		t.Fatalf("invoke: %v", err)
	}

	if bs[0].calls.Load() != 1 || bs[1].calls.Load() != 0 {
		t.Errorf("the call reached %d and %d times, want the first address only", bs[0].calls.Load(), bs[1].calls.Load())
	}
}

// --- the authority and TLS ------------------------------------------------

// selfSignedFor makes a certificate for one DNS name and no IP: a connection to an address
// passes only by the name it was told to verify.
func selfSignedFor(t *testing.T, dns string) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: dns}, DNSNames: []string{dns},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// Ground: contract — every link carries the name the user wrote as :authority, and under TLS
// verifies the certificate against it, though it dials an address. The certificate has the name
// and no IP, so a link that verified the address, or sent it as :authority, fails here. Mutation
// "drop WithAuthority" turns the plain case red.
func TestConnect_KeepsTheNameForAuthorityAndTLS(t *testing.T) {
	const host = "leettest.test"

	cert, pool := selfSignedFor(t, host)

	for _, tt := range []struct {
		name   string
		server []grpc.ServerOption
		opts   Options
	}{
		{"plain", nil, Options{}},
		{"tls", []grpc.ServerOption{grpc.Creds(credentials.NewServerTLSFromCert(&cert))}, Options{TLS: true, RootCAs: pool}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bs := []*backend{
				newBackend(t, "127.0.0.1:7001", slowTarget{}, tt.server...),
				newBackend(t, "127.0.0.1:7002", slowTarget{}, tt.server...),
			}
			tt.opts.Target = host + ":443"

			sender := mustConnect(t, 2, bs, tt.opts)

			for range 4 {
				if _, err := sender.Send(bounded(t), request(time.Now())); err != nil {
					t.Fatalf("send: %v", err)
				}
			}

			for _, b := range bs {
				seen := b.seenAuthorities()
				for _, a := range seen {
					if a != host+":443" {
						t.Errorf("%s saw :authority %q, want %q", b.addr, a, host+":443")
					}
				}
				if len(seen) != 2 {
					t.Errorf("%s saw %d calls, want 2", b.addr, len(seen))
				}
			}
		})
	}
}

// --- the calls ------------------------------------------------------------

// lateSender hands every call to the real sender with a deadline already past: the generator
// came too late, and the call must not touch the network. It notes the link each call got.
type lateSender struct {
	*Sender

	mu    sync.Mutex
	links map[time.Time]int
}

func (l *lateSender) Send(ctx context.Context, req engine.Request) (engine.Outcome, error) {
	req.Deadline = time.Now().Add(-time.Second)

	out, err := l.Sender.Send(ctx, req)

	l.mu.Lock()
	l.links[req.ScheduledAt] = out.Link
	l.mu.Unlock()

	return out, err
}

// Ground: contract — the connection of a call is picked first, before the deadline check: a call
// the generator handed over late is a call of its connection, in turn, or every late call lands
// on connection 1 and it looks worse than it is. Mutation "pick after the deadline check" turns
// it red.
func TestSend_ALateCallKeepsItsLink(t *testing.T) {
	bs := pair(t, slowTarget{}, nil, nil)
	late := &lateSender{Sender: mustConnect(t, 2, bs, Options{}), links: make(map[time.Time]int)}

	eng := load(t, late, 100, 100*time.Millisecond, time.Second)
	if err := eng.Run(bounded(t)); err != nil {
		t.Fatalf("run: %v", err)
	}

	var scheduled []time.Time
	for at := range late.links {
		scheduled = append(scheduled, at)
	}

	slices.SortFunc(scheduled, func(a, b time.Time) int { return a.Compare(b) })

	var links []int
	for _, at := range scheduled {
		links = append(links, late.links[at])
	}

	if want := []int{0, 1, 0, 1, 0, 1, 0, 1, 0, 1}; !slices.Equal(links, want) {
		t.Errorf("links of the late calls in order %v, want %v", links, want)
	}

	each := eachOf(t, eng.Report(), 2)
	if each[0].Calls != 5 || each[1].Calls != 5 {
		t.Errorf("calls %d and %d, want 5 and 5: late calls are calls of the connection they were handed", each[0].Calls, each[1].Calls)
	}
}

// Ground: contract — each connection has its own stream limit and its own wait for a stream: a
// call never waits on another connection's free stream, nor is held by its full one. Two links
// announce 4 and 1; the fourth call, on the one-stream link, waits ~200 ms for the second.
// Mutation "one shared stream gauge" turns it red (Each[1].StreamWaited 0).
func TestConnections_EachConnectionHasItsOwnStreamLimit(t *testing.T) {
	bs := pair(t, slowTarget{delay: 200 * time.Millisecond},
		[]grpc.ServerOption{grpc.MaxConcurrentStreams(4)}, []grpc.ServerOption{grpc.MaxConcurrentStreams(1)})
	sender := mustConnect(t, 2, bs, Options{})

	eng := load(t, sender, 100, 40*time.Millisecond, 2*time.Second)
	if err := eng.Run(bounded(t)); err != nil {
		t.Fatalf("run: %v", err)
	}

	report := eng.Report()
	each := eachOf(t, report, 2)

	if each[0].StreamWaited != 0 || each[1].StreamWaited != 1 {
		t.Errorf("stream waited %d and %d, want 0 and 1: only the call behind the one-stream link's held stream waits",
			each[0].StreamWaited, each[1].StreamWaited)
	}
	if each[0].LastLimit != 4 || each[1].LastLimit != 1 || !each[0].LimitAnnounced || !each[1].LimitAnnounced {
		t.Errorf("limits %d and %d, want 4 and 1 as each target announced", each[0].LastLimit, each[1].LastLimit)
	}

	if c := report.Connections; c.InFlightLimit != 5 || !c.InFlightAnnounced {
		t.Errorf("in flight limit %d (announced %v), want 5, the connections' limits added", c.InFlightLimit, c.InFlightAnnounced)
	}
}

// Ground: contract — what blocks an unsent call is the connection it was sent on: link 2 full,
// link 1 free, the call on link 2 that expires is blocked on the stream. Mutation "one shared
// stream gauge", with the limit of link 1, turns it red.
func TestSend_AnUnsentCallIsBlamedOnItsOwnConnection(t *testing.T) {
	hold := &holdingTarget{entered: make(chan struct{}, 4), release: make(chan struct{})}
	t.Cleanup(func() { close(hold.release) })

	bs := []*backend{
		newBackend(t, "10.0.0.1:443", slowTarget{}),
		newBackend(t, "10.0.0.2:443", hold, grpc.MaxConcurrentStreams(1)),
	}
	sender := mustConnect(t, 2, bs, Options{})

	send := func() (engine.Outcome, error) { return sender.Send(bounded(t), request(time.Now())) }

	if _, err := send(); err != nil {
		t.Fatalf("send to link 1: %v", err)
	}

	go func() { _, _ = send() }()

	select {
	case <-hold.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the second call never held a stream on link 2")
	}

	if _, err := send(); err != nil {
		t.Fatalf("send to link 1 again: %v", err)
	}

	req := request(time.Now())
	req.Deadline = req.ScheduledAt.Add(100 * time.Millisecond)

	out, err := sender.Send(bounded(t), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if out.Link != 1 || !out.NotSent || out.NotSentOn != engine.BlockedOnStream {
		t.Errorf("link %d, not sent %v, blocked on %v; want link 1 blocked on its full stream", out.Link, out.NotSent, out.NotSentOn)
	}
}

// Ground: contract — different limits are not averaged into one: the scalar limit fields mean
// one connection's and say nothing at two, the sum is the in-flight bound, and a link that
// announced none makes the sum unknown instead of 0.
func TestConnections_DifferentLimitsAreNotAveraged(t *testing.T) {
	for _, tt := range []struct {
		name         string
		limit1       []grpc.ServerOption
		wantInFlight bool
	}{
		{"4 and 1", []grpc.ServerOption{grpc.MaxConcurrentStreams(1)}, true},
		{"4 and none", nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bs := pair(t, slowTarget{}, []grpc.ServerOption{grpc.MaxConcurrentStreams(4)}, tt.limit1)
			sender := mustConnect(t, 2, bs, Options{})

			got, ok := sender.Connections()
			if !ok || got.Open != 2 {
				t.Fatalf("connections %+v (known %v), want 2 open", got, ok)
			}

			if got.FirstLimit != 0 || got.LastLimit != 0 || got.LimitChanges != 0 {
				t.Errorf("first %d last %d changes %d, want 0: one connection's limit is not the run's", got.FirstLimit, got.LastLimit, got.LimitChanges)
			}
			if got.InFlightAnnounced != tt.wantInFlight || got.LimitAnnounced != tt.wantInFlight {
				t.Errorf("in flight announced %v, limit announced %v, want %v for both", got.InFlightAnnounced, got.LimitAnnounced, tt.wantInFlight)
			}
			if tt.wantInFlight && got.InFlightLimit != 5 {
				t.Errorf("in flight limit %d, want 5", got.InFlightLimit)
			}

			if len(got.Each) != 2 || !got.Each[0].LimitAnnounced || got.Each[0].LastLimit != 4 || got.Each[1].LimitAnnounced != tt.wantInFlight {
				t.Errorf("entries %+v, want the first announcing 4 and the second announcing %v", got.Each, tt.wantInFlight)
			}
		})
	}
}

// Ground: contract — the reconnects of the connections are added: one of two reconnected once.
func TestConnections_ReconnectsAreAddedOverTheConnections(t *testing.T) {
	bs := pair(t, slowTarget{}, nil, nil)
	sender := mustConnect(t, 2, bs, Options{})

	if got, _ := sender.Connections(); got.Reconnects != 0 {
		t.Fatalf("reconnects %d before any cut, want 0", got.Reconnects)
	}

	bs[1].lis.cut()

	// A connection that lost its transport dials again with the next call it is given.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	for ctx.Err() == nil {
		if got, _ := sender.Connections(); got.Reconnects == 1 {
			break
		}

		_, _ = sender.Send(ctx, request(time.Now()))
	}

	if got, _ := sender.Connections(); got.Open != 2 || got.Reconnects != 1 {
		t.Errorf("open %d reconnects %d, want 2 and 1: the second connection reconnected once", got.Open, got.Reconnects)
	}
}

// Ground: contract — credentials of the caller's mean no handshake is read, but there are still
// two connections, and the report names them and what they carried; no limit is made up.
func TestConnections_CustomCredsStillReportLinks(t *testing.T) {
	bs := pair(t, slowTarget{}, []grpc.ServerOption{grpc.MaxConcurrentStreams(7)}, []grpc.ServerOption{grpc.MaxConcurrentStreams(7)})
	sender := mustConnect(t, 2, bs, Options{
		DialOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})

	eng := load(t, sender, 100, 200*time.Millisecond, time.Second)
	if err := eng.Run(bounded(t)); err != nil {
		t.Fatalf("run: %v", err)
	}

	report := eng.Report()
	each := eachOf(t, report, 2)

	c := report.Connections
	if c.Open != 2 || !slices.Equal(c.Resolved, addrsOf(bs)) {
		t.Errorf("open %d resolved %v, want 2 connections to %v", c.Open, c.Resolved, addrsOf(bs))
	}
	if c.LimitAnnounced || c.InFlightAnnounced || c.InFlightLimit != 0 {
		t.Errorf("connections %+v: no handshake was read, no limit may be named", *c)
	}

	for i, e := range each {
		if e.Address != bs[i].addr || e.Calls != 10 || e.LimitAnnounced || e.LastLimit != 0 {
			t.Errorf("entry %d: %+v, want %s with 10 calls and no limit", i, e, bs[i].addr)
		}
	}
}

// Ground: contract — a run cut short by Ctrl-C ends every call in flight at once; each is
// recorded on the connection it was on, so a healthy backend does not read as one that took the
// whole abort, and an abort is no failure of either. Mutations "Send's abort has no Link" and
// "the worker's rebuilt outcome drops Link" turn it red.
func TestConnections_AnAbortedCallKeepsItsLink(t *testing.T) {
	hold := &holdingTarget{entered: make(chan struct{}, 64), release: make(chan struct{})}
	bs := pair(t, hold, nil, nil)
	sender := mustConnect(t, 2, bs, Options{})

	eng := load(t, sender, 100, 100*time.Millisecond, 5*time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- eng.Run(ctx) }()

	limit := time.After(5 * time.Second)
	for range 10 {
		select {
		case <-hold.entered:
		case <-limit:
			t.Fatal("fewer than 10 calls reached the targets")
		}
	}

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run after the abort: %v, want context.Canceled", err)
		}
	case <-limit:
		t.Fatal("the run did not end after the abort")
	}

	each := eachOf(t, eng.Report(), 2)
	if each[0].Calls != 5 || each[1].Calls != 5 {
		t.Errorf("calls %d and %d, want 5 and 5: round robin split the ten calls in flight", each[0].Calls, each[1].Calls)
	}
	if each[0].Failed != 0 || each[1].Failed != 0 {
		t.Errorf("failed %d and %d, want 0 and 0: an abort is not a failure of the connection", each[0].Failed, each[1].Failed)
	}
}
