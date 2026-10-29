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
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// ErrTemplateField is a placeholder in a field it does not fit: of another
// type, or in a number that is not the whole value.
var ErrTemplateField = errors.New("placeholder does not fit the field")

const (
	maxInt32Seq  = math.MaxInt32
	maxUint32Seq = math.MaxUint32
)

// CheckTemplatesFit refuses a run whose plan hands out more requests than the
// field a seq goes into holds, before anything is sent. The count is
// arithmetic over the stages (engine.Stage.Ticks), so a plan of hours costs
// nothing; it counts the requests the call has already handed out in this
// process too. The guard in the engine is the second layer: it stops the run
// before a seq that does not fit is built.
func CheckTemplatesFit(calls []engine.Call) error {
	var errs []error

	for _, call := range calls {
		if call.Template == nil || call.Template.Max == 0 {
			continue
		}

		total := uint64(call.Template.Counter.Handed())
		for _, stage := range call.Stages {
			ticks := stage.Ticks()
			if total > math.MaxUint64-ticks {
				total = math.MaxUint64

				break
			}
			total += ticks
		}

		if total > call.Template.Max {
			errs = append(errs, fmt.Errorf("%w: %s: the plan hands out %d requests and {{seq}} goes into a field that holds %d; "+
				"shorten the plan, or put {{seq}} into a 64-bit number or a string",
				engine.ErrSeqOverflow, call.Method, total, call.Template.Max))
		}
	}

	return errors.Join(errs...)
}

// templatesLine is the line under a templated method in the text report. It
// says what the last seq counts: the requests handed to workers, which the
// report's own counts leave out in the ramp and the warm-up.
func templatesLine(t *engine.TemplatesReport) string {
	return fmt.Sprintf("  data: templated, %s (last seq %d, ramp and warm-up counted)", strings.Join(t.Placeholders, " "), t.LastSeq)
}

func seqOverflowJSON(o *engine.SeqOverflow) *jsonSeqOverflow {
	if o == nil {
		return nil
	}

	return &jsonSeqOverflow{Method: o.Method, Limit: o.Limit}
}

// seqOverflowNote tells why a run ended early when the guard did it.
func seqOverflowNote(report engine.Report) []string {
	o := report.SeqOverflow
	if o == nil {
		return nil
	}

	return []string{noteLine("stopped: the next request of %s would carry a {{seq}} above %d, the largest its field\n"+
		"holds, so it was not built (a wrapped number would send a body that is not the one in\n"+
		"the config). Put {{seq}} into a 64-bit number or a string, or run a shorter plan.",
		displayMethod(o.Method), o.Limit)}
}

// buildTemplate makes the engine template of a call whose data has
// placeholders. The type of each field the placeholders stand in comes from the
// method's descriptor, never from the look of the YAML.
//
// The body is made from a message built once from the data, with a few places
// (slots) overwritten per request; the bytes are those of the plain path,
// substitute the text and build the message anew (the reference test).
func buildTemplate(method *descriptor.Method, call *config.Call) (*engine.Template, error) {
	c := templateCompiler{method: call.Method}
	data := c.message(method.Input, call.Data, "data", nil)

	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}

	msg, err := requestMessage(method.UnmarshalOptions(), method.Input, data)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrRequestData, call.Method, err)
	}

	builder := &templateBuilder{msg: msg.ProtoReflect()}
	for _, s := range c.slots {
		bound, err := s.bind(msg.ProtoReflect())
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrRequestData, call.Method, err)
		}
		builder.slots = append(builder.slots, bound)
	}

	// A trial: a body that cannot be written fails here, not as an empty request
	// in the middle of a run.
	if _, err := builder.write(1); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrRequestData, call.Method, err)
	}

	return engine.NewTemplate(call.TemplatePlaceholders(), c.limit, builder.build), nil
}

type hopMode int

const (
	hopField hopMode = iota // a singular field
	hopIndex                // an element of a repeated field
	hopKey                  // a value of a map field
)

// step is how a value is reached from the message that holds it.
type step struct {
	mode hopMode
	fd   protoreflect.FieldDescriptor
	idx  int
	key  protoreflect.MapKey
}

// slot is a place in the message a placeholder stands in: a string made of
// parts, or, with parts nil, a number of kind that takes the whole seq.
type slot struct {
	path  []step
	leaf  step
	parts []config.TemplatePart
	kind  protoreflect.Kind
}

type templateCompiler struct {
	method string
	slots  []slot
	errs   []error
	// limit is the largest seq the narrowest number field holds; 0 is none.
	limit uint64
}

func (c *templateCompiler) fail(path, format string, args ...any) {
	c.errs = append(c.errs, fmt.Errorf("%w: %s: %s: %s", ErrTemplateField, c.method, path, fmt.Sprintf(format, args...)))
}

// message walks the data of message desc and returns it with a whole {{seq}}
// of a number field replaced by 0 (the slot fills it per request), so that
// the message can be built once. The data itself is not changed.
func (c *templateCompiler) message(desc protoreflect.MessageDescriptor, v any, path string, steps []step) any {
	fields, ok := v.(map[string]any)
	if !ok {
		return c.opaque(path, v)
	}

	out := make(map[string]any, len(fields))
	for key, e := range fields {
		fd := desc.Fields().ByJSONName(key)
		if fd == nil {
			fd = desc.Fields().ByName(protoreflect.Name(key))
		}
		if fd == nil {
			// The message builder names an unknown field; here it takes no placeholder.
			out[key] = c.opaque(path+"."+key, e)

			continue
		}

		out[key] = c.field(fd, e, path+"."+key, steps)
	}

	return out
}

func (c *templateCompiler) field(fd protoreflect.FieldDescriptor, v any, path string, steps []step) any {
	switch {
	case fd.IsMap():
		entries, ok := v.(map[string]any)
		if !ok {
			return c.opaque(path, v)
		}

		out := make(map[string]any, len(entries))
		for key, e := range entries {
			mapKey, ok := parseMapKey(fd.MapKey().Kind(), key)
			if !ok {
				out[key] = c.opaque(path+"."+key, e)

				continue
			}
			out[key] = c.value(fd.MapValue(), e, path+"."+key, steps, step{mode: hopKey, fd: fd, key: mapKey})
		}

		return out
	case fd.IsList():
		items, ok := v.([]any)
		if !ok {
			return c.opaque(path, v)
		}

		out := make([]any, len(items))
		for i, e := range items {
			out[i] = c.value(fd, e, fmt.Sprintf("%s[%d]", path, i), steps, step{mode: hopIndex, fd: fd, idx: i})
		}

		return out
	default:
		return c.value(fd, v, path, steps, step{mode: hopField, fd: fd})
	}
}

// value is one value of kind kfd, reached by at from the message at steps.
func (c *templateCompiler) value(kfd protoreflect.FieldDescriptor, v any, path string, steps []step, at step) any {
	switch kfd.Kind() {
	case protoreflect.StringKind:
		if text, ok := v.(string); ok {
			if parts, _ := config.ParseTemplate(text); hasPlaceholder(parts) {
				c.slots = append(c.slots, slot{path: steps, leaf: at, parts: parts})
			}
		}

		return v
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return c.number(kfd, v, path, steps, at, maxInt32Seq)
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return c.number(kfd, v, path, steps, at, maxUint32Seq)
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return c.number(kfd, v, path, steps, at, 0)
	case protoreflect.MessageKind, protoreflect.GroupKind:
		inner := kfd.Message()
		if strings.HasPrefix(string(inner.FullName()), "google.protobuf.") {
			// A well-known type is written as a string or a number: no place for a part of it.
			return c.opaque(path, v)
		}

		below := make([]step, len(steps)+1)
		copy(below, steps)
		below[len(steps)] = at

		return c.message(inner, v, path, below)
	default:
		return c.opaque(path, v)
	}
}

// number is a value of a number field: only a whole {{seq}} fits, and then it
// is the field's seq of the largest limit (0: 64-bit, none).
func (c *templateCompiler) number(kfd protoreflect.FieldDescriptor, v any, path string, steps []step, at step, limit uint64) any {
	text, ok := v.(string)
	if !ok {
		return v
	}

	parts, _ := config.ParseTemplate(text)
	if !hasPlaceholder(parts) {
		return v
	}

	if len(parts) != 1 || parts[0].Placeholder != config.PlaceholderSeq {
		c.fail(path, "a %s field takes only a whole {{seq}}, not %q", kfd.Kind(), text)

		return v
	}

	c.slots = append(c.slots, slot{path: steps, leaf: at, kind: kfd.Kind()})
	if limit != 0 && (c.limit == 0 || limit < c.limit) {
		c.limit = limit
	}

	return 0
}

// opaque is a value in a place that takes no placeholder: an error if it holds one.
func (c *templateCompiler) opaque(path string, v any) any {
	if containsPlaceholder(v) {
		c.fail(path, "a field of this type takes no placeholder")
	}

	return v
}

func hasPlaceholder(parts []config.TemplatePart) bool {
	for _, p := range parts {
		if p.Placeholder != "" {
			return true
		}
	}

	return false
}

func containsPlaceholder(v any) bool {
	switch x := v.(type) {
	case string:
		parts, _ := config.ParseTemplate(x)

		return hasPlaceholder(parts)
	case []any:
		for _, e := range x {
			if containsPlaceholder(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if containsPlaceholder(e) {
				return true
			}
		}
	}

	return false
}

// parseMapKey is the key of a map field as protojson reads it from a JSON name.
func parseMapKey(kind protoreflect.Kind, key string) (protoreflect.MapKey, bool) {
	switch kind {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(key).MapKey(), true
	case protoreflect.BoolKind:
		b, err := strconv.ParseBool(key)

		return protoreflect.ValueOfBool(b).MapKey(), err == nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		n, err := strconv.ParseInt(key, 10, 32)

		return protoreflect.ValueOfInt32(int32(n)).MapKey(), err == nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		n, err := strconv.ParseInt(key, 10, 64)

		return protoreflect.ValueOfInt64(n).MapKey(), err == nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		n, err := strconv.ParseUint(key, 10, 32)

		return protoreflect.ValueOfUint32(uint32(n)).MapKey(), err == nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		n, err := strconv.ParseUint(key, 10, 64)

		return protoreflect.ValueOfUint64(n).MapKey(), err == nil
	default:
		return protoreflect.MapKey{}, false
	}
}

// boundSlot is a slot tied to the one message every request is made in: set
// writes its value into the place.
type boundSlot struct {
	parts []config.TemplatePart
	kind  protoreflect.Kind
	set   func(protoreflect.Value)
}

// bind finds the place of the slot in msg once, so that a request does no walk.
func (s slot) bind(msg protoreflect.Message) (boundSlot, error) {
	m := msg

	for _, st := range s.path {
		inner, err := st.message(m)
		if err != nil {
			return boundSlot{}, err
		}
		m = inner
	}

	bound := boundSlot{parts: s.parts, kind: s.kind}
	leaf := s.leaf

	switch leaf.mode {
	case hopField:
		bound.set = func(v protoreflect.Value) { m.Set(leaf.fd, v) }
	case hopIndex:
		list := m.Mutable(leaf.fd).List()
		if leaf.idx >= list.Len() {
			return boundSlot{}, fmt.Errorf("%s: element %d is not there", leaf.fd.FullName(), leaf.idx)
		}
		bound.set = func(v protoreflect.Value) { list.Set(leaf.idx, v) }
	default:
		entries := m.Mutable(leaf.fd).Map()
		if !entries.Has(leaf.key) {
			return boundSlot{}, fmt.Errorf("%s: key %v is not there", leaf.fd.FullName(), leaf.key)
		}
		bound.set = func(v protoreflect.Value) { entries.Set(leaf.key, v) }
	}

	return bound, nil
}

// message is the message st leads to from m.
func (st step) message(m protoreflect.Message) (protoreflect.Message, error) {
	switch st.mode {
	case hopField:
		return m.Mutable(st.fd).Message(), nil
	case hopIndex:
		list := m.Mutable(st.fd).List()
		if st.idx >= list.Len() {
			return nil, fmt.Errorf("%s: element %d is not there", st.fd.FullName(), st.idx)
		}

		return list.Get(st.idx).Message(), nil
	default:
		entries := m.Mutable(st.fd).Map()
		if !entries.Has(st.key) {
			return nil, fmt.Errorf("%s: key %v is not there", st.fd.FullName(), st.key)
		}

		return entries.Get(st.key).Message(), nil
	}
}

// templateBuilder makes the body of request number seq in one message that is
// rewritten each time; the mutex is for a Template shared by runs.
type templateBuilder struct {
	mu    sync.Mutex
	msg   protoreflect.Message
	slots []boundSlot
	buf   []byte
}

// build is engine.Template.Build. A body that cannot be written was caught by
// the trial of buildTemplate, so an error here is not expected.
func (b *templateBuilder) build(seq uint64) []byte {
	body, err := b.write(seq)
	if err != nil {
		return nil
	}

	return body
}

func (b *templateBuilder) write(seq uint64) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := range b.slots {
		s := &b.slots[i]
		if s.parts != nil {
			s.set(protoreflect.ValueOfString(b.text(s.parts, seq)))

			continue
		}
		s.set(numberOf(s.kind, seq))
	}

	return proto.MarshalOptions{Deterministic: true}.Marshal(b.msg.Interface())
}

// text is the parts written out: {{seq}} as the number, each {{uuid}} as a value of its own.
func (b *templateBuilder) text(parts []config.TemplatePart, seq uint64) string {
	buf := b.buf[:0]

	for _, p := range parts {
		switch p.Placeholder {
		case config.PlaceholderSeq:
			buf = strconv.AppendUint(buf, seq, 10)
		case config.PlaceholderUUID:
			buf = appendUUID(buf)
		default:
			buf = append(buf, p.Text...)
		}
	}

	b.buf = buf

	return string(buf)
}

// numberOf is seq as a value of a number field of kind. A 32-bit field never
// gets a seq above its width: the check and the engine's guard see to it.
func numberOf(kind protoreflect.Kind, seq uint64) protoreflect.Value {
	switch kind {
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return protoreflect.ValueOfInt32(int32(seq)) //nolint:gosec // seq <= MaxInt32 by the Template's Max
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return protoreflect.ValueOfUint32(uint32(seq)) //nolint:gosec // seq <= MaxUint32 by the Template's Max
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return protoreflect.ValueOfUint64(seq)
	default:
		return protoreflect.ValueOfInt64(int64(seq)) //nolint:gosec // 2^63 requests are not sent
	}
}

// appendUUID writes a random version 4 uuid, lower case, 36 characters. The
// source is not secret: a uuid here is a unique value in a request, and 122
// random bits of the runtime's generator do for that.
func appendUUID(dst []byte) []byte {
	var raw [16]byte

	binary.BigEndian.PutUint64(raw[:8], rand.Uint64()) //nolint:gosec // not a secret
	binary.BigEndian.PutUint64(raw[8:], rand.Uint64()) //nolint:gosec // not a secret
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80

	var text [36]byte

	hex.Encode(text[0:8], raw[0:4])
	text[8] = '-'
	hex.Encode(text[9:13], raw[4:6])
	text[13] = '-'
	hex.Encode(text[14:18], raw[6:8])
	text[18] = '-'
	hex.Encode(text[19:23], raw[8:10])
	text[23] = '-'
	hex.Encode(text[24:36], raw[10:16])

	return append(dst, text[:]...)
}
