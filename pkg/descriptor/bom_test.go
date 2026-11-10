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

const (
	protoBOM = "\xEF\xBB\xBF"
	// protoUTF16Refusal is the sentence every user file kind answers a UTF-16 file with.
	protoUTF16Refusal = "file is UTF-16; save it as UTF-8"
)

// descriptorCompile compiles shop.proto of dir, which is its own import path.
func descriptorCompile(dir string) (*descriptor.FileResolver, error) {
	return descriptorCompileEntries(dir, "shop.proto")
}

func descriptorCompileEntries(dir string, entries ...string) (*descriptor.FileResolver, error) {
	return descriptor.NewProtoResolver(at(dir, entries...), at(dir, "."))
}

func protoUTF16LE(s string) string {
	b := []byte{0xFF, 0xFE}
	for _, r := range s {
		b = append(b, byte(r), 0)
	}

	return string(b)
}

// Kind: .proto source (app.proto.files and what they import). The compiler is
// protocompile; a file with a leading mark is compiled, or refused with a
// message that names the mark, not read as a syntax error at 1:1.
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

// A UTF-16 .proto is refused, naming the file.
// Ground: contract — the public API of pkg/descriptor: a compile error names the file.
func TestProtoResolver_AUTF16SourceIsRefused(t *testing.T) {
	dir := tree(t, map[string]string{"shop.proto": protoUTF16LE(shopProto)})

	_, err := descriptorCompile(dir)
	if err == nil {
		t.Fatal("a UTF-16 .proto compiled")
	}
	if !strings.Contains(err.Error(), protoUTF16Refusal) {
		t.Errorf("error = %q, want it to say %q", err, protoUTF16Refusal)
	}
	if !strings.Contains(err.Error(), "shop.proto") {
		t.Errorf("error = %q, want it to name shop.proto", err)
	}
}
