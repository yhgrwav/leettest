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
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// ErrRequestData says a call's data does not fit its method's request message.
var ErrRequestData = errors.New("request data does not fit the method")

// RequestBody builds a call's data into the wire form of message desc.
//
// The data goes YAML → JSON → protojson, so the type rules are protojson's:
// enums by name, well-known types in their JSON form, bytes in base64. No
// number passes through float64 on the way: the YAML decoder keeps integers as
// int64 or uint64 and encoding/json writes them digit for digit, so an ID above
// 2^53 arrives as written instead of rounded.
func RequestBody(desc protoreflect.MessageDescriptor, data any) ([]byte, error) {
	return requestBody(protojson.UnmarshalOptions{}, desc, data)
}

// requestBody is RequestBody reading with opts: the types of the method's own
// files, where an Any may name them.
func requestBody(opts protojson.UnmarshalOptions, desc protoreflect.MessageDescriptor, data any) ([]byte, error) {
	msg, err := requestMessage(opts, desc, data)
	if err != nil {
		return nil, err
	}

	// Deterministic, so every run of the same config sends the same bytes.
	return proto.MarshalOptions{Deterministic: true}.Marshal(msg)
}

// requestMessage is the message data makes, before it is written to the wire.
func requestMessage(opts protojson.UnmarshalOptions, desc protoreflect.MessageDescriptor, data any) (*dynamicpb.Message, error) {
	msg := dynamicpb.NewMessage(desc)

	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}

		// Strict: an unknown field is an error, not a silently dropped typo.
		if err := opts.Unmarshal(raw, msg); err != nil {
			return nil, withProtoNames(err, desc)
		}
	}

	return msg, nil
}

// recordBody builds one record of a dataset, the JSON of a line as written,
// into the wire form of message desc. The bytes go to protojson as they are,
// so an integer above 2^53 arrives as written, as it does from data.
//
// It says only whether the record fits: protojson's own text quotes the value,
// an unknown key and a duplicate map key, and a record may hold a secret.
func recordBody(opts protojson.UnmarshalOptions, desc protoreflect.MessageDescriptor, raw []byte) (body []byte, fits bool) {
	msg := dynamicpb.NewMessage(desc)

	if err := opts.Unmarshal(raw, msg); err != nil {
		return nil, false
	}

	// Deterministic, so every run of the same dataset sends the same bytes.
	body, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)

	return body, err == nil
}

// datasetBodies builds every record of call's dataset, in file order. The
// first record that does not fit is the error, named by its line in the file.
func datasetBodies(opts protojson.UnmarshalOptions, desc protoreflect.MessageDescriptor, call *config.Call) ([][]byte, error) {
	bodies, bad := recordBodies(opts, desc, call.Records)
	if bad > 0 {
		return nil, fmt.Errorf("%w: %s: dataset %s:%d: does not fit %s",
			ErrRequestData, call.Method, call.Dataset, bad, desc.FullName())
	}

	return bodies, nil
}

// recordBodies builds every record in file order. bad is the line of the first
// record that does not fit, 0 when all do; bodies are then nil: all or none.
func recordBodies(opts protojson.UnmarshalOptions, desc protoreflect.MessageDescriptor, records []config.DatasetRecord) (bodies [][]byte, bad int) {
	bodies = make([][]byte, len(records))

	for i, record := range records {
		body, fits := recordBody(opts, desc, record.JSON)
		if !fits {
			return nil, record.Line
		}

		bodies[i] = body
	}

	return bodies, 0
}

// Unchecked is a method nothing could be checked against before the run, and
// why: reflection off, refused, or not answering.
type Unchecked struct {
	Method string
	Err    error
}

// ErrCredentialsRejected is a method check the target refused as
// Unauthenticated while the config sent metadata.
var ErrCredentialsRejected = errors.New("target rejected credentials")

// AttachData resolves every method the run will call and fills in the payload
// of the calls that have data. It runs once, before the load: a method the
// target does not serve, or a body that does not fit its message, is a config
// error, and finding it out from a whole run of failures costs the run.
// Without reflection a method that has no data is only reported to warn, since
// it needs no schema to run.
//
// calls are the engine calls built from cfg, in the same order.
func AttachData(
	ctx context.Context, resolver descriptor.Resolver, cfg *config.MasterConfig,
	calls []engine.Call,
) (unchecked []Unchecked, err error) {
	unchecked, _, err = AttachMethods(ctx, resolver, cfg, calls)

	return unchecked, err
}

// AttachMethods is AttachData that also hands out what it resolved: the method
// of each call, in the order of calls, nil for one that could not be resolved
// (the ones in unchecked).
func AttachMethods(
	ctx context.Context, resolver descriptor.Resolver, cfg *config.MasterConfig,
	calls []engine.Call,
) (unchecked []Unchecked, methods []*descriptor.Method, err error) {
	var errs []error

	methods = make([]*descriptor.Method, len(cfg.Load.Calls))

	// The narrowest {{seq}} field of the templated steps of each call, folded
	// into the call's template once every call has its own.
	narrowest := make([]narrowestLimit, len(cfg.Load.Calls))

	for i := range cfg.Load.Calls {
		call := &cfg.Load.Calls[i]

		// What each extract name reads, by name, from the replies met so far. The
		// call's own extract comes first: its method is resolved before the steps,
		// for once, so that a step that reads it finds it.
		sources := map[string]protoreflect.FieldDescriptor{}

		var (
			method     *descriptor.Method
			resolveErr error
			resolved   bool
		)

		if len(call.Extract) > 0 {
			method, resolveErr = resolver.Resolve(ctx, call.Method)
			resolved = true

			if resolveErr == nil {
				errs = append(errs, extractSources(method, fmt.Sprintf("load.calls[%d]", i), call.Extract, sources)...)
			}
		}

		// The steps first: whatever the call itself turns out to be, a typo
		// in a step, or a body that does not fit it, costs the run too.
		for k, step := range call.Steps {
			stepMethod, stepUnchecked, stepErr := checkStepMethod(ctx, resolver, cfg, i, k, step)
			if stepUnchecked != nil {
				unchecked = append(unchecked, *stepUnchecked)
			}
			if stepErr != nil {
				errs = append(errs, stepErr)
			}
			if stepMethod == nil {
				continue
			}
			if len(step.Extract) > 0 {
				errs = append(errs, extractSources(stepMethod, fmt.Sprintf("load.calls[%d].steps[%d]", i, k), step.Extract, sources)...)
			}
			if step.Dataset != "" {
				// The step's own method checks its own file: all or none, as the call's.
				bodies, bad := recordBodies(stepMethod.UnmarshalOptions(), stepMethod.Input, step.Records)
				if bad > 0 {
					errs = append(errs, fmt.Errorf("%w: load.calls[%d].steps[%d].dataset: %s:%d: does not fit %s",
						ErrRequestData, i, k, step.Dataset, bad, stepMethod.Input.FullName()))

					continue
				}

				calls[i].Next[k].Payloads = bodies

				continue
			}
			if step.Data == nil {
				continue
			}

			if step.TemplatesOn() {
				// Made per iteration by the worker, with the iteration's seq; the step
				// holds no ready body. The narrow field is remembered by its step.
				at := fmt.Sprintf("load.calls[%d].steps[%d] (%s)", i, k, step.Method)

				build, limit, tplErr := compileTemplate(stepMethod, at, step.Data, sources)
				if tplErr != nil {
					errs = append(errs, tplErr)

					continue
				}

				calls[i].Next[k].Build = build
				narrowest[i].add(limit, fmt.Sprintf("%s (steps[%d])", calls[i].Next[k].Method, k))

				continue
			}

			// The step's own method, not the call's: its fields are the step's.
			body, bodyErr := requestBody(stepMethod.UnmarshalOptions(), stepMethod.Input, step.Data)
			if bodyErr != nil {
				errs = append(errs, fmt.Errorf("%w: load.calls[%d].steps[%d].data: %s: %w", ErrRequestData, i, k, step.Method, bodyErr))

				continue
			}

			calls[i].Next[k].Payload = body
		}

		if !resolved {
			method, resolveErr = resolver.Resolve(ctx, call.Method)
		}
		if resolveErr == nil {
			methods[i] = method
		}

		switch {

		// The metadata sent was refused: every call carries it, so the run
		// would only show a config mistake as the target's result. Only the
		// code is named; the target's message may echo what was sent.
		// PermissionDenied is not this: the credentials were accepted and may
		// serve the calls while not reaching reflection.
		case len(cfg.App.Metadata) > 0 && status.Code(resolveErr) == codes.Unauthenticated:
			errs = append(errs, fmt.Errorf("%s: %w (%s)", call.Method, ErrCredentialsRejected, codes.Unauthenticated))

			continue
		case resolveErr == nil:
		// An extract is a promise about the reply, which only a schema can check.
		case len(call.Extract) > 0 && errors.Is(resolveErr, descriptor.ErrReflectionUnsupported):
			errs = append(errs, fmt.Errorf("load.calls[%d] (%s): %w: the schema for its extract comes from reflection; "+
				"set app.proto or app.protoset to give it another way, or drop the extract: "+
				"a value cannot be read from a reply that has no schema", i, call.Method, resolveErr))

			continue
		// A method with no data needs no schema, so a resolver that could not
		// answer only costs the check, not the run. Which it was matters: a
		// target that never enabled reflection is not the same as one that
		// asked for credentials or did not answer in time.
		case call.Data == nil && call.Dataset == "" && len(call.Extract) == 0 && !errors.Is(resolveErr, descriptor.ErrMethodNotFound):
			if status.Code(resolveErr) == codes.Unauthenticated {
				resolveErr = fmt.Errorf("target requires credentials; app.metadata is not set: %w", resolveErr)
			}
			unchecked = append(unchecked, Unchecked{Method: call.Method, Err: resolveErr})

			continue
		case errors.Is(resolveErr, descriptor.ErrReflectionUnsupported) && call.Dataset != "":
			errs = append(errs, fmt.Errorf("%s: %w: the schema for its dataset comes from reflection; "+
				"set app.proto or app.protoset to give it another way, or drop the dataset: "+
				"without dataset the method runs with an empty message", call.Method, resolveErr))

			continue
		case errors.Is(resolveErr, descriptor.ErrReflectionUnsupported):
			errs = append(errs, fmt.Errorf("%s: %w: the schema for its data comes from reflection; "+
				"set app.proto or app.protoset to give it another way, or drop the data: "+
				"without data the method runs with an empty message", call.Method, resolveErr))

			continue
		default:
			// Named here rather than left to the resolver: which method the
			// run cannot make is ours to say, whoever resolves it.
			errs = append(errs, fmt.Errorf("%s: %w", call.Method, resolveErr))

			continue
		}

		if call.Dataset != "" {
			bodies, err := datasetBodies(method.UnmarshalOptions(), method.Input, call)
			if err != nil {
				errs = append(errs, err)

				continue
			}
			// All or none: a half-built dataset is never handed to the engine.
			calls[i].Payloads = bodies

			continue
		}

		if call.Data == nil {
			if call.TemplatesOn() {
				// Templated for the sake of its steps: step 1 is the empty message.
				calls[i].Template = engine.NewTemplate(call.AllTemplatePlaceholders(), 0, func(uint64) []byte { return nil })
			}

			continue
		}

		if call.TemplatesOn() {
			tpl, err := buildTemplate(method, call)
			if err != nil {
				errs = append(errs, err)

				continue
			}
			// The body is made per request; the call holds none.
			calls[i].Template = tpl

			continue
		}

		body, err := requestBody(method.UnmarshalOptions(), method.Input, call.Data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%w: %s: %w", ErrRequestData, call.Method, err))

			continue
		}

		calls[i].Payload = body
	}

	for i := range narrowest {
		narrowest[i].narrow(calls[i].Template)
	}

	if len(errs) == 0 && extractIn(cfg) {
		// Every check passed; the run side of extract is not there yet.
		return unchecked, methods, ErrExtractNotYet
	}

	return unchecked, methods, errors.Join(errs...)
}

// extractIn says some call or step of cfg has an extract.
func extractIn(cfg *config.MasterConfig) bool {
	for i := range cfg.Load.Calls {
		call := &cfg.Load.Calls[i]
		if len(call.Extract) > 0 {
			return true
		}

		for _, step := range call.Steps {
			if len(step.Extract) > 0 {
				return true
			}
		}
	}

	return false
}

// checkStepMethod asks the target about step k (counting from 0, after the
// call) of call i and returns the method it found. A step with no data needs no
// schema, so a resolver that could not answer only costs the check: that is the
// unchecked it returns. With data the body cannot be built, and the run would
// send an empty message instead: that is an error, as it is for a call. A
// method the target does not have, and credentials it refused, are errors that
// name the step.
func checkStepMethod(
	ctx context.Context, resolver descriptor.Resolver, cfg *config.MasterConfig, i, k int, step config.Step,
) (*descriptor.Method, *Unchecked, error) {
	method, err := resolver.Resolve(ctx, step.Method)
	if err == nil {
		return method, nil, nil
	}

	at := fmt.Sprintf("load.calls[%d].steps[%d] (%s)", i, k, step.Method)

	switch {
	case len(cfg.App.Metadata) > 0 && status.Code(err) == codes.Unauthenticated:
		return nil, nil, fmt.Errorf("%s: %w (%s)", at, ErrCredentialsRejected, codes.Unauthenticated)
	case errors.Is(err, descriptor.ErrMethodNotFound):
		return nil, nil, fmt.Errorf("%s: %w", at, err)
	case step.Dataset != "" && errors.Is(err, descriptor.ErrReflectionUnsupported):
		return nil, nil, fmt.Errorf("%s: %w: the schema for its dataset comes from reflection; "+
			"set app.proto or app.protoset to give it another way, or drop the dataset: "+
			"without dataset the step runs with an empty message", at, err)
	case step.Data != nil && errors.Is(err, descriptor.ErrReflectionUnsupported):
		return nil, nil, fmt.Errorf("%s: %w: the schema for its data comes from reflection; "+
			"set app.proto or app.protoset to give it another way, or drop the data: "+
			"without data the step runs with an empty message", at, err)
	case len(step.Extract) > 0 && errors.Is(err, descriptor.ErrReflectionUnsupported):
		return nil, nil, fmt.Errorf("%s: %w: the schema for its extract comes from reflection; "+
			"set app.proto or app.protoset to give it another way, or drop the extract: "+
			"a value cannot be read from a reply that has no schema", at, err)
	case step.Data != nil || step.Dataset != "" || len(step.Extract) > 0:
		return nil, nil, fmt.Errorf("%s: %w", at, err)
	}

	if status.Code(err) == codes.Unauthenticated {
		err = fmt.Errorf("target requires credentials; app.metadata is not set: %w", err)
	}

	return nil, &Unchecked{Method: step.Method, Err: err}, nil
}

// withProtoNames adds the .proto name of a field protojson names by its JSON
// name: the config is usually written with response_type, and an error about
// responseType reads like a different field.
func withProtoNames(err error, desc protoreflect.MessageDescriptor) error {
	jsonName := fieldInError(err.Error())
	if jsonName == "" {
		return err
	}

	// protojson names the field by JSON name alone: when fields of different
	// messages share it, any one name could send the user to a field that is fine.
	names := protoNames(desc, map[protoreflect.FullName]bool{}, map[string]map[string]bool{})[jsonName]
	if len(names) != 1 {
		return err
	}

	var name string
	for n := range names {
		name = n
	}
	if name == jsonName {
		return err
	}

	return fmt.Errorf("%w (field %s in the .proto)", err, name)
}

// fieldInError returns the JSON name protojson put in the text of an error, or
// "" if it named no field.
//
// protojson spells a field by its JSON name in exactly one message —
// "invalid value for <kind> field <jsonName>: <value>". On an unknown or a
// duplicate field it quotes the key as the config wrote it, and there is
// nothing to translate. The name is read out of that shape rather than looked
// up by substring: JSON names nest (idX inside idXRay), and the rejected value
// printed after the colon can name any field it likes.
func fieldInError(text string) string {
	_, rest, ok := strings.Cut(text, "invalid value for ")
	if !ok {
		return ""
	}

	_, rest, ok = strings.Cut(rest, " field ")
	if !ok {
		return ""
	}

	name, _, ok := strings.Cut(rest, ":")
	if !ok {
		return ""
	}

	return name
}

// protoNames collects, for the JSON name of every field reachable from desc,
// the .proto names spelled that way. seen stops at recursive messages.
func protoNames(desc protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool, names map[string]map[string]bool) map[string]map[string]bool {
	if seen[desc.FullName()] {
		return names
	}
	seen[desc.FullName()] = true

	fields := desc.Fields()
	for i := range fields.Len() {
		field := fields.Get(i)
		if names[field.JSONName()] == nil {
			names[field.JSONName()] = map[string]bool{}
		}
		names[field.JSONName()][string(field.Name())] = true
		if inner := field.Message(); inner != nil {
			protoNames(inner, seen, names)
		}
	}

	return names
}
