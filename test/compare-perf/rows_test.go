package main

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

// script stands in for the Docker runs: it records the labels in call order
// and answers from held, which says whether a run of a tool holds a rate with
// the cores it was given.
type script struct {
	labels []string
	held   func(tool string, s setup) bool
	failAt string
}

func (sc *script) run(_ context.Context, tool string, s setup, label string) (runOut, error) {
	sc.labels = append(sc.labels, label)
	if sc.failAt != "" && label == sc.failAt {
		return runOut{Tool: tool, Setup: s}, errors.New("docker fell over")
	}

	return runOut{Tool: tool, Setup: s, Spacing: &spacing{}, Verdict: verdict{Held: sc.held(tool, s), Share: 1}}, nil
}

func testHarness(t *testing.T, sc *script, args ...string) *harness {
	t.Helper()

	f, err := parseFlags(args, io.Discard)
	if err != nil {
		t.Fatal(err)
	}

	return &harness{f: f, run: sc.run}
}

// Rate 1000, 1250, 1550 with -rate-max 1600.
var ladderArgs = []string{"-tools", "leettest,ghz", "-runs", "2", "-rate-max", "1600"}

func TestRow1_StepsOutsideRunsInsideAndTheOrderGoesRound(t *testing.T) {
	sc := &script{held: func(string, setup) bool { return true }}
	h := testHarness(t, sc, append(slices.Clone(ladderArgs), "-rate-max", "1300", "-control-cpus", "0")...)

	var summary strings.Builder
	if err := h.row1(context.Background(), &summary, io.Discard); err != nil {
		t.Fatal(err)
	}

	// Two tools, two steps, two runs: a step is finished by every tool before
	// the next begins, and within a step the order shifts with each run and
	// each step.
	want := []string{
		"row1-leettest-c1-1000-1", "row1-ghz-c1-1000-1", "row1-ghz-c1-1000-2", "row1-leettest-c1-1000-2",
		"row1-ghz-c1-1250-1", "row1-leettest-c1-1250-1", "row1-leettest-c1-1250-2", "row1-ghz-c1-1250-2",
	}
	if !slices.Equal(sc.labels, want) {
		t.Errorf("calls:\n%v\nwant\n%v", sc.labels, want)
	}
}

func TestRow1_AToolThatDoesNotHoldAStepDropsOutTheOthersGoOn(t *testing.T) {
	sc := &script{held: func(tool string, s setup) bool { return tool == leettest || s.Rate <= 1000 }}
	h := testHarness(t, sc, append(slices.Clone(ladderArgs), "-control-cpus", "0")...)

	var summary strings.Builder
	if err := h.row1(context.Background(), &summary, io.Discard); err != nil {
		t.Fatal(err)
	}

	ghzAt := func(rate string) int {
		n := 0
		for _, l := range sc.labels {
			if strings.HasPrefix(l, "row1-ghz-c1-"+rate+"-") {
				n++
			}
		}

		return n
	}
	if ghzAt("1000") != 2 || ghzAt("1250") != 2 || ghzAt("1550") != 0 {
		t.Errorf("ghz runs at 1000/1250/1550 = %d/%d/%d, want 2/2/0: it stops after the first step it does not hold",
			ghzAt("1000"), ghzAt("1250"), ghzAt("1550"))
	}
	for _, want := range []string{"ghz: holds 1000 rps, did not hold 1250 rps", "leettest: holds ≥ 1550 rps (the top of the ladder"} {
		if !strings.Contains(summary.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, summary.String())
		}
	}
}

// leettest holds 1250 with one core and breaks at 1550; the control gives it
// more cores.
func controlScript(coresHold int) *script {
	return &script{held: func(tool string, s setup) bool {
		switch {
		case tool != leettest:
			return s.Rate <= 1000
		case s.ToolCPUs > 1:
			return s.Rate <= coresHold
		}

		return s.Rate <= 1250
	}}
}

func TestRow1_AControlThatHoldsNoMoreWithMoreCoresMakesTheRateTheStands(t *testing.T) {
	sc := controlScript(1250)
	h := testHarness(t, sc, ladderArgs...)

	var summary strings.Builder
	if err := h.row1(context.Background(), &summary, io.Discard); err != nil {
		t.Fatal(err)
	}

	var control []string
	for _, l := range sc.labels {
		if strings.Contains(l, "-c4-") {
			control = append(control, l)
		}
	}
	// From the fastest tool's own highest rate, up to the first it does not hold.
	want := []string{"row1-leettest-c4-1250-1", "row1-leettest-c4-1250-2", "row1-leettest-c4-1550-1", "row1-leettest-c4-1550-2"}
	if !slices.Equal(control, want) {
		t.Errorf("control runs = %v, want %v", control, want)
	}
	for _, want := range []string{"leettest: holds ≥ 1250 rps (stand's limit)", "ghz: holds 1000 rps, did not hold 1250 rps", "reaches the stand's limit"} {
		if !strings.Contains(summary.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, summary.String())
		}
	}
}

func TestRow1_AControlThatHoldsMoreWithMoreCoresLeavesTheRateTheTools(t *testing.T) {
	sc := controlScript(1550)
	h := testHarness(t, sc, ladderArgs...)

	var summary strings.Builder
	if err := h.row1(context.Background(), &summary, io.Discard); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(summary.String(), "stand's limit)") || strings.Contains(summary.String(), "reaches the stand's limit") {
		t.Errorf("the stand is called the limit though the control held more:\n%s", summary.String())
	}
	for _, want := range []string{"leettest: holds 1250 rps, did not hold 1550 rps", "more than the 1250 with 1"} {
		if !strings.Contains(summary.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, summary.String())
		}
	}
}

func TestRow1_NoControlWhenOffOrWhenNoToolBroke(t *testing.T) {
	for name, tt := range map[string]struct {
		args []string
		held func(string, setup) bool
	}{
		"off":          {[]string{"-control-cpus", "0"}, controlScript(1250).held},
		"nobody broke": {nil, func(string, setup) bool { return true }},
		"nobody holds": {nil, func(string, setup) bool { return false }},
	} {
		sc := &script{held: tt.held}
		h := testHarness(t, sc, append(slices.Clone(ladderArgs), tt.args...)...)

		var summary strings.Builder
		if err := h.row1(context.Background(), &summary, io.Discard); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, l := range sc.labels {
			if strings.Contains(l, "-c4-") {
				t.Errorf("%s: a control run %s", name, l)
			}
		}
	}
}

func TestRunLabel_TheCoresAreInIt(t *testing.T) {
	a, b := runLabel(1, ghz, 1, 20000, 2), runLabel(1, ghz, 4, 20000, 2)

	if a == b {
		t.Fatalf("one label %q for runs on 1 and 4 cores: the second would write over the first", a)
	}
	if a != "row1-ghz-c1-20000-2" {
		t.Errorf("label = %q", a)
	}
}

func TestRow1_AnErrorLeavesTheTablesOfTheFinishedSteps(t *testing.T) {
	sc := &script{held: func(string, setup) bool { return true }, failAt: "row1-ghz-c1-1250-1"}
	h := testHarness(t, sc, append(slices.Clone(ladderArgs), "-control-cpus", "0")...)

	var summary strings.Builder
	err := h.row1(context.Background(), &summary, io.Discard)

	if err == nil || !strings.Contains(err.Error(), "docker fell over") {
		t.Fatalf("err = %v, want the run's", err)
	}
	for _, want := range []string{"### leettest", "### ghz", "| 1000 | 2 of 2 |", "stopped by an error"} {
		if !strings.Contains(summary.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, summary.String())
		}
	}
	if strings.Contains(summary.String(), "| 1250 |") {
		t.Errorf("the step the error stopped in is in the table:\n%s", summary.String())
	}
}

func TestRow2_AnErrorLeavesTheRunsThatFinished(t *testing.T) {
	sc := &script{held: func(string, setup) bool { return true }, failAt: "row2-ghz-c1-1"}
	h := testHarness(t, sc, "-tools", "leettest,ghz", "-runs", "2")

	var summary strings.Builder
	err := h.row2(context.Background(), &summary, io.Discard)

	if err == nil {
		t.Fatal("want the run's error")
	}
	for _, want := range []string{"| leettest |", "| ghz | no run finished |", "stopped by an error"} {
		if !strings.Contains(summary.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, summary.String())
		}
	}
}

func TestRow1Table_TheStandsLimitIsALowerBoundForEveryToolThatReachedIt(t *testing.T) {
	f, _ := parseFlags([]string{"-tools", "leettest,ghz,k6"}, io.Discard)
	pt := func(rate int, held bool) point {
		return point{rate: rate, runs: []runOut{{Verdict: verdict{Held: held}}}}
	}
	results := map[string][]point{
		leettest: {pt(1000, true), pt(1250, true), pt(1550, false)},
		ghz:      {pt(1000, true), pt(1250, true), pt(1550, false)},
		k6:       {pt(1000, true), pt(1250, false)},
	}
	ctl := &control{Tool: leettest, CPUs: 4, From: 1250, Points: []point{pt(1250, true), pt(1550, false)}}

	var b strings.Builder
	row1Table(&b, f, results, ctl)

	for _, want := range []string{"leettest: holds ≥ 1250 rps (stand's limit)", "ghz: holds ≥ 1250 rps (stand's limit)", "k6: holds 1000 rps, did not hold 1250 rps"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("table lacks %q:\n%s", want, b.String())
		}
	}
}
