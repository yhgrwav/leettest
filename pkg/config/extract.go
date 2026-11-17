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
	"fmt"
	"sort"
)

const extractNamePattern = "[A-Za-z_][A-Za-z0-9_]*"

// validateExtract checks what the config alone can say of the extract of call
// at (a path such as load.calls[0]) and of its steps: the names, that no name is
// taken twice, that a step reads only what an earlier step took, and that
// every name is read. What a path leads to in the reply is checked against the
// descriptor, by the CLI.
func (c Call) validateExtract(at string) []error {
	var errs []error

	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: %s", ErrScenario, fmt.Sprintf(format, args...)))
	}

	// where a name is declared (the path of its extract), and which of them are
	// visible to the step being read: those declared before it.
	declared := map[string]string{}
	var order []string

	declare := func(owner string, extract map[string]string) {
		names := make([]string, 0, len(extract))
		for name := range extract {
			names = append(names, name)
		}

		sort.Strings(names)

		for _, name := range names {
			path := owner + ".extract." + name

			switch first, taken := declared[name]; {
			case !ValidExtractName(name):
				fail("%s: the name must match %s", path, extractNamePattern)
			case taken:
				fail("%s: %q is extracted twice, first at %s; a name is taken once in a scenario", path, name, first)
			default:
				declared[name] = path
				order = append(order, name)
			}
		}
	}

	used := map[string]bool{}

	// reads checks the names the data at path reads against those declared so far.
	reads := func(path string, data any) {
		var scan templateScan
		scan.walk("data", data)

		for _, name := range scan.readNames() {
			if _, ok := declared[name]; ok {
				used[name] = true

				continue
			}

			fail("%s: {{extract.%s}} is read here, and no earlier step extracts %q: "+
				"a value is read only by a step after the one that took it", path, name, name)
		}
	}

	// The call's own data goes out before any reply: it reads nothing, its own
	// extract included.
	if c.TemplatesOn() {
		reads(at+".data", c.Data)
	}

	declare(at, c.Extract)

	for k, step := range c.Steps {
		path := fmt.Sprintf("%s.steps[%d]", at, k)

		// Braces in a step without templates: true are text, and read nothing.
		if step.TemplatesOn() && step.Dataset == "" {
			reads(path+".data", step.Data)
		}

		declare(path, step.Extract)
	}

	for _, name := range order {
		if !used[name] {
			fail("%s: %q is extracted and never read; read it as {{extract.%s}} in the data of a later step "+
				"with templates: true, or drop it", declared[name], name, name)
		}
	}

	return errs
}
