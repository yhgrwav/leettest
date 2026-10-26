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
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

var updateReport = flag.Bool("update-report", false, "rewrite testdata/report from the current report")

// fullTextRun is one run that makes every branch of PrintReport print: the
// header with its source, passport, name and settings; both forms of the plan;
// a defined and an undefined p99.9; the bytes and dataset lines; every answer
// row; the notes, connections, unchecked and thresholds blocks.
func fullTextRun() RunReport {
	const planned = 40 * time.Second

	return RunReport{
		Report: engine.Report{
			Duration: planned,
			Planned:  planned,
			Sent:     11012,
			Failed:   20,
			NotSent:  12,
			Aborted:  3,
			Connections: &engine.Connections{
				Open:     2,
				Resolved: []string{"10.0.0.1:50051", "10.0.0.2:50051"},
				Each: []engine.LinkReport{
					{Address: "10.0.0.1:50051", Calls: 5500, P99: exactQ(31 * time.Millisecond)},
					{Address: "10.0.0.2:50051", Calls: 5524, Failed: 20, StreamWaited: 7, P99: exactQ(48 * time.Millisecond)},
				},
			},
			Methods: []engine.MethodReport{
				{
					Method: "/wallet.v1.Wallet/GetBalance",
					Sent:   6000, Failed: 20, RPS: 300,
					P50: exactQ(31 * time.Millisecond), P90: exactQ(36 * time.Millisecond),
					P95: exactQ(39 * time.Millisecond), P99: exactQ(45 * time.Millisecond),
					P999:            exactQ(52 * time.Millisecond),
					Latencies:       6000,
					PlannedDuration: planned,
					RPSLow:          100,
					RPSHigh:         300,
					RequestBytes:    &engine.RequestBytes{Raw: 1200.4, Wire: 310.2},
					Dataset:         &engine.DatasetReport{File: "data/users.jsonl", Records: 10, Used: 8, UsedMax: 3},
					Rejected: engine.RefusalLatency{
						Count: 4, P50: exactQ(300 * time.Microsecond),
					},
					Overload: engine.RefusalLatency{
						Count: 5, P50: exactQ(2 * time.Millisecond), P99: exactQ(3 * time.Millisecond),
					},
					Failure: engine.RefusalLatency{
						Count: 2, P50: exactQ(5 * time.Millisecond), P99: exactQ(6 * time.Millisecond),
					},
					BadResponse: engine.RefusalLatency{
						Count: 1, P50: exactQ(7 * time.Millisecond), P99: exactQ(8 * time.Millisecond),
					},
				},
				{
					Method: "/wallet.v1.Wallet/List",
					Sent:   5012, RPS: 150,
					P50:             exactQ(20 * time.Millisecond),
					P90:             exactQ(25 * time.Millisecond),
					P95:             exactQ(28 * time.Millisecond),
					P99:             exactQ(30 * time.Millisecond),
					Latencies:       100,
					PlannedDuration: planned,
					RPSLow:          150,
					RPSHigh:         150,
				},
			},
		},
		MethodsSource: &MethodsSource{Kind: SourceReflection},
		Passport: &Passport{
			ConfigSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			OS:           "linux",
			Arch:         "amd64",
			NumCPU:       8,
			GOMAXPROCS:   8,
			GoVersion:    "go1.26.0",
			Labels:       map[string]string{"run": "full"},
		},
		Name:     "wallet",
		Settings: &RunSettings{MaxInFlight: 5000, Connections: 2, Warmup: 5 * time.Second, Compression: "gzip"},
		Unchecked: []Unchecked{
			{Method: "/wallet.v1.Wallet/Unchecked", Err: descriptor.ErrReflectionUnsupported},
			{Method: "/wallet.v1.Wallet/Refused", Err: errors.New("reflection refused")},
		},
		Thresholds: []threshold.Result{
			{
				Method: "/wallet.v1.Wallet/GetBalance", Key: threshold.P99, Verdict: threshold.VerdictPass,
				Limit: 200 * time.Millisecond, Value: exactQ(45 * time.Millisecond),
			},
			{
				Method: "/wallet.v1.Wallet/GetBalance", Key: threshold.Errors, Verdict: threshold.VerdictPass,
				LimitPercent: "1%", Bad: 20, Total: 11012,
			},
		},
	}
}

// minimalTextRun is one run with every optional branch silent: no methods
// source, passport, name or settings line; no not-sent or aborted count; no
// plan, dataset or bytes line; undefined p99.9; no connections, unchecked or
// thresholds block; no answer rows. The two method rows stay, so the header and
// the table still print.
func minimalTextRun() RunReport {
	return RunReport{
		Report: engine.Report{
			Duration: 10 * time.Second,
			Sent:     150,
			Methods: []engine.MethodReport{
				{
					Method: "/min.v1.Min/First",
					Sent:   100, RPS: 10,
					P50: exactQ(1 * time.Millisecond), P90: exactQ(2 * time.Millisecond),
					P95: exactQ(3 * time.Millisecond), P99: exactQ(4 * time.Millisecond),
					Latencies: 50,
				},
				{
					Method: "/min.v1.Min/Second",
					Sent:   50, RPS: 5,
					P50: exactQ(5 * time.Millisecond), P90: exactQ(6 * time.Millisecond),
					P95: exactQ(7 * time.Millisecond), P99: exactQ(8 * time.Millisecond),
					Latencies: 50,
				},
			},
		},
	}
}

// Ground: contract — the text report as it is before the report.go refactor:
// one full run, byte for byte. A change here is a change of the report, never a
// side effect of moving code. -update-report rewrites the file.
func TestReport_TheTextIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	PrintReport(&buf, "localhost:50051", fullTextRun())

	path := filepath.Join("testdata", "report", "full.txt")
	if *updateReport {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update-report once)", path, err)
	}
	if got := buf.String(); got != string(want) {
		t.Errorf("the report changed; first difference at byte %d", firstDiff(got, string(want)))
	}
}

// Ground: contract — the text report with every optional branch silent, byte
// for byte. A change here is a change of the report, never a side effect of
// moving code. -update-report rewrites the file.
func TestReport_TheMinimalTextIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	PrintReport(&buf, "localhost:50051", minimalTextRun())

	path := filepath.Join("testdata", "report", "minimal.txt")
	if *updateReport {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with -update-report once)", path, err)
	}
	if got := buf.String(); got != string(want) {
		t.Errorf("the report changed; first difference at byte %d", firstDiff(got, string(want)))
	}
}
