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
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	walletv1 "github.com/yhgrwav/leettest/test/stand/proto/wallet/v1"
)

// Extract end to end (v0.9 unit 4b): a scenario whose second step sends what
// the first one's reply held, over a real connection, schema from reflection.

// walletTarget answers GetBalance with the amount 1000 + N for the wallet
// "wallet-N" it was asked about (0 with zero set: a reply without the value),
// and records what Transfer was sent.
type walletTarget struct {
	walletv1.UnimplementedWalletServiceServer

	zero bool

	mu        sync.Mutex
	transfers []transferSeen
}

type transferSeen struct {
	from, to string
	amount   int64
}

func (w *walletTarget) GetBalance(_ context.Context, req *walletv1.GetBalanceRequest) (*walletv1.Balance, error) {
	n, _ := strconv.Atoi(strings.TrimPrefix(req.GetWalletId(), "wallet-"))

	amount := int64(1000 + n)
	if w.zero {
		amount = 0
	}

	return &walletv1.Balance{WalletId: req.GetWalletId(), Amount: amount}, nil
}

func (w *walletTarget) Transfer(_ context.Context, req *walletv1.TransferRequest) (*walletv1.TransferReply, error) {
	w.mu.Lock()
	w.transfers = append(w.transfers, transferSeen{from: req.GetFrom(), to: req.GetTo(), amount: req.GetAmount()})
	w.mu.Unlock()

	return &walletv1.TransferReply{Id: "tx-" + req.GetFrom()}, nil
}

func (w *walletTarget) seen() []transferSeen {
	w.mu.Lock()
	defer w.mu.Unlock()

	return append([]transferSeen(nil), w.transfers...)
}

func startWalletTarget(t *testing.T, zero bool) (string, *walletTarget) {
	t.Helper()

	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	wallet := &walletTarget{zero: zero}
	srv := grpc.NewServer()
	walletv1.RegisterWalletServiceServer(srv, wallet)
	reflection.Register(srv)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String(), wallet
}

// payAndCheck is the scenario of examples/scenario-extract.yaml without its
// last step: the balance of a wallet, then a transfer from it of the whole
// balance to a new account.
const payAndCheck = `    - method: wallet.v1.WalletService/GetBalance
      rps: 40
      duration: 1s
      templates: true
      data:
        wallet_id: "wallet-{{seq}}"
      extract:
        wallet: wallet_id
        funds: amount
      scenario: pay
      steps:
        - method: wallet.v1.WalletService/Transfer
          templates: true
          data:
            from: "{{extract.wallet}}"
            to: "{{uuid}}"
            amount: "{{extract.funds}}"
`

var walletOfSeq = regexp.MustCompile(`^wallet-(\d+)$`)

// Every iteration sends its own wallet and its own balance: what step 2 sent
// is what the reply of step 1 of the same iteration held, never another
// iteration's and never a fixed value.
// Ground: contract — the whole path: config, reflection, extract, the worker,
// the wire; checked on what the target received (spec 1, 5, 7).
// Mutations: one set of values shared by all iterations (the amounts would
// not follow the wallets); the value of the first iteration kept; a number
// field sent as the seq.
func TestRun_AStepSendsWhatTheEarlierReplyHeld(t *testing.T) {
	addr, wallet := startWalletTarget(t, false)

	res := runCLI(t.Context(), t, 30*time.Second, "-c", dryConfig(t, addr, "", payAndCheck))
	if res.err != nil {
		t.Fatalf("run: %v\nstdout:\n%s\nstderr:\n%s", res.err, res.stdout, res.stderr)
	}

	seen := wallet.seen()
	if len(seen) == 0 {
		t.Fatalf("Transfer got no request: step 2 was never sent\nstdout:\n%s", res.stdout)
	}

	froms := map[string]bool{}

	for _, s := range seen {
		m := walletOfSeq.FindStringSubmatch(s.from)
		if m == nil {
			t.Errorf("from = %q, want the wallet of an iteration, wallet-<seq>", s.from)

			continue
		}
		n, _ := strconv.Atoi(m[1])

		if want := int64(1000 + n); s.amount != want {
			t.Errorf("transfer from %s has amount %d, want %d: the balance of that wallet", s.from, s.amount, want)
		}
		if len(s.to) != 36 {
			t.Errorf("to = %q, want a uuid", s.to)
		}
		froms[s.from] = true
	}

	if len(froms) != len(seen) {
		t.Errorf("%d different wallets in %d transfers, want one wallet for each iteration", len(froms), len(seen))
	}
	if strings.Contains(res.stdout, "codes sent by the target") {
		t.Errorf("a run that took every value reports failed codes:\n%s", res.stdout)
	}
}

// A reply without the value ends the iteration at that step: the step after it
// is never sent, the call is a bad_response with the code the target sent (OK),
// and a run in which every call is that is invalid, with a note that names the
// extract and not an encoding.
// Ground: contract — spec 4, 7 and the end-to-end of the missing value: exit
// code 2, the codes line, the invalid-run note (the gap found in 4a's report).
// Mutations: a zero amount taken as a value (Transfer would be sent with 0);
// the iteration going on after a failed step; the encoding note.
func TestRun_AReplyWithoutTheValueEndsTheIterationAsABadResponse(t *testing.T) {
	addr, wallet := startWalletTarget(t, true)

	res := runCLI(t.Context(), t, 30*time.Second, "-c", dryConfig(t, addr, "", payAndCheck))

	if !errors.Is(res.err, ErrInvalidRun) {
		t.Fatalf("run error = %v, want the run invalid: every call of GetBalance is a bad_response\nstdout:\n%s", res.err, res.stdout)
	}
	if got := len(wallet.seen()); got != 0 {
		t.Errorf("Transfer got %d requests, want none: a step is not sent after one that failed", got)
	}

	for _, want := range []string{
		"wallet.v1.WalletService/GetBalance codes sent by the target: OK ",
		"invalid run",
		"extract",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
		}
	}
	for _, wrong := range []string{"encoding", "compressed"} {
		if strings.Contains(res.stdout, wrong) {
			t.Errorf("stdout names %q, a cause the run does not have:\n%s", wrong, res.stdout)
		}
	}
}
