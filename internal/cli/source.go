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

package cli

import (
	"fmt"
	"os"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
)

// Where a run's methods came from, as the JSON report names it.
const (
	SourceReflection = "reflection"
	SourceProto      = "proto"
	SourceProtoset   = "protoset"
)

// MethodsSource says where the methods of a run were learned from.
type MethodsSource struct {
	// Kind is SourceReflection, SourceProto or SourceProtoset.
	Kind string
	// Files is the number of entry files of app.proto.files.
	Files int
	// Path is app.protoset as the config wrote it.
	Path string
}

// ReflectionSource is the source of a run that asked the target.
func ReflectionSource() *MethodsSource { return &MethodsSource{Kind: SourceReflection} }

// Line is the text report's line under the one that names the target.
func (s *MethodsSource) Line() string {
	switch s.Kind {
	case SourceProto:
		if s.Files == 1 {
			return "methods: 1 .proto file"
		}

		return fmt.Sprintf("methods: %d .proto files", s.Files)
	case SourceProtoset:
		return "methods: protoset " + s.Path
	}

	return "methods: server reflection"
}

// kind is the JSON value: nil, which is null, when nothing was resolved.
func (s *MethodsSource) kind() *string {
	if s == nil {
		return nil
	}

	return &s.Kind
}

// FileResolver reads the files cfg names in app.proto or app.protoset, with no
// call to the target. A config that names none gets a nil resolver and source:
// its methods come from reflection.
func FileResolver(cfg *config.MasterConfig) (descriptor.Resolver, *MethodsSource, error) {
	switch {
	case cfg.App.Proto != nil:
		r, err := descriptor.NewProtoResolver(cfg.App.Proto.Files, cfg.App.Proto.ImportPaths)
		if err != nil {
			return nil, nil, err
		}

		return r, &MethodsSource{Kind: SourceProto, Files: len(cfg.App.Proto.Files)}, nil
	case cfg.App.RawProtoset != nil:
		raw, err := os.ReadFile(cfg.App.Protoset)
		if err != nil {
			return nil, nil, fmt.Errorf("app.protoset: %w", err)
		}

		r, err := descriptor.NewProtosetResolver(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("app.protoset %s: %w", *cfg.App.RawProtoset, err)
		}

		return r, &MethodsSource{Kind: SourceProtoset, Path: *cfg.App.RawProtoset}, nil
	}

	return nil, nil, nil
}
