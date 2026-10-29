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
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

// streamBodies is a raw HTTP/2 target that refuses its first stream and
// answers the next ones, and keeps every DATA byte each stream carried.
type streamBodies struct {
	mu   sync.Mutex
	data map[uint32]*bytes.Buffer
	done map[uint32]bool
}

func (s *streamBodies) serve(conn net.Conn) {
	defer conn.Close()

	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return
	}

	fr := http2.NewFramer(conn, conn)
	if err := fr.WriteSettings(); err != nil {
		return
	}

	var refusedID uint32
	refused := false
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
			if !refused {
				refused, refusedID = true, f.StreamID
				_ = fr.WriteRSTStream(f.StreamID, http2.ErrCodeRefusedStream)
			}
		case *http2.DataFrame:
			s.mu.Lock()
			if s.data[f.StreamID] == nil {
				s.data[f.StreamID] = &bytes.Buffer{}
			}
			s.data[f.StreamID].Write(f.Data())
			if f.StreamEnded() {
				s.done[f.StreamID] = true
			}
			s.mu.Unlock()

			if f.StreamID == refusedID || !f.StreamEnded() {
				continue
			}

			var block bytes.Buffer
			enc := hpack.NewEncoder(&block)
			for _, h := range [][2]string{{":status", "200"}, {"content-type", "application/grpc"}, {"grpc-status", "5"}} {
				_ = enc.WriteField(hpack.HeaderField{Name: h[0], Value: h[1]})
			}
			_ = fr.WriteHeaders(http2.HeadersFrameParam{
				StreamID: f.StreamID, BlockFragment: block.Bytes(), EndStream: true, EndHeaders: true,
			})
		}
	}
}

// A request of a templated call is built once, before it is handed on; the
// sender sends the very bytes it was given. A transparent retry is the same
// request: its message on the wire is the first attempt's message, byte for
// byte, so a uuid in it is the same one.
// Ground: signal grpc-go v1.84.0 (as TestRetry_GrpcGoReportsEachTransparentAttempt) — the
// retry reuses the message the codec marshalled for the first attempt; this pins what is on the
// wire, so a sender that built the body per attempt would show as a second payload.
// Mutation: a retry builds the body anew (a different uuid).
func TestRetry_EveryAttemptCarriesTheRequestPayloadAsIs(t *testing.T) {
	bodies := &streamBodies{data: map[uint32]*bytes.Buffer{}, done: map[uint32]bool{}}

	lis := bufconn.Listen(1024 * 1024)
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go bodies.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = lis.Close() })

	sender := New(Options{Target: "passthrough:///bufnet", DialOptions: []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
	}})
	t.Cleanup(func() { _ = sender.Close() })

	if err := sender.Connect(bounded(t)); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// A string field of 36 characters, as a uuid is: field 1, length 36.
	payload := append([]byte{0x0a, 36}, bytes.Repeat([]byte("a"), 36)...)

	req := request(time.Now())
	req.Payload = payload
	req.Deadline = req.ScheduledAt.Add(2 * time.Second)

	out, err := sender.Send(bounded(t), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if out.Code != "NotFound" {
		t.Fatalf("code %s, want the retry's NotFound: the refusal did not turn into a retry", out.Code)
	}

	// The target answered only after the retry's DATA ended, so it is in; the
	// refused stream's DATA may or may not have been written before the refusal.
	bodies.mu.Lock()
	defer bodies.mu.Unlock()

	if len(bodies.done) == 0 {
		t.Fatal("no stream carried a complete message")
	}
	for id, buf := range bodies.data {
		if !bodies.done[id] {
			continue
		}

		raw := buf.Bytes()
		if len(raw) < 5 || raw[0] != 0 {
			t.Errorf("stream %d: %x is not an uncompressed gRPC message", id, raw)

			continue
		}
		if n := binary.BigEndian.Uint32(raw[1:5]); int(n) != len(raw)-5 {
			t.Errorf("stream %d: the prefix says %d bytes, %d follow", id, n, len(raw)-5)
		}
		if got := raw[5:]; !bytes.Equal(got, payload) {
			t.Errorf("stream %d carried %x, want the request's payload %x", id, got, payload)
		}
	}
}
