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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// A scenario step's dataset (v0.9 unit 3c). Compile stub: config.Step has the
// dataset fields, but nulls.go still refuses the keys, so every test of a step
// that is accepted, or refused in the words of the spec, is red until the code.
// The guard tests are green now and stay so.

const (
	// stepDatasetLine is the key of a step 10 spaces deep.
	stepDatasetLine = "          dataset: steps.jsonl\n"
	stepsFile       = "steps.jsonl"
	stepRecords     = "{\"id\":1}\n{\"id\":2}\n{\"id\":3}\n"
)

// datasetStep is a Transfer step with the given lines (10 spaces deep) after its method.
func datasetStep(lines string) string { return stepHead + lines }

// loadStepDataset writes raw as the config and the files next to it and loads it
// by path: only LoadFile reads a dataset. files maps a path under the config's
// directory to its content.
func loadStepDataset(t *testing.T, raw string, files map[string]string) (*config.MasterConfig, error) {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cfgPath := filepath.Join(dir, "leettest.yaml")
	if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return config.LoadFile(cfgPath)
}

func stepRaw(records []config.DatasetRecord) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = string(r.JSON)
	}

	return out
}

// loadedStep is the first step of the first call of a scenario that must load.
func loadedStep(t *testing.T, raw string, files map[string]string) config.Step {
	t.Helper()

	cfg, err := loadStepDataset(t, raw, files)
	if err != nil {
		t.Fatalf("LoadFile() = %v, want the step's dataset accepted", err)
	}

	return cfg.Load.Calls[0].Steps[0]
}

// Ground: contract — pkg/config is a library API; the step's file is read as the call's.
func TestLoad_StepDatasetReadsEachLine(t *testing.T) {
	step := loadedStep(t, twoStepScenario(datasetStep(stepDatasetLine)), map[string]string{stepsFile: stepRecords})

	if got, want := stepRaw(step.Records), []string{`{"id":1}`, `{"id":2}`, `{"id":3}`}; !slices.Equal(got, want) {
		t.Errorf("Steps[0].Records = %q, want %q", got, want)
	}
}

// A blank line is skipped and a line keeps its own number, as in a call.
// Ground: contract — the line number is what an error names (the CLI's does-not-fit).
func TestLoad_StepDatasetRecordsKeepTheirLineNumbers(t *testing.T) {
	step := loadedStep(t, twoStepScenario(datasetStep(stepDatasetLine)),
		map[string]string{stepsFile: "{\"id\":1}\n\n{\"id\":2}\n"})

	var lines []int
	for _, r := range step.Records {
		lines = append(lines, r.Line)
	}
	if want := []int{1, 3}; !slices.Equal(lines, want) {
		t.Errorf("record lines = %v, want %v", lines, want)
	}
}

// What the user wrote is what errors and the report print.
// Ground: contract — as the call's Dataset.
func TestLoad_StepDatasetPathAsWrittenKept(t *testing.T) {
	step := loadedStep(t, twoStepScenario(datasetStep("          dataset: data/steps.jsonl\n")),
		map[string]string{"data/steps.jsonl": stepRecords})

	if step.Dataset != "data/steps.jsonl" {
		t.Errorf("Steps[0].Dataset = %q, want the path as written", step.Dataset)
	}
	if step.RawDataset == nil || *step.RawDataset != "data/steps.jsonl" {
		t.Errorf("Steps[0].RawDataset = %v, want the field as written", step.RawDataset)
	}
}

// Ground: contract — the sha256 of the file as it is on disk, what sha256sum prints.
func TestLoad_StepDatasetSHA256IsOfTheFile(t *testing.T) {
	step := loadedStep(t, twoStepScenario(datasetStep(stepDatasetLine)), map[string]string{stepsFile: stepRecords})

	sum := sha256.Sum256([]byte(stepRecords))
	if want := hex.EncodeToString(sum[:]); step.DatasetSHA256 != want {
		t.Errorf("Steps[0].DatasetSHA256 = %q, want %q", step.DatasetSHA256, want)
	}
}

// The path is the config file's directory, not the working directory.
// Ground: contract — as the call's path.
func TestLoad_StepDatasetPathRelativeToConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	step := loadedStep(t, twoStepScenario(datasetStep("          dataset: data/steps.jsonl\n")),
		map[string]string{"data/steps.jsonl": "{\"id\":7}\n"})

	if got, want := stepRaw(step.Records), []string{`{"id":7}`}; !slices.Equal(got, want) {
		t.Errorf("Steps[0].Records = %q, want %q", got, want)
	}
}

// Ground: contract — each step reads its own file, by its own path.
func TestLoad_TwoStepsOfACallHaveTheirOwnDatasets(t *testing.T) {
	raw := twoStepScenario(datasetStep("          dataset: one.jsonl\n") +
		"        - method: " + getHistory + "\n          dataset: two.jsonl\n")

	cfg, err := loadStepDataset(t, raw, map[string]string{"one.jsonl": "{\"a\":1}\n", "two.jsonl": "{\"b\":1}\n{\"b\":2}\n"})
	if err != nil {
		t.Fatalf("LoadFile() = %v, want two steps with a dataset each", err)
	}

	steps := cfg.Load.Calls[0].Steps
	if len(steps[0].Records) != 1 || len(steps[1].Records) != 2 {
		t.Errorf("records = %d and %d, want 1 and 2", len(steps[0].Records), len(steps[1].Records))
	}
}

// A call with a dataset of its own and a step with one are two datasets, each in
// its own place.
// Ground: contract — spec 2, the call's dataset and the step's are independent.
func TestLoad_CallAndStepEachHaveADataset(t *testing.T) {
	raw := tplScenario("      dataset: call.jsonl\n", datasetStep(stepDatasetLine))

	cfg, err := loadStepDataset(t, raw, map[string]string{"call.jsonl": "{\"c\":1}\n{\"c\":2}\n", stepsFile: stepRecords})
	if err != nil {
		t.Fatalf("LoadFile() = %v, want a call and a step with a dataset valid", err)
	}

	call := cfg.Load.Calls[0]
	if len(call.Records) != 2 || len(call.Steps[0].Records) != 3 {
		t.Errorf("records = %d for the call and %d for the step, want 2 and 3", len(call.Records), len(call.Steps[0].Records))
	}
}

// Parse reads no file: the step names a dataset and holds none of its records.
// Ground: contract — as TestParse_DatasetNotRead for a call.
func TestParse_StepDatasetNotRead(t *testing.T) {
	cfg, err := config.Parse([]byte(twoStepScenario(datasetStep(stepDatasetLine))))
	if err != nil {
		t.Fatalf("Parse() = %v, want the key accepted without a file", err)
	}

	step := cfg.Load.Calls[0].Steps[0]
	if step.Dataset != "steps.jsonl" || len(step.Records) != 0 {
		t.Errorf("Steps[0]: Dataset = %q, Records = %d, want the path and no records", step.Dataset, len(step.Records))
	}
}

// Ground: contract — the missing file names the path as written and the os error, never the joined path.
func TestLoad_StepDatasetMissingFileNamesThePathAsWritten(t *testing.T) {
	_, err := loadStepDataset(t, twoStepScenario(datasetStep("          dataset: data/absent.jsonl\n")), nil)

	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error = %v, want the os error behind it (fs.ErrNotExist)", err)
	}
	if want := "load.calls[0].steps[0]: dataset data/absent.jsonl: "; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to say %q", err, want)
	}
}

// Ground: contract — a line that is not JSON is named by file and line, never quoted: it may be a secret.
func TestLoad_StepDatasetNotJSONNamesItsLine(t *testing.T) {
	_, err := loadStepDataset(t, twoStepScenario(datasetStep(stepDatasetLine)),
		map[string]string{stepsFile: "{\"id\":1}\nhunter2-not-json\n"})

	if err == nil {
		t.Fatal("LoadFile() = nil, want a refusal of a line that is not JSON")
	}
	if want := "load.calls[0].steps[0]: dataset steps.jsonl:2: not JSON"; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to say %q", err, want)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error = %q quotes the line", err)
	}
}

// Ground: contract — a dataset that gives the step nothing to send is refused.
func TestLoad_StepDatasetEmptyIsAnError(t *testing.T) {
	for name, content := range map[string]string{
		"an empty file":    "",
		"only blank lines": "\n  \n\t\n\r\n",
		"only a byte mark": "\xEF\xBB\xBF",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadStepDataset(t, twoStepScenario(datasetStep(stepDatasetLine)), map[string]string{stepsFile: content})

			if want := "load.calls[0].steps[0]: dataset steps.jsonl: no requests"; err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want it to say %q", err, want)
			}
		})
	}
}

// The second call's step is named by its own index.
// Ground: contract — the path of the error is where the user finds the key.
func TestLoad_StepDatasetErrorNamesTheSecondCall(t *testing.T) {
	raw := scenarioCall("") + secondCall("          dataset: absent.jsonl\n")

	_, err := loadStepDataset(t, raw, nil)

	if want := "load.calls[1].steps[0]: dataset absent.jsonl: "; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to say %q", err, want)
	}
}

// Ground: contract — as `dataset: ""` of a call (TestParse_DatasetEmptyPathIsAnError).
func TestParse_StepDatasetEmptyPathIsAnError(t *testing.T) {
	requireRefusal(t, twoStepScenario(datasetStep("          dataset: \"\"\n")), config.ErrEmptyDatasetPath,
		"load.calls[0].steps[0]")
}

// A key with no value is lost by the decoder as a key left out: the step would
// run on its own data and the report would not say why.
// Ground: contract — as the call's keys (TestParse_DatasetOrderAndSeedWithNoValueIsAnError).
func TestParse_StepDatasetKeysWithNoValueAreAnError(t *testing.T) {
	for _, key := range []struct {
		name string
		want error
	}{
		{"dataset", config.ErrEmptyDatasetPath},
		{"dataset_order", config.ErrDatasetOrder},
		{"dataset_seed", config.ErrDatasetSeed},
	} {
		for _, value := range []string{"", " ~", " null", " &a ~"} {
			t.Run(key.name+value, func(t *testing.T) {
				lines := stepDatasetLine + "          dataset_order: random\n"
				switch key.name {
				case "dataset":
					lines = "          dataset:" + value + "\n"
				case "dataset_order":
					lines = stepDatasetLine + "          dataset_order:" + value + "\n"
				default:
					lines += "          dataset_seed:" + value + "\n"
				}

				requireRefusal(t, twoStepScenario(datasetStep(lines)), key.want,
					"line ", "load.calls[0].steps[0]."+key.name)
			})
		}
	}
}

// Ground: contract — the keys are config API: what is written is what the step carries.
func TestParse_StepDatasetOrderAndSeedKeys(t *testing.T) {
	for _, tc := range []struct {
		name      string
		lines     string
		wantOrder *string
		wantSeed  *int64
	}{
		{"both left out", stepDatasetLine, nil, nil},
		{"file", stepDatasetLine + "          dataset_order: file\n", sptr("file"), nil},
		{"random without a seed", stepDatasetLine + "          dataset_order: random\n", sptr("random"), nil},
		{"random with a seed", stepDatasetLine + "          dataset_order: random\n          dataset_seed: 42\n", sptr("random"), ptr(42)},
		{"seed 0 is a seed", stepDatasetLine + "          dataset_order: random\n          dataset_seed: 0\n", sptr("random"), ptr(0)},
		{
			"the largest seed", stepDatasetLine + "          dataset_order: random\n          dataset_seed: 9007199254740991\n",
			sptr("random"), ptr(9007199254740991),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(twoStepScenario(datasetStep(tc.lines))))
			if err != nil {
				t.Fatalf("Parse() = %v, want the step accepted", err)
			}

			step := cfg.Load.Calls[0].Steps[0]
			if (step.DatasetOrder == nil) != (tc.wantOrder == nil) || (step.DatasetOrder != nil && *step.DatasetOrder != *tc.wantOrder) {
				t.Errorf("DatasetOrder = %v, want %v (nil: left out)", step.DatasetOrder, tc.wantOrder)
			}
			switch {
			case tc.wantSeed == nil && step.DatasetSeed != nil:
				t.Errorf("DatasetSeed = %d, want none: a seed left out stays nil", *step.DatasetSeed)
			case tc.wantSeed != nil && (step.DatasetSeed == nil || *step.DatasetSeed != *tc.wantSeed):
				t.Errorf("DatasetSeed = %v, want %d", step.DatasetSeed, *tc.wantSeed)
			}
		})
	}
}

// Each wrong combination stops the run before it, with the error of its own
// kind and the step's path.
// Ground: contract — the sentinels are the API, the same as the call's.
func TestParse_StepDatasetOrderAndSeedErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines string
		want  error
	}{
		{"an order that is not one", stepDatasetLine + "          dataset_order: shuffle\n", config.ErrDatasetOrder},
		{"an order in capitals", stepDatasetLine + "          dataset_order: Random\n", config.ErrDatasetOrder},
		{"order without a dataset", "          dataset_order: random\n", config.ErrOrderWithoutDataset},
		{"file order without a dataset", "          dataset_order: file\n", config.ErrOrderWithoutDataset},
		{"seed without a dataset", "          dataset_order: random\n          dataset_seed: 1\n", config.ErrOrderWithoutDataset},
		{"seed with no order", stepDatasetLine + "          dataset_seed: 1\n", config.ErrSeedWithoutRandom},
		{"seed with file order", stepDatasetLine + "          dataset_order: file\n          dataset_seed: 1\n", config.ErrSeedWithoutRandom},
		{"seed 2^53", stepDatasetLine + "          dataset_order: random\n          dataset_seed: 9007199254740992\n", config.ErrDatasetSeed},
		{"negative seed", stepDatasetLine + "          dataset_order: random\n          dataset_seed: -1\n", config.ErrDatasetSeed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireRefusal(t, twoStepScenario(datasetStep(tc.lines)), tc.want, "load.calls[0].steps[0]")
		})
	}
}

// A seed that is not a whole number is no seed.
// Ground: contract — a seed the report cannot print exactly is never accepted.
func TestParse_StepDatasetSeedThatIsNotAnIntegerIsAnError(t *testing.T) {
	// The baseline: a whole seed on the same step is accepted, so that a refusal
	// below is of the value and not of the key.
	baseline := twoStepScenario(datasetStep(stepDatasetLine + "          dataset_order: random\n          dataset_seed: 42\n"))
	if _, err := config.Parse([]byte(baseline)); err != nil {
		t.Fatalf("baseline: Parse() = %v, want a whole seed accepted on a step", err)
	}

	for name, value := range map[string]string{
		"a fraction":       "1.5",
		"a string":         `"abc"`,
		"a numeric string": `"42"`,
		"beyond 2^63":      "13835058055282163713",
	} {
		t.Run(name, func(t *testing.T) {
			raw := twoStepScenario(datasetStep(stepDatasetLine + "          dataset_order: random\n          dataset_seed: " + value + "\n"))

			if _, err := config.Parse([]byte(raw)); err == nil {
				t.Errorf("dataset_seed: %s accepted on a step", value)
			}
		})
	}
}

// Ground: contract — spec 2, a step takes its body from one place, as a call does.
func TestParse_StepDataAndDatasetIsAnError(t *testing.T) {
	raw := twoStepScenario(datasetStep(stepDatasetLine + "          data:\n            id: 1\n"))

	requireRefusal(t, raw, config.ErrDataAndDataset, "load.calls[0].steps[0]")
}

// The clash is found before any file is read.
// Ground: contract — as the call's (TestLoad_DatasetAndDataIsAnError).
func TestLoad_StepDataAndDatasetIsFoundBeforeTheFileIsRead(t *testing.T) {
	_, err := loadStepDataset(t, twoStepScenario(datasetStep(stepDatasetLine+"          data:\n            id: 1\n")), nil)

	if !errors.Is(err, config.ErrDataAndDataset) {
		t.Errorf("error = %v, want ErrDataAndDataset, with no file there", err)
	}
}

// Ground: contract — spec 2, the records of a file are not templated (the call's rule).
func TestParse_StepTemplatesTrueWithDatasetIsAnError(t *testing.T) {
	raw := tplScenario(templatedCall, datasetStep(stepTemplatesOn+stepDatasetLine))

	requireRefusal(t, raw, config.ErrTemplatesWithDataset, "load.calls[0].steps[0]")
}

// A step that has a dataset in a call with templates: true, and has no templates
// key of its own, is valid: the call is templated for its own data.
// Ground: contract — spec 2.
func TestParse_StepDatasetInATemplatedCallIsValid(t *testing.T) {
	cfg, err := config.Parse([]byte(tplScenario(templatedCall, datasetStep(stepDatasetLine))))
	if err != nil {
		t.Fatalf("Parse() = %v, want a step with a dataset valid in a templated call", err)
	}

	if step := cfg.Load.Calls[0].Steps[0]; step.Dataset != "steps.jsonl" || step.Templates != nil {
		t.Errorf("Steps[0]: Dataset = %q, Templates = %v, want the path and no templates", step.Dataset, step.Templates)
	}
}

// 7a: a record with {{ in a step of a templated call, the step with no templates
// key of its own, is refused as the braces of a step's data are: the user may
// think they work. The error is the scenario's (ErrScenario, as 3b's), names the
// file and the line, never the record, and offers the way out that exists for a
// dataset: templates: false on the step. It must not offer templates: true on the
// step (stepNeedsKey), which is refused for a step with a dataset
// (ErrTemplatesWithDataset): two refusals that send the user to each other.
// Ground: contract — spec 7a, the 3b rule (2d) for the records of a file, with its
// own words (round 1 of the review).
// Mutation: the refusal in the words of 3b (stepNeedsKey).
func TestLoad_BracesInAStepDatasetOfATemplatedCallAreRefused(t *testing.T) {
	for name, records := range map[string]string{
		"a value":        "{\"id\":1}\n{\"id\":\"{{seq}}-hunter2\"}\n",
		"unknown braces": "{\"id\":1}\n{\"id\":\"{{nope}}-hunter2\"}\n",
		"a lone brace":   "{\"id\":1}\n{\"id\":\"a {{b-hunter2\"}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadStepDataset(t, tplScenario(templatedCall, datasetStep(stepDatasetLine)), map[string]string{stepsFile: records})

			if err == nil {
				t.Fatal("LoadFile() = nil, want a refusal of braces in the records of a step")
			}
			if !errors.Is(err, config.ErrScenario) {
				t.Errorf("error = %v, want config.ErrScenario, as the braces of a step's data", err)
			}
			if want := "load.calls[0].steps[0].dataset: steps.jsonl:2"; !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to say %q", err, want)
			}
			if want := "templates: false on the step"; !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to offer %q", err, want)
			}
			if bad := "templates: true on the step"; strings.Contains(err.Error(), bad) {
				t.Errorf("error = %q offers %q: a step with a dataset refuses it", err, bad)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("error = %q quotes the record", err)
			}
		})
	}
}

// The refusal is of the braces, so the same step with a file without them loads;
// a plain call has no templates and its records are text; templates: false on the
// step is the user's answer that the braces are text.
// Ground: contract — spec 7a, the other half: a call without templates, no refusal.
func TestLoad_BracesInAStepDatasetAreTextWhereTheCallHasNoTemplates(t *testing.T) {
	braces := map[string]string{stepsFile: "{\"id\":1}\n{\"id\":\"{{seq}}\"}\n"}

	t.Run("baseline, no braces in a templated call", func(t *testing.T) {
		step := loadedStep(t, tplScenario(templatedCall, datasetStep(stepDatasetLine)), map[string]string{stepsFile: stepRecords})

		if len(step.Records) != 3 {
			t.Errorf("records = %d, want 3", len(step.Records))
		}
	})
	t.Run("a call without templates", func(t *testing.T) {
		step := loadedStep(t, twoStepScenario(datasetStep(stepDatasetLine)), braces)

		if got, want := stepRaw(step.Records), []string{`{"id":1}`, `{"id":"{{seq}}"}`}; !slices.Equal(got, want) {
			t.Errorf("records = %q, want the braces kept as text, %q", got, want)
		}
	})
	t.Run("a call with templates false", func(t *testing.T) {
		raw := tplScenario("      templates: false\n      data:\n        id: fixed\n", datasetStep(stepDatasetLine))

		if step := loadedStep(t, raw, braces); len(step.Records) != 2 {
			t.Errorf("records = %d, want 2", len(step.Records))
		}
	})
	t.Run("templates false on the step", func(t *testing.T) {
		raw := tplScenario(templatedCall, datasetStep("          templates: false\n"+stepDatasetLine))

		if step := loadedStep(t, raw, braces); len(step.Records) != 2 {
			t.Errorf("records = %d, want 2", len(step.Records))
		}
	})
}

// Ground: contract — the second call's step is named by its own index.
func TestLoad_BracesInAStepDatasetNameTheStepsOwnPath(t *testing.T) {
	raw := scenarioCall(templatedCall) + secondCallWith(templatedCall, "          dataset: steps.jsonl\n")

	_, err := loadStepDataset(t, raw, map[string]string{stepsFile: "{\"id\":\"{{seq}}\"}\n"})

	if want := "load.calls[1].steps[0].dataset: steps.jsonl:1"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want it to say %q", err, want)
	}
	if !errors.Is(err, config.ErrScenario) {
		t.Errorf("error = %v, want config.ErrScenario", err)
	}
}

// dataset_reuse stays a key of the call only.
// Ground: contract — spec 3; guard, green before and after.
func TestScenario_StepDatasetReuseStaysRefused(t *testing.T) {
	raw := twoStepScenario(datasetStep(stepDatasetLine + "          dataset_reuse: false\n"))

	requireRefusal(t, raw, config.ErrScenario, "load.calls[0].steps[0].dataset_reuse", "not supported in a step yet")
}

// A step with no dataset key carries no dataset.
// Ground: contract — a step without the keys is as it was; guard, green before and after.
func TestScenario_StepWithoutDatasetCarriesNone(t *testing.T) {
	cfg, err := config.Parse([]byte(twoStepScenario(twoSteps)))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}

	step := cfg.Load.Calls[0].Steps[0]
	if step.RawDataset != nil || step.Dataset != "" || len(step.Records) != 0 || step.DatasetOrder != nil || step.DatasetSeed != nil {
		t.Errorf("Steps[0] = %+v, want no dataset at all", step)
	}
}
