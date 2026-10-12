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
	"sort"

	"github.com/goccy/go-yaml/ast"
)

type duplicateAnchorError struct {
	name  string
	line  int
	first int
}

func (e *duplicateAnchorError) Error() string {
	return fmt.Sprintf("line %d: anchor &%s is already defined at line %d; give it another name", e.line, e.name, e.first)
}

func (e *duplicateAnchorError) Is(target error) bool {
	return target == ErrDuplicateAnchor
}

type anchorDef struct {
	name   string
	line   int
	column int
	value  ast.Node
}

type anchorCollector struct {
	defs []anchorDef
}

func (c *anchorCollector) Visit(node ast.Node) ast.Visitor {
	if anchor, ok := node.(*ast.AnchorNode); ok && anchor.Start != nil && anchor.Name != nil {
		c.defs = append(c.defs, anchorDef{
			name:   anchor.Name.String(),
			line:   anchor.Start.Position.Line,
			column: anchor.Start.Position.Column,
			value:  anchor.Value,
		})
	}

	return c
}

// collectAnchors maps every anchor name in the file to the node it marks.
// The decoder resolves an alias to the last definition of its name in the
// document, not the nearest one before it, so a reused name silently hands a
// field another anchor's value: a name defined twice, on any kind of node
// anywhere in the config, is refused, each repeat against the first definition.
func collectAnchors(file *ast.File) (map[string]ast.Node, error) {
	var c anchorCollector
	for _, doc := range file.Docs {
		ast.Walk(&c, doc)
	}

	sort.SliceStable(c.defs, func(i, j int) bool {
		if c.defs[i].line != c.defs[j].line {
			return c.defs[i].line < c.defs[j].line
		}

		return c.defs[i].column < c.defs[j].column
	})

	anchors := make(map[string]ast.Node, len(c.defs))
	firstLine := make(map[string]int, len(c.defs))
	var errs []error
	for _, def := range c.defs {
		if first, seen := firstLine[def.name]; seen {
			errs = append(errs, &duplicateAnchorError{name: def.name, line: def.line, first: first})

			continue
		}
		firstLine[def.name] = def.line
		anchors[def.name] = def.value
	}

	return anchors, errors.Join(errs...)
}
