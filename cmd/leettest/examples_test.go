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
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
	"github.com/yhgrwav/leettest/test/stand"
)

// Each example reaches the stand the way the CLI would — its TLS, metadata
// and request bodies against reflection — and every call it names is answered
// once. Full-length runs are the CI tour job's.
func TestExamples_RunAgainstStand(t *testing.T) {
	t.Setenv("LEETTEST_TOKEN", "demo")

	names, err := filepath.Glob("../../examples/*.yaml")
	if err != nil || len(names) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, name := range names {
		t.Run(filepath.Base(name), func(t *testing.T) {
			cfg, err := config.LoadFile(name)
			if err != nil {
				t.Fatal(err)
			}

			var opts []grpc.ServerOption
			if cfg.App.UseTLS {
				certs, certErr := stand.WriteCerts(t.TempDir())
				if certErr != nil {
					t.Fatal(certErr)
				}
				opt, optErr := certs.ServerOption(cfg.App.Cert != "")
				if optErr != nil {
					t.Fatal(optErr)
				}
				opts = append(opts, opt)
				cfg.App.CA = certs.CA()
				if cfg.App.Cert != "" {
					cfg.App.Cert, cfg.App.Key = certs.ClientCert(), certs.ClientKey()
				}
			}
			lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			s := stand.StartOn(lis, nil, opts...)
			defer s.Stop()
			cfg.App.Address = config.ConnectionString(s.Target())

			senderOpts, err := senderOptions(&cfg.App)
			if err != nil {
				t.Fatal(err)
			}
			sender := grpcsender.New(senderOpts)
			defer func() { _ = sender.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err = sender.Connect(ctx); err != nil {
				t.Fatalf("connect: %v", err)
			}
			calls, err := cli.CallsFromConfig(cfg)
			if err != nil {
				t.Fatalf("calls: %v", err)
			}
			unchecked, err := cli.AttachData(ctx, descriptor.NewReflectionResolver(sender.Conn()), cfg, calls)
			if err != nil {
				t.Fatalf("request bodies: %v", err)
			}
			for _, u := range unchecked {
				t.Errorf("%s not checked against the stand: %v", u.Method, u.Err)
			}

			for _, call := range calls {
				now := time.Now()
				out, err := sender.Send(ctx, engine.Request{
					Method: call.Method, Payload: call.Payload, ScheduledAt: now, Deadline: now.Add(call.Timeout),
				})
				if err != nil || out.Category != engine.CategorySuccess {
					t.Errorf("%s: category %v, error %v, status %v", call.Method, out.Category, err, out.Err)
				}
			}
		})
	}
}
