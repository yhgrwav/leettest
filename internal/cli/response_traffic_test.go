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
	"github.com/yhgrwav/leettest/pkg/metrics"
)

func exactQuantile(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Exact: true, Defined: true}
}

func floatPtr(v float64) *float64 { return &v }

// methodBlock is the lines under the method's row that belong to it: the
// indented ones, up to the next line that is not.
func methodBlock(lines []string, method string) []string {
	row := indexOfPrefix(lines, method)
	if row < 0 {
		return nil
	}

	var block []string
	for _, l := range lines[row+1:] {
		if !strings.HasPrefix(l, "  ") {
			break
		}
		block = append(block, l)
	}

	return block
}

func hasLine(block []string, want string) bool {
	for _, l := range block {
		if l == want {
			return true
		}
	}

	return false
}

// Ground: contract — a method whose measured calls got a body tells the mean
// reply size under its row, in whole bytes, whatever the compression setting:
// the reply is counted after decompression.
func TestResponseLine_AMethodTellsItsResponseBytes(t *testing.T) {
	for _, compression := range []string{"", "gzip"} {
		report := echoReport()
		report.Methods[0].ResponseBytes = floatPtr(2048)
		lines := echoLines(RunReport{Report: report, Name: "x", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1, Compression: compression}})

		if block := methodBlock(lines, "a.B/One"); !hasLine(block, "  response: 2048 B per call") {
			t.Errorf("compression %q: no response line under the row:\n%s", compression, strings.Join(lines, "\n"))
		}
	}
}

// Ground: contract — rounded to whole bytes like the bytes line.
func TestResponseLine_IsRoundedToWholeBytes(t *testing.T) {
	report := echoReport()
	report.Methods[0].ResponseBytes = floatPtr(1999.6)

	if block := methodBlock(echoLines(RunReport{Report: report}), "a.B/One"); !hasLine(block, "  response: 2000 B per call") {
		t.Errorf("want the mean 1999.6 as 2000, block:\n%s", strings.Join(block, "\n"))
	}
}

// Ground: contract — a method with no body in any measured call has no line:
// there is no size to tell.
func TestResponseLine_NoneWithoutABody(t *testing.T) {
	text := strings.Join(echoLines(RunReport{Report: echoReport()}), "\n")
	if strings.Contains(text, "response:") {
		t.Errorf("a response line without a body:\n%s", text)
	}
}

// Ground: contract — the traffic line is one line in the run's header block,
// before the method table: megabits are 10^6 bits, one decimal, and the line
// says what it counts.
func TestTrafficLine_IsInTheRunBlockInMegabitsOfTenToTheSixth(t *testing.T) {
	report := echoReport()
	report.Traffic = &engine.Traffic{SentBitsPerSecond: 12_345_678, ReceivedBitsPerSecond: 98_765_432}
	lines := echoLines(RunReport{Report: report, Name: "x"})

	const want = "traffic: sent 12.3 Mbit/s, received 98.8 Mbit/s (message bodies only, without HTTP/2, headers and TLS)"
	at, header, table := -1, indexOfPrefix(lines, "run finished:"), indexOfPrefix(lines, "method ")
	for i, l := range lines {
		if l == want {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no traffic line %q in:\n%s", want, strings.Join(lines, "\n"))
	}
	if at <= header || at >= table {
		t.Errorf("traffic line at %d, want it between the header (%d) and the method table (%d)", at, header, table)
	}
}

// Ground: contract — without a measured window there is no traffic to tell.
func TestTrafficLine_NoneWithoutTraffic(t *testing.T) {
	text := strings.Join(echoLines(RunReport{Report: echoReport()}), "\n")
	if strings.Contains(text, "traffic:") {
		t.Errorf("a traffic line with no traffic:\n%s", text)
	}
}

func warmupReport(w *engine.WarmupReport) engine.Report {
	report := echoReport()
	report.Methods[0].Warmup = w

	return report
}

// Ground: contract — the warm-up line is under the method's row: the calls
// answered, p50 and p99 in the report's latency format, the mean reply size.
func TestWarmupLine_TellsCallsPercentilesAndResponse(t *testing.T) {
	report := warmupReport(&engine.WarmupReport{
		Calls: 200, P50: exactQuantile(12300 * time.Microsecond), P99: exactQuantile(45600 * time.Microsecond), ResponseBytes: floatPtr(2048),
	})

	if block := methodBlock(echoLines(RunReport{Report: report}), "a.B/One"); !hasLine(block, "  warm-up: 200 calls, p50 12.3ms, p99 45.6ms, response 2048 B") {
		t.Errorf("no warm-up line under the row:\n%s", strings.Join(block, "\n"))
	}
}

// Ground: contract — a warm-up of 15 answered calls is too small for a p99: it
// is told as p99.9 is, with the count and the minimum, never as a number.
func TestWarmupLine_TooFewObservationsForP99(t *testing.T) {
	report := warmupReport(&engine.WarmupReport{
		Calls: 15, P50: exactQuantile(12300 * time.Microsecond), ResponseBytes: floatPtr(2048),
	})

	want := "  warm-up: 15 calls, p50 12.3ms, p99 too few observations (15 of 100), response 2048 B"
	if block := methodBlock(echoLines(RunReport{Report: report}), "a.B/One"); !hasLine(block, want) {
		t.Errorf("want %q in:\n%s", want, strings.Join(block, "\n"))
	}
}

// Ground: contract — no warm-up report (no warm-up, or none of its calls
// came back) means no warm-up line. The run-level "warm-up N sent" note is a
// different line and stays.
func TestWarmupLine_NoneWithoutAWarmupReport(t *testing.T) {
	lines := echoLines(RunReport{Report: warmupReport(nil), Name: "x"})
	for _, l := range lines {
		if strings.HasPrefix(l, "  warm-up:") {
			t.Errorf("a warm-up method line without a warm-up report: %q", l)
		}
	}
}

// Ground: contract — methods[].response_bytes is the mean as a float,
// unrounded, null when no measured call got a body.
func TestJSON_ResponseBytes(t *testing.T) {
	report := echoReport()
	report.Methods[0].ResponseBytes = floatPtr(1999.6)
	method := field(t, writeJSON(t, jsonRun(report)), "methods").([]any)[0]
	if got := field(t, method, "response_bytes"); got != 1999.6 {
		t.Errorf("response_bytes = %v, want 1999.6", got)
	}

	method = field(t, writeJSON(t, jsonRun(echoReport())), "methods").([]any)[0]
	if got := field(t, method, "response_bytes"); got != nil {
		t.Errorf("without a body response_bytes = %v, want null", got)
	}
}

// Ground: contract — traffic is on the run, in bits per second as floats,
// null without a measured window; schema_version stays 1 (the field is added).
func TestJSON_Traffic(t *testing.T) {
	report := echoReport()
	report.Traffic = &engine.Traffic{SentBitsPerSecond: 12_345_678.5, ReceivedBitsPerSecond: 98_765_432.25}
	out := writeJSON(t, jsonRun(report))

	if got := field(t, out, "traffic", "sent_bits_per_second"); got != 12_345_678.5 {
		t.Errorf("traffic.sent_bits_per_second = %v, want 12345678.5", got)
	}
	if got := field(t, out, "traffic", "received_bits_per_second"); got != 98_765_432.25 {
		t.Errorf("traffic.received_bits_per_second = %v, want 98765432.25", got)
	}
	if got := field(t, out, "schema_version"); got != 1.0 {
		t.Errorf("schema_version = %v, want 1", got)
	}

	if got := field(t, writeJSON(t, jsonRun(echoReport())), "traffic"); got != nil {
		t.Errorf("without traffic, traffic = %v, want null", got)
	}
}

// Ground: contract — methods[].warmup is {calls, p50_ns, p99_ns,
// response_bytes}; p99_ns is null when the p99 is not defined, response_bytes
// when no warm-up call got a body; the whole object is null without a report.
func TestJSON_Warmup(t *testing.T) {
	full := warmupReport(&engine.WarmupReport{
		Calls: 200, P50: exactQuantile(12300 * time.Microsecond), P99: exactQuantile(45600 * time.Microsecond), ResponseBytes: floatPtr(2048.5),
	})
	method := field(t, writeJSON(t, jsonRun(full)), "methods").([]any)[0]
	for path, want := range map[string]any{
		"calls": 200.0, "p50_ns": 12_300_000.0, "p99_ns": 45_600_000.0, "response_bytes": 2048.5,
	} {
		if got := field(t, method, "warmup", path); got != want {
			t.Errorf("warmup.%s = %v, want %v", path, got, want)
		}
	}

	small := warmupReport(&engine.WarmupReport{Calls: 15, P50: exactQuantile(12300 * time.Microsecond)})
	method = field(t, writeJSON(t, jsonRun(small)), "methods").([]any)[0]
	if got := field(t, method, "warmup", "calls"); got != 15.0 {
		t.Errorf("warmup.calls = %v, want 15", got)
	}
	for _, path := range []string{"p99_ns", "response_bytes"} {
		if got := field(t, method, "warmup", path); got != nil {
			t.Errorf("warmup.%s = %v, want null: not defined", path, got)
		}
	}

	method = field(t, writeJSON(t, jsonRun(warmupReport(nil))), "methods").([]any)[0]
	if got := field(t, method, "warmup"); got != nil {
		t.Errorf("without a warm-up report, warmup = %v, want null", got)
	}
}
