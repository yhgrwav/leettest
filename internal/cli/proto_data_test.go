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

package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	// Registered globally in this test binary on purpose: the files below do not
	// hold it, so finding it would be a fall back to the global registry.
	_ "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// payloadProto is a message with each shape a payload can take. The package
// is in no binary of this project, so a type of it is found only by a
// resolver that was given the file.
const payloadProto = `syntax = "proto3";
package payload.v1;
import "google/protobuf/any.proto";
enum Color { COLOR_UNSPECIFIED = 0; RED = 1; BLUE = 2; }
message Item { string name = 1; }
message Mix {
  oneof pick { string text = 1; int32 count = 2; }
  optional int32 maybe = 3;
  map<string, int32> tally = 4;
  Color color = 5;
  string user_name = 6 [json_name = "uname"];
  google.protobuf.Any any = 7;
}
message Ack {}
service Payload { rpc Send(Mix) returns (Ack); }
`

const sendMethod = "payload.v1.Payload/Send"

// payloadResolver compiles payloadProto.
func payloadResolver(t *testing.T) *descriptor.FileResolver {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "payload.proto")
	if err := os.WriteFile(file, []byte(payloadProto), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	r, err := descriptor.NewProtoResolver([]string{file}, []string{dir})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	return r
}

// attach runs AttachData over one call and returns the engine call it filled.
func attach(t *testing.T, resolver descriptor.Resolver, call config.Call) (engine.Call, error) {
	t.Helper()

	cfg := &config.MasterConfig{Load: config.Load{Calls: []config.Call{call}}}
	calls := make([]engine.Call, 1)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_, err := cli.AttachData(ctx, resolver, cfg, calls)

	return calls[0], err
}

// decode reads a request body back into the method's message, with its types.
func decode(t *testing.T, resolver descriptor.Resolver, body []byte) (*descriptor.Method, protoreflect.Message) {
	t.Helper()

	m, err := resolver.Resolve(t.Context(), sendMethod)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	msg := dynamicpb.NewMessage(m.Input)
	if err := (proto.UnmarshalOptions{Resolver: m.Types}).Unmarshal(body, msg); err != nil {
		t.Fatalf("the body is not a Mix: %v", err)
	}

	return m, msg.ProtoReflect()
}

// Ground: contract — protojson's rules for the shapes a message can have, applied to the files' schema.
func TestAttachData_FilesPayloadTakesEveryShapeOfAMessage(t *testing.T) {
	resolver := payloadResolver(t)

	tests := []struct {
		name  string
		data  map[string]any
		check func(t *testing.T, msg protoreflect.Message)
	}{
		{"a oneof member", map[string]any{"count": 7}, func(t *testing.T, msg protoreflect.Message) {
			t.Helper()
			which := msg.WhichOneof(msg.Descriptor().Oneofs().ByName("pick"))
			if which == nil || which.Name() != "count" || msg.Get(which).Int() != 7 {
				t.Errorf("pick = %v, want count 7", which)
			}
		}},
		{"the other oneof member", map[string]any{"text": "hi"}, func(t *testing.T, msg protoreflect.Message) {
			t.Helper()
			which := msg.WhichOneof(msg.Descriptor().Oneofs().ByName("pick"))
			if which == nil || which.Name() != "text" || msg.Get(which).String() != "hi" {
				t.Errorf("pick = %v, want text hi", which)
			}
		}},
		{"an optional set to its zero", map[string]any{"maybe": 0}, func(t *testing.T, msg protoreflect.Message) {
			t.Helper()
			if !msg.Has(msg.Descriptor().Fields().ByName("maybe")) {
				t.Error("maybe: 0 was dropped: an optional keeps its presence")
			}
		}},
		{"an optional left out", map[string]any{"text": "x"}, func(t *testing.T, msg protoreflect.Message) {
			t.Helper()
			if msg.Has(msg.Descriptor().Fields().ByName("maybe")) {
				t.Error("maybe is present though the data left it out")
			}
		}},
		{"a map", map[string]any{"tally": map[string]any{"a": 1, "b": 2}}, func(t *testing.T, msg protoreflect.Message) {
			t.Helper()
			tally := msg.Get(msg.Descriptor().Fields().ByName("tally")).Map()
			if tally.Len() != 2 || tally.Get(protoreflect.ValueOfString("b").MapKey()).Int() != 2 {
				t.Errorf("tally = %v, want a:1 b:2", tally)
			}
		}},
		{"an enum by name", map[string]any{"color": "BLUE"}, wantColor(2)},
		{"an enum by number", map[string]any{"color": 2}, wantColor(2)},
		{"a field by its json_name", map[string]any{"uname": "ann"}, wantUserName("ann")},
		{"a field by its name in the .proto", map[string]any{"user_name": "ann"}, wantUserName("ann")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			call, err := attach(t, resolver, config.Call{Method: sendMethod, Data: tc.data})
			if err != nil {
				t.Fatalf("attach: %v", err)
			}

			_, msg := decode(t, resolver, call.Payload)
			tc.check(t, msg)
		})
	}
}

func wantColor(number protoreflect.EnumNumber) func(*testing.T, protoreflect.Message) {
	return func(t *testing.T, msg protoreflect.Message) {
		t.Helper()

		if got := msg.Get(msg.Descriptor().Fields().ByName("color")).Enum(); got != number {
			t.Errorf("color = %d, want %d", got, number)
		}
	}
}

func wantUserName(name string) func(*testing.T, protoreflect.Message) {
	return func(t *testing.T, msg protoreflect.Message) {
		t.Helper()

		if got := msg.Get(msg.Descriptor().Fields().ByName("user_name")).String(); got != name {
			t.Errorf("user_name = %q, want %q", got, name)
		}
	}
}

// Ground: contract — the data is checked against the files before the run, as against reflection.
func TestAttachData_FilesPayloadRefusesWhatTheMessageLacks(t *testing.T) {
	resolver := payloadResolver(t)

	for name, data := range map[string]map[string]any{
		"an unknown field": {"no_such_field": 1},
		"an unknown enum":  {"color": "GREEN"},
		"a wrong type":     {"count": "many"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := attach(t, resolver, config.Call{Method: sendMethod, Data: data})

			if !errors.Is(err, cli.ErrRequestData) {
				t.Errorf("err = %v, want cli.ErrRequestData", err)
			}
		})
	}
}

// Ground: contract — an Any names a type of the files, and the body carries it encoded.
func TestAttachData_AnyOfATypeInTheFilesIsEncoded(t *testing.T) {
	resolver := payloadResolver(t)
	data := map[string]any{"any": map[string]any{"@type": "type.googleapis.com/payload.v1.Item", "name": "x"}}

	call, err := attach(t, resolver, config.Call{Method: sendMethod, Data: data})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	m, msg := decode(t, resolver, call.Payload)
	anyMsg := msg.Get(msg.Descriptor().Fields().ByName("any")).Message()
	url := anyMsg.Get(anyMsg.Descriptor().Fields().ByName("type_url")).String()
	value := anyMsg.Get(anyMsg.Descriptor().Fields().ByName("value")).Bytes()
	if url != "type.googleapis.com/payload.v1.Item" {
		t.Fatalf("type_url = %q, want the Item of the files", url)
	}

	mt, err := m.Types.FindMessageByURL(url)
	if err != nil {
		t.Fatalf("find %s: %v", url, err)
	}
	item := mt.New()
	if err := proto.Unmarshal(value, item.Interface()); err != nil {
		t.Fatalf("the Any holds no Item: %v", err)
	}
	if got := item.Get(item.Descriptor().Fields().ByName("name")).String(); got != "x" {
		t.Errorf("item.name = %q, want x", got)
	}
}

// Nobody imported duration.proto: the Any still names it, as the global registry knows it.
// Ground: contract — google.protobuf.* falls back to the built-in copies, user types do not.
func TestAttachData_AnyOfAWellKnownTypeTheFilesDoNotImport(t *testing.T) {
	resolver := payloadResolver(t)
	data := map[string]any{"any": map[string]any{"@type": "type.googleapis.com/google.protobuf.Duration", "value": "1.5s"}}

	call, err := attach(t, resolver, config.Call{Method: sendMethod, Data: data})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	_, msg := decode(t, resolver, call.Payload)
	anyMsg := msg.Get(msg.Descriptor().Fields().ByName("any")).Message()
	var d durationpb.Duration
	if err := proto.Unmarshal(anyMsg.Get(anyMsg.Descriptor().Fields().ByName("value")).Bytes(), &d); err != nil {
		t.Fatalf("the Any holds no Duration: %v", err)
	}
	if d.GetSeconds() != 1 || d.GetNanos() != 500_000_000 {
		t.Errorf("duration = %v, want 1.5s", &d)
	}
}

// Both names are real types: one in no file of the set, one registered in this
// binary and still not in the files. The error names the type, so the user
// knows which import to add.
// Ground: contract — an Any of a type the files lack is a startup error naming it.
func TestAttachData_AnyOfATypeTheFilesLackNamesTheType(t *testing.T) {
	resolver := payloadResolver(t)

	for _, name := range []string{"payload.v1.Nope", "grpc.health.v1.HealthCheckRequest"} {
		t.Run(name, func(t *testing.T) {
			data := map[string]any{"any": map[string]any{"@type": "type.googleapis.com/" + name}}

			_, err := attach(t, resolver, config.Call{Method: sendMethod, Data: data})

			if !errors.Is(err, cli.ErrRequestData) {
				t.Fatalf("err = %v, want cli.ErrRequestData", err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not name the type %s", err, name)
			}
		})
	}
}

// The line is named and the record is not: a record may hold a secret, and a
// type name is part of it. Only data names the type.
// Ground: contract — the dataset error stays "dataset <file>:<line>: does not fit <message>" (recordBody).
func TestAttachData_AnyOfAnUnknownTypeInADatasetNamesTheLineOnly(t *testing.T) {
	const secret = "S3CR3T-4821"

	resolver := payloadResolver(t)
	call := config.Call{
		Method: sendMethod, Dataset: "users.jsonl",
		Records: []config.DatasetRecord{
			{Line: 1, JSON: json.RawMessage(`{"text":"fine"}`)},
			{Line: 3, JSON: json.RawMessage(`{"any":{"@type":"type.googleapis.com/payload.v1.Nope","secret":"` + secret + `"}}`)},
		},
	}

	_, err := attach(t, resolver, call)

	if !errors.Is(err, cli.ErrRequestData) {
		t.Fatalf("err = %v, want cli.ErrRequestData", err)
	}
	if want := "dataset users.jsonl:3: does not fit payload.v1.Mix"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q lacks %q", err, want)
	}
	for _, leaked := range []string{"Nope", secret} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("error %q leaks %q", err, leaked)
		}
	}
}

// Ground: contract — a dataset record is parsed with the same types as data.
func TestAttachData_AnyOfATypeInTheFilesInADatasetIsEncoded(t *testing.T) {
	resolver := payloadResolver(t)
	call := config.Call{
		Method: sendMethod, Dataset: "users.jsonl",
		Records: []config.DatasetRecord{
			{Line: 1, JSON: json.RawMessage(`{"any":{"@type":"type.googleapis.com/payload.v1.Item","name":"y"}}`)},
		},
	}

	filled, err := attach(t, resolver, call)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	if len(filled.Payloads) != 1 {
		t.Fatalf("payloads = %d, want 1", len(filled.Payloads))
	}
	_, msg := decode(t, resolver, filled.Payloads[0])
	if !msg.Has(msg.Descriptor().Fields().ByName("any")) {
		t.Error("the Any of the record is not in the body")
	}
}

// A file made with a newer protoc than protocompile reads: the protoset path
// finds the method and the data is parsed against it.
// Ground: signal google.golang.org/protobuf v1.36.12 — protodesc builds an edition 2024 file.
func TestAttachData_ProtosetOfEdition2024ParsesTheData(t *testing.T) {
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING
	file := &descriptorpb.FileDescriptorProto{
		Name: proto.String("e2024.proto"), Package: proto.String("ed.v1"),
		Syntax: proto.String("editions"), Edition: descriptorpb.Edition_EDITION_2024.Enum(),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Req"), Field: []*descriptorpb.FieldDescriptorProto{{
				Name: proto.String("name"), Number: proto.Int32(1), Type: str.Enum(),
				Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			}}},
			{Name: proto.String("Resp")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Ed"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name: proto.String("Do"), InputType: proto.String(".ed.v1.Req"), OutputType: proto.String(".ed.v1.Resp"),
			}},
		}},
	}
	set, err := proto.Marshal(&descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resolver, err := descriptor.NewProtosetResolver(set)
	if err != nil {
		t.Fatalf("read the set: %v", err)
	}

	call, err := attach(t, resolver, config.Call{Method: "ed.v1.Ed/Do", Data: map[string]any{"name": "ann"}})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	m, err := resolver.Resolve(t.Context(), "ed.v1.Ed/Do")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	msg := dynamicpb.NewMessage(m.Input)
	if err := proto.Unmarshal(call.Payload, msg); err != nil {
		t.Fatalf("the body is not a Req: %v", err)
	}
	if got := msg.Get(m.Input.Fields().ByName("name")).String(); got != "ann" {
		t.Errorf("name = %q, want ann", got)
	}
}
