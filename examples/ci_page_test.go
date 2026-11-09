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

package examples_test

import (
	"os"
	"strings"
	"testing"
)

const (
	workflowStart = "# example workflow: start"
	workflowEnd   = "# example workflow: end"
)

func readUnixText(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// markedWorkflow is the text from the start marker line to the end marker line,
// both included: the part of the CI page's workflow that the "example workflow"
// job of ci.yml runs as written.
func markedWorkflow(t *testing.T, where, text string) string {
	t.Helper()

	var found [][]string
	var region []string
	open := false
	for line := range strings.SplitSeq(text, "\n") {
		switch trimmed := strings.TrimSpace(line); {
		case trimmed == workflowStart && !open:
			open = true
			region = []string{line}
		case trimmed == workflowEnd && open:
			found = append(found, append(region, line))
			open = false
		case open:
			region = append(region, line)
		}
	}
	if len(found) != 1 || open {
		t.Fatalf("%s has %d regions between the example workflow markers, want 1", where, len(found))
	}

	return strings.Join(found[0], "\n")
}

// The CI page shows a workflow; its marked part is the "example workflow" job
// of ci.yml, so the page's text is the text CI runs. The page's config block is
// the example file the job copies.
func TestCIPage_WorkflowIsTheCIJob(t *testing.T) {
	job := markedWorkflow(t, "ci.yml", readUnixText(t, "../.github/workflows/ci.yml"))

	for _, c := range []struct{ page, example string }{
		{"../docs/en/ci.md", "ci.yaml"},
		{"../docs/ru/ci.md", "ru/ci.yaml"},
	} {
		page := readUnixText(t, c.page)

		if got := markedWorkflow(t, c.page, page); got != job {
			t.Errorf("%s: the marked workflow differs from the ci.yml job:\n%s\nci.yml:\n%s", c.page, got, job)
		}

		config := readUnixText(t, c.example)
		if !strings.Contains(page, "```yaml\n"+config+"```") {
			t.Errorf("%s has no yaml block equal to %s", c.page, c.example)
		}
	}
}
