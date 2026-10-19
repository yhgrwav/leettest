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

package examples_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/test/stand"
	walletv1 "github.com/yhgrwav/leettest/test/stand/proto/wallet/v1"
)

// examples are the English examples, the names their ru copies share.
func examples(t *testing.T) []string {
	t.Helper()

	names, err := filepath.Glob("*.yaml")
	if err != nil || len(names) == 0 {
		t.Fatalf("no examples: %v", err)
	}

	return names
}

func loadExample(t *testing.T, path string) *config.MasterConfig {
	t.Helper()
	t.Setenv("LEETTEST_TOKEN", "demo")

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}

	return cfg
}

var durationType = reflect.TypeFor[time.Duration]()

// fieldPaths are the config's yaml paths, such as "load.calls[].timeout":
// every field a config file can set.
func fieldPaths(typ reflect.Type, prefix string) []string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch {
	case typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Struct:
		return fieldPaths(typ.Elem(), prefix+"[]")
	case typ.Kind() != reflect.Struct || typ == durationType:
		return []string{prefix}
	}

	var paths []string
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		if prefix != "" {
			name = prefix + "." + name
		}
		paths = append(paths, fieldPaths(typ.Field(i).Type, name)...)
	}

	return paths
}

// setIn says whether the parsed YAML sets path in any of its list items.
func setIn(node any, path []string) bool {
	if len(path) == 0 {
		return true
	}
	key, list := strings.CutSuffix(path[0], "[]")
	m, ok := node.(map[string]any)
	if !ok {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	if !list {
		return setIn(v, path[1:])
	}
	items, _ := v.([]any)

	return slices.ContainsFunc(items, func(item any) bool { return setIn(item, path[1:]) })
}

// Together the examples set every field of the config, each uncommented in at
// least one of them.
func TestExamples_CoverEveryField(t *testing.T) {
	var docs []any
	for _, name := range examples(t) {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		docs = append(docs, doc)
	}

	for _, path := range fieldPaths(reflect.TypeFor[config.MasterConfig](), "") {
		if !slices.ContainsFunc(docs, func(doc any) bool { return setIn(doc, strings.Split(path, ".")) }) {
			t.Errorf("no example sets %s", path)
		}
	}
}

// pass is a map in the config, so the field walk sees one leaf: the keys it
// takes are checked here, each set in some example.
func TestExamples_CoverEveryPassKey(t *testing.T) {
	setKeys := map[string]bool{}

	for _, name := range examples(t) {
		cfg := loadExample(t, name)
		for i := range cfg.Load.Calls {
			if raw := cfg.Load.Calls[i].RawPass; raw != nil {
				for key := range *raw {
					setKeys[key] = true
				}
			}
		}
		if b := cfg.Load.Breakpoint; b != nil && b.RawPass != nil {
			for key := range *b.RawPass {
				setKeys[key] = true
			}
		}
	}

	for _, key := range []string{"p50", "p90", "p95", "p99", "p999", "errors", "held_rps"} {
		if !setKeys[key] {
			t.Errorf("no example sets pass.%s", key)
		}
	}
}

// The Russian examples are the English ones with Russian comments: the same
// files, loading to the same config.
func TestExamples_LanguagesParseEqual(t *testing.T) {
	en := examples(t)
	ru, err := filepath.Glob("ru/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for i := range ru {
		ru[i] = filepath.Base(ru[i])
	}
	if !slices.Equal(en, ru) {
		t.Fatalf("examples %v, their ru copies %v", en, ru)
	}

	for _, name := range en {
		got, want := loadExample(t, filepath.Join("ru", name)), loadExample(t, name)
		// The ru example reaches the shared files from its own directory, so how
		// app.protoset is written differs; where it points (Protoset) must not.
		got.App.RawProtoset = want.App.RawProtoset
		// The files differ in their comments, so their hashes do.
		got.SHA256 = want.SHA256
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ru/%s loads to\n%+v\n%s loads to\n%+v", name, *got, name, *want)
		}
	}
}

// Each example, in each language, is a valid config.
func TestExamples_Load(t *testing.T) {
	for _, name := range examples(t) {
		loadExample(t, name)
		loadExample(t, filepath.Join("ru", name))
	}
}

// wallet.protoset is committed as built by protoc, so it is checked against the schema the stand
// really serves: a stand whose proto changed under the example would otherwise be run against a
// stale set, and the example would pass on a schema the stand no longer has.
func TestExamples_ProtosetIsTheStandSchema(t *testing.T) {
	raw, err := os.ReadFile("wallet.protoset")
	if err != nil {
		t.Fatal(err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &set); err != nil {
		t.Fatalf("wallet.protoset: %v", err)
	}

	byName := map[string]*descriptorpb.FileDescriptorProto{}
	for _, file := range set.GetFile() {
		byName[file.GetName()] = file
	}

	want := protodesc.ToFileDescriptorProto(walletv1.File_wallet_v1_wallet_proto)
	got, ok := byName[want.GetName()]
	if !ok {
		t.Fatalf("wallet.protoset has no %s, it holds %d files", want.GetName(), len(byName))
	}

	// Positions in the source are not part of the schema.
	want.SourceCodeInfo, got.SourceCodeInfo = nil, nil
	if !proto.Equal(got, want) {
		t.Errorf("%s in wallet.protoset differs from the stand's:\nset:   %v\nstand: %v", want.GetName(), got, want)
	}

	for _, dep := range want.GetDependency() {
		if _, ok := byName[dep]; !ok {
			t.Errorf("wallet.protoset lacks %s, which %s imports: build it with --include_imports", dep, want.GetName())
		}
	}
}

// The proto examples run against a stand that serves no reflection: each method they call
// resolves from the files alone, and the stand answers it through that schema.
func TestExamples_ProtoExamplesRunWithoutReflection(t *testing.T) {
	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bare := stand.StartBareOn(lis, nil)
	t.Cleanup(bare.Stop)

	conn, err := grpc.NewClient(bare.Target(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	for _, path := range []string{"proto.yaml", "protoset.yaml", "ru/proto.yaml", "ru/protoset.yaml"} {
		t.Run(path, func(t *testing.T) {
			cfg := loadExample(t, path)

			resolver, _, err := cli.FileResolver(cfg)
			if err != nil || resolver == nil {
				t.Fatalf("FileResolver: %v, resolver %v", err, resolver)
			}
			if len(cfg.Load.Calls) == 0 {
				t.Fatal("the example calls no method")
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			for _, call := range cfg.Load.Calls {
				method, err := resolver.Resolve(ctx, call.Method)
				if err != nil {
					t.Fatalf("%s: %v", call.Method, err)
				}

				body, err := cli.RequestBody(method.Input, call.Data)
				if err != nil {
					t.Fatalf("%s: %v", call.Method, err)
				}
				req, err := method.NewRequest(nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := proto.Unmarshal(body, req); err != nil {
					t.Fatal(err)
				}

				resp := method.NewResponse()
				if err := conn.Invoke(ctx, method.Path, req, resp); err != nil {
					t.Fatalf("%s on the stand without reflection: %v", call.Method, err)
				}
				if proto.Size(resp) == 0 {
					t.Errorf("%s: the stand answered an empty message", call.Method)
				}
			}
		})
	}
}

// The tour needs its token from the environment, and says so when it is
// missing rather than sending an empty one.
func TestExamples_TourNeedsItsToken(t *testing.T) {
	t.Setenv("LEETTEST_TOKEN", "")
	if err := os.Unsetenv("LEETTEST_TOKEN"); err != nil {
		t.Fatal(err)
	}

	_, err := config.LoadFile("tour.yaml")
	if err == nil || !strings.Contains(err.Error(), "export LEETTEST_TOKEN=<value>") {
		t.Errorf("tour.yaml without LEETTEST_TOKEN: %v", err)
	}
}

var yamlBlock = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// The README's run block is examples/leettest.yaml, in each language.
func TestReadme_BlockIsItsFile(t *testing.T) {
	for _, c := range []struct{ readme, heading, example string }{
		{"../README.md", "## Запуск", "ru/leettest.yaml"},
		{"../docs/en/README.md", "## Run", "leettest.yaml"},
	} {
		raw, err := os.ReadFile(c.readme)
		if err != nil {
			t.Fatal(err)
		}
		_, section, ok := strings.Cut(string(raw), "\n"+c.heading+"\n")
		if !ok {
			t.Fatalf("%s has no %q", c.readme, c.heading)
		}
		block := yamlBlock.FindStringSubmatch(section)
		if block == nil {
			t.Fatalf("%s: no yaml block under %q", c.readme, c.heading)
		}
		want, err := os.ReadFile(c.example)
		if err != nil {
			t.Errorf("%s: %v", c.readme, err)

			continue
		}
		if block[1] != string(want) {
			t.Errorf("%s %q block differs from %s:\n%s\nfile:\n%s", c.readme, c.heading, c.example, block[1], want)
		}
	}
}

var (
	secretKey = regexp.MustCompile(`(?i)auth|token|key|secret|password|cookie`)
	// envOnly is a value built from ${NAME} alone, after a scheme word.
	envOnly = regexp.MustCompile(`^((Bearer|Basic) )?(\$\{[A-Za-z_][A-Za-z0-9_]*\})+$`)
)

// No example carries a secret: credentials come from the environment, and no
// key or certificate is written into an example.
func TestExamples_NoSecrets(t *testing.T) {
	t.Setenv("LEETTEST_TOKEN", "demo")

	files, err := filepath.Glob("*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ru, err := filepath.Glob("ru/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range append(files, ru...) {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "-----BEGIN") {
			t.Errorf("%s holds a PEM block", name)
		}
		cfg, err := config.Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for key, value := range cfg.App.RawMetadata {
			if secretKey.MatchString(key) && !envOnly.MatchString(value) {
				t.Errorf("%s: metadata %q is %q, want it from ${...}", name, key, value)
			}
		}
	}
}
