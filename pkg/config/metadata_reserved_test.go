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

package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// Ground: contract — a metadata key the transport owns would go out as a second header next to
// the transport's own (measured: two content-type, two te on the wire) or, for connection, break
// the stream (RST_STREAM PROTOCOL_ERROR). The user learns it at config check, before any call,
// with the key and why. The secret value is never part of the message.
func TestMetadata_TransportOwnedKeyIsRefusedBeforeTheRun(t *testing.T) {
	const fromTransport, notInHTTP2 = "set by the gRPC transport", "not allowed in HTTP/2"

	for _, tc := range []struct {
		written, key, why string
	}{
		{"content-type", "content-type", fromTransport},
		{"Content-Type", "content-type", fromTransport},
		{"te", "te", fromTransport},
		{"TE", "te", fromTransport},
		{"connection", "connection", notInHTTP2},
		{"Connection", "connection", notInHTTP2},
		{"keep-alive", "keep-alive", notInHTTP2},
		{"Keep-Alive", "keep-alive", notInHTTP2},
		{"proxy-connection", "proxy-connection", notInHTTP2},
		{"Proxy-Connection", "proxy-connection", notInHTTP2},
		{"transfer-encoding", "transfer-encoding", notInHTTP2},
		{"Transfer-Encoding", "transfer-encoding", notInHTTP2},
		{"upgrade", "upgrade", notInHTTP2},
		{"UPGRADE", "upgrade", notInHTTP2},
	} {
		t.Run(tc.written, func(t *testing.T) {
			_, err := config.Parse(withApp("  metadata:\n    " + tc.written + ": hunter2-secret\n"))
			if !errors.Is(err, config.ErrInvalidMetadata) {
				t.Fatalf("err = %v, want ErrInvalidMetadata", err)
			}

			msg := err.Error()
			if !strings.Contains(strings.ToLower(msg), `"`+tc.key+`"`) {
				t.Errorf("the error does not name the key %q: %v", tc.key, err)
			}
			if !strings.Contains(msg, tc.why) {
				t.Errorf("the error does not say why (%q): %v", tc.why, err)
			}
			if strings.Contains(msg, "hunter2-secret") {
				t.Errorf("the error prints the value: %v", err)
			}
		})
	}
}

// Ground: contract — the check is by the whole key. Names that only contain or begin with a
// reserved one are ordinary keys (upgrade-insecure-requests is a real request header), and a
// refused key does not hide the others: every problem is named in one pass.
func TestMetadata_ReservedListIsExactKeysOnly(t *testing.T) {
	cfg, err := config.Parse(withApp("  metadata:\n" +
		"    x-te: a\n    te-id: b\n    content-type-hint: c\n    x-connection: d\n" +
		"    upgrade-insecure-requests: \"1\"\n    keep-alive-ms: \"5\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.App.Metadata) != 6 {
		t.Errorf("metadata %v, want all six keys", cfg.App.Metadata)
	}

	_, err = config.Parse(withApp("  metadata:\n    te: a\n    connection: b\n    x-ok: c\n"))
	if !errors.Is(err, config.ErrInvalidMetadata) {
		t.Fatalf("err = %v, want ErrInvalidMetadata", err)
	}
	for _, key := range []string{`"te"`, `"connection"`} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("the error misses %s: %v", key, err)
		}
	}
	if strings.Contains(err.Error(), "x-ok") {
		t.Errorf("an ordinary key is named as a problem: %v", err)
	}
}

// Ground: contract — user-agent stays allowed in the config (the sender carries it through the
// dial option, not as a second header); any case, value as written. Green before the change by
// design: it pins that the reserved list does not swallow it.
func TestMetadata_UserAgentIsAccepted(t *testing.T) {
	for _, written := range []string{"user-agent", "User-Agent"} {
		cfg, err := config.Parse(withApp("  metadata:\n    " + written + ": my-bench/1.0\n"))
		if err != nil {
			t.Fatalf("%s: parse: %v", written, err)
		}
		if got := cfg.App.Metadata["user-agent"]; got != "my-bench/1.0" {
			t.Errorf("%s: metadata[user-agent] = %q, want my-bench/1.0", written, got)
		}
	}
}
