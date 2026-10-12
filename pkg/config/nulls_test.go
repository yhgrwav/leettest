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

// datasetYAML ends at line 10, so the first extra call line is line 11.
// `dataset:` with no value is how a path is lost: the decoder reads it as the
// key left out and the call runs with an empty message. It is refused like
// `dataset: ""`, and the message points at the key.
// Ground: contract — pkg/config is a library API; the error value and the line are what callers see.
func TestParse_DatasetNullIsAnError(t *testing.T) {
	for name, line := range map[string]string{
		"nothing after the colon": "      dataset:\n",
		"a tilde":                 "      dataset: ~\n",
		"the word null":           "      dataset: null\n",
		"the word NULL":           "      dataset: NULL\n",
		"an anchor on a tilde":    "      dataset: &a ~\n",
		"an anchor on null":       "      dataset: &a null\n",
		"a null tag":              "      dataset: !!null\n",
		"a null tag and a tilde":  "      dataset: !!null ~\n",
		"an anchor on a null tag": "      dataset: &a !!null\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse([]byte(datasetYAML(line)))

			if !errors.Is(err, config.ErrEmptyDatasetPath) {
				t.Fatalf("error = %v, want %v", err, config.ErrEmptyDatasetPath)
			}
			if errors.Is(err, config.ErrReadConfig) {
				t.Errorf("error = %v, is a read error; the category is the empty path", err)
			}
			if want := "line 11:"; !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to carry %q", err, want)
			}
		})
	}
}

// The line is that of the key: a second call's null is not reported at the
// first call's line.
// Ground: contract — the line is the one place the user is sent to.
func TestParse_DatasetNullInASecondCallNamesItsLine(t *testing.T) {
	second := "      dataset: x.jsonl\n" +
		"    - method: wallet.v1.WalletService/GetHistory\n" +
		"      rps: 10\n" +
		"      duration: 1s\n" +
		"      dataset: ~\n"

	_, err := config.Parse([]byte(datasetYAML(second)))

	if !errors.Is(err, config.ErrEmptyDatasetPath) {
		t.Fatalf("error = %v, want %v", err, config.ErrEmptyDatasetPath)
	}
	if want := "line 15:"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to carry %q", err, want)
	}
	if strings.Contains(err.Error(), "line 11:") {
		t.Errorf("error = %q, names the first call's line", err)
	}
}

// A `dataset` key at any other path is not the call's dataset: a null there
// is read as it was before, whatever that is. Frozen as main does it.
// Ground: characterization — the rule covers load.calls[i].dataset and nothing else.
func TestParse_NullOutsideDatasetIsUnchanged(t *testing.T) {
	cases := map[string]struct {
		callLines string
		wantErr   bool
	}{
		"a key named dataset inside the request data": {"      data:\n        dataset: ~\n", false},
		"a null request data":                         {"      data: ~\n", false},
		"a null timeout":                              {"      timeout: ~\n", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse([]byte(datasetYAML(tc.callLines)))

			if errors.Is(err, config.ErrEmptyDatasetPath) {
				t.Fatalf("error = %v, a null outside load.calls[i].dataset was read as an empty dataset path", err)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("error = %v, want error: %v", err, tc.wantErr)
			}
		})
	}
}

// Under app.metadata the key is a header name that happens to be "dataset".
// Ground: characterization — same rule, a path the check must not reach.
func TestParse_NullDatasetKeyInMetadataIsUnchanged(t *testing.T) {
	raw := `
app:
  target:
    ip: localhost
    port: 50051
  metadata:
    dataset: ~
load:
  calls:
    - method: ` + datasetMethod + `
      rps: 10
      duration: 1s
`

	_, err := config.Parse([]byte(raw))

	if errors.Is(err, config.ErrEmptyDatasetPath) {
		t.Fatalf("error = %v, a metadata key named dataset was read as the call's dataset path", err)
	}
}

// An explicit empty string is refused through the path check, not the null
// check: no line. Frozen as main does it.
// Ground: characterization — the null rule unwraps anchors and tags, not strings.
func TestParse_EmptyStringTagIsUnchanged(t *testing.T) {
	_, err := config.Parse([]byte(datasetYAML("      dataset: !!str \"\"\n")))

	if !errors.Is(err, config.ErrEmptyDatasetPath) {
		t.Fatalf("error = %v, want %v", err, config.ErrEmptyDatasetPath)
	}
	if strings.Contains(err.Error(), "line ") {
		t.Errorf("error = %q, carries a line; main names the call, not the key", err)
	}
}

// An alias that resolves to a null is the same lost path as a direct null: it
// is refused with the direct form's error, at the dataset key's line. The
// anchor sits on line 11 (the call's data), the alias on line 12.
// Ground: contract — П13, no config silently ignored; the error value, text and line are what callers see.
func TestParse_DatasetAliasOfNullIsAnError(t *testing.T) {
	for name, lines := range map[string]string{
		"an anchor on a tilde":        "      data: &a ~\n      dataset: *a\n",
		"an anchor on null":           "      data: &a null\n      dataset: *a\n",
		"an anchor on a null tag":     "      data: &a !!null ~\n      dataset: *a\n",
		"an anchor with no value":     "      data: &a\n      dataset: *a\n",
		"a null tag on the alias":     "      data: &a ~\n      dataset: !!null *a\n",
		"a str tag on the null alias": "      data: &a ~\n      dataset: !!str *a\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse([]byte(datasetYAML(lines)))

			if !errors.Is(err, config.ErrEmptyDatasetPath) {
				t.Fatalf("error = %v, want %v", err, config.ErrEmptyDatasetPath)
			}
			if errors.Is(err, config.ErrReadConfig) {
				t.Errorf("error = %v, is a read error; the category is the empty path", err)
			}
			if want := "line 12: " + config.ErrEmptyDatasetPath.Error(); err.Error() != want {
				t.Errorf("error = %q, want the direct form's text %q", err, want)
			}
		})
	}
}

// The alias is followed from the second call to an anchor in the first: the
// line is that of the alias, not of the anchor.
// Ground: contract — the line is the one place the user is sent to.
func TestParse_DatasetAliasOfNullInASecondCallNamesItsLine(t *testing.T) {
	second := "      data: &a ~\n" +
		"    - method: wallet.v1.WalletService/GetHistory\n" +
		"      rps: 10\n" +
		"      duration: 1s\n" +
		"      dataset: *a\n"

	_, err := config.Parse([]byte(datasetYAML(second)))

	if !errors.Is(err, config.ErrEmptyDatasetPath) {
		t.Fatalf("error = %v, want %v", err, config.ErrEmptyDatasetPath)
	}
	if want := "line 15:"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to carry %q", err, want)
	}
	if strings.Contains(err.Error(), "line 11:") {
		t.Errorf("error = %q, names the anchor's line", err)
	}
}

// wantDuplicateAnchor checks the refusal of an anchor name defined twice: the
// sentinel, not a read error, and the whole text with both lines.
func wantDuplicateAnchor(t *testing.T, err error, want string) {
	t.Helper()

	if !errors.Is(err, config.ErrDuplicateAnchor) {
		t.Fatalf("error = %v, want %v", err, config.ErrDuplicateAnchor)
	}
	if errors.Is(err, config.ErrReadConfig) {
		t.Errorf("error = %v, is a read error; the category is the duplicate anchor", err)
	}
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

// An anchor name defined twice is refused, whatever the values: the decoder
// resolves an alias to the last definition in the document, not the nearest
// one before it, so a reused name silently swaps a value. Scenario A: a null
// and then a path under one name, the alias between them.
// Ground: contract — П13; the error value, text and both lines are what callers see.
func TestParse_AnchorDefinedTwiceIsAnError_NullThenPath(t *testing.T) {
	calls := "      data: &a ~\n" +
		"      dataset: *a\n" +
		"    - method: wallet.v1.WalletService/GetHistory\n" +
		"      rps: 10\n" +
		"      duration: 1s\n" +
		"      data: &a x.jsonl\n"

	_, err := config.Parse([]byte(datasetYAML(calls)))

	wantDuplicateAnchor(t, err, "line 16: anchor &a is already defined at line 11; give it another name")
}

// Scenario B: a path and then a null under one name. The config looks valid
// (the alias is the path), but the decoder gives the call the later null and
// the path is lost; it is refused as well. The anchor is the first call's
// data, the alias is the second call's dataset (data and dataset in one call
// are refused), the second anchor is the third call's data.
// Ground: contract — П13; no rule of the form "a repeat is fine when the decoder happens to get it right".
func TestParse_AnchorDefinedTwiceIsAnError_PathThenNull(t *testing.T) {
	calls := "      data: &a x.jsonl\n" +
		"    - method: wallet.v1.WalletService/GetHistory\n" +
		"      rps: 10\n" +
		"      duration: 1s\n" +
		"      dataset: *a\n" +
		"    - method: wallet.v1.WalletService/Transfer\n" +
		"      rps: 10\n" +
		"      duration: 1s\n" +
		"      data: &a ~\n"

	_, err := config.Parse([]byte(datasetYAML(calls)))

	wantDuplicateAnchor(t, err, "line 19: anchor &a is already defined at line 11; give it another name")
}

// The same with the first anchor in app.metadata: the decoder happens to
// resolve this one right, and it is refused all the same.
// Ground: contract — П13; one rule for every anchor.
func TestParse_AnchorDefinedTwiceIsAnError_PathThenNullInMetadata(t *testing.T) {
	raw := `
app:
  target:
    ip: localhost
    port: 50051
  metadata:
    k: &a x.jsonl
load:
  calls:
    - method: ` + datasetMethod + `
      rps: 10
      duration: 1s
      dataset: *a
    - method: wallet.v1.WalletService/GetHistory
      rps: 10
      duration: 1s
      data: &a ~
`

	_, err := config.Parse([]byte(raw))

	wantDuplicateAnchor(t, err, "line 17: anchor &a is already defined at line 7; give it another name")
}

// The rule is not limited to the calls or to the dataset: two anchors of one
// name among app.metadata values are refused, with no alias in sight.
// Ground: contract — П13; the rule covers the whole config.
func TestParse_AnchorDefinedTwiceInMetadataIsAnError(t *testing.T) {
	raw := `
app:
  target:
    ip: localhost
    port: 50051
  metadata:
    first: &m one
    second: &m two
load:
  calls:
    - method: ` + datasetMethod + `
      rps: 10
      duration: 1s
`

	_, err := config.Parse([]byte(raw))

	wantDuplicateAnchor(t, err, "line 8: anchor &m is already defined at line 7; give it another name")
}

// Two different names are two anchors: each alias is its own anchor's value.
// Ground: characterization — only a repeated name is refused.
func TestParse_DifferentAnchorNamesAreAccepted(t *testing.T) {
	raw := `
app:
  target:
    ip: localhost
    port: 50051
  metadata:
    first: &p x.jsonl
    second: &q y.jsonl
load:
  calls:
    - method: ` + datasetMethod + `
      rps: 10
      duration: 1s
      dataset: *p
    - method: wallet.v1.WalletService/GetHistory
      rps: 10
      duration: 1s
      dataset: *q
`

	cfg, err := config.Parse([]byte(raw))

	if err != nil {
		t.Fatalf("error = %v, want none: two names are two anchors", err)
	}
	if got := cfg.Load.Calls[0].Dataset; got != "x.jsonl" {
		t.Errorf("first call dataset = %q, want %q", got, "x.jsonl")
	}
	if got := cfg.Load.Calls[1].Dataset; got != "y.jsonl" {
		t.Errorf("second call dataset = %q, want %q", got, "y.jsonl")
	}
	if got, want := cfg.App.Metadata["first"]+" "+cfg.App.Metadata["second"], "x.jsonl y.jsonl"; got != want {
		t.Errorf("metadata first and second = %q, want %q", got, want)
	}
}

// An alias to a path is a path: a config that reuses one dataset file for two
// calls keeps working. A null alias used outside a call's dataset is not
// touched.
// Ground: characterization — only a null reached as load.calls[i].dataset is refused.
func TestParse_DatasetAliasOfAPathIsAccepted(t *testing.T) {
	t.Run("a path shared by two calls", func(t *testing.T) {
		calls := "      dataset: &p x.jsonl\n" +
			"    - method: wallet.v1.WalletService/GetHistory\n" +
			"      rps: 10\n" +
			"      duration: 1s\n" +
			"      dataset: *p\n"

		cfg, err := config.Parse([]byte(datasetYAML(calls)))

		if err != nil {
			t.Fatalf("error = %v, want none: an alias to a path is a path", err)
		}
		if got := len(cfg.Load.Calls); got != 2 {
			t.Fatalf("calls = %d, want 2", got)
		}
		if got := cfg.Load.Calls[1].Dataset; got != "x.jsonl" {
			t.Errorf("second call dataset = %q, want %q: an alias to a path is that path", got, "x.jsonl")
		}
	})

	t.Run("a path shared by two calls behind a str tag", func(t *testing.T) {
		calls := "      dataset: &p x.jsonl\n" +
			"    - method: wallet.v1.WalletService/GetHistory\n" +
			"      rps: 10\n" +
			"      duration: 1s\n" +
			"      dataset: !!str *p\n"

		cfg, err := config.Parse([]byte(datasetYAML(calls)))

		if err != nil {
			t.Fatalf("error = %v, want none: an alias to a path is a path", err)
		}
		if got := len(cfg.Load.Calls); got != 2 {
			t.Fatalf("calls = %d, want 2", got)
		}
		if got := cfg.Load.Calls[1].Dataset; got != "x.jsonl" {
			t.Errorf("second call dataset = %q, want %q: an alias to a path is that path", got, "x.jsonl")
		}
	})

	t.Run("a null alias outside the dataset", func(t *testing.T) {
		_, err := config.Parse([]byte(datasetYAML("      data: &a ~\n      timeout: *a\n")))

		if errors.Is(err, config.ErrEmptyDatasetPath) {
			t.Fatalf("error = %v, a null alias outside load.calls[i].dataset was read as an empty dataset path", err)
		}
	})
}

// A missing key stays valid.
// Ground: contract — the call runs with an empty message, as before.
func TestParse_DatasetMissingIsValid(t *testing.T) {
	if _, err := config.Parse([]byte(datasetYAML(""))); err != nil {
		t.Errorf("error = %v, want none: no dataset is a call with no dataset", err)
	}
}
