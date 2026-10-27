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
	"math/big"
	"testing"
	"time"
)

// refOffset is the reference for tick i of a ramp a to b over d: the least
// whole nanosecond T with a*t + (b-a)*t^2/(2*D) >= i (t, D in seconds),
// searched by bisection. The inequality is multiplied through by 2*d*10^9 so
// that it is whole numbers on big.Int: a float64 would make the check
// circular, and a*T*d on an hour does not fit int64. false when that T is not
// before d.
func refOffset(a, b int, d time.Duration, i int) (time.Duration, bool) {
	dn := big.NewInt(int64(d))
	target := new(big.Int).Mul(big.NewInt(int64(i)), dn)
	target.Mul(target, big.NewInt(2_000_000_000))
	reaches := func(ns int64) bool {
		t := big.NewInt(ns)
		lin := new(big.Int).Mul(big.NewInt(int64(2*a)), t)
		lin.Mul(lin, dn)
		quad := new(big.Int).Mul(t, t)
		quad.Mul(quad, big.NewInt(int64(b-a)))

		return lin.Add(lin, quad).Cmp(target) >= 0
	}
	if !reaches(int64(d) - 1) {
		return 0, false
	}
	lo, hi := int64(0), int64(d)-1
	for lo < hi {
		mid := lo + (hi-lo)/2
		if reaches(mid) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}

	return time.Duration(lo), true
}

// refTicks is the whole-number count of ticks of a ramp: ceil(d*(a+b)/2) with
// d in seconds, which does not fit int64 in nanoseconds on an hour.
func refTicks(a, b int, d time.Duration) int {
	num := new(big.Int).Mul(big.NewInt(int64(d)), big.NewInt(int64(a+b)))
	den := big.NewInt(2_000_000_000)
	num.Add(num, den).Sub(num, big.NewInt(1))

	return int(num.Div(num, den).Int64())
}

// sampledTicks are the ticks the reference compares: every one while the ramp
// has up to 100000, and for a longer ramp the first and the last 3000 with
// every 997th between, plus the first ones past the end.
func sampledTicks(n int) []int {
	var ticks []int
	if n <= 100_000 {
		for i := range n {
			ticks = append(ticks, i)
		}
	} else {
		for i := range 3000 {
			ticks = append(ticks, i)
		}
		for i := 3000; i < n-3000; i += 997 {
			ticks = append(ticks, i)
		}
		for i := n - 3000; i < n; i++ {
			ticks = append(ticks, i)
		}
	}

	return append(ticks, n, n+1, n+1000)
}

// Ground: reference — the closed form against a bisection on whole
// nanoseconds, in big integers, for ramps up, down, short, long, and with a
// duration that is inexact in float64 (100ms, 1.01s); the tick count against
// the whole-number ceil(D*(a+b)/2), the same one whatever rounding the offset
// takes. A tick exactly at the stage's end (N whole) is not a tick: 0 to 100
// over 1s is 50 ticks, 0 to 100 over 1.01s is 51. The 1-hour entry is sampled
// (9 million ticks of big-integer bisection are half a minute).
func TestStage_OffsetMatchesAReference(t *testing.T) {
	for _, tc := range []struct {
		name  string
		a, b  int
		d     time.Duration
		ticks int
	}{
		{"0 to 1 over 1s", 0, 1, time.Second, 1},
		{"0 to 100 over 1s", 0, 100, time.Second, 50},
		{"0 to 1000 over 1s", 0, 1000, time.Second, 500},
		{"0 to 5000 over 1h", 0, 5000, time.Hour, 9_000_000},
		{"100 to 900 over 10s", 100, 900, 10 * time.Second, 5000},
		{"900 to 100 over 10s", 900, 100, 10 * time.Second, 5000},
		{"0 to 3 over 1.5s", 0, 3, 1500 * time.Millisecond, 3},
		{"0 to 100 over 1.01s", 0, 100, 1010 * time.Millisecond, 51},
		{"0 to 800 over 30s", 0, 800, 30 * time.Second, 12_000},
		{"0 to 20 over 100ms, one tick", 0, 20, 100 * time.Millisecond, 1},
		{"0 to 10 over 200ms, one tick", 0, 10, 200 * time.Millisecond, 1},
		{"100 to 0 over 1s", 100, 0, time.Second, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := refTicks(tc.a, tc.b, tc.d); got != tc.ticks {
				t.Fatalf("the whole-number tick count is %d, the table says %d", got, tc.ticks)
			}
			s := Stage{StartRPS: tc.a, TargetRPS: tc.b, Duration: tc.d}
			prev := time.Duration(-1)
			for _, i := range sampledTicks(tc.ticks) {
				want, wantOK := refOffset(tc.a, tc.b, tc.d, i)
				if wantOK != (i < tc.ticks) {
					t.Fatalf("tick %d: the reference says ok %v, the count says %d ticks", i, wantOK, tc.ticks)
				}
				got, ok := s.offset(i)
				if ok != wantOK {
					t.Fatalf("tick %d of %d: ok %v, want %v", i, tc.ticks, ok, wantOK)
				}
				if !ok {
					continue
				}
				if diff := got - want; diff < -1 || diff > 1 {
					t.Fatalf("tick %d: offset %d ns, the reference %d ns, off by %d", i, got, want, diff)
				}
				if got < prev {
					t.Fatalf("tick %d: offset %d ns is before the tick ahead of it, at %d ns", i, got, prev)
				}
				prev = got
			}
			if first, ok := s.offset(0); !ok || first != 0 {
				t.Errorf("tick 0 = %v, %v; want 0, true", first, ok)
			}
		})
	}
}

// Ground: regression — a constant stage keeps the integer formula of the
// release, and a ramp whose ends are equal is not computed as a ramp (the
// closed form has a division by zero there and a float rounding of its own).
func TestStage_AConstantStageKeepsTheIntegerFormula(t *testing.T) {
	for _, rps := range []int{1, 3, 7, 1000, 5000} {
		for _, d := range []time.Duration{time.Second, 1500 * time.Millisecond, 2 * time.Second} {
			s := Stage{StartRPS: rps, TargetRPS: rps, Duration: d}
			ticks := 0
			for i := 0; ; i++ {
				want := time.Duration(i) * time.Second / time.Duration(rps)
				got, ok := s.offset(i)
				if want >= d {
					if ok {
						t.Fatalf("rps %d, duration %s: tick %d at %s is past the stage, ok true", rps, d, i, got)
					}

					break
				}
				if !ok || got != want {
					t.Fatalf("rps %d, duration %s: tick %d = %v, %v; want %v, true", rps, d, i, got, ok, want)
				}
				ticks++
			}
			if want := refTicks(rps, rps, d); ticks != want {
				t.Errorf("rps %d, duration %s: %d ticks, want %d", rps, d, ticks, want)
			}
		}
	}
}

// Ground: boundary — a stage of rate 0 at both ends has no tick and says
// false; it does not divide by zero. The scheduler refuses such a stage before
// it asks, but ScheduledAfter is public and takes stages unchecked. Green on
// the stub: a guard for the code, killed by dropping the check.
func TestStage_AStageOfRateZeroHasNoTick(t *testing.T) {
	s := Stage{StartRPS: 0, TargetRPS: 0, Duration: time.Second}
	if got, ok := s.offset(0); ok {
		t.Errorf("a stage of rate 0 places tick 0 at %v", got)
	}
}

// Ground: hot path — offset is called on every tick of every call; it
// allocates nothing, on either kind of stage. Green on the stub: a guard for
// the code (a big-integer or fmt path would show here).
func TestStage_OffsetAllocatesNothing(t *testing.T) {
	var sink time.Duration
	for _, s := range []Stage{
		{StartRPS: 800, TargetRPS: 800, Duration: time.Minute},
		{StartRPS: 0, TargetRPS: 800, Duration: time.Minute},
	} {
		i := 0
		allocs := testing.AllocsPerRun(1000, func() {
			at, _ := s.offset(i)
			sink += at
			i++
		})
		if allocs != 0 {
			t.Errorf("stage %d to %d: %v allocations per tick, want 0", s.StartRPS, s.TargetRPS, allocs)
		}
	}
	_ = sink
}

var benchSink time.Duration

// BenchmarkStageOffset is the cost of one tick's moment. Budget (reviewer,
// 2026-10-27): the constant stage no slower than the inline formula of the
// release by more than 2 ns/op, 0 allocs/op; the ramp is measured and reported
// (tens of ns expected: a division and a square root), 0 allocs/op.
// Ground: hot path — ns/op and allocs/op of the constant stage against the
// formula it replaces.
func BenchmarkStageOffset(b *testing.B) {
	const ticks = 1 << 20
	b.Run("inline", func(b *testing.B) {
		b.ReportAllocs()
		rps := time.Duration(800)
		var sum time.Duration
		for n := range b.N {
			sum += time.Duration(n&(ticks-1)) * time.Second / rps
		}
		benchSink = sum
	})
	b.Run("constant", func(b *testing.B) {
		b.ReportAllocs()
		s := Stage{StartRPS: 800, TargetRPS: 800, Duration: time.Hour}
		var sum time.Duration
		for n := range b.N {
			at, _ := s.offset(n & (ticks - 1))
			sum += at
		}
		benchSink = sum
	})
	b.Run("ramp", func(b *testing.B) {
		b.ReportAllocs()
		s := Stage{StartRPS: 0, TargetRPS: 800, Duration: time.Hour}
		var sum time.Duration
		for n := range b.N {
			at, _ := s.offset(n & (ticks - 1))
			sum += at
		}
		benchSink = sum
	})
}
