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
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

// refusingThenGone refuses the first stream with RST_STREAM REFUSED_STREAM
// and closes the connection right after, so the transparent retry finds no
// connection to write its headers into.
func refusingThenGone(conn net.Conn) {
	defer conn.Close()

	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return
	}
	fr := http2.NewFramer(conn, conn)
	if err := fr.WriteSettings(); err != nil {
		return
	}
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
		case *http2.HeadersFrame:
			_ = fr.WriteRSTStream(f.StreamID, http2.ErrCodeRefusedStream)

			return
		}
	}
}

// Ground: signal grpc-go v1.84.0 — TestRetry_AnAttemptWithoutHeadersClosesNoStream feeds the
// handler a retry with no OutHeader; this pins that grpc-go sends that sequence when the target
// refuses the stream and drops the connection before the retry is written, and that the gauge
// of open streams ends at 0. Later dials wait out the deadline, so the retry never gets a stream.
func TestRetry_ARetryWithoutHeadersOnTheWire(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	var dials atomic.Int32
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go refusingThenGone(conn)
		}
	}()
	t.Cleanup(func() { _ = lis.Close() })

	log := &eventLog{}
	sender := New(Options{Target: "passthrough:///bufnet", DialOptions: []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			if dials.Add(1) > 1 {
				<-ctx.Done()

				return nil, ctx.Err()
			}

			return lis.DialContext(ctx)
		}),
		grpc.WithStatsHandler(log),
	}})
	t.Cleanup(func() { _ = sender.Close() })

	if err := sender.Connect(bounded(t)); err != nil {
		t.Fatalf("connect: %v", err)
	}

	req := request(time.Now())
	req.Deadline = req.ScheduledAt.Add(500 * time.Millisecond)

	out, err := sender.Send(bounded(t), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	log.mu.Lock()
	events := append([]string(nil), log.events...)
	log.mu.Unlock()

	want := []string{"Begin", "OutHeader", "End", "Begin(retry)", "End"}
	if len(events) != len(want) {
		t.Fatalf("events %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events %v, want %v", events, want)
		}
	}
	if !out.NotSent {
		t.Errorf("not sent = false: the retry got no stream, nothing reached the target (%v)", out.Err)
	}
	if n := sender.OpenStreams(); n != 0 {
		t.Errorf("open streams %d after the call, want 0", n)
	}
}
