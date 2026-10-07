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

var callDatasetPath = regexp.MustCompile(`^\$\.load\.calls\[\d+\]\.dataset$`)

// checkNullDataset rejects `dataset:`, `dataset: ~` and `dataset: null` under
// a call. The decoder reads each as the key left out, so the path is lost
// without a word and the call runs with an empty message.
func checkNullDataset(raw []byte) error {
	file, err := parser.ParseBytes(raw, 0)
	if err != nil {
		return err
	}

	var v nullDatasetVisitor
	for _, doc := range file.Docs {
		ast.Walk(&v, doc)
	}

	return errors.Join(v.errs...)
}

type nullDatasetVisitor struct {
	errs []error
}

func (v *nullDatasetVisitor) Visit(node ast.Node) ast.Visitor {
	pair, ok := node.(*ast.MappingValueNode)
	if !ok || !callDatasetPath.MatchString(pair.GetPath()) {
		return v
	}

	if _, null := pair.Value.(*ast.NullNode); null {
		v.errs = append(v.errs, fmt.Errorf("line %d: %w", pair.Key.GetToken().Position.Line, ErrEmptyDatasetPath))
	}

	return v
}
