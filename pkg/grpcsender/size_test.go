package grpcsender

import (
	"context"
	"errors"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/test/bufconn"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// dialWith serves target under srvOpts and connects with callOpts, so a test
// can put a message-size limit on either side.
func dialWith(t testing.TB, srvOpts []grpc.ServerOption, callOpts ...grpc.DialOption) *Sender {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer(srvOpts...)
	grpc_health_v1.RegisterHealthServer(srv, &target{})

	go func() { _ = srv.Serve(lis) }()

	dial := append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	}, callOpts...)

	sender := New(Options{Target: "passthrough:///bufnet", DialOptions: dial})
	if err := sender.Connect(t.Context()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = sender.Close()
		srv.Stop()
	})

	return sender
}

const replyLimit = 1024

// trailerFirst holds the caller after its request goes out until the target's
// trailer has been read, so the reply is refused with the status already in:
// the order the stand sets, not the scheduler.
type trailerFirst struct {
	t       *testing.T
	trailer chan struct{}
	once    sync.Once
}

func (h *trailerFirst) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context { return ctx }
func (h *trailerFirst) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}
func (h *trailerFirst) HandleConn(context.Context, stats.ConnStats) {}

func (h *trailerFirst) HandleRPC(_ context.Context, s stats.RPCStats) {
	switch s.(type) {
	case *stats.InTrailer:
		h.once.Do(func() { close(h.trailer) })
	case *stats.OutPayload:
		select {
		case <-h.trailer:
		case <-time.After(5 * time.Second):
			h.t.Error("the target's trailer never arrived: the stand did not set the order")
		}
	}
}

// answeredAtReturn records what the sender will see of the trailer once
// Invoke returns: the precondition each order test states.
func answeredAtReturn(seen *bool) grpc.DialOption {
	return grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		err := invoker(ctx, method, req, reply, cc, opts...)
		if call, ok := ctx.Value(callKey{}).(*callStats); ok {
			*seen = call.read().answered
		}

		return err
	})
}

// replyingThenHolding answers with size bytes and holds the trailer until
// release is closed.
func replyingThenHolding(size int, release <-chan struct{}) grpc.ServerOption {
	return grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		var in []byte
		if err := stream.RecvMsg(&in); err != nil {
			return err
		}

		out := make([]byte, size)
		if err := stream.SendMsg(&out); err != nil {
			return err
		}

		select {
		case <-release:
		case <-stream.Context().Done():
		}

		return nil
	})
}

func bigRequest() engine.Request {
	req := request(time.Now())
	req.Method = "/leettest.test.Big/Get"

	return req
}

func refusedByUs(t *testing.T, out engine.Outcome) {
	t.Helper()

	if out.Category != engine.CategoryBadResponse {
		t.Errorf("category = %v, want %v: the reply came and our limit refused it (%v)", out.Category, engine.CategoryBadResponse, out.Err)
	}
	if out.CodeFromTarget {
		t.Errorf("code from target: %s was set by our client, not sent by the target", out.Code)
	}
	if !errors.Is(out.Err, ErrResponseTooLarge) {
		t.Errorf("error = %v, want ErrResponseTooLarge", out.Err)
	}
	if out.Code != codes.ResourceExhausted.String() {
		t.Errorf("code = %s, want %s: the report lists it among the codes the client set", out.Code, codes.ResourceExhausted)
	}
}

// Ground: concurrency — grpc-go reads the trailer on the transport's reader
// while the caller refuses the reply; with the trailer already in, a refusal
// of ours must not read as the target's.
func TestSend_AReplyOverTheLimitWithItsTrailerInIsABadResponse(t *testing.T) {
	opts := listen(t, &seeingTarget{}, grpc.ForceServerCodec(rawCodec{}), replying(replyLimit+1))
	opts.MaxResponseBytes = replyLimit
	var answered bool
	opts.DialOptions = append(opts.DialOptions,
		grpc.WithStatsHandler(&trailerFirst{t: t, trailer: make(chan struct{})}),
		answeredAtReturn(&answered))
	sender := connected(t, opts)

	out, err := sender.Send(bounded(t), bigRequest())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !answered {
		t.Fatal("the trailer was not in when Invoke returned: the test no longer covers this order")
	}

	refusedByUs(t, out)
}

// Ground: concurrency — the other order: the refusal comes while the target
// still holds its status.
func TestSend_AReplyOverTheLimitWithItsTrailerHeldIsABadResponse(t *testing.T) {
	release := make(chan struct{})
	opts := listen(t, &seeingTarget{}, grpc.ForceServerCodec(rawCodec{}), replyingThenHolding(replyLimit+1, release))
	t.Cleanup(func() { close(release) })
	opts.MaxResponseBytes = replyLimit
	answered := true
	opts.DialOptions = append(opts.DialOptions, answeredAtReturn(&answered))
	sender := connected(t, opts)

	out, err := sender.Send(bounded(t), bigRequest())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if answered {
		t.Fatal("the trailer was in when Invoke returned: the test no longer covers this order")
	}

	refusedByUs(t, out)
}

// Ground: boundary — grpc-go's own limit is twice ours, so a reply of exactly
// twice the limit still reaches our check.
func TestSend_AReplyOfTwiceTheLimitIsRefusedByUs(t *testing.T) {
	release := make(chan struct{})
	opts := listen(t, &seeingTarget{}, grpc.ForceServerCodec(rawCodec{}), replyingThenHolding(2*replyLimit, release))
	t.Cleanup(func() { close(release) })
	opts.MaxResponseBytes = replyLimit
	sender := connected(t, opts)

	out, err := sender.Send(bounded(t), bigRequest())
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	refusedByUs(t, out)
}

// Ground: signal grpc-go v1.84.0 — the known limit of the check: past twice
// our limit grpc-go refuses the reply itself, with the wording and the race
// the check exists to avoid.
func TestSend_AReplyOverTwiceTheLimitIsRefusedByGRPC(t *testing.T) {
	release := make(chan struct{})
	opts := listen(t, &seeingTarget{}, grpc.ForceServerCodec(rawCodec{}), replyingThenHolding(2*replyLimit+1, release))
	t.Cleanup(func() { close(release) })
	opts.MaxResponseBytes = replyLimit
	sender := connected(t, opts)

	out, err := sender.Send(bounded(t), bigRequest())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if errors.Is(out.Err, ErrResponseTooLarge) {
		t.Errorf("error = %v: grpc-go's limit is twice ours, past it the check never runs", out.Err)
	}
	if !strings.Contains(out.Err.Error(), sizeLimit) {
		t.Fatalf("error = %v, want grpc-go's size refusal: the test no longer pins that path", out.Err)
	}
	if out.Category != engine.CategoryBadResponse {
		t.Errorf("category = %v, want %v with the trailer held (%v)", out.Category, engine.CategoryBadResponse, out.Err)
	}
}

// Ground: contract — a decode target used again must not carry the refusal
// of the reply before: an over-limit reply, then one that fits.
func TestCodec_ARefusalDoesNotCarryOverToTheNextReply(t *testing.T) {
	codec := rawCodec{limit: 4}
	var r reply

	if err := codec.Unmarshal(make([]byte, 5), &r); !errors.Is(err, ErrResponseTooLarge) || !r.over {
		t.Fatalf("5 bytes over a 4-byte limit: err %v, over %v", err, r.over)
	}
	if err := codec.Unmarshal(make([]byte, 4), &r); err != nil || r.over {
		t.Errorf("4 bytes after the refusal: err %v, over %v; want accepted", err, r.over)
	}
}

// Ground: contract — Options.MaxResponseBytes is public, and grpc-go holds a
// limit in an int32: past it Connect refuses rather than let grpc-go's limit
// fall below ours.
func TestConnect_AMaxResponseOverInt32IsRefused(t *testing.T) {
	if strconv.IntSize == 32 {
		t.Skip("an int cannot exceed math.MaxInt32 here")
	}

	opts := listen(t, &seeingTarget{})
	opts.MaxResponseBytes = math.MaxInt32 + 1
	if err := New(opts).Connect(bounded(t)); !errors.Is(err, ErrMaxResponseOutOfRange) {
		t.Errorf("Connect = %v, want ErrMaxResponseOutOfRange", err)
	}

	opts.MaxResponseBytes = math.MaxInt32
	sender := New(opts)
	t.Cleanup(func() { _ = sender.Close() })
	if err := sender.Connect(bounded(t)); err != nil {
		t.Errorf("Connect at math.MaxInt32 = %v, want accepted", err)
	}
}

// Ground: boundary — a reply of exactly the limit fits.
func TestSend_AReplyOfExactlyTheLimitIsASuccess(t *testing.T) {
	if out := sendTo(t, replyLimit, replyLimit); out.Category != engine.CategorySuccess {
		t.Errorf("category = %v, want success (%v)", out.Category, out.Err)
	}
}

// Ground: boundary — an echo service: the request is over the target's limit
// and our reply limit is small too. The target refused before replying, so
// the check has nothing to see and the status is the target's.
func TestSend_ARequestOverTheTargetLimitStaysTheTargetsWithASmallReplyLimit(t *testing.T) {
	opts := listen(t, &seeingTarget{}, grpc.MaxRecvMsgSize(1))
	opts.MaxResponseBytes = 1
	sender := connected(t, opts)

	req := request(time.Now())
	req.Payload = []byte{0x0a, 0x04, 'b', 'i', 'g', '!'}

	out, err := sender.Send(bounded(t), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if out.Category != engine.CategoryClientFault {
		t.Errorf("category = %v, want %v (%v)", out.Category, engine.CategoryClientFault, out.Err)
	}
	if !out.CodeFromTarget {
		t.Errorf("code from target = false: the target sent %s", out.Code)
	}
	if errors.Is(out.Err, ErrResponseTooLarge) {
		t.Errorf("error = %v: no reply came, our check cannot have refused one", out.Err)
	}
}

// Ground: signal grpc-go v1.84.0 — a server over its own receive limit answers
// RESOURCE_EXHAUSTED, the same code a target out of capacity returns. The size
// message is what tells them apart, and the run's verdict depends on it: a
// request that does not fit will not fit at any rate.
func TestSend_ARequestOverTheServerLimitIsARequestError(t *testing.T) {
	sender := dialWith(t, []grpc.ServerOption{grpc.MaxRecvMsgSize(1)})

	// A health check with a service name of its own is longer than one byte.
	req := request(time.Now())
	req.Payload = []byte{0x0a, 0x04, 'b', 'i', 'g', '!'}

	out, err := sender.Send(t.Context(), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if out.Category != engine.CategoryClientFault {
		t.Errorf("category = %v, want %v (%v)", out.Category, engine.CategoryClientFault, out.Err)
	}
}

// Ground: boundary — the code that means both "out of capacity" and "does not
// fit": only the size limit is a request error.
func TestSend_PlainResourceExhaustedStaysOverload(t *testing.T) {
	sender := dialTarget(t, &target{code: codes.ResourceExhausted})

	out, err := sender.Send(context.Background(), request(time.Now()))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if out.Category != engine.CategoryOverload {
		t.Errorf("category = %v, want %v", out.Category, engine.CategoryOverload)
	}
}
