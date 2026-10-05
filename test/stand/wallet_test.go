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

package stand_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/test/stand"
)

// The wallet methods the examples call are served under reflection, and obey
// the stand's behavior like Health does.
func TestStand_ServesWallet(t *testing.T) {
	const hold = 50 * time.Millisecond

	s := stand.Start(stand.Constant(hold))
	defer s.Stop()

	conn, err := grpc.NewClient(s.Target(), s.DialOption(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), ceiling)
	defer cancel()

	methods, err := descriptor.NewReflectionResolver(conn).ResolveAll(ctx, []string{
		"wallet.v1.WalletService/GetBalance",
		"wallet.v1.WalletService/Transfer",
	})
	if err != nil {
		t.Fatalf("the stand does not serve the wallet methods: %v", err)
	}

	for name, m := range methods {
		req, err := m.NewRequest(nil)
		if err != nil {
			t.Fatalf("%s: request: %v", name, err)
		}
		start := time.Now()
		if err := conn.Invoke(ctx, "/"+name, req, m.NewResponse()); err != nil {
			t.Errorf("%s: %v", name, err)

			continue
		}
		if took := time.Since(start); took < hold {
			t.Errorf("%s answered in %v, the stand was told to hold %v", name, took, hold)
		}
	}
}
