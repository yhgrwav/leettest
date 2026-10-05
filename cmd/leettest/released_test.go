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

package main

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The release PR rewrites the fixture from the code: go test ./cmd/leettest -run Released -update.
var update = flag.Bool("update", false, "rewrite testdata/released.txt from the code")

const releasedFile = "testdata/released.txt"

// current is what the code has now: "flag <name>" and "json <path>" lines.
func current(t *testing.T) []string {
	t.Helper()

	var lines []string
	for _, f := range flagNames() {
		lines = append(lines, "flag "+f)
	}
	schema, err := os.ReadFile("../../internal/cli/testdata/schema_v1.txt")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(schema)) {
		if path, _, _ := strings.Cut(strings.TrimSpace(line), " "); path != "" {
			lines = append(lines, "json "+path)
		}
	}
	slices.Sort(lines)

	return lines
}

func released(t *testing.T) []string {
	t.Helper()

	if *update {
		if err := os.WriteFile(releasedFile, []byte(strings.Join(current(t), "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(releasedFile)
	if err != nil {
		t.Fatalf("%v: the release PR writes it with -update", err)
	}
	var lines []string
	for line := range strings.Lines(string(raw)) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

// unreleasedMark is how each language marks a row the last release does not have.
var unreleasedMark = map[string]string{
	"ru": "не выпущено", "en": "not released", "de": "nicht veröffentlicht", "zh-CN": "未发布",
}

// jsonHeading opens the JSON field tables in each language; rows before it
// name config fields, not JSON paths.
var jsonHeading = map[string]string{
	"ru": "## Поля JSON", "en": "## JSON fields", "de": "## JSON-Felder", "zh-CN": "## JSON 字段",
}

var (
	rowName = regexp.MustCompile("^\\| `([^`<]+)`")
	flagRow = regexp.MustCompile("^-[a-z-]+$")
)

// The reference describes the code on main, which runs ahead of the release a
// user installs. A flag or a JSON field the last release does not have carries
// the mark, so a reader of main is not promised what @latest lacks; a mark on
// something released is wrong the other way.
func TestReleased_UnreleasedRowsAreMarked(t *testing.T) {
	have := released(t)
	docs, err := filepath.Glob("../../docs/*/reference.md")
	if err != nil || len(docs) == 0 {
		t.Fatalf("no references: %v", err)
	}
	for _, doc := range docs {
		lang := filepath.Base(filepath.Dir(doc))
		mark, ok := unreleasedMark[lang]
		if !ok {
			t.Errorf("%s: no unreleased mark for language %q", doc, lang)

			continue
		}
		raw, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		inJSON := false
		for line := range strings.Lines(string(raw)) {
			if strings.HasPrefix(line, "## ") {
				inJSON = strings.TrimSpace(line) == jsonHeading[lang]
			}
			m := rowName.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			var key string
			switch {
			case flagRow.MatchString(m[1]):
				key = "flag " + strings.TrimPrefix(m[1], "-")
			case inJSON:
				key = "json " + m[1]
			default:
				continue
			}
			isReleased, marked := slices.Contains(have, key), strings.Contains(line, mark)
			switch {
			case !isReleased && !marked:
				t.Errorf("%s: %s is not in the last release and is not marked %q", doc, m[1], mark)
			case isReleased && marked:
				t.Errorf("%s: %s is released and still marked %q", doc, m[1], mark)
			}
		}
	}
}

// The fixture names only what the code has: a release PR that forgets -update
// after removing a flag fails here.
func TestReleased_FixtureIsTheCode(t *testing.T) {
	have, now := released(t), current(t)
	for _, line := range have {
		if !slices.Contains(now, line) {
			t.Errorf("%s says %q was released, the code has no such thing", releasedFile, line)
		}
	}
	if len(flagNames()) == 0 {
		t.Error("flagNames is empty: the CLI has flags")
	}
}
