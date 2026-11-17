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
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
)

// extractPlace is where an extract name reads its value: the field a path
// leads to, the messages it goes through, and the place of the value in the
// iteration's vals (one place for each name of the scenario, in the order the
// names are declared).
type extractPlace struct {
	hops  []protoreflect.FieldDescriptor
	leaf  protoreflect.FieldDescriptor
	index int
	path  string
}

// extractSources checks each path of extract against the reply of method and
// records where each name reads in into, giving it the next place of vals. at
// is the path of the call or of the step that extracts (load.calls[0],
// load.calls[0].steps[1]). A path that is wrong is an error and is not
// recorded. The function that runs the extract on a reply is nil when no name
// of extract was recorded.
func extractSources(
	method *descriptor.Method, at string, extract map[string]string, into map[string]extractPlace,
) (func(resp []byte, vals []string) error, []error) {
	names := make([]string, 0, len(extract))
	for name := range extract {
		names = append(names, name)
	}

	sort.Strings(names)

	var (
		errs  []error
		taken []string
	)

	for _, name := range names {
		where := at + ".extract." + name

		hops, leaf, err := extractLeaf(method.Output, extract[name])
		if err != nil {
			errs = append(errs, fmt.Errorf("%w: %s: %q: %w", config.ErrScenario, where, extract[name], err))

			continue
		}

		into[name] = extractPlace{hops: hops, leaf: leaf, index: len(into), path: extract[name]}
		taken = append(taken, name)
	}

	if len(taken) == 0 {
		return nil, errs
	}

	e := &extractor{output: method.Output}
	for _, name := range taken {
		e.names = append(e.names, name)
		e.sources = append(e.sources, into[name])
	}

	return e.run, errs
}

// extractor reads the values of one reply: the names of a call or a step, each
// with the place it reads.
type extractor struct {
	output  protoreflect.MessageDescriptor
	names   []string
	sources []extractPlace
}

// run takes the value of each name out of the reply into vals. A name whose
// value is not there, or a reply that does not decode, is an error naming the
// first such name: the target answered and the client cannot use the answer.
// A value is the text of its kind; a field the proto3 wire leaves out when it
// is zero is not there (a proto3 optional that is set is).
func (e *extractor) run(resp []byte, vals []string) error {
	msg := dynamicpb.NewMessage(e.output)
	// A reply a required field of is missing is still a reply: whether the
	// target met its own schema is not what this run measures.
	decoded := proto.UnmarshalOptions{AllowPartial: true}.Unmarshal(resp, msg) == nil

	for i := range e.sources {
		src := &e.sources[i]

		text, ok := "", false
		if decoded {
			text, ok = src.read(msg)
		}
		if !ok {
			return fmt.Errorf("extract %s: %s not in the response", e.names[i], src.path)
		}

		vals[src.index] = text
	}

	return nil
}

// read is the text of the value the path leads to in msg; false when a message
// on the way or the field itself is not there.
func (s *extractPlace) read(msg protoreflect.Message) (string, bool) {
	m := msg

	for _, hop := range s.hops {
		if !m.Has(hop) {
			return "", false
		}

		m = m.Get(hop).Message()
	}

	if !m.Has(s.leaf) {
		return "", false
	}

	return valueText(s.leaf, m.Get(s.leaf)), true
}

// valueText is v, a value of the field fd, as text: a string as it is, an
// integer in decimal, a bool as true or false, an enum by the name of its
// value (in decimal when the schema has no such value), a float or a double
// with the fewest digits that read back as the same number of its own width.
func valueText(fd protoreflect.FieldDescriptor, v protoreflect.Value) string {
	switch fd.Kind() {
	case protoreflect.StringKind:
		return v.String()
	case protoreflect.BoolKind:
		return strconv.FormatBool(v.Bool())
	case protoreflect.EnumKind:
		if value := fd.Enum().Values().ByNumber(v.Enum()); value != nil {
			return string(value.Name())
		}

		return strconv.FormatInt(int64(v.Enum()), 10)
	case protoreflect.FloatKind:
		return strconv.FormatFloat(v.Float(), 'g', -1, 32)
	case protoreflect.DoubleKind:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return strconv.FormatUint(v.Uint(), 10)
	default:
		// The signed integers: the path check let no other kind through.
		return strconv.FormatInt(v.Int(), 10)
	}
}

// extractLeaf is the field a path of names joined by dots leads to from the
// message msg: a single value of a kind that can be written as text, and the
// fields it goes through. A hop may go through a message and through nothing
// else; a name is looked up by its JSON name and then by its proto name, as
// the data of a request is.
func extractLeaf(msg protoreflect.MessageDescriptor, path string) (hops []protoreflect.FieldDescriptor, leaf protoreflect.FieldDescriptor, err error) {
	names := strings.Split(path, ".")

	for i, name := range names {
		if name == "" {
			return nil, nil, errors.New("a name between dots is empty")
		}

		fd := msg.Fields().ByJSONName(name)
		if fd == nil {
			fd = msg.Fields().ByName(protoreflect.Name(name))
		}
		if fd == nil {
			return nil, nil, fmt.Errorf("no field %q in %s", name, msg.FullName())
		}

		if i < len(names)-1 {
			switch {
			case fd.IsMap():
				return nil, nil, fmt.Errorf("%q is a map: a path cannot go through it", name)
			case fd.IsList():
				return nil, nil, fmt.Errorf("%q is repeated: a path cannot go through it", name)
			case fd.Message() == nil:
				return nil, nil, fmt.Errorf("%q is %s, not a message: a path cannot go below it", name, fd.Kind())
			}

			hops = append(hops, fd)
			msg = fd.Message()

			continue
		}

		switch {
		case fd.IsMap():
			return nil, nil, fmt.Errorf("%q is a map: name a single value", name)
		case fd.IsList():
			return nil, nil, fmt.Errorf("%q is repeated: name a single value", name)
		case fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind:
			return nil, nil, fmt.Errorf("%q is a message: name one of its fields, as %s.<field>", name, name)
		case fd.Kind() == protoreflect.BytesKind:
			return nil, nil, fmt.Errorf("%q is bytes: a value that is not text cannot be extracted", name)
		}

		return hops, fd, nil
	}

	// Split gives at least one name.
	return nil, nil, errors.New("the path is empty")
}

// integerKinds are the number kinds an extract may go into a number field from:
// how wide and whether signed.
var integerKinds = map[protoreflect.Kind]struct {
	bits   int
	signed bool
}{
	protoreflect.Int32Kind: {32, true}, protoreflect.Sint32Kind: {32, true}, protoreflect.Sfixed32Kind: {32, true},
	protoreflect.Int64Kind: {64, true}, protoreflect.Sint64Kind: {64, true}, protoreflect.Sfixed64Kind: {64, true},
	protoreflect.Uint32Kind: {32, false}, protoreflect.Fixed32Kind: {32, false},
	protoreflect.Uint64Kind: {64, false}, protoreflect.Fixed64Kind: {64, false},
}

// extractFit is why a value of kind src cannot go into a number field of kind
// dst, "" when it can: both are integers, of one sign and no wider than the
// field, or unsigned and narrower than a signed field. Every value of the
// source must fit, as the run cannot know which one it will meet.
func extractFit(src, dst protoreflect.Kind) string {
	from, ok := integerKinds[src]
	if !ok {
		return fmt.Sprintf("%s, not an integer: a number field takes only a whole {{extract.<name>}} of an integer", src)
	}

	to := integerKinds[dst]

	switch {
	case from.signed == to.signed && to.bits >= from.bits:
		return ""
	case !from.signed && to.signed && to.bits > from.bits:
		return ""
	}

	return fmt.Sprintf("%s, which does not fit the %s field", src, dst)
}
