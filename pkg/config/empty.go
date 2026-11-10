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

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// ErrEmptyConfig says the file has no YAML document with a body: it is empty,
// blank, only comments, only document markers (---) or an explicit null.
var ErrEmptyConfig = errors.New("config is empty: nothing to run")

// checkEmpty returns ErrEmptyConfig when no document has a body. Syntax errors
// are left to the checks that follow, which report them.
func checkEmpty(raw []byte) error {
	file, err := parser.ParseBytes(raw, 0)
	if err != nil {
		return nil //nolint:nilerr // reported by the next check
	}

	for _, doc := range file.Docs {
		if hasBody(doc) {
			return nil
		}
	}

	return ErrEmptyConfig
}

// hasBody reports whether the document holds something other than null.
func hasBody(doc *ast.DocumentNode) bool {
	if doc.Body == nil {
		return false
	}
	_, null := doc.Body.(*ast.NullNode)

	return !null
}

// walkDocs walks each document that has a node to walk. A bare `---` is a
// document with no body, and walking it would dereference nil.
func walkDocs(file *ast.File, v ast.Visitor) {
	for _, doc := range file.Docs {
		if doc.Body != nil {
			ast.Walk(v, doc)
		}
	}
}
