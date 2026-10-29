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
	"strconv"
	"strings"
	"testing"
)

// Ground: review verdict V3 — the config allows A-Za-z0-9_.- in a label key,
// Prometheus allows [a-zA-Z_][a-zA-Z0-9_]*: a key is refused, not rewritten
// (a.b and a_b would merge), and so are "__" names and the labels the series
// bring. The error names the key.
func TestCheckLabelName_RefusesWhatPrometheusCannotTake(t *testing.T) {
	for _, name := range []string{
		"commit.sha", "ci-env", "1x", "a b", "x\n", "é", "env!", // not [a-zA-Z_][a-zA-Z0-9_]*
		"__name__", "__x", "__", // reserved prefix
		"method", "category", "cause", "le", // the series' own labels
	} {
		t.Run(name, func(t *testing.T) {
			err := CheckLabelName(name)
			if err == nil {
				t.Fatalf("CheckLabelName(%q) = nil, want an error", name)
			}
			// The key as written, or quoted when it holds a control character.
			if msg := err.Error(); !strings.Contains(msg, name) && !strings.Contains(msg, strconv.Quote(name)) {
				t.Errorf("the error %q does not name the key %q", err, name)
			}
		})
	}
}

// Ground: edge — an empty key is refused too (it cannot be in a map from
// valid YAML, but the library does not trust its caller).
func TestCheckLabelName_RefusesAnEmptyName(t *testing.T) {
	if err := CheckLabelName(""); err == nil {
		t.Error(`CheckLabelName("") = nil, want an error`)
	}
}

// Ground: review verdict V3 — the other side of the line: what is valid stays
// valid. Green on the stub (it accepts everything): it holds the line for the
// code; a single underscore prefix, upper case, a double underscore inside and
// a near-miss of a reserved name are fine.
func TestCheckLabelName_AcceptsValidNames(t *testing.T) {
	for _, name := range []string{"env", "commit", "ci_env", "_x", "A1", "x__y", "Method", "LE", "cause2", "methods", "a"} {
		if err := CheckLabelName(name); err != nil {
			t.Errorf("CheckLabelName(%q) = %v, want nil", name, err)
		}
	}
}
