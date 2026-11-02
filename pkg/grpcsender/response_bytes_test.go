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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// replyWire is a server stats handler: the size of every reply the target
// put on the wire, before and after its compression.
type replyWire struct {
	mu                sync.Mutex
	length, onTheWire []int
}

func (r *replyWire) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context { return ctx }
func (r *replyWire) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}
func (r *replyWire) HandleConn(context.Context, stats.ConnStats) {}

func (r *replyWire) HandleRPC(_ context.Context, s stats.RPCStats) {
	p, ok := s.(*stats.OutPayload)
	if !ok {
		return
	}

	r.mu.Lock()
	r.length = append(r.length, p.Length)
	r.onTheWire = append(r.onTheWire, p.CompressedLength)
	r.mu.Unlock()
}

// sendReplying makes one call to a target that answers with size bytes.
func sendReplying(t *testing.T, size int, compression string, extra ...grpc.ServerOption) engine.Outcome {
	t.Helper()

	opts := listen(t, &seeingTarget{}, append([]grpc.ServerOption{grpc.ForceServerCodec(rawCodec{}), replying(size)}, extra...)...)
	opts.Compression = compression
	sender := connected(t, opts)

	req := request(time.Now())
	req.Method = "/leettest.test.Big/Get"

	out, err := sender.Send(bounded(t), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	return out
}

// Ground: contract — Outcome.ResponseBytes is public: the length of the reply
// body as the client's codec got it, the number the report's response size is
// made of. Without compression the size before decompression is the same.
func TestResponseBytes_IsTheLengthOfTheReplyBody(t *testing.T) {
	for _, size := range []int{1, 2048, 70_000} {
		out := sendReplying(t, size, "")
		if out.Category != engine.CategorySuccess {
			t.Fatalf("size %d: category %v (%v)", size, out.Category, out.Err)
		}
		if out.ResponseBytes != size {
			t.Errorf("size %d: ResponseBytes = %d, want the body's %d", size, out.ResponseBytes, size)
		}
		if out.ResponseWireBytes != size {
			t.Errorf("size %d: ResponseWireBytes = %d, want the body's %d: nothing was compressed", size, out.ResponseWireBytes, size)
		}
	}
}

// Ground: contract — ResponseBytes is the body after decompression, not what
// went over the wire: the same 8 KiB reply counts 8192 whether the target
// compressed it or not. ResponseWireBytes is the other one, the size before
// decompression without the frame, as the server's stats saw it leave: the
// number the traffic of the run is made of.
func TestResponseBytes_OfACompressedReplyIsAfterDecompression(t *testing.T) {
	const size = 8192

	wire := &replyWire{}
	out := sendReplying(t, size, "gzip", grpc.StatsHandler(wire))
	if out.Category != engine.CategorySuccess {
		t.Fatalf("category %v (%v)", out.Category, out.Err)
	}

	// The precondition: the reply really crossed the wire folded.
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if len(wire.onTheWire) != 1 || wire.onTheWire[0] <= 0 || wire.onTheWire[0] >= size/4 {
		t.Fatalf("the target sent %v compressed bytes of a %v body: the stand did not compress the reply", wire.onTheWire, wire.length)
	}

	if out.ResponseBytes != size {
		t.Errorf("ResponseBytes = %d, want %d: the body after decompression, not the %d on the wire", out.ResponseBytes, size, wire.onTheWire[0])
	}
	if out.ResponseWireBytes != wire.onTheWire[0] {
		t.Errorf("ResponseWireBytes = %d, want %d: the compressed size the target sent, not the %d after decompression", out.ResponseWireBytes, wire.onTheWire[0], size)
	}
}

// Ground: contract — a reply the client refused as larger than its limit
// (#126) is a bad response that gives no size: the body was not read, and a
// number for it would count bytes the client never took.
func TestResponseBytes_OfARefusedReplyIsZero(t *testing.T) {
	out := sendReplying(t, 4*mib+1, "")
	if out.Category != engine.CategoryBadResponse {
		t.Fatalf("category %v, want a bad response (%v)", out.Category, out.Err)
	}
	if out.ResponseBytes != 0 {
		t.Errorf("ResponseBytes = %d, want 0 for a refused reply", out.ResponseBytes)
	}
}

// Ground: contract — a call that got no body (an error status from the
// target) has no size: 0 in both fields.
func TestResponseBytes_OfACallWithoutABodyIsZero(t *testing.T) {
	opts := listen(t, &seeingTarget{}, grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
			var in []byte
			if err := stream.RecvMsg(&in); err != nil {
				return err
			}

			return status.Error(codes.Unavailable, "no capacity")
		}))
	sender := connected(t, opts)

	req := request(time.Now())
	req.Method = "/leettest.test.Big/Get"
	out, err := sender.Send(bounded(t), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if out.Category != engine.CategoryOverload {
		t.Fatalf("category %v, want overload: the stand answers UNAVAILABLE (%v)", out.Category, out.Err)
	}
	if out.ResponseBytes != 0 {
		t.Errorf("ResponseBytes = %d, want 0: no body came", out.ResponseBytes)
	}
	if out.ResponseWireBytes != 0 {
		t.Errorf("ResponseWireBytes = %d, want 0: no body came", out.ResponseWireBytes)
	}
}
