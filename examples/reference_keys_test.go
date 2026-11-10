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
	"bufio"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

const referencePage = "../docs/en/reference.md"

// docKey is a config key the reference page documents, and the page line that says so.
type docKey struct {
	path string
	line int
}

// referenceOnlyKeys are documented keys that no runnable example can set. Each has a reason.
// Add an entry only with one; a key that gets an example, or leaves the page, must leave this map.
var referenceOnlyKeys = map[string]string{}

// undocumentedExampleKeys are keys the examples set that no row of the reference tables names.
// Each has a reason. A key that gets a row must leave this map.
var undocumentedExampleKeys = map[string]string{
	"app.target.ip":          "the app.target row names `ip` and `port` inside its text, not as rows of their own",
	"app.target.port":        "the app.target row names `ip` and `port` inside its text, not as rows of their own",
	"load.breakpoint.from":   "documented in the README's breaking-point section, the reference has no table for it",
	"load.breakpoint.to":     "documented in the README's breaking-point section, the reference has no table for it",
	"load.breakpoint.factor": "documented in the README's breaking-point section, the reference has no table for it",
	"load.breakpoint.step":   "documented in the README's breaking-point section, the reference has no table for it",
	"load.breakpoint.hold":   "documented in the README's breaking-point section, the reference has no table for it",
	"load.breakpoint.settle": "documented in the README's breaking-point section, the reference has no table for it",
	"load.breakpoint.p99_limit": "documented in the README's breaking-point section, " +
		"the reference has no table for it",
}

// openKeys take keys of the user's own naming below them: a label, a header, a field of the message.
var openKeys = []string{"labels", "app.metadata", "load.calls[].data"}

var backtickSpan = regexp.MustCompile("`([^`]+)`")

// referenceKeys reads the key tables of the reference page: the config table under "## Config" and
// the first table under "## Pass limits", whose keys are relative to a call's `pass` (or, with
// `breakpoint.`, to `load.`).
func referenceKeys(t *testing.T) []docKey {
	t.Helper()

	f, err := os.Open(referencePage)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var (
		keys       []docKey
		section    string
		inTable    bool
		lineNumber int
	)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		lineNumber++
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			section, inTable = line, false

			continue
		}
		if section != "## Config" && section != "## Pass limits" {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			// The Pass limits section holds a second table after the key one: only the first counts.
			if inTable && section == "## Pass limits" {
				section = ""
			}
			inTable = false

			continue
		}
		inTable = true

		cell, _, _ := strings.Cut(strings.TrimPrefix(line, "|"), "|")
		cell = strings.TrimSpace(cell)
		if !strings.HasPrefix(cell, "`") {
			continue
		}
		for _, m := range backtickSpan.FindAllStringSubmatch(cell, -1) {
			path := m[1]
			if section == "## Pass limits" {
				if rest, ok := strings.CutPrefix(path, "breakpoint."); ok {
					path = "load.breakpoint." + rest
				} else {
					path = "load.calls[].pass." + path
				}
			}
			keys = append(keys, docKey{path: path, line: lineNumber})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	return keys
}

// examplePaths are the dotted paths the parsed YAML sets, list items as `[]`: every scalar, and a
// list of scalars or an empty map as one path.
func examplePaths(node any, prefix string, out map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		if len(v) == 0 && prefix != "" {
			out[prefix] = true

			return
		}
		for key, child := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			examplePaths(child, path, out)
		}
	case []any:
		hasMap := false
		for _, item := range v {
			if _, ok := item.(map[string]any); ok {
				hasMap = true
				examplePaths(item, prefix+"[]", out)
			}
		}
		if !hasMap {
			out[prefix] = true
		}
	default:
		out[prefix] = true
	}
}

// within says whether path is key itself or lies below it.
func within(path, key string) bool {
	return path == key || strings.HasPrefix(path, key+".")
}

// exampleKeyPaths maps every path some example sets to the examples that set it.
func exampleKeyPaths(t *testing.T) map[string][]string {
	t.Helper()

	byPath := map[string][]string{}
	for _, name := range examples(t) {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		paths := map[string]bool{}
		examplePaths(doc, "", paths)
		for path := range paths {
			byPath[path] = append(byPath[path], name)
		}
	}

	return byPath
}

// Every key the reference documents is set in some example the suite validates, and every key the
// examples set is documented: an example is the proof that a documented key works.
func TestReferenceKeys_ExamplesAndPageAgree(t *testing.T) {
	documented := referenceKeys(t)
	if len(documented) < 30 {
		t.Fatalf("read %d keys from %s: the table layout changed, the parser must follow", len(documented), referencePage)
	}
	byPath := exampleKeyPaths(t)

	setSomewhere := func(key string) bool {
		for path := range byPath {
			if within(path, key) {
				return true
			}
		}

		return false
	}

	documentedAs := func(path string) bool {
		for _, d := range documented {
			if path == d.path {
				return true
			}
			if slices.Contains(openKeys, d.path) && within(path, d.path) {
				return true
			}
		}

		return false
	}

	var missing []docKey
	for _, d := range documented {
		reason, allowed := referenceOnlyKeys[d.path]
		switch {
		case allowed && reason == "":
			t.Errorf("referenceOnlyKeys[%q] has no reason", d.path)
		case allowed && setSomewhere(d.path):
			t.Errorf("referenceOnlyKeys[%q] is set in an example now: remove the entry", d.path)
		case !allowed && !setSomewhere(d.path):
			missing = append(missing, d)
		}
	}
	for _, d := range missing {
		t.Errorf("%s:%d documents %s, no example in examples/ sets it", referencePage, d.line, d.path)
	}
	for key := range referenceOnlyKeys {
		if !slices.ContainsFunc(documented, func(d docKey) bool { return d.path == key }) {
			t.Errorf("referenceOnlyKeys[%q] is not a documented key: remove the entry", key)
		}
	}

	var undocumented []string
	for path := range byPath {
		if documentedAs(path) {
			if _, allowed := undocumentedExampleKeys[path]; allowed {
				t.Errorf("undocumentedExampleKeys[%q] is documented now: remove the entry", path)
			}

			continue
		}
		reason, allowed := undocumentedExampleKeys[path]
		switch {
		case allowed && reason == "":
			t.Errorf("undocumentedExampleKeys[%q] has no reason", path)
		case !allowed:
			undocumented = append(undocumented, path)
		}
	}
	sort.Strings(undocumented)
	for _, path := range undocumented {
		t.Errorf("%s sets %s, which no row of %s documents (a typo, or the page lacks a row)",
			strings.Join(byPath[path], ", "), path, referencePage)
	}
	for key := range undocumentedExampleKeys {
		if _, ok := byPath[key]; !ok {
			t.Errorf("undocumentedExampleKeys[%q] is set in no example: remove the entry", key)
		}
	}
}
