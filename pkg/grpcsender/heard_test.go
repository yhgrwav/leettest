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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// hanging reads the request, sends headers when told to, and then holds the
// call until the caller gives up.
func hanging(headers bool) grpc.ServerOption {
	return grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		var in []byte
		if err := stream.RecvMsg(&in); err != nil {
			return err
		}
		if headers {
			if err := stream.SendHeader(metadata.MD{}); err != nil {
				return err
			}
		}
		<-stream.Context().Done()

		return stream.Context().Err()
	})
}

func sendHanging(t *testing.T, headers bool) engine.Outcome {
	t.Helper()

	sender := connected(t, listen(t, &seeingTarget{}, grpc.ForceServerCodec(rawCodec{}), hanging(headers)))

	req := request(time.Now())
	req.Method = "/leettest.test.Hang/Get"
	req.Deadline = time.Now().Add(100 * time.Millisecond)

	out, err := sender.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if out.Category != engine.CategoryTimeout {
		t.Fatalf("category = %v, want a timeout: the stand holds the call (%v)", out.Category, out.Err)
	}

	return out
}

// Ground: contract — Outcome.Heard is public: headers are the target's own,
// so a call that timed out after them was heard.
func TestHeard_HeadersBeforeATimeoutAreHeard(t *testing.T) {
	if out := sendHanging(t, true); !out.Heard {
		t.Error("heard = false: the target sent its headers")
	}
}

// Ground: contract — a target that sent nothing is silent, even when grpc-go
// on its side answers DEADLINE_EXCEEDED once its copy of the deadline runs
// out: that trailer says the target did not answer.
func TestHeard_NothingBeforeATimeoutIsSilence(t *testing.T) {
	if out := sendHanging(t, false); out.Heard {
		t.Errorf("heard = true on a call the target never answered (%v, code from target %v)", out.Err, out.CodeFromTarget)
	}
}

// Ground: contract — a success is heard.
func TestHeard_ASuccessIsHeard(t *testing.T) {
	if out := sendTo(t, 16, 0); out.Category != engine.CategorySuccess || !out.Heard {
		t.Errorf("category %v, heard %v; want a heard success", out.Category, out.Heard)
	}
}
