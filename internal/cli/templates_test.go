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
	"bytes"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	_ "google.golang.org/protobuf/types/known/durationpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"
	_ "google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
)

const orderMethod = "shop.v1.Shop/Order"

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// orderMessage is a request with a field of every type a placeholder may meet:
// strings at every depth, numbers of every width, and the types that take no
// placeholder.
func orderMessage(t testing.TB) protoreflect.MessageDescriptor {
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
	message := func(name string, num int32, typeName string) *descriptorpb.FieldDescriptorProto {
		return field(name, num, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, optional, typeName)
	}

	const (
		str  = descriptorpb.FieldDescriptorProto_TYPE_STRING
		msg  = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
		i32  = descriptorpb.FieldDescriptorProto_TYPE_INT32
		i64  = descriptorpb.FieldDescriptorProto_TYPE_INT64
		u32  = descriptorpb.FieldDescriptorProto_TYPE_UINT32
		u64  = descriptorpb.FieldDescriptorProto_TYPE_UINT64
		s32  = descriptorpb.FieldDescriptorProto_TYPE_SINT32
		s64  = descriptorpb.FieldDescriptorProto_TYPE_SINT64
		f32  = descriptorpb.FieldDescriptorProto_TYPE_FIXED32
		f64  = descriptorpb.FieldDescriptorProto_TYPE_FIXED64
		sf32 = descriptorpb.FieldDescriptorProto_TYPE_SFIXED32
		sf64 = descriptorpb.FieldDescriptorProto_TYPE_SFIXED64
	)

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: proto.String("leettest/test/order.proto"), Package: proto.String("leettest.test"), Syntax: proto.String("proto3"),
		Dependency: []string{"google/protobuf/timestamp.proto", "google/protobuf/duration.proto", "google/protobuf/wrappers.proto"},
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Kind"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("KIND_UNKNOWN"), Number: proto.Int32(0)},
				{Name: proto.String("KIND_FIRST"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:  proto.String("Child"),
				Field: []*descriptorpb.FieldDescriptorProto{one("name", 1, str), one("n", 2, i32)},
			},
			{
				Name: proto.String("Order"),
				Field: []*descriptorpb.FieldDescriptorProto{
					one("id", 1, str), one("note", 2, str),
					one("count", 3, i32), one("big", 4, i64), one("u32", 5, u32), one("s32", 6, s32),
					one("f32", 7, f32), one("sf32", 8, sf32), one("u64", 9, u64),
					one("payload", 10, descriptorpb.FieldDescriptorProto_TYPE_BYTES),
					one("flag", 11, descriptorpb.FieldDescriptorProto_TYPE_BOOL),
					one("ratio", 12, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE),
					one("fl", 13, descriptorpb.FieldDescriptorProto_TYPE_FLOAT),
					field("kind", 14, descriptorpb.FieldDescriptorProto_TYPE_ENUM, optional, ".leettest.test.Kind"),
					message("at", 15, ".google.protobuf.Timestamp"),
					message("wait", 16, ".google.protobuf.Duration"),
					message("label", 17, ".google.protobuf.StringValue"),
					message("child", 18, ".leettest.test.Child"),
					field("tags", 19, str, repeated, ""),
					field("items", 20, msg, repeated, ".leettest.test.Child"),
					field("attrs", 21, msg, repeated, ".leettest.test.Order.AttrsEntry"),
					one("s64", 22, s64), one("f64", 23, f64), one("sf64", 24, sf64),
					one("ref", 25, str),
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

	return file.Messages().ByName("Order")
}

func templatesOn() *bool {
	on := true

	return &on
}

func templatesOff() *bool {
	off := false

	return &off
}

func orderCall(templates *bool, data map[string]any) config.Call {
	return config.Call{Method: orderMethod, Data: data, Templates: templates}
}

// attachOrder attaches data of the Order method as the run does and returns the
// engine call and the error.
func attachOrder(t testing.TB, call config.Call) (engine.Call, error) {
	t.Helper()

	cfg, calls := loadOf(call)
	resolver := &fakeResolver{known: map[string]protoreflect.MessageDescriptor{orderMethod: orderMessage(t)}}

	_, _, err := AttachMethods(t.Context(), resolver, cfg, calls)

	return calls[0], err
}

// attachedOrder is attachOrder for a call that must be accepted and has a template.
func attachedOrder(t testing.TB, data map[string]any) *engine.Template {
	t.Helper()

	call, err := attachOrder(t, orderCall(templatesOn(), data))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if call.Template == nil {
		t.Fatal("the call has no Template")
	}

	return call.Template
}

func decodeOrder(t *testing.T, body []byte) protoreflect.Message {
	t.Helper()

	msg := dynamicpb.NewMessage(orderMessage(t))
	if err := proto.Unmarshal(body, msg); err != nil {
		t.Fatalf("a built body is not an Order: %v", err)
	}

	return msg
}

func fieldOf(msg protoreflect.Message, name string) protoreflect.Value {
	return msg.Get(msg.Descriptor().Fields().ByName(protoreflect.Name(name)))
}

// Ground: contract — every placeholder of one request gets the same seq, a
// number field takes it whole, a string takes it in text; {{uuid}} is a fresh
// uuid v4 in lower case.
// Mutations: placeholders work at templates: false (the off test below);
// two requests get one seq; seq from 0; uuid in upper case.
func TestAttachMethods_TemplateBuildsSeqAndUuidIntoTheirFields(t *testing.T) {
	tpl := attachedOrder(t, map[string]any{
		"id":    "{{uuid}}",
		"note":  "order-{{seq}}-{{seq}}",
		"count": "{{seq}}",
		"big":   "{{seq}}",
	})

	msg := decodeOrder(t, tpl.Build(7))

	if got := fieldOf(msg, "note").String(); got != "order-7-7" {
		t.Errorf("note = %q, want order-7-7: every {{seq}} of a request is one number", got)
	}
	if got := fieldOf(msg, "count").Int(); got != 7 {
		t.Errorf("count = %d, want 7", got)
	}
	if got := fieldOf(msg, "big").Int(); got != 7 {
		t.Errorf("big = %d, want 7", got)
	}
	if got := fieldOf(msg, "id").String(); !uuidV4.MatchString(got) {
		t.Errorf("id = %q, want a lower-case uuid v4 of 36 characters", got)
	}

	one := decodeOrder(t, tpl.Build(1))
	if got := fieldOf(one, "note").String(); got != "order-1-1" {
		t.Errorf("Build(1): note = %q, want order-1-1: the first seq is 1", got)
	}
}

// Ground: contract — {{uuid}} is new for every request, and nothing else
// varies between two builds of one seq: the seq is the call's, the uuid is not.
// Mutation: a retry builds the body anew (different uuid) — the sender sends
// the one Payload; here only that Build is what makes a uuid a uuid.
func TestAttachMethods_TemplateUuidIsNewForEveryBuildAndNothingElseMoves(t *testing.T) {
	tpl := attachedOrder(t, map[string]any{"id": "{{uuid}}", "note": "n-{{seq}}"})

	seen := map[string]bool{}
	for seq := uint64(1); seq <= 1000; seq++ {
		id := fieldOf(decodeOrder(t, tpl.Build(seq)), "id").String()
		if !uuidV4.MatchString(id) {
			t.Fatalf("seq %d: id = %q, want a lower-case uuid v4", seq, id)
		}
		if seen[id] {
			t.Fatalf("seq %d: uuid %s was built before", seq, id)
		}
		seen[id] = true
	}

	a, b := decodeOrder(t, tpl.Build(5)), decodeOrder(t, tpl.Build(5))
	if fieldOf(a, "id").String() == fieldOf(b, "id").String() {
		t.Error("two builds of one seq have one uuid")
	}
	if fieldOf(a, "note").String() != fieldOf(b, "note").String() {
		t.Error("two builds of one seq differ in more than the uuid")
	}
}

// Ground: contract (amendment Г) — a string field takes a placeholder at any
// depth: nested message, element of a repeated field, map value.
func TestAttachMethods_TemplatePlaceholdersInStringsAtAnyDepth(t *testing.T) {
	tpl := attachedOrder(t, map[string]any{
		"child": map[string]any{"name": "c-{{seq}}", "n": "{{seq}}"},
		"tags":  []any{"t-{{seq}}", "plain", "{{uuid}}"},
		"items": []any{map[string]any{"name": "i-{{seq}}"}, map[string]any{"name": "j", "n": 5}},
		"attrs": map[string]any{"k": "v-{{seq}}", "k2": "x"},
	})

	msg := decodeOrder(t, tpl.Build(12))

	child := fieldOf(msg, "child").Message()
	if got := fieldOf(child, "name").String(); got != "c-12" {
		t.Errorf("child.name = %q, want c-12", got)
	}
	if got := fieldOf(child, "n").Int(); got != 12 {
		t.Errorf("child.n = %d, want 12: a number field at depth takes the whole {{seq}}", got)
	}

	tags := fieldOf(msg, "tags").List()
	if tags.Len() != 3 || tags.Get(0).String() != "t-12" || tags.Get(1).String() != "plain" || !uuidV4.MatchString(tags.Get(2).String()) {
		t.Errorf("tags = %v, want [t-12 plain <uuid>]", tags)
	}

	items := fieldOf(msg, "items").List()
	if items.Len() != 2 || fieldOf(items.Get(0).Message(), "name").String() != "i-12" ||
		fieldOf(items.Get(1).Message(), "name").String() != "j" || fieldOf(items.Get(1).Message(), "n").Int() != 5 {
		t.Errorf("items = %v, want [{i-12} {j 5}]", items)
	}

	attrs := fieldOf(msg, "attrs").Map()
	if got := attrs.Get(protoreflect.ValueOfString("k").MapKey()).String(); got != "v-12" {
		t.Errorf("attrs[k] = %q, want v-12", got)
	}
	if got := attrs.Get(protoreflect.ValueOfString("k2").MapKey()).String(); got != "x" {
		t.Errorf("attrs[k2] = %q, want x", got)
	}
}

// Ground: contract (reviewer T3) — what a whole "{{seq}}" is follows the field
// by its descriptor: a number in a number field, the text "7" in a string field
// (a request id, a tag), not a refusal and not a number.
// Mutation: a whole "{{seq}}" is taken for a number whatever the field is.
func TestAttachMethods_TemplateWholeSeqInAStringFieldIsText(t *testing.T) {
	tpl := attachedOrder(t, map[string]any{
		"ref":   "{{seq}}",
		"count": "{{seq}}",
		"tags":  []any{"{{seq}}"},
		"attrs": map[string]any{"k": "{{seq}}"},
	})

	msg := decodeOrder(t, tpl.Build(7))

	if got := fieldOf(msg, "ref").String(); got != "7" {
		t.Errorf("ref = %q, want the text 7", got)
	}
	if got := fieldOf(msg, "count").Int(); got != 7 {
		t.Errorf("count = %d, want the number 7", got)
	}
	if tags := fieldOf(msg, "tags").List(); tags.Len() != 1 || tags.Get(0).String() != "7" {
		t.Errorf("tags = %v, want [7]", tags)
	}
	if got := fieldOf(msg, "attrs").Map().Get(protoreflect.ValueOfString("k").MapKey()).String(); got != "7" {
		t.Errorf("attrs[k] = %q, want the text 7", got)
	}
}

// Ground: contract (reviewer T6) — a {{uuid}} is one value where it stands: two
// of them in one body are two uuids, the same seq notwithstanding.
// Mutation: one uuid per request, written to every place.
func TestAttachMethods_TemplateTwoUuidsInOneBodyAreTwoValues(t *testing.T) {
	tpl := attachedOrder(t, map[string]any{"id": "{{uuid}}", "note": "{{uuid}}-{{uuid}}-{{seq}}"})

	msg := decodeOrder(t, tpl.Build(1))
	id, note := fieldOf(msg, "id").String(), fieldOf(msg, "note").String()
	if !uuidV4.MatchString(id) {
		t.Fatalf("id = %q, not a uuid", id)
	}

	parts := regexp.MustCompile(`^([0-9a-f-]{36})-([0-9a-f-]{36})-1$`).FindStringSubmatch(note)
	if len(parts) < 3 || !uuidV4.MatchString(parts[1]) || !uuidV4.MatchString(parts[2]) {
		t.Fatalf("note = %q, want <uuid>-<uuid>-1", note)
	}

	seen := map[string]bool{}
	for _, u := range []string{id, parts[1], parts[2]} {
		seen[u] = true
	}
	if len(seen) != 3 {
		t.Errorf("3 places for a uuid, %d distinct values: %s %s %s", len(seen), id, parts[1], parts[2])
	}

	all := map[string]bool{}
	for range 1000 {
		m := decodeOrder(t, tpl.Build(1))
		all[fieldOf(m, "id").String()] = true
		for _, u := range regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f-]{27}`).FindAllString(fieldOf(m, "note").String(), -1) {
			all[u] = true
		}
	}
	if len(all) != 3000 {
		t.Errorf("1000 builds of 3 uuids give %d distinct values, want 3000", len(all))
	}
}

// Ground: contract (amendment Г) — int64 goes through no float: a seq above 2^53
// arrives digit for digit.
func TestAttachMethods_TemplateInt64TakesSeqExactly(t *testing.T) {
	tpl := attachedOrder(t, map[string]any{"big": "{{seq}}", "u64": "{{seq}}"})

	const seq = uint64(9007199254740993) // 2^53 + 1: the first integer a float64 cannot hold
	msg := decodeOrder(t, tpl.Build(seq))

	if got := uint64(fieldOf(msg, "big").Int()); got != seq {
		t.Errorf("big = %d, want %d", got, seq)
	}
	if got := fieldOf(msg, "u64").Uint(); got != seq {
		t.Errorf("u64 = %d, want %d", got, seq)
	}
}

// Ground: contract (amendment Г, brief point 2) — a placeholder in a field it
// does not fit is a config error before the start, with the method and the path
// of the field, from the descriptor: not by the look of the YAML.
func TestAttachMethods_TemplateInAFieldItDoesNotFitIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]any
		path string
	}{
		{"bytes", map[string]any{"payload": "{{seq}}"}, "data.payload"},
		{"enum", map[string]any{"kind": "{{seq}}"}, "data.kind"},
		{"bool", map[string]any{"flag": "{{seq}}"}, "data.flag"},
		{"double", map[string]any{"ratio": "{{seq}}"}, "data.ratio"},
		{"float", map[string]any{"fl": "{{seq}}"}, "data.fl"},
		{"a Timestamp", map[string]any{"at": "{{uuid}}"}, "data.at"},
		{"a Duration", map[string]any{"wait": "{{seq}}"}, "data.wait"},
		{"a wrapper of a string", map[string]any{"label": "{{uuid}}"}, "data.label"},
		{"uuid in an int32", map[string]any{"count": "{{uuid}}"}, "data.count"},
		{"uuid in an int64", map[string]any{"big": "a-{{uuid}}"}, "data.big"},
		{"seq with a digit before it in a number", map[string]any{"count": "1{{seq}}"}, "data.count"},
		{"seq twice in a number", map[string]any{"count": "{{seq}}{{seq}}"}, "data.count"},
		{"seq with text after it in a number", map[string]any{"big": "{{seq}}x"}, "data.big"},
		{"a string for a message field", map[string]any{"child": "{{seq}}"}, "data.child"},
		{"text in a number of a nested message", map[string]any{"child": map[string]any{"n": "a-{{seq}}"}}, "data.child.n"},
		{"uuid in a number of a repeated message", map[string]any{"items": []any{map[string]any{"name": "ok"}, map[string]any{"n": "{{uuid}}"}}}, "data.items[1].n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A good placeholder beside it, so that the error cannot be "no placeholders".
			tc.data["id"] = "{{uuid}}"

			_, err := attachOrder(t, orderCall(templatesOn(), tc.data))

			if !errors.Is(err, ErrTemplateField) {
				t.Fatalf("error = %v, want ErrTemplateField", err)
			}
			for _, want := range []string{orderMethod, tc.path} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

// Ground: contract — one run of the check names every field, not the first.
func TestAttachMethods_TemplateErrorsNameEveryBadField(t *testing.T) {
	_, err := attachOrder(t, orderCall(templatesOn(), map[string]any{
		"id": "{{uuid}}", "payload": "{{seq}}", "flag": "{{seq}}",
	}))

	if !errors.Is(err, ErrTemplateField) {
		t.Fatalf("error = %v, want ErrTemplateField", err)
	}
	for _, want := range []string{"data.payload", "data.flag"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// Ground: contract — without templates the body is exactly as before: text
// stays text, a literal {{uuid}} goes out as it is, and the call has no Template.
// Mutation: placeholders work at templates: false.
func TestAttachMethods_TemplatesOffSendsBracesAsText(t *testing.T) {
	for name, on := range map[string]*bool{"absent": nil, "false": templatesOff()} {
		t.Run(name, func(t *testing.T) {
			call, err := attachOrder(t, orderCall(on, map[string]any{"note": "{{uuid}}-{{seq}}-{{nope}}"}))
			if err != nil {
				t.Fatalf("attach: %v", err)
			}

			if call.Template != nil {
				t.Error("a call with templates off has a Template")
			}
			if got := fieldOf(decodeOrder(t, call.Payload), "note").String(); got != "{{uuid}}-{{seq}}-{{nope}}" {
				t.Errorf("note = %q, want the text as written", got)
			}
		})
	}
}

// Ground: contract — a number field with templates off still takes only a number.
func TestAttachMethods_TemplatesOffNumberPlaceholderIsARequestDataError(t *testing.T) {
	_, err := attachOrder(t, orderCall(nil, map[string]any{"count": "{{seq}}"}))

	if !errors.Is(err, ErrRequestData) {
		t.Fatalf("error = %v, want ErrRequestData: the text is not a number", err)
	}
}

// -fake has no schema: CallsFromConfig leaves a template that counts and has no
// Build, so no body is made under -fake.
// Ground: contract (reviewer T5) — -fake counts and does not build.
// Mutation: -fake builds the body.
func TestCallsFromConfig_ATemplateWithoutASchemaCountsAndBuildsNothing(t *testing.T) {
	cfg := &config.MasterConfig{Load: config.Load{Calls: []config.Call{{
		Method: orderMethod, RPS: 10, Duration: time.Second,
		Data: map[string]any{"id": "{{uuid}}", "note": "n-{{seq}}"}, Templates: templatesOn(),
	}}}}

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}

	tpl := calls[0].Template
	if tpl == nil {
		t.Fatal("the call has no template")
	}
	if tpl.Build != nil {
		t.Errorf("Build is set: a call with no schema builds a body")
	}
	if !slices.Equal(tpl.Placeholders, []string{"seq", "uuid"}) {
		t.Errorf("placeholders = %v, want [seq uuid]", tpl.Placeholders)
	}
}

// Ground: contract — what the engine call carries: the placeholders the data
// uses, sorted; a counter of its own; no ready body (the body is made per request).
func TestAttachMethods_TemplateCallCarriesPlaceholdersAndCounter(t *testing.T) {
	call, err := attachOrder(t, orderCall(templatesOn(), map[string]any{"id": "{{uuid}}", "note": "n-{{seq}}"}))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	tpl := call.Template
	if tpl == nil {
		t.Fatal("the call has no Template")
	}
	if want := []string{"seq", "uuid"}; !slices.Equal(tpl.Placeholders, want) {
		t.Errorf("Placeholders = %q, want %q (sorted)", tpl.Placeholders, want)
	}
	if tpl.Counter == nil {
		t.Error("the Template has no Counter")
	}
	if call.Payload != nil || call.Payloads != nil {
		t.Errorf("the call holds a ready body (%q, %d payloads) beside the Template", call.Payload, len(call.Payloads))
	}

	only, err := attachOrder(t, orderCall(templatesOn(), map[string]any{"note": "n-{{seq}}"}))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if only.Template == nil || !slices.Equal(only.Template.Placeholders, []string{"seq"}) {
		t.Errorf("a call with only seq: Template %+v, want Placeholders [seq]", only.Template)
	}
}

// Ground: contract (amendment В) — the largest seq a call's field holds: 32-bit
// numbers are limited, 64-bit numbers and strings are not (0).
// Mutation: seq wraps in int32.
func TestAttachMethods_TemplateMaxIsThatOfTheNarrowestNumberField(t *testing.T) {
	const (
		maxInt32  = 2147483647
		maxUint32 = 4294967295
	)

	for _, tc := range []struct {
		name string
		data map[string]any
		want uint64
	}{
		{"int32", map[string]any{"count": "{{seq}}"}, maxInt32},
		{"sint32", map[string]any{"s32": "{{seq}}"}, maxInt32},
		{"sfixed32", map[string]any{"sf32": "{{seq}}"}, maxInt32},
		{"uint32", map[string]any{"u32": "{{seq}}"}, maxUint32},
		{"fixed32", map[string]any{"f32": "{{seq}}"}, maxUint32},
		{"int32 in a nested message", map[string]any{"child": map[string]any{"n": "{{seq}}"}}, maxInt32},
		{"int64", map[string]any{"big": "{{seq}}"}, 0},
		{"uint64", map[string]any{"u64": "{{seq}}"}, 0},
		{"sint64", map[string]any{"s64": "{{seq}}"}, 0},
		{"fixed64", map[string]any{"f64": "{{seq}}"}, 0},
		{"sfixed64", map[string]any{"sf64": "{{seq}}"}, 0},
		{"a string", map[string]any{"note": "{{seq}}"}, 0},
		{"a string and a 64-bit number", map[string]any{"note": "{{seq}}", "big": "{{seq}}"}, 0},
		{"an int32 and a uint32: the narrower", map[string]any{"count": "{{seq}}", "u32": "{{seq}}"}, maxInt32},
		{"an int32 beside a string", map[string]any{"count": "{{seq}}", "note": "{{seq}}"}, maxInt32},
		{"a uuid only", map[string]any{"id": "{{uuid}}"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := attachedOrder(t, tc.data).Max; got != tc.want {
				t.Errorf("Max = %d, want %d", got, tc.want)
			}
		})
	}
}

// textFields are the names of the string fields of Order (and the map that
// holds strings): a whole "{{seq}}" in one is the text "7", not the number.
var textFields = map[string]bool{"id": true, "note": true, "ref": true, "tags": true, "name": true, "attrs": true}

// substituted is data with every placeholder written out by plain string
// replacement, the naive path of the spec: a whole "{{seq}}" becomes a number
// in a number field and the text in a string field (reviewer T3), any other
// string takes the text.
func substituted(v any, seq uint64, uuid string) any {
	return substitutedIn(v, seq, uuid, false)
}

func substitutedIn(v any, seq uint64, uuid string, text bool) any {
	switch x := v.(type) {
	case string:
		if x == "{{seq}}" && !text {
			return int64(seq)
		}

		return strings.ReplaceAll(strings.ReplaceAll(x, "{{seq}}", strconv.FormatUint(seq, 10)), "{{uuid}}", uuid)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = substitutedIn(x[i], seq, uuid, text)
		}

		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = substitutedIn(e, seq, uuid, text || textFields[k])
		}

		return out
	default:
		return v
	}
}

// Ground: reference — a fast build (slots in place of a parse per request) is
// allowed only if it gives the bytes of the naive path: substitute, build with
// RequestBody, Marshal. 1100 values of seq over a nested, repeated and map body.
func TestAttachMethods_TemplateBuildEqualsTheNaivePath(t *testing.T) {
	data := map[string]any{
		"id":    "{{uuid}}",
		"note":  "n-{{seq}}-{{seq}}",
		"count": "{{seq}}",
		"big":   "{{seq}}",
		"child": map[string]any{"name": "c-{{seq}}", "n": "{{seq}}"},
		"tags":  []any{"t-{{seq}}", "plain", "{{seq}}"},
		"items": []any{map[string]any{"name": "i-{{seq}}"}, map[string]any{"name": "j", "n": 5}},
		"attrs": map[string]any{"k": "v-{{seq}}", "k2": "x", "k3": "{{seq}}"},
		"ref":   "{{seq}}",
	}
	tpl := attachedOrder(t, data)
	desc := orderMessage(t)

	seqs := []uint64{2147483647}
	for seq := uint64(1); seq <= 1100; seq++ {
		seqs = append(seqs, seq)
	}

	for _, seq := range seqs {
		got := tpl.Build(seq)

		uuid := fieldOf(decodeOrder(t, got), "id").String()
		if !uuidV4.MatchString(uuid) {
			t.Fatalf("seq %d: id = %q, not a uuid", seq, uuid)
		}

		want, err := RequestBody(desc, substituted(data, seq, uuid))
		if err != nil {
			t.Fatalf("seq %d: the naive path: %v", seq, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("seq %d: built %x, the naive path gives %x", seq, got, want)
		}
	}
}

// benchBody is the nested, repeated and map body of the reference test, cut
// down to what a real config carries: a few strings with a seq and a uuid.
var benchBody = map[string]any{
	"id":    "{{uuid}}",
	"note":  "n-{{seq}}",
	"count": "{{seq}}",
	"child": map[string]any{"name": "c-{{seq}}", "n": "{{seq}}"},
	"tags":  []any{"t-{{seq}}", "plain"},
}

// The build of one request, the fast path of the run against the naive one the
// reference test compares it with (substitute, RequestBody, Marshal). The gain
// of the fast path is the number in the report.
func BenchmarkTemplateBuild(b *testing.B) {
	tpl := attachedOrder(b, benchBody)

	b.Run("fast", func(b *testing.B) {
		b.ReportAllocs()

		var seq uint64

		for b.Loop() {
			seq++
			_ = tpl.Build(seq)
		}
	})

	b.Run("naive", func(b *testing.B) {
		desc := orderMessage(b)
		uuid := "0f8fad5b-d9cb-469f-a165-70867728950e"

		b.ReportAllocs()

		var seq uint64

		for b.Loop() {
			seq++

			if _, err := RequestBody(desc, substituted(benchBody, seq, uuid)); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func orderStages(rps int, d time.Duration) []engine.Stage {
	return []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: d}}
}

func seqLimited(limit uint64, stages ...engine.Stage) []engine.Call {
	return []engine.Call{{
		Method:   "/pkg.S/M",
		Template: engine.NewTemplate([]string{"seq"}, limit, nil),
		Stages:   stages,
	}}
}

// checkedWithin runs the check with a ceiling: an estimate that walks every
// tick of a long plan does not end, and the test says so instead of hanging.
func checkedWithin(t *testing.T, calls []engine.Call) error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- CheckTemplatesFit(calls) }()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the estimate walked the ticks of the plan: it must be arithmetic over the stages")

		return nil
	}
}

// Ground: boundary (amendment В) — the plan hands out rps x duration requests;
// a seq equal to the field's maximum fits, one more does not.
// Mutations: seq wraps in int32 (the estimate lets it through).
func TestCheckTemplatesFit_TheLastSeqThatFitsIsLegal(t *testing.T) {
	if err := checkedWithin(t, seqLimited(1000, orderStages(100, 10*time.Second)...)); err != nil {
		t.Errorf("1000 requests into a field of 1000: %v", err)
	}

	err := checkedWithin(t, seqLimited(999, orderStages(100, 10*time.Second)...))
	if !errors.Is(err, engine.ErrSeqOverflow) {
		t.Fatalf("1000 requests into a field of 999: error = %v, want engine.ErrSeqOverflow", err)
	}
	for _, want := range []string{"pkg.S/M", "999", "1000"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not give %q: the call, the limit and what the run hands out", err, want)
		}
	}
}

// Ground: contract — the plan is all its stages, the warm-up and ramp among
// them: the sum, not the largest stage.
func TestCheckTemplatesFit_StagesAreAdded(t *testing.T) {
	stages := []engine.Stage{
		{StartRPS: 100, TargetRPS: 100, Duration: 5 * time.Second},
		{StartRPS: 100, TargetRPS: 100, Duration: 5 * time.Second},
	}

	if err := checkedWithin(t, seqLimited(1000, stages...)); err != nil {
		t.Errorf("2 x 500 requests into a field of 1000: %v", err)
	}
	if err := checkedWithin(t, seqLimited(700, stages...)); !errors.Is(err, engine.ErrSeqOverflow) {
		t.Errorf("2 x 500 requests into a field of 700: error = %v, want engine.ErrSeqOverflow", err)
	}
}

// Ground: contract — the estimate is an upper bound: 3 rps for 1.5 s places 5
// ticks (the first is at 0), and a field of 4 would be passed by 4.5 cut down.
func TestCheckTemplatesFit_AFractionOfARequestCountsAsOne(t *testing.T) {
	stage := engine.Stage{StartRPS: 3, TargetRPS: 3, Duration: 1500 * time.Millisecond}
	if got := engine.ScheduledAfter([]engine.Stage{stage}, 0); got != 5 {
		t.Fatalf("test setup: the plan places %d requests, the test expects 5", got)
	}

	if err := checkedWithin(t, seqLimited(5, stage)); err != nil {
		t.Errorf("5 requests into a field of 5: %v", err)
	}
	if err := checkedWithin(t, seqLimited(4, stage)); !errors.Is(err, engine.ErrSeqOverflow) {
		t.Errorf("5 requests into a field of 4: error = %v, want engine.ErrSeqOverflow", err)
	}
}

// Ground: contract — a ramp is counted by an upper bound: it never lets
// through a run that overflows, and a field far past the plan passes.
func TestCheckTemplatesFit_ARampIsCountedByAnUpperBound(t *testing.T) {
	ramp := engine.Stage{StartRPS: 0, TargetRPS: 100, Duration: 10 * time.Second} // about 500 requests

	if err := checkedWithin(t, seqLimited(400, ramp)); !errors.Is(err, engine.ErrSeqOverflow) {
		t.Errorf("about 500 requests into a field of 400: error = %v, want engine.ErrSeqOverflow", err)
	}
	if err := checkedWithin(t, seqLimited(2000, ramp)); err != nil {
		t.Errorf("about 500 requests into a field of 2000: %v", err)
	}
}

// Ground: contract — a plan of real size is told by arithmetic: 1e6 rps for 3
// hours is 1.08e10 requests, past int32 and uint32, inside int64 (no limit).
func TestCheckTemplatesFit_APlanOfRealSizeIsArithmetic(t *testing.T) {
	long := engine.Stage{StartRPS: 1_000_000, TargetRPS: 1_000_000, Duration: 3 * time.Hour}

	for name, limit := range map[string]uint64{"int32": 2147483647, "uint32": 4294967295} {
		if err := checkedWithin(t, seqLimited(limit, long)); !errors.Is(err, engine.ErrSeqOverflow) {
			t.Errorf("%s: error = %v, want engine.ErrSeqOverflow", name, err)
		}
	}

	if err := checkedWithin(t, seqLimited(0, long)); err != nil {
		t.Errorf("a field without a limit: %v", err)
	}
}

// Ground: contract — only templated calls with a limit are checked; the others
// are not looked at, and an error names its own call.
func TestCheckTemplatesFit_OtherCallsAreLeftAlone(t *testing.T) {
	stage := orderStages(100, 10*time.Second)
	calls := []engine.Call{
		{Method: "/pkg.S/Plain", Payload: []byte("x"), Stages: stage},
		{Method: "/pkg.S/Free", Template: engine.NewTemplate([]string{"seq"}, 0, nil), Stages: stage},
		{Method: "/pkg.S/Narrow", Template: engine.NewTemplate([]string{"seq"}, 999, nil), Stages: stage},
	}

	err := checkedWithin(t, calls)
	if !errors.Is(err, engine.ErrSeqOverflow) {
		t.Fatalf("error = %v, want engine.ErrSeqOverflow", err)
	}
	if !strings.Contains(err.Error(), "pkg.S/Narrow") || strings.Contains(err.Error(), "pkg.S/Plain") || strings.Contains(err.Error(), "pkg.S/Free") {
		t.Errorf("error %q names a call that fits or the wrong one", err)
	}
}

// Ground: contract (amendment Д) — the line says what it counts: the requests
// handed to workers, ramp and warm-up included, which the report's own counts
// leave out. The placeholders are in the order the JSON gives them.
func TestReport_TemplatesLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		t    *engine.TemplatesReport
		want string
	}{
		{"both", &engine.TemplatesReport{Placeholders: []string{"seq", "uuid"}, LastSeq: 18578},
			"  data: templated, seq uuid (last seq 18578, ramp and warm-up counted)"},
		{"seq only", &engine.TemplatesReport{Placeholders: []string{"seq"}, LastSeq: 3},
			"  data: templated, seq (last seq 3, ramp and warm-up counted)"},
		{"uuid only", &engine.TemplatesReport{Placeholders: []string{"uuid"}, LastSeq: 40},
			"  data: templated, uuid (last seq 40, ramp and warm-up counted)"},
		{"nothing handed out", &engine.TemplatesReport{Placeholders: []string{"seq"}, LastSeq: 0},
			"  data: templated, seq (last seq 0, ramp and warm-up counted)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			PrintReport(&out, "localhost:50051", reportWith(engine.MethodReport{Method: "/pkg.S/M", Templates: tc.t}))

			if got := lineAfter(t, out.String(), "pkg.S/M "); got != tc.want {
				t.Errorf("line under the method = %q, want %q", got, tc.want)
			}
			if n := strings.Count(out.String(), "  data: "); n != 1 {
				t.Errorf("%d data lines, want 1:\n%s", n, out.String())
			}
		})
	}
}

// Ground: contract — the line belongs to its own method; a plain call has none.
func TestReport_TemplatesLineStandsUnderItsOwnMethod(t *testing.T) {
	var out strings.Builder
	PrintReport(&out, "localhost:50051", reportWith(
		engine.MethodReport{Method: "/pkg.S/Plain"},
		engine.MethodReport{Method: "/pkg.S/Templated", Templates: &engine.TemplatesReport{Placeholders: []string{"uuid"}, LastSeq: 9}},
	))

	if got := lineAfter(t, out.String(), "pkg.S/Plain "); strings.Contains(got, "data:") {
		t.Errorf("the plain method has a data line: %q", got)
	}
	if got, want := lineAfter(t, out.String(), "pkg.S/Templated "), "  data: templated, uuid (last seq 9, ramp and warm-up counted)"; got != want {
		t.Errorf("line under the row = %q, want %q", got, want)
	}
}

// Ground: contract (brief point 7, reviewer T4) — methods[].templates is an
// object, the placeholders used (sorted) and the last seq handed out, or null;
// the key is always there; schema_version stays 1.
func TestJSON_Templates(t *testing.T) {
	out := writeJSON(t, jsonRun(engine.Report{Methods: []engine.MethodReport{
		{Method: "/pkg.S/Templated", Sent: 1, Templates: &engine.TemplatesReport{Placeholders: []string{"seq", "uuid"}, LastSeq: 5}},
		{Method: "/pkg.S/Plain", Sent: 1},
	}}))

	methods := field(t, out, "methods").([]any)

	got, ok := field(t, methods[0], "templates").(map[string]any)
	if !ok {
		t.Fatalf("templated: templates = %v, want an object", field(t, methods[0], "templates"))
	}
	if list, _ := got["placeholders"].([]any); len(list) != 2 || list[0] != "seq" || list[1] != "uuid" {
		t.Errorf("templated: placeholders = %v, want [seq uuid]", got["placeholders"])
	}
	if got["last_seq"] != 5.0 {
		t.Errorf("templated: last_seq = %v, want 5", got["last_seq"])
	}
	if len(got) != 2 {
		t.Errorf("templated: templates = %v, want only placeholders and last_seq", got)
	}
	if v := field(t, methods[1], "templates"); v != nil {
		t.Errorf("plain: templates = %v, want null", v)
	}
	if v := field(t, out, "schema_version"); v != 1.0 {
		t.Errorf("schema_version = %v, want 1: a field was added, not changed", v)
	}
}

func overflowedRun() RunReport {
	run := reportWith(engine.MethodReport{
		Method: "/pkg.S/Templated", Templates: &engine.TemplatesReport{Placeholders: []string{"seq"}, LastSeq: 2147483647},
	})
	run.Incomplete = true
	run.SeqOverflow = &engine.SeqOverflow{Method: "/pkg.S/Templated", Limit: 2147483647}

	return run
}

// Ground: contract (reviewer T1) — the person who sees exit 3 sees why: the
// report names the call, the limit and what that means for the numbers.
// Mutation: the report is printed without the cause of the stop.
func TestReport_SeqOverflowNamesTheCallAndTheLimit(t *testing.T) {
	var out strings.Builder
	PrintReport(&out, "localhost:50051", overflowedRun())

	for _, want := range []string{"pkg.S/Templated", "2147483647", "{{seq}}", "incomplete"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report lacks %q:\n%s", want, out.String())
		}
	}
}

// Ground: contract (reviewer T1) — the JSON says the same: stop_reason
// "seq_overflow" and an object with the call and the limit; null when it did
// not happen.
func TestJSON_SeqOverflow(t *testing.T) {
	run := overflowedRun()

	out := writeJSON(t, JSONRun{
		Target: "localhost:50051", Version: "v0.0.0-test", Outcome: OutcomeComplete,
		StartedAt: jsonRun(engine.Report{}).StartedAt, Run: run,
	})

	if got := field(t, out, "stop_reason"); got != "seq_overflow" {
		t.Errorf("stop_reason = %v, want seq_overflow", got)
	}
	if got := field(t, out, "seq_overflow", "method"); got != "/pkg.S/Templated" {
		t.Errorf("seq_overflow.method = %v, want /pkg.S/Templated", got)
	}
	if got := field(t, out, "seq_overflow", "limit"); got != 2147483647.0 {
		t.Errorf("seq_overflow.limit = %v, want 2147483647", got)
	}

	plain := writeJSON(t, jsonRun(engine.Report{}))
	if got := field(t, plain, "seq_overflow"); got != nil {
		t.Errorf("a run that fit: seq_overflow = %v, want null", got)
	}
	if got := field(t, plain, "stop_reason"); got != nil {
		t.Errorf("a run that fit: stop_reason = %v, want null", got)
	}
}
