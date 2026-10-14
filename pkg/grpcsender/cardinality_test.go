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
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/codes"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// The two texts our client writes when a unary reply breaks the one-request
// one-reply shape (grpc-go v1.84.0: stream.go:1296 and call.go:94 /
// stream.go:222). The server-side texts of the same family (stream.go:1931,
// :1970) are the target's, sent to us as a status.
const (
	noReplyText    = "received no response message from non-server-streaming RPC"
	extraReplyText = "expected <EOF> for non server-streaming RPCs, but received another message"
)

// unaryReply is a gRPC length-prefixed message of n zero bytes.
func unaryReply(n int) []byte {
	frame := make([]byte, 5+n)
	frame[1], frame[2], frame[3], frame[4] = byte(n>>24), byte(n>>16), byte(n>>8), byte(n)

	return frame
}

// trailersWithStatus ends a stream with a trailer carrying a status and, when
// message is not empty, grpc-message.
func trailersWithStatus(fr *http2.Framer, stream uint32, code codes.Code, message string) {
	var block bytes.Buffer
	enc := hpack.NewEncoder(&block)
	fields := [][2]string{{"grpc-status", strconv.Itoa(int(code))}}
	if message != "" {
		fields = append(fields, [2]string{"grpc-message", message})
	}
	for _, h := range fields {
		_ = enc.WriteField(hpack.HeaderField{Name: h[0], Value: h[1]})
	}
	_ = fr.WriteHeaders(http2.HeadersFrameParam{StreamID: stream, BlockFragment: block.Bytes(), EndStream: true, EndHeaders: true})
}

// answerOnRequest answers a stream once its request is in: on HEADERS alone
// the client may not have written the request yet.
func answerOnRequest(t *testing.T, answer func(fr *http2.Framer, stream uint32)) engine.Outcome {
	t.Helper()

	return sendWithin(t, rawSender(t, func(conn net.Conn) {
		serveRawData(conn, func(*http2.Framer, uint32, int) {}, func(fr *http2.Framer, id uint32) bool {
			// The stand takes its time, so a latency of zero cannot pass for one.
			time.Sleep(badReplyDelay)
			answer(fr, id)

			return false
		})
	}), 2*time.Second)
}

// A target that answered OK to a unary call without a message did answer, but
// not with a reply: grpc-go's client refuses it with Internal. The code is the
// client's, not the one the target sent (#79).
//
// Ground: stand with known behavior — the trailers-only OK found in
// reviews/done/eof-9443-status.md (experiment 2, case D); the text is pinned
// against grpc-go v1.84.0 by the Err check.
func TestSend_AnOKWithoutAReplyIsABadResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(fr *http2.Framer, stream uint32)
	}{
		{"trailers only", func(fr *http2.Framer, id uint32) { trailersOnly(fr, id, codes.OK) }},
		{"headers then trailers", func(fr *http2.Framer, id uint32) {
			replyHeaders(fr, id)
			okTrailers(fr, id)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := answerOnRequest(t, tc.answer)

			t.Logf("category %v, code %s, from target %v, heard %v, err %v", out.Category, out.Code, out.CodeFromTarget, out.Heard, out.Err)
			if out.Err == nil || !strings.Contains(out.Err.Error(), noReplyText) {
				t.Fatalf("err %v: grpc-go no longer words it with %q", out.Err, noReplyText)
			}
			if out.Category != engine.CategoryBadResponse {
				t.Errorf("category %v, want a bad response", out.Category)
			}
			if out.Code != codes.Internal.String() {
				t.Errorf("code %s, want %s", out.Code, codes.Internal)
			}
			if out.CodeFromTarget {
				t.Errorf("CodeFromTarget = true: the target said OK, the client set %s", out.Code)
			}
			if !out.Heard {
				t.Errorf("Heard = false: the target answered")
			}
			if out.SentAt.IsZero() || out.DoneAt.Sub(out.SentAt) < badReplyDelay {
				t.Errorf("SentAt %v, DoneAt %v: want at least the stand's %v", out.SentAt, out.DoneAt, badReplyDelay)
			}
		})
	}
}

// Two messages for a unary call: grpc-go returns Internal after the second
// message, before it reads the trailer. The target's OK is still in the
// trailer it never got to read, and the call is a bad response all the same.
//
// Ground: stand with known behavior; the text is pinned against grpc-go
// v1.84.0 by the Err check.
func TestSend_TwoMessagesForAUnaryCallIsABadResponse(t *testing.T) {
	out := answerOnRequest(t, func(fr *http2.Framer, id uint32) {
		replyHeaders(fr, id)
		_ = fr.WriteData(id, false, unaryReply(0))
		_ = fr.WriteData(id, false, unaryReply(0))
		okTrailers(fr, id)
	})

	t.Logf("category %v, code %s, from target %v, heard %v, err %v", out.Category, out.Code, out.CodeFromTarget, out.Heard, out.Err)
	if out.Err == nil || !strings.Contains(out.Err.Error(), extraReplyText) {
		t.Fatalf("err %v: grpc-go no longer words it with %q", out.Err, extraReplyText)
	}
	if out.Category != engine.CategoryBadResponse {
		t.Errorf("category %v, want a bad response", out.Category)
	}
	if out.Code != codes.Internal.String() {
		t.Errorf("code %s, want %s", out.Code, codes.Internal)
	}
	if out.CodeFromTarget {
		t.Errorf("CodeFromTarget = true: the client refused the second message, the target sent no Internal")
	}
	if !out.Heard {
		t.Errorf("Heard = false: the target answered")
	}
}

// An Internal the target really sent stays the target's, with or without the
// text of grpc-go's own server-side cardinality check: grpc-go on the target
// writes "cardinality violation" for a request that broke the shape and sends
// it as a status.
//
// Ground: grpc-go v1.84.0 stream.go:1931 and :1970 are server-side; a target
// on grpc-go sends them to us in a trailer.
func TestSend_AnInternalTheTargetSentStaysTheTargets(t *testing.T) {
	for _, tc := range []struct{ name, message string }{
		{"no text", ""},
		{"server-side cardinality text", "cardinality violation: received no request message from non-client-streaming RPC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := answerOnRequest(t, func(fr *http2.Framer, id uint32) {
				replyHeaders(fr, id)
				trailersWithStatus(fr, id, codes.Internal, tc.message)
			})

			t.Logf("category %v, code %s, from target %v, err %v", out.Category, out.Code, out.CodeFromTarget, out.Err)
			if out.Category != engine.CategoryServerFault {
				t.Errorf("category %v, want failure", out.Category)
			}
			if out.Code != codes.Internal.String() || !out.CodeFromTarget {
				t.Errorf("code %s from target %v, want Internal sent by the target", out.Code, out.CodeFromTarget)
			}
		})
	}
}
