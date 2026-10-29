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
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// templated is the lines of a call with templates on and data as given: each
// line of data is already indented by eight spaces.
func templated(data string) string {
	return "      templates: true\n      data:\n" + data
}

func parseTemplated(t *testing.T, lines string) (*config.MasterConfig, error) {
	t.Helper()

	return config.Parse([]byte(datasetYAML(lines)))
}

// Ground: contract — templates is config API: what is written is what the call
// carries, left out is nil (off), and the placeholders come out sorted.
func TestParse_TemplatesKey(t *testing.T) {
	for _, tc := range []struct {
		name       string
		lines      string
		wantSet    *bool
		wantOn     bool
		wantHolder []string
	}{
		{"left out", "      data:\n        id: \"{{uuid}}\"\n", nil, false, nil},
		{"false", "      templates: false\n      data:\n        id: \"{{uuid}}\"\n", bptr(false), false, nil},
		{"true with uuid", templated("        id: \"{{uuid}}\"\n"), bptr(true), true, []string{"uuid"}},
		{"true with seq", templated("        id: \"{{seq}}\"\n"), bptr(true), true, []string{"seq"}},
		{"true with both, sorted", templated("        id: \"{{uuid}}\"\n        n: \"{{seq}}\"\n"), bptr(true), true, []string{"seq", "uuid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseTemplated(t, tc.lines)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			call := cfg.Load.Calls[0]
			switch {
			case tc.wantSet == nil && call.Templates != nil:
				t.Errorf("Templates = %v, want nil: left out stays nil", *call.Templates)
			case tc.wantSet != nil && (call.Templates == nil || *call.Templates != *tc.wantSet):
				t.Errorf("Templates = %v, want %v", call.Templates, *tc.wantSet)
			}
			if got := call.TemplatesOn(); got != tc.wantOn {
				t.Errorf("TemplatesOn = %v, want %v", got, tc.wantOn)
			}
			if got := call.TemplatePlaceholders(); !slices.Equal(got, tc.wantHolder) {
				t.Errorf("TemplatePlaceholders = %v, want %v", got, tc.wantHolder)
			}
		})
	}
}

// A placeholder named twice, or in several places, is listed once; the walk
// goes into nested messages, lists and map values.
// Ground: contract — the report and the JSON carry this list.
func TestParse_TemplatePlaceholdersAreFoundAtAnyDepthAndListedOnce(t *testing.T) {
	cfg, err := parseTemplated(t, templated(`        note: "a {{uuid}} b {{uuid}}"
        child:
          name: "x"
          inner:
            tags: ["t", "{{seq}}"]
        items:
          - name: "n"
          - name: "{{uuid}}"
        attrs:
          k: "v-{{seq}}"
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got, want := cfg.Load.Calls[0].TemplatePlaceholders(), []string{"seq", "uuid"}; !slices.Equal(got, want) {
		t.Errorf("TemplatePlaceholders = %v, want %v", got, want)
	}
}

// With templates off a `{{` is text like any other: the body is the one the
// config always made, and nothing in it is checked. A literal `{{` is sent by
// leaving templates off.
// Ground: contract — "off is absent": an existing config stays byte-equal.
func TestParse_TemplatesOffTakesAnyBraces(t *testing.T) {
	for _, text := range []string{"{{uuid}}", "{{nope}}", "{{ seq }}", "{{", "a {{b", "{{uuid"} {
		for _, off := range []string{"", "      templates: false\n"} {
			t.Run(strings.TrimSpace(off)+text, func(t *testing.T) {
				cfg, err := parseTemplated(t, off+"      data:\n        note: \""+text+"\"\n")
				if err != nil {
					t.Fatalf("parse: %v", err)
				}

				call := cfg.Load.Calls[0]
				if call.TemplatesOn() || call.TemplatePlaceholders() != nil {
					t.Errorf("on = %v, placeholders = %v: templates are off", call.TemplatesOn(), call.TemplatePlaceholders())
				}
				if want := map[string]any{"note": text}; !reflect.DeepEqual(call.Data, want) {
					t.Errorf("Data = %#v, want %#v: the text as written", call.Data, want)
				}
			})
		}
	}
}

// With templates on, a `{{` is the start of a placeholder and only {{uuid}} and
// {{seq}} are placeholders: no spaces inside, the case exact, no escape. The
// error names the method, the field by its path, the fragment found, what is
// allowed, and how to send a literal `{{`.
// Ground: contract — decisions: no mini-language to learn, no escape; a typo is
// an error and not a body that goes out as text.
func TestParse_TemplatesUnknownBracesAreAnError(t *testing.T) {
	for name, tc := range map[string]struct{ text, fragment string }{
		"a name":              {"{{nope}}", "{{nope}}"},
		"spaces inside":       {"{{ uuid }}", "{{ uuid }}"},
		"space before":        {"{{ seq}}", "{{ seq}}"},
		"space after":         {"{{uuid }}", "{{uuid }}"},
		"upper case":          {"{{UUID}}", "{{UUID}}"},
		"capital first":       {"{{Seq}}", "{{Seq}}"},
		"empty":               {"{{}}", "{{}}"},
		"an escape attempt":   {`{{\"{{\"}}`, "{{"},
		"unterminated":        {"{{uuid", "{{uuid"},
		"a lone open pair":    {"{{", "{{"},
		"inside a text":       {"order-{{seq}}-{{id}}", "{{id}}"},
		"three braces":        {"{{{uuid}}}", "{{{uuid}}"},
		"one closing brace":   {"{{seq}", "{{seq}"},
		"a placeholder first": {"{{uuid}} and {{nope}}", "{{nope}}"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTemplated(t, templated("        note: \""+tc.text+"\"\n"))

			if !errors.Is(err, config.ErrTemplatesUnknown) {
				t.Fatalf("error = %v, want %v", err, config.ErrTemplatesUnknown)
			}
			msg := err.Error()
			for _, want := range []string{datasetMethod, "data.note", tc.fragment, "{{uuid}}", "{{seq}}", "literal"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q lacks %q", msg, want)
				}
			}
		})
	}
}

// Ground: contract — a lone closing pair and single braces are text.
func TestParse_TemplatesTakeTextAroundPlaceholders(t *testing.T) {
	for _, text := range []string{
		"{{uuid}}", "{{seq}}", "order-{{seq}}", "{{uuid}}-{{seq}}", "a }} b {{seq}}", "}}{{seq}}",
		"{seq}", "{uuid}", "a { b } c {{seq}}", "{{seq}}}}", "$ {{seq}}",
	} {
		t.Run(text, func(t *testing.T) {
			if _, err := parseTemplated(t, templated("        note: \""+text+"\"\n")); err != nil {
				t.Errorf("%q refused: %v", text, err)
			}
		})
	}
}

// The path is where the user can find the field: keys with a dot, list items
// by their index from 0, map values by their key.
// Ground: contract — the text is ours; the field must be findable in the config.
func TestParse_TemplatesErrorNamesThePathOfTheField(t *testing.T) {
	for name, tc := range map[string]struct{ data, path string }{
		"top level": {"        note: \"{{x}}\"\n", "data.note"},
		"nested":    {"        child:\n          name: \"{{x}}\"\n", "data.child.name"},
		"deeper":    {"        a:\n          b:\n            c: \"{{x}}\"\n", "data.a.b.c"},
		"a list":    {"        tags: [\"ok\", \"{{x}}\"]\n", "data.tags[1]"},
		"a list of messages": {
			"        items:\n          - name: \"ok\"\n          - name: \"{{x}}\"\n", "data.items[1].name",
		},
		"a map value": {"        attrs:\n          k: \"{{x}}\"\n", "data.attrs.k"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTemplated(t, templated(tc.data))

			if !errors.Is(err, config.ErrTemplatesUnknown) {
				t.Fatalf("error = %v, want %v", err, config.ErrTemplatesUnknown)
			}
			if !strings.Contains(err.Error(), tc.path) {
				t.Errorf("error %q lacks the path %q", err, tc.path)
			}
		})
	}
}

// Every field is named, not only the first: one run of the tool finds them all.
// Ground: contract — config errors are joined, as everywhere in Validate.
func TestParse_TemplatesNamesEveryBadField(t *testing.T) {
	_, err := parseTemplated(t, templated("        a: \"{{x}}\"\n        b: \"{{ y }}\"\n"))

	if err == nil {
		t.Fatal("accepted")
	}
	for _, want := range []string{"data.a", "{{x}}", "data.b", "{{ y }}"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

// A key is a field name or a map key: neither changes per request, and a map
// whose key changes is a map of a thousand entries nobody meant. An error, with
// the path of the object that holds the key.
// Ground: contract — reviewer's verdict: a placeholder in a map key is an error.
func TestParse_TemplatesInAKeyIsAnError(t *testing.T) {
	for name, tc := range map[string]struct{ data, path string }{
		"a map key":   {"        attrs:\n          \"{{uuid}}\": v\n", "data.attrs"},
		"a field key": {"        \"{{seq}}\": 1\n", "data"},
		"a text key":  {"        attrs:\n          \"id-{{seq}}\": v\n", "data.attrs"},
		"an unknown":  {"        attrs:\n          \"{{x}}\": v\n", "data.attrs"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTemplated(t, templated("        ok: \"{{uuid}}\"\n"+tc.data))

			if !errors.Is(err, config.ErrTemplatesKey) {
				t.Fatalf("error = %v, want %v", err, config.ErrTemplatesKey)
			}
			for _, want := range []string{datasetMethod, tc.path} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
		})
	}
}

// A switch with no effect is a surprise: the user believes the body changes.
// The text says what is missing.
// Ground: contract — brief point 2.
func TestParse_TemplatesTrueWithoutPlaceholdersIsAnError(t *testing.T) {
	for name, lines := range map[string]string{
		"data without braces": templated("        id: \"fixed\"\n        n: 5\n"),
		"no data":             "      templates: true\n",
		"empty data":          "      templates: true\n      data: {}\n",
		"a lone brace":        templated("        id: \"{ seq }\"\n"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTemplated(t, lines)

			if !errors.Is(err, config.ErrTemplatesNoPlaceholders) {
				t.Fatalf("error = %v, want %v", err, config.ErrTemplatesNoPlaceholders)
			}
			for _, want := range []string{datasetMethod, "templates: true", "placeholder"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
		})
	}
}

// templates: false asks for nothing, so it needs no placeholder.
// Ground: contract — "off" is the same as left out.
func TestParse_TemplatesFalseNeedsNoPlaceholders(t *testing.T) {
	if _, err := parseTemplated(t, "      templates: false\n      data:\n        id: x\n"); err != nil {
		t.Errorf("templates: false with plain data refused: %v", err)
	}
}

// Unit 1 templates data only; a record of a dataset is not templated.
// Ground: contract — brief point 2: the error says what works.
func TestParse_TemplatesWithADatasetIsAnError(t *testing.T) {
	_, err := parseTemplated(t, "      templates: true\n      dataset: users.jsonl\n")

	if !errors.Is(err, config.ErrTemplatesWithDataset) {
		t.Fatalf("error = %v, want %v", err, config.ErrTemplatesWithDataset)
	}
	for _, want := range []string{datasetMethod, "templates work with `data` only"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if errors.Is(err, config.ErrTemplatesNoPlaceholders) {
		t.Errorf("error %q also says there are no placeholders: the file is not read here", err)
	}
}

// Ground: contract — templates: false beside a dataset is the default spelled out.
func TestParse_TemplatesFalseWithADatasetIsFine(t *testing.T) {
	if _, err := parseTemplated(t, "      templates: false\n      dataset: users.jsonl\n"); err != nil {
		t.Errorf("refused: %v", err)
	}
}

// The key written with no value is lost by the decoder as a key left out, and
// the call would send its braces as text without a word: a config error with
// the line.
// Ground: contract — as dataset_reuse with no value.
func TestParse_TemplatesWithNoValueIsAnError(t *testing.T) {
	for name, value := range map[string]string{
		"nothing":      "",
		"a tilde":      " ~",
		"null":         " null",
		"an anchor":    " &a ~",
		"a null tag":   " !!null ~",
		"an alias":     " *a",
		"a tagged one": " !!null *a",
	} {
		t.Run(name, func(t *testing.T) {
			lines := "      templates:" + value + "\n      data:\n        id: \"{{uuid}}\"\n"
			if strings.Contains(value, "*a") {
				lines = "      data: &a ~\n      templates:" + value + "\n"
			}

			_, err := parseTemplated(t, lines)

			if !errors.Is(err, config.ErrTemplatesValue) {
				t.Fatalf("error = %v, want %v", err, config.ErrTemplatesValue)
			}
			if !strings.Contains(err.Error(), "line ") || !strings.Contains(err.Error(), "templates") {
				t.Errorf("error %q does not give the line of templates", err)
			}
		})
	}
}

// A value that is not a bool is refused, not read as true or false.
// Ground: contract — a switch that guesses what "no" or 1 meant is a surprise.
func TestParse_TemplatesThatIsNotABoolIsAnError(t *testing.T) {
	for name, value := range map[string]string{
		"a number":       "1",
		"zero":           "0",
		"a quoted true":  `"true"`,
		"a quoted false": `"false"`,
		"a word":         "maybe",
		"a list":         "[true]",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTemplated(t, "      templates: "+value+"\n      data:\n        id: \"{{uuid}}\"\n")
			if err == nil {
				t.Errorf("templates: %s accepted", value)
			}
		})
	}
}

// A placeholder written without quotes is YAML's flow mapping, not a string:
// the decoder refuses it, with templates on or off, and never reads it as data.
// Ground: signal goccy/go-yaml — `{{` opens a flow map; the docs say to quote it.
func TestParse_AnUnquotedPlaceholderIsAnError(t *testing.T) {
	for _, on := range []string{"      templates: true\n", ""} {
		if _, err := parseTemplated(t, on+"      data:\n        id: {{uuid}}\n"); err == nil {
			t.Errorf("an unquoted {{uuid}} accepted (templates line %q)", on)
		}
	}
}
