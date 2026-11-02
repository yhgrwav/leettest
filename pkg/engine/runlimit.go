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

import "github.com/yhgrwav/leettest/pkg/metrics"

// WaitCause is the client-side wait that held a run back.
type WaitCause string

const (
	WaitGenerator  WaitCause = "generator"
	WaitStream     WaitCause = "stream"
	WaitConnection WaitCause = "connection"
)

// Side is whose limit a client-side wait shows.
type Side string

const (
	// SideRun: the generator or the connection's stream limit held calls
	// back; the target above that is untested.
	SideRun Side = "run"
	// SideTarget: the connection was not ready — the target or the network
	// refusing it (#90).
	SideTarget Side = "target"
)

// Side is whose limit the cause shows.
func (c WaitCause) Side() Side {
	if c == WaitConnection {
		return SideTarget
	}

	return SideRun
}

// WaitVerdict says whether client-side waits set the tail (#93, #140) and
// whose limit that is. When some method's p99 drops by a tenth or more
// without the waits, cause is the one most common in the p99 tail. When none
// does, calls that went unsent decide alone: cause is the one that left the
// most calls unsent. The text report's verdict and a breaking-point search
// decide by this one rule.
func (r Report) WaitVerdict() (cause WaitCause, side Side, ok bool) {
	cause, ok = r.waitCause()
	if !ok {
		return "", "", false
	}

	return cause, cause.Side(), true
}

// waitCause judges by the tail when some p99 moved, and by the unsent calls
// alone when none did: with the p99 where it would be without the waits, only
// the calls that never went out are left to name a cause.
func (r Report) waitCause() (cause WaitCause, ok bool) {
	switch {
	case r.ClientWaitsMoved():
		return leader(r.GeneratorTailCalls, r.StreamTailCalls, r.ConnectionTailCalls)
	case r.NotSent > 0:
		return leader(r.NotSentGenerator, r.NotSentStream, r.NotSentConnection)
	}

	return "", false
}

// leader is the cause with the most calls; a tie keeps the order generator,
// stream, connection. No calls, no cause.
func leader(generator, stream, connection int) (cause WaitCause, ok bool) {
	top := 0
	for _, c := range []struct {
		n     int
		cause WaitCause
	}{
		{generator, WaitGenerator},
		{stream, WaitStream},
		{connection, WaitConnection},
	} {
		if c.n > top {
			top, cause = c.n, c.cause
		}
	}

	return cause, top > 0
}

// p99Observations is the count from which a p99 is no longer the maximum.
var p99Observations = metrics.MinCount(0.99)

// GeneratorDecidedTail says the generator's lag decided the run's p99 tail:
// the wait verdict names the generator, and some method was late by it
// (MethodReport.LateByGenerator) or the generator left calls unsent. The one
// answer for the exit code, the text and the JSON of generator_late.
func (r Report) GeneratorDecidedTail() bool {
	cause, _, ok := r.WaitVerdict()
	if !ok || cause != WaitGenerator {
		return false
	}

	if r.NotSentGenerator > 0 {
		return true
	}

	for i := range r.Methods {
		if r.Methods[i].LateByGenerator() {
			return true
		}
	}

	return false
}

// LateByGenerator says the generator's lag decided this method's numbers: the
// generator left its calls unsent, or its p99 moved and rests on at least
// metrics.MinCount(0.99) observations. Below that count p99 is the maximum, and
// one call over the wait floor moves it.
func (m *MethodReport) LateByGenerator() bool {
	return m.NotSentGenerator > 0 || (m.Moved() && int64(m.Latencies) >= p99Observations)
}

// ClientWaitsMoved says some method's p99 drops by a tenth or more when every
// client-side wait is taken out. A tenth is a hypothesis (docs/decisions.md);
// it is taken on the histogram's values, lower bounds included.
func (r Report) ClientWaitsMoved() bool {
	for i := range r.Methods {
		if r.Methods[i].Moved() {
			return true
		}
	}

	return false
}

// Moved says the method's p99 drops by a tenth or more without the
// client-side waits.
func (m *MethodReport) Moved() bool {
	return m.P99.Defined && m.P99WithoutClientWaits.Defined &&
		10*(m.P99.Value-m.P99WithoutClientWaits.Value) >= m.P99.Value
}
