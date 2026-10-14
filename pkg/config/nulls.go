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
	"regexp"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

var callDatasetPath = regexp.MustCompile(`^\$\.load\.calls\[\d+\]\.(dataset|dataset_order|dataset_seed|dataset_reuse)$`)

// emptyKeyErrors is the error of each key that must not be written with no value.
var emptyKeyErrors = map[string]error{
	"dataset":       ErrEmptyDatasetPath,
	"dataset_order": ErrDatasetOrder,
	"dataset_seed":  ErrDatasetSeed,
	"dataset_reuse": ErrDatasetReuse,
}

// checkTree runs the checks the decoder cannot make. An anchor name defined
// twice is refused first (see collectAnchors), so every alias has one anchor.
// Then it rejects `dataset:`, `dataset: ~` and `dataset: null` under a call
// (and the same for dataset_order and dataset_seed, whose lost value would
// change the run without a word),
// also behind an anchor (`&a ~`) or a tag (`!!null`, `&a !!null`), and an alias
// (`*a`, also behind a tag) of an anchor that marks a null. The decoder reads
// each as the key left out, so the path is lost without a word and the call
// runs with an empty message. An alias with no anchor is the decoder's error.
func checkTree(raw []byte) error {
	file, err := parser.ParseBytes(raw, 0)
	if err != nil {
		return err
	}

	anchors, err := collectAnchors(file)
	if err != nil {
		return err
	}

	v := nullDatasetVisitor{anchors: anchors}
	for _, doc := range file.Docs {
		ast.Walk(&v, doc)
	}

	return errors.Join(v.errs...)
}

type nullDatasetVisitor struct {
	anchors map[string]ast.Node
	errs    []error
}

func (v *nullDatasetVisitor) Visit(node ast.Node) ast.Visitor {
	pair, ok := node.(*ast.MappingValueNode)
	if !ok {
		return v
	}
	match := callDatasetPath.FindStringSubmatch(pair.GetPath())
	if match == nil {
		return v
	}

	value := pair.Value
	followed := map[string]bool{}
unwrap:
	for {
		switch wrapped := value.(type) {
		case *ast.AnchorNode:
			value = wrapped.Value
		case *ast.TagNode:
			value = wrapped.Value
		case *ast.AliasNode:
			name := wrapped.Value.String()
			target, defined := v.anchors[name]
			if !defined || followed[name] {
				return v
			}
			followed[name] = true
			value = target
		default:
			break unwrap
		}
	}

	_, null := value.(*ast.NullNode)
	// The decoder takes 1.5 and "42" for a whole number; a seed is neither.
	_, whole := value.(*ast.IntegerNode)
	if null || (match[1] == "dataset_seed" && !whole) {
		v.errs = append(v.errs, fmt.Errorf("line %d: %w", pair.Key.GetToken().Position.Line, emptyKeyErrors[match[1]]))
	}

	return v
}
