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

// sizedRun is a run of the stand's GetBalance with a 2000-byte wallet id: the
// stand answers with the id it was given and an amount, so the reply has a size
// known from the proto, and the request one of its own.
type sizedRun struct {
	report    engine.Report
	request   []byte
	replySize int
}

func runSized(t *testing.T, compression string) sizedRun {
	t.Helper()

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

	sender := grpcsender.New(grpcsender.Options{
		Target: target.Target(), DialOptions: []grpc.DialOption{target.DialOption()}, Compression: compression,
	})
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

	return sizedRun{report: report, request: request, replySize: replySize}
}

// A method whose reply has a size known from the proto: the report must carry
// that size exactly, the traffic it moved to the accuracy of the window, and
// the warm-up's own numbers beside the measured ones.
func TestReport_ResponseSizeAndTrafficAreThoseOfTheStandsReply(t *testing.T) {
	run := runSized(t, "")
	report, replySize := run.report, run.replySize
	m := report.Methods[0]

	if m.ResponseBytes == nil || *m.ResponseBytes != float64(replySize) {
		t.Errorf("ResponseBytes = %v, want exactly the reply's %d", m.ResponseBytes, replySize)
	}

	// The window is the one the rate divides by: with every call answered and no
	// compression, received is the reply size times the rate, sent the request
	// size times it.
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
		{"sent", tr.SentBitsPerSecond, float64(len(run.request)) * 8 * m.RPS},
	} {
		if math.Abs(c.got-c.want) > c.want/100 {
			t.Errorf("%s = %.0f bit/s, want %.0f within 1%%: size x 8 x %.1f calls/s", c.name, c.got, c.want, m.RPS)
		}
	}

	w := m.Warmup
	if w == nil {
		t.Errorf("Warmup is nil: %d warm-up calls were sent", report.WarmupSent)
		w = &engine.WarmupReport{}
	}
	if w.Calls == 0 || w.Calls > report.WarmupSent {
		t.Errorf("Warmup.Calls = %d, warm-up sent %d: want some, and no more than went out", w.Calls, report.WarmupSent)
	}
	if !w.P50.Defined || !w.P50.Exact || w.P50.Value <= 0 || w.P50.Value > ceiling {
		t.Errorf("Warmup.P50 = %+v, want an exact latency", w.P50)
	}
	if w.ResponseBytes == nil || *w.ResponseBytes != float64(replySize) {
		t.Errorf("Warmup.ResponseBytes = %v, want the reply's %d", w.ResponseBytes, replySize)
	}
	if m.Latencies+w.Calls != report.Sent+report.WarmupSent {
		t.Errorf("latencies %d + warm-up %d != sent %d + warm-up sent %d: a call is in both histograms or in neither",
			m.Latencies, w.Calls, report.Sent, report.WarmupSent)
	}

	var out strings.Builder
	cli.PrintReport(&out, "stand", cli.RunReport{Report: report})
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

// With gzip the stand answers folded too: the reply is told by its size after
// decompression (the input of a calibration), the traffic by what went over the
// wire, a fraction of it for a body of one repeated letter. Counting the
// received traffic after decompression would report a channel the run did not use.
func TestReport_TrafficOfACompressedRunIsTheCompressedSize(t *testing.T) {
	run := runSized(t, "gzip")
	report, replySize := run.report, run.replySize
	m := report.Methods[0]

	if m.ResponseBytes == nil || *m.ResponseBytes != float64(replySize) {
		t.Errorf("ResponseBytes = %v, want exactly the reply's %d after decompression", m.ResponseBytes, replySize)
	}

	tr := report.Traffic
	if tr == nil {
		t.Fatal("Traffic is nil in a run with a measured window")
	}
	unfolded := float64(replySize) * 8 * m.RPS
	if tr.ReceivedBitsPerSecond <= 0 || tr.ReceivedBitsPerSecond > unfolded/4 {
		t.Errorf("received = %.0f bit/s, want a fraction (at most a quarter) of the %.0f bit/s the unfolded replies make", tr.ReceivedBitsPerSecond, unfolded)
	}
	unfoldedSent := float64(len(run.request)) * 8 * m.RPS
	if tr.SentBitsPerSecond <= 0 || tr.SentBitsPerSecond > unfoldedSent/4 {
		t.Errorf("sent = %.0f bit/s, want a fraction (at most a quarter) of the %.0f bit/s the unfolded requests make", tr.SentBitsPerSecond, unfoldedSent)
	}
}
