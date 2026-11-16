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
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// Step data (v0.9 unit 3a): config.Step.Data to engine.Step.Payload, checked
// against the step method's input before the load. The config is built by hand;
// the red tests of Parse are in pkg/config. The step methods take the order
// message (fields id, note, count, child...), the call's own method and the
// third step take an empty one, so a body checked against the wrong method is
// a body that does not fit.

const wsPing = "wallet.v1.WalletService/Ping"

// stepDataResolver knows every method of scenarioConfig: the second one, the
// first step's, takes the order message, the others the empty one.
func stepDataResolver(t *testing.T) *fakeResolver {
	t.Helper()

	empty := (&emptypb.Empty{}).ProtoReflect().Descriptor()

	return &fakeResolver{known: map[string]protoreflect.MessageDescriptor{
		wsFirst: empty, wsSecond: orderMessage(t), wsThird: empty, wsPing: empty,
	}}
}

// wantOrderBody is the wire form of an order with the id and the count, made
// field by field and not through the code under test.
func wantOrderBody(t *testing.T, id string, count int32) []byte {
	t.Helper()

	desc := orderMessage(t)
	msg := dynamicpb.NewMessage(desc)
	msg.Set(desc.Fields().ByName("id"), protoreflect.ValueOfString(id))
	msg.Set(desc.Fields().ByName("count"), protoreflect.ValueOfInt32(count))

	body, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	return body
}

// A step's data is the message the step sends: its encoded body is the step's
// Payload. The first step's own method does not take these fields, the second's
// does, so a check against the call's method would refuse a good body. The
// other step and the call keep no body.
// Ground: contract — spec 3 and 6, the body is the step method's message.
func TestAttachMethods_StepDataIsEncodedIntoThePayload(t *testing.T) {
	cfg := scenarioConfig()
	cfg.Load.Calls[0].Steps[0].Data = map[string]any{"id": "a-1", "count": 7}
	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	unchecked, _, err := AttachMethods(t.Context(), stepDataResolver(t), cfg, calls)

	if err != nil {
		t.Fatalf("AttachMethods() = %v, want the body to fit the step's method", err)
	}
	if len(unchecked) != 0 {
		t.Errorf("unchecked = %v, want none", unchecked)
	}
	want := wantOrderBody(t, "a-1", 7)
	if len(want) == 0 {
		t.Fatal("the expected body is empty: the test is not set up")
	}
	if got := calls[0].Next[0].Payload; !bytes.Equal(got, want) {
		t.Errorf("Next[0].Payload = %x, want %x", got, want)
	}
	if got := calls[0].Next[1].Payload; got != nil {
		t.Errorf("Next[1].Payload = %x, want nil: a step without data keeps the empty message", got)
	}
	if got := calls[0].Payload; got != nil {
		t.Errorf("the call's Payload = %x, want nil: a step's data is not the call's", got)
	}
}

// The body of a step is the body the same data gives a call of the same method:
// the same bytes, so a number above 2^53, an empty map, a nested message and a
// `{{` come out as they do for a call, and a config moved between the two does
// not change what goes on the wire.
// Ground: contract — spec 3 and 5, the call's own check, the call's own bytes.
func TestAttachMethods_StepDataIsEncodedLikeACallsData(t *testing.T) {
	for name, data := range map[string]any{
		"plain":           map[string]any{"id": "a-1", "count": 7},
		"braces are text": map[string]any{"id": "{{uuid}}", "note": "{{seq}}"},
		"empty map":       map[string]any{},
		"nested":          map[string]any{"child": map[string]any{"name": "n", "n": 3}, "tags": []any{"x", "y"}},
		"above 2^53":      map[string]any{"big": uint64(9007199254740993)},
	} {
		t.Run(name, func(t *testing.T) {
			stepCfg := scenarioConfig()
			stepCfg.Load.Calls[0].Steps[0].Data = data
			stepCalls, err := CallsFromConfig(stepCfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = AttachMethods(t.Context(), stepDataResolver(t), stepCfg, stepCalls); err != nil {
				t.Fatalf("AttachMethods() of the step = %v", err)
			}

			callCfg := &config.MasterConfig{Load: config.Load{Calls: []config.Call{
				{Method: wsSecond, RPS: 20, Duration: 10 * time.Second, Timeout: time.Second, Data: data},
			}}}
			callCalls, err := CallsFromConfig(callCfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = AttachMethods(t.Context(), stepDataResolver(t), callCfg, callCalls); err != nil {
				t.Fatalf("AttachMethods() of the call = %v: the case is not a call's valid body", err)
			}

			got, want := stepCalls[0].Next[0].Payload, callCalls[0].Payload
			if !bytes.Equal(got, want) {
				t.Errorf("step Payload = %x, want the call's %x for the same data", got, want)
			}
			if (got == nil) != (want == nil) {
				t.Errorf("step Payload nil = %v, call's nil = %v: they differ in having a body at all", got == nil, want == nil)
			}
		})
	}
}

// A body that does not fit the step's message is a config error before the
// load, named by its path in the config, the way a call's bad data is named by
// its method: a typo in a field name, a value of the wrong type, and the second
// step's own data are all found, and a good step beside a bad one does not hide it.
// Ground: contract — spec 3, the same exit code as a call's bad data (ErrRequestData).
func TestAttachMethods_StepDataWithAWrongFieldIsAConfigError(t *testing.T) {
	good := map[string]any{"id": "a-1"}

	for _, tc := range []struct {
		name    string
		first   any
		second  any
		want    []string // paths and words the error names
		notWant []string
		bad     []int // the steps whose body must not reach the engine
	}{
		{"a field that does not exist", map[string]any{"idd": "a-1"}, nil,
			[]string{"load.calls[0].steps[0].data", wsSecond, "idd"}, []string{"steps[1]"}, []int{0}},
		{"a value of the wrong type", map[string]any{"count": "seven"}, nil,
			[]string{"load.calls[0].steps[0].data", wsSecond, "count"}, []string{"steps[1]"}, []int{0}},
		{"the second step's data", good, map[string]any{"nope": 1},
			[]string{"load.calls[0].steps[1].data", wsThird, "nope"}, []string{"steps[0]"}, []int{1}},
		{"both steps", map[string]any{"idd": 1}, map[string]any{"nope": 1},
			[]string{"load.calls[0].steps[0].data", "load.calls[0].steps[1].data"}, nil, []int{0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := scenarioConfig()
			cfg.Load.Calls[0].Steps[0].Data = tc.first
			cfg.Load.Calls[0].Steps[1].Data = tc.second
			calls, err := CallsFromConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}

			_, _, err = AttachMethods(t.Context(), stepDataResolver(t), cfg, calls)

			if !errors.Is(err, ErrRequestData) {
				t.Fatalf("error = %v, want %v: the exit code of a call's bad data", err, ErrRequestData)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to name %q", err, want)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(err.Error(), notWant) {
					t.Errorf("error = %q, names %q, a step that is fine", err, notWant)
				}
			}
			for _, k := range tc.bad {
				if got := calls[0].Next[k].Payload; got != nil {
					t.Errorf("Next[%d].Payload = %x, want none: a body that does not fit is not handed to the engine", k, got)
				}
			}
		})
	}
}

// Without reflection a step with data cannot be built at all, as a call with
// data cannot: the error is the call's («the schema for its data comes from
// reflection», app.proto and app.protoset as the other way) and names the step.
// A step beside it without data only costs the check, as today.
// Ground: contract — spec 4.
func TestAttachMethods_StepDataWithoutReflectionIsRefused(t *testing.T) {
	cfg := scenarioConfig()
	cfg.Load.Calls[0].Steps[0].Data = map[string]any{"id": "a-1"}
	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{err: descriptor.ErrReflectionUnsupported}

	unchecked, _, err := AttachMethods(t.Context(), resolver, cfg, calls)

	if !errors.Is(err, descriptor.ErrReflectionUnsupported) {
		t.Fatalf("error = %v, want %v", err, descriptor.ErrReflectionUnsupported)
	}
	for _, want := range []string{
		"load.calls[0].steps[0]", wsSecond, "the schema for its data comes from reflection", "app.proto", "app.protoset",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "steps[1]") {
		t.Errorf("error = %q, names the step that has no data", err)
	}
	var named []string
	for _, u := range unchecked {
		named = append(named, u.Method)
	}
	if !strings.Contains(strings.Join(named, " "), wsThird) {
		t.Errorf("unchecked = %v, want the step without data (%s) still only warned about", named, wsThird)
	}
	if strings.Contains(strings.Join(named, " "), wsSecond) {
		t.Errorf("unchecked = %v, names the step with data: it is an error, not a warning", named)
	}
	if calls[0].Next[0].Payload != nil {
		t.Errorf("Next[0].Payload = %x, want none", calls[0].Next[0].Payload)
	}
}

// Reflection that is there but refuses or does not answer is no different for a
// step with data: the body cannot be checked, and a step that went out with an
// empty message instead would be a number that lies.
// Ground: contract — spec 4 read as the call reads it: data needs a schema, whatever kept it.
func TestAttachMethods_StepDataWithAResolverThatFailedIsNotUnchecked(t *testing.T) {
	cfg := scenarioConfig()
	cfg.Load.Calls[0].Steps[0].Data = map[string]any{"id": "a-1"}
	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	refused := status.Error(codes.PermissionDenied, "reflection needs a token")

	unchecked, _, err := AttachMethods(t.Context(), &fakeResolver{err: refused}, cfg, calls)

	if !errors.Is(err, refused) {
		t.Fatalf("error = %v, want the resolver's own error", err)
	}
	if !strings.Contains(err.Error(), "load.calls[0].steps[0]") {
		t.Errorf("error = %q, want it to name load.calls[0].steps[0]", err)
	}
	for _, u := range unchecked {
		if u.Method == wsSecond {
			t.Errorf("unchecked names %s: a step with data that could not be built was let through", wsSecond)
		}
	}
}

// A step without data keeps the empty message of its method, with reflection and
// without it, whatever the step beside it carries: the payload stays nil, as
// unit 2 left it.
// Ground: contract — spec 6, «byte for byte».
func TestAttachMethods_AStepWithoutDataKeepsAnEmptyPayload(t *testing.T) {
	for name, tc := range map[string]struct {
		resolver func(*testing.T) *fakeResolver
		withData bool
	}{
		"reflection, no data anywhere":       {stepDataResolver, false},
		"reflection, data on the other step": {stepDataResolver, true},
		"no reflection, no data anywhere": {func(*testing.T) *fakeResolver {
			return &fakeResolver{err: descriptor.ErrReflectionUnsupported}
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := scenarioConfig()
			if tc.withData {
				// The step that has data is the first; the second is the one checked.
				cfg.Load.Calls[0].Steps[0].Data = map[string]any{"id": "a-1", "count": 7}
			}
			calls, err := CallsFromConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}

			if _, _, err = AttachMethods(t.Context(), tc.resolver(t), cfg, calls); err != nil {
				t.Fatalf("AttachMethods() = %v", err)
			}

			want := []engine.Step{{Method: "/" + wsSecond}, {Method: "/" + wsThird}}
			for k := range calls[0].Next {
				if tc.withData && k == 0 {
					continue
				}
				if got := calls[0].Next[k].Payload; got != nil {
					t.Errorf("Next[%d].Payload = %x, want nil: the empty message", k, got)
				}
				if got := calls[0].Next[k].Method; got != want[k].Method {
					t.Errorf("Next[%d].Method = %q, want %q", k, got, want[k].Method)
				}
			}
			if got := calls[0].Payload; got != nil {
				t.Errorf("the call's Payload = %x, want nil", got)
			}
		})
	}
}
