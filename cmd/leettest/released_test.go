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
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
)

// The release branch's first PR after the cut writes the fixture from the
// code: go test ./cmd/leettest -run Released -update. Never on main: main's
// fixture is a copy from the tag (git show vX.Y.Z:cmd/leettest/testdata/released.txt),
// or main's unreleased flags would read as released.
var update = flag.Bool("update", false, "rewrite testdata/released.txt from the code (release branch only)")

const releasedFile = "testdata/released.txt"

var durationType = reflect.TypeFor[time.Duration]()

// configPaths are the config file's keys by full path, from the yaml tags.
func configPaths(typ reflect.Type, prefix string) []string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch {
	case typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Struct:
		return configPaths(typ.Elem(), prefix+"[]")
	case typ.Kind() != reflect.Struct || typ == durationType:
		return []string{prefix}
	}
	var paths []string
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		if prefix != "" {
			name = prefix + "." + name
		}
		paths = append(paths, configPaths(typ.Field(i).Type, name)...)
	}

	return paths
}

// current is what the code has now: "flag <name>", "json <path>" and
// "config <path>" lines.
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
	for _, p := range configPaths(reflect.TypeFor[config.MasterConfig](), "") {
		lines = append(lines, "config "+p)
	}
	slices.Sort(lines)

	return slices.Compact(lines)
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
		t.Fatalf("%v: the release branch writes it with -update", err)
	}
	var lines []string
	for line := range strings.Lines(string(raw)) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

// unreleasedMark is how each language marks what the last release does not have.
var unreleasedMark = map[string]string{
	"ru": "не выпущено", "en": "not released", "de": "nicht veröffentlicht", "zh-CN": "未发布",
}

// jsonHeading opens the JSON field tables in each language.
var jsonHeading = map[string]string{
	"ru": "## Поля JSON", "en": "## JSON fields", "de": "## JSON-Felder", "zh-CN": "## JSON 字段",
}

var (
	codeSpan = regexp.MustCompile("`([^`]+)`")
	flagName = regexp.MustCompile(`^-[a-z][a-z-]*$`)
)

// isConfig says whether line names the config key path or a parent of keys
// (app.target).
func isConfig(line, path string) bool {
	return line == "config "+path || strings.HasPrefix(line, "config "+path+".") ||
		strings.HasPrefix(line, "config "+path+"[]")
}

// kind names what a backticked name is in the code: "flag x", "json x",
// "config x", or "" for anything else. A JSON path counts only where JSON
// paths are meant (inJSON).
func kind(name string, now []string, inJSON bool) string {
	switch {
	case flagName.MatchString(name):
		if slices.Contains(now, "flag "+name[1:]) {
			return "flag " + name[1:]
		}
	case inJSON && slices.Contains(now, "json "+name):
		return "json " + name
	case slices.ContainsFunc(now, func(l string) bool { return isConfig(l, name) }):
		return "config " + name
	}

	return ""
}

// releasedKey says whether the fixture holds key; a config parent is released
// when any key under it is.
func releasedKey(key string, have []string) bool {
	path, ok := strings.CutPrefix(key, "config ")
	if !ok {
		return slices.Contains(have, key)
	}

	return slices.ContainsFunc(have, func(l string) bool { return isConfig(l, path) })
}

// The reference describes the code on main, which runs ahead of the release a
// user installs. Every flag, JSON field and config key the last release lacks
// is marked in its row, so a reader of main is not promised what @latest
// rejects; a mark on something released is wrong the other way. Every name
// in a row's first cell is checked, and a row mixing released and unreleased
// names must be split.
func TestReleased_UnreleasedRowsAreMarked(t *testing.T) {
	have, now := released(t), current(t)
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
		seen := map[string]int{}
		inJSON := false
		for line := range strings.Lines(string(raw)) {
			if strings.HasPrefix(line, "## ") {
				inJSON = strings.TrimSpace(line) == jsonHeading[lang]
			}
			cells := strings.Split(line, "|")
			if !strings.HasPrefix(line, "| `") || len(cells) < 3 {
				continue
			}
			var isNew, isOld []string
			for _, m := range codeSpan.FindAllStringSubmatch(cells[1], -1) {
				key := kind(m[1], now, inJSON)
				if key == "" {
					continue
				}
				seen[strings.Fields(key)[0]]++
				if releasedKey(key, have) {
					isOld = append(isOld, m[1])
				} else {
					isNew = append(isNew, m[1])
				}
			}
			marked := strings.Contains(line, mark)
			switch {
			case len(isNew) > 0 && len(isOld) > 0:
				t.Errorf("%s: %v released and %v not, in one row: split it", doc, isOld, isNew)
			case len(isNew) > 0 && !marked:
				t.Errorf("%s: %v not in the last release and not marked %q", doc, isNew, mark)
			case len(isOld) > 0 && marked:
				t.Errorf("%s: %v released and still marked %q", doc, isOld, mark)
			}
		}
		// A renamed heading or table must not turn the check off silently.
		for _, k := range []string{"flag", "json", "config"} {
			if seen[k] == 0 {
				t.Errorf("%s: no %s row recognised", doc, k)
			}
		}
	}
}

// In prose — the READMEs and the docs pages — a name the code has but the
// last release lacks stands on a line with the mark. Names the code does not
// have (-race, -count) are not this check's business.
func TestReleased_ProseMarksUnreleasedNames(t *testing.T) {
	have, now := released(t), current(t)
	pages, err := filepath.Glob("../../docs/*/*.md")
	if err != nil {
		t.Fatal(err)
	}
	readmes, err := filepath.Glob("../../README*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range append(readmes, pages...) {
		if filepath.Base(f) == "reference.md" {
			continue
		}
		mark, ok := unreleasedMark[filepath.Base(filepath.Dir(f))]
		if !ok {
			mark = unreleasedMark["ru"]
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.Lines(string(raw)) {
			for _, m := range codeSpan.FindAllStringSubmatch(line, -1) {
				words := strings.Fields(m[1])
				if len(words) == 0 {
					continue
				}
				key := kind(words[0], now, true)
				if key != "" && !releasedKey(key, have) && !strings.Contains(line, mark) {
					t.Errorf("%s: %q is not in the last release and its line is not marked %q", f, words[0], mark)
				}
			}
		}
	}
}

// flagNames is the CLI's own flag set: every flag -help lists, nothing else.
func TestFlagNames_AreTheHelp(t *testing.T) {
	var stderr bytes.Buffer
	_ = run(t.Context(), nil, nil, []string{"-h"}, &bytes.Buffer{}, &stderr)

	var help []string
	for _, m := range regexp.MustCompile(`(?m)^\s+-([a-z][a-z-]*)`).FindAllStringSubmatch(stderr.String(), -1) {
		help = append(help, m[1])
	}
	slices.Sort(help)
	got := slices.Sorted(slices.Values(flagNames()))
	if len(help) == 0 || !slices.Equal(got, help) {
		t.Errorf("flagNames = %q, -help lists %q", got, help)
	}
}
