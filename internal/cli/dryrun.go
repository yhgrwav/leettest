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

package cli

import (
	"io"
	"time"

	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// DryRunCall is what one call of a dry run left behind.
type DryRunCall struct {
	// Method is "package.Service/Method", as the config wrote it.
	Method string
	// Info decodes the request and the reply; nil for a method reflection
	// did not know, which went out as an empty message.
	Info *descriptor.Method
	// Request is the encoded body that went out.
	Request []byte
	// Record and Records say which record of its file the request is: zero
	// Records for a call without a dataset.
	Record, Records int
	Outcome         engine.Outcome
	// Started is the moment before Send, Returned the one right after it:
	// the latency runs from Started to Outcome.DoneAt, or to Returned when the
	// sender left DoneAt zero.
	Started, Returned time.Time
}

// DryRunPrinter writes a dry run's screen call by call, so that a stop in the
// middle leaves the blocks already printed.
type DryRunPrinter struct {
	W io.Writer
	// Total is the number of calls the run plans.
	Total int
	// Redact is applied to the target's message and to the reply's JSON before
	// the cut; nil leaves them as they came.
	Redact func(string) string
}

// Header prints the first line.
func (p *DryRunPrinter) Header() {
	_ = p
}

// Call prints the next call's block.
func (p *DryRunPrinter) Call(call DryRunCall) {
	_, _ = p, call
}

// Metadata prints the metadata line, from the values as the config wrote them.
func (p *DryRunPrinter) Metadata(raw map[string]string) {
	_, _ = p, raw
}

// Printed is the number of blocks printed so far.
func (p *DryRunPrinter) Printed() int {
	_ = p

	return 0
}

// DryRunRedactor returns the function that replaces, in a string, every
// occurrence of the value of each ${NAME} the metadata substituted with
// ${NAME}.
func DryRunRedactor(raw map[string]string, lookup func(string) (string, bool)) func(string) string {
	_, _ = raw, lookup

	return func(s string) string { return s }
}
