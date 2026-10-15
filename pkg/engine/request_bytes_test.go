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
	"testing"
	"time"
)

// Ground: contract — MethodReport.RequestBytes is the mean over the measured
// calls whose request reached the transport, whatever came back: a failed call
// sent its body too. Out of it: warm-up (#88) and a call that
// handed no body to the transport (an unreachable target, a call never sent).
func TestReport_RequestBytesAreTheMeanOfMeasuredCallsThatSentABody(t *testing.T) {
	ms := time.Millisecond
	stats := warmupRun(t, []Outcome{
		{Category: CategorySuccess, RawBytes: 9000, WireBytes: 9000}, // warm-up
		{Category: CategorySuccess, RawBytes: 1000, WireBytes: 100},  // measured
		{Category: CategoryOverload, RawBytes: 3000, WireBytes: 300}, // measured, failed
		{Category: CategoryUnreachable, Code: "Unavailable"},         // no body handed over
		{Category: CategorySuccess, RawBytes: 2000, WireBytes: 203},  // measured
	}, []time.Duration{100 * ms, 1100 * ms, 1200 * ms, 1300 * ms, 1400 * ms})

	got := stats.Report().Methods[0].RequestBytes
	if got == nil {
		t.Fatal("RequestBytes is nil, want the mean of the three measured calls")
	}
	if got.Raw != 2000 || got.Wire != 201 {
		t.Errorf("RequestBytes raw %v wire %v, want 2000 and 201: (1000+3000+2000)/3 and (100+300+203)/3", got.Raw, got.Wire)
	}
}

// Ground: contract — a method none of whose calls sent a body has no sizes:
// nil, not zeros that read as an empty request.
func TestReport_RequestBytesAreNilWhenNoCallSentABody(t *testing.T) {
	stats := warmupRun(t, []Outcome{
		{Category: CategoryUnreachable, Code: "Unavailable"},
	}, []time.Duration{1500 * time.Millisecond})

	if got := stats.Report().Methods[0].RequestBytes; got != nil {
		t.Errorf("RequestBytes = %+v, want nil", got)
	}
}
