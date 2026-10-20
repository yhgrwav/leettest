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

package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/compare"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
	"github.com/yhgrwav/leettest/pkg/share"
)

// runSpec is a plain run to write as a JSON report, with only the fields
// leettest compare reads varying: one method of 1000 calls.
type runSpec struct {
	target string
	p99    metrics.Quantile
	// failed are the failed calls; clientError of them are the client's own.
	failed, clientError int
}

func okRun() runSpec {
	return runSpec{
		target: cmpTarget,
		p99:    metrics.Quantile{Value: 100 * time.Millisecond, Exact: true, Defined: true},
		failed: 10,
	}
}

// write builds the report from the real JSON types, the method named with a
// leading slash as a real run names it.
func (s runSpec) write(t *testing.T, dir, name string) string {
	t.Helper()

	return writeReportFile(t, dir, name, cli.NewJSONReport(cli.JSONRun{
		Target: s.target, Name: "svc", Version: cmpVersion, Outcome: cli.OutcomeComplete,
		StartedAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
		Run: cli.RunReport{
			Report: engine.Report{Methods: []engine.MethodReport{{
				Method: "/" + cmpMethod, Sent: 1000, Failed: s.failed, ClientError: s.clientError, P99: s.p99,
			}}},
			Passport: cmpPassport(),
		},
	}))
}

// comparedRuns runs leettest compare on two plain runs, flags before the files.
func comparedRuns(t *testing.T, base, nw runSpec, flags ...string) result {
	t.Helper()

	dir := t.TempDir()
	basePath, newPath := base.write(t, dir, "base.json"), nw.write(t, dir, "new.json")

	return runCLI(t.Context(), t, 10*time.Second, append(append([]string{"compare"}, flags...), basePath, newPath)...)
}

func wantMessage(t *testing.T, res result, want string) {
	t.Helper()

	if res.err == nil || res.err.Error() != want {
		t.Errorf("error %v, want %q", res.err, want)
	}
}

// Ground: contract — every limit met: exit 0, no error, the verdict of each
// check on the output.
func TestCompareRuns_AllChecksPassExit0(t *testing.T) {
	nw := okRun()
	nw.p99.Value = 110 * time.Millisecond

	res := comparedRuns(t, okRun(), nw, "-max-p99-rise", "20%", "-max-errors-rise", "1%")

	wantExit(t, res, 0)

	if res.err != nil {
		t.Errorf("error %v, want none", res.err)
	}

	for prefix, want := range map[string]string{
		cmpMethod + ": p99":    cmpMethod + ": p99 100ms -> 110ms, rise 10.0%, limit 20%: pass",
		cmpMethod + ": errors": cmpMethod + ": errors 1.0% -> 1.0%, no change, limit 1%: pass",
	} {
		if got := lineStarting(t, res.stdout, prefix); got != want {
			t.Errorf("line %q, want %q", got, want)
		}
	}
}

// Ground: contract — no limit given, the runs comparable: exit 0, and the
// output says nothing was judged.
func TestCompareRuns_NoLimitExit0(t *testing.T) {
	res := comparedRuns(t, okRun(), okRun())

	wantExit(t, res, 0)

	if got, want := lastLine(res.stdout), "no limit given: nothing judged (-max-p99-rise, -max-errors-rise)"; got != want {
		t.Errorf("last line %q, want %q", got, want)
	}
}

// Ground: contract — a check past its limit is exit 4 with the fail text, and
// stays 4 beside a check that could not be decided; the inverse is the
// undecided check alone, which is 2 (TestCompareRuns_UnknownExit2).
func TestCompareRuns_FailBesideUnknownExit4(t *testing.T) {
	nw := okRun()
	nw.p99.Value = 130 * time.Millisecond
	nw.failed, nw.clientError = 30, 30

	res := comparedRuns(t, okRun(), nw, "-max-p99-rise", "20%", "-max-errors-rise", "1%")

	wantExit(t, res, 4)
	wantMessage(t, res, "compare: a limit was not met")

	if !errors.Is(res.err, ErrCompareFailed) {
		t.Errorf("error %v, want ErrCompareFailed", res.err)
	}

	for prefix, want := range map[string]string{
		cmpMethod + ": p99":    ", limit 20%: FAIL",
		cmpMethod + ": errors": ", limit 1%: unknown, client-side errors decide it",
	} {
		if got := lineStarting(t, res.stdout, prefix); !strings.HasSuffix(got, want) {
			t.Errorf("line %q, want it to end %q", got, want)
		}
	}
}

// Ground: contract — a check that cannot be decided, none failed: exit 2 with
// its own text, which does not call the run invalid.
func TestCompareRuns_UnknownExit2(t *testing.T) {
	nw := okRun()
	nw.p99.Exact = false

	res := comparedRuns(t, okRun(), nw, "-max-p99-rise", "20%")

	wantExit(t, res, 2)
	wantMessage(t, res, "compare: a limit could not be decided")

	if !errors.Is(res.err, ErrCompareUndecided) || errors.Is(res.err, ErrInvalidRun) {
		t.Errorf("error %v, want ErrCompareUndecided and not ErrInvalidRun", res.err)
	}
}

// Ground: contract — reports that are not comparable are exit 2 with limits
// and without: nothing was decided, and the text says so, not that the run is
// invalid.
func TestCompareRuns_NotComparableExit2(t *testing.T) {
	other := okRun()
	other.target = "10.0.0.2:50051"

	for _, flags := range [][]string{nil, {"-max-p99-rise", "20%"}, {"-max-errors-rise", "1%"}} {
		res := comparedRuns(t, okRun(), other, flags...)

		wantExit(t, res, 2)
		wantMessage(t, res, "compare: reports are not comparable: nothing decided")

		if errors.Is(res.err, ErrInvalidRun) {
			t.Errorf("flags %v: error %v wraps ErrInvalidRun", flags, res.err)
		}
	}
}

// Ground: contract — the two new sentinels map to 2 and 4, bare and wrapped.
func TestExitCode_CompareSentinels(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{ErrCompareUndecided, 2},
		{fmt.Errorf("compare: %w", ErrCompareUndecided), 2},
		{ErrCompareFailed, 4},
		{fmt.Errorf("compare: %w", ErrCompareFailed), 4},
	} {
		if got := exitCode(tc.err); got != tc.want {
			t.Errorf("exitCode(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

// Ground: contract — a met-or-not answer outranks "could not decide": a
// failed limit is 4 whatever else is open, and an undecided one is 2 whether
// the reports differ or a check could not tell; nothing asked and held is nil.
func TestCompareResult_FailOutranksUndecided(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  cli.Comparison
		want error
	}{
		{"fail, reports differ", cli.Comparison{Comparable: false, Judged: true, Verdict: compare.Fail}, ErrCompareFailed},
		{"fail, reports alike", cli.Comparison{Comparable: true, Judged: true, Verdict: compare.Fail}, ErrCompareFailed},
		{"unknown", cli.Comparison{Comparable: true, Judged: true, Verdict: compare.Unknown}, ErrCompareUndecided},
		{"reports differ, nothing judged", cli.Comparison{Comparable: false}, ErrCompareUndecided},
		{"pass", cli.Comparison{Comparable: true, Judged: true, Verdict: compare.Pass}, nil},
		{"no limit", cli.Comparison{Comparable: true}, nil},
	} {
		got := compareResult(tc.res)
		if (tc.want == nil) != (got == nil) || (tc.want != nil && !errors.Is(got, tc.want)) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Ground: contract — a limit that only belongs to the other kind of report is
// refused before anything is compared: exit 1, nothing on stdout, the first
// such flag named.
func TestCompareRuns_FlagAndModeMismatchExit1(t *testing.T) {
	dir := t.TempDir()
	base, nw := brokeAt(1000).write(t, dir, "sbase.json"), brokeAt(950).write(t, dir, "snew.json")
	runBase, runNew := okRun().write(t, dir, "rbase.json"), okRun().write(t, dir, "rnew.json")

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"p99 limit with searches", []string{"-max-p99-rise", "20%", base, nw}, "compare: -max-p99-rise applies to plain runs"},
		{"errors limit with searches", []string{"-max-errors-rise", "1%", base, nw}, "compare: -max-errors-rise applies to plain runs"},
		{"both limits with searches", []string{"-max-p99-rise", "20%", "-max-errors-rise", "1%", base, nw},
			"compare: -max-p99-rise applies to plain runs"},
		{"capacity limit with runs", []string{"-max-capacity-drop", "10%", runBase, runNew},
			"compare: -max-capacity-drop applies to breakpoint searches"},
	} {
		res := runCLI(t.Context(), t, 10*time.Second, append([]string{"compare"}, tc.args...)...)

		wantExit(t, res, 1)

		if res.stdout != "" {
			t.Errorf("%s: stdout %q, want nothing", tc.name, res.stdout)
		}
		wantMessage(t, res, tc.want)
	}
}

// Ground: contract — a percent of the run limits goes through the same
// grammar as the capacity limit, and its refusal names the flag with its dash.
func TestCompareRuns_LimitRefusalsNameTheFlag(t *testing.T) {
	dir := t.TempDir()
	base, nw := okRun().write(t, dir, "base.json"), okRun().write(t, dir, "new.json")

	for _, flag := range []string{"-max-p99-rise", "-max-errors-rise"} {
		_, badPercent := share.Parse("10", flag)
		if badPercent == nil {
			t.Fatalf("share.Parse accepted 10 for %s", flag)
		}

		for _, tc := range []struct {
			value string
			want  []string
		}{{"10", []string{badPercent.Error()}}, {"101%", []string{flag, "over 100%"}}} {
			res := runCLI(t.Context(), t, 10*time.Second, "compare", flag, tc.value, base, nw)

			wantExit(t, res, 1)

			if res.stdout != "" {
				t.Errorf("%s %s: stdout %q, want nothing", flag, tc.value, res.stdout)
			}

			for _, has := range tc.want {
				if !strings.Contains(res.err.Error()+res.stderr, has) {
					t.Errorf("%s %s: error %q (stderr %q), want it to say %q", flag, tc.value, res.err, res.stderr, has)
				}
			}
		}
	}
}
