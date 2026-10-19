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

package share

// Percent is a share written in the config, kept in thousandths of a percent
// so that comparisons are integer: 0.7% is 700. Text is it as written; empty
// Text is a Percent nobody wrote.
type Percent struct {
	Thousandths int64
	Text        string
}

// Default is the limit of a search that sets none.
var Default = Percent{Thousandths: 1000, Text: "1%"}

// Verdict is which side of the limit a run's numbers fall on.
type Verdict int

const (
	// Undecided: the target's own failures are within the limit, all of them
	// together are not; the rest are the client's, which say nothing of the
	// target.
	Undecided Verdict = iota
	// Holds: all failures together are within the limit.
	Holds
	// Breaks: the target's own failures are over the limit.
	Breaks
)

// Parse reads text such as 0.125% for the config key key, which a refusal
// names: up to three digits after the point, at most 100%.
func Parse(_, _ string) (Percent, error) {
	return Percent{}, nil
}

// Decide judges bad failed calls of total, badClient of them the client's
// (rejected requests, client errors, our bad responses). Nothing sent holds.
func Decide(_, _, _ int, _ Percent) Verdict {
	return Undecided
}

// Format is the share bad/total as a percent to print beside limit: to a
// tenth of a percent, or, when that lands on the wrong side of the printed
// limit, to as many digits as the limit has, rounded up for Breaks.
func Format(_, _ int, _ Percent, _ Verdict) string {
	return ""
}
