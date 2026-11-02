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

package measure

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
	"github.com/yhgrwav/leettest/test/stand"
	walletv1 "github.com/yhgrwav/leettest/test/stand/proto/wallet/v1"
)

// A method whose reply has a size known from the proto: the stand's GetBalance
// answers with the wallet id it was given and an amount, so a 2000-byte id
// makes a reply of about 2 KB. The report must carry that size exactly, the
// traffic it moved to the accuracy of the window, and the warm-up's own
// numbers beside the measured ones.
func TestReport_ResponseSizeAndTrafficAreThoseOfTheStandsReply(t *testing.T) {
	const (
		rps      = 50
		warmup   = 500 * time.Millisecond
		duration = 2 * time.Second
	)

	id := strings.Repeat("w", 2000)
	request, err := proto.Marshal(&walletv1.GetBalanceRequest{WalletId: id})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	replySize := proto.Size(&walletv1.Balance{WalletId: id, Amount: 1000})

	target := stand.Start(stand.Constant(0))
	t.Cleanup(target.Stop)

	sender := grpcsender.New(grpcsender.Options{Target: target.Target(), DialOptions: []grpc.DialOption{target.DialOption()}})
	if err = sender.Connect(t.Context()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sender.Close() })

	call := load(walletv1.WalletService_GetBalance_FullMethodName, rps, duration, time.Second)
	call.Payload = request

	eng, err := engine.New(engine.Options{
		Calls:       []engine.Call{call},
		Sender:      sender,
		MaxInFlight: rps * 2,
		Warmup:      warmup,
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), ceiling)
	defer cancel()
	if err := eng.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	report := eng.Report()
	checkNoSenderDefects(t, report)
	if report.Failed != 0 || report.Sent == 0 || len(report.Methods) != 1 {
		t.Fatalf("sent %d, failed %d over %d methods: the stand answers every call", report.Sent, report.Failed, len(report.Methods))
	}
	m := report.Methods[0]

	if m.ResponseBytes == nil || *m.ResponseBytes != float64(replySize) {
		t.Errorf("ResponseBytes = %v, want exactly the reply's %d", m.ResponseBytes, replySize)
	}

	// The window is the one the rate divides by: with every call answered,
	// received is the reply size times the rate, sent the request size times it.
	tr := report.Traffic
	if tr == nil {
		t.Error("Traffic is nil in a run with a measured window")
		tr = &engine.Traffic{}
	}
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"received", tr.ReceivedBitsPerSecond, float64(replySize) * 8 * m.RPS},
		{"sent", tr.SentBitsPerSecond, float64(len(request)) * 8 * m.RPS},
	} {
		if math.Abs(c.got-c.want) > c.want/100 {
			t.Errorf("%s = %.0f bit/s, want %.0f within 1%%: size x 8 x %.1f calls/s", c.name, c.got, c.want, m.RPS)
		}
	}

	w := m.Warmup
	if w == nil {
		t.Errorf("Warmup is nil: %d warm-up calls were answered", report.WarmupSent)
		w = &engine.WarmupReport{}
	}
	if w.Calls == 0 || w.Calls > report.WarmupSent {
		t.Errorf("Warmup.Calls = %d, warm-up sent %d: want some, and no more than went out", w.Calls, report.WarmupSent)
	}
	if !w.P50.Defined || w.P50.Value <= 0 || w.P50.Value > ceiling {
		t.Errorf("Warmup.P50 = %+v, want a latency", w.P50)
	}
	if w.ResponseBytes == nil || *w.ResponseBytes != float64(replySize) {
		t.Errorf("Warmup.ResponseBytes = %v, want the reply's %d", w.ResponseBytes, replySize)
	}
	if m.Latencies+w.Calls != report.Sent+report.WarmupSent {
		t.Errorf("latencies %d + warm-up %d != sent %d + warm-up sent %d: a call is in both histograms or in neither",
			m.Latencies, w.Calls, report.Sent, report.WarmupSent)
	}

	var out strings.Builder
	cli.PrintReport(&out, target.Target(), cli.RunReport{Report: report})
	text := out.String()
	for _, want := range []string{
		fmt.Sprintf("  response: %d B per call", replySize),
		"\ntraffic: sent ",
		"  warm-up: ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in the report:\n%s", want, text)
		}
	}
}
