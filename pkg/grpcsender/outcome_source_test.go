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
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// A call that went out and got no status back reached the other end: it was
// cut off, not unreachable. "Unreachable" says the target never saw the call;
// for a write that it may have processed, that is the wrong thing to say.
//
// Ground: contract — engine.CategoryCutOff is public.
func TestSend_WentOutNoStatusIsCutOffNotUnreachable(t *testing.T) {
	cases := []struct {
		name string
		send func(t *testing.T) engine.Outcome
		code codes.Code
		want engine.Category
	}{
		{"stream reset with INTERNAL_ERROR", func(t *testing.T) engine.Outcome {
			return sendWithin(t, rawTargetOnData(t, func(fr *http2.Framer, id uint32) bool {
				_ = fr.WriteRSTStream(id, http2.ErrCodeInternal)

				return false
			}), 0)
		}, codes.Internal, engine.CategoryCutOff},
		// The other end took the request and dropped the connection.
		{"connection dropped after the request went out", func(t *testing.T) engine.Outcome {
			return sendWithin(t, rawTarget(t, func(*http2.Framer, uint32, int) {}, true), time.Second)
		}, codes.Unavailable, engine.CategoryCutOff},
		// The connection was gone before anything was written: nothing reached anyone.
		{"nothing answered", func(t *testing.T) engine.Outcome {
			return sendWithin(t, vanishedTarget(t), 0)
		}, codes.Unavailable, engine.CategoryUnreachable},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := c.send(t)
			if out.Code != c.code.String() {
				t.Fatalf("code = %q, want %q: the case no longer pins its code", out.Code, c.code)
			}
			if out.Category != c.want {
				t.Errorf("category = %v, want %v (sent at %v)", out.Category, c.want, out.SentAt)
			}
		})
	}
}

// The outcome and the code's source never contradict each other, for every
// code and every way a call can end. Unreachable and cut off mean no status came
// back, so their code is never the target's; overload and server fault are
// statuses that came back, so theirs always is. A later change to categorize
// that breaks this would put "unreachable" next to "sent by the target" again.
func TestCategorize_OutcomeAgreesWithTheCodesSource(t *testing.T) {
	sizeCut := status.Error(codes.ResourceExhausted, "grpc: received message larger than max (5 vs. 4)")

	for code := codes.Canceled; code <= codes.Unauthenticated; code++ {
		for _, size := range []bool{false, true} {
			if size && code != codes.ResourceExhausted {
				continue
			}
			err := status.Error(code, "x")
			if size {
				err = sizeCut
			}
			for _, answered := range []bool{true, false} {
				for _, wentOut := range []bool{true, false} {
					name := fmt.Sprintf("%v/answered=%v/wentOut=%v/size=%v", code, answered, wentOut, size)
					got := categorize(err, answered, wentOut)

					switch got {
					case engine.CategoryUnreachable, engine.CategoryCutOff:
						if answered {
							t.Errorf("%s: %v, but a status came back", name, got)
						}
					case engine.CategoryOverload, engine.CategoryServerFault:
						if !answered {
							t.Errorf("%s: %v, but no status came back", name, got)
						}
					}
					if got == engine.CategoryUnreachable && wentOut {
						t.Errorf("%s: unreachable, but the request went out", name)
					}
					if !answered && wentOut && code != codes.DeadlineExceeded && !size && got != engine.CategoryCutOff {
						t.Errorf("%s: %v, want cut off: went out, no status came back", name, got)
					}
				}
			}
		}
	}
}

// refusingAfterPayload refuses streams with REFUSED_STREAM once their DATA has
// arrived, so the attempt's OutPayload has fired. The first connection refuses
// its first stream; with once set it then hangs up and the listener is gone,
// so the transparent retry fails before writing anything. Without once every
// stream is refused. The record says what reached each side before the first
// refusal, so a test can check its scenario held.
func refusingAfterPayload(t *testing.T, once bool) (*Sender, *refusalRecord) {
	t.Helper()

	return refusing(t, once, nil)
}

// always refuses every stream on its HEADERS.
func always() bool { return true }

// refusing is refusingAfterPayload, or with onHeaders a stand that decides on
// each stream's HEADERS, before any DATA: refuse it, or leave it hanging.
func refusing(t *testing.T, once bool, onHeaders func() bool) (*Sender, *refusalRecord) {
	t.Helper()

	rec := &refusalRecord{}
	lis := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = lis.Close() })

	go func() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		if once {
			_ = lis.Close()
		}
		defer conn.Close()

		preface := make([]byte, len(http2.ClientPreface))
		if _, err := io.ReadFull(conn, preface); err != nil {
			return
		}
		fr := http2.NewFramer(conn, conn)
		if err := fr.WriteSettings(); err != nil {
			return
		}
		sawData := make(map[uint32]bool)
		for {
			f, err := fr.ReadFrame()
			if err != nil {
				return
			}
			// Recorded apart from the refusal, so a refusal moved before the
			// DATA shows in the record.
			if d, ok := f.(*http2.DataFrame); ok {
				sawData[d.StreamID] = true
			}
			switch f := f.(type) {
			case *http2.SettingsFrame:
				if !f.IsAck() {
					_ = fr.WriteSettingsAck()
				}
			case *http2.HeadersFrame:
				if onHeaders != nil && onHeaders() {
					rec.refused.Store(true)
					rec.refusals.Add(1)
					_ = fr.WriteRSTStream(f.StreamID, http2.ErrCodeRefusedStream)
				}
			case *http2.DataFrame:
				if onHeaders != nil {
					break
				}
				// GOAWAY first: the client stops opening streams on this
				// connection before it sees the refusal, so the retry cannot
				// be written here before the hang-up (5 in 300 on go1.25.0,
				// -race, 2 CPUs).
				if !rec.refused.Swap(true) {
					rec.dataBeforeRefusal.Store(sawData[f.StreamID])
				}
				rec.refusals.Add(1)
				if once {
					_ = fr.WriteGoAway(f.StreamID, http2.ErrCodeNo, nil)
				}
				_ = fr.WriteRSTStream(f.StreamID, http2.ErrCodeRefusedStream)
				if once {
					return
				}
			}
		}
	}()

	sender := New(Options{Target: "passthrough:///bufnet", DialOptions: []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithStatsHandler(rec),
	}})
	t.Cleanup(func() { _ = sender.Close() })
	if err := sender.Connect(bounded(t)); err != nil {
		t.Fatalf("connect: %v", err)
	}

	return sender, rec
}

// refusalRecord notes whether the stand got the first attempt's DATA and
// whether grpc-go reported its OutPayload before that attempt ended.
type refusalRecord struct {
	refused           atomic.Bool
	refusals          atomic.Int32
	dataBeforeRefusal atomic.Bool
	payloadBeforeEnd  atomic.Bool
	firstEnded        atomic.Bool
}

func (r *refusalRecord) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context { return ctx }
func (r *refusalRecord) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}
func (r *refusalRecord) HandleConn(context.Context, stats.ConnStats) {}

func (r *refusalRecord) HandleRPC(_ context.Context, s stats.RPCStats) {
	switch s.(type) {
	case *stats.OutPayload:
		if !r.firstEnded.Load() {
			r.payloadBeforeEnd.Store(true)
		}
	case *stats.End:
		r.firstEnded.Store(true)
	}
}

// As the state in #75, the last attempt decides whether the call went out: the
// first went out and was refused unprocessed, the retry never got written.
// A sticky "went out" would say the target may have processed a call that
// REFUSED_STREAM guarantees it did not.
func TestSend_WentOutIsTheLastAttempts(t *testing.T) {
	sender, rec := refusingAfterPayload(t, true)
	out := sendWithin(t, sender, time.Second)

	// The scenario: the first attempt went out — its DATA reached the stand and
	// grpc-go reported its payload — and only then was it refused.
	if !rec.dataBeforeRefusal.Load() || !rec.payloadBeforeEnd.Load() {
		t.Fatalf("DATA at the stand %v, OutPayload before the first End %v: the first attempt did not go out",
			rec.dataBeforeRefusal.Load(), rec.payloadBeforeEnd.Load())
	}
	if out.Category != engine.CategoryUnreachable {
		t.Errorf("category = %v (code %s), want unreachable: the retry went nowhere", out.Category, out.Code)
	}
}

// Ground: contract — REFUSED_STREAM on the last attempt means the target did
// not process the stream (RFC 9113 §8.7), so "cut off, may have processed" is
// untrue. It is the target refusing work: overload, a code sent by the target,
// as UNAVAILABLE from it is. grpc-go v1.84.0 says so only in the status text,
// "stream terminated by RST_STREAM with error code: REFUSED_STREAM"
// (internal/transport/http2_client.go:1318, the same since v1.20.0); this
// test runs against grpc-go itself, so a changed text fails it.
func TestSend_RefusedOnTheLastAttemptIsTheTargetsOverload(t *testing.T) {
	sender, _ := refusingAfterPayload(t, false)
	out := sendWithin(t, sender, time.Second)

	if out.Category != engine.CategoryOverload || !out.CodeFromTarget {
		t.Errorf("category = %v (code %s from the target %v, %v), want overload from the target",
			out.Category, out.Code, out.CodeFromTarget, out.Err)
	}
	if !out.Heard {
		t.Errorf("a refused stream is not heard: the target answered it")
	}
}

// A target that refuses streams is alive and saying no. In a run where it
// refuses every stream in one 100ms window and leaves every stream hanging in
// the next, each second has timeouts, and only the refusals keep it from
// reading as silent from the first.
func TestRun_ATargetThatRefusesStreamsIsNotSilent(t *testing.T) {
	start := time.Now()
	sender, _ := refusing(t, false, func() bool { return time.Since(start)/(100*time.Millisecond)%2 == 0 })
	eng, err := engine.New(engine.Options{
		Calls: []engine.Call{{
			Method: "grpc.health.v1.Health/Check", Timeout: 300 * time.Millisecond,
			Stages: []engine.Stage{{StartRPS: 50, TargetRPS: 50, Duration: 2 * time.Second}},
		}},
		Sender:      sender,
		MaxInFlight: 100,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	m := eng.Report().Methods[0]
	if m.SilentFrom != nil || m.TimedOut == 0 || m.Failed != m.Sent {
		silent := "none"
		if m.SilentFrom != nil {
			silent = fmt.Sprint(*m.SilentFrom)
		}
		t.Errorf("silent from %s, timed out %d, failed %d of %d; want none, some timed out, every call failed",
			silent, m.TimedOut, m.Failed, m.Sent)
	}
}

// The same refusal before any DATA, on the stream's HEADERS: grpc-go retries
// the first attempt transparently, the stand refuses the retry too, and the
// call lands where a refusal after the payload does.
//
// grpc-go may return a bare io.EOF after a transparent retry
// (https://github.com/grpc/grpc-go/issues/9443, seen in stress run
// 37291575003); such a call must never carry a code from the target.
func TestSend_RefusedOnItsHeadersIsTheTargetsOverloadToo(t *testing.T) {
	const calls = 5

	sender, rec := refusing(t, false, always)

	overload := 0
	for i := 1; i <= calls; i++ {
		before := rec.refusals.Load()
		out := sendWithin(t, sender, time.Second)

		switch {
		case out.Category == engine.CategoryOverload && out.CodeFromTarget && out.Heard:
			overload++
		case errors.Is(out.Err, io.EOF) && rec.refusals.Load()-before >= 2 && isLostStatus(out):
			t.Logf("call %d: bare EOF after a refused retry, grpc-go#9443", i)
		default:
			t.Errorf("call %d: category = %v (code %s from the target %v, heard %v, %v), want overload from the target, heard",
				i, out.Category, out.Code, out.CodeFromTarget, out.Heard, out.Err)
		}
	}

	if !rec.refused.Load() {
		t.Fatalf("the stand refused nothing")
	}
	if overload < 1 {
		t.Fatalf("all %d calls lost the status to grpc-go#9443", calls)
	}
}

// isLostStatus is what a call whose status grpc-go#9443 lost reports: the
// target was heard from, no status came back, the code is ours.
func isLostStatus(out engine.Outcome) bool {
	return out.Category == engine.CategoryCutOff && out.Heard && out.Code == "Unknown" && !out.CodeFromTarget
}

// pausedRetry holds the transparent retry's OutHeader until the stand reports
// that the client has processed the retry's answer, which makes grpc-go#9443
// certain instead of one in hundreds: the retry's stream is then gone before
// the caller goroutine goes on. The order is by construction, not by time: the
// stand follows its answer with a PING, grpc-go's reader answers it only after
// every frame before it, and the caller goroutine held here blocks neither the
// reader nor the writer.
type pausedRetry struct {
	retry     atomic.Bool
	processed chan struct{}
	never     atomic.Bool
}

func (*pausedRetry) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context { return ctx }
func (*pausedRetry) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}
func (*pausedRetry) HandleConn(context.Context, stats.ConnStats) {}

func (p *pausedRetry) HandleRPC(_ context.Context, s stats.RPCStats) {
	switch v := s.(type) {
	case *stats.Begin:
		p.retry.Store(v.IsTransparentRetryAttempt)
	case *stats.OutHeader:
		if !p.retry.Load() {
			return
		}
		select {
		case <-p.processed:
		case <-time.After(2 * time.Second):
			p.never.Store(true)
		}
	}
}

var lostStatusPings atomic.Uint64

// lostStatusStand serves one connection: the first stream is refused on its
// HEADERS, the second answered with answer (nil: refused too), and the answer
// is followed by a PING that p.processed waits for.
func lostStatusStand(conn net.Conn, p *pausedRetry, answer func(fr *http2.Framer, stream uint32)) {
	var payload [8]byte
	binary.BigEndian.PutUint64(payload[:], lostStatusPings.Add(1))
	var once sync.Once

	serveRawFrames(conn, func(fr *http2.Framer, stream uint32, n int) {
		if n > 1 && answer != nil {
			answer(fr, stream)
		} else {
			_ = fr.WriteRSTStream(stream, http2.ErrCodeRefusedStream)
		}
		if n > 1 {
			_ = fr.WritePing(false, payload)
		}
	}, nil, func(data [8]byte) {
		if data == payload {
			once.Do(func() { close(p.processed) })
		}
	})
}

// lostStatusCalls sends calls calls, each over a fresh connection to a stand
// that refuses the first stream on its HEADERS and answers the second with
// answer (nil: refuses it too), and returns what each came back as.
func lostStatusCalls(t *testing.T, calls int, answer func(fr *http2.Framer, stream uint32)) []engine.Outcome {
	t.Helper()

	var outs []engine.Outcome
	for i := range calls {
		lis := bufconn.Listen(1024 * 1024)
		t.Cleanup(func() { _ = lis.Close() })
		pause := &pausedRetry{processed: make(chan struct{})}
		go func() {
			for {
				conn, err := lis.Accept()
				if err != nil {
					return
				}
				go lostStatusStand(conn, pause, answer)
			}
		}()
		sender := New(Options{Target: "passthrough:///bufnet", DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
			grpc.WithStatsHandler(pause),
		}})
		t.Cleanup(func() { _ = sender.Close() })
		if err := sender.Connect(bounded(t)); err != nil {
			t.Fatalf("connect: %v", err)
		}
		out := sendWithin(t, sender, time.Second)
		if pause.never.Load() {
			t.Fatalf("call %d: the client never acknowledged the stand's PING after the retry's answer", i)
		}
		outs = append(outs, out)
	}

	return outs
}

// checkLostStatusCalls holds every call to one of two outcomes. grpc-go#9443
// is a race the stand makes the common case but cannot make certain: a call
// either comes back as a bare io.EOF and is "no status came back" (the code is
// not the target's), or grpc-go kept the status and the call is that status's
// own outcome, which isReal says. Anything else fails. At least one call must
// be the bare EOF, or grpc-go no longer loses the status.
func checkLostStatusCalls(t *testing.T, outs []engine.Outcome, isReal func(engine.Outcome) bool) {
	t.Helper()

	bare := 0
	for i, out := range outs {
		switch {
		case errors.Is(out.Err, io.EOF):
			bare++
			if !isLostStatus(out) {
				t.Errorf("call %d: bare EOF as category %v, code %s from the target %v, heard %v; want cut off, Unknown, not from the target, heard",
					i, out.Category, out.Code, out.CodeFromTarget, out.Heard)
			}
		case isReal(out):
		default:
			t.Errorf("call %d: neither a bare EOF nor the status's own outcome: category %v, code %s from the target %v, heard %v, err %v",
				i, out.Category, out.Code, out.CodeFromTarget, out.Heard, out.Err)
		}
	}
	t.Logf("bare EOF %d of %d", bare, len(outs))
	if bare == 0 {
		t.Fatalf("no call of %d came back as a bare EOF: grpc-go no longer returns a bare EOF here — #9443 fixed?", len(outs))
	}
}

// grpc-go#9443: a call whose retry was refused comes back as a bare io.EOF,
// the status lost. Nothing says what the target would have answered, so the
// call is "no status came back" and the code is not the target's. Before this
// it was the client's error and the target a silent one. When grpc-go keeps
// the status instead, the target refused the call: overload, from the target.
//
// Ground: signal grpc-go v1.84.0 — https://github.com/grpc/grpc-go/issues/9443:
// a bare io.EOF after a refused transparent retry, the status lost, in most
// calls. A run with no bare EOF at all says grpc-go fixed it.
func TestSend_AStatusLostAfterARefusedRetryIsNoStatus(t *testing.T) {
	checkLostStatusCalls(t, lostStatusCalls(t, 20, nil), func(out engine.Outcome) bool {
		return out.Category == engine.CategoryOverload && out.Code == codes.Unavailable.String() && out.CodeFromTarget && out.Heard
	})
}

// The retry answered with a status loses it the same way: it must not be read
// as the target's own answer (#75 does not let attempt 1 stand for it either).
// When grpc-go keeps the status, the call is that status's own outcome.
//
// Ground: signal grpc-go v1.84.0 — https://github.com/grpc/grpc-go/issues/9443:
// the same bare io.EOF when the retry was answered trailers-only (experiment
// of 2026-10-06), in most calls. A run with no bare EOF at all says grpc-go
// fixed it.
func TestSend_AStatusLostOnAnAnsweredRetryIsNotTheTargets(t *testing.T) {
	for _, tc := range []struct {
		name string
		code codes.Code
		real func(engine.Outcome) bool
	}{
		{"NOT_FOUND", codes.NotFound, func(out engine.Outcome) bool {
			return out.Category == engine.CategoryClientFault && out.Code == codes.NotFound.String() && out.CodeFromTarget
		}},
		// grpc-go's own "cardinality violation" on an OK with no message: the
		// same outcome the call has without #9443.
		{"OK without a reply", codes.OK, func(out engine.Outcome) bool {
			return out.Category == engine.CategoryServerFault && out.Code == codes.Internal.String() && out.CodeFromTarget
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outs := lostStatusCalls(t, 20, func(fr *http2.Framer, stream uint32) { trailersOnly(fr, stream, tc.code) })
			checkLostStatusCalls(t, outs, tc.real)
		})
	}
}

// The cell #85 lacked: a trailer came over the wire, it said OK, and the code
// in the error is one our client set on the reply it refused. The category is
// the client's, and so is the code's source.
func TestCategorize_ATrailerSayingOKWithAReplyTheClientRefused(t *testing.T) {
	for _, err := range []error{
		status.Error(codes.Internal, `grpc: Decompressor is not installed for grpc-encoding "gzip"`),
		status.Error(codes.Internal, "grpc: failed to decompress the received message: gzip: invalid header"),
	} {
		if got := categorize(err, true, true); got != engine.CategoryBadResponse {
			t.Errorf("%v with a trailer: category %v, want a bad response", err, got)
		}
		if !refusedReply(err, true) {
			t.Errorf("%v with a trailer: not seen as the client's, so the code would read as the target's", err)
		}
	}
}
