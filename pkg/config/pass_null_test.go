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

package config_test

import (
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// nullAnchor is a call's data line that makes `*n` an alias of null.
const nullAnchor = "      data: &n ~\n"

// callPass is a plain run of one call with the given lines before its pass:
// key and the given text from the key on (the key and its value, whole).
func callPass(before, pass string) []byte {
	return []byte("app:\n  target:\n    ip: localhost\n    port: 50051\nload:\n  calls:\n" +
		"    - method: shop.v1.Catalog/Get\n      rps: 100\n      duration: 60s\n" + before + pass)
}

// searchPass is a search over one call with the given extra call lines and the
// given text for the breakpoint's pass: key (the key and its value, whole).
func searchPass(callExtra, pass string) []byte {
	return []byte(strings.Replace(searchYAML, "      timeout: 500ms\n", "      timeout: 500ms\n"+callExtra, 1) + pass)
}

// A pass: that holds no value is a threshold the user wrote and nothing
// judges: the decoder reads it as the key left out and the run exits 0.
// Ground: contract — a limit written by the user is never dropped without a word; the error value and the key's path are what callers see.
func TestPass_NullIsRefusedInACall(t *testing.T) {
	for name, tc := range map[string]struct{ before, pass string }{
		"nothing after the colon": {"", "      pass:\n"},
		"a tilde":                 {"", "      pass: ~\n"},
		"the word null":           {"", "      pass: null\n"},
		"a null tag":              {"", "      pass: !!null\n"},
		"an alias of null":        {nullAnchor, "      pass: *n\n"},
	} {
		t.Run(name, func(t *testing.T) {
			refuses(t, name, callPass(tc.before, tc.pass), "load.calls[0].pass")
		})
	}
}

// Ground: contract — the same for the search's own pass: held_rps is the limit that would be lost.
func TestPass_NullIsRefusedInASearch(t *testing.T) {
	for name, tc := range map[string]struct{ callExtra, pass string }{
		"nothing after the colon": {"", "    pass:\n"},
		"a tilde":                 {"", "    pass: ~\n"},
		"the word null":           {"", "    pass: null\n"},
		"a null tag":              {"", "    pass: !!null\n"},
		"an alias of null":        {nullAnchor, "    pass: *n\n"},
	} {
		t.Run(name, func(t *testing.T) {
			refuses(t, name, searchPass(tc.callExtra, tc.pass), "load.breakpoint.pass")
		})
	}
}

// Ground: contract — an empty section checks nothing and reads as "check";
// it is refused in a call and in a search, by the section's name.
func TestPass_EmptyMappingIsRefusedInACallAndASearch(t *testing.T) {
	refuses(t, "calls[].pass: {}", callPass("", "      pass: {}\n"), "pass")
	refuses(t, "breakpoint.pass: {}", searchPass("", "    pass: {}\n"), "breakpoint.pass")
}

// A key inside pass: with no value is a limit that has no number: the same
// lost threshold one level down. The error names the key.
// Ground: contract — a limit written by the user is never dropped without a word.
func TestPass_KeyWithNoValueIsRefusedInACall(t *testing.T) {
	for name, tc := range map[string]struct{ before, pass, key string }{
		"p99 with nothing":     {"", "      pass:\n        p99:\n        errors: 1%\n", "pass.p99"},
		"p99 a tilde":          {"", "      pass:\n        p99: ~\n        errors: 1%\n", "pass.p99"},
		"p99 null":             {"", "      pass:\n        p99: null\n        errors: 1%\n", "pass.p99"},
		"p99 a null tag":       {"", "      pass:\n        p99: !!null\n        errors: 1%\n", "pass.p99"},
		"p99 an alias of null": {nullAnchor, "      pass:\n        p99: *n\n        errors: 1%\n", "pass.p99"},
		"errors with nothing":  {"", "      pass:\n        p99: 200ms\n        errors:\n", "pass.errors"},
		"errors a tilde":       {"", "      pass:\n        p99: 200ms\n        errors: ~\n", "pass.errors"},
		"errors null":          {"", "      pass:\n        p99: 200ms\n        errors: null\n", "pass.errors"},
		"errors a null tag":    {"", "      pass:\n        p99: 200ms\n        errors: !!null\n", "pass.errors"},
		"errors an alias":      {nullAnchor, "      pass:\n        p99: 200ms\n        errors: *n\n", "pass.errors"},
		"errors alone":         {"", "      pass:\n        errors: ~\n", "pass.errors"},
	} {
		t.Run(name, func(t *testing.T) {
			refuses(t, name, callPass(tc.before, tc.pass), tc.key)
		})
	}
}

// Ground: contract — the same for held_rps, the search's only limit.
func TestPass_KeyWithNoValueIsRefusedInASearch(t *testing.T) {
	for name, tc := range map[string]struct{ callExtra, pass string }{
		"nothing after the colon": {"", "    pass:\n      held_rps:\n"},
		"a tilde":                 {"", "    pass:\n      held_rps: ~\n"},
		"the word null":           {"", "    pass:\n      held_rps: null\n"},
		"a null tag":              {"", "    pass:\n      held_rps: !!null\n"},
		"an alias of null":        {nullAnchor, "    pass:\n      held_rps: *n\n"},
	} {
		t.Run(name, func(t *testing.T) {
			refuses(t, name, searchPass(tc.callExtra, tc.pass), "held_rps")
		})
	}
}

// The inverse of the refusals above: a pass with real values is untouched by
// them. (TestPass_KeysBecomeLimits reads every call key; TestPass_HeldRPSIsAStepOfThePlan
// reads held_rps.)
// Ground: contract — the null rule does not touch a valid pass.
func TestPass_ValidSectionIsAccepted(t *testing.T) {
	cfg, err := config.Parse(callPass("", "      pass:\n        p99: 200ms\n        errors: 1%\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := cfg.Load.Calls[0].Pass; got.P99 == 0 || got.Errors == nil {
		t.Errorf("pass = %+v, want p99 and errors set", got)
	}

	cfg, err = config.Parse(searchPass("", "    pass:\n      held_rps: 744\n"))
	if err != nil {
		t.Fatalf("parse search: %v", err)
	}
	if got := cfg.Load.Breakpoint.Pass.HeldRPS; got != 744 {
		t.Errorf("held_rps = %d, want 744", got)
	}
}
