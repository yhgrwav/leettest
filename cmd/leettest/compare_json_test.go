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
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// What `leettest compare -output json` writes, as the brief words it. The
// shapes are written here from the spec, not taken from the production types:
// a rename there cannot hide behind itself.

type cjSide struct {
	Path            string            `json:"path"`
	Name            string            `json:"name"`
	StartedAt       string            `json:"started_at"`
	LeetTestVersion string            `json:"leettest_version"`
	Labels          map[string]string `json:"labels"`
}

type cjCapacity struct {
	RPS   int    `json:"rps"`
	Bound string `json:"bound"`
}

type cjP99 struct {
	US         int64 `json:"us"`
	LowerBound bool  `json:"lower_bound"`
}

type cjErrors struct {
	Bad       int `json:"bad"`
	BadClient int `json:"bad_client"`
	Total     int `json:"total"`
}

type cjCheck struct {
	Method       string      `json:"method"`
	Key          string      `json:"key"`
	BaseCapacity *cjCapacity `json:"base_capacity"`
	NewCapacity  *cjCapacity `json:"new_capacity"`
	BaseOutcome  *string     `json:"base_outcome"`
	NewOutcome   *string     `json:"new_outcome"`
	BaseP99      *cjP99      `json:"base_p99"`
	NewP99       *cjP99      `json:"new_p99"`
	BaseErrors   *cjErrors   `json:"base_errors"`
	NewErrors    *cjErrors   `json:"new_errors"`
	LimitPercent *string     `json:"limit_percent"`
	Verdict      *string     `json:"verdict"`
	Reason       *string     `json:"reason"`
	Why          *string     `json:"why"`
}

type cjDiff struct {
	Field string `json:"field"`
	Base  string `json:"base"`
	New   string `json:"new"`
}

type cjReport struct {
	SchemaVersion   int       `json:"schema_version"`
	Mode            string    `json:"mode"`
	ComparedMode    string    `json:"compared_mode"`
	LeetTestVersion string    `json:"leettest_version"`
	Base            cjSide    `json:"base"`
	New             cjSide    `json:"new"`
	Comparable      bool      `json:"comparable"`
	Differences     []cjDiff  `json:"differences"`
	Verdict         *string   `json:"verdict"`
	Checks          []cjCheck `json:"checks"`
}

// cjReasons is the closed list of reasons a check can be unknown.
var cjReasons = []string{
	"not_comparable", "method_invalid", "invalid", "incomplete", "generator", "stream", "few_observations",
	"base_zero", "censored", "nothing_sent", "client_errors", "not_measured", "broke_at_first", "lower_bound",
}

// cjNotComparable is the error text of two reports that cannot be compared.
const cjNotComparable = "compare: reports are not comparable: nothing decided"

func cjDecode(t *testing.T, stdout string) cjReport {
	t.Helper()

	decodeOnly(t, stdout)

	var rep cjReport
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("stdout does not fit the compare report: %v\n%s", err, stdout)
	}

	return rep
}

// cjWantNullable: want "" is null, none of the nullable strings has "" as a
// value.
func cjWantNullable(t *testing.T, what string, got *string, want string) {
	t.Helper()

	switch {
	case want == "" && got != nil:
		t.Errorf("%s = %q, want null", what, *got)
	case want != "" && got == nil:
		t.Errorf("%s = null, want %q", what, want)
	case want != "" && *got != want:
		t.Errorf("%s = %q, want %q", what, *got, want)
	}
}

func cjText(r result) string {
	if r.err == nil {
		return ""
	}

	return r.err.Error()
}

// Plain runs for the pairs below: the same one-method run as runSpec, with
// the parts of the report compare reads open to change, and the method named
// with a slash as a real run names it.

type jrMethod struct {
	name                                                        string
	sent, failed, rejected, clientError, badResponse            int
	notSent, notSentGenerator, notSentStream, notSentConnection int
	p99                                                         metrics.Quantile
	datasetSHA                                                  string
}

type jrRun struct {
	target, version, outcome string
	startedAt                time.Time
	passport                 *cli.Passport
	methods                  []jrMethod
	// edit changes the report after it is built, for what the engine report
	// does not set directly.
	edit []func(*cli.JSONReport)
}

func jrExact(ms int) metrics.Quantile {
	return metrics.Quantile{Value: time.Duration(ms) * time.Millisecond, Exact: true, Defined: true}
}

func jrLower(ms int) metrics.Quantile {
	return metrics.Quantile{Value: time.Duration(ms) * time.Millisecond, Defined: true}
}

func jrCall(name string, p99 metrics.Quantile, failed int) jrMethod {
	return jrMethod{name: name, sent: 1000, failed: failed, p99: p99}
}

func jrBase() jrRun {
	return jrRun{
		target: cmpTarget, version: cmpVersion, outcome: cli.OutcomeComplete,
		startedAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC), passport: cmpPassport(),
		methods: []jrMethod{jrCall("/"+cmpMethod, jrExact(100), 10)},
	}
}

func jrNew() jrRun {
	return jrBase().with(func(r *jrRun) { r.startedAt = time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC) })
}

// with is the run changed by f, the original untouched.
func (r jrRun) with(f func(*jrRun)) jrRun {
	r.methods = slices.Clone(r.methods)
	r.edit = slices.Clone(r.edit)
	f(&r)

	return r
}

func jrTail(cause string) func(*jrRun) {
	return func(r *jrRun) {
		r.edit = append(r.edit, func(rep *cli.JSONReport) { rep.TailWaitCause = &cause })
	}
}

func (r jrRun) write(t *testing.T, dir, file string) string {
	t.Helper()

	rep := engine.Report{}

	for _, m := range r.methods {
		mr := engine.MethodReport{
			Method: m.name, Sent: m.sent, Failed: m.failed, P99: m.p99,
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

	out := cli.NewJSONReport(cli.JSONRun{
		Target: r.target, Name: "svc", Version: r.version, Outcome: r.outcome, StartedAt: r.startedAt,
		Run: cli.RunReport{Report: rep, Passport: r.passport},
	})
	for _, e := range r.edit {
		e(&out)
	}

	return writeReportFile(t, dir, file, out)
}

// cjPair is two reports and the flags compare runs them with. run runs the
// pair in text mode and in json mode.
type cjPair struct {
	name  string
	flags []string
	write func(t *testing.T, dir string) (base, nw string)
}

type cjRan struct {
	text, js          result
	basePath, newPath string
}

func (p cjPair) run(t *testing.T) cjRan {
	t.Helper()

	dir := t.TempDir()
	base, nw := p.write(t, dir)

	args := func(extra ...string) []string {
		a := append([]string{"compare"}, extra...)
		a = append(a, p.flags...)

		return append(a, base, nw)
	}

	return cjRan{
		text:     runCLI(t.Context(), t, 10*time.Second, args()...),
		js:       runCLI(t.Context(), t, 10*time.Second, args("-output", "json")...),
		basePath: base, newPath: nw,
	}
}

// judged: a limit was given.
func (p cjPair) judged() bool {
	return slices.ContainsFunc(p.flags, func(f string) bool { return strings.HasPrefix(f, "-max-") })
}

func cjRuns(name string, base, nw jrRun, flags ...string) cjPair {
	return cjPair{name: name, flags: flags, write: func(t *testing.T, dir string) (string, string) {
		t.Helper()

		return base.write(t, dir, "base.json"), nw.write(t, dir, "new.json")
	}}
}

func cjSearches(name string, base, nw reportSpec, flags ...string) cjPair {
	return cjPair{name: name, flags: flags, write: func(t *testing.T, dir string) (string, string) {
		t.Helper()

		return base.write(t, dir, "base.json"), nw.write(t, dir, "new.json")
	}}
}

// cjPatch rewrites a report file as a map, for what the real types cannot
// produce.
func cjPatch(t *testing.T, path string, edit func(map[string]any)) {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}

	edit(fields)

	if raw, err = json.Marshal(fields); err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}

	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

var (
	cjBothLimits = []string{"-max-p99-rise", "20%", "-max-errors-rise", "1%"}
	cjCapLimit   = []string{"-max-capacity-drop", "10%"}
)

func cjOtherTarget(r *jrRun) { r.target = "10.0.0.2:50051" }

func inOrder(methods ...jrMethod) []jrMethod { return methods }

// The pairs the text tests build, and the ones the reasons need.

func cjFixedPairs() []cjPair {
	slow, failing := jrNew(), jrNew()
	slow.methods[0].p99 = jrExact(130)
	failing.methods[0].failed, failing.methods[0].clientError = 30, 30

	slowAndFailing := slow.with(func(r *jrRun) { r.methods[0].failed, r.methods[0].clientError = 30, 30 })
	rise := jrNew().with(func(r *jrRun) { r.methods[0].p99 = jrExact(110) })
	censored := jrNew().with(func(r *jrRun) { r.methods[0].p99 = jrLower(100) })
	moreFailed := jrNew().with(func(r *jrRun) { r.methods[0].failed = 30 })

	var held = func(rps int) reportSpec { return endedAt(breakpoint.HeldThroughout, rps) }

	other := brokeAt(1000)
	other.target = "10.0.0.2:50051"

	return []cjPair{
		cjRuns("runs, all pass", jrBase(), rise, cjBothLimits...),
		cjRuns("runs, p99 past the limit", jrBase(), slow, "-max-p99-rise", "20%"),
		cjRuns("runs, errors past the limit", jrBase(), moreFailed, "-max-errors-rise", "1%"),
		cjRuns("runs, fail beside unknown", jrBase(), slowAndFailing, cjBothLimits...),
		cjRuns("runs, undecided p99", jrBase(), censored, "-max-p99-rise", "20%"),
		cjRuns("runs, only the p99 limit given", jrBase(), jrNew(), "-max-p99-rise", "20%"),
		cjRuns("runs, only the errors limit given", jrBase(), jrNew(), "-max-errors-rise", "1%"),
		cjRuns("runs, no limit, comparable", jrBase(), jrNew()),
		cjRuns("runs, no limit, not comparable", jrBase(), jrNew().with(cjOtherTarget)),
		cjRuns("runs, p99 limit, not comparable", jrBase(), jrNew().with(cjOtherTarget), "-max-p99-rise", "20%"),
		cjRuns("runs, errors limit, not comparable", jrBase(), jrNew().with(cjOtherTarget), "-max-errors-rise", "1%"),
		cjRuns("runs, a method only on one side", jrBase(), jrNew().with(func(r *jrRun) {
			r.methods = append(r.methods, jrCall("/pkg.Svc/Other", jrExact(50), 5))
		}), cjBothLimits...),
		cjRuns("runs, two common methods in another order", jrBase().with(func(r *jrRun) {
			r.methods = inOrder(jrCall("/pkg.Svc/A", jrExact(100), 10), jrCall("/pkg.Svc/B", jrExact(200), 10))
		}),
			jrNew().with(func(r *jrRun) {
				r.methods = inOrder(jrCall("/pkg.Svc/B", jrExact(210), 12), jrCall("/pkg.Svc/A", jrExact(300), 20))
			}), cjBothLimits...),
		cjSearches("searches, within the limit", brokeAt(1000), brokeAt(950), cjCapLimit...),
		cjSearches("searches, past the limit", brokeAt(1000), brokeAt(800), cjCapLimit...),
		cjSearches("searches, undecided", brokeAtFirst(), brokeAt(1000), cjCapLimit...),
		cjSearches("searches, a lower bound passes", brokeAt(1000), held(950), cjCapLimit...),
		cjSearches("searches, a lower bound fails", held(1000), brokeAt(800), cjCapLimit...),
		cjSearches("searches, no limit", brokeAt(1000), brokeAt(500)),
		cjSearches("searches, no limit, not comparable", brokeAt(1000), other),
		cjSearches("searches, limit, not comparable", brokeAt(1000), other, cjCapLimit...),
	}
}

// cjWant is what one check says of a reason case: verdict, reason and why,
// "" for null.
type cjWant struct{ verdict, reason, why string }

type cjReasonCase struct {
	pair cjPair
	// want is by check key; a plain run has one method, so one p99 and one
	// errors check.
	want map[string]cjWant
}

var cjSame = cjWant{verdict: "pass"}

func cjAllUnknown(reason, why string) map[string]cjWant {
	w := cjWant{verdict: "unknown", reason: reason, why: why}

	return map[string]cjWant{"p99": w, "errors": w}
}

func cjReasonCases(t *testing.T) []cjReasonCase {
	t.Helper()

	method := func(f func(*jrMethod)) func(*jrRun) { return func(r *jrRun) { f(&r.methods[0]) } }
	run := func(name string, base, nw jrRun, want map[string]cjWant) cjReasonCase {
		return cjReasonCase{pair: cjRuns(name, base, nw, cjBothLimits...), want: want}
	}
	onlyP99 := func(reason, why string) map[string]cjWant {
		return map[string]cjWant{"p99": {verdict: "unknown", reason: reason, why: why}, "errors": cjSame}
	}
	capacity := func(name string, base, nw reportSpec, reason, why string) cjReasonCase {
		return cjReasonCase{
			pair: cjSearches(name, base, nw, cjCapLimit...),
			want: map[string]cjWant{"capacity": {verdict: "unknown", reason: reason, why: why}},
		}
	}

	incomplete := jrBase().with(func(r *jrRun) { r.outcome = cli.OutcomeIncomplete })
	invalid := jrNew().with(func(r *jrRun) {
		r.outcome = cli.OutcomeInvalid
		r.edit = append(r.edit, func(rep *cli.JSONReport) { rep.InvalidReasons = []string{"in_flight_cap"} })
	})
	rejected := jrNew().with(func(r *jrRun) { r.methods[0] = jrMethod{name: "/" + cmpMethod, sent: 40, failed: 40, rejected: 40} })
	other := brokeAt(1000)
	other.target = "10.0.0.2:50051"

	// A search whose held rate reads 0 is not one leettest writes (rate(0) is
	// null); a hand-made report is how the base-zero reason is reached.
	zero := cjPair{
		name: "searches, a base capacity of 0", flags: cjCapLimit,
		write: func(t *testing.T, dir string) (string, string) {
			t.Helper()

			base := brokeAt(1000).write(t, dir, "base.json")
			cjPatch(t, base, func(m map[string]any) { m["breakpoint"].(map[string]any)["held_rps"] = 0 })

			return base, brokeAt(800).write(t, dir, "new.json")
		},
	}

	return []cjReasonCase{
		run("the reports differ", jrBase(), jrNew().with(cjOtherTarget), cjAllUnknown("not_comparable", "reports are not comparable")),
		run("a method that measured nothing", jrBase(), rejected, cjAllUnknown("method_invalid", "new method is invalid (request_error)")),
		run("an invalid run", jrBase(), invalid, cjAllUnknown("invalid", "new run is invalid")),
		run("an incomplete run", incomplete, jrNew(), cjAllUnknown("incomplete", "base run is incomplete")),
		run("held back by the generator", jrBase(), jrNew().with(jrTail("generator")),
			cjAllUnknown("generator", "new: the generator held calls back")),
		run("held back by the stream limit", jrBase().with(jrTail("stream")), jrNew(),
			cjAllUnknown("stream", "base: the stream limit held calls back")),
		run("too few observations", jrBase(), jrNew().with(method(func(m *jrMethod) { m.p99 = metrics.Quantile{} })),
			onlyP99("few_observations", "too few observations for p99")),
		run("a base p99 of 0", jrBase().with(method(func(m *jrMethod) { m.p99 = jrExact(0) })), jrNew(),
			onlyP99("base_zero", "base p99 is 0 us")),
		run("a new p99 that is a lower bound", jrBase(), jrNew().with(method(func(m *jrMethod) { m.p99 = jrLower(100) })),
			onlyP99("censored", "new p99 only a lower bound (timeouts)")),
		run("a base p99 that is a lower bound", jrBase().with(method(func(m *jrMethod) { m.p99 = jrLower(100) })),
			jrNew().with(method(func(m *jrMethod) { m.p99 = jrExact(200) })),
			onlyP99("censored", "base p99 only a lower bound (timeouts)")),
		run("both p99 lower bounds", jrBase().with(method(func(m *jrMethod) { m.p99 = jrLower(100) })),
			jrNew().with(method(func(m *jrMethod) { m.p99 = jrLower(100) })),
			onlyP99("censored", "both p99 only a lower bound (timeouts)")),
		run("nothing sent by the base", jrBase().with(method(func(m *jrMethod) { *m = jrMethod{name: m.name} })), jrNew(),
			map[string]cjWant{
				"p99":    {verdict: "unknown", reason: "few_observations", why: "too few observations for p99"},
				"errors": {verdict: "unknown", reason: "nothing_sent", why: "base: nothing sent"},
			}),
		run("nothing sent by the new", jrBase(), jrNew().with(method(func(m *jrMethod) { *m = jrMethod{name: m.name} })),
			map[string]cjWant{
				"p99":    {verdict: "unknown", reason: "few_observations", why: "too few observations for p99"},
				"errors": {verdict: "unknown", reason: "nothing_sent", why: "new: nothing sent"},
			}),
		run("client-side errors decide it", jrBase(), jrNew().with(method(func(m *jrMethod) { m.failed, m.clientError = 30, 30 })),
			map[string]cjWant{"p99": cjSame, "errors": {verdict: "unknown", reason: "client_errors", why: "client-side errors decide it"}}),
		run("a pass and a fail, no reason", jrBase(), jrNew().with(method(func(m *jrMethod) { m.p99 = jrExact(130) })),
			map[string]cjWant{"p99": {verdict: "fail"}, "errors": cjSame}),

		capacity("searches differ", brokeAt(1000), other, "not_comparable", "reports are not comparable"),
		capacity("base not measured", endedAt(breakpoint.Invalid, 244), brokeAt(800), "not_measured", "base not measured (invalid)"),
		capacity("new not measured", brokeAt(1000), endedAt(breakpoint.Invalid, 244), "not_measured", "new not measured (invalid)"),
		capacity("a run limit without a held rate", brokeAt(1000), endedAt(breakpoint.RunLimit, 0),
			"not_measured", "new not measured (run_limit)"),
		capacity("base broke at the first step", brokeAtFirst(), brokeAt(800),
			"broke_at_first", "base broke at the first step: lower the plan to see a drop"),
		capacity("new broke at the first step", brokeAt(1000), brokeAtFirstOf(950),
			"broke_at_first", "new broke at the first step: lower the plan to see the drop"),
		capacity("new a lower bound", brokeAt(1000), endedAt(breakpoint.HeldThroughout, 800),
			"lower_bound", "new run ended before its capacity (held_all)"),
		capacity("base a lower bound", endedAt(breakpoint.HeldThroughout, 1000), brokeAt(950),
			"lower_bound", "base capacity is only a lower bound (held_all)"),
		capacity("both a lower bound", endedAt(breakpoint.HeldThroughout, 1000), endedAt(breakpoint.HeldThroughout, 900),
			"lower_bound", "both capacities are lower bounds"),
		{pair: zero, want: map[string]cjWant{"capacity": {verdict: "unknown", reason: "base_zero", why: "base capacity is 0 rps"}}},
		{pair: cjSearches("searches, within the limit", brokeAt(1000), brokeAt(950), cjCapLimit...), want: map[string]cjWant{"capacity": cjSame}},
		{pair: cjSearches("searches, past the limit", brokeAt(1000), brokeAt(800), cjCapLimit...), want: map[string]cjWant{"capacity": {verdict: "fail"}}},
	}
}

func cjAllPairs(t *testing.T) []cjPair {
	t.Helper()

	pairs := cjFixedPairs()
	for _, c := range cjReasonCases(t) {
		pairs = append(pairs, c.pair)
	}

	return pairs
}

// The check lines and the differences of the text report, which the JSON says
// again.

var cjCheckLine = regexp.MustCompile(`^(\S+): (p99|errors) `)

// cjTextCheck: "" for a part the line does not have.
type cjTextCheck struct{ method, key, verdict, why, limit string }

func cjTextChecks(out string) []cjTextCheck {
	var checks []cjTextCheck

	for line := range strings.Lines(out) {
		line = strings.TrimSuffix(line, "\n")

		var c cjTextCheck

		switch m := cjCheckLine.FindStringSubmatch(line); {
		case strings.HasPrefix(line, "capacity: "):
			c.key = "capacity"
		case m != nil:
			c.method, c.key = m[1], m[2]
		default:
			continue
		}

		if _, rest, ok := strings.Cut(line, ", limit "); ok {
			limit, tail, _ := strings.Cut(rest, ": ")
			c.limit = limit

			switch {
			case tail == "pass":
				c.verdict = "pass"
			case tail == "FAIL":
				c.verdict = "fail"
			case strings.HasPrefix(tail, "unknown, "):
				c.verdict, c.why = "unknown", strings.TrimPrefix(tail, "unknown, ")
			default:
				c.verdict = "unparsed: " + tail
			}
		}

		checks = append(checks, c)
	}

	return checks
}

// cjWorst is the top verdict the checks make: "" when no limit was given.
func cjWorst(checks []cjTextCheck, judged bool) string {
	worst := ""

	for _, c := range checks {
		switch c.verdict {
		case "fail":
			return "fail"
		case "unknown":
			worst = "unknown"
		case "pass":
			if worst == "" {
				worst = "pass"
			}
		}
	}

	if worst == "" && judged {
		return "unknown"
	}

	return worst
}

const (
	cjConfigNote = " (config bytes differ: a comment or line endings count too)"
	cjNoPassport = ": no passport (report from leettest before v0.3)"
)

// cjTextDiffs are the differences the text names, in its order. A missing
// passport is one entry of the field "passport", "none" on the side that lacks
// it and "present" on the other.
func cjTextDiffs(out, basePath, newPath string) []cjDiff {
	diffs := []cjDiff{}
	at, missing := -1, map[string]bool{}

	for line := range strings.Lines(out) {
		line = strings.TrimSuffix(line, "\n")

		switch {
		case strings.HasPrefix(line, "differs: "):
			field, vals, _ := strings.Cut(strings.TrimPrefix(line, "differs: "), ": ")
			b, n, _ := strings.Cut(strings.TrimSuffix(vals, cjConfigNote), " -> ")
			diffs = append(diffs, cjDiff{Field: field, Base: b, New: n})
		case strings.HasSuffix(line, cjNoPassport):
			if at < 0 {
				diffs, at = append(diffs, cjDiff{Field: "passport"}), len(diffs)
			}

			missing[strings.TrimSuffix(line, cjNoPassport)] = true
		}
	}

	if at >= 0 {
		diffs[at].Base, diffs[at].New = "present", "present"

		if missing[basePath] {
			diffs[at].Base = "none"
		}

		if missing[newPath] {
			diffs[at].New = "none"
		}
	}

	return diffs
}

// cjShape: what holds of every check, whatever it is.
func cjShape(t *testing.T, name string, c *cjCheck) {
	t.Helper()

	if (c.Verdict == nil) != (c.LimitPercent == nil) {
		t.Errorf("%s %s: verdict %v with limit_percent %v: a check has a verdict exactly when it has a limit", name, c.Key, c.Verdict, c.LimitPercent)
	}

	if c.Verdict != nil && !slices.Contains([]string{"pass", "fail", "unknown"}, *c.Verdict) {
		t.Errorf("%s %s: verdict %q is not pass, fail or unknown", name, c.Key, *c.Verdict)
	}

	if unknown := c.Verdict != nil && *c.Verdict == "unknown"; unknown != (c.Reason != nil) || unknown != (c.Why != nil) {
		t.Errorf("%s %s: verdict %v with reason %v and why %v: both are there exactly when the verdict is unknown",
			name, c.Key, c.Verdict, c.Reason, c.Why)
	}

	if c.Reason != nil && !slices.Contains(cjReasons, *c.Reason) {
		t.Errorf("%s %s: reason %q is not in the closed list", name, c.Key, *c.Reason)
	}
}

// Ground: contract — stdout of `-output json` is one object and a newline,
// nothing else, and the exit code, the error and stderr are those of text
// mode for the same pair.
func TestCompareJSON_StdoutIsOnlyTheObject(t *testing.T) {
	none, fail, unknown := "", "fail", "unknown"
	pass := "pass"

	slow := jrNew().with(func(r *jrRun) {
		r.methods[0].p99 = jrExact(130)
		r.methods[0].failed, r.methods[0].clientError = 30, 30
	})
	rise := jrNew().with(func(r *jrRun) { r.methods[0].p99 = jrExact(110) })
	censored := jrNew().with(func(r *jrRun) { r.methods[0].p99 = jrLower(100) })
	other := brokeAt(1000)
	other.target = "10.0.0.2:50051"

	for _, tc := range []struct {
		pair       cjPair
		compared   string
		exit       int
		comparable bool
		verdict    *string
	}{
		{cjRuns("all checks pass", jrBase(), rise, cjBothLimits...), "run", 0, true, &pass},
		{cjRuns("a check undecided", jrBase(), censored, "-max-p99-rise", "20%"), "run", 2, true, &unknown},
		{cjRuns("no limit given", jrBase(), jrNew()), "run", 0, true, nil},
		{cjRuns("not comparable, no limit", jrBase(), jrNew().with(cjOtherTarget)), "run", 2, false, nil},
		{cjRuns("not comparable, a limit", jrBase(), jrNew().with(cjOtherTarget), "-max-p99-rise", "20%"), "run", 2, false, &unknown},
		{cjRuns("a fail beside an unknown", jrBase(), slow, cjBothLimits...), "run", 4, true, &fail},
		{cjSearches("a drop past the limit", brokeAt(1000), brokeAt(800), cjCapLimit...), "breakpoint", 4, true, &fail},
		{cjSearches("a drop within the limit", brokeAt(1000), brokeAt(950), cjCapLimit...), "breakpoint", 0, true, &pass},
		{cjSearches("a capacity undecided", brokeAtFirst(), brokeAt(1000), cjCapLimit...), "breakpoint", 2, true, &unknown},
		{cjSearches("no limit given", brokeAt(1000), brokeAt(500)), "breakpoint", 0, true, nil},
		{cjSearches("not comparable", brokeAt(1000), other), "breakpoint", 2, false, nil},
	} {
		got := tc.pair.run(t)

		for what, res := range map[string]result{"text": got.text, "json": got.js} {
			if code := exitCode(res.err); code != tc.exit {
				t.Errorf("%s, %s mode: exit code %d (%v), want %d", tc.pair.name, what, code, res.err, tc.exit)
			}
		}

		if a, b := cjText(got.text), cjText(got.js); a != b {
			t.Errorf("%s: error %q in json mode, %q in text mode", tc.pair.name, b, a)
		}

		if got.js.stderr != got.text.stderr {
			t.Errorf("%s: stderr %q in json mode, %q in text mode", tc.pair.name, got.js.stderr, got.text.stderr)
		}

		rep := cjDecode(t, got.js.stdout)

		if rep.SchemaVersion != cli.JSONSchemaVersion || rep.Mode != "compare" || rep.ComparedMode != tc.compared {
			t.Errorf("%s: schema_version %d, mode %q, compared_mode %q, want %d, \"compare\", %q",
				tc.pair.name, rep.SchemaVersion, rep.Mode, rep.ComparedMode, cli.JSONSchemaVersion, tc.compared)
		}

		if rep.Comparable != tc.comparable {
			t.Errorf("%s: comparable = %v, want %v", tc.pair.name, rep.Comparable, tc.comparable)
		}

		want := none
		if tc.verdict != nil {
			want = *tc.verdict
		}

		cjWantNullable(t, tc.pair.name+": verdict", rep.Verdict, want)
	}
}

// Ground: contract — no comparison made, nothing on stdout in json mode,
// whatever the input error: the message is the one of text mode.
func TestCompareJSON_InputErrorsLeaveStdoutEmpty(t *testing.T) {
	dir := t.TempDir()
	search := brokeAt(1000).write(t, dir, "search.json")
	otherSearch := brokeAt(900).write(t, dir, "other.json")
	plain := jrBase().write(t, dir, "plain.json")
	otherPlain := jrNew().write(t, dir, "plain2.json")
	missing := filepath.Join(dir, "missing.json")

	notJSON := filepath.Join(dir, "notjson.json")
	if err := os.WriteFile(notJSON, []byte("this is not a report\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"a missing file", []string{search, missing}},
		{"a file that is not JSON", []string{notJSON, search}},
		{"a search and a plain run", []string{search, plain}},
		{"a plain run and a search", []string{plain, search}},
		{"the capacity limit with plain runs", []string{"-max-capacity-drop", "10%", plain, otherPlain}},
		{"the p99 limit with searches", []string{"-max-p99-rise", "20%", search, otherSearch}},
		{"the errors limit with searches", []string{"-max-errors-rise", "1%", search, otherSearch}},
		{"a percent without a sign", []string{"-max-capacity-drop", "10", search, otherSearch}},
		{"one file", []string{search}},
	} {
		text := runCLI(t.Context(), t, 10*time.Second, append([]string{"compare"}, tc.args...)...)
		js := runCLI(t.Context(), t, 10*time.Second, append([]string{"compare", "-output", "json"}, tc.args...)...)

		if code := exitCode(js.err); code != 1 {
			t.Errorf("%s: exit code %d (%v), want 1", tc.name, code, js.err)
		}

		if js.stdout != "" {
			t.Errorf("%s: stdout %q, want nothing", tc.name, js.stdout)
		}

		if a, b := cjText(text), cjText(js); a != b {
			t.Errorf("%s: error %q in json mode, %q in text mode", tc.name, b, a)
		}
	}
}

// Ground: contract — any other value of -output is refused before a file is
// read, in the words of the run's flag with compare's prefix; text is the
// default spelled out.
func TestCompareJSON_BadOutputValueExit1(t *testing.T) {
	dir := t.TempDir()
	base, nw := brokeAt(1000).write(t, dir, "base.json"), brokeAt(950).write(t, dir, "new.json")
	missing := filepath.Join(dir, "missing.json")

	for _, value := range []string{"yaml", "JSON", "Text", "", "json "} {
		for _, files := range [][]string{{base, nw}, {missing, missing}} {
			res := runCLI(t.Context(), t, 10*time.Second, append([]string{"compare", "-output", value}, files...)...)

			if code := exitCode(res.err); code != 1 {
				t.Errorf("-output %q: exit code %d (%v), want 1", value, code, res.err)
			}

			if want := "compare: -output " + quote(value) + ": want text or json"; cjText(res) != want {
				t.Errorf("-output %q: error %q, want %q", value, cjText(res), want)
			}

			if res.stdout != "" {
				t.Errorf("-output %q: stdout %q, want nothing", value, res.stdout)
			}
		}
	}

	plain := runCLI(t.Context(), t, 10*time.Second, "compare", base, nw)
	spelled := runCLI(t.Context(), t, 10*time.Second, "compare", "-output", "text", base, nw)

	if spelled.stdout != plain.stdout || spelled.stdout == "" || cjText(spelled) != cjText(plain) {
		t.Errorf("-output text: stdout %q error %q, want those of the default: %q %q", spelled.stdout, cjText(spelled), plain.stdout, cjText(plain))
	}
}

func quote(s string) string {
	raw, _ := json.Marshal(s)

	return string(raw)
}

// Ground: contract — the JSON says what the text says: each check's verdict,
// the words after `unknown, `, the limit as written, the checks in the text's
// order, the worst verdict on top, the differences in order, and whether the
// reports are comparable.
func TestCompareJSON_VerdictsMatchText(t *testing.T) {
	for _, p := range cjAllPairs(t) {
		got := p.run(t)
		rep := cjDecode(t, got.js.stdout)
		texts := cjTextChecks(got.text.stdout)

		if len(rep.Checks) != len(texts) {
			t.Errorf("%s: %d checks, the text has %d", p.name, len(rep.Checks), len(texts))

			continue
		}

		for i := range rep.Checks {
			c, want := &rep.Checks[i], texts[i]
			what := p.name + ", check " + want.method + " " + want.key

			cjShape(t, p.name, c)

			if c.Key != want.key || strings.TrimPrefix(c.Method, "/") != strings.TrimPrefix(want.method, "/") && want.method != "" {
				t.Errorf("%s: JSON check %s %s, the text has %s %s", p.name, c.Method, c.Key, want.method, want.key)
			}

			cjWantNullable(t, what+": verdict", c.Verdict, want.verdict)
			cjWantNullable(t, what+": why", c.Why, want.why)
			cjWantNullable(t, what+": limit_percent", c.LimitPercent, want.limit)
		}

		cjWantBaseOrder(t, p.name, got, rep)

		cjWantNullable(t, p.name+": top verdict", rep.Verdict, cjWorst(texts, p.judged()))

		if wantDiffs := cjTextDiffs(got.text.stdout, got.basePath, got.newPath); !slices.Equal(rep.Differences, wantDiffs) {
			t.Errorf("%s: differences %v, the text has %v", p.name, rep.Differences, wantDiffs)
		}

		if want := cjText(got.text) != cjNotComparable; rep.Comparable != want {
			t.Errorf("%s: comparable = %v, want %v", p.name, rep.Comparable, want)
		}

		if exitCode(got.js.err) != exitCode(got.text.err) {
			t.Errorf("%s: exit code %d in json mode, %d in text mode", p.name, exitCode(got.js.err), exitCode(got.text.err))
		}
	}
}

// cjWantBaseOrder: the methods of a run comparison come in the order of the
// base report, whatever order the new one has them in. The text cannot say it:
// it is built from the same checks, so it follows any order the JSON has.
func cjWantBaseOrder(t *testing.T, name string, got cjRan, rep cjReport) {
	t.Helper()

	if rep.ComparedMode != "run" {
		return
	}

	baseFile, err := cli.ReadReport(got.basePath)
	if err != nil {
		t.Fatalf("%s: read the base: %v", name, err)
	}

	newFile, err := cli.ReadReport(got.newPath)
	if err != nil {
		t.Fatalf("%s: read the new: %v", name, err)
	}

	var want []string

	for i := range baseFile.Run.Methods {
		for j := range newFile.Run.Methods {
			if newFile.Run.Methods[j].Method == baseFile.Run.Methods[i].Method {
				want = append(want, baseFile.Run.Methods[i].Method)
			}
		}
	}

	var have []string

	for i := range rep.Checks {
		if rep.Checks[i].Key == "p99" {
			have = append(have, rep.Checks[i].Method)
		}
	}

	if !slices.Equal(have, want) {
		t.Errorf("%s: methods in the order %v, the base report has the common ones as %v", name, have, want)
	}
}

// Ground: contract — a check that cannot be decided has one reason of the
// closed list and its words; every reason of the list is reached by a pair;
// and a check that passed or failed has neither (the inverse, also held for
// every check by cjShape).
func TestCompareJSON_ReasonForEachUnknown(t *testing.T) {
	reached := map[string]bool{}

	for _, tc := range cjReasonCases(t) {
		got := tc.pair.run(t)
		rep := cjDecode(t, got.js.stdout)
		texts := cjTextChecks(got.text.stdout)

		if len(rep.Checks) != len(tc.want) || len(texts) != len(tc.want) {
			t.Errorf("%s: %d checks, the text has %d, want %d", tc.pair.name, len(rep.Checks), len(texts), len(tc.want))

			continue
		}

		for i := range rep.Checks {
			c := &rep.Checks[i]
			want, ok := tc.want[c.Key]
			what := tc.pair.name + " " + c.Key

			if !ok {
				t.Errorf("%s: a check %s nobody expected", tc.pair.name, c.Key)

				continue
			}

			cjShape(t, tc.pair.name, c)
			cjWantNullable(t, what+": verdict", c.Verdict, want.verdict)
			cjWantNullable(t, what+": reason", c.Reason, want.reason)
			cjWantNullable(t, what+": why", c.Why, want.why)

			// The pair is what it claims to be: the text says the same words.
			if want.verdict == "unknown" && texts[i].why != want.why {
				t.Errorf("%s: the text says %q, the fixture expects %q", what, texts[i].why, want.why)
			}

			if c.Reason != nil {
				reached[*c.Reason] = true
			}
		}
	}

	for _, r := range cjReasons {
		if !reached[r] {
			t.Errorf("no pair reaches the reason %q", r)
		}
	}
}

// Ground: contract — the numbers are the reports' own, copied and never
// recomputed: the p99 object as the report has it (a lower bound too, null
// when the report has none), the parts of the failed share, the capacity of
// each outcome with its bound, the method named as its report names it; a
// check carries nothing of another key's kind.
func TestCompareJSON_ValuesAreTheReportsOwn(t *testing.T) {
	// Do: failed 40 of 1000 with 5 rejected, 7 client errors and 3 bad
	// responses among them, and 11 calls lost to the connection. Other has no
	// p99 and sent nothing.
	do := jrMethod{
		name: "/" + cmpMethod, sent: 1000, failed: 40, rejected: 5, clientError: 7, badResponse: 3,
		notSent: 11, notSentConnection: 11, p99: metrics.Quantile{Value: 123456789 * time.Nanosecond, Exact: true, Defined: true},
	}
	other := jrMethod{name: "/pkg.Svc/Other"}
	// Second is Do the other way round: the base p99 a lower bound, the new
	// one exact, so a lower_bound forced to false on the base or to true on
	// the new side is seen on a side Do does not cover.
	second := jrMethod{name: "/pkg.Svc/Second", sent: 1000, failed: 10, p99: jrLower(500)}
	base := jrBase().with(func(r *jrRun) { r.methods = []jrMethod{do, other, second} })
	nw := jrNew().with(func(r *jrRun) {
		do.p99, do.failed, do.rejected, do.clientError, do.badResponse = metrics.Quantile{Value: 987654321 * time.Nanosecond, Defined: true}, 80, 0, 2, 0
		do.notSent, do.notSentConnection = 0, 0
		second.p99, second.failed = jrExact(250), 20
		r.methods = []jrMethod{do, other, second}
	})

	got := cjRuns("values", base, nw).run(t)
	rep := cjDecode(t, got.js.stdout)

	if rep.ComparedMode != "run" || len(rep.Checks) != 6 {
		t.Fatalf("compared_mode %q with %d checks, want run with 6 (p99 and errors of three methods)\n%s", rep.ComparedMode, len(rep.Checks), got.js.stdout)
	}

	baseFile, err := cli.ReadReport(got.basePath)
	if err != nil {
		t.Fatalf("read the base: %v", err)
	}

	newFile, err := cli.ReadReport(got.newPath)
	if err != nil {
		t.Fatalf("read the new: %v", err)
	}

	fromReport := func(rep cli.ComparedReport, i int) *cjP99 {
		q := rep.Run.Methods[i].Latency.P99
		if q == nil {
			return nil
		}

		return &cjP99{US: q.US, LowerBound: q.LowerBound}
	}

	// The p99 the report holds, whatever microseconds the nanoseconds became.
	if b := fromReport(baseFile, 0); b == nil || b.LowerBound || b.US < 123000 {
		t.Fatalf("fixture: the base p99 reads %v", b)
	}

	if n := fromReport(newFile, 0); n == nil || !n.LowerBound || n.US < 987000 {
		t.Fatalf("fixture: the new p99 reads %v", n)
	}

	if b := fromReport(baseFile, 2); b == nil || !b.LowerBound || b.US < 500000 {
		t.Fatalf("fixture: the base p99 of Second reads %v, want a lower bound", b)
	}

	if n := fromReport(newFile, 2); n == nil || n.LowerBound || n.US < 250000 {
		t.Fatalf("fixture: the new p99 of Second reads %v, want an exact value", n)
	}

	wantP99 := [][2]*cjP99{
		{fromReport(baseFile, 0), fromReport(newFile, 0)},
		{nil, nil},
		{fromReport(baseFile, 2), fromReport(newFile, 2)},
	}
	wantErrors := [][2]*cjErrors{
		{{Bad: 51, BadClient: 15, Total: 1011}, {Bad: 80, BadClient: 2, Total: 1000}},
		{{}, {}},
		{{Bad: 10, Total: 1000}, {Bad: 20, Total: 1000}},
	}
	methods := []string{"/" + cmpMethod, "/pkg.Svc/Other", "/pkg.Svc/Second"}

	for i, c := range rep.Checks {
		m, kind := i/2, []string{"p99", "errors"}[i%2]
		what := methods[m] + " " + kind

		if c.Method != methods[m] || c.Key != kind {
			t.Errorf("check %d is %s %s, want %s %s: the method as its report names it, p99 then errors", i, c.Method, c.Key, methods[m], kind)
		}

		if c.BaseCapacity != nil || c.NewCapacity != nil || c.BaseOutcome != nil || c.NewOutcome != nil {
			t.Errorf("%s: capacity fields are not null", what)
		}

		if kind == "p99" {
			if c.BaseErrors != nil || c.NewErrors != nil {
				t.Errorf("%s: errors fields are not null", what)
			}

			if !cjSamePtr(c.BaseP99, wantP99[m][0]) || !cjSamePtr(c.NewP99, wantP99[m][1]) {
				t.Errorf("%s: base %v new %v, want the reports' %v and %v (null when the report has none)",
					what, cjShow(c.BaseP99), cjShow(c.NewP99), cjShow(wantP99[m][0]), cjShow(wantP99[m][1]))
			}

			continue
		}

		if c.BaseP99 != nil || c.NewP99 != nil {
			t.Errorf("%s: p99 fields are not null", what)
		}

		if !cjSamePtr(c.BaseErrors, wantErrors[m][0]) || !cjSamePtr(c.NewErrors, wantErrors[m][1]) {
			t.Errorf("%s: base %v new %v, want %v and %v", what, cjShow(c.BaseErrors), cjShow(c.NewErrors), cjShow(wantErrors[m][0]), cjShow(wantErrors[m][1]))
		}
	}

	// Capacity: each outcome on each side.
	for _, tc := range []struct {
		name string
		spec reportSpec
		want *cjCapacity
		out  string
	}{
		{"broke", brokeAt(800), &cjCapacity{RPS: 800, Bound: "exact"}, "broke"},
		{"broke at the first step", brokeAtFirstOf(150), &cjCapacity{RPS: 150, Bound: "upper"}, "broke_at_first"},
		{"held all", endedAt(breakpoint.HeldThroughout, 700), &cjCapacity{RPS: 700, Bound: "lower"}, "held_all"},
		{"run limit", endedAt(breakpoint.RunLimit, 650), &cjCapacity{RPS: 650, Bound: "lower"}, "run_limit"},
		{"stopped", endedAt(breakpoint.Stopped, 600), &cjCapacity{RPS: 600, Bound: "lower"}, "stopped"},
		{"invalid", endedAt(breakpoint.Invalid, 244), nil, "invalid"},
		{"a run limit without a held rate", endedAt(breakpoint.RunLimit, 0), nil, "run_limit"},
	} {
		for _, onBase := range []bool{true, false} {
			pair := cjSearches(tc.name, brokeAt(1000), tc.spec)
			if onBase {
				pair = cjSearches(tc.name, tc.spec, brokeAt(1000))
			}

			ran := pair.run(t)
			rep = cjDecode(t, ran.js.stdout)

			if len(rep.Checks) != 1 {
				t.Errorf("%s: %d checks, want 1", tc.name, len(rep.Checks))

				continue
			}

			c := rep.Checks[0]
			steady, varied := c.BaseCapacity, c.NewCapacity
			steadyOut, variedOut := c.BaseOutcome, c.NewOutcome

			if onBase {
				steady, varied, steadyOut, variedOut = varied, steady, variedOut, steadyOut
			}

			if c.Method != cmpMethod || c.Key != "capacity" {
				t.Errorf("%s: check %s %s, want %s capacity: the search's method as its report writes it", tc.name, c.Method, c.Key, cmpMethod)
			}

			if !cjSamePtr(varied, tc.want) {
				t.Errorf("%s, on the base %v: capacity %v, want %v", tc.name, onBase, cjShow(varied), cjShow(tc.want))
			}

			if !cjSamePtr(steady, &cjCapacity{RPS: 1000, Bound: "exact"}) {
				t.Errorf("%s, on the base %v: the other side's capacity %v, want 1000 exact", tc.name, onBase, cjShow(steady))
			}

			cjWantNullable(t, tc.name+": outcome", variedOut, tc.out)
			cjWantNullable(t, tc.name+": the other side's outcome", steadyOut, "broke")

			if c.BaseP99 != nil || c.NewP99 != nil || c.BaseErrors != nil || c.NewErrors != nil {
				t.Errorf("%s: p99 or errors fields are not null on a capacity check", tc.name)
			}
		}
	}
}

func cjSamePtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

func cjShow[T any](p *T) string {
	if p == nil {
		return "null"
	}

	raw, _ := json.Marshal(p)

	return string(raw)
}

// Ground: contract — the differences are the text's, one per `differs:` line
// in its order, with both values as printed (the note after the config hash
// is the text's alone), a report without a passport one entry "passport".
func TestCompareJSON_DifferencesFollowTheText(t *testing.T) {
	quota := 2.0
	sha := func(c string) string { return strings.Repeat(c, 64) }

	for _, tc := range []struct {
		name string
		edit func(*reportSpec)
		want cjDiff
	}{
		{"target", func(s *reportSpec) { s.target = "10.0.0.2:50051" }, cjDiff{"target", "10.0.0.1:50051", "10.0.0.2:50051"}},
		{"method", func(s *reportSpec) { s.method = "pkg.Svc/Other" }, cjDiff{"method", "pkg.Svc/Do", "pkg.Svc/Other"}},
		{"leettest_version", func(s *reportSpec) { s.version = "v0.3.1" }, cjDiff{"leettest_version", "v0.3.0", "v0.3.1"}},
		{"config_sha256", func(s *reportSpec) { s.passport.ConfigSHA256 = sha("b") }, cjDiff{"passport.config_sha256", sha("a"), sha("b")}},
		{"os", func(s *reportSpec) { s.passport.OS = "darwin" }, cjDiff{"passport.os", "linux", "darwin"}},
		{"arch", func(s *reportSpec) { s.passport.Arch = "arm64" }, cjDiff{"passport.arch", "amd64", "arm64"}},
		{"num_cpu", func(s *reportSpec) { s.passport.NumCPU = 4 }, cjDiff{"passport.num_cpu", "8", "4"}},
		{"gomaxprocs", func(s *reportSpec) { s.passport.GOMAXPROCS = 4 }, cjDiff{"passport.gomaxprocs", "8", "4"}},
		{"cpu_quota", func(s *reportSpec) { s.passport.CPUQuota = &quota }, cjDiff{"passport.cpu_quota", "4", "2"}},
		{"cpu_quota to none", func(s *reportSpec) { s.passport.CPUQuota = nil }, cjDiff{"passport.cpu_quota", "4", "none"}},
		{"go_version", func(s *reportSpec) { s.passport.GoVersion = "go1.26.9" }, cjDiff{"passport.go_version", "go1.27.1", "go1.26.9"}},
		{"dataset", func(s *reportSpec) { s.datasetSHA = sha("d") }, cjDiff{"dataset", sha("c"), sha("d")}},
		{"dataset gone", func(s *reportSpec) { s.datasetSHA = "" }, cjDiff{"dataset", sha("c"), "none"}},
	} {
		for _, flags := range [][]string{nil, cjCapLimit} {
			base, nw := brokeAt(1000), brokeAt(500)
			base.datasetSHA, nw.datasetSHA = sha("c"), sha("c")
			tc.edit(&nw)

			ran := cjSearches(tc.name, base, nw, flags...).run(t)
			if got := cjTextDiffs(ran.text.stdout, ran.basePath, ran.newPath); !slices.Equal(got, []cjDiff{tc.want}) {
				t.Errorf("fixture %s %v: the text says %v, the fixture expects %v", tc.name, flags, got, tc.want)
			}

			rep := cjDecode(t, ran.js.stdout)

			if rep.Comparable || !slices.Equal(rep.Differences, []cjDiff{tc.want}) {
				t.Errorf("%s %v: comparable %v differences %v, want false and %v", tc.name, flags, rep.Comparable, rep.Differences, tc.want)
			}
		}
	}

	// Many differences at once: the order of the text, searches and runs.
	many := brokeAt(1000)
	manyNew := brokeAt(500)
	manyNew.target, manyNew.method, manyNew.version = "10.0.0.2:50051", "pkg.Svc/Other", "v0.3.1"
	manyNew.passport.OS, manyNew.passport.GoVersion, manyNew.datasetSHA = "darwin", "go1.26.9", sha("d")

	runNew := jrNew().with(func(r *jrRun) {
		r.target, r.version = "10.0.0.2:50051", "v0.3.1"
		r.passport.OS, r.passport.GoVersion = "darwin", "go1.26.9"
		r.methods[0].datasetSHA = sha("d")
		r.methods = append(r.methods, jrCall("/pkg.Svc/Extra", jrExact(50), 5))
	})

	for _, p := range []cjPair{
		cjSearches("several differences, searches", many, manyNew),
		cjRuns("several differences, runs", jrBase(), runNew),
	} {
		ran := p.run(t)
		rep := cjDecode(t, ran.js.stdout)
		want := cjTextDiffs(ran.text.stdout, ran.basePath, ran.newPath)

		if len(want) < 5 {
			t.Fatalf("%s: the text names %d differences, the fixture wants 5 or more:\n%s", p.name, len(want), ran.text.stdout)
		}

		if !slices.Equal(rep.Differences, want) {
			t.Errorf("%s: differences %v, the text has %v in this order", p.name, rep.Differences, want)
		}
	}

	// A report without a passport: one entry, whichever side lacks it.
	for _, tc := range []struct {
		name          string
		noBase, noNew bool
		want          cjDiff
	}{
		{"the base lacks it", true, false, cjDiff{"passport", "none", "present"}},
		{"the new lacks it", false, true, cjDiff{"passport", "present", "none"}},
		{"both lack it", true, true, cjDiff{"passport", "none", "none"}},
	} {
		base, nw := brokeAt(1000), brokeAt(500)
		if tc.noBase {
			base.passport = nil
		}

		if tc.noNew {
			nw.passport = nil
		}

		for _, p := range []cjPair{
			cjSearches("searches, "+tc.name, base, nw),
			cjRuns("runs, "+tc.name, jrBase().with(func(r *jrRun) {
				if tc.noBase {
					r.passport = nil
				}
			}), jrNew().with(func(r *jrRun) {
				if tc.noNew {
					r.passport = nil
				}
			})),
		} {
			ran := p.run(t)
			rep := cjDecode(t, ran.js.stdout)

			if got := cjTextDiffs(ran.text.stdout, ran.basePath, ran.newPath); !slices.Equal(got, []cjDiff{tc.want}) {
				t.Errorf("fixture %s: the text says %v, the fixture expects %v", p.name, got, tc.want)
			}

			if rep.Comparable || !slices.Equal(rep.Differences, []cjDiff{tc.want}) {
				t.Errorf("%s: comparable %v differences %v, want false and [%v]", p.name, rep.Comparable, rep.Differences, tc.want)
			}
		}
	}
}

// Ground: contract — no limit given is no verdict, not a pass: the top verdict
// and every check's verdict, limit_percent, reason and why are null, while
// the checks stay listed with both sides' values. A limit given for one check
// leaves the other null.
func TestCompareJSON_NoLimitNoVerdict(t *testing.T) {
	other := brokeAt(1000)
	other.target = "10.0.0.2:50051"

	for _, tc := range []struct {
		pair       cjPair
		exit       int
		comparable bool
		checks     int
	}{
		{cjRuns("runs", jrBase(), jrNew()), 0, true, 2},
		{cjRuns("runs, not comparable", jrBase(), jrNew().with(cjOtherTarget)), 2, false, 2},
		{cjSearches("searches", brokeAt(1000), brokeAt(500)), 0, true, 1},
		{cjSearches("searches, not comparable", brokeAt(1000), other), 2, false, 1},
	} {
		ran := tc.pair.run(t)
		rep := cjDecode(t, ran.js.stdout)

		if code := exitCode(ran.js.err); code != tc.exit {
			t.Errorf("%s: exit code %d (%v), want %d", tc.pair.name, code, ran.js.err, tc.exit)
		}

		if rep.Comparable != tc.comparable || len(rep.Checks) != tc.checks {
			t.Errorf("%s: comparable %v with %d checks, want %v with %d", tc.pair.name, rep.Comparable, len(rep.Checks), tc.comparable, tc.checks)
		}

		cjWantNullable(t, tc.pair.name+": top verdict", rep.Verdict, "")

		for _, c := range rep.Checks {
			cjWantNullable(t, tc.pair.name+" "+c.Key+": verdict", c.Verdict, "")
			cjWantNullable(t, tc.pair.name+" "+c.Key+": limit_percent", c.LimitPercent, "")
			cjWantNullable(t, tc.pair.name+" "+c.Key+": reason", c.Reason, "")
			cjWantNullable(t, tc.pair.name+" "+c.Key+": why", c.Why, "")

			values := c.BaseCapacity != nil && c.NewCapacity != nil || c.BaseP99 != nil && c.NewP99 != nil || c.BaseErrors != nil && c.NewErrors != nil
			if !values {
				t.Errorf("%s %s: the check carries no values of both sides: %+v", tc.pair.name, c.Key, c)
			}
		}
	}

	// One limit given: that check has it, the other none, the top is the
	// verdict of the one asked.
	ran := cjRuns("one limit", jrBase(), jrNew(), "-max-p99-rise", "20%").run(t)
	rep := cjDecode(t, ran.js.stdout)

	if len(rep.Checks) != 2 {
		t.Fatalf("%d checks, want p99 and errors", len(rep.Checks))
	}

	cjWantNullable(t, "p99 limit_percent", rep.Checks[0].LimitPercent, "20%")
	cjWantNullable(t, "p99 verdict", rep.Checks[0].Verdict, "pass")
	cjWantNullable(t, "errors limit_percent", rep.Checks[1].LimitPercent, "")
	cjWantNullable(t, "errors verdict", rep.Checks[1].Verdict, "")
	cjWantNullable(t, "top verdict", rep.Verdict, "pass")
}

// Ground: contract — the sides are the reports' own: the path as given on the
// command line, the name (empty stays empty, the text's "-" is not the data),
// the start as the report has it, the version of the report, the labels; a
// report without a passport or labels has `{}`. The top version is that of the
// leettest running compare.
func TestCompareJSON_SidesAreTheReportsOwn(t *testing.T) {
	info, _ := debug.ReadBuildInfo()
	running := versionString(version, info)

	// One label value holds the characters encoding/json escapes by default.
	labels := map[string]string{"region": "a&b<c>", "env": "prod"}

	base, nw := brokeAt(1000), brokeAt(950)
	base.passport.Labels = labels
	nw.name, nw.passport, nw.version = "", nil, "v0.3.1"
	nw.startedAt = time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)

	runBase := jrBase().with(func(r *jrRun) { r.passport.Labels = labels })
	runNew := jrNew().with(func(r *jrRun) { r.passport, r.version = nil, "v0.3.1" })

	for _, p := range []cjPair{cjSearches("searches", base, nw), cjRuns("runs", runBase, runNew)} {
		ran := p.run(t)
		rep := cjDecode(t, ran.js.stdout)

		escaped := func(hex string) bool { return strings.Contains(ran.js.stdout, "\\"+"u"+hex) }

		if !strings.Contains(ran.js.stdout, `"a&b<c>"`) || escaped("0026") || escaped("003c") || escaped("003e") {
			t.Errorf("%s: a label with & < > is not written as is (SetEscapeHTML(false), like -output json of a run)\n%s", p.name, ran.js.stdout)
		}

		if rep.LeetTestVersion != running || running == cmpVersion {
			t.Errorf("%s: leettest_version %q, want the running leettest's %q", p.name, rep.LeetTestVersion, running)
		}

		wantBase := cjSide{
			Path: ran.basePath, Name: "svc", StartedAt: "2026-10-01T10:00:00Z", LeetTestVersion: cmpVersion,
			Labels: labels,
		}
		if rep.Base.Path != wantBase.Path || rep.Base.Name != wantBase.Name || rep.Base.StartedAt != wantBase.StartedAt ||
			rep.Base.LeetTestVersion != wantBase.LeetTestVersion || !maps.Equal(rep.Base.Labels, wantBase.Labels) {
			t.Errorf("%s: base %+v, want %+v", p.name, rep.Base, wantBase)
		}

		wantName := ""
		if p.name == "runs" {
			wantName = "svc"
		}

		if rep.New.Path != ran.newPath || rep.New.Name != wantName || rep.New.StartedAt != "2026-10-02T10:00:00Z" ||
			rep.New.LeetTestVersion != "v0.3.1" || rep.New.Labels == nil || len(rep.New.Labels) != 0 {
			t.Errorf("%s: new %+v, want path %q, name %q, started 2026-10-02T10:00:00Z, version v0.3.1 and labels {}", p.name, rep.New, ran.newPath, wantName)
		}
	}
}

// Ground: boundary — no method in common: not comparable, `differences` and
// `checks` empty lists and not null, exit 2; with a limit the verdict is
// unknown, nothing could be judged.
func TestCompareJSON_NothingInCommon(t *testing.T) {
	empty := func(r *jrRun) { r.methods = nil }
	elsewhere := jrNew().with(func(r *jrRun) { r.methods = []jrMethod{jrCall("/pkg.Svc/Else", jrExact(100), 10)} })

	for _, tc := range []struct {
		pair    cjPair
		diffs   []cjDiff
		verdict string
	}{
		{cjRuns("no methods at all", jrBase().with(empty), jrNew().with(empty)), []cjDiff{}, ""},
		{cjRuns("no methods at all, a limit", jrBase().with(empty), jrNew().with(empty), "-max-p99-rise", "20%"), []cjDiff{}, "unknown"},
		{cjRuns("no methods at all, both limits", jrBase().with(empty), jrNew().with(empty), cjBothLimits...), []cjDiff{}, "unknown"},
		{cjRuns("different methods", jrBase(), elsewhere), []cjDiff{{"methods", "pkg.Svc/Do", "pkg.Svc/Else"}}, ""},
		{cjRuns("different methods, a limit", jrBase(), elsewhere, "-max-errors-rise", "1%"), []cjDiff{{"methods", "pkg.Svc/Do", "pkg.Svc/Else"}}, "unknown"},
	} {
		ran := tc.pair.run(t)
		rep := cjDecode(t, ran.js.stdout)
		raw := decodeOnly(t, ran.js.stdout)

		if code := exitCode(ran.js.err); code != 2 || cjText(ran.js) != cjNotComparable {
			t.Errorf("%s: exit code %d, error %q, want 2 and %q", tc.pair.name, code, cjText(ran.js), cjNotComparable)
		}

		if rep.Comparable {
			t.Errorf("%s: comparable = true", tc.pair.name)
		}

		if got, ok := raw["checks"].([]any); !ok || len(got) != 0 {
			t.Errorf("%s: checks = %v, want []", tc.pair.name, raw["checks"])
		}

		if _, ok := raw["differences"].([]any); !ok || !slices.Equal(rep.Differences, tc.diffs) {
			t.Errorf("%s: differences = %v (a list: %v), want %v", tc.pair.name, raw["differences"], ok, tc.diffs)
		}

		cjWantNullable(t, tc.pair.name+": verdict", rep.Verdict, tc.verdict)
	}
}

// The keys of the schema, as the golden file has them.
func cjSchema(t *testing.T) (kinds map[string]string) {
	t.Helper()

	golden, err := os.ReadFile("../../internal/cli/testdata/schema_compare_v1.txt")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}

	kinds = map[string]string{}

	for line := range strings.Lines(string(golden)) {
		path, kind, _ := strings.Cut(strings.TrimSpace(line), " ")
		kinds[path] = kind
	}

	return kinds
}

// cjValidate: every key of a decoded value is in the schema, null only where
// the schema has `?`, and every key of the schema whose parent is there is
// there; the elements of a list are each checked.
func cjValidate(t *testing.T, what string, v any, objPath string, kinds map[string]string) {
	t.Helper()

	switch x := v.(type) {
	case map[string]any:
		prefix := ""
		if objPath != "" {
			prefix = objPath + "."
		}

		for k, child := range x {
			p := prefix + k

			kind, ok := kinds[p]
			if !ok {
				t.Errorf("%s: output has %q, which the schema does not", what, p)

				continue
			}

			if child == nil && !strings.HasSuffix(kind, "?") {
				t.Errorf("%s: %q is null, which the schema does not allow", what, p)
			}

			if kind != "map" {
				cjValidate(t, what, child, p, kinds)
			}
		}

		for p := range kinds {
			parent := ""
			if i := strings.LastIndex(p, "."); i >= 0 {
				parent = p[:i]
			}

			if _, there := x[strings.TrimPrefix(p, prefix)]; parent == objPath && !there {
				t.Errorf("%s: output lacks %q", what, p)
			}
		}
	case []any:
		for _, e := range x {
			cjValidate(t, what, e, objPath+"[]", kinds)
		}
	}
}

// cjOrder records the keys of every object in the order they are written,
// by the path of the object.
func cjOrder(dec *json.Decoder, path string, seen map[string][]string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}

	switch tok {
	case json.Delim('{'):
		var keys []string

		for dec.More() {
			kt, tokErr := dec.Token()
			if tokErr != nil {
				return tokErr
			}

			key, _ := kt.(string)
			keys = append(keys, key)

			child := key
			if path != "" {
				child = path + "." + key
			}

			if childErr := cjOrder(dec, child, seen); childErr != nil {
				return childErr
			}
		}

		seen[path] = append(seen[path], strings.Join(keys, ","))
	case json.Delim('['):
		for dec.More() {
			if elemErr := cjOrder(dec, path+"[]", seen); elemErr != nil {
				return elemErr
			}
		}
	}

	if _, isDelim := tok.(json.Delim); isDelim {
		_, err = dec.Token()
	}

	return err
}

// Ground: contract — as for a run's report, the keys of the compare report
// are those of schema v1 (golden file), null only on `?` paths, every key
// there under a parent that is, and written in the order of the brief: the
// top level, the sides, each difference and each check.
func TestCompareJSON_KeysAreSchemaV1(t *testing.T) {
	kinds := cjSchema(t)

	limitedRuns := cjRuns("runs, limits", jrBase(), jrNew(), cjBothLimits...)
	other := brokeAt(1000)
	other.target = "10.0.0.2:50051"
	noPassport := jrNew().with(func(r *jrRun) { r.passport = nil })

	pairs := []cjPair{
		limitedRuns,
		cjRuns("runs, one limit, a null p99", jrBase().with(func(r *jrRun) { r.methods[0].p99 = metrics.Quantile{} }), jrNew(), "-max-p99-rise", "20%"),
		cjRuns("runs, no limit, differences", jrBase(), noPassport),
		cjRuns("runs, no common method", jrBase(), jrNew().with(func(r *jrRun) { r.methods = nil }), cjBothLimits...),
		cjSearches("searches, a limit", brokeAt(1000), brokeAt(950), cjCapLimit...),
		cjSearches("searches, one side not measured", brokeAt(1000), endedAt(breakpoint.Invalid, 244), cjCapLimit...),
		cjSearches("searches, no limit, not comparable", brokeAt(1000), other),
	}

	order := map[string]string{
		"":                       "schema_version,mode,compared_mode,leettest_version,base,new,comparable,differences,verdict,checks",
		"base":                   "path,name,started_at,leettest_version,labels",
		"new":                    "path,name,started_at,leettest_version,labels",
		"differences[]":          "field,base,new",
		"checks[]":               "method,key,base_capacity,new_capacity,base_outcome,new_outcome,base_p99,new_p99,base_errors,new_errors,limit_percent,verdict,reason,why",
		"checks[].base_capacity": "rps,bound",
		"checks[].new_capacity":  "rps,bound",
		"checks[].base_p99":      "us,lower_bound",
		"checks[].new_p99":       "us,lower_bound",
		"checks[].base_errors":   "bad,bad_client,total",
		"checks[].new_errors":    "bad,bad_client,total",
	}
	seenPaths := map[string]bool{}

	for _, p := range pairs {
		ran := p.run(t)
		raw := decodeOnly(t, ran.js.stdout)

		cjValidate(t, p.name, raw, "", kinds)

		seen := map[string][]string{}
		if err := cjOrder(json.NewDecoder(strings.NewReader(ran.js.stdout)), "", seen); err != nil {
			t.Fatalf("%s: read the keys: %v", p.name, err)
		}

		for path, orders := range seen {
			want, ok := order[path]
			if !ok {
				continue
			}

			seenPaths[path] = true

			for _, got := range orders {
				if got != want {
					t.Errorf("%s: keys of %q are written %s, want %s", p.name, path, got, want)
				}
			}
		}
	}

	for path := range order {
		if !seenPaths[path] {
			t.Errorf("no pair wrote an object at %q, its key order is not checked", path)
		}
	}
}

// Ground: fixtures — the pairs of the tests above are what they claim to be:
// the text report, which this unit does not touch, says the verdict and the
// words each reason case expects. Green before and after the unit.
func TestCompareJSON_FixturesSayWhatTheyClaim(t *testing.T) {
	for _, tc := range cjReasonCases(t) {
		got := tc.pair.run(t)
		texts := cjTextChecks(got.text.stdout)

		if len(texts) != len(tc.want) {
			t.Errorf("%s: the text has %d checks, the fixture expects %d:\n%s", tc.pair.name, len(texts), len(tc.want), got.text.stdout)

			continue
		}

		for _, c := range texts {
			want, ok := tc.want[c.key]
			if !ok {
				t.Errorf("%s: the text has a check %s the fixture does not expect", tc.pair.name, c.key)

				continue
			}

			if c.verdict != want.verdict || c.why != want.why {
				t.Errorf("%s %s: the text says %q %q, the fixture expects %q %q:\n%s",
					tc.pair.name, c.key, c.verdict, c.why, want.verdict, want.why, got.text.stdout)
			}
		}
	}
}
