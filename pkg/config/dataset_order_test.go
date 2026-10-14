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

// order is the call's lines for a dataset with the given extra lines.
func orderLines(extra string) string {
	return "      dataset: users.jsonl\n" + extra
}

// Ground: contract — the two keys are config API: what is written is what the call carries.
func TestParse_DatasetOrderAndSeedKeys(t *testing.T) {
	for _, tc := range []struct {
		name      string
		lines     string
		wantOrder *string
		wantSeed  *int64
	}{
		{"both left out", orderLines(""), nil, nil},
		{"file", orderLines("      dataset_order: file\n"), sptr("file"), nil},
		{"random without a seed", orderLines("      dataset_order: random\n"), sptr("random"), nil},
		{"random with a seed", orderLines("      dataset_order: random\n      dataset_seed: 42\n"), sptr("random"), ptr(42)},
		{"seed 0 is a seed", orderLines("      dataset_order: random\n      dataset_seed: 0\n"), sptr("random"), ptr(0)},
		{"the largest seed", orderLines("      dataset_order: random\n      dataset_seed: 9007199254740991\n"), sptr("random"), ptr(9007199254740991)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(datasetYAML(tc.lines)))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			call := cfg.Load.Calls[0]
			if (call.DatasetOrder == nil) != (tc.wantOrder == nil) || (call.DatasetOrder != nil && *call.DatasetOrder != *tc.wantOrder) {
				t.Errorf("DatasetOrder = %v, want %v (nil: left out)", call.DatasetOrder != nil, tc.wantOrder != nil)
			}
			switch {
			case tc.wantSeed == nil && call.DatasetSeed != nil:
				t.Errorf("DatasetSeed = %d, want none: a seed left out stays nil", *call.DatasetSeed)
			case tc.wantSeed != nil && (call.DatasetSeed == nil || *call.DatasetSeed != *tc.wantSeed):
				t.Errorf("DatasetSeed = %v, want %d", call.DatasetSeed, *tc.wantSeed)
			}
		})
	}
}

func ptr(v int64) *int64 { return &v }

func sptr(v string) *string { return &v }

// Each wrong combination stops the run before it, with the error of its own kind.
// Ground: contract — config errors before the run; the sentinel is the API.
func TestParse_DatasetOrderAndSeedErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines string
		want  error
	}{
		{"an order that is not one", orderLines("      dataset_order: shuffle\n"), config.ErrDatasetOrder},
		{"an order in capitals", orderLines("      dataset_order: Random\n"), config.ErrDatasetOrder},
		{"order without a dataset", "      dataset_order: random\n", config.ErrOrderWithoutDataset},
		{"file order without a dataset", "      dataset_order: file\n", config.ErrOrderWithoutDataset},
		{"seed without a dataset", "      dataset_order: random\n      dataset_seed: 1\n", config.ErrOrderWithoutDataset},
		{"seed with no order", orderLines("      dataset_seed: 1\n"), config.ErrSeedWithoutRandom},
		{"seed with file order", orderLines("      dataset_order: file\n      dataset_seed: 1\n"), config.ErrSeedWithoutRandom},
		{"seed 2^53", orderLines("      dataset_order: random\n      dataset_seed: 9007199254740992\n"), config.ErrDatasetSeed},
		{"negative seed", orderLines("      dataset_order: random\n      dataset_seed: -1\n"), config.ErrDatasetSeed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse([]byte(datasetYAML(tc.lines)))
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
			if err != nil && !strings.Contains(err.Error(), datasetMethod) {
				t.Errorf("error %q does not name the call's method", err)
			}
		})
	}
}

// A seed that is not a whole number is no seed: a fraction, a string, a number
// beyond any integer. The wording is the YAML decoder's or ours; the config is refused.
// Ground: contract — a seed the report cannot print exactly is never accepted.
func TestParse_DatasetSeedThatIsNotAnIntegerIsAnError(t *testing.T) {
	for name, value := range map[string]string{
		"a fraction":       "1.5",
		"a string":         `"abc"`,
		"a numeric string": `"42"`,
		"beyond 2^63":      "13835058055282163713",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse([]byte(datasetYAML(orderLines("      dataset_order: random\n      dataset_seed: " + value + "\n"))))
			if err == nil {
				t.Errorf("dataset_seed: %s accepted", value)
			}
		})
	}
}

// The same rules hold when the config is loaded from a file, the dataset read.
// Ground: contract — LoadFile is the CLI's way in.
func TestLoad_DatasetOrderRandomWithSeed(t *testing.T) {
	cfg, err := loadDataset(t, t.TempDir(), orderLines("      dataset_order: random\n      dataset_seed: 5\n"),
		map[string]string{"users.jsonl": "{}\n{}\n"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c := cfg.Load.Calls[0]; c.DatasetOrder == nil || *c.DatasetOrder != "random" || c.DatasetSeed == nil || *c.DatasetSeed != 5 || len(c.Records) != 2 {
		t.Errorf("call = order set %v seed set %v records %d, want random, 5, 2", c.DatasetOrder != nil, c.DatasetSeed != nil, len(c.Records))
	}
}

// A key written with no value is lost by the decoder as a key left out: the
// repeat run that forgot its number would draw another seed and say nothing.
// Ground: contract — as `dataset:` with no value (TestParse_DatasetNullIsAnError).
func TestParse_DatasetOrderAndSeedWithNoValueIsAnError(t *testing.T) {
	for _, key := range []struct {
		name string
		want error
	}{
		{"dataset_order", config.ErrDatasetOrder},
		{"dataset_seed", config.ErrDatasetSeed},
	} {
		for _, value := range []string{"", " ~", " null", " &a ~"} {
			t.Run(key.name+value, func(t *testing.T) {
				lines := orderLines("      dataset_order: random\n      " + key.name + ":" + value + "\n")
				if key.name == "dataset_order" {
					lines = orderLines("      dataset_order:" + value + "\n")
				}

				_, err := config.Parse([]byte(datasetYAML(lines)))

				if !errors.Is(err, key.want) {
					t.Fatalf("error = %v, want %v", err, key.want)
				}
				if !strings.Contains(err.Error(), "line ") || !strings.Contains(err.Error(), key.name) {
					t.Errorf("error %q does not give the line of %s", err, key.name)
				}
			})
		}
	}
}
