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

package stand

import (
	"context"

	walletv1 "github.com/yhgrwav/leettest/test/stand/proto/wallet/v1"
)

// wallet answers the examples' methods with fixed replies, the way the stand
// was told to.
type wallet struct {
	walletv1.UnimplementedWalletServiceServer

	s *Stand
}

func (w wallet) GetBalance(ctx context.Context, req *walletv1.GetBalanceRequest) (*walletv1.Balance, error) {
	if err := w.s.serve(ctx); err != nil {
		return nil, err
	}

	return &walletv1.Balance{WalletId: req.GetWalletId(), Amount: 1000}, nil
}

func (w wallet) Transfer(ctx context.Context, _ *walletv1.TransferRequest) (*walletv1.TransferReply, error) {
	if err := w.s.serve(ctx); err != nil {
		return nil, err
	}

	return &walletv1.TransferReply{Id: "stand"}, nil
}
