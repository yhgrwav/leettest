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
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// schemaNames are the field names of schema_v1.txt, the last segment of each
// path: "methods[].seconds[].begun" is "begun".
func schemaNames(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile("testdata/schema_v1.txt")
	if err != nil {
		t.Fatalf("read golden schema: %v", err)
	}
	var names []string
	for line := range strings.Lines(string(data)) {
		path, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		if path != "" {
			names = append(names, leafName(path))
		}
	}
	slices.Sort(names)

	return slices.Compact(names)
}

func leafName(path string) string {
	path = strings.ReplaceAll(path, "[]", "")

	return path[strings.LastIndex(path, ".")+1:]
}

var codeSpan = regexp.MustCompile("`([^`]+)`")

// tableNames are the names in the first column of every table under heading
// in a reference file, up to the end of the file.
func tableNames(t *testing.T, file, heading string) []string {
	t.Helper()

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	_, section, ok := strings.Cut(string(data), "\n"+heading+"\n")
	if !ok {
		t.Fatalf("%s has no %q section", file, heading)
	}
	var names []string
	for line := range strings.Lines(section) {
		cells := strings.Split(line, "|")
		if !strings.HasPrefix(line, "| `") || len(cells) < 3 {
			continue
		}
		for _, m := range codeSpan.FindAllStringSubmatch(cells[1], -1) {
			names = append(names, leafName(m[1]))
		}
	}
	slices.Sort(names)

	return slices.Compact(names)
}

// Every JSON field is described in the reference, in each language, and the
// tables name no field the schema does not have.
func TestReference_DescribesEveryJSONField(t *testing.T) {
	want := schemaNames(t)
	for _, doc := range []struct{ file, heading string }{
		{"../../docs/ru/reference.md", "## Поля JSON"},
		{"../../docs/en/reference.md", "## JSON fields"},
	} {
		got := tableNames(t, doc.file, doc.heading)
		var missing, extra []string
		for _, name := range want {
			if !slices.Contains(got, name) {
				missing = append(missing, name)
			}
		}
		for _, name := range got {
			if !slices.Contains(want, name) {
				extra = append(extra, name)
			}
		}
		if len(missing) > 0 || len(extra) > 0 {
			t.Errorf("%s, %q: missing %v, not in the schema %v", doc.file, doc.heading, missing, extra)
		}
	}
}
