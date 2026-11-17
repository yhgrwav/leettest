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
	"math"
	"regexp"
	"sync"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// Extract (v0.9 unit 4b, the run side). The extract of a call or a step reads
// a value out of the reply bytes into the iteration's vals; Build of a later
// step writes it into the request. Both are checked here on bytes, through
// AttachMethods as the run makes them. Stub: the engine fields exist,
// AttachMethods still refuses a config with extract as not supported yet, so
// every test below that attaches is red until the code.
//
// wideReply has a field of every scalar kind a path may end in; the order
// message of the 4a tests is the request.

// wideReply is a reply with one field of each kind an extract reads. Names in
// snake case: the JSON name of text_value is textValue.
func wideReply(t testing.TB) protoreflect.MessageDescriptor {
	t.Helper()

	const optional = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL

	field := func(name string, num int32, kind descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
		f := &descriptorpb.FieldDescriptorProto{
			Name: proto.String(name), Number: proto.Int32(num), Type: kind.Enum(), Label: optional.Enum(),
		}
		if typeName != "" {
			f.TypeName = proto.String(typeName)
		}

		return f
	}

	const (
		str  = descriptorpb.FieldDescriptorProto_TYPE_STRING
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
		msg  = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	)

	opt := field("opt", 17, i32, "")
	opt.Proto3Optional = proto.Bool(true)
	opt.OneofIndex = proto.Int32(0)

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: proto.String("leettest/test/wide.proto"), Package: proto.String("leettest.wide"), Syntax: proto.String("proto3"),
		Dependency: []string{"google/protobuf/wrappers.proto"},
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Mode"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("MODE_NONE"), Number: proto.Int32(0)},
				{Name: proto.String("MODE_FAST"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:  proto.String("Inner"),
				Field: []*descriptorpb.FieldDescriptorProto{field("amount", 1, i64, ""), field("name", 2, str, "")},
			},
			{
				Name: proto.String("Wide"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("text_value", 1, str, ""),
					field("n32", 2, i32, ""), field("n64", 3, i64, ""),
					field("u32", 4, u32, ""), field("u64", 5, u64, ""),
					field("s32", 6, s32, ""), field("s64", 7, s64, ""),
					field("fx32", 8, f32, ""), field("fx64", 9, f64, ""),
					field("sf32", 10, sf32, ""), field("sf64", 11, sf64, ""),
					field("flag", 12, descriptorpb.FieldDescriptorProto_TYPE_BOOL, ""),
					field("mode", 13, descriptorpb.FieldDescriptorProto_TYPE_ENUM, ".leettest.wide.Mode"),
					field("ratio", 14, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, ""),
					field("fl", 15, descriptorpb.FieldDescriptorProto_TYPE_FLOAT, ""),
					field("inner", 16, msg, ".leettest.wide.Inner"),
					opt,
					field("label", 18, msg, ".google.protobuf.StringValue"),
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("_opt")}},
			},
		},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("build the test file: %v", err)
	}

	return file.Messages().ByName("Wide")
}

// wideBody is a Wide reply on the wire with the fields fill sets.
func wideBody(t testing.TB, fill func(set func(name string, v protoreflect.Value), inner func(name string, v protoreflect.Value))) []byte {
	t.Helper()

	msg := dynamicpb.NewMessage(wideReply(t))
	fd := func(m protoreflect.Message, name string) protoreflect.FieldDescriptor {
		f := m.Descriptor().Fields().ByName(protoreflect.Name(name))
		if f == nil {
			t.Fatalf("no field %s in the test reply", name)
		}

		return f
	}

	fill(
		func(name string, v protoreflect.Value) { msg.Set(fd(msg, name), v) },
		func(name string, v protoreflect.Value) {
			in := msg.Mutable(fd(msg, "inner")).Message()
			in.Set(fd(in, name), v)
		},
	)

	body, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal the reply: %v", err)
	}

	return body
}

// wideBodyOf sets the one field name of the reply.
func wideBodyOf(t testing.TB, name string, v protoreflect.Value) []byte {
	t.Helper()

	return wideBody(t, func(set func(string, protoreflect.Value), _ func(string, protoreflect.Value)) { set(name, v) })
}

// attachWide attaches cfg as the run does, with every method's reply the Wide
// message, and returns the call.
func attachWide(t testing.TB, cfg *config.MasterConfig) engine.Call {
	t.Helper()

	resolver := newExtractResolver(t, nil)
	wide := wideReply(t)
	for _, m := range resolver.known {
		m.Output = wide
	}

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}
	if _, _, err = AttachMethods(t.Context(), resolver, cfg, calls); err != nil {
		t.Fatalf("AttachMethods() = %v, want a config that passes every check accepted", err)
	}

	return calls[0]
}

// extractedOrder runs the extract of the call on reply, builds the first step
// with what it took and decodes the body. The error is the extract's.
func extractedOrder(t *testing.T, call engine.Call, reply []byte) (protoreflect.Message, error) {
	t.Helper()

	if call.Extract == nil {
		t.Fatal("the call has no Extract")
	}
	if len(call.Next) == 0 || call.Next[0].Build == nil {
		t.Fatal("the step that reads has no Build")
	}

	vals := make([]string, call.Values)
	if err := call.Extract(reply, vals); err != nil {
		return nil, err
	}

	return decodeOrder(t, call.Next[0].Build(7, vals)), nil
}

// wantField says the field of msg holds want: a string, an int64 or a uint64.
func wantField(t *testing.T, msg protoreflect.Message, name string, want any) {
	t.Helper()

	switch w := want.(type) {
	case string:
		if got := fieldOf(msg, name).String(); got != w {
			t.Errorf("%s = %q, want %q", name, got, w)
		}
	case int64:
		if got := fieldOf(msg, name).Int(); got != w {
			t.Errorf("%s = %d, want %d", name, got, w)
		}
	case uint64:
		if got := fieldOf(msg, name).Uint(); got != w {
			t.Errorf("%s = %d, want %d", name, got, w)
		}
	default:
		t.Fatalf("a want of type %T", want)
	}
}

// Ground: contract — spec 4 and 5: a value is written as the text of its kind,
// and a number goes into a number field whole: a string as is, integers in
// decimal, a bool as true, an enum by its name (an unknown number in decimal),
// a float with the shortest digits of its own width. The path is read by the
// proto name and by the JSON name, through a message, through a wrapper.
// Mutations: a float32 formatted at 64 bits (0.10000000149011612); an enum
// written as its number; a uint64 through int64 (-1); a string quoted or
// escaped; the path read by the proto name only.
func TestExtract_AValueIsWrittenAsTheTextOfItsKind(t *testing.T) {
	const whole = "{{extract.x}}"

	for _, tc := range []struct {
		name string
		path string
		body func() []byte
		dest string
		text string
		want any
	}{
		{"a string", "text_value", func() []byte { return wideBodyOf(t, "text_value", protoreflect.ValueOfString("hello")) }, "note", "v=" + whole, "v=hello"},
		{"a string by the JSON name", "textValue", func() []byte { return wideBodyOf(t, "text_value", protoreflect.ValueOfString("hello")) }, "note", "v=" + whole, "v=hello"},
		{"a string as it is, spaces and non-ASCII", "text_value", func() []byte { return wideBodyOf(t, "text_value", protoreflect.ValueOfString(`a "b"/ü `)) }, "note", whole, `a "b"/ü `},
		{"a string twice in a text", "text_value", func() []byte { return wideBodyOf(t, "text_value", protoreflect.ValueOfString("hello")) }, "note", "a=" + whole + "/b=" + whole, "a=hello/b=hello"},
		{"an int32 below zero in text", "n32", func() []byte { return wideBodyOf(t, "n32", protoreflect.ValueOfInt32(-5)) }, "note", "n=" + whole, "n=-5"},
		{"an int32 into int32", "n32", func() []byte { return wideBodyOf(t, "n32", protoreflect.ValueOfInt32(-5)) }, "count", whole, int64(-5)},
		{"an int64 at its top into int64", "n64", func() []byte { return wideBodyOf(t, "n64", protoreflect.ValueOfInt64(math.MaxInt64)) }, "big", whole, int64(math.MaxInt64)},
		{"an int64 at its bottom in text", "n64", func() []byte { return wideBodyOf(t, "n64", protoreflect.ValueOfInt64(math.MinInt64)) }, "note", whole, "-9223372036854775808"},
		{"a uint32 at its top into int64", "u32", func() []byte { return wideBodyOf(t, "u32", protoreflect.ValueOfUint32(math.MaxUint32)) }, "big", whole, int64(math.MaxUint32)},
		{"a uint32 at its top into uint32", "u32", func() []byte { return wideBodyOf(t, "u32", protoreflect.ValueOfUint32(math.MaxUint32)) }, "u32", whole, uint64(math.MaxUint32)},
		{"a uint64 at its top into uint64", "u64", func() []byte { return wideBodyOf(t, "u64", protoreflect.ValueOfUint64(math.MaxUint64)) }, "u64", whole, uint64(math.MaxUint64)},
		{"a uint64 at its top in text", "u64", func() []byte { return wideBodyOf(t, "u64", protoreflect.ValueOfUint64(math.MaxUint64)) }, "note", whole, "18446744073709551615"},
		{"a sint32 into sint32", "s32", func() []byte { return wideBodyOf(t, "s32", protoreflect.ValueOfInt32(-3)) }, "s32", whole, int64(-3)},
		{"a sint32 into int64", "s32", func() []byte { return wideBodyOf(t, "s32", protoreflect.ValueOfInt32(-3)) }, "big", whole, int64(-3)},
		{"a sint64 at its bottom into sint64", "s64", func() []byte { return wideBodyOf(t, "s64", protoreflect.ValueOfInt64(math.MinInt64)) }, "s64", whole, int64(math.MinInt64)},
		{"a fixed32 at its top into fixed32", "fx32", func() []byte { return wideBodyOf(t, "fx32", protoreflect.ValueOfUint32(math.MaxUint32)) }, "f32", whole, uint64(math.MaxUint32)},
		{"a fixed32 at its top into int64", "fx32", func() []byte { return wideBodyOf(t, "fx32", protoreflect.ValueOfUint32(math.MaxUint32)) }, "big", whole, int64(math.MaxUint32)},
		{"a fixed64 at its top into fixed64", "fx64", func() []byte { return wideBodyOf(t, "fx64", protoreflect.ValueOfUint64(math.MaxUint64)) }, "f64", whole, uint64(math.MaxUint64)},
		{"a sfixed32 into sfixed32", "sf32", func() []byte { return wideBodyOf(t, "sf32", protoreflect.ValueOfInt32(-7)) }, "sf32", whole, int64(-7)},
		{"a sfixed64 into sfixed64", "sf64", func() []byte { return wideBodyOf(t, "sf64", protoreflect.ValueOfInt64(-9)) }, "sf64", whole, int64(-9)},
		{"a bool", "flag", func() []byte { return wideBodyOf(t, "flag", protoreflect.ValueOfBool(true)) }, "note", whole, "true"},
		{"an enum by its name", "mode", func() []byte { return wideBodyOf(t, "mode", protoreflect.ValueOfEnum(1)) }, "note", whole, "MODE_FAST"},
		{"an enum the schema does not know, in decimal", "mode", func() []byte { return wideBodyOf(t, "mode", protoreflect.ValueOfEnum(7)) }, "note", whole, "7"},
		{"a double", "ratio", func() []byte { return wideBodyOf(t, "ratio", protoreflect.ValueOfFloat64(0.5)) }, "note", whole, "0.5"},
		{"a double with an exponent", "ratio", func() []byte { return wideBodyOf(t, "ratio", protoreflect.ValueOfFloat64(2.5e10)) }, "note", whole, "2.5e+10"},
		{"a double that is a tenth", "ratio", func() []byte { return wideBodyOf(t, "ratio", protoreflect.ValueOfFloat64(0.1)) }, "note", whole, "0.1"},
		{"a float with the digits of its own width", "fl", func() []byte { return wideBodyOf(t, "fl", protoreflect.ValueOfFloat32(0.1)) }, "note", whole, "0.1"},
		{"a field of a message", "inner.amount", func() []byte {
			return wideBody(t, func(_ func(string, protoreflect.Value), inner func(string, protoreflect.Value)) {
				inner("amount", protoreflect.ValueOfInt64(77))
			})
		}, "big", whole, int64(77)},
		{"a string of a wrapper", "label.value", func() []byte {
			return wideBody(t, func(set func(string, protoreflect.Value), _ func(string, protoreflect.Value)) {
				// a StringValue is a message with the one field value
				set("label", protoreflect.ValueOfMessage(stringValue(t, "w")))
			})
		}, "note", whole, "w"},
		{"an optional set to zero", "opt", func() []byte { return wideBodyOf(t, "opt", protoreflect.ValueOfInt32(0)) }, "note", whole, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := attachWide(t, extractConfig(map[string]string{"x": tc.path}, readerStep(map[string]any{tc.dest: tc.text})))

			msg, err := extractedOrder(t, call, tc.body())
			if err != nil {
				t.Fatalf("Extract() = %v, want the value taken", err)
			}

			wantField(t, msg, tc.dest, tc.want)
		})
	}
}

// stringValue is a google.protobuf.StringValue holding text.
func stringValue(t testing.TB, text string) protoreflect.Message {
	t.Helper()

	desc := wideReply(t).Fields().ByName("label").Message()
	msg := dynamicpb.NewMessage(desc)
	msg.Set(desc.Fields().ByName("value"), protoreflect.ValueOfString(text))

	return msg
}

// Ground: boundary — spec 4, a value that is not there: a field the reply does
// not hold, a proto3 field at its implicit zero (the wire cannot tell it from
// absent), a message that is not there, a reply that does not decode. All are
// the one missing value, named by the name and the path; only a proto3
// optional that is set is a value at zero.
// Mutations: a zero read as a value ("0", "", "false"); an absent message read
// as a value; an undecodable reply read as a value or passed as success.
func TestExtract_AMissingValueIsAnErrorThatNamesTheNameAndThePath(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		body []byte
	}{
		{"an empty reply", "text_value", nil},
		{"another field set", "text_value", wideBodyOf(t, "n32", protoreflect.ValueOfInt32(4))},
		{"an empty string", "text_value", wideBodyOf(t, "text_value", protoreflect.ValueOfString(""))},
		{"an int at zero", "n32", wideBodyOf(t, "n32", protoreflect.ValueOfInt32(0))},
		{"a bool at false", "flag", wideBodyOf(t, "flag", protoreflect.ValueOfBool(false))},
		{"an enum at its zero", "mode", wideBodyOf(t, "mode", protoreflect.ValueOfEnum(0))},
		{"a double at zero", "ratio", wideBodyOf(t, "ratio", protoreflect.ValueOfFloat64(0))},
		{"a message that is not there", "inner.amount", wideBodyOf(t, "n32", protoreflect.ValueOfInt32(4))},
		{"a message that is there with the field at zero", "inner.amount", wideBody(t, func(_ func(string, protoreflect.Value), inner func(string, protoreflect.Value)) {
			inner("name", protoreflect.ValueOfString("n"))
		})},
		{"an optional that is not set", "opt", wideBodyOf(t, "n32", protoreflect.ValueOfInt32(4))},
		{"a wrapper that is not there", "label.value", nil},
		{"a reply that does not decode", "text_value", []byte{0xff, 0xff, 0xff, 0xff}},
		{"a reply cut inside a field", "text_value", []byte{0x0a, 0x09, 'a'}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := attachWide(t, extractConfig(map[string]string{"x": tc.path}, readerStep(map[string]any{"note": "{{extract.x}}"})))

			_, err := extractedOrder(t, call, tc.body)
			if err == nil {
				t.Fatalf("Extract() = nil, want the value missing")
			}

			if want := "extract x: " + tc.path + " not in the response"; err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
		})
	}
}

// Of two names the one that is not in the reply is the one that is named.
// Ground: contract — spec 4, the error is the name that failed.
func TestExtract_TheMissingNameIsTheOneNamed(t *testing.T) {
	call := attachWide(t, extractConfig(
		map[string]string{"a": "text_value", "b": "n32"},
		readerStep(map[string]any{"note": "{{extract.a}}-{{extract.b}}"}),
	))

	_, err := extractedOrder(t, call, wideBodyOf(t, "text_value", protoreflect.ValueOfString("here")))
	if err == nil || err.Error() != "extract b: n32 not in the response" {
		t.Fatalf("Extract() = %v, want %q", err, "extract b: n32 not in the response")
	}
}

// A field that goes on the wire twice is read as the protobuf spec says, the
// last one; a last one at zero is a zero.
// Ground: contract — spec 4, last wins for a repeated wire field.
// Mutations: the first value kept; the values of both joined.
func TestExtract_AFieldTwiceOnTheWireTakesTheLast(t *testing.T) {
	twice := func(first, last func([]byte) []byte) []byte {
		return last(first(nil))
	}
	text := func(s string) func([]byte) []byte {
		return func(b []byte) []byte {
			return protowire.AppendString(protowire.AppendTag(b, 1, protowire.BytesType), s)
		}
	}
	number := func(v uint64) func([]byte) []byte {
		return func(b []byte) []byte {
			return protowire.AppendVarint(protowire.AppendTag(b, 2, protowire.VarintType), v)
		}
	}

	t.Run("a string", func(t *testing.T) {
		call := attachWide(t, extractConfig(map[string]string{"x": "text_value"}, readerStep(map[string]any{"note": "{{extract.x}}"})))

		msg, err := extractedOrder(t, call, twice(text("first"), text("second")))
		if err != nil {
			t.Fatalf("Extract() = %v", err)
		}
		wantField(t, msg, "note", "second")
	})

	t.Run("a number", func(t *testing.T) {
		call := attachWide(t, extractConfig(map[string]string{"x": "n32"}, readerStep(map[string]any{"count": "{{extract.x}}"})))

		msg, err := extractedOrder(t, call, twice(number(5), number(9)))
		if err != nil {
			t.Fatalf("Extract() = %v", err)
		}
		wantField(t, msg, "count", int64(9))
	})

	t.Run("a last one at zero is a zero", func(t *testing.T) {
		call := attachWide(t, extractConfig(map[string]string{"x": "text_value"}, readerStep(map[string]any{"note": "{{extract.x}}"})))

		_, err := extractedOrder(t, call, twice(text("first"), text("")))
		if err == nil || err.Error() != "extract x: text_value not in the response" {
			t.Fatalf("Extract() = %v, want the value missing", err)
		}
	})
}

// A value outlives the reply it was read from: the engine drops the reply
// right after the extract, and a pooled buffer is written over.
// Ground: contract — the vals hold their own text.
// Mutation: a string made over the reply bytes (unsafe) instead of copied.
func TestExtract_AValueDoesNotAliasTheReply(t *testing.T) {
	call := attachWide(t, extractConfig(map[string]string{"x": "text_value"}, readerStep(map[string]any{"note": "{{extract.x}}"})))

	reply := wideBodyOf(t, "text_value", protoreflect.ValueOfString("hello"))
	vals := make([]string, call.Values)
	if err := call.Extract(reply, vals); err != nil {
		t.Fatalf("Extract() = %v", err)
	}

	for i := range reply {
		reply[i] = 'Z'
	}

	wantField(t, decodeOrder(t, call.Next[0].Build(1, vals)), "note", "hello")
}

// A name is one place in the iteration's vals, whichever call or step names
// it: the call's and a step's are two names, each read from its own reply.
// Ground: contract — spec 5, the name to its place in vals across a scenario.
// Mutations: two names in one place (the later overwrites); a step's name
// read from the call's reply; Values smaller than the number of names.
func TestExtract_NamesOfTheCallAndOfAStepAreSeparatePlacesInTheIteration(t *testing.T) {
	cfg := extractConfig(
		map[string]string{"a": "text_value"},
		middleStep(map[string]string{"b": "n32"}),
		readerStep(map[string]any{"note": "{{extract.a}}|{{extract.b}}", "big": "{{extract.b}}"}),
	)
	call := attachWide(t, cfg)

	if call.Values != 2 {
		t.Errorf("Values = %d, want 2: one place for each of the two names", call.Values)
	}
	if call.Extract == nil || call.Next[0].Extract == nil {
		t.Fatalf("Extract of the call = %v, of the step = %v, want both set", call.Extract != nil, call.Next[0].Extract != nil)
	}

	vals := make([]string, call.Values)

	// Each reply holds the other name's field with another value: reading the
	// wrong reply, or the wrong field, shows.
	first := wideBody(t, func(set func(string, protoreflect.Value), _ func(string, protoreflect.Value)) {
		set("text_value", protoreflect.ValueOfString("A"))
		set("n32", protoreflect.ValueOfInt32(99))
	})
	second := wideBody(t, func(set func(string, protoreflect.Value), _ func(string, protoreflect.Value)) {
		set("text_value", protoreflect.ValueOfString("wrong"))
		set("n32", protoreflect.ValueOfInt32(7))
	})

	if err := call.Extract(first, vals); err != nil {
		t.Fatalf("call.Extract() = %v", err)
	}
	if err := call.Next[0].Extract(second, vals); err != nil {
		t.Fatalf("step.Extract() = %v", err)
	}

	msg := decodeOrder(t, call.Next[1].Build(3, vals))
	wantField(t, msg, "note", "A|7")
	wantField(t, msg, "big", int64(7))
}

// What the call and its steps ask of the engine follows from what they extract:
// the reply is kept for an extract and for nothing else, and the vals are made
// only when there is a name.
// Ground: contract — spec 5, the hot path of a scenario with no extract is
// untouched: no Extract, no kept reply, no vals.
// Mutations: KeepResponse always set; Values set for a scenario with no
// extract; the call's Extract set for an extract on a step only.
func TestExtract_WhatTheEngineIsToldFollowsWhatIsExtracted(t *testing.T) {
	read := readerStep(map[string]any{"note": "{{extract.x}}"})

	for _, tc := range []struct {
		name        string
		cfg         func() *config.MasterConfig
		wantCall    bool
		wantStep    bool
		wantValues  int
		wantKeepOnC bool
	}{
		{"no extract anywhere", func() *config.MasterConfig {
			return extractConfig(nil, middleStep(nil), readerStep(map[string]any{"note": "n-{{seq}}"}))
		}, false, false, 0, false},
		{"an extract on the call", func() *config.MasterConfig {
			return extractConfig(map[string]string{"x": "text_value"}, read)
		}, true, false, 1, true},
		{"an extract on a step only", func() *config.MasterConfig {
			return extractConfig(nil, middleStep(map[string]string{"x": "text_value"}), read)
		}, false, true, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := attachWide(t, tc.cfg())

			if got := call.Extract != nil; got != tc.wantCall {
				t.Errorf("call.Extract set = %v, want %v", got, tc.wantCall)
			}
			if got := call.Next[0].Extract != nil; got != tc.wantStep {
				t.Errorf("steps[0].Extract set = %v, want %v", got, tc.wantStep)
			}
			if call.Values != tc.wantValues {
				t.Errorf("Values = %d, want %d", call.Values, tc.wantValues)
			}
			if call.KeepResponse != tc.wantKeepOnC {
				t.Errorf("KeepResponse = %v, want %v: the call's reply is kept only when the call extracts", call.KeepResponse, tc.wantKeepOnC)
			}
		})
	}
}

// Placeholders of the three kinds in one text, and the same Build used for
// iteration after iteration: each body has its own values and keeps nothing
// of the one before.
// Ground: contract — spec 5, Build is a function of seq and vals alone.
// Mutations: the previous value left in a slot; a text slot that appends to
// what was there; the seq of an earlier body.
func TestExtract_BuildIsAFunctionOfSeqAndValsAlone(t *testing.T) {
	call := attachWide(t, extractConfig(
		map[string]string{"x": "text_value", "n": "n32"},
		readerStep(map[string]any{"note": "w-{{extract.x}}-{{seq}}-{{uuid}}", "count": "{{extract.n}}", "big": "{{seq}}"}),
	))

	build := call.Next[0].Build
	vals := make([]string, call.Values)

	reply := func(text string, n int32) []byte {
		return wideBody(t, func(set func(string, protoreflect.Value), _ func(string, protoreflect.Value)) {
			set("text_value", protoreflect.ValueOfString(text))
			set("n32", protoreflect.ValueOfInt32(n))
		})
	}

	shape := regexp.MustCompile(`^w-(\w+)-(\d+)-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	for _, tc := range []struct {
		seq  uint64
		text string
		n    int32
	}{{1, "alpha", 5}, {2, "b", -6}, {3, "gamma", 7}} {
		if err := call.Extract(reply(tc.text, tc.n), vals); err != nil {
			t.Fatalf("Extract() = %v", err)
		}

		msg := decodeOrder(t, build(tc.seq, vals))

		m := shape.FindStringSubmatch(fieldOf(msg, "note").String())
		if m == nil {
			t.Fatalf("note = %q, want w-<value>-<seq>-<uuid v4>", fieldOf(msg, "note").String())
		}
		if m[1] != tc.text || m[2] != fmt.Sprint(tc.seq) {
			t.Errorf("note holds value %q and seq %s, want %q and %d", m[1], m[2], tc.text, tc.seq)
		}
		wantField(t, msg, "count", int64(tc.n))
		wantField(t, msg, "big", int64(tc.seq))
	}
}

// Builds from many goroutines, each with the values of its own iteration:
// no body holds a value of another. The race detector sees the shared message
// of the builder.
// Ground: concurrency — workers of one call build and extract at once; the
// builder's message is shared, the vals and the replies are not.
// Mutations: Build without its lock; Extract decoding into one shared message.
func TestExtract_ConcurrentIterationsKeepTheirOwnValues(t *testing.T) {
	call := attachWide(t, extractConfig(
		map[string]string{"x": "text_value", "n": "n64"},
		readerStep(map[string]any{"note": "{{extract.x}}", "big": "{{extract.n}}"}),
	))

	const (
		workers = 8
		rounds  = 200
	)

	var wg sync.WaitGroup

	errs := make(chan string, workers)

	for w := range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			vals := make([]string, call.Values)

			for i := range rounds {
				text := fmt.Sprintf("w%d-%d", w, i)
				number := int64(w*1000 + i + 1)

				reply := wideBody(t, func(set func(string, protoreflect.Value), _ func(string, protoreflect.Value)) {
					set("text_value", protoreflect.ValueOfString(text))
					set("n64", protoreflect.ValueOfInt64(number))
				})
				if err := call.Extract(reply, vals); err != nil {
					errs <- fmt.Sprintf("worker %d round %d: Extract() = %v", w, i, err)

					return
				}

				msg := decodeOrder(t, call.Next[0].Build(uint64(i+1), vals))
				if got := fieldOf(msg, "note").String(); got != text {
					errs <- fmt.Sprintf("worker %d round %d: note = %q, want %q", w, i, got, text)

					return
				}
				if got := fieldOf(msg, "big").Int(); got != number {
					errs <- fmt.Sprintf("worker %d round %d: big = %d, want %d", w, i, got, number)

					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}
}

// A text that is a plain string and nothing else in it still builds as it did:
// a step with only {{seq}} takes no vals and no Extract.
// Ground: contract — the step with no extract is unchanged by the vals.
func TestExtract_AStepWithoutExtractBuildsFromSeqAlone(t *testing.T) {
	call := attachWide(t, extractConfig(nil, middleStep(nil), readerStep(map[string]any{"note": "n-{{seq}}", "big": "{{seq}}"})))

	msg := decodeOrder(t, call.Next[1].Build(42, nil))

	wantField(t, msg, "note", "n-42")
	wantField(t, msg, "big", int64(42))
}
