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
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/compare"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
	shr "github.com/yhgrwav/leettest/pkg/share"
)

const (
	crTarget  = "10.0.0.1:50051"
	crVersion = "v0.3.0"
	crDo      = "pkg.Svc/Do"
	// crHeader is the two header lines of the default pair, whose reports are
	// passed as base.json and new.json.
	crHeader = "base: base.json  svc  2026-10-01T10:00:00Z\n" +
		"new:  new.json  svc  2026-10-02T10:00:00Z\n"
	crNoPassport = "no passport (report from leettest before v0.3)"
)

// crMethod is one method of a plain run, with only the fields leettest compare
// reads varying. The report names it with a leading slash, as a real run does.
type crMethod struct {
	name                                                        string
	sent, failed                                                int
	notSent, notSentGenerator, notSentStream, notSentConnection int
	// rejected, clientError and badResponse are the failed calls the client's
	// side owns: request_error.count, client_error, bad_response.count.
	rejected, clientError, badResponse int
	p99                                metrics.Quantile
	datasetSHA                         string
}

// crRun is a plain run as the report is built from the real JSON types.
type crRun struct {
	name, outcome, target, version string
	startedAt                      time.Time
	passport                       *Passport
	// tail is the report's tail_wait_cause: generator, stream, connection, or
	// "" for null.
	tail    string
	methods []crMethod
}

func crMillis(n int) time.Duration { return time.Duration(n) * time.Millisecond }

// crExact is a p99 that is a measured value; crLower one that is only a lower
// bound, a timeout among the observations it landed on.
func crExact(d time.Duration) metrics.Quantile {
	return metrics.Quantile{Value: d, Exact: true, Defined: true}
}

func crLower(d time.Duration) metrics.Quantile { return metrics.Quantile{Value: d, Defined: true} }

// crPassport does not depend on the machine the test runs on.
func crPassport() *Passport {
	quota := 4.0
	p := NewPassport(strings.Repeat("a", 64), nil, &quota)
	p.OS, p.Arch, p.NumCPU, p.GOMAXPROCS, p.GoVersion = "linux", "amd64", 8, 8, "go1.27.1"

	return p
}

// crCall is a method of 1000 calls, `failed` of them failed, with the p99 given.
func crCall(name string, p99 metrics.Quantile, failed int) crMethod {
	return crMethod{name: name, sent: 1000, failed: failed, p99: p99}
}

func crBase() crRun {
	return crRun{
		name: "svc", outcome: OutcomeComplete, target: crTarget, version: crVersion,
		startedAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC), passport: crPassport(),
		methods: []crMethod{crCall(crDo, crExact(crMillis(100)), 10)},
	}
}

func crNew() crRun {
	r := crBase()
	r.startedAt = time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)

	return r
}

// file builds the report through NewJSONReport and reads it back from its
// JSON, so the comparison sees what a file holds.
func (r crRun) file(t *testing.T, path string) RunFile {
	t.Helper()

	rep := engine.Report{}
	for _, m := range r.methods {
		mr := engine.MethodReport{
			Method: "/" + m.name, Sent: m.sent, Failed: m.failed, P99: m.p99,
			NotSent: m.notSent, NotSentGenerator: m.notSentGenerator, NotSentStream: m.notSentStream,
			NotSentConnection: m.notSentConnection,
			Rejected:          engine.RefusalLatency{Count: m.rejected}, ClientError: m.clientError,
			BadResponse: engine.RefusalLatency{Count: m.badResponse},
		}
		if m.datasetSHA != "" {
			mr.Dataset = &engine.DatasetReport{File: "data.jsonl", SHA256: m.datasetSHA, Records: 10}
		}
		rep.Methods = append(rep.Methods, mr)
	}

	// The run-level verdict is the engine's own: some wait in the tail, and
	// calls unsent, name the cause.
	switch r.tail {
	case "generator":
		rep.NotSent, rep.GeneratorTailCalls = 1, 1
	case "stream":
		rep.NotSent, rep.StreamTailCalls = 1, 1
	case "connection":
		rep.NotSent, rep.ConnectionTailCalls = 1, 1
	}

	raw, err := json.Marshal(NewJSONReport(JSONRun{
		Target: r.target, Name: r.name, Version: r.version, Outcome: r.outcome, StartedAt: r.startedAt,
		Run: RunReport{Report: rep, Passport: r.passport},
	}))
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}

	var out JSONReport
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}

	if got := out.TailWaitCause; (got == nil) != (r.tail == "") || got != nil && *got != r.tail {
		t.Fatalf("fixture %s: tail_wait_cause %v, want %q", path, got, r.tail)
	}

	return RunFile{Path: path, Report: out}
}

func crLimit(t *testing.T, flag, text string) *shr.Percent {
	t.Helper()

	if text == "" {
		return nil
	}

	p, err := shr.Parse(text, flag)
	if err != nil {
		t.Fatalf("limit %q: %v", text, err)
	}

	return &p
}

// crCompare prints the comparison of base and nw; "" for a limit is none given.
func crCompare(t *testing.T, base, nw crRun, p99, errs string) (string, Comparison) {
	t.Helper()

	var out bytes.Buffer
	c := CompareRuns(&out, base.file(t, "base.json"), nw.file(t, "new.json"),
		crLimit(t, "-max-p99-rise", p99), crLimit(t, "-max-errors-rise", errs))

	return out.String(), c
}

// crLine is the one line of out that starts with prefix.
func crLine(t *testing.T, out, prefix string) string {
	t.Helper()

	var found []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			found = append(found, line)
		}
	}

	if len(found) != 1 {
		t.Fatalf("want one line starting %q, got %d in\n%s", prefix, len(found), out)
	}

	return found[0]
}

// crDiffs are the lines of out that name a difference between the reports.
func crDiffs(out string) []string {
	var lines []string

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "differs:") || strings.Contains(line, ": "+crNoPassport) {
			lines = append(lines, line)
		}
	}

	return lines
}

func crWantFrame(t *testing.T, got, want string) {
	t.Helper()

	if got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}

func crWantResult(t *testing.T, got, want Comparison) {
	t.Helper()

	if got != want {
		t.Errorf("comparison %+v, want %+v", got, want)
	}
}

// Ground: contract — two comparable runs and no limit: both headers, the p99
// and the error share of each method and one closing line saying nothing was
// judged. The result is comparable, not judged.
func TestCompareRuns_NoLimitsPrintsAndJudgesNothing(t *testing.T) {
	nw := crNew()
	nw.methods[0] = crCall(crDo, crExact(crMillis(110)), 15)

	out, c := crCompare(t, crBase(), nw, "", "")

	crWantFrame(t, out, crHeader+
		"pkg.Svc/Do: p99 100ms -> 110ms, rise 10.0%\n"+
		"pkg.Svc/Do: errors 1.0% -> 1.5%, rise 0.5%\n"+
		"no limit given: nothing judged (-max-p99-rise, -max-errors-rise)\n")
	crWantResult(t, c, Comparison{Comparable: true})
}

// Ground: contract — a p99 past its limit is FAIL beside an errors line that
// passes; the result is a fail, comparable and judged.
func TestCompareRuns_P99FailBesideErrorsPass(t *testing.T) {
	nw := crNew()
	nw.methods[0] = crCall(crDo, crExact(crMillis(130)), 15)

	out, c := crCompare(t, crBase(), nw, "20%", "1%")

	crWantFrame(t, out, crHeader+
		"pkg.Svc/Do: p99 100ms -> 130ms, rise 30.0%, limit 20%: FAIL\n"+
		"pkg.Svc/Do: errors 1.0% -> 1.5%, rise 0.5%, limit 1%: pass\n")
	crWantResult(t, c, Comparison{Comparable: true, Judged: true, Verdict: compare.Fail})
}

// Ground: contract — a new p99 that is only a lower bound and sits within the
// limit is unknown, never a pass: the value is printed with its bound, the
// rise with `>=`, the reason says what is censored. The result is unknown.
func TestCompareRuns_ACensoredNewP99IsUnknown(t *testing.T) {
	nw := crNew()
	nw.methods[0] = crCall(crDo, crLower(crMillis(110)), 10)

	out, c := crCompare(t, crBase(), nw, "20%", "")

	crWantFrame(t, out, crHeader+
		"pkg.Svc/Do: p99 100ms -> >110ms, rise >= 10.0%, limit 20%: unknown, new p99 only a lower bound (timeouts)\n"+
		"pkg.Svc/Do: errors 1.0% -> 1.0%, no change\n")
	crWantResult(t, c, Comparison{Comparable: true, Judged: true, Verdict: compare.Unknown})
}

// Ground: contract — reports that differ are not compared: the difference is
// named, every limit given is unknown and says why, no change is worked out,
// and there is no closing line after the last check.
func TestCompareRuns_NotComparableWithLimits(t *testing.T) {
	nw := crNew()
	nw.target = "10.0.0.2:50051"
	nw.methods[0] = crCall(crDo, crExact(crMillis(130)), 15)

	out, c := crCompare(t, crBase(), nw, "20%", "1%")

	crWantFrame(t, out, crHeader+
		"differs: target: 10.0.0.1:50051 -> 10.0.0.2:50051\n"+
		"pkg.Svc/Do: p99 100ms -> 130ms, rise -, limit 20%: unknown, reports are not comparable\n"+
		"pkg.Svc/Do: errors 1.0% -> 1.5%, rise -, limit 1%: unknown, reports are not comparable\n")
	crWantResult(t, c, Comparison{Judged: true})
}

// Ground: contract — reports that differ and no limit: the values are printed
// without a change, and the closing line says the reports are not comparable.
func TestCompareRuns_NotComparableWithoutLimits(t *testing.T) {
	nw := crNew()
	nw.target = "10.0.0.2:50051"
	nw.methods[0] = crCall(crDo, crExact(crMillis(130)), 15)

	out, c := crCompare(t, crBase(), nw, "", "")

	crWantFrame(t, out, crHeader+
		"differs: target: 10.0.0.1:50051 -> 10.0.0.2:50051\n"+
		"pkg.Svc/Do: p99 100ms -> 130ms, rise -\n"+
		"pkg.Svc/Do: errors 1.0% -> 1.5%, rise -\n"+
		"reports are not comparable: nothing judged\n")
	crWantResult(t, c, Comparison{})
}

// Ground: contract — each thing that makes two runs incomparable is named on
// its own line with both values, in every form the spec lists: target,
// version, a missing passport, each passport field, the set of methods, a
// method's dataset. With a limit the checks are unknown and last; without
// one the closing line says so. Nothing else is named.
func TestCompareRuns_EachDifferenceIsNamed(t *testing.T) {
	quota := 2.0
	c, d, e := strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64)

	for _, tc := range []struct {
		name string
		edit func(base, nw *crRun)
		want []string
		// methodsRow: the method sets differ, which lines follow is not the
		// spec's to say here.
		methodsRow bool
	}{
		{"target", func(_, n *crRun) { n.target = "10.0.0.2:50051" },
			[]string{"differs: target: 10.0.0.1:50051 -> 10.0.0.2:50051"}, false},
		{"leettest_version", func(_, n *crRun) { n.version = "v0.3.1" },
			[]string{"differs: leettest_version: v0.3.0 -> v0.3.1"}, false},
		{"passport.config_sha256", func(_, n *crRun) { n.passport.ConfigSHA256 = strings.Repeat("b", 64) },
			[]string{"differs: passport.config_sha256: " + strings.Repeat("a", 64) + " -> " + strings.Repeat("b", 64) +
				" (config bytes differ: a comment or line endings count too)"}, false},
		{"passport.os", func(_, n *crRun) { n.passport.OS = "darwin" },
			[]string{"differs: passport.os: linux -> darwin"}, false},
		{"passport.arch", func(_, n *crRun) { n.passport.Arch = "arm64" },
			[]string{"differs: passport.arch: amd64 -> arm64"}, false},
		{"passport.num_cpu", func(_, n *crRun) { n.passport.NumCPU = 4 },
			[]string{"differs: passport.num_cpu: 8 -> 4"}, false},
		{"passport.gomaxprocs", func(_, n *crRun) { n.passport.GOMAXPROCS = 4 },
			[]string{"differs: passport.gomaxprocs: 8 -> 4"}, false},
		{"passport.cpu_quota", func(_, n *crRun) { n.passport.CPUQuota = &quota },
			[]string{"differs: passport.cpu_quota: 4 -> 2"}, false},
		{"passport.cpu_quota to none", func(_, n *crRun) { n.passport.CPUQuota = nil },
			[]string{"differs: passport.cpu_quota: 4 -> none"}, false},
		{"passport.go_version", func(_, n *crRun) { n.passport.GoVersion = "go1.26.9" },
			[]string{"differs: passport.go_version: go1.27.1 -> go1.26.9"}, false},
		{"no passport on the base", func(b, _ *crRun) { b.passport = nil },
			[]string{"base.json: " + crNoPassport}, false},
		{"no passport on the new", func(_, n *crRun) { n.passport = nil },
			[]string{"new.json: " + crNoPassport}, false},
		{"no passport on both", func(b, n *crRun) { b.passport, n.passport = nil, nil },
			[]string{"base.json: " + crNoPassport, "new.json: " + crNoPassport}, false},
		{"methods, one more on the new", func(_, n *crRun) { n.methods = append(n.methods, crCall("pkg.Svc/Other", crExact(crMillis(10)), 0)) },
			[]string{"differs: methods: pkg.Svc/Do -> pkg.Svc/Do, pkg.Svc/Other"}, true},
		{"methods, one renamed", func(_, n *crRun) { n.methods[0].name = "pkg.Svc/Other" },
			[]string{"differs: methods: pkg.Svc/Do -> pkg.Svc/Other"}, true},
		{"methods, none on the new", func(_, n *crRun) { n.methods = nil },
			[]string{"differs: methods: pkg.Svc/Do -> "}, true},
		{"dataset", func(b, n *crRun) { b.methods[0].datasetSHA, n.methods[0].datasetSHA = c, d },
			[]string{"differs: dataset pkg.Svc/Do: " + c + " -> " + d}, false},
		{"dataset gone on the new", func(b, _ *crRun) { b.methods[0].datasetSHA = c },
			[]string{"differs: dataset pkg.Svc/Do: " + c + " -> none"}, false},
		{"dataset only on the new", func(_, n *crRun) { n.methods[0].datasetSHA = e },
			[]string{"differs: dataset pkg.Svc/Do: none -> " + e}, false},
	} {
		for _, limits := range [][2]string{{"", ""}, {"20%", "1%"}} {
			base, nw := crBase(), crNew()
			tc.edit(&base, &nw)

			out, got := crCompare(t, base, nw, limits[0], limits[1])

			if diffs := crDiffs(out); !slices.Equal(diffs, tc.want) {
				t.Errorf("%s %v: difference lines %q, want %q\n%s", tc.name, limits, diffs, tc.want, out)
			}

			crWantResult(t, got, Comparison{Judged: limits[0] != ""})

			if tc.methodsRow {
				continue
			}

			lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			if limits[0] == "" {
				if last := lines[len(lines)-1]; last != "reports are not comparable: nothing judged" {
					t.Errorf("%s: last line %q, want the not comparable closing line", tc.name, last)
				}

				continue
			}

			for _, prefix := range []string{"pkg.Svc/Do: p99", "pkg.Svc/Do: errors"} {
				if line := crLine(t, out, prefix); !strings.HasSuffix(line, ", unknown, reports are not comparable") {
					t.Errorf("%s: line %q, want it unknown, reports are not comparable", tc.name, line)
				}
			}

			if last := lines[len(lines)-1]; !strings.HasPrefix(last, "pkg.Svc/Do: errors") {
				t.Errorf("%s: last line %q, want the last check, nothing after it", tc.name, last)
			}
		}
	}
}

// Ground: contract — the differences are named in the order of the spec:
// target, version, the passport (a missing one, or its fields in the order of
// the passport), the sets of methods, then each dataset in the base report's
// order, not sorted; the set line is sorted and carries the names without the
// leading slash. A method on one side only has no dataset line.
func TestCompareRuns_DifferencesComeInTheSpecOrder(t *testing.T) {
	c, d, e := strings.Repeat("c", 64), strings.Repeat("d", 64), strings.Repeat("e", 64)
	crTwo := func(names ...string) []crMethod {
		var ms []crMethod
		for _, n := range names {
			ms = append(ms, crCall(n, crExact(crMillis(10)), 0))
		}

		return ms
	}

	base, nw := crBase(), crNew()
	base.methods, nw.methods = crTwo("pkg.Svc/B", "pkg.Svc/A", "pkg.Svc/C"), crTwo("pkg.Svc/A", "pkg.Svc/B", "pkg.Svc/D")
	base.methods[0].datasetSHA, nw.methods[1].datasetSHA = c, d
	base.methods[1].datasetSHA, nw.methods[0].datasetSHA = "", e
	nw.target, nw.version = "10.0.0.2:50051", "v0.3.1"
	nw.passport.ConfigSHA256, nw.passport.OS, nw.passport.GoVersion = strings.Repeat("b", 64), "darwin", "go1.26.9"

	out, _ := crCompare(t, base, nw, "", "")

	want := []string{
		"differs: target: 10.0.0.1:50051 -> 10.0.0.2:50051",
		"differs: leettest_version: v0.3.0 -> v0.3.1",
		"differs: passport.config_sha256: " + strings.Repeat("a", 64) + " -> " + strings.Repeat("b", 64) +
			" (config bytes differ: a comment or line endings count too)",
		"differs: passport.os: linux -> darwin",
		"differs: passport.go_version: go1.27.1 -> go1.26.9",
		"differs: methods: pkg.Svc/A, pkg.Svc/B, pkg.Svc/C -> pkg.Svc/A, pkg.Svc/B, pkg.Svc/D",
		"differs: dataset pkg.Svc/B: " + c + " -> " + d,
		"differs: dataset pkg.Svc/A: none -> " + e,
	}
	if got := crDiffs(out); !slices.Equal(got, want) {
		t.Errorf("difference lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// A missing passport stands where the passport fields would, and its
	// fields are not compared.
	base, nw = crBase(), crNew()
	base.passport = nil
	nw.target, nw.passport.OS = "10.0.0.2:50051", "darwin"
	nw.methods = append(nw.methods, crCall("pkg.Svc/Other", crExact(crMillis(10)), 0))

	out, _ = crCompare(t, base, nw, "", "")

	want = []string{
		"differs: target: 10.0.0.1:50051 -> 10.0.0.2:50051",
		"base.json: " + crNoPassport,
		"differs: methods: pkg.Svc/Do -> pkg.Svc/Do, pkg.Svc/Other",
	}
	if got := crDiffs(out); !slices.Equal(got, want) {
		t.Errorf("difference lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Ground: contract — labels are what differs by design: printed on the header,
// never compared, and a report without a name prints a dash. The inverse is
// the verdict: the runs are compared and the checks pass.
func TestCompareRuns_LabelsAreNotCompared(t *testing.T) {
	base, nw := crBase(), crNew()
	base.passport.Labels = map[string]string{"region": "eu", "env": "prod"}
	nw.passport.Labels = map[string]string{"env": "staging"}
	nw.name = ""

	out, c := crCompare(t, base, nw, "20%", "1%")

	crWantFrame(t, out, "base: base.json  svc  2026-10-01T10:00:00Z  labels: env=prod, region=eu\n"+
		"new:  new.json  -  2026-10-02T10:00:00Z  labels: env=staging\n"+
		"pkg.Svc/Do: p99 100ms -> 100ms, no change, limit 20%: pass\n"+
		"pkg.Svc/Do: errors 1.0% -> 1.0%, no change, limit 1%: pass\n")
	crWantResult(t, c, Comparison{Comparable: true, Judged: true, Verdict: compare.Pass})
}

// Ground: contract — the name of a method is compared without the leading
// slash: a report that carries it and one that does not name the same method.
func TestCompareRuns_TheLeadingSlashOfAMethodIsNotCompared(t *testing.T) {
	base, nw := crBase().file(t, "base.json"), crNew().file(t, "new.json")
	nw.Report.Methods[0].Method = strings.TrimPrefix(nw.Report.Methods[0].Method, "/")

	var out bytes.Buffer
	c := CompareRuns(&out, base, nw, crLimit(t, "-max-p99-rise", "20%"), nil)

	if diffs := crDiffs(out.String()); len(diffs) != 0 {
		t.Errorf("difference lines %q, want none\n%s", diffs, out.String())
	}

	if got, want := crLine(t, out.String(), "pkg.Svc/Do: p99"), "pkg.Svc/Do: p99 100ms -> 100ms, no change, limit 20%: pass"; got != want {
		t.Errorf("p99 line %q, want %q", got, want)
	}

	crWantResult(t, c, Comparison{Comparable: true, Judged: true, Verdict: compare.Pass})
}

// Ground: contract — a run or a method that measured nothing about the target
// is never compared, however the numbers look; the state decides before the
// check does, in the order not comparable, invalid, incomplete, and a
// client-side wait last. The words name the side and, for a method, why.
func TestCompareRuns_RunStateDecidesBeforeTheCheck(t *testing.T) {
	invalid := func(reason string) crMethod {
		m := crCall(crDo, crExact(crMillis(100)), 100)
		m.sent = 100

		switch reason {
		case "request_error":
			m.rejected = 100
		case "client_error":
			m.clientError = 100
		case "bad_response":
			m.badResponse = 100
		case "mixed":
			m.rejected, m.clientError = 50, 50
		}

		return m
	}

	for _, tc := range []struct {
		name string
		edit func(base, nw *crRun)
		want string
	}{
		{"the base run is invalid", func(b, _ *crRun) { b.outcome = OutcomeInvalid }, "base run is invalid"},
		{"the new run is invalid", func(_, n *crRun) { n.outcome = OutcomeInvalid }, "new run is invalid"},
		{"the base run is incomplete", func(b, _ *crRun) { b.outcome = OutcomeIncomplete }, "base run is incomplete"},
		{"the new run is incomplete", func(_, n *crRun) { n.outcome = OutcomeIncomplete }, "new run is incomplete"},
		{"the new method is request_error", func(_, n *crRun) { n.methods[0] = invalid("request_error") },
			"new method is invalid (request_error)"},
		{"the base method is client_error", func(b, _ *crRun) { b.methods[0] = invalid("client_error") },
			"base method is invalid (client_error)"},
		{"the new method is bad_response", func(_, n *crRun) { n.methods[0] = invalid("bad_response") },
			"new method is invalid (bad_response)"},
		{"the new method is mixed", func(_, n *crRun) { n.methods[0] = invalid("mixed") },
			"new method is invalid (mixed)"},
		{"invalid before incomplete: incomplete base, invalid new", func(b, n *crRun) {
			b.outcome, n.outcome = OutcomeIncomplete, OutcomeInvalid
		}, "new run is invalid"},
		{"invalid before incomplete: incomplete base, invalid new method", func(b, n *crRun) {
			b.outcome = OutcomeIncomplete
			n.methods[0] = invalid("request_error")
		}, "new method is invalid (request_error)"},
		{"invalid before a client-side wait", func(_, n *crRun) {
			n.outcome, n.tail = OutcomeInvalid, "generator"
		}, "new run is invalid"},
		{"incomplete before a client-side wait", func(_, n *crRun) {
			n.outcome, n.tail = OutcomeIncomplete, "stream"
		}, "new run is incomplete"},
		{"not comparable before invalid", func(_, n *crRun) {
			n.outcome, n.target = OutcomeInvalid, "10.0.0.2:50051"
		}, "reports are not comparable"},
	} {
		base, nw := crBase(), crNew()
		tc.edit(&base, &nw)

		out, c := crCompare(t, base, nw, "20%", "1%")

		for _, check := range []struct{ prefix, limit string }{{"pkg.Svc/Do: p99", "20%"}, {"pkg.Svc/Do: errors", "1%"}} {
			want := ", limit " + check.limit + ": unknown, " + tc.want
			if line := crLine(t, out, check.prefix); !strings.HasSuffix(line, want) {
				t.Errorf("%s: line %q, want it to end %q", tc.name, line, want)
			}
		}

		if c.Verdict != compare.Unknown || !c.Judged {
			t.Errorf("%s: comparison %+v, want judged and unknown", tc.name, c)
		}
	}
}

// Ground: contract — a run that its own side held back says nothing of the
// target: when the generator or the stream limit set the tail
// (tail_wait_cause), or the method has calls unsent for a reason that is not
// the connection. The reason is the tail cause first, else the method's
// unsent calls: generator, then stream, else generator. The inverse is a
// connection cause and calls not sent because of the connection: the target's
// side, compared as usual. When both runs are held back the base's reason
// comes first.
func TestCompareRuns_TheRunSideIsReadFromTheReport(t *testing.T) {
	const (
		gen    = "the generator held calls back"
		stream = "the stream limit held calls back"
	)

	unsent := func(notSent, generator, streamed, connection int) func(*crRun) {
		return func(r *crRun) {
			m := &r.methods[0]
			m.notSent, m.notSentGenerator, m.notSentStream, m.notSentConnection = notSent, generator, streamed, connection
		}
	}
	tail := func(cause string) func(*crRun) { return func(r *crRun) { r.tail = cause } }
	both := func(fs ...func(*crRun)) func(*crRun) {
		return func(r *crRun) {
			for _, f := range fs {
				f(r)
			}
		}
	}

	for _, tc := range []struct {
		name      string
		base, nw  func(*crRun)
		want      string
		wantLimit string
	}{
		{"new tail cause generator", func(*crRun) {}, tail("generator"), "new: " + gen, ""},
		{"base tail cause stream", tail("stream"), func(*crRun) {}, "base: " + stream, ""},
		{"base tail cause generator", tail("generator"), func(*crRun) {}, "base: " + gen, ""},
		{"new tail cause stream", func(*crRun) {}, tail("stream"), "new: " + stream, ""},
		{"both held back: the base first", tail("stream"), tail("generator"), "base: " + stream, ""},
		{"both held back by calls: the base first", unsent(5, 0, 5, 0), unsent(5, 5, 0, 0), "base: " + stream, ""},
		{"new calls unsent by the generator", func(*crRun) {}, unsent(5, 5, 0, 0), "new: " + gen, ""},
		{"new calls unsent by the stream", func(*crRun) {}, unsent(5, 0, 5, 0), "new: " + stream, ""},
		{"base calls unsent by the stream", unsent(5, 0, 5, 0), func(*crRun) {}, "base: " + stream, ""},
		{"the generator before the stream", func(*crRun) {}, unsent(5, 2, 3, 0), "new: " + gen, ""},
		{"unsent with no breakdown", func(*crRun) {}, unsent(5, 0, 0, 0), "new: " + gen, ""},
		{"unsent beside the connection's", func(*crRun) {}, unsent(7, 0, 2, 5), "new: " + stream, ""},
		{"the tail cause before the calls", func(*crRun) {}, both(tail("stream"), unsent(5, 5, 0, 0)), "new: " + stream, ""},
		{"a connection cause is the target's side", func(*crRun) {}, tail("connection"), "", "pass"},
		{"calls unsent for the connection only", func(*crRun) {}, unsent(5, 0, 0, 5), "", "pass"},
	} {
		base, nw := crBase(), crNew()
		tc.base(&base)
		tc.nw(&nw)

		out, _ := crCompare(t, base, nw, "20%", "5%")

		for _, check := range []struct{ prefix, limit string }{{"pkg.Svc/Do: p99", "20%"}, {"pkg.Svc/Do: errors", "5%"}} {
			want := ", limit " + check.limit + ": " + tc.wantLimit
			if tc.want != "" {
				want = ", limit " + check.limit + ": unknown, " + tc.want
			}

			if line := crLine(t, out, check.prefix); !strings.HasSuffix(line, want) {
				t.Errorf("%s: line %q, want it to end %q", tc.name, line, want)
			}
		}
	}
}

// Ground: contract — the side is judged per method: one method with calls
// unsent for its own side is unknown, the other, compared as usual, passes.
func TestCompareRuns_TheRunSideIsPerMethod(t *testing.T) {
	base, nw := crBase(), crNew()
	base.methods = append(base.methods, crCall("pkg.Svc/Other", crExact(crMillis(50)), 5))
	nw.methods = append(nw.methods, crCall("pkg.Svc/Other", crExact(crMillis(50)), 5))
	nw.methods[1].notSent, nw.methods[1].notSentGenerator = 5, 5

	out, _ := crCompare(t, base, nw, "20%", "5%")

	for prefix, want := range map[string]string{
		"pkg.Svc/Do: p99":       ", limit 20%: pass",
		"pkg.Svc/Do: errors":    ", limit 5%: pass",
		"pkg.Svc/Other: p99":    ", limit 20%: unknown, new: the generator held calls back",
		"pkg.Svc/Other: errors": ", limit 5%: unknown, new: the generator held calls back",
	} {
		if line := crLine(t, out, prefix); !strings.HasSuffix(line, want) {
			t.Errorf("line %q, want it to end %q", line, want)
		}
	}
}

// Ground: contract — the change in p99 is worded by what is known, the unit-1
// capacity wording with the direction of the p99: an exact pair is a rise or a
// fall to a tenth, half up; a lower bound on the new side leaves the rise open
// above (`>=`) and a fall open below (`<=`); one on the base side the other
// way; both bounds, a p99 not defined and a base of 0 say no number. A
// printed bound is rounded toward what is known: a lower bound of the rise
// down, an upper bound up.
func TestCompareRuns_P99ChangeIsWordedByWhatIsKnown(t *testing.T) {
	exact := func(n int) metrics.Quantile { return crExact(crMillis(n)) }
	lower := func(n int) metrics.Quantile { return crLower(crMillis(n)) }

	for _, tc := range []struct {
		name     string
		base, nw metrics.Quantile
		want     string
	}{
		{"a rise", exact(300), exact(400), "p99 300ms -> 400ms, rise 33.3%"},
		{"a fall, rounded half up", exact(300), exact(100), "p99 300ms -> 100ms, fall 66.7%"},
		{"no change", exact(100), exact(100), "p99 100ms -> 100ms, no change"},
		{"new a lower bound, above", exact(300), lower(400), "p99 300ms -> >400ms, rise >= 33.3%"},
		{"new a lower bound, below", exact(300), lower(200), "p99 300ms -> >200ms, fall <= 33.4%"},
		{"new a lower bound, at the base", exact(300), lower(300), "p99 300ms -> >300ms, rise >= 0.0%"},
		{"base a lower bound, new above", lower(300), exact(400), "p99 >300ms -> 400ms, rise <= 33.4%"},
		{"base a lower bound, new below", lower(300), exact(200), "p99 >300ms -> 200ms, fall >= 33.3%"},
		{"base a lower bound, at the base", lower(300), exact(300), "p99 >300ms -> 300ms, fall >= 0.0%"},
		{"both lower bounds", lower(300), lower(200), "p99 >300ms -> >200ms, rise -"},
		{"base 0", exact(0), exact(100), "p99 0 -> 100ms, rise -"},
		{"base not defined", metrics.Quantile{}, exact(100), "p99 - -> 100ms, rise -"},
		{"new not defined", exact(100), metrics.Quantile{}, "p99 100ms -> -, rise -"},
	} {
		base, nw := crBase(), crNew()
		base.methods[0].p99, nw.methods[0].p99 = tc.base, tc.nw

		out, _ := crCompare(t, base, nw, "", "")

		if got, want := crLine(t, out, "pkg.Svc/Do: p99"), "pkg.Svc/Do: "+tc.want; got != want {
			t.Errorf("%s:\n  got  %q\n  want %q", tc.name, got, want)
		}
	}
}

// Ground: contract — with a limit each reason of a p99 that cannot be judged
// is said in words, and a judged line carries its bound beside the verdict: a
// bound past the limit on the side that can only grow the rise is FAIL, a
// bound that cannot be told apart is unknown, with the side named.
func TestCompareRuns_P99UnknownSaysWhy(t *testing.T) {
	exact := func(n int) metrics.Quantile { return crExact(crMillis(n)) }
	lower := func(n int) metrics.Quantile { return crLower(crMillis(n)) }

	for _, tc := range []struct {
		name     string
		base, nw metrics.Quantile
		want     string
	}{
		{"base not defined", metrics.Quantile{}, exact(100), "p99 - -> 100ms, rise -, limit 20%: unknown, too few observations for p99"},
		{"new not defined", exact(100), metrics.Quantile{}, "p99 100ms -> -, rise -, limit 20%: unknown, too few observations for p99"},
		{"base 0", exact(0), exact(100), "p99 0 -> 100ms, rise -, limit 20%: unknown, base p99 is 0 us"},
		{"both lower bounds", lower(100), lower(100), "p99 >100ms -> >100ms, rise -, limit 20%: unknown, both p99 only a lower bound (timeouts)"},
		{"base a lower bound, new past the limit", lower(100), exact(130),
			"p99 >100ms -> 130ms, rise <= 30.0%, limit 20%: unknown, base p99 only a lower bound (timeouts)"},
		{"base a lower bound, new within the limit", lower(100), exact(110), "p99 >100ms -> 110ms, rise <= 10.0%, limit 20%: pass"},
		{"new a lower bound, past the limit", exact(100), lower(130), "p99 100ms -> >130ms, rise >= 30.0%, limit 20%: FAIL"},
		{"new a lower bound, within the limit", exact(100), lower(110),
			"p99 100ms -> >110ms, rise >= 10.0%, limit 20%: unknown, new p99 only a lower bound (timeouts)"},
	} {
		base, nw := crBase(), crNew()
		base.methods[0].p99, nw.methods[0].p99 = tc.base, tc.nw

		out, _ := crCompare(t, base, nw, "20%", "")

		if got, want := crLine(t, out, "pkg.Svc/Do: p99"), "pkg.Svc/Do: "+tc.want; got != want {
			t.Errorf("%s:\n  got  %q\n  want %q", tc.name, got, want)
		}
	}
}

// Ground: boundary — a printed rise never argues with the verdict: exactly on
// the limit it prints the limit, a hair past it prints more than the limit
// (rounded up to the digits it takes), and a limit with digits shows them when
// the tenth would pass them. The same for the error share, in points.
func TestCompareRuns_APrintedRiseNeverArguesWithTheVerdict(t *testing.T) {
	p99 := func(us int64) crRun {
		r := crNew()
		r.methods[0].p99 = crExact(time.Duration(us) * time.Microsecond)

		return r
	}
	failures := func(bad, sent int) crRun {
		r := crNew()
		r.methods[0].sent, r.methods[0].failed = sent, bad

		return r
	}
	zeroBase := crBase()
	zeroBase.methods[0].sent, zeroBase.methods[0].failed = 100000, 0

	for _, tc := range []struct {
		name         string
		base, nw     crRun
		p99Limit     string
		errsLimit    string
		prefix, want string
	}{
		{"p99 on the limit", crBase(), p99(120000), "20%", "", "pkg.Svc/Do: p99", "p99 100ms -> 120ms, rise 20.0%, limit 20%: pass"},
		{"p99 a microsecond-hair past the limit", crBase(), p99(120040), "20%", "", "pkg.Svc/Do: p99",
			"p99 100ms -> 120ms, rise 20.1%, limit 20%: FAIL"},
		{"p99 on a limit with digits", crBase(), p99(120060), "20.06%", "", "pkg.Svc/Do: p99",
			"p99 100ms -> 120ms, rise 20.06%, limit 20.06%: pass"},
		{"errors on the limit", crBase(), failures(20, 1000), "", "1%", "pkg.Svc/Do: errors",
			"errors 1.0% -> 2.0%, rise 1.0%, limit 1%: pass"},
		{"errors a hair past the limit", zeroBase, failures(1004, 100000), "", "1%", "pkg.Svc/Do: errors",
			"errors 0.0% -> 1.0%, rise 1.1%, limit 1%: FAIL"},
	} {
		out, _ := crCompare(t, tc.base, tc.nw, tc.p99Limit, tc.errsLimit)

		if got, want := crLine(t, out, tc.prefix), "pkg.Svc/Do: "+tc.want; got != want {
			t.Errorf("%s:\n  got  %q\n  want %q", tc.name, got, want)
		}
	}
}

// Ground: contract — the share of failed calls counts what the run report
// counts: the calls sent and those lost to the connection are the total; the
// failed and those lost to the connection are the bad ones. Failed calls the
// client's side owns (rejected requests, client errors, our bad responses)
// make a rise past the limit unknown, not a fail. The inverse: failed calls
// with none of them are a fail.
func TestCompareRuns_TheErrorShareIsCountedAsThePassLimitCountsIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(m *crMethod)
		want string
	}{
		{"calls lost to the connection are in both counts", func(m *crMethod) {
			m.sent, m.failed, m.notSent, m.notSentConnection = 900, 0, 100, 100
		}, "errors 0.0% -> 10.0%, rise 10.0%, limit 5%: FAIL"},
		{"failed calls of the target are a fail", func(m *crMethod) { m.failed = 30 },
			"errors 0.0% -> 3.0%, rise 3.0%, limit 1%: FAIL"},
		{"rejected requests are the client's", func(m *crMethod) { m.failed, m.rejected = 30, 30 },
			"errors 0.0% -> 3.0%, rise 3.0%, limit 1%: unknown, client-side errors decide it"},
		{"client errors are the client's", func(m *crMethod) { m.failed, m.clientError = 30, 30 },
			"errors 0.0% -> 3.0%, rise 3.0%, limit 1%: unknown, client-side errors decide it"},
		{"bad responses are the client's", func(m *crMethod) { m.failed, m.badResponse = 30, 30 },
			"errors 0.0% -> 3.0%, rise 3.0%, limit 1%: unknown, client-side errors decide it"},
	} {
		base, nw := crBase(), crNew()
		base.methods[0].failed = 0
		tc.edit(&nw.methods[0])

		limit := "1%"
		if strings.Contains(tc.want, "limit 5%") {
			limit = "5%"
		}

		out, _ := crCompare(t, base, nw, "", limit)

		if got, want := crLine(t, out, "pkg.Svc/Do: errors"), "pkg.Svc/Do: "+tc.want; got != want {
			t.Errorf("%s:\n  got  %q\n  want %q", tc.name, got, want)
		}
	}
}

// Ground: contract — the change in the error share is a difference in points,
// to a tenth and half up; the same share on different totals is no change; a
// side with nothing sent has no share to take: a dash and no number.
func TestCompareRuns_ErrorsChangeIsWordedInPoints(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		baseSent, baseFailed int
		newSent, newFailed   int
		limit                string
		want                 string
	}{
		{"the same share on other totals", 1000, 10, 2000, 20, "", "errors 1.0% -> 1.0%, no change"},
		{"a fall", 1000, 10, 1000, 5, "", "errors 1.0% -> 0.5%, fall 0.5%"},
		{"a rise of a quarter point rounds up", 1000, 0, 400, 1, "", "errors 0.0% -> 0.3%, rise 0.3%"},
		{"a fall of a quarter point rounds up", 400, 1, 1000, 0, "", "errors 0.3% -> 0.0%, fall 0.3%"},
		{"nothing sent by the base", 0, 0, 1000, 10, "", "errors - -> 1.0%, rise -"},
		{"nothing sent by the new", 1000, 10, 0, 0, "", "errors 1.0% -> -, rise -"},
		{"nothing sent by the base, a limit", 0, 0, 1000, 10, "1%", "errors - -> 1.0%, rise -, limit 1%: unknown, base: nothing sent"},
		{"nothing sent by the new, a limit", 1000, 10, 0, 0, "1%", "errors 1.0% -> -, rise -, limit 1%: unknown, new: nothing sent"},
	} {
		base, nw := crBase(), crNew()
		base.methods[0].sent, base.methods[0].failed = tc.baseSent, tc.baseFailed
		nw.methods[0].sent, nw.methods[0].failed = tc.newSent, tc.newFailed

		out, _ := crCompare(t, base, nw, "", tc.limit)

		if got, want := crLine(t, out, "pkg.Svc/Do: errors"), "pkg.Svc/Do: "+tc.want; got != want {
			t.Errorf("%s:\n  got  %q\n  want %q", tc.name, got, want)
		}
	}
}

// Ground: contract — methods are listed in the base report's order, the p99
// and then the errors of one before the next; the new report's order does not
// matter.
func TestCompareRuns_MethodsFollowTheBaseReportOrder(t *testing.T) {
	base, nw := crBase(), crNew()
	base.methods = []crMethod{crCall("pkg.Svc/B", crExact(crMillis(100)), 10), crCall("pkg.Svc/A", crExact(crMillis(200)), 20)}
	nw.methods = []crMethod{crCall("pkg.Svc/A", crExact(crMillis(220)), 20), crCall("pkg.Svc/B", crExact(crMillis(150)), 10)}

	out, _ := crCompare(t, base, nw, "", "")

	crWantFrame(t, out, crHeader+
		"pkg.Svc/B: p99 100ms -> 150ms, rise 50.0%\n"+
		"pkg.Svc/B: errors 1.0% -> 1.0%, no change\n"+
		"pkg.Svc/A: p99 200ms -> 220ms, rise 10.0%\n"+
		"pkg.Svc/A: errors 2.0% -> 2.0%, no change\n"+
		"no limit given: nothing judged (-max-p99-rise, -max-errors-rise)\n")
}

// Ground: contract — a limit is given for one check only: that check carries
// its verdict, the other line none, and the closing line about no limit is not
// printed. The inverse of the no-limit frame.
func TestCompareRuns_OneLimitJudgesOneCheck(t *testing.T) {
	out, c := crCompare(t, crBase(), crNew(), "20%", "")

	crWantFrame(t, out, crHeader+
		"pkg.Svc/Do: p99 100ms -> 100ms, no change, limit 20%: pass\n"+
		"pkg.Svc/Do: errors 1.0% -> 1.0%, no change\n")
	crWantResult(t, c, Comparison{Comparable: true, Judged: true, Verdict: compare.Pass})

	out, c = crCompare(t, crBase(), crNew(), "", "1%")

	crWantFrame(t, out, crHeader+
		"pkg.Svc/Do: p99 100ms -> 100ms, no change\n"+
		"pkg.Svc/Do: errors 1.0% -> 1.0%, no change, limit 1%: pass\n")
	crWantResult(t, c, Comparison{Comparable: true, Judged: true, Verdict: compare.Pass})
}

// Ground: contract — one result for all the checks: a fail beside an unknown is
// a fail, an unknown beside a pass is unknown, a pass everywhere is a pass.
func TestCompareRuns_TheResultIsTheWorstCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(m *crMethod)
		want compare.Verdict
	}{
		{"a fail beside an unknown", func(m *crMethod) {
			m.p99, m.failed, m.rejected = crExact(crMillis(130)), 30, 30
		}, compare.Fail},
		{"an unknown beside a pass", func(m *crMethod) { m.failed, m.rejected = 30, 30 }, compare.Unknown},
		{"a pass beside a pass", func(*crMethod) {}, compare.Pass},
	} {
		nw := crNew()
		tc.edit(&nw.methods[0])
		base := crBase()
		base.methods[0].failed = 0

		_, c := crCompare(t, base, nw, "20%", "1%")

		crWantResult(t, c, Comparison{Comparable: true, Judged: true, Verdict: tc.want})
	}
}
