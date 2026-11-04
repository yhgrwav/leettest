package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skipRecorder stands in for *testing.T: Skipf on a real one ends the goroutine.
type skipRecorder struct {
	name    string
	skipped bool
	message string
	logs    []string
}

func (r *skipRecorder) Helper()      {}
func (r *skipRecorder) Name() string { return r.name }

func (r *skipRecorder) Logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}

func (r *skipRecorder) Skipf(format string, args ...any) {
	r.skipped = true
	r.message = fmt.Sprintf(format, args...)
}

func underStress(t *testing.T) {
	t.Helper()

	t.Setenv(stressEnv, "1")
	t.Setenv(stressSkipsEnv, "")
}

func reportWith(reasons string) []byte {
	return []byte(`{"schema_version":1,"mode":"run","outcome":"invalid","invalid_reasons":` + reasons + `}`)
}

var invalidRun = fmt.Errorf("run: %w", ErrInvalidRun)

// Ground: contract — under make stress, a run that ended invalid for the
// generator's sake (late, or a coarse clock) and nothing else is no verdict on
// the code: the test skips with the reasons named.
func TestSkipIfStarved_SkipsAStarvedGenerator(t *testing.T) {
	for reasons, named := range map[string]string{
		`["generator_late"]`:              "(generator_late)",
		`["clock_step"]`:                  "(clock_step)",
		`["clock_step","generator_late"]`: "(clock_step,generator_late)",
	} {
		underStress(t)
		r := &skipRecorder{name: "TestX"}

		skipIfStarved(r, invalidRun, reportWith(reasons))

		if !r.skipped {
			t.Errorf("invalid_reasons %s: not skipped", reasons)
		}
		for _, want := range []string{"stress: invalid run " + named, "starved generator"} {
			if !strings.Contains(r.message, want) {
				t.Errorf("invalid_reasons %s: skip message %q lacks %q", reasons, r.message, want)
			}
		}
	}
}

// Ground: contract — only an invalid run is excused: a run that failed any
// other way, or that did not fail, is left to the test's own check, even when
// its report says the generator was late.
func TestSkipIfStarved_OnlyAnInvalidRunIsExcused(t *testing.T) {
	for name, err := range map[string]error{
		"no error":          nil,
		"another error":     errors.New("dial: refused"),
		"incomplete":        fmt.Errorf("run: %w", ErrIncomplete),
		"compare undecided": fmt.Errorf("run: %w", ErrCompareUndecided),
	} {
		underStress(t)
		r := &skipRecorder{}

		skipIfStarved(r, err, reportWith(`["generator_late"]`))

		if r.skipped {
			t.Errorf("%s: skipped, want the test to run on and fail", name)
		}
	}
}

// Ground: contract — ordinary CI and make test never skip: the flag is set by
// make stress alone, and only the value 1 counts.
func TestSkipIfStarved_NeedsTheStressFlag(t *testing.T) {
	for _, value := range []string{"", "0", "true", "yes"} {
		t.Setenv(stressEnv, value)
		t.Setenv(stressSkipsEnv, "")
		r := &skipRecorder{}

		skipIfStarved(r, invalidRun, reportWith(`["generator_late"]`))

		if r.skipped {
			t.Errorf("%s=%q: skipped, want the test to run on and fail", stressEnv, value)
		}
	}
}

// Ground: contract — a verdict on the target is not starvation: one reason
// that is not generator_late or clock_step fails the test, whatever stands
// beside it.
func TestSkipIfStarved_EveryReasonMustBeStarvation(t *testing.T) {
	for _, reasons := range []string{
		`["in_flight_cap"]`,
		`["nothing_measured"]`,
		`["thresholds_undecided"]`,
		`["generator_late","in_flight_cap"]`,
		`["nothing_measured","clock_step"]`,
		`["clock_step","generator_late","thresholds_undecided"]`,
		`["generator_late "]`,
		`["GENERATOR_LATE"]`,
	} {
		underStress(t)
		r := &skipRecorder{}

		skipIfStarved(r, invalidRun, reportWith(reasons))

		if r.skipped {
			t.Errorf("invalid_reasons %s: skipped, want the test to fail", reasons)
		}
	}
}

// Ground: contract — the helper fails closed: no document, a document that is
// not a report, a report without the reason, or with an empty list says
// nothing about the generator, and the test fails as it does without the helper.
func TestSkipIfStarved_NoReasonNoSkip(t *testing.T) {
	for name, doc := range map[string][]byte{
		"no document":      nil,
		"empty file":       []byte(""),
		"not JSON":         []byte("the run is invalid"),
		"truncated JSON":   []byte(`{"invalid_reasons":["generator_late"`),
		"an array":         []byte(`["generator_late"]`),
		"no field":         []byte(`{"schema_version":1,"mode":"run","outcome":"invalid"}`),
		"empty list":       reportWith(`[]`),
		"null":             reportWith(`null`),
		"a string":         reportWith(`"generator_late"`),
		"numbers":          reportWith(`[1]`),
		"search report":    []byte(`{"schema_version":1,"mode":"breakpoint","breakpoint":{"outcome":"invalid","runs":[{"report":{"invalid_reasons":["generator_late"]}}]}}`),
		"reason one level": []byte(`{"settings":{"invalid_reasons":["generator_late"]}}`),
	} {
		underStress(t)
		r := &skipRecorder{}

		skipIfStarved(r, invalidRun, doc)

		if r.skipped {
			t.Errorf("%s: skipped, want the test to fail", name)
		}
	}
}

// Ground: contract — the skips are counted, not silent: with the file named,
// every skip appends one line with the test and its reasons; a test that
// fails, and a run without the file named, write nothing.
func TestSkipIfStarved_RecordsEachSkip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stress-skips.txt")
	t.Setenv(stressEnv, "1")
	t.Setenv(stressSkipsEnv, path)

	skipIfStarved(&skipRecorder{name: "TestA"}, invalidRun, reportWith(`["generator_late"]`))
	skipIfStarved(&skipRecorder{name: "TestB/sub"}, invalidRun, reportWith(`["clock_step","generator_late"]`))
	skipIfStarved(&skipRecorder{name: "TestC"}, invalidRun, reportWith(`["in_flight_cap"]`))
	skipIfStarved(&skipRecorder{name: "TestD"}, nil, reportWith(`["generator_late"]`))
	skipIfStarved(&skipRecorder{name: "TestA"}, invalidRun, reportWith(`["generator_late"]`))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the skips file: %v", err)
	}
	want := "TestA\tgenerator_late\nTestB/sub\tclock_step,generator_late\nTestA\tgenerator_late\n"
	if string(data) != want {
		t.Errorf("skips file:\n%q\nwant:\n%q", data, want)
	}
}

// Ground: contract — a skip that cannot be recorded is not made: the test
// fails with the log line saying why, rather than skipping unseen.
func TestSkipIfStarved_DoesNotSkipWhatItCannotRecord(t *testing.T) {
	t.Setenv(stressEnv, "1")
	t.Setenv(stressSkipsEnv, filepath.Join(t.TempDir(), "no-such-dir", "stress-skips.txt"))
	r := &skipRecorder{}

	skipIfStarved(r, invalidRun, reportWith(`["generator_late"]`))

	if r.skipped {
		t.Errorf("skipped, but the skip was not recorded")
	}
	if len(r.logs) != 1 || !strings.Contains(r.logs[0], "cannot be recorded") {
		t.Errorf("logs = %q, want one line saying the skip cannot be recorded", r.logs)
	}
}

// Ground: contract — without the file named the skip still happens and nothing
// is written anywhere.
func TestSkipIfStarved_WithoutAFileSkipsAnyway(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	underStress(t)
	r := &skipRecorder{}

	skipIfStarved(r, invalidRun, reportWith(`["clock_step"]`))

	if !r.skipped {
		t.Errorf("not skipped")
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("files appeared in the working directory: %v, %v", entries, err)
	}
}

// Ground: contract — the helper takes the real *testing.T, so a test calls it
// where it now calls Fatalf.
var _ skipper = (*testing.T)(nil)
