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

// WaitCause is the client-side wait that held a run back.
type WaitCause string

const (
	WaitGenerator  WaitCause = "generator"
	WaitStream     WaitCause = "stream"
	WaitConnection WaitCause = "connection"
)

// RunLimit says whether the run, not the target, set the tail (#93, #140):
// some method's p99 drops by a tenth or more without the client-side waits,
// or calls went unsent, and some wait is in the p99 tail. cause is the one
// most common there. The text report's verdict and a breaking-point search
// decide by this one rule.
func (r Report) RunLimit() (cause WaitCause, limited bool) {
	if !r.ClientWaitsMoved() && r.NotSent == 0 {
		return "", false
	}

	// A tie keeps the order generator, stream, connection.
	top := 0
	for _, c := range []struct {
		tail  int
		cause WaitCause
	}{
		{r.GeneratorTailCalls, WaitGenerator},
		{r.StreamTailCalls, WaitStream},
		{r.ConnectionTailCalls, WaitConnection},
	} {
		if c.tail > top {
			top, cause = c.tail, c.cause
		}
	}

	return cause, top > 0
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
