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

package cli

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// datasetLine is what the text report says of a call's dataset, indented as a
// sub-line of its method. It states how much of the file went out and how often
// the most used request did, never that the target saw that many distinct
// requests: a request can repeat in the file itself.
func datasetLine(d *engine.DatasetReport) string {
	// The path as the config wrote it, whichever separator the host that wrote
	// it used.
	name := path.Base(strings.ReplaceAll(d.File, `\`, "/"))

	rule := ""
	if d.Once {
		rule = ", each at most once"
	}

	order := ""
	if d.Random {
		order = fmt.Sprintf(", random order, seed %d", d.Seed)
	}

	if d.Used == 0 {
		return fmt.Sprintf("  data: none of %d %s from %s used%s%s", d.Records, noun(d.Records, "request"), name, rule, order)
	}

	if d.Once {
		return fmt.Sprintf("  data: %d of %d %s from %s%s%s", d.Used, d.Records, noun(d.Records, "request"), name, rule, order)
	}

	return fmt.Sprintf("  data: %d of %d %s from %s, each used up to %d %s%s",
		d.Used, d.Records, noun(d.Records, "request"), name, d.UsedMax, noun(d.UsedMax, "time"), order)
}

// noun is word, in the plural unless n is 1.
func noun(n int, word string) string {
	if n == 1 {
		return word
	}

	return word + "s"
}

// hasRamp says whether a plan has a stage whose rate moves.
func hasRamp(stages []engine.Stage) bool {
	return slices.ContainsFunc(stages, func(s engine.Stage) bool { return s.StartRPS != s.TargetRPS })
}

// CheckDatasetsFit refuses a run whose plan hands out more requests than a
// dataset_reuse: false file has records, before anything is sent. The count is
// the engine's own, warm-up and ramp included: they use records. A call that
// reuses its file is not checked.
func CheckDatasetsFit(calls []engine.Call) error {
	var errs []error

	for i := range calls {
		call := &calls[i]
		if call.Dataset == nil || !call.Dataset.Once {
			continue
		}

		need := engine.ScheduledAfter(call.Stages, 0)
		if have := len(call.Payloads); have < need {
			included := "warm-up"
			if hasRamp(call.Stages) {
				included = "ramp and warm-up"
			}
			errs = append(errs, fmt.Errorf("call %q: dataset %s has %d records, the run hands out %d (%s included); "+
				"dataset_reuse: false sends each at most once",
				displayMethod(call.Method), path.Base(strings.ReplaceAll(call.Dataset.File, `\`, "/")), have, need, included))
		}
	}

	return errors.Join(errs...)
}
