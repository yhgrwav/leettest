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

package prometheus

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// goldenView is the view behind testdata/render.golden: two methods, one full
// of distinct numbers (a number in the wrong series is seen at once) and one
// all zero (a zero series is printed, not skipped); labels given out of order
// with a value that needs all three escapes.
func goldenView() View {
	return View{
		Labels:   []Label{{"env", "st\"a\\ge\nx"}, {"commit", "abc1"}},
		Started:  true,
		InFlight: 3,
		Warmup:   true,
		Methods: []Method{
			{
				Name:       "pkg.Svc/Get",
				PlannedRPS: 12.5,
				// success, request_error, overload, failure, timed_out, cut_off,
				// unreachable, client_error, bad_response, aborted, unclassified.
				Calls: [11]int64{7, 1, 2, 3, 4, 5, 6, 8, 9, 10, 11},
				Latency: Histogram{
					Cumulative: [19]int64{0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 5, 5, 6, 6, 6, 6, 6},
					Count:      7,
					SumSeconds: 123.456,
				},
				NotSent:    [3]int64{20, 30, 40},
				WarmupSent: 15,
			},
			{Name: "pkg.Svc/Put"},
		},
	}
}

func renderOf(t *testing.T, v View) string {
	t.Helper()

	var b bytes.Buffer
	if err := Render(&b, v); err != nil {
		t.Fatalf("Render: %v", err)
	}

	return b.String()
}

// samples maps each sample line of an exposition, without its value, to the
// value: `name{labels}` -> `7`.
func samples(text string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.LastIndexByte(line, ' ')
		out[line[:i]] = line[i+1:]
	}

	return out
}

func countPrefix(text, prefix string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}

	return n
}

// Ground: Brief test 1 — the whole text, byte for byte: names, HELP and TYPE
// lines, the order of families, methods and series, label escaping, cumulative
// le buckets, _sum and _count, +Inf equal to the count, user labels sorted
// after the series' own. The HELP wording is part of the contract.
func TestRender_GoldenText(t *testing.T) {
	raw, err := os.ReadFile("testdata/render.golden")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	want := strings.ReplaceAll(string(raw), "\r\n", "\n")

	got := renderOf(t, goldenView())
	if got == want {
		return
	}

	gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(gl), len(wl)) {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			t.Fatalf("the text differs from testdata/render.golden at line %d (got %d lines, want %d):\n got: %q\nwant: %q",
				i+1, len(gl), len(wl), g, w)
		}
	}
}

// Ground: Brief test 1 (Bounds are a contract) — pasted here on purpose: a
// change of the list is a change of this test and a note "incompatible".
// Green on the stub (the constants are given, not stubbed): it holds the line.
func TestBounds_AreTheNineteenContractEdgesInSeconds(t *testing.T) {
	want := []float64{
		0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05,
		0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25, 50, 60,
	}
	if !slices.Equal(Bounds[:], want) {
		t.Errorf("Bounds = %v, want %v", Bounds, want)
	}
}

// Ground: strategy / reference — the 11 contract category names, in the order
// of Method.Calls. Green on the stub: it holds the line.
func TestCategories_AreTheElevenContractNamesInOrder(t *testing.T) {
	want := []string{
		"success", "request_error", "overload", "failure", "timed_out", "cut_off",
		"unreachable", "client_error", "bad_response", "aborted", "unclassified",
	}
	if !slices.Equal(Categories[:], want) {
		t.Errorf("Categories = %v, want %v", Categories, want)
	}
}

// Ground: review verdict V5 — the causes of not_sent_total, in the order of
// Method.NotSent. Green on the stub: it holds the line.
func TestCauses_AreGeneratorConnectionStream(t *testing.T) {
	if want := []string{"generator", "connection", "stream"}; !slices.Equal(Causes[:], want) {
		t.Errorf("Causes = %v, want %v", Causes, want)
	}
}

// Ground: review verdict R1 — a scrape before the first call is 200 with the
// metadata of every series and not a single sample (methods are reserved
// inside Run), even if the view carries counters.
func TestRender_ANotStartedViewIsMetadataOnly(t *testing.T) {
	v := goldenView()
	v.Started = false
	text := renderOf(t, v)

	if got := len(samples(text)); got != 0 || countPrefix(text, "# TYPE ") != 9 || countPrefix(text, "# HELP ") != 9 {
		t.Errorf("not-started view: %d samples, %d TYPE, %d HELP lines, want 0, 9, 9\n%s",
			got, countPrefix(text, "# TYPE "), countPrefix(text, "# HELP "), text)
	}
}

// Ground: edge — a started run with no method named: only the two series that
// belong to the whole run, without braces when there is no label.
func TestRender_AStartedViewWithoutMethodsHasOnlyTheRunWideSamples(t *testing.T) {
	got := samples(renderOf(t, View{Started: true, InFlight: 2}))

	want := map[string]string{"leettest_in_flight": "2", "leettest_warmup": "0"}
	if len(got) != len(want) || got["leettest_in_flight"] != "2" || got["leettest_warmup"] != "0" {
		t.Errorf("samples = %v, want %v", got, want)
	}
}

// Ground: edge — no user labels: the series' own labels alone, in braces.
func TestRender_WithoutUserLabelsASeriesCarriesItsOwnLabelsOnly(t *testing.T) {
	got := samples(renderOf(t, View{Started: true, Methods: []Method{{Name: "m", PlannedRPS: 5}}}))

	if got[`leettest_planned_rps{method="m"}`] != "5" {
		t.Errorf(`no sample leettest_planned_rps{method="m"} 5 in %v`, got)
	}
}

// Ground: strategy — user labels are constant labels on every series; sorted
// by name so a series' text does not depend on map order.
func TestRender_UserLabelsAreSortedByName(t *testing.T) {
	v := View{Started: true, Labels: []Label{{"z", "2"}, {"a", "1"}, {"m", "3"}}}

	if got := samples(renderOf(t, v)); got[`leettest_in_flight{a="1",m="3",z="2"}`] != "0" {
		t.Errorf("no leettest_in_flight{a=\"1\",m=\"3\",z=\"2\"} 0 in %v", got)
	}
}

// Ground: Brief test 1 — label values escape backslash, double quote and
// newline, exactly these three.
func TestRender_LabelValuesEscapeBackslashQuoteAndNewline(t *testing.T) {
	v := View{Started: true, Labels: []Label{{"env", "a\\b\"c\nd\te"}}}

	if got := samples(renderOf(t, v)); got["leettest_in_flight{env=\"a\\\\b\\\"c\\nd\te\"}"] != "0" {
		t.Errorf("no escaped in_flight sample in %v", got)
	}
}

// Ground: review verdict R2.7 — latency _count, +Inf and calls_total{success}
// are the same number in one render, also when values past the last bound
// make the last finite bucket lower.
func TestRender_PlusInfCountAndSuccessCallsAreOneNumber(t *testing.T) {
	m := Method{Name: "m"}
	m.Calls[0] = 7
	m.Latency = Histogram{Count: 7}
	m.Latency.Cumulative[18] = 5 // two observations are above 60 s
	got := samples(renderOf(t, View{Started: true, Methods: []Method{m}}))

	have := []string{
		got[`leettest_latency_seconds_bucket{method="m",le="+Inf"}`],
		got[`leettest_latency_seconds_count{method="m"}`],
		got[`leettest_calls_total{method="m",category="success"}`],
		got[`leettest_latency_seconds_bucket{method="m",le="60"}`],
	}
	if want := []string{"7", "7", "7", "5"}; !slices.Equal(have, want) {
		t.Errorf("+Inf, _count, calls_total{success}, le=60 = %v, want %v", have, want)
	}
}

// Ground: review verdict V4 — censored_total and excluded_total repeat the
// calls_total of their categories and only of those: timed_out, aborted;
// unreachable, cut_off, client_error, unclassified. No other category has a
// series there, and no one is in both.
func TestRender_CensoredAndExcludedRepeatTheirCategoriesOfCallsTotal(t *testing.T) {
	m := Method{Name: "m", Calls: [11]int64{7, 1, 2, 3, 4, 5, 6, 8, 9, 10, 11}}
	got := samples(renderOf(t, View{Started: true, Methods: []Method{m}}))

	censored := map[string]bool{"timed_out": true, "aborted": true}
	excluded := map[string]bool{"unreachable": true, "cut_off": true, "client_error": true, "unclassified": true}

	var bad []string
	for i, c := range Categories {
		calls := got[`leettest_calls_total{method="m",category="`+c+`"}`]
		cs, hasC := got[`leettest_censored_total{method="m",category="`+c+`"}`]
		es, hasE := got[`leettest_excluded_total{method="m",category="`+c+`"}`]
		if calls == "" || calls != strconv.FormatInt(m.Calls[i], 10) {
			bad = append(bad, c+": calls_total "+calls)
		}
		if hasC != censored[c] || (hasC && cs != calls) {
			bad = append(bad, c+": censored_total "+cs)
		}
		if hasE != excluded[c] || (hasE && es != calls) {
			bad = append(bad, c+": excluded_total "+es)
		}
	}
	if len(bad) > 0 {
		t.Errorf("series do not match calls_total: %v", bad)
	}
}

// Ground: review verdict V2, V5 — no `run` label (a new series set per run) and
// no start_lag series (no distribution of it exists). Green on the stub: it
// holds the line for the code.
func TestRender_HasNoRunLabelAndNoStartLagSeries(t *testing.T) {
	text := renderOf(t, goldenView())

	if strings.Contains(text, "run=") || strings.Contains(text, "start_lag") {
		t.Errorf("the text has a run label or a start_lag series:\n%s", text)
	}
}

// Ground: review verdict V6 — the warm-up gauge is 1 inside the warm-up and 0
// after; warm-up sends are a series of their own.
func TestRender_WarmupGaugeFollowsTheView(t *testing.T) {
	on := samples(renderOf(t, View{Started: true, Warmup: true}))
	off := samples(renderOf(t, View{Started: true}))

	if on["leettest_warmup"] != "1" || off["leettest_warmup"] != "0" {
		t.Errorf("leettest_warmup = %q in the warm-up, %q after; want 1 and 0", on["leettest_warmup"], off["leettest_warmup"])
	}
}

// Ground: determinism — the same view renders the same bytes (no map order,
// no clock in the text).
func TestRender_TheSameViewRendersTheSameBytes(t *testing.T) {
	a, b := renderOf(t, goldenView()), renderOf(t, goldenView())

	if a != b || a == "" {
		t.Errorf("two renders of one view differ or are empty (%d and %d bytes)", len(a), len(b))
	}
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

// Ground: edge — a broken connection is an error to the caller, not swallowed.
func TestRender_ReturnsTheWritersError(t *testing.T) {
	boom := errors.New("broken pipe")

	if err := Render(failingWriter{boom}, goldenView()); !errors.Is(err, boom) {
		t.Errorf("Render to a failing writer = %v, want %v", err, boom)
	}
}
