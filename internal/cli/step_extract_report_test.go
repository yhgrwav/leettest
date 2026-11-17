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
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// Extract (v0.9 unit 4b, the report side). A reply that lacks the value is a
// bad_response with the code OK sent by the target: the status the target
// gave is the truth, and the client refused the content. No new category and
// no new JSON path: the report says it in the places it already has.

// extractMissed is one method every call of which was a bad_response of the
// extract kind: the target answered OK, the value was not in the reply.
func extractMissed() engine.Report {
	const sent = 12

	return engine.Report{
		Duration: 10 * time.Second, Sent: sent, Failed: sent, RequestRejected: true,
		Methods: []engine.MethodReport{{
			Method: "wallet.v1.WalletService/GetBalance", Sent: sent, Failed: sent,
			BadResponse:  refusals(sent, time.Millisecond),
			FailureCodes: []engine.CodeCount{{Code: "OK", Count: sent, FromTarget: true}},
		}},
	}
}

// Ground: contract — spec 4: the code of an extract failure is the one the
// target sent, listed under "sent by the target", as for any failure.
// Guard: green before the code (the report prints any code it is given).
func TestPrintReport_AMissingExtractIsTheTargetsOKInTheCodesLine(t *testing.T) {
	text := printCodes([]engine.CodeCount{{Code: "OK", Count: 12, FromTarget: true}}, 12)

	if want := "a.B/One codes sent by the target: OK 12"; !strings.Contains(text, want) {
		t.Errorf("no %q in the report:\n%s", want, text)
	}
	if strings.Contains(text, "set by the client") {
		t.Errorf("a line of codes the client set, for a status the target sent:\n%s", text)
	}
}

// Ground: contract — spec 4, the JSON: the entry of failure_codes is OK, the
// count, from_target true; the same fields of the schema as for any code.
// Guard: green before the code.
func TestJSON_AMissingExtractIsTheTargetsOKInFailureCodes(t *testing.T) {
	out := writeJSON(t, jsonRun(extractMissed()))

	method := field(t, out, "methods").([]any)[0]
	entry := field(t, method, "failure_codes").([]any)[0]

	if got := field(t, entry, "code"); got != "OK" {
		t.Errorf("code = %v, want OK", got)
	}
	if got := field(t, entry, "count"); got != float64(12) {
		t.Errorf("count = %v, want 12", got)
	}
	if got := field(t, entry, "from_target"); got != true {
		t.Errorf("from_target = %v, want true: the target did send OK", got)
	}
	if got := field(t, method, "bad_response", "count"); got != float64(12) {
		t.Errorf("bad_response.count = %v, want 12: the category is the existing one", got)
	}
}

// Every call of a method failing the extract is an invalid run, and the note
// says what happened: the reply lacked the value. It must not say the replies
// came compressed with an encoding the client does not accept, which is the
// note for a bad_response that is not oversized (a false cause, found in
// 4a's report).
// Ground: contract — spec 4 and the numbers-do-not-lie rule: the note names
// the cause the report holds, the target's OK with the value missing.
// Mutations: the encoding note for this report (the gap today); the extract
// note for the compressed report; a note that names no method.
func TestInvalidNote_EveryCallMissingItsExtractSaysSo(t *testing.T) {
	for _, maxResponse := range []string{"", "10MB"} {
		report := extractMissed()

		got := invalidNote(&report.Methods[0], maxResponse)

		if !strings.HasPrefix(got, "invalid run") {
			t.Errorf("max_response_size %q: note = %q, want an invalid run note", maxResponse, got)
		}
		if !strings.Contains(got, "extract") {
			t.Errorf("max_response_size %q: note = %q, want it to name the extract", maxResponse, got)
		}
		for _, wrong := range []string{"encoding", "compressed", "4MiB", "max_response_size"} {
			if strings.Contains(got, wrong) {
				t.Errorf("max_response_size %q: note = %q, want no %q: the replies were fine, the value was not in them", maxResponse, got, wrong)
			}
		}
		if !strings.Contains(got, "GetBalance") {
			t.Errorf("max_response_size %q: note = %q, want the method named", maxResponse, got)
		}
	}
}

// The compressed note keeps its report: a bad_response whose code is not the
// target's OK is still the encoding, as it was.
// Ground: contract — the extract note is for the extract only.
// Guard: green before the code.
func TestInvalidNote_ABadResponseThatIsNotAnExtractIsStillTheEncoding(t *testing.T) {
	const sent = 12

	m := engine.MethodReport{
		Method: "wallet.v1.WalletService/GetBalance", Sent: sent, Failed: sent,
		BadResponse:  refusals(sent, time.Millisecond),
		FailureCodes: []engine.CodeCount{{Code: "Internal", Count: sent, FromTarget: false}},
	}

	got := invalidNote(&m, "")
	if !strings.Contains(got, "encoding") {
		t.Errorf("note = %q, want the encoding note for a reply the client could not read", got)
	}
	if strings.Contains(got, "extract") {
		t.Errorf("note = %q, want no extract in a note about an encoding", got)
	}
}

// A call templated only for its steps' {{extract.<name>}} has no placeholder of
// the engine's: the line must not print an empty list between the commas.
// Ground: boundary — the empty list of placeholders.
// Mutation: the list joined unconditionally (two spaces in the line).
func TestTemplatesLine_NoPlaceholdersLeavesNoGap(t *testing.T) {
	got := templatesLine(&engine.TemplatesReport{LastSeq: 40})

	if want := "  data: templated (last seq 40, ramp and warm-up counted)"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}

	withSome := templatesLine(&engine.TemplatesReport{Placeholders: []string{"seq", "uuid"}, LastSeq: 40})
	if want := "  data: templated, seq uuid (last seq 40, ramp and warm-up counted)"; withSome != want {
		t.Errorf("line = %q, want %q", withSome, want)
	}
}

// A method whose bad responses are half the extract's and half something else
// has no single cause to name: the general line, with the counts. The extract
// note needs every bad response to be the target's OK, the encoding note needs
// none of them to be.
// Ground: boundary — the discriminator is "all" for the extract and "none" for
// the encoding; the middle belongs to neither.
// Mutations: missed > 0 where missed == bad (the extract note for the mix);
// oversized == 0 alone where oversized == 0 && missed == 0 (the encoding note
// for the mix).
func TestInvalidNote_MixedBadResponsesGetTheGeneralLine(t *testing.T) {
	const half = 6

	m := engine.MethodReport{
		Method: "wallet.v1.WalletService/GetBalance", Sent: 2 * half, Failed: 2 * half,
		BadResponse: refusals(2*half, time.Millisecond),
		FailureCodes: []engine.CodeCount{
			{Code: "OK", Count: half, FromTarget: true},
			{Code: "Internal", Count: half, FromTarget: false},
		},
	}

	got := invalidNote(&m, "")

	if !strings.HasPrefix(got, "invalid run") {
		t.Fatalf("note = %q, want an invalid run note", got)
	}
	if want := "request error 0, client error 0, bad response 12"; !strings.Contains(got, want) {
		t.Errorf("note = %q, want the general line with %q", got, want)
	}
	for _, wrong := range []string{"extract", "encoding", "compressed"} {
		if strings.Contains(got, wrong) {
			t.Errorf("note = %q, want no %q: neither cause holds for all of the replies", got, wrong)
		}
	}
}

// The notes of a run carry it: one invalid-run line for the method, the
// extract's, in the place the other invalid-run lines have.
// Ground: contract — the note reaches the report through reportNotes.
func TestReportNotes_AnExtractThatMissedIsAnInvalidRunLine(t *testing.T) {
	got := strings.Join(reportNotes(extractMissed(), ""), "\n")

	if !strings.Contains(got, "invalid run") || !strings.Contains(got, "extract") {
		t.Errorf("notes:\n%s\nwant an invalid run line about the extract", got)
	}
	if strings.Contains(got, "encoding") {
		t.Errorf("notes:\n%s\nwant no word of an encoding", got)
	}
}
