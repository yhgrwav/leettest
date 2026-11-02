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

func boundQuantile(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Exact: false, Defined: true}
}

func floatPtr(v float64) *float64 { return &v }

// afterWarmup is the report of a run that had a warm-up: a run-level line about
// it ("warm-up N sent") is in the text, so a method line printed because of
// that count, run-level or the method's own, not because of the method's
// warm-up report, would show.
func afterWarmup() engine.Report {
	report := echoReport()
	report.WarmupSent = 50
	for i := range report.Methods {
		report.Methods[i].WarmupSent = 50
	}

	return report
}

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

// Ground: contract — a method whose measured successes returned a body tells
// the mean reply size under its row, in whole bytes, whatever the compression
// setting: the reply is counted after decompression.
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

// Ground: contract — a method whose successes were all empty replies still has
// the line, with 0: that is a size, not a missing one.
func TestResponseLine_AnEmptyReplyIsZeroBytes(t *testing.T) {
	report := echoReport()
	report.Methods[0].ResponseBytes = floatPtr(0)

	if block := methodBlock(echoLines(RunReport{Report: report}), "a.B/One"); !hasLine(block, "  response: 0 B per call") {
		t.Errorf("want \"response: 0 B per call\" for an empty reply, block:\n%s", strings.Join(block, "\n"))
	}
}

// Ground: contract — a method with no measured success has no line: there is
// no size to tell. The run had a warm-up, which does not give it one.
func TestResponseLine_NoneWithoutAnySuccess(t *testing.T) {
	text := strings.Join(echoLines(RunReport{Report: afterWarmup()}), "\n")
	if strings.Contains(text, "response:") {
		t.Errorf("a response line without a size:\n%s", text)
	}
}

// Ground: contract — the traffic line is one line in the run's header block,
// before the method table: it says what it counts, and the numbers are
// scaled to the unit that reads best.
func TestTrafficLine_IsInTheRunBlockBeforeTheMethodTable(t *testing.T) {
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

// Ground: contract — the traffic numbers take the unit that keeps them
// readable (bit/s, kbit/s, Mbit/s, Gbit/s, steps of 10^3) and 3 significant
// digits, rounded to nearest. A run of 400 bit/s is not "0.0 Mbit/s", which
// would read the same as a run that moved nothing; 0 is "0 bit/s".
func TestTrafficLine_ScalesTheUnitAndKeepsThreeSignificantDigits(t *testing.T) {
	for _, c := range []struct {
		bits float64
		want string
	}{
		{0, "0 bit/s"},
		{400, "400 bit/s"},
		{1234, "1.23 kbit/s"},
		{12_345, "12.3 kbit/s"},
		{123_456, "123 kbit/s"},
		{65_536_000, "65.5 Mbit/s"},
		{65_560_000, "65.6 Mbit/s"},
		{98_765_432, "98.8 Mbit/s"},
		{1_234_567_890, "1.23 Gbit/s"},
		// The unit is chosen after rounding to 3 digits, and trailing zeros stay.
		{999, "999 bit/s"},
		{999.6, "1.00 kbit/s"},
		{999_999, "1.00 Mbit/s"},
		{2_500_000, "2.50 Mbit/s"},
	} {
		report := echoReport()
		report.Traffic = &engine.Traffic{SentBitsPerSecond: c.bits, ReceivedBitsPerSecond: c.bits}

		want := "traffic: sent " + c.want + ", received " + c.want + " (message bodies only, without HTTP/2, headers and TLS)"
		if lines := echoLines(RunReport{Report: report, Name: "x"}); !hasLine(lines, want) {
			t.Errorf("%v bit/s: want %q in:\n%s", c.bits, want, strings.Join(lines, "\n"))
		}
	}
}

// Ground: contract — a measured window that moved nothing still has the line,
// with zeros: it says the generator sent nothing, which is not the same as not
// telling.
func TestTrafficLine_ZeroTrafficIsTold(t *testing.T) {
	report := echoReport()
	report.Traffic = &engine.Traffic{}

	const want = "traffic: sent 0 bit/s, received 0 bit/s (message bodies only, without HTTP/2, headers and TLS)"
	if lines := echoLines(RunReport{Report: report, Name: "x"}); !hasLine(lines, want) {
		t.Errorf("want %q in:\n%s", want, strings.Join(lines, "\n"))
	}
}

// Ground: contract — without a measured window there is no traffic to tell.
// The run had a warm-up and sent calls, which does not make a traffic line.
func TestTrafficLine_NoneWithoutTraffic(t *testing.T) {
	text := strings.Join(echoLines(RunReport{Report: afterWarmup()}), "\n")
	if strings.Contains(text, "traffic:") {
		t.Errorf("a traffic line with no traffic:\n%s", text)
	}
}

func warmupReport(w *engine.WarmupReport) engine.Report {
	report := afterWarmup()
	report.Methods[0].Warmup = w

	return report
}

// Ground: contract — the warm-up line is under the method's row: the
// observations, p50 and p99 in the report's latency format, the mean reply
// size.
func TestWarmupLine_TellsCallsPercentilesAndResponse(t *testing.T) {
	report := warmupReport(&engine.WarmupReport{
		Calls: 200, P50: exactQuantile(12300 * time.Microsecond), P99: exactQuantile(45600 * time.Microsecond), ResponseBytes: floatPtr(2048),
	})

	if block := methodBlock(echoLines(RunReport{Report: report}), "a.B/One"); !hasLine(block, "  warm-up: 200 calls, p50 12.3ms, p99 45.6ms, response 2048 B") {
		t.Errorf("no warm-up line under the row:\n%s", strings.Join(block, "\n"))
	}
}

// Ground: contract — a warm-up of 15 observations is too small for a p99: it
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

// Ground: contract — a percentile that is a lower bound (a timed-out call
// reaches it) is printed as one, with ">", like every latency of the report:
// a "p99 1.71s" would state a number that is not known.
func TestWarmupLine_ALowerBoundKeepsItsSign(t *testing.T) {
	report := warmupReport(&engine.WarmupReport{
		Calls: 100, P50: boundQuantile(12300 * time.Microsecond), P99: boundQuantile(1710 * time.Millisecond), ResponseBytes: floatPtr(2048),
	})

	want := "  warm-up: 100 calls, p50 >12.3ms, p99 >1.71s, response 2048 B"
	if block := methodBlock(echoLines(RunReport{Report: report}), "a.B/One"); !hasLine(block, want) {
		t.Errorf("want %q in:\n%s", want, strings.Join(block, "\n"))
	}
}

// Ground: contract — a warm-up in which nothing succeeded has the line and no
// ", response" tail: there is no reply size to tell, and a timed-out warm-up
// is the line that matters most.
func TestWarmupLine_NoResponseTailWithoutASuccess(t *testing.T) {
	report := warmupReport(&engine.WarmupReport{Calls: 30, P50: boundQuantile(1710 * time.Millisecond)})

	want := "  warm-up: 30 calls, p50 >1.71s, p99 too few observations (30 of 100)"
	if block := methodBlock(echoLines(RunReport{Report: report}), "a.B/One"); !hasLine(block, want) {
		t.Errorf("want %q in:\n%s", want, strings.Join(block, "\n"))
	}
}

// Ground: contract — a warm-up whose successes were empty replies has a size
// of 0, told.
func TestWarmupLine_AnEmptyReplyIsZeroBytes(t *testing.T) {
	report := warmupReport(&engine.WarmupReport{Calls: 200, P50: exactQuantile(12300 * time.Microsecond), P99: exactQuantile(45600 * time.Microsecond), ResponseBytes: floatPtr(0)})

	want := "  warm-up: 200 calls, p50 12.3ms, p99 45.6ms, response 0 B"
	if block := methodBlock(echoLines(RunReport{Report: report}), "a.B/One"); !hasLine(block, want) {
		t.Errorf("want %q in:\n%s", want, strings.Join(block, "\n"))
	}
}

// Ground: contract — no warm-up report (no warm-up, or no call of it left an
// observation) means no warm-up line, even in a run whose warm-up sent calls:
// the run-level "warm-up N sent" note is a different line and stays.
func TestWarmupLine_NoneWithoutAWarmupReport(t *testing.T) {
	lines := echoLines(RunReport{Report: warmupReport(nil), Name: "x"})
	for _, l := range lines {
		if strings.HasPrefix(l, "  warm-up:") {
			t.Errorf("a warm-up method line without a warm-up report: %q", l)
		}
	}
}

// Ground: contract — methods[].response_bytes is the mean as a float,
// unrounded, 0 for empty replies, null when no measured call succeeded.
func TestJSON_ResponseBytes(t *testing.T) {
	for _, c := range []struct {
		name string
		mean *float64
		want any
	}{
		{"a mean", floatPtr(1999.6), 1999.6},
		{"empty replies", floatPtr(0), 0.0},
		{"no success", nil, nil},
	} {
		report := echoReport()
		report.Methods[0].ResponseBytes = c.mean
		method := field(t, writeJSON(t, jsonRun(report)), "methods").([]any)[0]
		if got := field(t, method, "response_bytes"); got != c.want {
			t.Errorf("%s: response_bytes = %v, want %v", c.name, got, c.want)
		}
	}
}

// Ground: contract — traffic is on the run, in raw bits per second as floats
// (the text scales the unit, the JSON does not), null without a measured
// window; schema_version stays 1 (the field is added).
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

// Ground: contract — methods[].warmup is {calls, p50, p99, response_bytes}:
// the percentiles in the form of every other latency of the report ({us, ns,
// lower_bound}), p99 null when it is not defined, response_bytes null when no
// warm-up call succeeded; the whole object is null without a report.
func TestJSON_Warmup(t *testing.T) {
	full := warmupReport(&engine.WarmupReport{
		Calls: 200, P50: exactQuantile(12300 * time.Microsecond), P99: exactQuantile(45600 * time.Microsecond), ResponseBytes: floatPtr(2048.5),
	})
	method := field(t, writeJSON(t, jsonRun(full)), "methods").([]any)[0]
	for _, c := range []struct {
		path []string
		want any
	}{
		{[]string{"calls"}, 200.0},
		{[]string{"p50", "us"}, 12_300.0},
		{[]string{"p50", "ns"}, 12_300_000.0},
		{[]string{"p50", "lower_bound"}, false},
		{[]string{"p99", "us"}, 45_600.0},
		{[]string{"p99", "ns"}, 45_600_000.0},
		{[]string{"p99", "lower_bound"}, false},
		{[]string{"response_bytes"}, 2048.5},
	} {
		if got := field(t, method, append([]string{"warmup"}, c.path...)...); got != c.want {
			t.Errorf("warmup.%s = %v, want %v", strings.Join(c.path, "."), got, c.want)
		}
	}

	small := warmupReport(&engine.WarmupReport{Calls: 15, P50: exactQuantile(12300 * time.Microsecond)})
	method = field(t, writeJSON(t, jsonRun(small)), "methods").([]any)[0]
	if got := field(t, method, "warmup", "calls"); got != 15.0 {
		t.Errorf("warmup.calls = %v, want 15", got)
	}
	for _, path := range []string{"p99", "response_bytes"} {
		if got := field(t, method, "warmup", path); got != nil {
			t.Errorf("warmup.%s = %v, want null: not defined", path, got)
		}
	}

	method = field(t, writeJSON(t, jsonRun(warmupReport(nil))), "methods").([]any)[0]
	if got := field(t, method, "warmup"); got != nil {
		t.Errorf("without a warm-up report, warmup = %v, want null", got)
	}
}

// Ground: contract — a lower bound is a lower bound in the JSON too: the same
// {lower_bound: true} as every latency of the report, never a plain number.
func TestJSON_WarmupLowerBoundsAreFlagged(t *testing.T) {
	report := warmupReport(&engine.WarmupReport{
		Calls: 100, P50: boundQuantile(12300 * time.Microsecond), P99: boundQuantile(1710 * time.Millisecond),
	})
	method := field(t, writeJSON(t, jsonRun(report)), "methods").([]any)[0]

	for _, c := range []struct {
		path []string
		want any
	}{
		{[]string{"p50", "lower_bound"}, true},
		{[]string{"p50", "ns"}, 12_300_000.0},
		{[]string{"p99", "lower_bound"}, true},
		{[]string{"p99", "ns"}, 1_710_000_000.0},
		{[]string{"response_bytes"}, nil},
	} {
		if got := field(t, method, append([]string{"warmup"}, c.path...)...); got != c.want {
			t.Errorf("warmup.%s = %v, want %v", strings.Join(c.path, "."), got, c.want)
		}
	}
}
