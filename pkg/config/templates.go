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
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

var (
	// ErrTemplatesValue is a templates that is not true or false, or has no value.
	ErrTemplatesValue = errors.New("templates must be true or false")
	// ErrTemplatesNoPlaceholders is templates: true on a call whose data has none.
	ErrTemplatesNoPlaceholders = errors.New("templates: true without placeholders in data")
	// ErrTemplatesUnknown is a {{ in data with templates on that is not {{uuid}} or {{seq}}.
	ErrTemplatesUnknown = errors.New("unknown placeholder in data")
	// ErrTemplatesKey is a {{ in a key of data with templates on.
	ErrTemplatesKey = errors.New("placeholder in a key of data")
	// ErrTemplatesWithDataset is templates: true on a call with a dataset.
	ErrTemplatesWithDataset = errors.New("templates work with `data` only")
)

// The placeholders of a templated call's data: the names between the braces.
const (
	PlaceholderUUID = "uuid"
	PlaceholderSeq  = "seq"
)

// TemplatePart is a piece of a text with placeholders: a literal text, or, when
// Placeholder is set, the placeholder of that name (Text is then empty).
type TemplatePart struct {
	Text        string
	Placeholder string
}

// ParseTemplate splits text into literal pieces and placeholders. Only
// {{uuid}} and {{seq}} are placeholders, written exactly: no spaces, no other
// case, no escape. bad is the first fragment that starts a placeholder and is
// none, from its {{ to the next }} or the end of the text; parts are then nil.
func ParseTemplate(text string) (parts []TemplatePart, bad string) {
	rest := text

	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			break
		}
		if i > 0 {
			parts = append(parts, TemplatePart{Text: rest[:i]})
		}

		rest = rest[i:]

		switch {
		case strings.HasPrefix(rest, "{{"+PlaceholderUUID+"}}"):
			parts = append(parts, TemplatePart{Placeholder: PlaceholderUUID})
			rest = rest[len("{{"+PlaceholderUUID+"}}"):]
		case strings.HasPrefix(rest, "{{"+PlaceholderSeq+"}}"):
			parts = append(parts, TemplatePart{Placeholder: PlaceholderSeq})
			rest = rest[len("{{"+PlaceholderSeq+"}}"):]
		default:
			if end := strings.Index(rest[2:], "}}"); end >= 0 {
				return nil, rest[:end+4]
			}

			return nil, rest
		}
	}

	if rest != "" {
		parts = append(parts, TemplatePart{Text: rest})
	}

	return parts, ""
}

// TemplatesOn says the call's data is a template.
func (c Call) TemplatesOn() bool {
	return c.Templates != nil && *c.Templates
}

// TemplatePlaceholders are the placeholders the call's data uses, sorted; nil
// when templates are off.
func (c Call) TemplatePlaceholders() []string {
	if !c.TemplatesOn() {
		return nil
	}

	var scan templateScan
	scan.walk("data", c.Data)

	return scan.names()
}

// templateScan is one walk over a call's data.
type templateScan struct {
	found  map[string]bool
	braces bool
	errs   []error
}

func (s *templateScan) names() []string {
	out := make([]string, 0, len(s.found))
	for name := range s.found {
		out = append(out, name)
	}

	sort.Strings(out)

	if len(out) == 0 {
		return nil
	}

	return out
}

func (s *templateScan) walk(path string, v any) {
	switch x := v.(type) {
	case string:
		s.text(path, x)
	case []any:
		for i, e := range x {
			s.walk(fmt.Sprintf("%s[%d]", path, i), e)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}

		slices.Sort(keys)

		for _, k := range keys {
			if strings.Contains(k, "{{") {
				s.braces = true
				s.errs = append(s.errs, fmt.Errorf("%w: %s has the key %q; a key is a name that does not change "+
					"per request, only a value takes a placeholder", ErrTemplatesKey, path, k))
			}

			s.walk(path+"."+k, x[k])
		}
	}
}

func (s *templateScan) text(path, text string) {
	if !strings.Contains(text, "{{") {
		return
	}

	s.braces = true

	parts, bad := ParseTemplate(text)
	if bad != "" {
		s.errs = append(s.errs, fmt.Errorf("%w: %s has %q; the placeholders are {{%s}} and {{%s}}, "+
			"written exactly (no spaces, no other case); there is no escape: a literal {{ goes out "+
			"from a call without templates", ErrTemplatesUnknown, path, bad, PlaceholderUUID, PlaceholderSeq))

		return
	}

	if s.found == nil {
		s.found = map[string]bool{}
	}

	for _, p := range parts {
		if p.Placeholder != "" {
			s.found[p.Placeholder] = true
		}
	}
}

// validateTemplates checks a call that asks for templates: they work with data
// only, and the data must use a placeholder, spelled as the list above.
func (c Call) validateTemplates() []error {
	if !c.TemplatesOn() {
		return nil
	}
	if c.RawDataset != nil {
		return []error{fmt.Errorf("%w: templates: true with a dataset; the records of a file are not templated", ErrTemplatesWithDataset)}
	}

	var scan templateScan
	scan.walk("data", c.Data)

	if !scan.braces {
		return []error{fmt.Errorf("%w: templates: true changes nothing; put {{%s}} or {{%s}} into a string of data "+
			"or remove the key", ErrTemplatesNoPlaceholders, PlaceholderUUID, PlaceholderSeq)}
	}

	return scan.errs
}
