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
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// compressible is a Check request body of about 2 KB that gzip folds to a few
// dozen bytes.
func compressible(t *testing.T) []byte {
	t.Helper()

	body, err := proto.Marshal(&grpc_health_v1.HealthCheckRequest{Service: strings.Repeat("abcd", 500)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return body
}

// arrivals is a server stats handler: what each request came with, as the
// target saw it.
type arrivals struct {
	mu        sync.Mutex
	encodings []string
	pairs     []int
}

func (a *arrivals) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context { return ctx }
func (a *arrivals) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}
func (a *arrivals) HandleConn(context.Context, stats.ConnStats) {}

func (a *arrivals) HandleRPC(_ context.Context, s stats.RPCStats) {
	v, ok := s.(*stats.InHeader)
	if !ok {
		return
	}

	n := 0
	for _, vals := range v.Header {
		n += len(vals)
	}

	a.mu.Lock()
	a.encodings = append(a.encodings, v.Compression)
	a.pairs = append(a.pairs, n)
	a.mu.Unlock()
}

func send(t *testing.T, s *Sender, payload []byte) engine.Outcome {
	t.Helper()

	req := request(time.Now())
	req.Payload = payload
	out, err := s.Send(bounded(t), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	return out
}

// Ground: contract — Options.Compression "gzip" compresses every request
// body; the target sees grpc-encoding: gzip on each call, and the outcome
// carries the body before and after (stats.OutPayload Length and
// CompressedLength, grpc-go v1.84.0 rpc_util.go:897-905).
func TestCompression_GzipRequestsReachTheTargetCompressed(t *testing.T) {
	seen := &arrivals{}
	opts := listen(t, &seeingTarget{}, grpc.StatsHandler(seen))
	opts.Compression = "gzip"
	sender := connected(t, opts)
	body := compressible(t)

	for i := range 3 {
		out := send(t, sender, body)
		if out.Category != engine.CategorySuccess {
			t.Fatalf("call %d: category %v (%v)", i, out.Category, out.Err)
		}
		if out.RawBytes != len(body) {
			t.Errorf("call %d: RawBytes %d, want the message's %d", i, out.RawBytes, len(body))
		}
		if out.WireBytes <= 0 || out.WireBytes >= len(body)/4 {
			t.Errorf("call %d: WireBytes %d, want a gzip of %d repeated bytes: above 0, under a quarter", i, out.WireBytes, len(body))
		}
	}

	seen.mu.Lock()
	defer seen.mu.Unlock()
	if len(seen.encodings) != 3 {
		t.Fatalf("the target saw %d calls, want 3", len(seen.encodings))
	}
	for i, e := range seen.encodings {
		if e != "gzip" {
			t.Errorf("call %d: grpc-encoding %q, want gzip", i, e)
		}
	}
}

// Ground: contract — off means absent: no compressor on the call, no
// grpc-encoding, and the body is the same before and after.
func TestCompression_WithoutItNothingIsCompressed(t *testing.T) {
	seen := &arrivals{}
	sender := connected(t, listen(t, &seeingTarget{}, grpc.StatsHandler(seen)))
	body := compressible(t)

	out := send(t, sender, body)
	if out.Category != engine.CategorySuccess {
		t.Fatalf("category %v (%v)", out.Category, out.Err)
	}
	if out.RawBytes != len(body) || out.WireBytes != len(body) {
		t.Errorf("RawBytes %d, WireBytes %d, want both %d: nothing was compressed", out.RawBytes, out.WireBytes, len(body))
	}

	seen.mu.Lock()
	defer seen.mu.Unlock()
	if len(seen.encodings) != 1 || seen.encodings[0] != "" {
		t.Errorf("grpc-encoding %q, want none", seen.encodings)
	}
}

// Ground: signal grpc-go v1.84.0 — ghz#403 (a race and a crash with
// compression on): many goroutines over four connections, run under -race.
// Every call still arrives gzip-encoded, so the test cannot pass by the option
// being dropped.
func TestCompression_ManyGoroutinesOverFourConnections(t *testing.T) {
	seen := &arrivals{}
	bs := pair(t, &seeingTarget{}, []grpc.ServerOption{grpc.StatsHandler(seen)}, []grpc.ServerOption{grpc.StatsHandler(seen)})
	sender := mustConnect(t, 4, bs, Options{Compression: "gzip"})
	body := compressible(t)

	const goroutines, each = 8, 25

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range each {
				req := request(time.Now())
				req.Payload = body
				out, err := sender.Send(context.Background(), req)
				if err != nil || out.Category != engine.CategorySuccess {
					t.Errorf("send: category %v, err %v, outcome err %v", out.Category, err, out.Err)

					return
				}
			}
		})
	}
	wg.Wait()

	seen.mu.Lock()
	defer seen.mu.Unlock()
	if len(seen.encodings) != goroutines*each {
		t.Fatalf("the targets saw %d calls, want %d", len(seen.encodings), goroutines*each)
	}
	for i, e := range seen.encodings {
		if e != "gzip" {
			t.Fatalf("call %d: grpc-encoding %q, want gzip", i, e)
		}
	}
}

// Ground: signal ghz#441 — shared metadata that grew with every call under
// compression. The metadata a call carries has the same size on the first call
// and on the hundredth. A safety net: the sender touches no metadata, so a
// mutation cannot be written for it.
func TestCompression_TheMetadataDoesNotGrowWithCalls(t *testing.T) {
	seen := &arrivals{}
	opts := listen(t, &seeingTarget{}, grpc.StatsHandler(seen))
	opts.Compression = "gzip"
	opts.Metadata = map[string]string{"authorization": "Bearer abc", "x-api-key": "k"}
	sender := connected(t, opts)
	body := compressible(t)

	const calls = 100
	for range calls {
		send(t, sender, body)
	}

	seen.mu.Lock()
	defer seen.mu.Unlock()
	if len(seen.pairs) != calls {
		t.Fatalf("the target saw %d calls, want %d", len(seen.pairs), calls)
	}
	if seen.pairs[0] != seen.pairs[calls-1] {
		t.Errorf("metadata values on call 1: %d, on call %d: %d, want the same", seen.pairs[0], calls, seen.pairs[calls-1])
	}
}

// refusingGzip is a raw HTTP/2 target that does not know gzip. It records the
// request's grpc-encoding header and the compressed flag of the body's frame,
// and answers trailers-only UNIMPLEMENTED with the text grpc-go's own server
// uses (server.go:1402), once the body has arrived: answered on HEADERS, the
// status could beat the client's write of the body.
type refusingGzip struct {
	mu       sync.Mutex
	encoding string
	flag     int
}

func (r *refusingGzip) serve(conn net.Conn) {
	defer conn.Close()

	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return
	}

	fr := http2.NewFramer(conn, conn)
	if err := fr.WriteSettings(); err != nil {
		return
	}

	dec := hpack.NewDecoder(4096, func(f hpack.HeaderField) {
		if f.Name == "grpc-encoding" {
			r.mu.Lock()
			r.encoding = f.Value
			r.mu.Unlock()
		}
	})

	for {
		f, err := fr.ReadFrame()
		if err != nil {
			return
		}

		switch f := f.(type) {
		case *http2.SettingsFrame:
			if !f.IsAck() {
				_ = fr.WriteSettingsAck()
			}
		case *http2.PingFrame:
			if !f.IsAck() {
				_ = fr.WritePing(true, f.Data)
			}
		case *http2.HeadersFrame:
			_, _ = dec.Write(f.HeaderBlockFragment())
		case *http2.DataFrame:
			if data := f.Data(); len(data) > 0 {
				r.mu.Lock()
				r.flag = int(data[0])
				r.mu.Unlock()
			}
			if !f.StreamEnded() {
				continue
			}

			var block bytes.Buffer
			enc := hpack.NewEncoder(&block)
			for _, h := range [][2]string{
				{":status", "200"}, {"content-type", "application/grpc"}, {"grpc-status", "12"},
				{"grpc-message", `grpc: Decompressor is not installed for grpc-encoding "gzip"`},
			} {
				_ = enc.WriteField(hpack.HeaderField{Name: h[0], Value: h[1]})
			}
			_ = fr.WriteHeaders(http2.HeadersFrameParam{
				StreamID: f.StreamID, BlockFragment: block.Bytes(), EndStream: true, EndHeaders: true,
			})
		}
	}
}

// Ground: signal grpc-go v1.84.0 — a target without gzip answers UNIMPLEMENTED
// "grpc: Decompressor is not installed" over the wire (server.go:1402). That is
// the target refusing our request: request_error, the code the target's, not a
// reply our client refused. In-process the stand cannot refuse gzip, the
// compressor registry being global, hence the raw HTTP/2 target. It also proves
// what went on the wire: grpc-encoding: gzip, and the body's compressed flag.
func TestCompression_ATargetWithoutGzipRefusesOurRequestAndIsHeard(t *testing.T) {
	stand := &refusingGzip{}
	lis := bufconn.Listen(1024 * 1024)
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go stand.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = lis.Close() })

	sender := connected(t, Options{
		Target: "passthrough:///bufnet", Compression: "gzip",
		DialOptions: []grpc.DialOption{grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		})},
	})

	out := send(t, sender, compressible(t))

	stand.mu.Lock()
	encoding, flag := stand.encoding, stand.flag
	stand.mu.Unlock()
	if encoding != "gzip" || flag != 1 {
		t.Errorf("on the wire: grpc-encoding %q, compressed flag %d, want gzip and 1", encoding, flag)
	}
	if out.Category != engine.CategoryClientFault {
		t.Errorf("category %v, want request_error: the target refused our request (%v)", out.Category, out.Err)
	}
	if out.Code != codes.Unimplemented.String() || !out.CodeFromTarget {
		t.Errorf("code %s from the target %v, want Unimplemented, sent by the target", out.Code, out.CodeFromTarget)
	}
}
