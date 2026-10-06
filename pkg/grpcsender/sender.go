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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// rawCall moves bytes through a call untouched, with no size check: the
// connection probe's. Calls of the run use Sender.call. Both are set per call,
// not on the connection: reflection shares the connection and needs the proto
// codec. A ready slice passed with ... costs no allocation per call, a fresh
// variadic one escapes to the heap.
var rawCall = []grpc.CallOption{grpc.ForceCodec(rawCodec{})}

var (
	ErrNotConnected = errors.New("sender is not connected, call Connect before the run")
	ErrClosed       = errors.New("sender is closed")
	// ErrMaxResponseOutOfRange is a MaxResponseBytes grpc-go cannot hold: its
	// limits are int32.
	ErrMaxResponseOutOfRange = errors.New("max response size must be between 0 and 2147483647 bytes")
	// ErrClosedAfterHandshake is a target that completed the TLS handshake and
	// closed before its first SETTINGS: under TLS 1.3 that is how a refused
	// client certificate looks from the client.
	ErrClosedAfterHandshake = errors.New("the target closed the connection right after the TLS handshake; " +
		"it may require a client certificate or refuse the one given")
	// ErrIdleShorterThanConnect is an IdleTimeout shorter than the time
	// Connect has: the idle timer would cut the first dial before it ends.
	ErrIdleShorterThanConnect = errors.New("idle timeout is shorter than the time to connect")
)

// Options describes the target and how to reach it.
type Options struct {
	// Target is the address to dial, as host:port.
	Target string
	// TLS turns on transport credentials; without it the connection is
	// insecure, which is the usual case for a service behind a mesh.
	TLS bool
	// RootCAs verifies the target under TLS; nil means the system pool.
	RootCAs *x509.CertPool
	// Certificates are presented to a target that asks for a client
	// certificate (mutual TLS). Used only with TLS.
	Certificates []tls.Certificate
	// ServerName is checked against the target's certificate and sent as SNI
	// instead of the host in Target; :authority stays Target's. Used only with
	// TLS.
	ServerName string
	// Metadata is sent with every call, such as authorization or x-api-key.
	// Keys must already be lowercase; nil adds nothing to a call.
	Metadata map[string]string
	// MaxResponseBytes raises or lowers the largest reply a call accepts;
	// 0 keeps grpc-go's default of 4 MiB, above math.MaxInt32 fails Connect.
	// A larger reply fails the call as a bad response with
	// ErrResponseTooLarge. A call in flight may buffer up to twice this.
	MaxResponseBytes int
	// IdleTimeout closes the connection after this long without calls
	// (grpc.WithIdleTimeout); 0 keeps grpc-go's default. Connect refuses one
	// shorter than the time its ctx leaves it, or set without a deadline:
	// the idle timer would cut the first dial before it ends, and Connect
	// would hang to its deadline. An idle timeout given in DialOptions is
	// not seen by this check; with both set, this one is applied after
	// DialOptions and wins.
	IdleTimeout time.Duration
	// DialOptions are passed through for cases the fields above do not cover,
	// such as custom credentials or an in-process dialer in tests. Custom
	// transport credentials replace the sender's own, which read the stream
	// limit the target announces; Connections then reports nothing.
	//
	// The service config the target hands out through its resolver is
	// ignored: the sender keeps one connection to one address, pick_first,
	// and with several addresses in DNS loads that one backend only. A
	// config given here with grpc.WithDefaultServiceConfig still applies:
	// that is the caller's own choice, and the CLI offers none. With a load
	// balancing policy other than pick_first the connection lines and the
	// "limited by the run" verdict are wrong: they assume one connection.
	DialOptions []grpc.DialOption
}

// Sender delivers calls to a real gRPC target.
type Sender struct {
	opts Options

	mu     sync.RWMutex
	conn   *grpc.ClientConn
	closed bool

	tracker *connTracker
	ready   readyWindow
	// streams tells a wait for a stream from a delay on our side.
	streams *streamGauge
	// stopWatch ends the connection watcher; watched closes when it has.
	stopWatch context.CancelFunc
	watched   chan struct{}
	// limit is the largest reply a call accepts, and call the options that
	// check it: both set by Connect.
	limit int
	call  []grpc.CallOption
}

// checkIdle refuses an idle timeout shorter than the time ctx leaves Connect:
// equal or longer, Connect gives up first and the idle timer never cuts its
// dial.
func checkIdle(ctx context.Context, idle time.Duration) error {
	if idle <= 0 {
		return nil
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return fmt.Errorf("%w: idle timeout %v, but connect has no deadline; "+
			"set a connect timeout or drop the idle timeout", ErrIdleShorterThanConnect, idle)
	}
	if left := time.Until(deadline); idle < left {
		return fmt.Errorf("%w: idle timeout %v, connect timeout %v; "+
			"raise the idle timeout or shorten the connect timeout", ErrIdleShorterThanConnect, idle, ceilMillis(left))
	}

	return nil
}

// ceilMillis rounds d up to a whole millisecond: what is left of a timeout
// set in milliseconds reads back as that timeout while under 1ms has passed.
func ceilMillis(d time.Duration) time.Duration {
	if r := d % time.Millisecond; r > 0 {
		return d - r + time.Millisecond
	}

	return d
}

// defaultMaxResponse is grpc-go's own default, kept when no limit is given.
const defaultMaxResponse = 4 << 20

// New prepares a sender. It does not dial: the connection is established by
// Connect, before the run starts, so a wrong address fails immediately rather
// than a minute into the load.
func New(opts Options) *Sender {
	tracker := &connTracker{}

	return &Sender{opts: opts, tracker: tracker, streams: &streamGauge{limit: tracker.limit}}
}

// Connect establishes the connection and waits for it to become usable, so an
// unreachable target is reported before any load is scheduled rather than as a
// wall of failures once the run is under way.
//
// The first failed attempt is an error: a refused connection, a name that does
// not resolve, a failed handshake. ctx only bounds a target that neither
// answers nor refuses.
func (s *Sender) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}
	if s.conn != nil {
		return nil
	}

	creds := insecure.NewCredentials()
	if s.opts.TLS {
		creds = credentials.NewTLS(tlsConfig(s.opts))
	}

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(trackingCreds{TransportCredentials: creds, tracker: s.tracker, serverName: s.opts.ServerName}),
		grpc.WithStatsHandler(handler{streams: s.streams}),
		// A retry by a service config policy may follow an attempt the target
		// served, and would count one call for several. Transparent retries
		// stay: they follow only attempts the target never processed.
		grpc.WithDisableRetry(),
		// The target's service config would change what is measured: more
		// connections, a shorter deadline, calls held on a failed connection,
		// a reply limit of its own. See Options.DialOptions.
		grpc.WithDisableServiceConfig(),
	}
	if len(s.opts.Metadata) > 0 {
		// Per-RPC credentials rather than a context per call: gRPC attaches
		// them itself, and a run without metadata has nothing in the send path.
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(staticMetadata(s.opts.Metadata)))
	}
	limit := s.opts.MaxResponseBytes
	if limit == 0 {
		limit = defaultMaxResponse
	}
	if limit < 0 || limit > math.MaxInt32 {
		return fmt.Errorf("%w: %d", ErrMaxResponseOutOfRange, limit)
	}
	if err := checkIdle(ctx, s.opts.IdleTimeout); err != nil {
		return err
	}
	dialOpts = append(dialOpts, s.opts.DialOptions...)
	if s.opts.IdleTimeout > 0 {
		dialOpts = append(dialOpts, grpc.WithIdleTimeout(s.opts.IdleTimeout))
	}

	conn, err := grpc.NewClient(s.opts.Target, dialOpts...)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", s.opts.Target, err)
	}
	s.limit = limit
	// Our codec refuses a reply over the limit; grpc-go's own limit is set
	// twice as high, so it only bounds the memory a reply can take. Past it
	// grpc-go refuses first, in words that also mean the target refused our
	// request. Per call rather than as the connection's default: a default
	// costs grpc-go an allocation per call to merge with these.
	s.call = []grpc.CallOption{
		grpc.ForceCodec(&rawCodec{limit: limit}),
		grpc.MaxCallRecvMsgSize(int(min(2*int64(limit), math.MaxInt32))),
	}

	if err := waitReady(ctx, conn); err != nil {
		_ = conn.Close()

		if s.opts.TLS && s.tracker.handshookSilently() {
			return fmt.Errorf("connect to %s: %w: %w", s.opts.Target, ErrClosedAfterHandshake, err)
		}

		return fmt.Errorf("connect to %s: %w", s.opts.Target, err)
	}

	s.conn = conn

	// Recorded here, not by the watcher: the run starts the moment Connect
	// returns, before the watcher's goroutine may have run.
	s.ready.entered(time.Now())

	watchCtx, stop := context.WithCancel(context.Background())
	s.stopWatch, s.watched = stop, make(chan struct{})

	go s.watch(watchCtx, conn, s.watched)

	return nil
}

// watch records the connection's changes of state from READY, which Connect
// has already recorded, until ctx ends or the connection shuts down.
func (s *Sender) watch(ctx context.Context, conn *grpc.ClientConn, done chan<- struct{}) {
	defer close(done)

	for state := connectivity.Ready; conn.WaitForStateChange(ctx, state); {
		state = conn.GetState()

		switch state {
		case connectivity.Shutdown:
			return
		case connectivity.Ready:
			s.ready.entered(time.Now())
		default:
			s.ready.left(time.Now())
		}
	}
}

// waitReady blocks until the connection is usable, the first attempt fails, or
// ctx runs out. Waiting past a failure would wait forever: grpc-go backs off and
// retries an unreachable target without end.
func waitReady(ctx context.Context, conn *grpc.ClientConn) error {
	conn.Connect()

	for {
		state := conn.GetState()

		switch state {
		case connectivity.Ready:
			return nil
		case connectivity.TransientFailure:
			if cause := transportCause(ctx, conn); cause != nil {
				return cause
			}

			continue
		case connectivity.Shutdown:
			return ErrClosed
		}

		if !conn.WaitForStateChange(ctx, state) {
			return ctx.Err()
		}
	}
}

// probeMethod is a path no service implements. The probe must not be able to
// do anything if it does reach a target.
const probeMethod = "/leettest.v0.Probe/DoesNotExist"

// transportCause asks the connection why it failed, or returns nil if it works
// after all. grpc-go has no public accessor for the last connection error, but
// a call that does not wait for readiness fails at once with that error's text.
func transportCause(ctx context.Context, conn *grpc.ClientConn) error {
	empty := []byte{}
	probe := &callStats{}

	err := conn.Invoke(context.WithValue(ctx, callKey{}, probe), probeMethod, &empty, &discarded{}, rawCall...)
	if err == nil || probe.read().answered {
		// The connection came up between the failure and the probe. Any status
		// from the target proves it, not only Unimplemented: a service that
		// checks credentials before routing says Unauthenticated instead.
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	return errors.New(status.Convert(err).Message())
}

// Close releases the connection. Calling it twice is safe.
func (s *Sender) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true

	if s.conn == nil {
		return nil
	}

	conn := s.conn
	s.conn = nil

	err := conn.Close()

	s.stopWatch()
	<-s.watched

	return err
}

// Send performs one call. See engine.Sender for what the two failure channels
// mean: a returned error says the sender is unusable, while a non-success
// category inside the outcome is data about the target.
func (s *Sender) Send(ctx context.Context, req engine.Request) (engine.Outcome, error) {
	s.mu.RLock()
	conn, closed, callOpts, limit := s.conn, s.closed, s.call, s.limit
	s.mu.RUnlock()

	switch {
	case closed:
		return engine.Outcome{}, ErrClosed
	case conn == nil:
		return engine.Outcome{}, ErrNotConnected
	}

	// A deadline already in the past is a timeout without touching the network:
	// the call had no budget left by the time it reached the sender.
	if !req.Deadline.IsZero() && !time.Now().Before(req.Deadline) {
		now := time.Now()

		// Nothing on the connection's side had a chance to hold it: the
		// generator handed it over too late.
		return engine.Outcome{
			SentAt:    now,
			NotSent:   true,
			NotSentOn: engine.BlockedOnGenerator,
			DoneAt:    now,
			Category:  engine.CategoryTimeout,
			Code:      codes.DeadlineExceeded.String(),
			Err:       context.DeadlineExceeded,
		}, nil
	}

	call := &callStats{}
	callCtx := context.WithValue(ctx, callKey{}, call)

	if !req.Deadline.IsZero() {
		var cancel context.CancelFunc

		// Applied as given: recomputing it from now would hand a request that
		// queued for two seconds a fresh full budget.
		callCtx, cancel = context.WithDeadline(callCtx, req.Deadline)
		defer cancel()
	}

	payload := req.Payload
	if req.KeepResponse {
		call.reply.body = new([]byte)
	}

	// Before Invoke: grpc-go waits for the resolver before its first Begin.
	call.times.invokedAt = time.Now()
	err := conn.Invoke(callCtx, req.Method, &payload, &call.reply, callOpts...)

	// The run was stopped: not a broken sender, but nothing was measured either.
	// gRPC does not wrap ctx.Err(), so the wrapping happens here — without it the
	// engine cannot tell a deliberate stop from a failure, and Ctrl+C would end
	// the run with a non-zero exit code.
	if err != nil && ctx.Err() != nil {
		return engine.Outcome{}, fmt.Errorf("call aborted with %s: %w", status.Code(err), ctx.Err())
	}

	times := call.read()
	category := categorize(err, times.answered, !times.sentAt.IsZero())
	// A refused stream's UNAVAILABLE is grpc-go's word for the target's
	// REFUSED_STREAM: the refusal came from the target.
	code, fromTarget := status.Code(err), (times.answered && !refusedReply(err, times.answered)) || refusedStream(err)
	if err != nil && call.reply.over {
		// Our codec refused the reply: whatever status the target sent after
		// it, and whether it had arrived yet, the call is ours to fail.
		err = fmt.Errorf("%w: %d bytes, the limit is %d", ErrResponseTooLarge, call.reply.size, limit)
		category, code, fromTarget = engine.CategoryBadResponse, codes.ResourceExhausted, false
	}
	if echoOfOurDeadline(code, times, req.Deadline) {
		// The target answered our RST at the deadline with CANCELLED, and its
		// trailer beat our own handling of the deadline: the call timed out.
		category, fromTarget = engine.CategoryTimeout, false
	}
	sentAt, doneAt, notSent := timestamps(times, category)

	outcome := engine.Outcome{
		SentAt:     sentAt,
		NotSent:    notSent,
		StreamWait: times.streamWait(),
		ConnWait:   times.connWait,
		DoneAt:     doneAt,
		Category:   category,
		Code:       code.String(),
		Err:        err,

		CodeFromTarget: fromTarget,
		// A trailer that only copies or echoes our deadline is grpc-go on the
		// target answering by itself: it does not show the target alive.
		Heard: times.heard || call.reply.over || refusedStream(err) ||
			(times.answered && !expiredCopy(code, times, req.Deadline) && !echoOfOurDeadline(code, times, req.Deadline)),
	}
	if notSent {
		outcome.NotSentOn = s.blocker(conn, times)
	}
	if call.reply.body != nil {
		outcome.Response = *call.reply.body
	}

	return outcome, nil
}

// echoOfOurDeadline reports whether a CANCELLED from the target is its answer
// to our own cancel at the deadline: it arrived no earlier than the deadline.
// The target sends one only after our RST, so no threshold short of the
// deadline is needed, and one would take the target's own late cancel for an
// echo. The run stopping is the other cancel of ours; Send has returned by
// then.
func echoOfOurDeadline(code codes.Code, times callTimes, deadline time.Time) bool {
	return code == codes.Canceled && times.answered && !deadline.IsZero() && !times.answeredAt.Before(deadline)
}

// expiredCopy reports whether a trailer is only the target's copy of our
// deadline running out: DEADLINE_EXCEEDED arriving at or after 90% of the
// budget the call went out with. grpc-go on the target sends it by itself,
// frozen application or not, so it does not show the target alive. One that
// came earlier is the target's own answer, such as a dependency timing out.
func expiredCopy(code codes.Code, times callTimes, deadline time.Time) bool {
	if code != codes.DeadlineExceeded || deadline.IsZero() || times.sentAt.IsZero() {
		return code == codes.DeadlineExceeded
	}

	budget := deadline.Sub(times.sentAt)

	return !times.answeredAt.Before(times.sentAt.Add(budget * 9 / 10))
}

// Conn is the connection calls go through, for resolving method schemas over
// it before the run. Nil before Connect.
func (s *Sender) Conn() grpc.ClientConnInterface {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.conn
}

// timestamps picks the outcome's SentAt and DoneAt. A timeout whose body never
// went out gets a SentAt anyway: left zero, the pool would fall back to the
// moment Send was called and book the whole wait as the target's service time.
// Without headers the stream was never opened and the deadline expired waiting
// for stream quota, so nothing counts as service; with headers the target saw
// the stream and the rest is its own doing, such as a closed flow-control
// window. An outcome other than unreachable whose body was not reported sent —
// the target answered before it was written — takes headerAt: the moment the
// stream was granted, not a moment anything reached the wire (grpc-go reports
// OutHeader before the headers are queued). Unreachable keeps no SentAt:
// headerAt does not prove anything left.
func timestamps(call callTimes, category engine.Category) (sentAt, doneAt time.Time, notSent bool) {
	sentAt, doneAt = call.sentAt, call.doneAt
	if sentAt.IsZero() && category != engine.CategoryTimeout && category != engine.CategoryUnreachable {
		sentAt = call.headerAt
	}
	if category != engine.CategoryTimeout || !sentAt.IsZero() {
		return sentAt, doneAt, false
	}

	if doneAt.IsZero() {
		doneAt = time.Now()
	}
	if !call.headerAt.IsZero() {
		return call.headerAt, doneAt, false
	}

	return doneAt, doneAt, true
}

// blocker says what an unsent call waited for. A connection not ready for all
// of the call, or not ready now in case the watcher is behind, is the
// connection. On a ready one it is a stream only if the connection was full at
// some moment of the wait; otherwise the delay was ours.
func (s *Sender) blocker(conn *grpc.ClientConn, t callTimes) engine.Blocker {
	switch {
	case t.begunAt.IsZero() || !s.ready.throughout(t.begunAt) || conn.GetState() != connectivity.Ready:
		return engine.BlockedOnConnection
	case s.streams.fullSince(t.waitFrom()):
		return engine.BlockedOnStream
	default:
		return engine.BlockedOnGenerator
	}
}

// staticMetadata is the same metadata for every call.
type staticMetadata map[string]string

func (m staticMetadata) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return m, nil
}

// RequireTransportSecurity is false: a plaintext target behind a mesh takes
// tokens too, and refusing them would be a rule the user has to learn.
func (staticMetadata) RequireTransportSecurity() bool { return false }

// tlsConfig is the one place TLS is configured. Verification stays on: no
// option here turns InsecureSkipVerify on, and a nil RootCAs is the system
// pool while a given one replaces it.
func tlsConfig(opts Options) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      opts.RootCAs,
		Certificates: opts.Certificates,
	}
}
