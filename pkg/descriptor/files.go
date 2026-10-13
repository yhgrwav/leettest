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

	"google.golang.org/protobuf/reflect/protoregistry"
)

// Stubs of the file-based resolvers: every call fails with ErrNotImplemented
// until the code of the unit lands.
var (
	ErrNotImplemented = errors.New("not implemented")
	// ErrCompile is a .proto file that did not compile: a syntax or type error,
	// an import not found, an edition the compiler does not know. The text
	// carries file:line:col where the compiler gave one.
	ErrCompile = errors.New("cannot compile the .proto files")
	// ErrOutsideImportPaths is an entry file under none of the import paths.
	ErrOutsideImportPaths = errors.New("the .proto file is not under any import path")
	// ErrMissingDependency is a protoset that lacks a file one of its files imports.
	ErrMissingDependency = errors.New("the protoset lacks a file its files import")
	// ErrBadProtoset is bytes that are not a FileDescriptorSet.
	ErrBadProtoset = errors.New("not a protoset")
)

// Types resolves the message types a payload may name.
type Types interface {
	protoregistry.MessageTypeResolver
	protoregistry.ExtensionTypeResolver
}

// FileResolver resolves methods from compiled .proto files or a protoset, with
// no call to the target.
type FileResolver struct{}

// NewProtoResolver compiles the entry files. files are paths of files on disk,
// each under one of importPaths; their imports are looked up on importPaths.
func NewProtoResolver(files, importPaths []string) (*FileResolver, error) {
	return nil, ErrNotImplemented
}

// NewProtosetResolver reads a serialized FileDescriptorSet.
func NewProtosetResolver(set []byte) (*FileResolver, error) {
	return nil, ErrNotImplemented
}

func (r *FileResolver) Resolve(ctx context.Context, method string) (*Method, error) {
	return nil, ErrNotImplemented
}

func (r *FileResolver) ResolveAll(ctx context.Context, methods []string) (map[string]*Method, error) {
	return nil, ErrNotImplemented
}
