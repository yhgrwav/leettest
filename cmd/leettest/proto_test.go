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

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/descriptor"
)

// healthProto is the health service of grpc-go, as a .proto: the targets of
// these tests serve it without reflection, so a run by file is the only way
// to learn its schema.
const healthProto = `syntax = "proto3";
package grpc.health.v1;
message HealthCheckRequest { string service = 1; }
message HealthCheckResponse {
  enum ServingStatus { UNKNOWN = 0; SERVING = 1; NOT_SERVING = 2; SERVICE_UNKNOWN = 3; }
  ServingStatus status = 1;
}
service Health { rpc Check(HealthCheckRequest) returns (HealthCheckResponse); }
`

// otherProto is a second entry file with no service in it.
const otherProto = `syntax = "proto3";
package other.v1;
message Thing { string id = 1; }
`

// layout writes files (name with forward slashes, content) under a fresh
// directory and returns it.
func layout(t *testing.T, files map[string]string) string {
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

// writeHealthProtoset writes the protoset of grpc-go's health service, made
// from the descriptor its generated code carries, to dir/api.protoset.
func writeHealthProtoset(t *testing.T, dir string) {
	t.Helper()

	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		protodesc.ToFileDescriptorProto(grpc_health_v1.File_grpc_health_v1_health_proto),
	}}
	raw, err := proto.Marshal(set)
	if err != nil {
		t.Fatalf("marshal the protoset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "api.protoset"), raw, 0o600); err != nil {
		t.Fatalf("write the protoset: %v", err)
	}
}

// protoConfig writes leettest.yaml into dir: the target, appLines as fields of
// app, and loadLines as the body of load (already indented).
func protoConfig(t *testing.T, dir, addr, appLines, loadLines string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	cfg := fmt.Sprintf("app:\n  target:\n    ip: %s\n    port: %s\n  tls: false\n%sload:\n%s", host, port, appLines, loadLines)

	path := filepath.Join(dir, "leettest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// checkLoad is one call of the health Check with callLines (fields of the
// call) after it.
func checkLoad(callLines string) string {
	return "  calls:\n    - method: " + checkMethod + "\n      rps: 50\n      duration: 300ms\n" + callLines
}

// searchLoad is a breaking-point search over the health Check.
func searchLoad() string {
	return "  calls:\n    - method: " + checkMethod + "\n      timeout: 200ms\n" + quickSearch
}

const (
	filesApp    = "  proto:\n    files: [health.proto]\n"
	twoFilesApp = "  proto:\n    files: [health.proto, other.proto]\n"
	protosetApp = "  protoset: api.protoset\n"
	serviceData = "      data:\n        service: wallet\n"
)

// servedService is the service field of the last Check the target saw.
func servedService(target liveTarget) string {
	target.health.mu.Lock()
	defer target.health.mu.Unlock()

	return target.health.service
}

// --- a run by file ------------------------------------------------------------

// The target has no reflection: the schema can only have come from the file.
// The data reaches it encoded, which is the proof the run used the schema.
// Ground: contract — end to end, a run by .proto and by protoset on a target without reflection.
func TestRun_FilesReplaceReflection(t *testing.T) {
	tests := map[string]struct {
		app   string
		files map[string]string
		set   bool
	}{
		"a .proto file": {app: filesApp, files: map[string]string{"health.proto": healthProto}},
		"a protoset":    {app: protosetApp, set: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			target := startTarget(t)
			dir := layout(t, tc.files)
			if tc.set {
				writeHealthProtoset(t, dir)
			}
			cfg := protoConfig(t, dir, target.addr, tc.app, checkLoad(serviceData))

			res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)

			if res.err != nil {
				t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
			}
			if got := servedService(target); got != "wallet" {
				t.Errorf("target received service = %q, want wallet from data", got)
			}
			if strings.Contains(res.stderr, "not checked") {
				t.Errorf("stderr says a method was not checked, but the files hold it:\n%s", res.stderr)
			}
		})
	}
}

// With no import_paths the config's directory is the import path, as protoc's
// -I. is: the import is spelled from it, whatever directory leettest runs in.
// Ground: contract — the default import path is the config file's directory.
func TestRun_ProtoImportsAreFoundFromTheConfigDirectoryByDefault(t *testing.T) {
	target := startTarget(t)
	dir := layout(t, map[string]string{
		"api/health_types.proto": `syntax = "proto3";
package grpc.health.v1;
message HealthCheckRequest { string service = 1; }
message HealthCheckResponse { int32 status = 1; }
`,
		"api/health.proto": `syntax = "proto3";
package grpc.health.v1;
import "api/health_types.proto";
service Health { rpc Check(HealthCheckRequest) returns (HealthCheckResponse); }
`,
	})
	cfg := protoConfig(t, dir, target.addr, "  proto:\n    files: [api/health.proto]\n", checkLoad(serviceData))

	res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)

	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	if got := servedService(target); got != "wallet" {
		t.Errorf("target received service = %q, want wallet", got)
	}
}

// Ground: contract — import_paths are relative to the config file, and an entry file is named under them.
func TestRun_ProtoImportPathsAreRelativeToTheConfigDirectory(t *testing.T) {
	target := startTarget(t)
	dir := layout(t, map[string]string{
		"api/health_types.proto": `syntax = "proto3";
package grpc.health.v1;
message HealthCheckRequest { string service = 1; }
message HealthCheckResponse { int32 status = 1; }
`,
		"api/health.proto": `syntax = "proto3";
package grpc.health.v1;
import "health_types.proto";
service Health { rpc Check(HealthCheckRequest) returns (HealthCheckResponse); }
`,
	})
	cfg := protoConfig(t, dir, target.addr, "  proto:\n    files: [api/health.proto]\n    import_paths: [api]\n", checkLoad(serviceData))

	res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)

	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	if got := servedService(target); got != "wallet" {
		t.Errorf("target received service = %q, want wallet", got)
	}
}

// The target answers reflection with a schema whose field is called service;
// the user's file calls it svc (same number). A run that asked the target
// first would take the target's schema and refuse svc as an unknown field.
// Ground: contract — with proto set no reflection call is made, even where the target offers it.
func TestRun_FilesAreUsedEvenWhereTheTargetOffersReflection(t *testing.T) {
	target := startReflectingTarget(t)
	dir := layout(t, map[string]string{"health.proto": strings.Replace(healthProto, "string service = 1;", "string svc = 1;", 1)})
	cfg := protoConfig(t, dir, target.addr, filesApp, checkLoad("      data:\n        svc: wallet\n"))

	res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)

	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	if got := servedService(target); got != "wallet" {
		t.Errorf("target received service = %q, want wallet from the file's svc", got)
	}

	js := runCLI(t.Context(), t, 10*time.Second, "-output", "json", "-c", cfg)
	if js.err != nil {
		t.Fatalf("json run: %v\nstderr:\n%s", js.err, js.stderr)
	}
	if got := jsonAt(t, decodeOnly(t, js.stdout), "methods_source"); got != "proto" {
		t.Errorf("methods_source = %v, want proto", got)
	}
}

// --- files are read before the network -------------------------------------------

// «A config error does not wait for the network» (main.go): the target accepts
// the connection and never answers, and the connect timeout is far longer than
// the test gives the run. What the files and the config can say is said first.
// Ground: contract — compile, find the methods and check data/dataset before connect.
func TestRun_FilesAreCheckedBeforeTheConnection(t *testing.T) {
	dataset := writeDataset(t, "{\"nope\":1}\n")

	tests := []struct {
		name  string
		proto string
		load  string
		check func(t *testing.T, err error)
	}{
		{
			name:  "a syntax error",
			proto: strings.Replace(healthProto, "string service = 1;", "string service = one;", 1),
			load:  checkLoad(""),
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, descriptor.ErrCompile) {
					t.Errorf("err = %v, want descriptor.ErrCompile", err)

					return
				}
				// The broken field is in the message on line 3 of healthProto.
				if !regexp.MustCompile(`health\.proto:3:\d+`).MatchString(err.Error()) {
					t.Errorf("error %q does not carry health.proto:3:<column>", err)
				}
			},
		},
		{
			name:  "a method the files lack",
			proto: healthProto,
			load:  "  calls:\n    - method: " + missingMethod + "\n      rps: 50\n      duration: 300ms\n",
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, descriptor.ErrMethodNotFound) || !strings.Contains(err.Error(), missingMethod) {
					t.Errorf("err = %v, want descriptor.ErrMethodNotFound naming %s", err, missingMethod)
				}
			},
		},
		{
			name:  "data that does not fit",
			proto: healthProto,
			load:  checkLoad("      data:\n        no_such_field: 1\n"),
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, cli.ErrRequestData) || !strings.Contains(err.Error(), "no_such_field") {
					t.Errorf("err = %v, want cli.ErrRequestData naming no_such_field", err)
				}
			},
		},
		{
			name:  "a dataset that does not fit",
			proto: healthProto,
			load:  checkLoad("      dataset: '" + dataset + "'\n"),
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, cli.ErrRequestData) || !strings.Contains(err.Error(), "users.jsonl:1") {
					t.Errorf("err = %v, want cli.ErrRequestData naming users.jsonl:1", err)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := layout(t, map[string]string{"health.proto": tc.proto})
			cfg := protoConfig(t, dir, silentTarget(t), filesApp, tc.load)

			res := runCLI(t.Context(), t, 4*time.Second, "-connect-timeout", "30s", "-c", cfg)

			if exitCode(res.err) != 1 {
				t.Fatalf("exit code %d, want 1: a config error\nerr %v", exitCode(res.err), res.err)
			}
			tc.check(t, res.err)
		})
	}
}

// Without a file the run is as before: reflection is asked, and a target
// without it is refused. What changes is that the refusal says what to set.
// Ground: contract — the reflection-off message names app.proto and app.protoset.
func TestRun_NoReflectionNamesTheWayOut(t *testing.T) {
	dataset := writeDataset(t, "{\"service\":\"a\"}\n")

	for name, data := range map[string]string{
		"data":    serviceData,
		"dataset": "      dataset: '" + dataset + "'\n",
	} {
		t.Run(name, func(t *testing.T) {
			target := startTarget(t)
			cfg := dataConfig(t, target.addr, checkCall(data))

			res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)

			if !errors.Is(res.err, descriptor.ErrReflectionUnsupported) {
				t.Fatalf("err = %v, want descriptor.ErrReflectionUnsupported", res.err)
			}
			for _, want := range []string{"app.proto", "app.protoset"} {
				if !strings.Contains(res.err.Error(), want) {
					t.Errorf("error %q does not name %s", res.err, want)
				}
			}
			if n := target.health.calls.Load(); n != 0 {
				t.Errorf("target received %d calls, want none before a config error", n)
			}
		})
	}
}

// --- where the methods came from ----------------------------------------------------

// lineAfterRunFinished is the line under «run finished:» in a text report.
func lineAfterRunFinished(t *testing.T, stdout string) string {
	t.Helper()

	lines := strings.Split(stdout, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "run finished:") && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	t.Fatalf("stdout has no «run finished:» line:\n%s", stdout)

	return ""
}

// methodsSources are the three ways to learn a method, with what the report
// says of each: the JSON value and the text line. «2 .proto files» counts
// the entry files of app.proto.files, not every file compiled.
var methodsSources = []struct {
	name, app, jsonValue, line string
	files                      map[string]string
	protoset, reflect          bool
}{
	{name: "reflection", reflect: true, jsonValue: "reflection", line: "methods: server reflection"},
	{
		name: "one entry .proto file", app: filesApp, jsonValue: "proto", line: "methods: 1 .proto file",
		files: map[string]string{"health.proto": healthProto},
	},
	{
		name: "two entry .proto files", app: twoFilesApp, jsonValue: "proto", line: "methods: 2 .proto files",
		files: map[string]string{"health.proto": healthProto, "other.proto": otherProto},
	},
	{
		name: "a protoset", app: protosetApp, protoset: true, jsonValue: "protoset", line: "methods: protoset api.protoset",
	},
}

// Ground: contract — methods_source in the run's JSON and the line under «run finished:».
func TestRun_ReportSaysWhereTheMethodsCameFrom(t *testing.T) {
	for _, src := range methodsSources {
		t.Run(src.name, func(t *testing.T) {
			target := startTarget(t)
			if src.reflect {
				target = startReflectingTarget(t)
			}
			dir := layout(t, src.files)
			if src.protoset {
				writeHealthProtoset(t, dir)
			}
			cfg := protoConfig(t, dir, target.addr, src.app, checkLoad(""))

			text := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)
			if text.err != nil {
				t.Fatalf("run: %v\nstderr:\n%s", text.err, text.stderr)
			}
			if got := lineAfterRunFinished(t, text.stdout); got != src.line {
				t.Errorf("the line under «run finished:» is %q, want %q", got, src.line)
			}

			js := runCLI(t.Context(), t, 10*time.Second, "-output", "json", "-c", cfg)
			if js.err != nil {
				t.Fatalf("json run: %v\nstderr:\n%s", js.err, js.stderr)
			}
			if got := jsonAt(t, decodeOnly(t, js.stdout), "methods_source"); got != src.jsonValue {
				t.Errorf("methods_source = %v, want %q", got, src.jsonValue)
			}
		})
	}
}

// -fake resolves no method, so it has no source: null, and no line that would
// say «reflection» of a run that asked nobody. A config that names a file does
// not change that.
// Ground: contract — null of methods_source and no text line when nothing was resolved.
func TestRun_FakeHasNoMethodsSource(t *testing.T) {
	dir := layout(t, map[string]string{"health.proto": healthProto})
	configs := map[string]string{
		"no source set":    protoConfig(t, dir, closedPort(t), "", checkLoad("")),
		"a proto file set": protoConfig(t, dir, closedPort(t), filesApp, checkLoad("")),
	}
	for name, cfg := range configs {
		t.Run(name, func(t *testing.T) {
			js := runCLI(t.Context(), t, 10*time.Second, "-fake", "-output", "json", "-c", cfg)
			if js.err != nil {
				t.Fatalf("json run: %v", js.err)
			}
			if got := jsonAt(t, decodeOnly(t, js.stdout), "methods_source"); got != nil {
				t.Errorf("methods_source = %v, want null", got)
			}

			text := runCLI(t.Context(), t, 10*time.Second, "-fake", "-c", cfg)
			if text.err != nil {
				t.Fatalf("run: %v", text.err)
			}
			if strings.Contains(text.stdout, "methods:") {
				t.Errorf("a fake run says where its methods came from:\n%s", text.stdout)
			}
		})
	}
}

// searchReported says a search ended with a report: complete, invalid or
// stopped. How far a machine holds the steps is not what these tests claim.
func searchReported(err error) bool {
	code := exitCode(err)

	return code == 0 || code == 2 || code == 3
}

// A search reports the source too, in its text and at the top of its JSON.
// Ground: contract — methods_source in the breaking-point JSON and the line in its text report.
func TestRun_ASearchSaysWhereTheMethodWasFoundToo(t *testing.T) {
	for _, src := range methodsSources {
		t.Run(src.name, func(t *testing.T) {
			target := startTarget(t)
			if src.reflect {
				target = startReflectingTarget(t)
			}
			dir := layout(t, src.files)
			if src.protoset {
				writeHealthProtoset(t, dir)
			}
			cfg := protoConfig(t, dir, target.addr, src.app, searchLoad())

			text := runCLI(t.Context(), t, 60*time.Second, "-c", cfg)
			if !searchReported(text.err) {
				t.Fatalf("search: %v\nstderr:\n%s", text.err, text.stderr)
			}
			if !strings.Contains(text.stdout, "\n"+src.line+"\n") {
				t.Errorf("the text report lacks the line %q:\n%s", src.line, text.stdout)
			}

			js := runCLI(t.Context(), t, 60*time.Second, "-output", "json", "-c", cfg)
			if !searchReported(js.err) {
				t.Fatalf("json search: %v\nstderr:\n%s", js.err, js.stderr)
			}
			doc := decodeOnly(t, js.stdout)
			if got := jsonAt(t, doc, "mode"); got != "breakpoint" {
				t.Fatalf("mode = %v, want breakpoint", got)
			}
			if got := jsonAt(t, doc, "methods_source"); got != src.jsonValue {
				t.Errorf("methods_source = %v, want %q", got, src.jsonValue)
			}
		})
	}
}

// Ground: contract — a search on the fake target resolves nothing either.
func TestRun_AFakeSearchHasNoMethodsSource(t *testing.T) {
	dir := layout(t, nil)
	cfg := protoConfig(t, dir, closedPort(t), "", searchLoad())

	js := runCLI(t.Context(), t, 60*time.Second, "-fake", "-output", "json", "-c", cfg)
	if js.err != nil {
		t.Fatalf("json search: %v", js.err)
	}
	if got := jsonAt(t, decodeOnly(t, js.stdout), "methods_source"); got != nil {
		t.Errorf("methods_source = %v, want null", got)
	}

	text := runCLI(t.Context(), t, 60*time.Second, "-fake", "-c", cfg)
	if text.err != nil {
		t.Fatalf("search: %v", text.err)
	}
	if strings.Contains(text.stdout, "methods:") {
		t.Errorf("a fake search says where its methods came from:\n%s", text.stdout)
	}
}
