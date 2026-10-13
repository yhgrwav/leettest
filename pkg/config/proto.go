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
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// UnmarshalYAML reads app.proto strictly and names the keys it has when one is
// misspelled (import_path, file): the parser's own «unknown field» does not.
func (p *ProtoSource) UnmarshalYAML(raw []byte) error {
	var keys map[string]any
	if err := yaml.Unmarshal(raw, &keys); err == nil {
		for key := range keys {
			if key == "files" || key == "import_paths" {
				continue
			}

			return fmt.Errorf("unknown key %q under app.proto: the keys are files and import_paths", key)
		}
	}

	type plain ProtoSource

	return yaml.UnmarshalWithOptions(raw, (*plain)(p), yaml.Strict())
}

// validateProto refuses app.proto and app.protoset that cannot name a source:
// both, or one with nothing in it. Neither is reflection, as before.
func (a *App) validateProto() error {
	var errs []error

	if a.Proto != nil && a.RawProtoset != nil {
		errs = append(errs, fmt.Errorf("%w: app.proto and app.protoset are both set; use one", ErrProtoSource))
	}
	if a.Proto != nil {
		if len(a.Proto.Files) == 0 {
			errs = append(errs, fmt.Errorf("%w: app.proto.files must list at least one .proto file", ErrProtoSource))
		}
		for _, f := range a.Proto.Files {
			if strings.TrimSpace(f) == "" {
				errs = append(errs, fmt.Errorf("%w: app.proto.files has a blank entry", ErrProtoSource))

				break
			}
		}
		for _, p := range a.Proto.ImportPaths {
			if strings.TrimSpace(p) == "" {
				errs = append(errs, fmt.Errorf("%w: app.proto.import_paths has a blank entry", ErrProtoSource))

				break
			}
		}
	}
	if a.RawProtoset != nil && strings.TrimSpace(*a.RawProtoset) == "" {
		errs = append(errs, fmt.Errorf("%w: app.protoset must be the path of a file", ErrProtoSource))
	}

	return errors.Join(errs...)
}

// resolveProto makes the paths of app.proto and app.protoset relative to dir,
// the directory of the config file (#191), and checks the files are there.
// Without import_paths the import path is dir, as protoc's -I. is. The paths
// as the config wrote them stay in RawProtoset; an error names both forms.
func (a *App) resolveProto(dir string) error {
	var errs []error

	if p := a.Proto; p != nil {
		for i, written := range p.Files {
			resolved := underDir(dir, written)
			if err := fileThere(resolved); err != nil {
				errs = append(errs, fmt.Errorf("%w: app.proto.files: %w (as written %q, resolved %s)", ErrProtoSource, err, written, resolved))
			}
			p.Files[i] = resolved
		}
		if len(p.ImportPaths) == 0 {
			p.ImportPaths = []string{dir}
		}
		for i, written := range p.ImportPaths {
			p.ImportPaths[i] = underDir(dir, written)
		}
	}

	if a.RawProtoset != nil {
		a.Protoset = underDir(dir, *a.RawProtoset)
		if err := fileThere(a.Protoset); err != nil {
			errs = append(errs, fmt.Errorf("%w: app.protoset: %w (as written %q, resolved %s)", ErrProtoSource, err, *a.RawProtoset, a.Protoset))
		}
	}

	return errors.Join(errs...)
}

func underDir(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(dir, path)
}

// fileThere says why path is not a file that can be read.
func fileThere(path string) error {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return errors.New("no such file")
	case err != nil:
		return err
	case info.IsDir():
		return errors.New("a directory, not a file")
	}

	return nil
}
