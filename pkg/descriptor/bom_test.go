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

	"github.com/yhgrwav/leettest/pkg/descriptor"
)

const protoBOM = "\xEF\xBB\xBF"

// descriptorCompile compiles shop.proto of dir, which is its own import path.
func descriptorCompile(dir string) (*descriptor.FileResolver, error) {
	return descriptorCompileEntries(dir, "shop.proto")
}

func descriptorCompileEntries(dir string, entries ...string) (*descriptor.FileResolver, error) {
	return descriptor.NewProtoResolver(at(dir, entries...), at(dir, "."))
}

// Kind: .proto source (app.proto.files and what they import). The compiler is
// protocompile, which skips a leading mark itself: these are guards, so that an
// upgrade of it that stops doing so is seen. A UTF-16 .proto is not refused by
// name: it is a compile error at 1:1.
// Ground: contract — the public API of pkg/descriptor: a method of a compiled file.
func TestProtoResolver_ALeadingBOMIsNotPartOfTheSource(t *testing.T) {
	dir := tree(t, map[string]string{"shop.proto": protoBOM + shopProto})

	r, err := descriptorCompile(dir)
	if err != nil {
		t.Fatalf("a .proto with a leading mark: %v", err)
	}

	m, err := r.Resolve(t.Context(), "shop.v1.Shop/Get")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if m.Input.FullName() != "shop.v1.Req" || m.Output.FullName() != "shop.v1.Resp" {
		t.Errorf("input %s, output %s, want shop.v1.Req and shop.v1.Resp", m.Input.FullName(), m.Output.FullName())
	}
}

// An imported file with a mark is read like an entry file with one.
// Ground: contract — the public API of pkg/descriptor: a method of a compiled file.
func TestProtoResolver_AnImportedFileWithABOMIsRead(t *testing.T) {
	dir := tree(t, map[string]string{
		"shop.proto": "syntax = \"proto3\";\npackage shop.v1;\nimport \"common.proto\";\n" +
			"service Shop { rpc Get(common.v1.Id) returns (common.v1.Id); }\n",
		"common.proto": protoBOM + "syntax = \"proto3\";\npackage common.v1;\nmessage Id { string id = 1; }\n",
	})

	if _, err := descriptorCompileEntries(dir, "shop.proto"); err != nil {
		t.Fatalf("an import with a leading mark: %v", err)
	}
}

// An error in a file with a mark is at the line and column it has without one.
// Ground: contract — the public API of pkg/descriptor: a compile error carries its position.
func TestProtoResolver_AnErrorInAFileWithABOMKeepsItsPosition(t *testing.T) {
	const broken = "syntax = \"proto3\";\npackage shop.v1;\nmessage Req { string id = 1 }\n"

	plainDir := tree(t, map[string]string{"shop.proto": broken})
	markedDir := tree(t, map[string]string{"shop.proto": protoBOM + broken})

	_, plainErr := descriptorCompile(plainDir)
	if plainErr == nil {
		t.Fatal("fixture: the broken file compiles")
	}

	_, markedErr := descriptorCompile(markedDir)
	if markedErr == nil {
		t.Fatal("a broken file with a mark compiled")
	}

	got := strings.ReplaceAll(markedErr.Error(), markedDir, "DIR")
	want := strings.ReplaceAll(plainErr.Error(), plainDir, "DIR")

	if got != want {
		t.Errorf("error with a mark:\n%s\nerror without:\n%s", got, want)
	}
}
