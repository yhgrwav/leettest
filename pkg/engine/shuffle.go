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
	"math/rand/v2"
	"sync/atomic"
)

// shuffleStream is the second word of the PCG state: ASCII "leettest". Seed
// and stream together are the mapping a seed in an old report must keep
// giving the same order for.
const shuffleStream = 0x6c65657474657374

// MaxShuffleSeed is the largest seed: 2^53-1, the largest integer a JSON
// reader on float64 holds exactly.
const MaxShuffleSeed = 1<<53 - 1

// shuffler hands out the records of a dataset in a random order, one position
// at a time. A pass is a Fisher-Yates shuffle done step by step: when the
// counter reaches position i, perm[i] is swapped with perm[i+IntN(n-i)] (the
// draw is made for the last position too) and the record at perm[i] is the
// one handed out. The next pass goes on with the same array and generator.
// Every step costs O(1), so no request waits for a reshuffle.
//
// Only the goroutine that moves the counter calls step; current is read by any.
type shuffler struct {
	seed    uint64
	rng     *rand.Rand
	perm    []int32
	pos     int
	current atomic.Int32
}

func newShuffler(n int, seed uint64) *shuffler {
	s := &shuffler{seed: seed, rng: rand.New(rand.NewPCG(seed, shuffleStream)), perm: make([]int32, n)}
	for i := range s.perm {
		s.perm[i] = int32(i)
	}
	s.place()

	return s
}

// step moves to the next position.
func (s *shuffler) step() {
	s.pos++
	if s.pos == len(s.perm) {
		s.pos = 0
	}
	s.place()
}

func (s *shuffler) place() {
	j := s.pos + s.rng.IntN(len(s.perm)-s.pos)
	s.perm[s.pos], s.perm[j] = s.perm[j], s.perm[s.pos]
	s.current.Store(s.perm[s.pos])
}
