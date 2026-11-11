package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const (
	// stressEnv is set to "1" by `make stress` and by `make release-test` (its -race part), nothing else.
	stressEnv = "LEETTEST_STRESS"
	// stressSkipsEnv is the file `make stress` gives the skips to: one line
	// "<test>\t<reasons>" per skip, appended.
	stressSkipsEnv = "LEETTEST_STRESS_SKIPS"
)

// skipper is the part of *testing.T skipIfStarved uses, so a test can hand it
// a recorder.
type skipper interface {
	Helper()
	Name() string
	Logf(format string, args ...any)
	Skipf(format string, args ...any)
}

// skipIfStarved skips the test when a run it needs to be valid ended invalid
// because the generator was starved, and only under `make stress`: the race
// detector on a shared runner takes the CPU the schedule needs, and the run
// says so itself. Call it where a test turns a failed run into Fatalf, with
// the run's error and its JSON report (a test that waits for the invalid run
// does not call it).
//
// The skip needs all of these, anything else leaves the test to fail:
//   - LEETTEST_STRESS is "1";
//   - err is ErrInvalidRun;
//   - jsonDoc is a JSON object whose top-level invalid_reasons is a non-empty
//     array and every reason in it is generator_late or clock_step.
//
// A reason that is a verdict on the target (in_flight_cap, nothing_measured,
// thresholds_undecided), a report without the field (a search carries none at
// the top) and a document that does not parse all fail the test.
func skipIfStarved(t skipper, err error, jsonDoc []byte) {
	t.Helper()

	if os.Getenv(stressEnv) != "1" || !errors.Is(err, ErrInvalidRun) {
		return
	}

	reasons, ok := starvedReasons(jsonDoc)
	if !ok {
		return
	}

	joined := strings.Join(reasons, ",")
	if werr := noteStressSkip(t.Name(), joined); werr != nil {
		// A skip nobody can count is a silent one: the test fails instead.
		t.Logf("stress: not skipping, the skip cannot be recorded: %v", werr)

		return
	}

	t.Skipf("stress: invalid run (%s) on a starved generator; judged by make stress-timing", joined)
}

// starvedReasons are the invalid_reasons of doc, and whether they are only
// the two a starved generator gives.
func starvedReasons(doc []byte) ([]string, bool) {
	var report struct {
		InvalidReasons []string `json:"invalid_reasons"`
	}
	if json.Unmarshal(doc, &report) != nil || len(report.InvalidReasons) == 0 {
		return nil, false
	}

	for _, r := range report.InvalidReasons {
		if r != "generator_late" && r != "clock_step" {
			return nil, false
		}
	}

	return report.InvalidReasons, true
}

// noteStressSkip appends one line to the file named by LEETTEST_STRESS_SKIPS,
// in one write: the packages of `go test ./...` are separate processes that
// append to the same file at once. Without the variable it does nothing.
func noteStressSkip(test, reasons string) error {
	path := os.Getenv(stressSkipsEnv)
	if path == "" {
		return nil
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	if _, err := f.WriteString(test + "\t" + reasons + "\n"); err != nil {
		_ = f.Close()

		return err
	}

	return f.Close()
}
