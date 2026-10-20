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
	"encoding/json"
	"io"
	"maps"

	"github.com/yhgrwav/leettest/pkg/compare"
	shr "github.com/yhgrwav/leettest/pkg/share"
)

// ModeCompare is the "mode" of the JSON `leettest compare` writes.
const ModeCompare = "compare"

// CompareReport is `leettest compare -output json`, schema_version 1. The
// comparison builds it once; the text printer reads the same value (the
// unexported fields are what only the text has), so the two cannot say
// different things. A pointer is the only thing that may be null.
type CompareReport struct {
	SchemaVersion int `json:"schema_version"`
	// Mode is always ModeCompare.
	Mode string `json:"mode"`
	// ComparedMode is the mode of the two reports: ModeRun or ModeBreakpoint.
	ComparedMode string `json:"compared_mode"`
	// LeetTestVersion is that of the leettest running compare.
	LeetTestVersion string        `json:"leettest_version"`
	Base            CompareSide   `json:"base"`
	New             CompareSide   `json:"new"`
	Comparable      bool          `json:"comparable"`
	Differences     []CompareDiff `json:"differences"`
	// Verdict is the worst verdict of the checks that had a limit, null when
	// no limit was given.
	Verdict *string        `json:"verdict"`
	Checks  []CompareCheck `json:"checks"`
}

// CompareSide names one of the two reports.
type CompareSide struct {
	// Path is as given on the command line.
	Path            string            `json:"path"`
	Name            string            `json:"name"`
	StartedAt       string            `json:"started_at"`
	LeetTestVersion string            `json:"leettest_version"`
	Labels          map[string]string `json:"labels"`
}

// CompareDiff is one field that makes the reports incomparable.
type CompareDiff struct {
	Field string `json:"field"`
	Base  string `json:"base"`
	New   string `json:"new"`

	// lines are the text lines naming it: one, two when both reports lack a
	// passport.
	lines []string
}

// CompareCapacity is the capacity a search found.
type CompareCapacity struct {
	RPS int `json:"rps"`
	// Bound is "exact", "lower" (at least RPS) or "upper" (below RPS).
	Bound string `json:"bound"`
}

// CompareErrors are the parts of a method's share of failed calls.
type CompareErrors struct {
	Bad       int `json:"bad"`
	BadClient int `json:"bad_client"`
	Total     int `json:"total"`
}

// CompareCheck is one check. The fields of the other keys' kinds are null.
type CompareCheck struct {
	// Method is as the report writes it: with the leading slash in a run
	// report, without in a search report.
	Method       string           `json:"method"`
	Key          string           `json:"key"`
	BaseCapacity *CompareCapacity `json:"base_capacity"`
	NewCapacity  *CompareCapacity `json:"new_capacity"`
	// The outcomes are the searches' `breakpoint.outcome`.
	BaseOutcome *string `json:"base_outcome"`
	NewOutcome  *string `json:"new_outcome"`
	// The p99 are the run reports' own `latency.p99`.
	BaseP99      *jsonQuantile  `json:"base_p99"`
	NewP99       *jsonQuantile  `json:"new_p99"`
	BaseErrors   *CompareErrors `json:"base_errors"`
	NewErrors    *CompareErrors `json:"new_errors"`
	LimitPercent *string        `json:"limit_percent"`
	// Verdict is "pass", "fail" or "unknown"; null without a limit.
	Verdict *string `json:"verdict"`
	// Reason is one of pkg/compare's Reason constants and Why the words text
	// prints after `unknown, `; both null unless the verdict is unknown.
	Reason *string `json:"reason"`
	Why    *string `json:"why"`

	// line is the check as text prints it; decided is its verdict, asked
	// whether a limit was given.
	line    string
	decided compare.Verdict
	asked   bool
}

// NewCompareReport is the report to write: the comparison's own, stamped with
// the version of the leettest that ran it.
func NewCompareReport(running string, r CompareReport) CompareReport {
	r.SchemaVersion = JSONSchemaVersion
	r.Mode = ModeCompare
	r.LeetTestVersion = running

	if r.Differences == nil {
		r.Differences = []CompareDiff{}
	}

	if r.Checks == nil {
		r.Checks = []CompareCheck{}
	}

	return r
}

// WriteCompareJSON writes the comparison as one JSON object and a newline.
func WriteCompareJSON(w io.Writer, r CompareReport) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	return enc.Encode(r)
}

func compareSide(path, name, startedAt, version string, p *jsonPassport) CompareSide {
	labels := map[string]string{}
	if p != nil {
		maps.Copy(labels, p.Labels)
	}

	return CompareSide{Path: path, Name: name, StartedAt: startedAt, LeetTestVersion: version, Labels: labels}
}

func verdictName(v compare.Verdict) string {
	switch v {
	case compare.Pass:
		return "pass"
	case compare.Fail:
		return "fail"
	case compare.Unknown:
	}

	return "unknown"
}

// decide records what a check's limit and verdict were. A nil limit is a check
// not asked for: no verdict, no reason.
func (c *CompareCheck) decide(limit *shr.Percent, v compare.Verdict, reason, words string) {
	c.decided = v

	if limit == nil {
		return
	}

	c.asked = true
	c.LimitPercent = ptrTo(limit.Text)
	c.Verdict = ptrTo(verdictName(v))

	if v == compare.Unknown {
		c.Reason, c.Why = ptrTo(reason), ptrTo(words)
	}
}
