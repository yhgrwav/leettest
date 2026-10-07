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
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse([]byte(datasetYAML(line)))

			if !errors.Is(err, config.ErrEmptyDatasetPath) {
				t.Fatalf("error = %v, want %v", err, config.ErrEmptyDatasetPath)
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

// A missing key stays valid.
// Ground: contract — the call runs with an empty message, as before.
func TestParse_DatasetMissingIsValid(t *testing.T) {
	if _, err := config.Parse([]byte(datasetYAML(""))); err != nil {
		t.Errorf("error = %v, want none: no dataset is a call with no dataset", err)
	}
}
