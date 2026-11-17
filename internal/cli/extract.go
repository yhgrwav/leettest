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
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
)

// ErrExtractNotYet is a config whose extract passed every check and is
// refused: the run side of extract is not there yet (v0.9 unit 4a; removed in 4b).
var ErrExtractNotYet = errors.New("extract: not supported yet")

// extractSources checks each path of extract against the reply of method and
// records the field each name reads in into. at is the path of the call or of
// the step that extracts (load.calls[0], load.calls[0].steps[1]). A path that
// is wrong is an error and is not recorded.
func extractSources(
	method *descriptor.Method, at string, extract map[string]string, into map[string]protoreflect.FieldDescriptor,
) []error {
	names := make([]string, 0, len(extract))
	for name := range extract {
		names = append(names, name)
	}

	sort.Strings(names)

	var errs []error

	for _, name := range names {
		where := at + ".extract." + name

		fd, err := extractLeaf(method.Output, extract[name])
		if err != nil {
			errs = append(errs, fmt.Errorf("%w: %s: %q: %w", config.ErrScenario, where, extract[name], err))

			continue
		}

		into[name] = fd
	}

	return errs
}

// extractLeaf is the field a path of names joined by dots leads to from the
// message msg: a single value of a kind that can be written as text. A hop may
// go through a message and through nothing else; a name is looked up by its
// JSON name and then by its proto name, as the data of a request is.
func extractLeaf(msg protoreflect.MessageDescriptor, path string) (protoreflect.FieldDescriptor, error) {
	names := strings.Split(path, ".")

	for i, name := range names {
		if name == "" {
			return nil, errors.New("a name between dots is empty")
		}

		fd := msg.Fields().ByJSONName(name)
		if fd == nil {
			fd = msg.Fields().ByName(protoreflect.Name(name))
		}
		if fd == nil {
			return nil, fmt.Errorf("no field %q in %s", name, msg.FullName())
		}

		if i < len(names)-1 {
			switch {
			case fd.IsMap():
				return nil, fmt.Errorf("%q is a map: a path cannot go through it", name)
			case fd.IsList():
				return nil, fmt.Errorf("%q is repeated: a path cannot go through it", name)
			case fd.Message() == nil:
				return nil, fmt.Errorf("%q is %s, not a message: a path cannot go below it", name, fd.Kind())
			}

			msg = fd.Message()

			continue
		}

		switch {
		case fd.IsMap():
			return nil, fmt.Errorf("%q is a map: name a single value", name)
		case fd.IsList():
			return nil, fmt.Errorf("%q is repeated: name a single value", name)
		case fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind:
			return nil, fmt.Errorf("%q is a message: name one of its fields, as %s.<field>", name, name)
		case fd.Kind() == protoreflect.BytesKind:
			return nil, fmt.Errorf("%q is bytes: a value that is not text cannot be extracted", name)
		}

		return fd, nil
	}

	// Split gives at least one name.
	return nil, errors.New("the path is empty")
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
