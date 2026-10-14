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

// Ground: contract — dataset_reuse is config API: what is written is what the
// call carries, and left out is told apart from true (nil, not a value).
func TestParse_DatasetReuseKey(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines string
		want  *bool
	}{
		{"left out", orderLines(""), nil},
		{"true", orderLines("      dataset_reuse: true\n"), bptr(true)},
		{"false", orderLines("      dataset_reuse: false\n"), bptr(false)},
		{"false with random order and a seed", orderLines("      dataset_order: random\n      dataset_seed: 3\n      dataset_reuse: false\n"), bptr(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(datasetYAML(tc.lines)))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			got := cfg.Load.Calls[0].DatasetReuse
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("DatasetReuse = %v, want nil: left out stays nil", *got)
			case tc.want != nil && (got == nil || *got != *tc.want):
				t.Errorf("DatasetReuse = %v, want %v", got, *tc.want)
			}
		})
	}
}

func bptr(v bool) *bool { return &v }

// The key written with no value is lost by the decoder as a key left out, and
// the call would wrap its file again without a word: a config error with the line.
// Ground: contract — as dataset_seed with no value.
func TestParse_DatasetReuseWithNoValueIsAnError(t *testing.T) {
	for name, value := range map[string]string{
		"nothing":                 "",
		"a tilde":                 " ~",
		"null":                    " null",
		"an anchor on a tilde":    " &a ~",
		"a null tag":              " !!null ~",
		"an alias of a null":      " *a",
		"a null tag on the alias": " !!null *a",
	} {
		t.Run(name, func(t *testing.T) {
			lines := orderLines("      dataset_reuse:" + value + "\n")
			if strings.Contains(value, "*a") {
				lines = "      data: &a ~\n" + orderLines("      dataset_reuse:"+value+"\n")
			}

			_, err := config.Parse([]byte(datasetYAML(lines)))

			if !errors.Is(err, config.ErrDatasetReuse) {
				t.Fatalf("error = %v, want %v", err, config.ErrDatasetReuse)
			}
			if !strings.Contains(err.Error(), "line ") || !strings.Contains(err.Error(), "dataset_reuse") {
				t.Errorf("error %q does not give the line of dataset_reuse", err)
			}
		})
	}
}

// A value that is not a bool is refused, not read as true or false.
// Ground: contract — a switch that guesses what "no" or 1 meant is a surprise.
func TestParse_DatasetReuseThatIsNotABoolIsAnError(t *testing.T) {
	for name, value := range map[string]string{
		"a number":          "1",
		"zero":              "0",
		"a quoted false":    `"false"`,
		"a quoted true":     `"true"`,
		"a word":            "maybe",
		"a fraction":        "0.5",
		"a list":            "[false]",
		"the old name once": "once",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse([]byte(datasetYAML(orderLines("      dataset_reuse: " + value + "\n"))))
			if err == nil {
				t.Errorf("dataset_reuse: %s accepted", value)
			}
		})
	}
}

// Ground: contract — it says what to do with a file, so it needs one; both
// values are refused, a `true` that changes nothing is a typo in waiting.
func TestParse_DatasetReuseWithoutADatasetIsAnError(t *testing.T) {
	for _, value := range []string{"true", "false"} {
		t.Run(value, func(t *testing.T) {
			_, err := config.Parse([]byte(datasetYAML("      dataset_reuse: " + value + "\n")))
			if !errors.Is(err, config.ErrReuseWithoutDataset) {
				t.Fatalf("error = %v, want %v", err, config.ErrReuseWithoutDataset)
			}
			if !strings.Contains(err.Error(), datasetMethod) {
				t.Errorf("error %q does not name the call's method", err)
			}
		})
	}
}

// The same rules hold when the config is loaded from a file, the dataset read.
// Ground: contract — LoadFile is the CLI's way in.
func TestLoad_DatasetReuseFalse(t *testing.T) {
	cfg, err := loadDataset(t, t.TempDir(), orderLines("      dataset_reuse: false\n"),
		map[string]string{"users.jsonl": "{}\n{}\n"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c := cfg.Load.Calls[0]; c.DatasetReuse == nil || *c.DatasetReuse || len(c.Records) != 2 {
		t.Errorf("call = reuse %v records %d, want false, 2", c.DatasetReuse, len(c.Records))
	}
}
