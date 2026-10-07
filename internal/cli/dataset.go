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
	"fmt"
	"path"
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

	if d.Used == 0 {
		return fmt.Sprintf("  data: none of %d %s from %s used", d.Records, noun(d.Records, "request"), name)
	}

	return fmt.Sprintf("  data: %d of %d %s from %s, each used up to %d %s",
		d.Used, d.Records, noun(d.Records, "request"), name, d.UsedMax, noun(d.UsedMax, "time"))
}

// noun is word, in the plural unless n is 1.
func noun(n int, word string) string {
	if n == 1 {
		return word
	}

	return word + "s"
}
