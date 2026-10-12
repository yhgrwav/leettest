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
	"strings"
	"testing"
	"time"
)

// Ground: contract — the user's user-agent reaches the target as the one user-agent, the user's
// value first and grpc-go's own suffix after it. Measured on grpc-go 1.84.0: sent as per-RPC
// metadata it went out as a second user-agent field next to the transport's own.
func TestMetadata_UserAgentIsTheOnlyOneTheTargetSees(t *testing.T) {
	target := &seeingTarget{}
	opts := listen(t, target)
	opts.Metadata = map[string]string{"user-agent": "my-bench/1.0"}
	sender := connected(t, opts)

	out, err := sender.Send(bounded(t), request(time.Now()))
	if err != nil || out.Code != "OK" {
		t.Fatalf("send: %v, code %v", err, out.Code)
	}

	got := target.metadata[0].Get("user-agent")
	if len(got) != 1 {
		t.Fatalf("the target saw user-agent %q, want exactly one value", got)
	}
	if !strings.HasPrefix(got[0], "my-bench/1.0 ") || !strings.Contains(got[0], "grpc-go/") {
		t.Errorf("user-agent = %q, want the user's value followed by grpc-go's own", got[0])
	}
}

// Ground: contract — the inverse: without a user-agent in the metadata the target sees grpc-go's
// own, once; and the ordinary keys next to a user-agent still arrive once each, as before.
func TestMetadata_OrdinaryKeysAndDefaultUserAgentAreUnchanged(t *testing.T) {
	plain := &seeingTarget{}
	sender := connected(t, listen(t, plain))
	if _, err := sender.Send(bounded(t), request(time.Now())); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := plain.metadata[0].Get("user-agent"); len(got) != 1 || !strings.HasPrefix(got[0], "grpc-go/") {
		t.Errorf("without metadata user-agent = %q, want grpc-go's own once", got)
	}

	target := &seeingTarget{}
	opts := listen(t, target)
	opts.Metadata = map[string]string{"user-agent": "x", "x-request-source": "leettest", "authorization": "Bearer abc"}
	sender = connected(t, opts)
	if _, err := sender.Send(bounded(t), request(time.Now())); err != nil {
		t.Fatalf("send: %v", err)
	}

	md := target.metadata[0]
	for k, want := range map[string]string{"x-request-source": "leettest", "authorization": "Bearer abc"} {
		if got := md.Get(k); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %q, want [%q]", k, got, want)
		}
	}
}

// Ground: contract — the dial option is set for every connection of the run, not only the first:
// with two connections each call still shows the target one user-agent.
func TestMetadata_UserAgentIsOnceOnEveryConnection(t *testing.T) {
	target := &seeingTarget{}
	sender := mustConnect(t, 2, pair(t, target, nil, nil), Options{
		Metadata: map[string]string{"user-agent": "my-bench/1.0", "x-request-source": "leettest"},
	})

	for range 6 {
		if out, err := sender.Send(bounded(t), request(time.Now())); err != nil || out.Code != "OK" {
			t.Fatalf("send: %v, code %v", err, out.Code)
		}
	}

	for i, md := range target.metadata {
		got := md.Get("user-agent")
		if len(got) != 1 || !strings.HasPrefix(got[0], "my-bench/1.0 ") {
			t.Errorf("call %d: user-agent = %q, want one value starting with the user's", i, got)
		}
		if src := md.Get("x-request-source"); len(src) != 1 {
			t.Errorf("call %d: x-request-source = %q, want once", i, src)
		}
	}
}

// Ground: contract — Options.Metadata is the caller's map (the CLI hands over its config's): taking
// user-agent out for the dial option does not take it out of theirs.
func TestMetadata_CallersMapIsNotChanged(t *testing.T) {
	md := map[string]string{"user-agent": "my-bench/1.0", "x-api-key": "k"}
	opts := listen(t, &seeingTarget{})
	opts.Metadata = md
	_ = connected(t, opts)

	if len(md) != 2 || md["user-agent"] != "my-bench/1.0" || md["x-api-key"] != "k" {
		t.Errorf("the caller's map is now %v", md)
	}
}
