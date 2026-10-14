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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/yhgrwav/leettest/pkg/descriptor"
)

// tree writes files (name relative to the returned directory, content) into a
// fresh directory. Names use forward slashes.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	return dir
}

// at is a path under dir, in the form the OS writes it.
func at(dir string, names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(dir, filepath.FromSlash(n))
	}

	return out
}

// compile builds a resolver from the entry files (names under dir) with the
// import paths (names under dir), failing the test if the build does.
func compile(t *testing.T, dir string, entries, imports []string) *descriptor.FileResolver {
	t.Helper()

	r, err := descriptor.NewProtoResolver(at(dir, entries...), at(dir, imports...))
	if err != nil {
		t.Fatalf("compile %v: %v", entries, err)
	}

	return r
}

// compileErr is the error of a build that must fail.
func compileErr(dir string, entries, imports []string) error {
	_, err := descriptor.NewProtoResolver(at(dir, entries...), at(dir, imports...))

	return err
}

const shopProto = `syntax = "proto3";
package shop.v1;
message Req { string id = 1; }
message Resp { string name = 1; }
service Shop {
  rpc Get(Req) returns (Resp);
  rpc Watch(Req) returns (stream Resp);
}
`

// fieldMessage is the full name of the message a field of msg has, "" if the
// field is missing or not a message.
func fieldMessage(msg protoreflect.MessageDescriptor, field string) protoreflect.FullName {
	f := msg.Fields().ByName(protoreflect.Name(field))
	if f == nil || f.Message() == nil {
		return ""
	}

	return f.Message().FullName()
}

// --- .proto files ----------------------------------------------------------

// Ground: contract — the public API of pkg/descriptor: a method of a compiled file.
func TestProtoResolver_ResolvesAMethodOfAnEntryFile(t *testing.T) {
	dir := tree(t, map[string]string{"shop.proto": shopProto})
	r := compile(t, dir, []string{"shop.proto"}, []string{"."})

	m, err := r.Resolve(t.Context(), "shop.v1.Shop/Get")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if m.Path != "/shop.v1.Shop/Get" {
		t.Errorf("path = %q, want /shop.v1.Shop/Get", m.Path)
	}
	if m.Input.FullName() != "shop.v1.Req" || m.Output.FullName() != "shop.v1.Resp" {
		t.Errorf("input %s, output %s, want shop.v1.Req and shop.v1.Resp", m.Input.FullName(), m.Output.FullName())
	}
}

// Ground: contract — ResolveAll answers every name asked, from the same files.
func TestProtoResolver_ResolveAllAnswersEveryNameAsked(t *testing.T) {
	dir := tree(t, map[string]string{"shop.proto": shopProto})
	r := compile(t, dir, []string{"shop.proto"}, []string{"."})

	got, err := r.ResolveAll(t.Context(), []string{"shop.v1.Shop/Get", "shop.v1.Shop/Get"})
	if err != nil {
		t.Fatalf("resolve all: %v", err)
	}

	if m := got["shop.v1.Shop/Get"]; len(got) != 1 || m == nil || m.Input.FullName() != "shop.v1.Req" {
		t.Errorf("answers = %v, want one entry for shop.v1.Shop/Get", got)
	}
}

// The order file sits under api/ and imports a file from third_party/: the
// import is found on the second import path.
// Ground: contract — imports are looked up on every import path.
func TestProtoResolver_FindsAnImportOnASecondImportPath(t *testing.T) {
	dir := tree(t, map[string]string{
		"api/order.proto": `syntax = "proto3";
package order.v1;
import "common/money.proto";
message Req { common.v1.Money amount = 1; }
message Resp {}
service Orders { rpc Pay(Req) returns (Resp); }
`,
		"third_party/common/money.proto": `syntax = "proto3";
package common.v1;
message Money { int64 units = 1; }
`,
	})
	r := compile(t, dir, []string{"api/order.proto"}, []string{"api", "third_party"})

	m, err := r.Resolve(t.Context(), "order.v1.Orders/Pay")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := fieldMessage(m.Input, "amount"); got != "common.v1.Money" {
		t.Errorf("amount is %q, want common.v1.Money from third_party", got)
	}
}

// Ground: contract — well-known types resolve with no import path that holds them.
func TestProtoResolver_WellKnownTypesNeedNoImportPath(t *testing.T) {
	dir := tree(t, map[string]string{"wkt.proto": `syntax = "proto3";
package wkt.v1;
import "google/protobuf/any.proto";
import "google/protobuf/duration.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/struct.proto";
import "google/protobuf/timestamp.proto";
import "google/protobuf/wrappers.proto";
message Req {
  google.protobuf.Any any = 1;
  google.protobuf.Duration timeout = 2;
  google.protobuf.Empty nothing = 3;
  google.protobuf.Struct meta = 4;
  google.protobuf.Timestamp at = 5;
  google.protobuf.StringValue name = 6;
}
message Resp {}
service Wkt { rpc Do(Req) returns (Resp); }
`})
	r := compile(t, dir, []string{"wkt.proto"}, []string{"."})

	m, err := r.Resolve(t.Context(), "wkt.v1.Wkt/Do")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	want := map[string]protoreflect.FullName{
		"any": "google.protobuf.Any", "timeout": "google.protobuf.Duration", "nothing": "google.protobuf.Empty",
		"meta": "google.protobuf.Struct", "at": "google.protobuf.Timestamp", "name": "google.protobuf.StringValue",
	}
	for field, full := range want {
		if got := fieldMessage(m.Input, field); got != full {
			t.Errorf("field %s is %q, want %s", field, got, full)
		}
	}
}

// Ground: contract — a compile error says where.
func TestProtoResolver_ASyntaxErrorCarriesTheFileLineAndColumn(t *testing.T) {
	dir := tree(t, map[string]string{"bad.proto": `syntax = "proto3";
package bad.v1;
message Req {
  string id = one;
}
`})

	err := compileErr(dir, []string{"bad.proto"}, []string{"."})

	if !errors.Is(err, descriptor.ErrCompile) {
		t.Fatalf("err = %v, want descriptor.ErrCompile", err)
	}
	if !regexp.MustCompile(`bad\.proto:4:\d+`).MatchString(err.Error()) {
		t.Errorf("error %q does not carry bad.proto:4:<column>", err)
	}
}

// Ground: contract — a type error says where and names the type.
func TestProtoResolver_AnUnknownTypeIsACompileErrorWithPosition(t *testing.T) {
	dir := tree(t, map[string]string{"bad.proto": `syntax = "proto3";
package bad.v1;
message Req {
  Missing item = 1;
}
`})

	err := compileErr(dir, []string{"bad.proto"}, []string{"."})

	if !errors.Is(err, descriptor.ErrCompile) {
		t.Fatalf("err = %v, want descriptor.ErrCompile", err)
	}
	if !regexp.MustCompile(`bad\.proto:4:\d+`).MatchString(err.Error()) || !strings.Contains(err.Error(), "Missing") {
		t.Errorf("error %q does not carry bad.proto:4:<column> and the name Missing", err)
	}
}

// The commonest miss with grpc-gateway services: an import that lives in
// another repository. The message lists where it looked, and the other way out.
// Ground: contract — the not-found message names the paths and the protoset.
func TestProtoResolver_AMissingImportListsThePathsAndSuggestsAProtoset(t *testing.T) {
	dir := tree(t, map[string]string{
		"api/svc.proto": `syntax = "proto3";
package gw.v1;
import "google/api/annotations.proto";
message Req {}
message Resp {}
service Gw { rpc Do(Req) returns (Resp); }
`,
		"third_party/keep": "",
	})

	err := compileErr(dir, []string{"api/svc.proto"}, []string{"api", "third_party"})

	if !errors.Is(err, descriptor.ErrCompile) {
		t.Fatalf("err = %v, want descriptor.ErrCompile", err)
	}
	if !regexp.MustCompile(`svc\.proto:3:\d+`).MatchString(err.Error()) {
		t.Errorf("error %q does not carry svc.proto:3:<column>", err)
	}
	wants := append([]string{"google/api/annotations.proto", "protoset"}, at(dir, "api", "third_party")...)
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// Options defined in a file of the user's: compiled when the file is on the
// import paths, and the request is the same message with or without them.
// Ground: contract — custom options (protovalidate, grpc-gateway) never change the request.
func TestProtoResolver_CustomOptionsCompileAndDoNotChangeTheRequest(t *testing.T) {
	dir := tree(t, map[string]string{
		"api/svc.proto": `syntax = "proto3";
package opt.v1;
import "opts.proto";
message Req { string id = 1 [(rules.v1.rule) = "required"]; }
message Resp {}
service Opt { rpc Do(Req) returns (Resp); }
`,
		"lib/opts.proto": `syntax = "proto3";
package rules.v1;
import "google/protobuf/descriptor.proto";
extend google.protobuf.FieldOptions { string rule = 50001; }
`,
	})
	r := compile(t, dir, []string{"api/svc.proto"}, []string{"api", "lib"})

	m, err := r.Resolve(t.Context(), "opt.v1.Opt/Do")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	fields := m.Input.Fields()
	if fields.Len() != 1 || fields.Get(0).Name() != "id" || fields.Get(0).Kind() != protoreflect.StringKind {
		t.Errorf("request fields = %v, want only the string id", fields)
	}
}

// Ground: contract — editions 2023 is read.
func TestProtoResolver_Edition2023Compiles(t *testing.T) {
	dir := tree(t, map[string]string{"e2023.proto": `edition = "2023";
package ed.v1;
message Req { string name = 1; }
message Resp {}
service Ed { rpc Do(Req) returns (Resp); }
`})
	r := compile(t, dir, []string{"e2023.proto"}, []string{"."})

	m, err := r.Resolve(t.Context(), "ed.v1.Ed/Do")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if m.Input.FullName() != "ed.v1.Req" {
		t.Errorf("input = %s, want ed.v1.Req", m.Input.FullName())
	}
}

// protocompile v0.14.1 stops at edition 2023 and says so with a position
// (ghz#471). The message adds the way out, which does work: protoset (the
// protobuf-go side reads 2024).
// Ground: signal github.com/bufbuild/protocompile v0.14.1 — the compiler's edition limit; the way out is ours.
func TestProtoResolver_Edition2024IsRefusedWithTheWayOut(t *testing.T) {
	dir := tree(t, map[string]string{"e2024.proto": `edition = "2024";
package ed.v1;
message Req { string name = 1; }
`})

	err := compileErr(dir, []string{"e2024.proto"}, []string{"."})

	if !errors.Is(err, descriptor.ErrCompile) {
		t.Fatalf("err = %v, want descriptor.ErrCompile", err)
	}
	if !regexp.MustCompile(`e2024\.proto:1:\d+`).MatchString(err.Error()) {
		t.Errorf("error %q does not carry e2024.proto:1:<column>", err)
	}
	for _, want := range []string{"2024", "protoset"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// The file name in the set is the path under its import path, as protoc names
// it. The order file imports "user/v1/user.proto" and both are entry files:
// had the user file been compiled under its path from the config's directory,
// the import would reach it under a second name and fail with "already
// defined" (ghz#206, #347).
// Ground: contract — one file, one name, whatever the entry list and the imports say.
func TestProtoResolver_TwoEntryFilesOneImportingTheOtherCompileTogether(t *testing.T) {
	dir := tree(t, map[string]string{
		"api/user/v1/user.proto": `syntax = "proto3";
package user.v1;
message User { string id = 1; }
message GetReq {}
service Users { rpc Get(GetReq) returns (User); }
`,
		"api/order/v1/order.proto": `syntax = "proto3";
package order.v1;
import "user/v1/user.proto";
message Order { user.v1.User owner = 1; }
message ListReq {}
service Orders { rpc List(ListReq) returns (Order); }
`,
	})
	r := compile(t, dir, []string{"api/user/v1/user.proto", "api/order/v1/order.proto"}, []string{"api"})

	for _, method := range []string{"user.v1.Users/Get", "order.v1.Orders/List"} {
		if _, err := r.Resolve(t.Context(), method); err != nil {
			t.Errorf("resolve %s: %v", method, err)
		}
	}
	m, err := r.Resolve(t.Context(), "order.v1.Orders/List")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := fieldMessage(m.Output, "owner"); got != "user.v1.User" {
		t.Errorf("owner is %q, want user.v1.User", got)
	}
}

// An entry file that is also only imported, not listed: the same name again.
// Ground: contract — the imported file is found under the name its importer uses.
func TestProtoResolver_AnImportedFileIsNotAlsoAnEntryUnderAnotherName(t *testing.T) {
	dir := tree(t, map[string]string{
		"api/user/v1/user.proto": `syntax = "proto3";
package user.v1;
message User { string id = 1; }
`,
		"api/order/v1/order.proto": `syntax = "proto3";
package order.v1;
import "user/v1/user.proto";
message Order { user.v1.User owner = 1; }
message ListReq {}
service Orders { rpc List(ListReq) returns (Order); }
`,
	})
	r := compile(t, dir, []string{"api/order/v1/order.proto"}, []string{"api"})

	if _, err := r.Resolve(t.Context(), "order.v1.Orders/List"); err != nil {
		t.Errorf("resolve: %v", err)
	}
}

// Ground: contract — an entry file outside the import paths is a mistake with both parts named.
func TestProtoResolver_AFileUnderNoImportPathIsRefused(t *testing.T) {
	dir := tree(t, map[string]string{
		"loose/x.proto":    `syntax = "proto3"; package x.v1;`,
		"api/keep":         "",
		"third_party/keep": "",
	})

	err := compileErr(dir, []string{"loose/x.proto"}, []string{"api", "third_party"})

	if !errors.Is(err, descriptor.ErrOutsideImportPaths) {
		t.Fatalf("err = %v, want descriptor.ErrOutsideImportPaths", err)
	}
	wants := append(at(dir, "loose/x.proto"), at(dir, "api", "third_party")...)
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// Two services in one name under two roots: api/foo/x.proto is service A,
// vendor/foo/x.proto is service B.
func shadowTree(t *testing.T) string {
	t.Helper()

	return tree(t, map[string]string{
		"api/foo/x.proto": `syntax = "proto3";
package a.v1;
message Req {}
service A { rpc Get(Req) returns (Req); }
`,
		"vendor/foo/x.proto": `syntax = "proto3";
package b.v1;
message Req {}
service B { rpc Get(Req) returns (Req); }
`,
	})
}

// protoc refuses ("Input is shadowed in the --proto_path"); compiling the other
// file under this file's name would be the wrong schema, silently (ghz#206).
// Ground: contract — an entry file is the file the user listed or an error naming both.
func TestProtoResolver_AnEntryFileShadowedByAnEarlierImportPathIsRefused(t *testing.T) {
	dir := shadowTree(t)

	r, err := descriptor.NewProtoResolver(at(dir, "vendor/foo/x.proto"), at(dir, "api", "vendor"))

	if !errors.Is(err, descriptor.ErrShadowedInput) {
		t.Fatalf("err = %v, want descriptor.ErrShadowedInput", err)
	}
	if r != nil {
		t.Errorf("a resolver came back with the error: it could answer from the other file")
	}
	namesBothFilesAndTheWayOut(t, err, dir, "vendor/foo/x.proto", "api/foo/x.proto")
}

// namesBothFilesAndTheWayOut checks the text of a shadowing refusal: the entry
// file, the file that shadows it, and the way out. A root is a substring of its
// file's path, so a bare Contains on the root is met by the file alone; in the
// right text the shadowing root appears twice (the file in the description and in
// "list ... instead") and the entry's root twice (the file and "put ... first").
func namesBothFilesAndTheWayOut(t *testing.T, err error, dir, entry, shadowing string) {
	t.Helper()

	msg := err.Error()
	for _, want := range at(dir, entry, shadowing) {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	for _, name := range []string{entry, shadowing} {
		root := at(dir, strings.SplitN(name, "/", 2)[0])[0]
		if n := strings.Count(msg, root); n < 2 {
			t.Errorf("error %q names %q %d time(s): the way out (list the other file, or put the entry's root first) is missing", err, root, n)
		}
	}
}

// Ground: contract — the same two files, the entry's root first: nothing shadows it.
func TestProtoResolver_AnEntryFileFirstInImportPathsIsNotShadowed(t *testing.T) {
	dir := shadowTree(t)
	r := compile(t, dir, []string{"vendor/foo/x.proto"}, []string{"vendor", "api"})

	if _, err := r.Resolve(t.Context(), "b.v1.B/Get"); err != nil {
		t.Errorf("resolve B: %v", err)
	}
	if _, err := r.Resolve(t.Context(), "a.v1.A/Get"); err == nil {
		t.Errorf("A resolved: the other file was compiled too")
	}
}

// One file under two roots is one file, not a shadow. A hard link is the
// spelling that needs no privilege on Windows; nameUnder takes the first root
// that holds the entry, so two spellings of one directory never reach the check
// (the second subtest holds that).
// Ground: contract — the same physical file is not shadowed by itself.
func TestProtoResolver_TheSameFileUnderTwoImportPathsIsNotShadowed(t *testing.T) {
	t.Run("a hard link", func(t *testing.T) {
		dir := tree(t, map[string]string{
			"api/foo/x.proto": shopProto,
			"vendor/foo/keep": "",
		})
		if err := os.Link(at(dir, "api/foo/x.proto")[0], at(dir, "vendor/foo/x.proto")[0]); err != nil {
			t.Skipf("no hard links here: %v", err)
		}

		r := compile(t, dir, []string{"vendor/foo/x.proto"}, []string{"api", "vendor"})

		if _, err := r.Resolve(t.Context(), "shop.v1.Shop/Get"); err != nil {
			t.Errorf("resolve: %v", err)
		}
	})

	t.Run("one root spelled twice", func(t *testing.T) {
		dir := tree(t, map[string]string{"api/foo/x.proto": shopProto})
		root := at(dir, "api")[0]

		r, err := descriptor.NewProtoResolver(at(dir, "api/foo/x.proto"), []string{root, root + string(filepath.Separator) + "."})
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if _, err := r.Resolve(t.Context(), "shop.v1.Shop/Get"); err != nil {
			t.Errorf("resolve: %v", err)
		}
	})
}

// Whatever sits at the name shadows, a directory too: the compiler's own answer
// there ("is a directory" / "Incorrect function") names neither file nor the way out.
// Ground: contract — anything but the entry file itself at its name on an earlier path is refused, naming that path.
func TestProtoResolver_ADirectoryAtTheNameInAnEarlierPathShadowsToo(t *testing.T) {
	dir := tree(t, map[string]string{
		"api/foo/x.proto/keep": "",
		"vendor/foo/x.proto":   shopProto,
	})

	err := compileErr(dir, []string{"vendor/foo/x.proto"}, []string{"api", "vendor"})

	if !errors.Is(err, descriptor.ErrShadowedInput) {
		t.Fatalf("err = %v, want descriptor.ErrShadowedInput", err)
	}
	namesBothFilesAndTheWayOut(t, err, dir, "vendor/foo/x.proto", "api/foo/x.proto")
}

// The common case: the entry sits under the second path and the first holds
// nothing at its name. Absence, or a failed Stat, is not a shadow.
// Ground: contract — only something at the name on an earlier path shadows; nothing there does not.
func TestProtoResolver_NothingAtTheNameInAnEarlierPathIsNotShadowed(t *testing.T) {
	dir := tree(t, map[string]string{
		"third_party/keep": "",
		"api/foo/x.proto":  shopProto,
	})

	r := compile(t, dir, []string{"api/foo/x.proto"}, []string{"third_party", "api"})

	if _, err := r.Resolve(t.Context(), "shop.v1.Shop/Get"); err != nil {
		t.Errorf("resolve: %v", err)
	}
}

// The library does not guess an import path: the config puts the directory of
// the config file there. With none, no file is under one.
// Ground: contract — an empty collection of import paths holds no file.
func TestProtoResolver_NoImportPathsHoldNoFile(t *testing.T) {
	dir := tree(t, map[string]string{"shop.proto": shopProto})

	for name, imports := range map[string][]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			_, err := descriptor.NewProtoResolver(at(dir, "shop.proto"), imports)

			if !errors.Is(err, descriptor.ErrOutsideImportPaths) {
				t.Errorf("err = %v, want descriptor.ErrOutsideImportPaths", err)
			}
		})
	}
}

// --- finding the method -----------------------------------------------------

// bigProto is a service of n methods named M01, M02, ...
func bigProto(n int) string {
	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\npackage bigpkg.v1;\nmessage Req {}\nmessage Resp {}\nservice Big {\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "  rpc M%02d(Req) returns (Resp);\n", i)
	}
	b.WriteString("}\n")

	return b.String()
}

// What was found goes in the message, at most 20 names and then how many more:
// at 20 nothing is cut, at 21 one name is, at 23 three.
// Ground: contract — the list is capped at 20.
func TestProtoResolver_AnUnknownMethodListsWhatWasFoundUpToTwenty(t *testing.T) {
	names := regexp.MustCompile(`M\d\d`)

	tests := []struct {
		methods     int
		listed      int
		moreMessage string
	}{
		{3, 3, ""},
		{20, 20, ""},
		{21, 20, "and 1 more"},
		{23, 20, "and 3 more"},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%d methods", tc.methods), func(t *testing.T) {
			dir := tree(t, map[string]string{"big.proto": bigProto(tc.methods)})
			r := compile(t, dir, []string{"big.proto"}, []string{"."})

			_, err := r.Resolve(t.Context(), "bigpkg.v1.Big/Absent")

			if !errors.Is(err, descriptor.ErrMethodNotFound) {
				t.Fatalf("err = %v, want descriptor.ErrMethodNotFound", err)
			}
			if !strings.Contains(err.Error(), "Absent") {
				t.Errorf("error %q does not name the method asked for", err)
			}
			if got := len(names.FindAllString(err.Error(), -1)); got != tc.listed {
				t.Errorf("error lists %d names, want %d:\n%s", got, tc.listed, err)
			}
			if tc.moreMessage == "" && strings.Contains(err.Error(), " more") {
				t.Errorf("error %q cuts nothing but says \"more\"", err)
			}
			if tc.moreMessage != "" && !strings.Contains(err.Error(), tc.moreMessage) {
				t.Errorf("error %q lacks %q", err, tc.moreMessage)
			}
		})
	}
}

// Ground: contract — a service the files lack is the same error as a method they lack.
func TestProtoResolver_AnUnknownServiceListsWhatWasFound(t *testing.T) {
	dir := tree(t, map[string]string{"shop.proto": shopProto})
	r := compile(t, dir, []string{"shop.proto"}, []string{"."})

	_, err := r.Resolve(t.Context(), "other.v1.Nothing/Get")

	if !errors.Is(err, descriptor.ErrMethodNotFound) {
		t.Fatalf("err = %v, want descriptor.ErrMethodNotFound", err)
	}
	for _, want := range []string{"other.v1.Nothing", "Shop"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// svcProto is a file of package pkg with one service Svc of the given methods,
// importing the given files.
func svcProto(pkg string, imports []string, methods ...string) string {
	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\npackage " + pkg + ";\n")
	for _, imp := range imports {
		fmt.Fprintf(&b, "import %q;\n", imp)
	}
	b.WriteString("message Req {}\nmessage Resp {}\nservice Svc {\n")
	for _, m := range methods {
		fmt.Fprintf(&b, "  rpc %s(Req) returns (Resp);\n", m)
	}
	b.WriteString("}\n")

	return b.String()
}

// The list is what can be called: a method of an imported file is in it, once,
// after the methods of the entry file. The imported package sorts before the
// entry one, so a plain sort of everything would put it first.
// Ground: decision — the list shows every method the files can call (brain, 2026-10-14).
func TestProtoResolver_AnUnknownMethodListsTheMethodsOfImportedFilesAfterTheEntryOnes(t *testing.T) {
	dir := tree(t, map[string]string{
		"a.proto": svcProto("zpkg.v1", []string{"b.proto"}, "ZetaOne", "ZetaTwo"),
		"b.proto": svcProto("bpkg.v1", nil, "BetaOne"),
	})
	r := compile(t, dir, []string{"a.proto"}, []string{"."})

	_, err := r.Resolve(t.Context(), "zpkg.v1.Svc/Absent")

	if !errors.Is(err, descriptor.ErrMethodNotFound) {
		t.Fatalf("err = %v, want descriptor.ErrMethodNotFound", err)
	}
	text := err.Error()
	for _, want := range []string{"zpkg.v1.Svc/ZetaOne", "zpkg.v1.Svc/ZetaTwo", "bpkg.v1.Svc/BetaOne"} {
		if got := strings.Count(text, want); got != 1 {
			t.Errorf("error %q lists %q %d times, want once", text, want, got)
		}
	}
	lastEntry := strings.LastIndex(text, "ZetaTwo")
	imported := strings.Index(text, "BetaOne")
	if lastEntry < 0 || imported < 0 || imported < lastEntry {
		t.Errorf("imported method must come after the entry file's methods:\n%s", text)
	}
}

// Characterization: what the list shows is callable.
// Ground: contract — a method of an imported file resolves.
func TestProtoResolver_AMethodOfAnImportedFileResolves(t *testing.T) {
	dir := tree(t, map[string]string{
		"a.proto": svcProto("apkg.v1", []string{"b.proto"}, "AlphaOne"),
		"b.proto": svcProto("bpkg.v1", nil, "BetaOne"),
	})
	r := compile(t, dir, []string{"a.proto"}, []string{"."})

	m, err := r.Resolve(t.Context(), "bpkg.v1.Svc/BetaOne")

	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if m.Path != "/bpkg.v1.Svc/BetaOne" {
		t.Errorf("path = %q, want /bpkg.v1.Svc/BetaOne", m.Path)
	}
}

// Imported files are listed sorted by full name, the same text every time:
// the registry's own order is not stable.
// Ground: decision — imported methods sorted by full name (brain, 2026-10-14).
func TestProtoResolver_ImportedMethodsAreListedSortedAndTheTextIsStable(t *testing.T) {
	files := map[string]string{
		"a.proto": svcProto("apkg.v1", []string{"z.proto", "m.proto", "c.proto"}, "AlphaOne"),
		"z.proto": svcProto("zpkg.v1", nil, "ZetaOne"),
		"m.proto": svcProto("mpkg.v1", nil, "MuOne"),
		"c.proto": svcProto("cpkg.v1", nil, "CeeOne"),
	}
	dir := tree(t, files)
	wantOrder := []string{"apkg.v1.Svc/AlphaOne", "cpkg.v1.Svc/CeeOne", "mpkg.v1.Svc/MuOne", "zpkg.v1.Svc/ZetaOne"}

	var first string
	for i := range 12 {
		r := compile(t, dir, []string{"a.proto"}, []string{"."})
		_, err := r.Resolve(t.Context(), "apkg.v1.Svc/Absent")
		if !errors.Is(err, descriptor.ErrMethodNotFound) {
			t.Fatalf("err = %v, want descriptor.ErrMethodNotFound", err)
		}
		text := err.Error()
		prev := -1
		for _, name := range wantOrder {
			at := strings.Index(text, name)
			if at < 0 || at < prev {
				t.Fatalf("run %d: %q missing or out of order (want %v):\n%s", i, name, wantOrder, text)
			}
			prev = at
		}
		if i == 0 {
			first = text
		} else if text != first {
			t.Fatalf("run %d: text differs from run 0:\n%s\n%s", i, text, first)
		}
	}
}

// Ground: contract — streaming is refused as it is under reflection.
func TestProtoResolver_AStreamingMethodIsRefused(t *testing.T) {
	dir := tree(t, map[string]string{"shop.proto": shopProto})
	r := compile(t, dir, []string{"shop.proto"}, []string{"."})

	_, err := r.Resolve(t.Context(), "shop.v1.Shop/Watch")

	if !errors.Is(err, descriptor.ErrStreamingUnsupported) {
		t.Errorf("err = %v, want descriptor.ErrStreamingUnsupported", err)
	}
}

// Ground: contract — a name that is not package.Service/Method is refused as it is under reflection.
func TestProtoResolver_AMalformedMethodNameIsRefused(t *testing.T) {
	dir := tree(t, map[string]string{"shop.proto": shopProto})
	r := compile(t, dir, []string{"shop.proto"}, []string{"."})

	for _, method := range []string{"Shop/Get", "shop.v1.Shop", "shop.v1.Shop/", ""} {
		if _, err := r.Resolve(t.Context(), method); !errors.Is(err, descriptor.ErrMalformedMethod) {
			t.Errorf("resolve %q: err = %v, want descriptor.ErrMalformedMethod", method, err)
		}
	}
}

// --- the types a payload may name --------------------------------------------

const anyProto = `syntax = "proto3";
package anytest.v1;
import "google/protobuf/any.proto";
message Item { string name = 1; }
message Envelope { google.protobuf.Any item = 1; }
message Ack {}
service Echo { rpc Say(Envelope) returns (Ack); }
`

// The Types of a method find the messages of its files and the well-known
// types, and nothing else: a message the test binary has registered globally
// but the files lack is not found. The wallet.v1 of the reference stand is
// registered in any binary that imports test/stand, which is why this test
// uses the health messages and asks for what the files do not hold.
// Ground: contract — types come from the files; only google.protobuf.* falls back to the global registry.
func TestProtoResolver_MethodTypesAreTheFilesAndTheWellKnownOnes(t *testing.T) {
	dir := tree(t, map[string]string{"any.proto": anyProto})
	r := compile(t, dir, []string{"any.proto"}, []string{"."})

	m, err := r.Resolve(t.Context(), "anytest.v1.Echo/Say")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if m.Types == nil {
		t.Fatal("Types is nil: a payload would resolve against the global registry")
	}

	found := map[string]string{
		"type.googleapis.com/anytest.v1.Item":          "anytest.v1.Item",
		"type.googleapis.com/google.protobuf.Duration": "google.protobuf.Duration",
	}
	for url, want := range found {
		mt, err := m.Types.FindMessageByURL(url)
		if err != nil {
			t.Errorf("find %s: %v", url, err)

			continue
		}
		if got := string(mt.Descriptor().FullName()); got != want {
			t.Errorf("find %s = %s, want %s", url, got, want)
		}
	}

	for _, url := range []string{
		"type.googleapis.com/anytest.v1.Nope",
		// Registered in this test binary, not in the files.
		"type.googleapis.com/grpc.health.v1.HealthCheckRequest",
	} {
		if _, err := m.Types.FindMessageByURL(url); !errors.Is(err, protoregistry.NotFound) {
			t.Errorf("find %s: err = %v, want protoregistry.NotFound", url, err)
		}
	}
}

// NewRequest parses with the same types as the CLI does.
// Ground: contract — a public method of pkg/descriptor must not parse an Any differently from the CLI.
func TestMethod_NewRequestParsesAnAnyOfTheFilesTypes(t *testing.T) {
	dir := tree(t, map[string]string{"any.proto": anyProto})
	r := compile(t, dir, []string{"any.proto"}, []string{"."})

	m, err := r.Resolve(t.Context(), "anytest.v1.Echo/Say")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	_, err = m.NewRequest([]byte(`{"item":{"@type":"type.googleapis.com/anytest.v1.Item","name":"x"}}`))
	if err != nil {
		t.Errorf("new request: %v", err)
	}
}

// --- protoset ---------------------------------------------------------------

func field(name string, num int32, kind descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
	f := &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(num),
		Type:   kind.Enum(),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
	}
	if typeName != "" {
		f.TypeName = proto.String(typeName)
	}

	return f
}

const (
	tString  = descriptorpb.FieldDescriptorProto_TYPE_STRING
	tInt64   = descriptorpb.FieldDescriptorProto_TYPE_INT64
	tMessage = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
)

func service(name, method, in, out string) *descriptorpb.ServiceDescriptorProto {
	return &descriptorpb.ServiceDescriptorProto{
		Name:   proto.String(name),
		Method: []*descriptorpb.MethodDescriptorProto{{Name: proto.String(method), InputType: proto.String(in), OutputType: proto.String(out)}},
	}
}

func depFile() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name: proto.String("dep.proto"), Package: proto.String("dep.v1"), Syntax: proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Money"), Field: []*descriptorpb.FieldDescriptorProto{field("units", 1, tInt64, "")},
		}},
	}
}

func mainFile() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name: proto.String("main.proto"), Package: proto.String("main.v1"), Syntax: proto.String("proto3"),
		Dependency: []string{"dep.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Req"), Field: []*descriptorpb.FieldDescriptorProto{field("amount", 1, tMessage, ".dep.v1.Money")}},
			{Name: proto.String("Resp")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{service("Pay", "Do", ".main.v1.Req", ".main.v1.Resp")},
	}
}

// protoset is the bytes protoc writes for --descriptor_set_out.
func protoset(t *testing.T, files ...*descriptorpb.FileDescriptorProto) []byte {
	t.Helper()

	b, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: files})
	if err != nil {
		t.Fatalf("marshal the set: %v", err)
	}

	return b
}

// Ground: contract — a protoset made with --include_imports resolves.
func TestProtosetResolver_ResolvesAMethodWhenTheSetHoldsItsDependencies(t *testing.T) {
	r, err := descriptor.NewProtosetResolver(protoset(t, depFile(), mainFile()))
	if err != nil {
		t.Fatalf("read the set: %v", err)
	}

	m, err := r.Resolve(t.Context(), "main.v1.Pay/Do")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := fieldMessage(m.Input, "amount"); got != "dep.v1.Money" {
		t.Errorf("amount is %q, want dep.v1.Money", got)
	}
}

// The set was made without --include_imports (ghz#327): the file it lacks and
// the command that fixes it are in the message.
// Ground: contract — the message names the missing file and the flag.
func TestProtosetResolver_ADependencyMissingFromTheSetNamesTheFileAndTheFix(t *testing.T) {
	_, err := descriptor.NewProtosetResolver(protoset(t, mainFile()))

	if !errors.Is(err, descriptor.ErrMissingDependency) {
		t.Fatalf("err = %v, want descriptor.ErrMissingDependency", err)
	}
	for _, want := range []string{"dep.proto", "--include_imports"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// Ground: contract — well-known types are built in, so a set that lacks them still resolves.
func TestProtosetResolver_WellKnownTypesMissingFromTheSetComeFromTheBuiltInCopies(t *testing.T) {
	file := &descriptorpb.FileDescriptorProto{
		Name: proto.String("wkt.proto"), Package: proto.String("wkt.v1"), Syntax: proto.String("proto3"),
		Dependency: []string{"google/protobuf/timestamp.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Req"), Field: []*descriptorpb.FieldDescriptorProto{field("at", 1, tMessage, ".google.protobuf.Timestamp")}},
			{Name: proto.String("Resp")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{service("Wkt", "Do", ".wkt.v1.Req", ".wkt.v1.Resp")},
	}

	r, err := descriptor.NewProtosetResolver(protoset(t, file))
	if err != nil {
		t.Fatalf("read the set: %v", err)
	}

	m, err := r.Resolve(t.Context(), "wkt.v1.Wkt/Do")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := fieldMessage(m.Input, "at"); got != "google.protobuf.Timestamp" {
		t.Errorf("at is %q, want google.protobuf.Timestamp", got)
	}
}

// protobuf-go reads edition 2024 where protocompile v0.14.1 does not, so a
// protoset is the way to a 2024 service (ghz#471).
// Ground: signal google.golang.org/protobuf v1.36.12 — protodesc builds an edition 2024 file.
func TestProtosetResolver_Edition2024Resolves(t *testing.T) {
	file := &descriptorpb.FileDescriptorProto{
		Name: proto.String("e2024.proto"), Package: proto.String("ed.v1"),
		Syntax: proto.String("editions"), Edition: descriptorpb.Edition_EDITION_2024.Enum(),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Req"), Field: []*descriptorpb.FieldDescriptorProto{field("name", 1, tString, "")}},
			{Name: proto.String("Resp")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{service("Ed", "Do", ".ed.v1.Req", ".ed.v1.Resp")},
	}
	if _, err := protodesc.NewFile(file, nil); err != nil {
		t.Fatalf("the fixture is not a valid edition 2024 file: %v", err)
	}

	r, err := descriptor.NewProtosetResolver(protoset(t, file))
	if err != nil {
		t.Fatalf("read the set: %v", err)
	}

	m, err := r.Resolve(t.Context(), "ed.v1.Ed/Do")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if m.Input.FullName() != "ed.v1.Req" {
		t.Errorf("input = %s, want ed.v1.Req", m.Input.FullName())
	}
}

// Ground: contract — bytes that are not a set, and no bytes at all (a protoc that failed
// leaves an empty file), are refused before any method is looked for.
func TestProtosetResolver_BytesThatAreNotASetAreRefused(t *testing.T) {
	for name, set := range map[string][]byte{
		"text":  []byte("this is not a descriptor set"),
		"empty": nil,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := descriptor.NewProtosetResolver(set); !errors.Is(err, descriptor.ErrBadProtoset) {
				t.Errorf("err = %v, want descriptor.ErrBadProtoset", err)
			}
		})
	}
}

// A protoset carries the types of its method the same way .proto files do
// (amendment 3: both paths use Method.Types). With none, protojson would use
// the global registry and an Any of a type of the set would not be found.
// Ground: contract — types come from the set; only google.protobuf.* falls back to the global registry.
func TestProtosetResolver_MethodTypesAreTheSetAndTheWellKnownOnes(t *testing.T) {
	r, err := descriptor.NewProtosetResolver(protoset(t, depFile(), mainFile()))
	if err != nil {
		t.Fatalf("read the set: %v", err)
	}
	m, err := r.Resolve(t.Context(), "main.v1.Pay/Do")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if m.Types == nil {
		t.Fatal("Types is nil: a payload would resolve against the global registry")
	}

	found := map[string]string{
		"type.googleapis.com/dep.v1.Money":             "dep.v1.Money",
		"type.googleapis.com/google.protobuf.Duration": "google.protobuf.Duration",
	}
	for url, want := range found {
		mt, err := m.Types.FindMessageByURL(url)
		if err != nil {
			t.Errorf("find %s: %v", url, err)

			continue
		}
		if got := string(mt.Descriptor().FullName()); got != want {
			t.Errorf("find %s = %s, want %s", url, got, want)
		}
	}

	// Registered in this test binary, not in the set.
	if _, err := m.Types.FindMessageByURL("type.googleapis.com/grpc.health.v1.HealthCheckRequest"); !errors.Is(err, protoregistry.NotFound) {
		t.Errorf("find HealthCheckRequest: err = %v, want protoregistry.NotFound", err)
	}
}

// Ground: contract — a method the set lacks lists what it has, as .proto files do.
func TestProtosetResolver_AnUnknownMethodListsWhatWasFound(t *testing.T) {
	r, err := descriptor.NewProtosetResolver(protoset(t, depFile(), mainFile()))
	if err != nil {
		t.Fatalf("read the set: %v", err)
	}

	_, err = r.Resolve(t.Context(), "main.v1.Pay/Absent")

	if !errors.Is(err, descriptor.ErrMethodNotFound) {
		t.Fatalf("err = %v, want descriptor.ErrMethodNotFound", err)
	}
	for _, want := range []string{"Absent", "Pay", "Do"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}
