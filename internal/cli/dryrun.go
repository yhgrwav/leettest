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
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// dryRunBodyLimit is how many bytes of a request, a reply or a message the
// screen keeps: the question is whether it works, and a reply of a megabyte
// would carry the other blocks off the screen.
const dryRunBodyLimit = 4096

// DryRunCall is what one call of a dry run left behind.
type DryRunCall struct {
	// Method is "package.Service/Method", as the config wrote it.
	Method string
	// Info decodes the request and the reply; nil for a method reflection
	// did not know, which went out as an empty message.
	Info *descriptor.Method
	// Request is the encoded body that went out.
	Request []byte
	// Record and Records say which record of its file the request is: zero
	// Records for a call without a dataset.
	Record, Records int
	Outcome         engine.Outcome
	// Started is the moment before Send, Returned the one right after it:
	// the latency runs from Started to Outcome.DoneAt, or to Returned when the
	// sender left DoneAt zero.
	Started, Returned time.Time
}

// DryRunPrinter writes a dry run's screen call by call, so that a stop in the
// middle leaves the blocks already printed.
type DryRunPrinter struct {
	W io.Writer
	// Total is the number of calls the run plans.
	Total int
	// Redact is applied to the target's message and to the reply's JSON before
	// the cut; nil leaves them as they came.
	Redact func(string) string

	printed int
}

// Header prints the first line.
func (p *DryRunPrinter) Header() {
	if p.Total == 1 {
		fmt.Fprintln(p.W, "dry run: 1 call, one request, nothing measured")

		return
	}

	fmt.Fprintf(p.W, "dry run: %d calls, one request each, nothing measured\n", p.Total)
}

// Call prints the next call's block.
func (p *DryRunPrinter) Call(call DryRunCall) {
	p.printed++
	out := call.Outcome

	var b strings.Builder

	fmt.Fprintf(&b, "%d/%d %s: %s, %s", p.printed, p.Total, call.Method, out.Category.Name(), out.Code)

	if out.Category != engine.CategorySuccess {
		// The report's words (notes.go): a code the client set is not the target's answer.
		if out.CodeFromTarget {
			b.WriteString(" sent by the target")
		} else {
			b.WriteString(" set by the client")
		}
	}

	// As in a run, a call that got no status has no latency.
	if out.Category != engine.CategoryUnreachable && out.Category != engine.CategoryCutOff {
		done := out.DoneAt
		if done.IsZero() {
			done = call.Returned
		}

		b.WriteString(", " + formatLatency(done.Sub(call.Started)))
	}

	b.WriteString("\n  request")

	if call.Records > 0 {
		fmt.Fprintf(&b, " (record %d of %d)", call.Record, call.Records)
	}

	b.WriteString(": ")

	if call.Info == nil {
		b.WriteString("{}")
	} else if text, err := dryRunJSON(call.Info, call.Info.Input, call.Request); err != nil {
		fmt.Fprintf(&b, "%d bytes, not decoded: %v", len(call.Request), err)
	} else {
		b.WriteString(text)
	}

	b.WriteByte('\n')

	switch {
	case out.Category != engine.CategorySuccess:
		if msg := status.Convert(out.Err).Message(); msg != "" {
			b.WriteString("  message: " + p.shown(msg) + "\n")
		}
	case call.Info == nil:
		fmt.Fprintf(&b, "  response: %d bytes, not decoded: the method was not checked\n", len(out.Response))
	default:
		text, err := dryRunJSON(call.Info, call.Info.Output, out.Response)
		if err != nil {
			text = fmt.Sprintf("%d bytes, not decoded: %v", len(out.Response), err)
		}

		b.WriteString("  response: " + p.shown(text) + "\n")
	}

	_, _ = io.WriteString(p.W, b.String())
}

// Metadata prints the metadata line, from the values as the config wrote them.
func (p *DryRunPrinter) Metadata(raw map[string]string) {
	if len(raw) == 0 {
		return
	}

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for i, k := range keys {
		keys[i] = k + ": " + raw[k]
	}

	fmt.Fprintf(p.W, "metadata: %s\n", escapeControls(strings.Join(keys, ", ")))
}

// Printed is the number of blocks printed so far.
func (p *DryRunPrinter) Printed() int {
	return p.printed
}

// shown is text as the screen gets it: the secrets replaced first, then the
// control characters, then the cut, so that no step can leave a piece of what
// the one before took away.
func (p *DryRunPrinter) shown(text string) string {
	if p.Redact != nil {
		text = p.Redact(text)
	}

	text = escapeControls(text)
	if len(text) <= dryRunBodyLimit {
		return text
	}

	cut := dryRunBodyLimit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return fmt.Sprintf("%s… (%d bytes in all)", text[:cut], len(text))
}

// escapeControls writes the control characters of text as \xNN: the target
// chooses a message, and a terminal acts on an escape sequence in it.
func escapeControls(text string) string {
	if !strings.ContainsFunc(text, unicode.IsControl) {
		return text
	}

	var b strings.Builder

	for _, r := range text {
		if unicode.IsControl(r) {
			fmt.Fprintf(&b, `\x%02x`, r)

			continue
		}

		b.WriteRune(r)
	}

	return b.String()
}

// dryRunJSON decodes body as a message of desc and prints it as compact JSON in
// the names of the .proto, the way the config is written. An empty body is the
// message with every field at its default.
func dryRunJSON(m *descriptor.Method, desc protoreflect.MessageDescriptor, body []byte) (string, error) {
	msg := dynamicpb.NewMessage(desc)
	if err := proto.Unmarshal(body, msg); err != nil {
		return "", err
	}

	opts := protojson.MarshalOptions{UseProtoNames: true}
	if m.Types != nil {
		// An Any names a type of the files the method came from.
		opts.Resolver = m.Types
	}

	raw, err := opts.Marshal(msg)
	if err != nil {
		return "", err
	}

	// protojson's spacing is not stable on purpose.
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", err
	}

	return compact.String(), nil
}

// DryRunRedactor returns the function that replaces, in a string, every
// occurrence of the value of each ${NAME} the metadata substituted with
// ${NAME}, in the raw form of the value and in the form it has inside a JSON
// string. raw is the metadata as the config wrote it; lookup is the
// environment.
//
// The text is read once. Where occurrences overlap — one value inside another,
// or two that share a few characters — the stretch they cover is one piece and
// takes the placeholder of the longest value in it, so that neither the order
// of the variables nor a short value can leave part of a secret. Nothing that
// was put in is searched again.
func DryRunRedactor(raw map[string]string, lookup func(string) (string, bool)) func(string) string {
	seen := map[string]bool{}

	var names []string

	for _, value := range raw {
		for _, name := range config.SubstitutedEnvNames(value) {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}

	// A fixed order, so that two variables with one value always give the same name.
	slices.Sort(names)

	var forms []secretForm

	for _, name := range names {
		value, ok := lookup(name)
		if !ok || value == "" {
			continue
		}

		placeholder := "${" + name + "}"
		forms = append(forms, secretForm{value, placeholder})

		if escaped := jsonEscaped(value); escaped != value {
			forms = append(forms, secretForm{escaped, placeholder})
		}
	}

	if len(forms) == 0 {
		return func(s string) string { return s }
	}

	return func(s string) string { return redactForms(s, forms) }
}

// secretForm is a text to hide and what to show in its place.
type secretForm struct{ text, placeholder string }

// jsonEscaped is how a metadata value looks inside a JSON string. Values are
// printable ASCII, so the quote and the backslash are the whole difference.
func jsonEscaped(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

// span is the half-open stretch [from, to) of a string that a form covers.
type span struct {
	from, to int
	form     *secretForm
}

// redactForms replaces the pieces of s that forms cover: see DryRunRedactor.
func redactForms(s string, forms []secretForm) string {
	var spans []span

	for i := range forms {
		for at := 0; ; at++ {
			j := strings.Index(s[at:], forms[i].text)
			if j < 0 {
				break
			}

			at += j
			spans = append(spans, span{at, at + len(forms[i].text), &forms[i]})
		}
	}

	if len(spans) == 0 {
		return s
	}

	slices.SortStableFunc(spans, func(a, b span) int { return a.from - b.from })

	var out strings.Builder

	pos := 0

	for i := 0; i < len(spans); {
		piece := spans[i]
		longest := piece

		j := i + 1
		for ; j < len(spans) && spans[j].from < piece.to; j++ {
			piece.to = max(piece.to, spans[j].to)

			if len(spans[j].form.text) > len(longest.form.text) {
				longest = spans[j]
			}
		}

		out.WriteString(s[pos:piece.from])
		out.WriteString(longest.form.placeholder)

		pos, i = piece.to, j
	}

	out.WriteString(s[pos:])

	return out.String()
}
