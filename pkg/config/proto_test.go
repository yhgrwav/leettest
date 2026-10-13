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

package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// protoYAML is a valid config whose app section carries appExtra, indented as
// fields of app.
func protoYAML(appExtra string) []byte {
	return []byte("app:\n  target:\n    ip: localhost\n    port: 50051\n" + appExtra +
		"load:\n  calls:\n    - method: pkg.v1.Service/Method\n      rps: 10\n      duration: 1s\n")
}

// writeProtoConfig writes appExtra into leettest.yaml of a fresh directory,
// with every name of touch created as an empty file (directories on the way
// included), and returns the directory and the config's path.
func writeProtoConfig(t *testing.T, appExtra string, touch ...string) (dir, path string) {
	t.Helper()

	dir = t.TempDir()
	for _, name := range touch {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	path = filepath.Join(dir, "leettest.yaml")
	if err := os.WriteFile(path, protoYAML(appExtra), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return dir, path
}

// Ground: contract — the two keys of app that name where methods come from.
func TestParse_ProtoAndProtosetTogetherAreRefused(t *testing.T) {
	_, err := config.Parse(protoYAML("  proto:\n    files: [a.proto]\n  protoset: a.protoset\n"))

	if !errors.Is(err, config.ErrProtoSource) {
		t.Fatalf("err = %v, want config.ErrProtoSource", err)
	}
	for _, key := range []string{"app.proto", "app.protoset"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error %q does not name %s", err, key)
		}
	}
}

// Ground: contract — a source with nothing in it is a mistake, not "reflection as today".
func TestParse_ProtoSourceWithNothingInItIsRefused(t *testing.T) {
	tests := []struct {
		name, app, key string
	}{
		{"an empty files list", "  proto:\n    files: []\n", "app.proto.files"},
		{"proto with only import paths", "  proto:\n    import_paths: [api]\n", "app.proto.files"},
		{"a blank entry among the files", "  proto:\n    files: [a.proto, '']\n", "app.proto.files"},
		{"a blank protoset path", "  protoset: ''\n", "app.protoset"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse(protoYAML(tc.app))

			if !errors.Is(err, config.ErrProtoSource) {
				t.Fatalf("err = %v, want config.ErrProtoSource", err)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("error %q does not name %s", err, tc.key)
			}
		})
	}
}

// Ground: contract — the shape of the keys; Parse keeps the paths as written.
func TestParse_ProtoKeysAreRead(t *testing.T) {
	cfg, err := config.Parse(protoYAML("  proto:\n    files: [api/user/v1/user.proto]\n    import_paths: [api, third_party]\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := cfg.App.Proto; got == nil ||
		!slices.Equal(got.Files, []string{"api/user/v1/user.proto"}) ||
		!slices.Equal(got.ImportPaths, []string{"api", "third_party"}) {
		t.Errorf("proto = %+v, want the files and import paths as written", got)
	}

	cfg, err = config.Parse(protoYAML("  protoset: build/api.protoset\n"))
	if err != nil {
		t.Fatalf("parse protoset: %v", err)
	}
	if got := cfg.App.RawProtoset; got == nil || *got != "build/api.protoset" {
		t.Errorf("protoset = %v, want build/api.protoset", got)
	}

	cfg, err = config.Parse(protoYAML(""))
	if err != nil {
		t.Fatalf("parse neither: %v", err)
	}
	if cfg.App.Proto != nil || cfg.App.RawProtoset != nil {
		t.Errorf("neither key set, got proto %v, protoset %v: reflection is the default", cfg.App.Proto, cfg.App.RawProtoset)
	}
}

// A typo of a key under proto: names the key that was meant. The YAML parser's
// own "unknown field" is not a hint.
// Ground: contract — the hint names the key.
func TestParse_ATypoUnderProtoNamesTheRightKey(t *testing.T) {
	tests := map[string]struct{ app, want string }{
		"import_path": {"  proto:\n    files: [a.proto]\n    import_path: [api]\n", "import_paths"},
		"file":        {"  proto:\n    file: [a.proto]\n", "files"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := config.Parse(protoYAML(tc.app))

			if !errors.Is(err, config.ErrReadConfig) {
				t.Fatalf("err = %v, want config.ErrReadConfig", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name %q as the key to use", err, tc.want)
			}
		})
	}
}

// The same rule as the data file (#191): relative to the directory of the
// config, whatever the working directory is.
// Ground: contract — paths of app.proto resolve like the dataset's.
func TestLoadFile_ProtoPathsAreRelativeToTheConfigDirectory(t *testing.T) {
	dir, path := writeProtoConfig(t,
		"  proto:\n    files: [api/user/v1/user.proto]\n    import_paths: [api, third_party]\n",
		"api/user/v1/user.proto", "third_party/keep")

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	wantFiles := []string{filepath.Join(dir, "api", "user", "v1", "user.proto")}
	wantImports := []string{filepath.Join(dir, "api"), filepath.Join(dir, "third_party")}
	if cfg.App.Proto == nil || !slices.Equal(cfg.App.Proto.Files, wantFiles) {
		t.Errorf("files = %+v, want %v", cfg.App.Proto, wantFiles)
	}
	if cfg.App.Proto == nil || !slices.Equal(cfg.App.Proto.ImportPaths, wantImports) {
		t.Errorf("import paths = %+v, want %v", cfg.App.Proto, wantImports)
	}
}

// Without import_paths the import path is the config's directory, as protoc's
// -I. is. LoadFile puts it there, like every path from that directory (#191);
// Parse has no directory and leaves the list empty.
// Ground: contract — the default import path is set where paths are resolved.
func TestLoadFile_NoImportPathsMeansTheConfigDirectory(t *testing.T) {
	dir, path := writeProtoConfig(t, "  proto:\n    files: [svc.proto]\n", "svc.proto")

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.App.Proto == nil || !slices.Equal(cfg.App.Proto.ImportPaths, []string{dir}) {
		t.Errorf("import paths = %+v, want [%s]", cfg.App.Proto, dir)
	}

	parsed, err := config.Parse(protoYAML("  proto:\n    files: [svc.proto]\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.App.Proto.ImportPaths) != 0 {
		t.Errorf("Parse import paths = %v, want none: it has no directory", parsed.App.Proto.ImportPaths)
	}
}

// Ground: contract — an absolute path is used as it is.
func TestLoadFile_AbsoluteProtoPathsStayAsTheyAre(t *testing.T) {
	elsewhere := t.TempDir()
	proto := filepath.Join(elsewhere, "svc.proto")
	set := filepath.Join(elsewhere, "api.protoset")
	for _, f := range []string{proto, set} {
		if err := os.WriteFile(f, nil, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// A single-quoted YAML scalar keeps a Windows path's backslashes.
	_, path := writeProtoConfig(t, "  proto:\n    files: ['"+proto+"']\n    import_paths: ['"+elsewhere+"']\n")
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("load proto: %v", err)
	}
	if cfg.App.Proto == nil || !slices.Equal(cfg.App.Proto.Files, []string{proto}) ||
		!slices.Equal(cfg.App.Proto.ImportPaths, []string{elsewhere}) {
		t.Errorf("proto = %+v, want the absolute paths unchanged", cfg.App.Proto)
	}

	_, path = writeProtoConfig(t, "  protoset: '"+set+"'\n")
	cfg, err = config.LoadFile(path)
	if err != nil {
		t.Fatalf("load protoset: %v", err)
	}
	if cfg.App.Protoset != set {
		t.Errorf("protoset = %q, want %q", cfg.App.Protoset, set)
	}
}

// The path as written is what the report prints («methods: protoset <path>»),
// the resolved one is what is opened.
// Ground: contract — both forms of the protoset path.
func TestLoadFile_ProtosetKeepsTheWrittenPathAndResolvesTheOther(t *testing.T) {
	dir, path := writeProtoConfig(t, "  protoset: build/api.protoset\n", "build/api.protoset")

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.App.RawProtoset == nil || *cfg.App.RawProtoset != "build/api.protoset" {
		t.Errorf("as written = %v, want build/api.protoset", cfg.App.RawProtoset)
	}
	if want := filepath.Join(dir, "build", "api.protoset"); cfg.App.Protoset != want {
		t.Errorf("resolved = %q, want %q", cfg.App.Protoset, want)
	}
}

// A file that is not there is a config error naming the path as written and
// as resolved. The written path is dirty on purpose: a clean one is a suffix
// of the resolved one, and either would pass for both.
// Ground: contract — the message names both paths.
func TestLoadFile_AMissingProtoFileNamesBothPaths(t *testing.T) {
	const written = "./api/../api/missing.proto"

	dir, path := writeProtoConfig(t, "  proto:\n    files: ["+written+"]\n")

	_, err := config.LoadFile(path)

	if !errors.Is(err, config.ErrProtoSource) {
		t.Fatalf("err = %v, want config.ErrProtoSource", err)
	}
	resolved := filepath.Join(dir, "api", "missing.proto")
	for _, want := range []string{written, resolved} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// Ground: contract — the same for the protoset.
func TestLoadFile_AMissingProtosetNamesBothPaths(t *testing.T) {
	const written = "./build/../build/api.protoset"

	dir, path := writeProtoConfig(t, "  protoset: "+written+"\n")

	_, err := config.LoadFile(path)

	if !errors.Is(err, config.ErrProtoSource) {
		t.Fatalf("err = %v, want config.ErrProtoSource", err)
	}
	resolved := filepath.Join(dir, "build", "api.protoset")
	for _, want := range []string{written, resolved} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}
