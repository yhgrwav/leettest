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
	"errors"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// emptyText is what a config with no document says. Go strings carry explicit
// \n: a Windows checkout must not change the input.
const emptyText = "config is empty: nothing to run"

// parseNoPanic is Parse that reports a panic as a failure of the row instead of
// ending the test binary.
func parseNoPanic(t *testing.T, raw string) (cfg *config.MasterConfig, err error) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Parse(%q) panicked: %v", raw, r)
		}
	}()

	return config.Parse([]byte(raw))
}

// A file with no YAML document in it — nothing, blanks, only comments, an
// explicit null — is an invalid config with its own text, not a panic. The
// fuzz run of FuzzParse found the first two.
// Ground: contract — pkg/config is a library API; callers get an error, never a panic.
func TestParse_EmptyInput(t *testing.T) {
	for name, raw := range map[string]string{
		"nothing":            "",
		"a newline":          "\n",
		"spaces":             "   ",
		"only a comment":     "# only a comment\n",
		"fuzz comment input": "# a comment and no document\n",
		"blank lines":        "\n\n  \n",
		"null":               "null\n",
		"a tilde":            "~\n",
		"null after comment": "# off\nnull\n",
		"a tab":              "\t",
		"document marker":    "---\n",
		"marker, comment":    "--- # c\n",
		"two markers":        "---\n---\n",
		"comment, marker":    "# c\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := parseNoPanic(t, raw)

			if cfg != nil {
				t.Errorf("Parse(%q) config = %+v, want nil", raw, cfg)
			}
			if err == nil {
				t.Fatalf("Parse(%q) returned no error", raw)
			}
			if !strings.Contains(err.Error(), emptyText) {
				t.Errorf("Parse(%q) error = %q, want it to say %q", raw, err, emptyText)
			}
			for _, internal := range []string{"ast.", "nil pointer", "runtime error", "goroutine"} {
				if strings.Contains(err.Error(), internal) {
					t.Errorf("Parse(%q) error %q names an internal thing: %q", raw, err, internal)
				}
			}
		})
	}
}

// A second document with no body after a real one is not asked to say the
// config is empty: the first document is there. Only no panic and an error are
// required; which error is not specified.
// Ground: contract — pkg/config is a library API; callers get an error, never a panic.
func TestParse_EmptySecondDocumentDoesNotPanic(t *testing.T) {
	cfg, err := parseNoPanic(t, "a: 1\n---\n")

	if cfg != nil || err == nil {
		t.Errorf("Parse = (%v, %v), want a nil config and an error", cfg, err)
	}
}

// An explicitly written empty mapping is a document: it fails like any config
// with its keys left out, and does not claim to be empty.
// Ground: contract — a document that is there keeps its existing errors.
func TestParse_EmptyMappingKeepsTheMissingFieldError(t *testing.T) {
	for _, raw := range []string{"{}\n", "{}", "# a comment\n{}\n"} {
		cfg, err := parseNoPanic(t, raw)

		if cfg != nil {
			t.Errorf("Parse(%q) config = %+v, want nil", raw, cfg)
		}
		if !errors.Is(err, config.ErrInvalidIP) {
			t.Errorf("Parse(%q) error = %v, want the missing-field error (%v)", raw, err, config.ErrInvalidIP)
		}
		if err != nil && strings.Contains(err.Error(), emptyText) {
			t.Errorf("Parse(%q) error = %q, an existing document must not get the empty-config text", raw, err)
		}
	}
}
