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

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fuzz targets read untrusted input: a config, a template text, a dataset
// file. The invariant of each is that nothing panics and the answer is an error
// or a valid result. Plain `go test` runs the seeds only; `make fuzz` mutates
// them (30 s per target). A crash it finds is committed under testdata/fuzz
// by go test itself and is a bug to fix, not to hide.

const (
	fuzzSecretName  = "LEETTEST_FUZZ_SECRET"
	fuzzSecretValue = "zq-fuzz-secret-7731"
)

// exampleConfigs are the repository's own configs: the richest valid inputs.
func exampleConfigs(f *testing.F) [][]byte {
	f.Helper()

	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.yaml"))
	if err != nil || len(paths) == 0 {
		f.Fatalf("no example configs found: %v", err)
	}

	var out [][]byte
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			f.Fatalf("read %s: %v", p, err)
		}
		out = append(out, raw)
	}

	return out
}

func FuzzParse(f *testing.F) {
	f.Setenv(fuzzSecretName, fuzzSecretValue)

	for _, raw := range exampleConfigs(f) {
		f.Add(raw)
	}
	f.Add([]byte("app:\n  target:\n    ip: localhost\n    port: 50051\nload:\n  calls:\n    - method: a.B/C\n      rps: 10\n      duration: 1s\n      templates: true\n      data:\n        id: \"{{uuid}}\"\n"))
	f.Add([]byte("app:\n  target:\n    ip: localhost\n    port: 1\n  metadata:\n    authorization: Bearer ${" + fuzzSecretName + "}\n"))
	f.Add([]byte("# a comment and no document\n"))
	f.Add([]byte("a: &x [*x]\n"))

	f.Fuzz(func(t *testing.T, raw []byte) {
		cfg, err := Parse(raw)
		if err != nil {
			if cfg != nil {
				t.Fatalf("Parse returned a config and the error %v", err)
			}
			// A secret read from the environment is never in an error.
			if !strings.Contains(string(raw), fuzzSecretValue) && strings.Contains(err.Error(), fuzzSecretValue) {
				t.Fatalf("the error names the value of %s: %v", fuzzSecretName, err)
			}

			return
		}
		if cfg == nil {
			t.Fatal("Parse returned no config and no error")
		}

		again, err := Parse(raw)
		if err != nil {
			t.Fatalf("the same input parsed once and failed the second time: %v", err)
		}
		if again.SHA256 != cfg.SHA256 {
			t.Fatalf("the same input has two digests: %s and %s", cfg.SHA256, again.SHA256)
		}
	})
}

func FuzzParseTemplate(f *testing.F) {
	for _, s := range []string{
		"", "plain", "{{uuid}}", "{{seq}}", "id-{{uuid}}-{{seq}}!", "{{", "}}", "{{}}", "{{ uuid }}",
		"{{UUID}}", "a{{b", "{{uuid}", "{{{{seq}}}}", "{{seq}}{{seq}}", "é{{seq}}ü",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, text string) {
		parts, bad := ParseTemplate(text)

		if bad != "" {
			if parts != nil {
				t.Fatalf("bad %q and parts %v together", bad, parts)
			}
			if !strings.HasPrefix(bad, "{{") || !strings.Contains(text, bad) {
				t.Fatalf("bad %q is not a fragment of %q that starts with {{", bad, text)
			}

			return
		}

		var back strings.Builder
		for _, p := range parts {
			switch {
			case p.Placeholder == "":
				if p.Text == "" {
					t.Fatalf("an empty literal piece in %v", parts)
				}
				back.WriteString(p.Text)
			case p.Text != "":
				t.Fatalf("a placeholder with a text: %+v", p)
			case p.Placeholder != PlaceholderUUID && p.Placeholder != PlaceholderSeq:
				t.Fatalf("an unknown placeholder %q", p.Placeholder)
			default:
				back.WriteString("{{" + p.Placeholder + "}}")
			}
		}
		if back.String() != text {
			t.Fatalf("the parts give back %q, want %q", back.String(), text)
		}
	})
}

func FuzzExpandEnvRefs(f *testing.F) {
	for _, s := range []string{
		"", "plain", "${A}", "$${A}", "$$${A}", "Bearer ${TOKEN}", "${", "${}", "${1A}", "${A", "$ {A}",
		"${A}${B}", "$${A}${B}", "é${É}", "${A-B}",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, value string) {
		var asked []string

		got := expandEnvRefs(value, func(name string) string {
			asked = append(asked, name)

			return "<" + name + ">"
		})

		for _, name := range asked {
			if !envRef.MatchString("${" + name + "}") {
				t.Fatalf("fn asked for %q, which is not a variable name", name)
			}
		}
		if !strings.Contains(value, "${") && got != value {
			t.Fatalf("%q has no ${ and came out as %q", value, got)
		}
		if names := SubstitutedEnvNames(value); len(names) != len(asked) {
			t.Fatalf("SubstitutedEnvNames %v and expandEnvRefs %v disagree", names, asked)
		}
	})
}

func FuzzReadDataset(f *testing.F) {
	for _, s := range []string{
		"", "\n", "{}\n", "{\"wallet_id\": \"w-42\"}\n{\"wallet_id\": \"w-43\"}\n", "\xEF\xBB\xBF{\"a\":1}\r\n\r\n{\"a\":2}",
		"  \n{\"a\":1}\n   \n", "not json\n", "{\"a\":1}\nzqxcanary\n", "[1,2]\n\"s\"\n3\nnull\n", "{\"a\":",
	} {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "d.jsonl")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		records, sum, err := readDataset(path, "d.jsonl")
		if err != nil {
			if records != nil || sum != "" {
				t.Fatalf("an error and a result together: %v", err)
			}
			// A line of the file is never in an error: it may be a secret.
			const canary = "zqxcanary"
			if strings.Contains(string(data), canary) && strings.Contains(err.Error(), canary) {
				t.Fatalf("the error holds a line of the file: %v", err)
			}

			return
		}

		if len(records) == 0 || len(sum) != 64 {
			t.Fatalf("no error, %d records, digest %q", len(records), sum)
		}
		last := 0
		for _, r := range records {
			if r.Line <= last {
				t.Fatalf("lines not increasing: %d after %d", r.Line, last)
			}
			last = r.Line
			if !json.Valid(r.JSON) {
				t.Fatalf("line %d is not JSON: %q", r.Line, r.JSON)
			}
		}
	})
}
