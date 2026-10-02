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

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
)

// Ground: boundary — the idle check compares with the time Connect has, and in
// the CLI that is -connect-timeout, not the run: under a parent that lasts a
// minute, as a run would, an idle timeout between the two passes the check
// and one under the connect timeout does not.
func TestConnect_TheIdleCheckSeesTheConnectTimeoutNotTheRun(t *testing.T) {
	run, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	for _, tc := range []struct {
		idle    time.Duration
		refused bool
	}{
		{3 * time.Second, false},
		{time.Second, true},
	} {
		sender := grpcsender.New(grpcsender.Options{
			Target:      "passthrough:///target",
			IdleTimeout: tc.idle,
			DialOptions: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return nil, errors.New("connection refused")
			})},
		})
		err := connect(run, io.Discard, sender, &config.App{Address: "target"}, 2*time.Second)
		if got := errors.Is(err, grpcsender.ErrIdleShorterThanConnect); got != tc.refused {
			t.Errorf("idle %v with -connect-timeout 2s in a 1m run: refused %v (%v), want %v", tc.idle, got, err, tc.refused)
		}
	}
}
