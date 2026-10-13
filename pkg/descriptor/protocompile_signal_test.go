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

package descriptor_test

import (
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
)

func compileSource(t *testing.T, src string) error {
	t.Helper()

	c := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			Accessor: protocompile.SourceAccessorFromMap(map[string]string{"x.proto": src}),
		}),
	}
	_, err := c.Compile(t.Context(), "x.proto")

	return err
}

// The edition limit of the pinned compiler is what the error hint for 2024 is
// written around (TestProtoResolver_Edition2024IsRefusedWithTheWayOut). The day
// the compiler is upgraded past it, this goes red: the hint and the
// «decisions» entry change then, not silently.
// Ground: signal github.com/bufbuild/protocompile v0.14.1 — the newest edition it reads is 2023.
func TestProtocompile_ReadsEdition2023AndNot2024(t *testing.T) {
	if err := compileSource(t, "edition = \"2023\";\npackage p;\nmessage M {}\n"); err != nil {
		t.Errorf("edition 2023: %v", err)
	}

	err := compileSource(t, "edition = \"2024\";\npackage p;\nmessage M {}\n")
	if err == nil || !strings.Contains(err.Error(), "2024") {
		t.Errorf("edition 2024: err = %v, want the compiler's refusal naming 2024", err)
	}
}

// The standard imports are the compiler's own copies: no import path holds them.
// Ground: signal github.com/bufbuild/protocompile v0.14.1 — WithStandardImports serves google/protobuf/*.proto.
func TestProtocompile_ServesWellKnownTypesWithoutAnImportPath(t *testing.T) {
	src := "syntax = \"proto3\";\npackage p;\nimport \"google/protobuf/any.proto\";\nimport \"google/protobuf/duration.proto\";\n" +
		"message M { google.protobuf.Any a = 1; google.protobuf.Duration d = 2; }\n"

	if err := compileSource(t, src); err != nil {
		t.Errorf("well-known imports: %v", err)
	}
}
