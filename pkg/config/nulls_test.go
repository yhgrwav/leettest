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

// An alias refers to the nearest anchor of that name before it (YAML 1.2,
// 7.1), not to the last one in the document. Scenario A: the anchor is a null
// when the alias comes and a path only later, so the alias is a null.
// Ground: contract — П13; the error value, text and line are what callers see.
func TestParse_DatasetAliasTakesTheNearestPrecedingAnchor_NullThenPath(t *testing.T) {
	calls := "      data: &a ~\n" +
		"      dataset: *a\n" +
		"    - method: wallet.v1.WalletService/GetHistory\n" +
		"      rps: 10\n" +
		"      duration: 1s\n" +
		"      data: &a x.jsonl\n"

	_, err := config.Parse([]byte(datasetYAML(calls)))

	if !errors.Is(err, config.ErrEmptyDatasetPath) {
		t.Fatalf("error = %v, want %v: the alias at line 12 follows the null anchor at line 11", err, config.ErrEmptyDatasetPath)
	}
	if want := "line 12: " + config.ErrEmptyDatasetPath.Error(); err.Error() != want {
		t.Errorf("error = %q, want the direct form's text %q", err, want)
	}
}

// Scenario B: the anchor is a path when the alias comes and a null only
// later, so the alias is the path and the config is valid. The path is
// checked too: the decoder resolves the alias itself, and a null from the
// later anchor would lose the path with the check green. The anchor is the
// first call's data, the alias is the second call's dataset (data and dataset
// in one call are refused), the null anchor is the third call's data.
// Ground: contract — П13; a valid config is not refused and keeps its path.
func TestParse_DatasetAliasTakesTheNearestPrecedingAnchor_PathThenNull(t *testing.T) {
	calls := "      data: &a x.jsonl\n" +
		"    - method: wallet.v1.WalletService/GetHistory\n" +
		"      rps: 10\n" +
		"      duration: 1s\n" +
		"      dataset: *a\n" +
		"    - method: wallet.v1.WalletService/Transfer\n" +
		"      rps: 10\n" +
		"      duration: 1s\n" +
		"      data: &a ~\n"

	cfg, err := config.Parse([]byte(datasetYAML(calls)))

	if err != nil {
		t.Fatalf("error = %v, want none: the alias follows the path anchor, the later null anchor is not its target", err)
	}
	if got := cfg.Load.Calls[1].Dataset; got != "x.jsonl" {
		t.Errorf("second call dataset = %q, want %q", got, "x.jsonl")
	}
}

// The same with the anchor in app.metadata: the alias is outside the calls'
// data, and the config is valid with either reading of "nearest".
// Ground: contract — П13; a valid config is not refused and keeps its path.
func TestParse_DatasetAliasTakesTheNearestPrecedingAnchor_PathThenNullInMetadata(t *testing.T) {
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

	cfg, err := config.Parse([]byte(raw))

	if err != nil {
		t.Fatalf("error = %v, want none: the alias follows the path anchor, the later null anchor is not its target", err)
	}
	if got := cfg.Load.Calls[0].Dataset; got != "x.jsonl" {
		t.Errorf("first call dataset = %q, want %q", got, "x.jsonl")
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
