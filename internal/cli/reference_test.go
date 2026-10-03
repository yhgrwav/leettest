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
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// schemaPaths are the field paths of schema_v1.txt, such as
// "methods[].seconds[].begun".
func schemaPaths(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile("testdata/schema_v1.txt")
	if err != nil {
		t.Fatalf("read golden schema: %v", err)
	}
	var paths []string
	for line := range strings.Lines(string(data)) {
		if path, _, _ := strings.Cut(strings.TrimSpace(line), " "); path != "" {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)

	return paths
}

// answerCategories are the methods' fields shaped as jsonAnswers: what the
// reference's category placeholder stands for.
func answerCategories() []string {
	var names []string
	typ := reflect.TypeFor[jsonMethod]()
	for i := range typ.NumField() {
		if f := typ.Field(i); f.Type == reflect.TypeFor[jsonAnswers]() {
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			names = append(names, name)
		}
	}

	return names
}

var codeSpan = regexp.MustCompile("`([^`]+)`")

// referenceDoc is one language's reference: where its JSON tables start and
// how it writes the two placeholders and the percentile type.
type referenceDoc struct {
	file, heading        string
	category, percentile string
	percentileTypePrefix string
}

// documentedPaths are the paths in the first column of every table under the
// heading, up to the end of the file. The category placeholder runs over
// answerCategories; the percentile placeholder over every path whose type is
// a percentile.
func documentedPaths(t *testing.T, doc referenceDoc) []string {
	t.Helper()

	data, err := os.ReadFile(doc.file)
	if err != nil {
		t.Fatalf("read %s: %v", doc.file, err)
	}
	_, section, ok := strings.Cut(string(data), "\n"+doc.heading+"\n")
	if !ok {
		t.Fatalf("%s has no %q section", doc.file, doc.heading)
	}

	type row struct {
		paths      []string
		percentile bool
	}
	var rows []row
	for line := range strings.Lines(section) {
		cells := strings.Split(line, "|")
		if !strings.HasPrefix(line, "| `") || len(cells) < 4 {
			continue
		}
		var r row
		for _, m := range codeSpan.FindAllStringSubmatch(cells[1], -1) {
			if !strings.Contains(m[1], doc.category) {
				r.paths = append(r.paths, m[1])

				continue
			}
			for _, c := range answerCategories() {
				r.paths = append(r.paths, strings.ReplaceAll(m[1], doc.category, c))
			}
		}
		r.percentile = strings.HasPrefix(strings.TrimSpace(cells[2]), doc.percentileTypePrefix)
		rows = append(rows, r)
	}

	var percentiles, paths []string
	for _, r := range rows {
		if r.percentile {
			percentiles = append(percentiles, r.paths...)
		}
	}
	for _, r := range rows {
		for _, p := range r.paths {
			if !strings.Contains(p, doc.percentile) {
				paths = append(paths, p)

				continue
			}
			for _, q := range percentiles {
				paths = append(paths, strings.ReplaceAll(p, doc.percentile, q))
			}
		}
	}
	slices.Sort(paths)

	return slices.Compact(paths)
}

// Every JSON field is described in the reference, in each language, by its
// full path, and the tables name no path the schema does not have.
func TestReference_DescribesEveryJSONField(t *testing.T) {
	want := schemaPaths(t)
	for _, doc := range []referenceDoc{
		{"../../docs/ru/reference.md", "## Поля JSON", "<категория>", "<перцентиль>", "перцентиль"},
		{"../../docs/en/reference.md", "## JSON fields", "<category>", "<percentile>", "percentile"},
	} {
		got := documentedPaths(t, doc)
		var missing, extra []string
		for _, p := range want {
			if !slices.Contains(got, p) {
				missing = append(missing, p)
			}
		}
		for _, p := range got {
			if !slices.Contains(want, p) {
				extra = append(extra, p)
			}
		}
		if len(missing) > 0 || len(extra) > 0 {
			t.Errorf("%s, %q: missing %v, not in the schema %v", doc.file, doc.heading, missing, extra)
		}
	}
}
