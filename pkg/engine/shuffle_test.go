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

package engine

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// referenceOrder is the simple version of the order: whole passes, one loop
// per pass, the array and the generator going on from pass to pass.
func referenceOrder(n int, seed uint64, count int) []int {
	rng := rand.New(rand.NewPCG(seed, 0x6c65657474657374))
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}

	var out []int
	for len(out) < count {
		for i := 0; i < n && len(out) < count; i++ {
			j := i + rng.IntN(n-i)
			perm[i], perm[j] = perm[j], perm[i]
			out = append(out, perm[i])
		}
	}

	return out
}

// counterOrder is the first count records the counter hands out, moved like
// the dispatcher moves it.
func counterOrder(n int, seed uint64, count int) []int {
	counter := NewShuffledCounter(n, seed)
	call := Call{Payloads: make([][]byte, n), Dataset: &DatasetRef{Counter: counter}}
	for i := range call.Payloads {
		call.Payloads[i] = []byte{byte(i), byte(i >> 8), byte(i >> 16)}
	}

	out := make([]int, 0, count)
	for range count {
		p := call.payload()
		out = append(out, int(p[0])|int(p[1])<<8|int(p[2])<<16)
		call.handedOut()
	}

	return out
}

// The step-wise order is the whole-pass order of the simple version, on the
// same inputs: single record, two, odd, and a pass count that ends mid-pass.
// Ground: contract — the performance trick (O(1) per request) changes no number.
func TestShuffledCounter_MatchesTheSimpleReference(t *testing.T) {
	for _, n := range []int{1, 2, 3, 5, 64, 1000} {
		for _, seed := range []uint64{0, 1, 42, MaxShuffleSeed} {
			t.Run(fmt.Sprintf("n=%d seed=%d", n, seed), func(t *testing.T) {
				count := 3*n + n/2 + 1
				want, got := referenceOrder(n, seed, count), counterOrder(n, seed, count)
				if !slices.Equal(got, want) {
					t.Errorf("counter order differs from the reference:\n got %v\nwant %v", got[:min(len(got), 12)], want[:min(len(want), 12)])
				}
			})
		}
	}
}

// A single record is handed out again and again, with no draw out of range.
// Ground: contract — n = 1 is a valid dataset.
func TestShuffledCounter_OneRecord(t *testing.T) {
	if got := counterOrder(1, 9, 5); !slices.Equal(got, []int{0, 0, 0, 0, 0}) {
		t.Errorf("one record: %v, want 0 five times", got)
	}
}

var sinkPayload []byte

// BenchmarkHandOut is one request handed out: the payload read and the counter
// moved, as the dispatcher does. ns/op must not grow with n; the random
// order runs through many whole passes (b.N is above 2n for the large n).
func BenchmarkHandOut(b *testing.B) {
	for _, n := range []int{1000, 1_000_000} {
		payloads := make([][]byte, n)
		for i := range payloads {
			payloads[i] = []byte{1}
		}
		for name, counter := range map[string]func() *RecordCounter{
			"file":   NewRecordCounter,
			"random": func() *RecordCounter { return NewShuffledCounter(n, 42) },
		} {
			b.Run(fmt.Sprintf("%s/n=%d", name, n), func(b *testing.B) {
				call := Call{Payloads: payloads, Dataset: &DatasetRef{Counter: counter()}}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					sinkPayload = call.payload()
					call.handedOut()
				}
			})
		}
	}
}
