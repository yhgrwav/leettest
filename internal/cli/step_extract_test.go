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
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
)

// Extract (v0.9 unit 4a, the descriptor side; the checks of the config alone
// are in pkg/config). The config is built by hand. The source of every value
// is a method whose reply is replyMessage; the step that reads it takes the
// order message. A config that passes every check is accepted (4b: the run
// side is there; 4a refused it as not supported yet). The run side is in
// step_extract_run_test.go.

const (
	extractSource = "wallet.v1.Wallet/Open"
	extractMiddle = "wallet.v1.Wallet/Check"
	extractReader = "wallet.v1.Wallet/Pay"
)

// replyMessage is a reply with a field of every kind a path may end in or pass
// through. Names in snake case: the JSON name of wallet_id is walletId.
func replyMessage(t testing.TB) protoreflect.MessageDescriptor {
	t.Helper()

	const (
		optional = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
		repeated = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	)

	field := func(name string, num int32, kind descriptorpb.FieldDescriptorProto_Type, label descriptorpb.FieldDescriptorProto_Label, typeName string) *descriptorpb.FieldDescriptorProto {
		f := &descriptorpb.FieldDescriptorProto{
			Name: proto.String(name), Number: proto.Int32(num), Type: kind.Enum(), Label: label.Enum(),
		}
		if typeName != "" {
			f.TypeName = proto.String(typeName)
		}

		return f
	}
	one := func(name string, num int32, kind descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
		return field(name, num, kind, optional, "")
	}

	const (
		str = descriptorpb.FieldDescriptorProto_TYPE_STRING
		msg = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	)

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: proto.String("leettest/test/reply.proto"), Package: proto.String("leettest.reply"), Syntax: proto.String("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Mode"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("MODE_NONE"), Number: proto.Int32(0)},
				{Name: proto.String("MODE_FAST"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Balance"),
				Field: []*descriptorpb.FieldDescriptorProto{
					one("amount", 1, descriptorpb.FieldDescriptorProto_TYPE_INT64), one("currency_code", 2, str),
				},
			},
			{
				Name: proto.String("Reply"),
				Field: []*descriptorpb.FieldDescriptorProto{
					one("wallet_id", 1, str),
					field("balance", 2, msg, optional, ".leettest.reply.Balance"),
					one("seq_no", 3, descriptorpb.FieldDescriptorProto_TYPE_UINT32),
					one("delta", 4, descriptorpb.FieldDescriptorProto_TYPE_INT32),
					one("limit", 5, descriptorpb.FieldDescriptorProto_TYPE_UINT64),
					one("ratio", 6, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE),
					one("ok", 7, descriptorpb.FieldDescriptorProto_TYPE_BOOL),
					field("mode", 8, descriptorpb.FieldDescriptorProto_TYPE_ENUM, optional, ".leettest.reply.Mode"),
					one("raw", 9, descriptorpb.FieldDescriptorProto_TYPE_BYTES),
					field("tags", 10, str, repeated, ""),
					field("entries", 11, msg, repeated, ".leettest.reply.Balance"),
					field("attrs", 12, msg, repeated, ".leettest.reply.Reply.AttrsEntry"),
				},
				NestedType: []*descriptorpb.DescriptorProto{{
					Name:    proto.String("AttrsEntry"),
					Field:   []*descriptorpb.FieldDescriptorProto{one("key", 1, str), one("value", 2, str)},
					Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
				}},
			},
		},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("build the test file: %v", err)
	}

	return file.Messages().ByName("Reply")
}

// extractResolver answers for the three methods above, each with the order
// message as input and the reply message as output; fail names the ones that
// answer with an error instead (no reflection, say).
type extractResolver struct {
	known map[string]*descriptor.Method
	fail  map[string]error
}

func newExtractResolver(t testing.TB, fail map[string]error) *extractResolver {
	t.Helper()

	known := map[string]*descriptor.Method{}
	for _, m := range []string{extractSource, extractMiddle, extractReader} {
		known[m] = &descriptor.Method{Path: "/" + m, Input: orderMessage(t), Output: replyMessage(t)}
	}

	return &extractResolver{known: known, fail: fail}
}

func (r *extractResolver) Resolve(_ context.Context, method string) (*descriptor.Method, error) {
	if err, ok := r.fail[method]; ok {
		return nil, err
	}
	m, ok := r.known[method]
	if !ok {
		return nil, descriptor.ErrMethodNotFound
	}

	return m, nil
}

func (r *extractResolver) ResolveAll(ctx context.Context, methods []string) (map[string]*descriptor.Method, error) {
	out := make(map[string]*descriptor.Method, len(methods))
	for _, m := range methods {
		resolved, err := r.Resolve(ctx, m)
		if err != nil {
			return nil, err
		}
		out[m] = resolved
	}

	return out, nil
}

// extractConfig is a templated scenario: the call (Open) with callExtract, then
// the steps. A step that reads takes data and has templates on.
func extractConfig(callExtract map[string]string, steps ...config.Step) *config.MasterConfig {
	call := config.Call{
		Method: extractSource, RPS: 50, Duration: 300 * time.Millisecond, Timeout: 100 * time.Millisecond,
		Scenario: "wallet-flow", Steps: steps, Templates: templatesOn(), Extract: callExtract,
	}

	return &config.MasterConfig{Load: config.Load{Calls: []config.Call{call}}}
}

func readerStep(data map[string]any) config.Step {
	return config.Step{Method: extractReader, Timeout: 100 * time.Millisecond, Data: data, Templates: templatesOn()}
}

func middleStep(extract map[string]string) config.Step {
	return config.Step{Method: extractMiddle, Timeout: 100 * time.Millisecond, Extract: extract}
}

func attachExtract(t testing.TB, cfg *config.MasterConfig, resolver descriptor.Resolver) error {
	t.Helper()

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}

	_, _, err = AttachMethods(t.Context(), resolver, cfg, calls)

	return err
}

// Ground: contract — spec 1, 3 and 6: a path that does not lead to a scalar of
// the reply is refused before the load, by the path of the extract and the
// segment that is wrong.
func TestAttachMethods_ExtractSourceIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		onStep bool
		path   string
		want   []string
	}{
		{"an unknown field", false, "nope", []string{"load.calls[0].extract.x", "nope", "no field", "Reply"}},
		{"an unknown field below a message", false, "balance.nope", []string{"load.calls[0].extract.x", "balance.nope", "no field", "Balance"}},
		{"an unknown field of a step", true, "nope", []string{"load.calls[0].steps[0].extract.x", "nope", "no field", "Reply"}},
		{"a hop through a repeated field", false, "entries.amount", []string{"load.calls[0].extract.x", "entries", "repeated"}},
		{"a hop through a map", false, "attrs.value", []string{"load.calls[0].extract.x", "attrs", "map"}},
		{"a hop through a scalar", false, "wallet_id.x", []string{"load.calls[0].extract.x", "wallet_id", "not a message"}},
		{"a repeated leaf", false, "tags", []string{"load.calls[0].extract.x", "tags", "repeated"}},
		{"a map leaf", false, "attrs", []string{"load.calls[0].extract.x", "attrs", "map"}},
		{"a message leaf", false, "balance", []string{"load.calls[0].extract.x", "balance", "message"}},
		{"a bytes leaf", false, "raw", []string{"load.calls[0].extract.x", "raw", "bytes"}},
		{"an empty segment", false, "balance..amount", []string{"load.calls[0].extract.x", "balance..amount"}},
		{"a leaf of a step that is repeated", true, "tags", []string{"load.calls[0].steps[0].extract.x", "tags", "repeated"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := readerStep(map[string]any{"id": "{{extract.x}}"})

			cfg := extractConfig(map[string]string{"x": tc.path}, read)
			if tc.onStep {
				cfg = extractConfig(nil, middleStep(map[string]string{"x": tc.path}), read)
			}

			err := attachExtract(t, cfg, newExtractResolver(t, nil))
			if err == nil {
				t.Fatalf("AttachMethods() = nil, want a refusal with %q", tc.want)
			}
			if !errors.Is(err, config.ErrScenario) {
				t.Errorf("error = %v, want it to be config.ErrScenario", err)
			}
			for _, text := range tc.want {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("error = %q, want it to contain %q", err, text)
				}
			}
		})
	}
}

// Ground: contract — spec 3: a value goes only where the existing slot rules put
// a placeholder: a string takes it among text, a number takes a whole one from
// an integer that fits its range; every other field takes none. Each refusal
// names the step and the field and is ErrTemplateField.
func TestAttachMethods_ExtractDestinationIsRefused(t *testing.T) {
	const whole = "{{extract.x}}"

	for _, tc := range []struct {
		name   string
		source string
		field  string
		text   string
		want   []string
	}{
		{"a number with text around", "delta", "count", "n-" + whole, []string{"data.count", "takes only a whole {{extract.x}}"}},
		{"a number with a seq beside", "delta", "count", whole + "{{seq}}", []string{"data.count", "takes only a whole {{extract.x}}"}},
		{"int64 into int32", "balance.amount", "count", whole, []string{"data.count", "does not fit", "int64", "int32"}},
		{"uint64 into int64", "limit", "big", whole, []string{"data.big", "does not fit", "uint64", "int64"}},
		{"int32 into uint32", "delta", "u32", whole, []string{"data.u32", "does not fit", "int32", "uint32"}},
		{"int32 into uint64", "delta", "u64", whole, []string{"data.u64", "does not fit", "int32", "uint64"}},
		{"uint32 into int32", "seq_no", "count", whole, []string{"data.count", "does not fit", "uint32", "int32"}},
		{"uint64 into uint32", "limit", "u32", whole, []string{"data.u32", "does not fit", "uint64", "uint32"}},
		{"a double into an int64", "ratio", "big", whole, []string{"data.big", "double", "not an integer"}},
		{"a string into a number", "wallet_id", "count", whole, []string{"data.count", "string", "not an integer"}},
		{"a bool into a number", "ok", "big", whole, []string{"data.big", "bool", "not an integer"}},
		{"an enum into a number", "mode", "big", whole, []string{"data.big", "enum", "not an integer"}},
		{"a bool field", "wallet_id", "flag", whole, []string{"data.flag", "takes no placeholder"}},
		{"an enum field", "wallet_id", "kind", whole, []string{"data.kind", "takes no placeholder"}},
		{"a double field", "delta", "ratio", whole, []string{"data.ratio", "takes no placeholder"}},
		{"a bytes field", "wallet_id", "payload", whole, []string{"data.payload", "takes no placeholder"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := extractConfig(map[string]string{"x": tc.source}, readerStep(map[string]any{tc.field: tc.text}))

			err := attachExtract(t, cfg, newExtractResolver(t, nil))
			if err == nil {
				t.Fatalf("AttachMethods() = nil, want a refusal with %q", tc.want)
			}
			if !errors.Is(err, ErrTemplateField) {
				t.Errorf("error = %v, want it to be ErrTemplateField", err)
			}
			for _, text := range append([]string{"steps[0]"}, tc.want...) {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("error = %q, want it to contain %q", err, text)
				}
			}
		})
	}
}

// A place that takes no number does not take a value either, however deep.
// Ground: contract — spec 3, the slot rules at every depth.
func TestAttachMethods_ExtractDestinationBelowAMessageAndInAListIsRefused(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"a number below a message": {"child": map[string]any{"n": "{{extract.x}}x"}},
		"a number in a list":       {"items": []any{map[string]any{"n": "a{{extract.x}}"}}},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := extractConfig(map[string]string{"x": "delta"}, readerStep(data))

			err := attachExtract(t, cfg, newExtractResolver(t, nil))
			if !errors.Is(err, ErrTemplateField) {
				t.Fatalf("error = %v, want it to be ErrTemplateField", err)
			}
			if !strings.Contains(err.Error(), "takes only a whole {{extract.x}}") {
				t.Errorf("error = %q, want it to say a number takes only a whole {{extract.x}}", err)
			}
		})
	}
}

// Ground: contract — spec 6: a method with no schema cannot be checked, and an
// extract is a promise about its reply: the same "needs schema" refusal as data,
// by the path of the extract. The call and a step are asked alike.
func TestAttachMethods_ExtractNeedsASchema(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		cfg    *config.MasterConfig
		want   []string
	}{
		{
			"the call", extractSource,
			extractConfig(map[string]string{"x": "wallet_id"}, readerStep(map[string]any{"id": "{{extract.x}}"})),
			[]string{"load.calls[0]", extractSource, "extract", "app.proto"},
		},
		{
			"a step", extractMiddle,
			extractConfig(nil, middleStep(map[string]string{"x": "wallet_id"}), readerStep(map[string]any{"id": "{{extract.x}}"})),
			[]string{"load.calls[0].steps[0]", extractMiddle, "extract", "app.proto"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := newExtractResolver(t, map[string]error{tc.method: descriptor.ErrReflectionUnsupported})

			err := attachExtract(t, tc.cfg, resolver)
			if !errors.Is(err, descriptor.ErrReflectionUnsupported) {
				t.Fatalf("error = %v, want it to be ErrReflectionUnsupported", err)
			}
			for _, text := range tc.want {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("error = %q, want it to contain %q", err, text)
				}
			}
		})
	}
}

// Ground: contract — spec 1, 3: the paths and the fits the spec allows pass
// every check and the config is accepted, with the extract set on the call.
// Mutation: a config with extract still refused after the checks.
func TestAttachMethods_ExtractThatPassesIsAccepted(t *testing.T) {
	const whole = "{{extract.x}}"

	for _, tc := range []struct {
		name   string
		source string
		data   map[string]any
	}{
		{"a path by the proto name", "wallet_id", map[string]any{"id": whole}},
		{"a path by the JSON name", "walletId", map[string]any{"id": whole}},
		{"a nested path by the proto name", "balance.currency_code", map[string]any{"id": whole}},
		{"a nested path by the JSON name", "balance.currencyCode", map[string]any{"id": whole}},
		{"a string among text and a seq", "wallet_id", map[string]any{"note": "w-" + whole + "-{{seq}}"}},
		{"a string below a message", "wallet_id", map[string]any{"child": map[string]any{"name": whole}}},
		{"uint32 into int64", "seq_no", map[string]any{"big": whole}},
		{"int32 into int64", "delta", map[string]any{"big": whole}},
		{"int64 into int64", "balance.amount", map[string]any{"big": whole}},
		{"int32 into int32", "delta", map[string]any{"count": whole}},
		{"uint32 into uint32", "seq_no", map[string]any{"u32": whole}},
		{"uint64 into uint64", "limit", map[string]any{"u64": whole}},
		{"a double into a string", "ratio", map[string]any{"note": "r=" + whole}},
		{"a bool into a string", "ok", map[string]any{"note": whole}},
		{"an enum into a string", "mode", map[string]any{"note": whole}},
		{"a uint64 into a string", "limit", map[string]any{"note": whole}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := extractConfig(map[string]string{"x": tc.source}, readerStep(tc.data))

			if err := attachExtract(t, cfg, newExtractResolver(t, nil)); err != nil {
				t.Fatalf("AttachMethods() = %v, want the config accepted", err)
			}
		})
	}
}

// The step takes the extract as the call does, and a call and a step may both.
// Ground: contract — spec 1, extract on the call entry or on a step.
func TestAttachMethods_ExtractOfAStepAndOfTheCallIsAccepted(t *testing.T) {
	cfg := extractConfig(
		map[string]string{"w": "wallet_id"},
		middleStep(map[string]string{"amount": "balance.amount"}),
		readerStep(map[string]any{"id": "{{extract.w}}", "big": "{{extract.amount}}"}),
	)

	if err := attachExtract(t, cfg, newExtractResolver(t, nil)); err != nil {
		t.Fatalf("AttachMethods() = %v, want the config accepted", err)
	}
}

// One bad path among the good is still refused, by the path of the bad one.
// Ground: contract — every check speaks; the good paths do not hide the bad.
func TestAttachMethods_ABadExtractPathAmongTheGoodIsRefused(t *testing.T) {
	cfg := extractConfig(
		map[string]string{"w": "wallet_id", "bad": "nope"},
		readerStep(map[string]any{"id": "{{extract.w}}", "note": "{{extract.bad}}"}),
	)

	err := attachExtract(t, cfg, newExtractResolver(t, nil))
	if err == nil {
		t.Fatalf("AttachMethods() = nil, want the refusal of the bad path")
	}
	if !strings.Contains(err.Error(), "load.calls[0].extract.bad") {
		t.Errorf("error = %q, want it to name load.calls[0].extract.bad", err)
	}
}

// A scenario with no extract is attached as it was: no extract, no refusal.
// Ground: contract — the key left out changes nothing.
func TestAttachMethods_AScenarioWithoutExtractIsNotRefused(t *testing.T) {
	cfg := extractConfig(nil, middleStep(nil), readerStep(map[string]any{"note": "n-{{seq}}"}))

	if err := attachExtract(t, cfg, newExtractResolver(t, nil)); err != nil {
		t.Fatalf("AttachMethods() = %v, want a scenario without extract accepted", err)
	}
}

// A method with no schema and no extract is not refused: the check needs it
// only for an extract.
// Ground: contract — spec 6, the refusal is for extract and not for scenarios.
func TestAttachMethods_AStepWithoutExtractNeedsNoSchema(t *testing.T) {
	cfg := extractConfig(nil, middleStep(nil))
	resolver := newExtractResolver(t, map[string]error{extractMiddle: descriptor.ErrReflectionUnsupported})

	if err := attachExtract(t, cfg, resolver); err != nil {
		t.Fatalf("AttachMethods() = %v, want a step with no data and no extract unchecked, not refused", err)
	}
}
