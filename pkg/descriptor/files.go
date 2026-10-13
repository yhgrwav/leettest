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

package descriptor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	// The well-known types a protoset may leave out: their built-in copies.
	_ "google.golang.org/protobuf/types/known/anypb"
	_ "google.golang.org/protobuf/types/known/durationpb"
	_ "google.golang.org/protobuf/types/known/emptypb"
	_ "google.golang.org/protobuf/types/known/fieldmaskpb"
	_ "google.golang.org/protobuf/types/known/structpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"
	_ "google.golang.org/protobuf/types/known/wrapperspb"
)

var (
	// ErrCompile is a .proto file that did not compile: a syntax or type error,
	// an import not found, an edition the compiler does not know. The text
	// carries file:line:col where the compiler gave one.
	ErrCompile = errors.New("cannot compile the .proto files")
	// ErrOutsideImportPaths is an entry file under none of the import paths.
	ErrOutsideImportPaths = errors.New("the .proto file is not under any import path")
	// ErrShadowedInput is an entry file that an earlier import path shadows with
	// a different file of the same name.
	ErrShadowedInput = errors.New("the .proto file is shadowed by an earlier import path")
	// ErrMissingDependency is a protoset that lacks a file one of its files imports.
	ErrMissingDependency = errors.New("the protoset lacks a file its files import")
	// ErrBadProtoset is bytes that are not a usable FileDescriptorSet.
	ErrBadProtoset = errors.New("not a protoset")
)

// listedNames is how many method names an unknown-method error lists.
const listedNames = 20

// protosetHint is the other way to a schema, said where the .proto route fails.
const protosetHint = "build a protoset with protoc --include_imports --descriptor_set_out=FILE and use it instead"

// Types resolves the message types a payload may name.
type Types interface {
	protoregistry.MessageTypeResolver
	protoregistry.ExtensionTypeResolver
}

// FileResolver resolves methods from compiled .proto files or a protoset, with
// no call to the target.
type FileResolver struct {
	reg *protoregistry.Files
	// files are the files the user gave or the set holds, in the order they came:
	// what an unknown-method error lists.
	files []protoreflect.FileDescriptor
	types Types
}

func newFileResolver(reg *protoregistry.Files, files []protoreflect.FileDescriptor) *FileResolver {
	return &FileResolver{reg: reg, files: files, types: fileTypes{dyn: dynamicpb.NewTypes(reg)}}
}

// NewProtoResolver compiles the entry files. files are paths of files on disk,
// each under one of importPaths; their imports are looked up on importPaths,
// and the well-known types need no path. A file is named by its path under the
// first import path that holds it, as protoc names it, so one file is one name
// whether listed or imported. No import paths hold no file.
func NewProtoResolver(files, importPaths []string) (*FileResolver, error) {
	names := make([]string, len(files))
	for i, file := range files {
		name, ok := nameUnder(file, importPaths)
		if !ok {
			return nil, fmt.Errorf("%w: %s; import paths: %s", ErrOutsideImportPaths, file, listPaths(importPaths))
		}
		names[i] = name
	}

	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: importPaths}),
	}

	compiled, err := compiler.Compile(context.Background(), names...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrCompile, compileMessage(err, importPaths))
	}

	reg := new(protoregistry.Files)
	entries := make([]protoreflect.FileDescriptor, 0, len(compiled))
	for _, f := range compiled {
		if err := register(reg, f); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCompile, err)
		}
		entries = append(entries, f)
	}

	return newFileResolver(reg, entries), nil
}

// nameUnder is file's path relative to the first of importPaths that holds it,
// with forward slashes, which is how an import spells it.
func nameUnder(file string, importPaths []string) (string, bool) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", false
	}

	for _, ip := range importPaths {
		root, err := filepath.Abs(ip)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}

		return filepath.ToSlash(rel), true
	}

	return "", false
}

func listPaths(paths []string) string {
	if len(paths) == 0 {
		return "none"
	}

	return strings.Join(paths, ", ")
}

// compileMessage is the compiler's message, with what to do next where the
// compiler cannot know it: an import not found, an edition it does not read.
func compileMessage(err error, importPaths []string) string {
	msg := err.Error()

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("%s; looked in the import paths: %s; add the directory that holds it, or %s",
			msg, listPaths(importPaths), protosetHint)
	case strings.Contains(msg, "edition") && strings.Contains(msg, "not recognized"):
		return fmt.Sprintf("%s; this compiler reads editions up to 2023; %s", msg, protosetHint)
	}

	return msg
}

// register adds f and, before it, everything it imports.
func register(reg *protoregistry.Files, f protoreflect.FileDescriptor) error {
	if _, err := reg.FindFileByPath(f.Path()); err == nil {
		return nil
	}

	imports := f.Imports()
	for i := range imports.Len() {
		if err := register(reg, imports.Get(i).FileDescriptor); err != nil {
			return err
		}
	}

	return reg.RegisterFile(f)
}

// NewProtosetResolver reads a serialized FileDescriptorSet, as protoc writes
// with --descriptor_set_out. A file the set imports but does not hold is an
// error, except the well-known types, which have built-in copies.
func NewProtosetResolver(set []byte) (*FileResolver, error) {
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(set, &fds); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadProtoset, err)
	}
	if len(fds.GetFile()) == 0 {
		return nil, fmt.Errorf("%w: the set holds no files", ErrBadProtoset)
	}

	b := &setBuilder{
		byName: make(map[string]*descriptorpb.FileDescriptorProto, len(fds.GetFile())),
		built:  make(map[string]protoreflect.FileDescriptor),
		active: make(map[string]bool),
		reg:    new(protoregistry.Files),
	}
	for _, f := range fds.GetFile() {
		b.byName[f.GetName()] = f
	}

	files := make([]protoreflect.FileDescriptor, 0, len(fds.GetFile()))
	for _, f := range fds.GetFile() {
		fd, err := b.build(f.GetName())
		if err != nil {
			return nil, err
		}
		files = append(files, fd)
	}

	return newFileResolver(b.reg, files), nil
}

// setBuilder builds the files of a set in dependency order, whatever order the
// set lists them in.
type setBuilder struct {
	byName map[string]*descriptorpb.FileDescriptorProto
	built  map[string]protoreflect.FileDescriptor
	active map[string]bool
	reg    *protoregistry.Files
}

func (b *setBuilder) build(name string) (protoreflect.FileDescriptor, error) {
	if fd, ok := b.built[name]; ok {
		return fd, nil
	}
	if b.active[name] {
		return nil, fmt.Errorf("%w: %s imports itself through a cycle", ErrBadProtoset, name)
	}
	b.active[name] = true
	defer delete(b.active, name)

	file := b.byName[name]
	for _, dep := range file.GetDependency() {
		if _, ok := b.byName[dep]; ok {
			if _, err := b.build(dep); err != nil {
				return nil, err
			}

			continue
		}
		if builtin, err := protoregistry.GlobalFiles.FindFileByPath(dep); err == nil && strings.HasPrefix(dep, "google/protobuf/") {
			if err := register(b.reg, builtin); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrBadProtoset, err)
			}
			b.built[dep] = builtin

			continue
		}

		return nil, fmt.Errorf("%w: %s imports %s, which is not in the set; build the set with protoc --include_imports",
			ErrMissingDependency, name, dep)
	}

	fd, err := protodesc.NewFile(file, b.reg)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrBadProtoset, name, err)
	}
	if err := b.reg.RegisterFile(fd); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrBadProtoset, name, err)
	}
	b.built[name] = fd

	return fd, nil
}

// Resolve finds package.Service/Method in the files.
func (r *FileResolver) Resolve(ctx context.Context, method string) (*Method, error) {
	resolved, err := r.ResolveAll(ctx, []string{method})
	if err != nil {
		return nil, err
	}

	return resolved[method], nil
}

// ResolveAll finds every method asked for, or fails on the first that is not
// there. It reads memory only.
func (r *FileResolver) ResolveAll(_ context.Context, methods []string) (map[string]*Method, error) {
	resolved := make(map[string]*Method, len(methods))
	for _, method := range methods {
		if _, done := resolved[method]; done {
			continue
		}

		m, err := r.resolveOne(method)
		if err != nil {
			return nil, err
		}
		resolved[method] = m
	}

	return resolved, nil
}

func (r *FileResolver) resolveOne(method string) (*Method, error) {
	service, name, err := splitMethod(method)
	if err != nil {
		return nil, err
	}

	var md protoreflect.MethodDescriptor
	if d, err := r.reg.FindDescriptorByName(protoreflect.FullName(service)); err == nil {
		if sd, ok := d.(protoreflect.ServiceDescriptor); ok {
			md = sd.Methods().ByName(protoreflect.Name(name))
		}
	}
	if md == nil {
		return nil, fmt.Errorf("%w: %s; the files have %s", ErrMethodNotFound, method, r.found())
	}

	if md.IsStreamingClient() || md.IsStreamingServer() {
		return nil, fmt.Errorf("%w: %s", ErrStreamingUnsupported, method)
	}

	return &Method{Path: "/" + method, Input: md.Input(), Output: md.Output(), Types: r.types}, nil
}

// found lists the methods of the files, listedNames of them and how many more.
func (r *FileResolver) found() string {
	var names []string
	for _, f := range r.files {
		services := f.Services()
		for i := range services.Len() {
			methods := services.Get(i).Methods()
			for j := range methods.Len() {
				names = append(names, string(services.Get(i).FullName())+"/"+string(methods.Get(j).Name()))
			}
		}
	}

	switch {
	case len(names) == 0:
		return "no services"
	case len(names) > listedNames:
		return fmt.Sprintf("%s and %d more", strings.Join(names[:listedNames], ", "), len(names)-listedNames)
	}

	return strings.Join(names, ", ")
}

// fileTypes finds the types of the files; of the global registry only the
// well-known ones, which a file need not import for an Any to name them.
type fileTypes struct {
	dyn *dynamicpb.Types
}

const wellKnownPrefix = "google.protobuf."

func (t fileTypes) FindMessageByName(name protoreflect.FullName) (protoreflect.MessageType, error) {
	mt, err := t.dyn.FindMessageByName(name)
	if errors.Is(err, protoregistry.NotFound) && strings.HasPrefix(string(name), wellKnownPrefix) {
		return protoregistry.GlobalTypes.FindMessageByName(name)
	}

	return mt, err
}

// FindMessageByURL reads the type name after the last slash, as Any defines.
func (t fileTypes) FindMessageByURL(url string) (protoreflect.MessageType, error) {
	if i := strings.LastIndexByte(url, '/'); i >= 0 {
		url = url[i+1:]
	}

	return t.FindMessageByName(protoreflect.FullName(url))
}

func (t fileTypes) FindExtensionByName(name protoreflect.FullName) (protoreflect.ExtensionType, error) {
	return t.dyn.FindExtensionByName(name)
}

func (t fileTypes) FindExtensionByNumber(message protoreflect.FullName, field protoreflect.FieldNumber) (protoreflect.ExtensionType, error) {
	return t.dyn.FindExtensionByNumber(message, field)
}
