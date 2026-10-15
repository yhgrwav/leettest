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

	"github.com/yhgrwav/leettest/pkg/engine"
)

// gzipReport is echoReport whose method sent 1200.4-byte bodies that went out
// as 310.2.
func gzipReport() engine.Report {
	r := echoReport()
	r.Methods[0].RequestBytes = &engine.RequestBytes{Raw: 1200.4, Wire: 310.2}

	return r
}

// Ground: contract — settings tells the compression when it is on, as the
// last item; off, the line is what it was.
func TestCompression_TheSettingsLineTellsGzip(t *testing.T) {
	on := echoLines(RunReport{Report: gzipReport(), Name: "x", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1, Compression: "gzip"}})
	if i := indexOfPrefix(on, "settings:"); i < 0 || on[i] != "settings: warm-up 0s, max in flight 5000, connections 1, compression gzip" {
		t.Errorf("with gzip:\n%s", strings.Join(on, "\n"))
	}

	off := echoLines(RunReport{Report: echoReport(), Name: "x", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1}})
	if i := indexOfPrefix(off, "settings:"); i < 0 || off[i] != "settings: warm-up 0s, max in flight 5000, connections 1" {
		t.Errorf("without it:\n%s", strings.Join(off, "\n"))
	}
}

// Ground: contract — under a method row, after its plan and before the
// dataset line: the mean request body before and after compression, rounded
// to whole bytes. The row stays the table's width.
func TestCompression_AMethodTellsItsRequestBytes(t *testing.T) {
	report := gzipReport()
	report.Methods[0].Dataset = &engine.DatasetReport{File: "w.jsonl", Records: 4, Used: 4, UsedMax: 1}
	lines := echoLines(RunReport{Report: report, Name: "x", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1, Compression: "gzip"}})

	row := indexOfPrefix(lines, "a.B/One")
	if row < 0 || row+4 >= len(lines) {
		t.Fatalf("no method row:\n%s", strings.Join(lines, "\n"))
	}
	if lines[row+1] != "  plan: 300 rps for 40s" || !strings.HasPrefix(lines[row+2], "  p99.9:") ||
		lines[row+3] != "  bytes: 1200 -> 310 per call" || !strings.HasPrefix(lines[row+4], "  data:") {
		t.Errorf("under the row want plan, p99.9, bytes, data; got:\n%s", strings.Join(lines[row:row+5], "\n"))
	}
}

// Ground: contract — without compression the two numbers are the same and the
// line is noise: it is not printed, even when the report holds the sizes; with
// gzip but no call that sent a body there is nothing to tell.
func TestCompression_NoBytesLineWithoutGzipOrWithoutSizes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		report      engine.Report
		compression string
	}{
		{"off", gzipReport(), ""},
		{"on, no sizes", echoReport(), "gzip"},
	} {
		text := strings.Join(echoLines(RunReport{Report: tc.report, Name: "x", Settings: &RunSettings{MaxInFlight: 5000, Connections: 1, Compression: tc.compression}}), "\n")
		if strings.Contains(text, "bytes:") {
			t.Errorf("%s: a bytes line in:\n%s", tc.name, text)
		}
	}
}

// Ground: contract — JSON settings.compression is "gzip" or null.
func TestCompression_JSONSettings(t *testing.T) {
	for _, tc := range []struct {
		compression string
		want        any
	}{{"gzip", "gzip"}, {"", nil}} {
		run := jsonRun(echoReport())
		run.Run.Settings = &RunSettings{MaxInFlight: 5000, Connections: 1, Compression: tc.compression}

		if got := field(t, writeJSON(t, run), "settings", "compression"); got != tc.want {
			t.Errorf("compression %q: settings.compression = %v, want %v", tc.compression, got, tc.want)
		}
	}
}

// Ground: contract — methods[].request_bytes {raw, wire} are the means as
// floats, unrounded; null without compression, and with it when no call sent
// a body.
func TestCompression_JSONRequestBytes(t *testing.T) {
	run := jsonRun(gzipReport())
	run.Run.Settings = &RunSettings{MaxInFlight: 5000, Connections: 1, Compression: "gzip"}
	method := field(t, writeJSON(t, run), "methods").([]any)[0]
	if raw, wire := field(t, method, "request_bytes", "raw"), field(t, method, "request_bytes", "wire"); raw != 1200.4 || wire != 310.2 {
		t.Errorf("request_bytes raw %v wire %v, want 1200.4 and 310.2", raw, wire)
	}

	for _, tc := range []struct {
		name        string
		report      engine.Report
		compression string
	}{{"off", gzipReport(), ""}, {"on, no sizes", echoReport(), "gzip"}} {
		run := jsonRun(tc.report)
		run.Run.Settings = &RunSettings{MaxInFlight: 5000, Connections: 1, Compression: tc.compression}
		method := field(t, writeJSON(t, run), "methods").([]any)[0]
		if got := field(t, method, "request_bytes"); got != nil {
			t.Errorf("%s: request_bytes = %v, want null", tc.name, got)
		}
	}
}

// Ground: contract — the search echoes the compression in its text settings
// line and in its JSON settings, for the same configuration as a plain run.
func TestCompression_ASearchEchoesIt(t *testing.T) {
	run := echoSearch(bpPlan, 2)
	run.Compression = "gzip"

	if out := echoSearchText(run); !strings.Contains(out, "\nsettings: settle 1s, hold 5s, max in flight per step, connections 2, compression gzip\n") {
		t.Errorf("text:\n%s", out)
	}
	if got := field(t, searchJSONOf(t, run), "settings", "compression"); got != "gzip" {
		t.Errorf("settings.compression = %v, want gzip", got)
	}

	run.Compression = ""
	if out := echoSearchText(run); strings.Contains(out, "compression") {
		t.Errorf("without it, text:\n%s", out)
	}
	if got := field(t, searchJSONOf(t, run), "settings", "compression"); got != nil {
		t.Errorf("without it settings.compression = %v, want null", got)
	}
}
