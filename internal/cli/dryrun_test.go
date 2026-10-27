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
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

var dryStart = time.Unix(1_700_000_000, 0)

// healthMethod is Health/Check as reflection would hand it out.
func healthMethod() *descriptor.Method {
	return &descriptor.Method{
		Path:   "grpc.health.v1.Health/Check",
		Input:  (&grpc_health_v1.HealthCheckRequest{}).ProtoReflect().Descriptor(),
		Output: (&grpc_health_v1.HealthCheckResponse{}).ProtoReflect().Descriptor(),
	}
}

// mixMethod is a method whose request and reply are both payload.v1.Mix: the
// one message of the test files that has strings and an Any.
func mixMethod(t *testing.T) *descriptor.Method {
	t.Helper()

	m, err := payloadResolver(t).Resolve(t.Context(), sendMethod)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	return &descriptor.Method{Path: sendMethod, Input: m.Input, Output: m.Input, Types: m.Types}
}

func wire(t *testing.T, msg proto.Message) []byte {
	t.Helper()

	b, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return b
}

// mixText is a Mix with only its text set, encoded.
func mixText(t *testing.T, m *descriptor.Method, text string) []byte {
	t.Helper()

	msg := dynamicpb.NewMessage(m.Input)
	msg.Set(m.Input.Fields().ByName("text"), protoreflect.ValueOfString(text))

	return wire(t, msg)
}

// done is a call that started at dryStart and finished after latency.
func done(method string, info *descriptor.Method, request []byte, out engine.Outcome, latency time.Duration) cli.DryRunCall {
	out.DoneAt = dryStart.Add(latency)

	return cli.DryRunCall{
		Method: method, Info: info, Request: request, Outcome: out,
		Started: dryStart, Returned: dryStart.Add(latency + time.Microsecond),
	}
}

// screen is everything a dry run of total calls prints, the calls given and
// the metadata line included.
func screen(total int, redact func(string) string, metadata map[string]string, calls ...cli.DryRunCall) string {
	var out bytes.Buffer

	p := &cli.DryRunPrinter{W: &out, Total: total, Redact: redact}
	p.Header()
	for i := range calls {
		p.Call(calls[i])
	}
	p.Metadata(metadata)

	return out.String()
}

// withoutHeader is the screen less its first line, so a test of one call does
// not pin how the header counts one.
func withoutHeader(t *testing.T, screen string) string {
	t.Helper()

	first, rest, ok := strings.Cut(screen, "\n")
	if !ok || !strings.HasPrefix(first, "dry run: ") {
		t.Fatalf("the screen does not start with the header line:\n%s", screen)
	}

	return rest
}

func targetSays(c codes.Code, msg string) engine.Outcome {
	return engine.Outcome{
		Category: engine.CategoryClientFault, Code: c.String(), CodeFromTarget: true,
		Err: status.Error(c, msg),
	}
}

// The screen a user reads, whole and line by line: the header, a block per call
// numbered of the total, the request as it went on the wire, the reply, the
// metadata as written.
// Ground: contract — the output of -dry-run is what scripts and people read.
func TestDryRun_PrintsEachCallsRequestAndReply(t *testing.T) {
	health := healthMethod()
	ok := engine.Outcome{
		Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true,
		Response: wire(t, &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}),
	}
	calls := []cli.DryRunCall{
		done("grpc.health.v1.Health/Check", health,
			wire(t, &grpc_health_v1.HealthCheckRequest{Service: "wallet"}), ok, 1200*time.Microsecond),
		done("wallet.Wallet/Transfer", health,
			wire(t, &grpc_health_v1.HealthCheckRequest{Service: "a-1"}),
			targetSays(codes.InvalidArgument, "amount must be positive"), 800*time.Microsecond),
	}
	calls[1].Record, calls[1].Records = 1, 500

	got := screen(2, nil, map[string]string{"x-tenant": "acme", "authorization": "Bearer ${TOKEN}"}, calls...)

	want := `dry run: 2 calls, one request each, nothing measured
1/2 grpc.health.v1.Health/Check: success, OK, 1.20ms
  request: {"service":"wallet"}
  response: {"status":"SERVING"}
2/2 wallet.Wallet/Transfer: request_error, InvalidArgument sent by the target, 800us
  request (record 1 of 500): {"service":"a-1"}
  message: amount must be positive
metadata: authorization: Bearer ${TOKEN}, x-tenant: acme
`
	if got != want {
		t.Errorf("screen:\n%s\nwant:\n%s", got, want)
	}
}

// Who set the code is the report's pair of words, with nothing after; a call
// that never got a status prints no latency, as in a run.
// Ground: contract — "sent by the target" / "set by the client" (notes.go), #116 category names, and the codes as the text report spells them (InvalidArgument).
func TestDryRun_NamesWhoSetTheCode(t *testing.T) {
	health := healthMethod()
	for _, tc := range []struct {
		name string
		out  engine.Outcome
		lat  time.Duration
		want string
	}{
		{
			"the target's status",
			targetSays(codes.NotFound, "no such wallet"), 5 * time.Millisecond,
			"1/1 m/M: request_error, NotFound sent by the target, 5.00ms\n  request: {}\n  message: no such wallet\n",
		},
		{
			"a status with no message has no message line",
			targetSays(codes.NotFound, ""), 5 * time.Millisecond,
			"1/1 m/M: request_error, NotFound sent by the target, 5.00ms\n  request: {}\n",
		},
		{
			"our own deadline",
			engine.Outcome{
				Category: engine.CategoryTimeout, Code: "DeadlineExceeded",
				Err: status.Error(codes.DeadlineExceeded, "context deadline exceeded"),
			},
			250 * time.Millisecond,
			"1/1 m/M: timed_out, DeadlineExceeded set by the client, 250ms\n  request: {}\n" +
				"  message: context deadline exceeded\n",
		},
		{
			"a reply the client refused",
			engine.Outcome{
				Category: engine.CategoryBadResponse, Code: "ResourceExhausted",
				Err: status.Error(codes.ResourceExhausted, "reply is over the limit"),
			},
			3 * time.Millisecond,
			"1/1 m/M: bad_response, ResourceExhausted set by the client, 3.00ms\n  request: {}\n" +
				"  message: reply is over the limit\n",
		},
		{
			"unreachable has no latency",
			engine.Outcome{
				Category: engine.CategoryUnreachable, Code: "Unavailable",
				Err: status.Error(codes.Unavailable, "connection refused"),
			},
			40 * time.Microsecond,
			"1/1 m/M: unreachable, Unavailable set by the client\n  request: {}\n  message: connection refused\n",
		},
		{
			"cut off has no latency",
			engine.Outcome{
				Category: engine.CategoryCutOff, Code: "Unavailable",
				Err: status.Error(codes.Unavailable, "the stream was reset"),
			},
			7 * time.Millisecond,
			"1/1 m/M: cut_off, Unavailable set by the client\n  request: {}\n  message: the stream was reset\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := withoutHeader(t, screen(1, nil, nil, done("m/M", health, nil, tc.out, tc.lat)))
			if got != tc.want {
				t.Errorf("block:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// The latency runs from the moment before Send; Outcome.DoneAt ends it, and a
// sender that leaves DoneAt zero ends it at the moment Send returned (the pool
// does the same), instead of a number of 2562047 hours.
// Ground: contract — latency is from send to done, as the pool measures it (worker.go).
func TestDryRun_LatencyEndsAtDoneAtOrWhenSendReturned(t *testing.T) {
	ok := engine.Outcome{
		Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true,
		Response: wire(t, &grpc_health_v1.HealthCheckResponse{}),
	}

	t.Run("DoneAt is set", func(t *testing.T) {
		call := done("m/M", healthMethod(), nil, ok, time.Millisecond)
		call.Returned = dryStart.Add(9 * time.Millisecond)

		got := withoutHeader(t, screen(1, nil, nil, call))
		if want := "1/1 m/M: success, OK, 1.00ms\n"; !strings.HasPrefix(got, want) {
			t.Errorf("block:\n%s\nwant it to start with %q", got, want)
		}
	})

	t.Run("DoneAt is zero", func(t *testing.T) {
		call := done("m/M", healthMethod(), nil, ok, 0)
		call.Outcome.DoneAt = time.Time{}
		call.Returned = dryStart.Add(2500 * time.Microsecond)

		got := withoutHeader(t, screen(1, nil, nil, call))
		if want := "1/1 m/M: success, OK, 2.50ms\n"; !strings.HasPrefix(got, want) {
			t.Errorf("block:\n%s\nwant it to start with %q", got, want)
		}
	})
}

// The header says "1 call, one request" for a single call, not "1 calls, one
// request each".
// Ground: contract — the first line of the screen.
func TestDryRun_TheHeaderOfOneCallIsInTheSingular(t *testing.T) {
	var out bytes.Buffer

	(&cli.DryRunPrinter{W: &out, Total: 1}).Header()

	if got, want := out.String(), "dry run: 1 call, one request, nothing measured\n"; got != want {
		t.Errorf("header %q, want %q", got, want)
	}
}

// A reply with nothing in it is a reply: braces, not a missing line.
// Ground: boundary — a body of 0 bytes decodes to the message with every field at its default.
func TestDryRun_ASuccessfulEmptyReplyPrintsBraces(t *testing.T) {
	ok := engine.Outcome{Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true}

	got := withoutHeader(t, screen(1, nil, nil, done("m/M", healthMethod(), nil, ok, time.Millisecond)))

	want := "1/1 m/M: success, OK, 1.00ms\n  request: {}\n  response: {}\n"
	if got != want {
		t.Errorf("block:\n%s\nwant:\n%s", got, want)
	}
}

// The method reflection did not know went out as an empty message and its
// reply cannot be read.
// Ground: contract — the unchecked method of AttachData (data.go) is shown as such.
func TestDryRun_AnUncheckedMethodSaysItWasNotChecked(t *testing.T) {
	ok := engine.Outcome{
		Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true,
		Response: []byte{1, 2, 3, 4},
	}

	got := withoutHeader(t, screen(1, nil, nil, done("m/M", nil, nil, ok, time.Millisecond)))

	want := "1/1 m/M: success, OK, 1.00ms\n  request: {}\n" +
		"  response: 4 bytes, not decoded: the method was not checked\n"
	if got != want {
		t.Errorf("block:\n%s\nwant:\n%s", got, want)
	}
}

// A reply the descriptor cannot read says so and keeps its category: the run
// would have counted it a success. dynamicpb refuses invalid UTF-8 in a proto3
// string, and protojson an Any of a type nobody knows; confirmed by a run of
// both before this test was written.
// Ground: signal google.golang.org/protobuf v1.36.12 — the text of its errors (UTF-8, the type name); a target not written in Go may send invalid UTF-8 or an Any the files lack.
func TestDryRun_AReplyThatDoesNotDecodeKeepsItsCategory(t *testing.T) {
	mix := mixMethod(t)

	url := "type.googleapis.com/nope.Missing"
	anyField := append([]byte{0x0a, byte(len(url))}, url...)
	unknownAny := append([]byte{0x3a, byte(len(anyField))}, anyField...)

	for _, tc := range []struct {
		name     string
		response []byte
		contains string
	}{
		{"invalid UTF-8 in a string", []byte{0x0a, 0x01, 0xff}, "UTF-8"},
		{"an Any of an unknown type", unknownAny, "nope.Missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok := engine.Outcome{
				Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true,
				Response: tc.response,
			}

			got := withoutHeader(t, screen(1, nil, nil, done("m/M", mix, nil, ok, time.Millisecond)))

			lines := strings.Split(got, "\n")
			if len(lines) < 3 || lines[0] != "1/1 m/M: success, OK, 1.00ms" {
				t.Fatalf("the call is not printed as a success:\n%s", got)
			}

			wantPrefix := "  response: " + strconv.Itoa(len(tc.response)) + " bytes, not decoded: "
			if !strings.HasPrefix(lines[2], wantPrefix) || !strings.Contains(lines[2], tc.contains) {
				t.Errorf("response line %q: want it to start with %q and name %q", lines[2], wantPrefix, tc.contains)
			}
		})
	}
}

// The config is written in the names of the .proto, so the line a user compares
// with it is too: user_name, not the json_name uname.
// Ground: contract — protojson with UseProtoNames, as the dataset's errors speak (data.go).
func TestDryRun_PrintsFieldsByTheirProtoNames(t *testing.T) {
	mix := mixMethod(t)
	msg := dynamicpb.NewMessage(mix.Input)
	msg.Set(mix.Input.Fields().ByName("user_name"), protoreflect.ValueOfString("ann"))

	ok := engine.Outcome{
		Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true, Response: wire(t, msg),
	}

	got := withoutHeader(t, screen(1, nil, nil, done("m/M", mix, wire(t, msg), ok, time.Millisecond)))

	want := "1/1 m/M: success, OK, 1.00ms\n  request: {\"user_name\":\"ann\"}\n  response: {\"user_name\":\"ann\"}\n"
	if got != want {
		t.Errorf("block:\n%s\nwant:\n%s", got, want)
	}
}

// A body over 4096 bytes of compact JSON is cut at 4096 and says how long it
// was; the cut never leaves half a rune.
// Ground: boundary — 4096 bytes in, mid-rune and on a rune edge.
func TestDryRun_ABodyOver4096BytesIsCutOnARuneBoundary(t *testing.T) {
	mix := mixMethod(t)

	t.Run("ascii, JSON of 5000 bytes", func(t *testing.T) {
		const overhead = len(`{"text":""}`)
		text := strings.Repeat("a", 5000-overhead)
		ok := engine.Outcome{
			Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true,
			Response: mixText(t, mix, text),
		}

		got := withoutHeader(t, screen(1, nil, nil, done("m/M", mix, nil, ok, time.Millisecond)))

		full := `{"text":"` + text + `"}`
		want := "1/1 m/M: success, OK, 1.00ms\n  request: {}\n  response: " + full[:4096] + "… (5000 bytes in all)\n"
		if got != want {
			t.Errorf("block differs (got %d bytes, want %d); tail of got: %q", len(got), len(want), tail(got, 60))
		}
	})

	t.Run("a rune on the 4096th byte", func(t *testing.T) {
		text := strings.Repeat("é", 2500)
		ok := engine.Outcome{
			Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true,
			Response: mixText(t, mix, text),
		}

		got := withoutHeader(t, screen(1, nil, nil, done("m/M", mix, nil, ok, time.Millisecond)))

		if !utf8.ValidString(got) {
			t.Fatalf("the cut left a broken rune: %q", tail(got, 40))
		}

		full := `{"text":"` + text + `"}`
		want := "1/1 m/M: success, OK, 1.00ms\n  request: {}\n  response: " + full[:4095] + "… (" + strconv.Itoa(len(full)) + " bytes in all)\n"
		if got != want {
			t.Errorf("block differs (got %d bytes, want %d); tail of got: %q", len(got), len(want), tail(got, 60))
		}
	})
}

// The cut is of the text with the secrets already replaced: a secret that
// straddles byte 4096 must not leave its first letters on the screen.
// Ground: boundary — the secret starts three bytes before the cut.
func TestDryRun_SecretsAreReplacedBeforeTheCut(t *testing.T) {
	mix := mixMethod(t)
	redact := cli.DryRunRedactor(
		map[string]string{"authorization": "Bearer ${LT_DRY_TOKEN}"},
		func(name string) (string, bool) { return "s3cr3t", name == "LT_DRY_TOKEN" },
	)

	// `{"text":"` is 9 bytes: the secret starts at byte 4093 of the JSON.
	text := strings.Repeat("a", 4093-9) + "s3cr3t" + strings.Repeat("b", 500)
	ok := engine.Outcome{
		Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true,
		Response: mixText(t, mix, text),
	}

	got := screen(1, redact, nil, done("m/M", mix, nil, ok, time.Millisecond))

	if strings.Contains(got, "s3c") {
		t.Errorf("the first letters of the secret reached the screen: %q", tail(got, 80))
	}

	// The JSON is 9 + 4084 + 6 + 500 + 2 = 4601 bytes; the placeholder is 9
	// longer than the secret, and the number is of the text that was cut.
	if !strings.HasSuffix(got, "… (4610 bytes in all)\n") {
		t.Errorf("the body was not cut as 4610 bytes: %q", tail(got, 80))
	}
}

// The message of a failed call goes through the same replacement.
// Ground: contract — #81, secrets never printed; the target's message may echo what was sent.
func TestDryRun_ARedactedMessageShowsThePlaceholder(t *testing.T) {
	redact := cli.DryRunRedactor(
		map[string]string{"authorization": "Bearer ${LT_DRY_TOKEN}"},
		func(string) (string, bool) { return "s3cr3t", true },
	)
	out := targetSays(codes.Unauthenticated, `invalid token "Bearer s3cr3t"`)

	got := withoutHeader(t, screen(1, redact, nil, done("m/M", healthMethod(), nil, out, time.Millisecond)))

	want := "  message: invalid token \"Bearer ${LT_DRY_TOKEN}\"\n"
	if !strings.Contains(got, want) {
		t.Errorf("block:\n%s\nwant it to hold %q", got, want)
	}
}

// The target chooses the message, and a terminal acts on an escape sequence in
// it and reads a newline as the next line of the block: control characters are
// written as \xNN, letters of any script stay.
// Ground: contract — nothing the target sends moves the cursor or forges a line.
func TestDryRun_ControlCharactersInAMessageAreWrittenOut(t *testing.T) {
	out := targetSays(codes.Internal, "boom\x1b[2J\nresponse: {} да")

	got := withoutHeader(t, screen(1, nil, nil, done("m/M", healthMethod(), nil, out, time.Millisecond)))

	want := `  message: boom\x1b[2J\x0aresponse: {} да` + "\n"
	if !strings.Contains(got, want) {
		t.Errorf("block:\n%s\nwant it to hold %q", got, want)
	}
}

// Metadata is one line after the blocks, keys sorted, values as written; none,
// no line.
// Ground: contract — RawMetadata is what the config says, ${ENV} unexpanded.
func TestDryRun_MetadataIsOneSortedLineOrNone(t *testing.T) {
	ok := engine.Outcome{Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true}
	call := done("m/M", healthMethod(), nil, ok, time.Millisecond)

	for _, md := range []map[string]string{nil, {}} {
		if got := screen(1, nil, md, call); strings.Contains(got, "metadata") {
			t.Errorf("metadata %v printed a line:\n%s", md, got)
		}
	}

	got := screen(1, nil, map[string]string{"x-b": "2", "authorization": "Bearer ${T}", "x-a": "$${HOME}"}, call)
	want := "metadata: authorization: Bearer ${T}, x-a: $${HOME}, x-b: 2\n"
	if !strings.HasSuffix(got, "\n"+want) {
		t.Errorf("screen:\n%s\nwant it to end with %q", got, want)
	}
}

// K of a stop is the number of printed blocks.
// Ground: contract — K in "stopped after K of N calls" is what the screen shows.
func TestDryRun_PrintedCountsTheBlocksOnScreen(t *testing.T) {
	var out bytes.Buffer
	p := &cli.DryRunPrinter{W: &out, Total: 3}

	p.Header()
	if got := p.Printed(); got != 0 {
		t.Errorf("after the header Printed = %d, want 0", got)
	}

	ok := engine.Outcome{Category: engine.CategorySuccess, Code: "OK", CodeFromTarget: true}
	for want := 1; want <= 2; want++ {
		p.Call(done("m/M", healthMethod(), nil, ok, time.Millisecond))
		if got := p.Printed(); got != want {
			t.Errorf("after %d blocks Printed = %d", want, got)
		}
	}

	if got := strings.Count(out.String(), " m/M: "); got != 2 {
		t.Errorf("%d blocks on screen, want 2:\n%s", got, out.String())
	}
}

// One pass over the text, longest value first: no order of the variables, and
// no value inside an inserted ${NAME}, can leave a piece of a secret.
// Ground: contract — the replacement does not depend on the order of the variables (Go randomises map order, so the loop builds the redactor many times).
func TestDryRunRedactor_ALongerValueWinsOverItsSubstring(t *testing.T) {
	lookup := func(name string) (string, bool) {
		return map[string]string{"TOKEN": "ab1cd", "SHARD": "1", "LT_A": "xyz12345", "LT_B": "LT_A"}[name], true
	}

	for range 50 {
		redact := cli.DryRunRedactor(map[string]string{
			"authorization": "Bearer ${TOKEN}", "x-shard": "${SHARD}",
		}, lookup)
		if got, want := redact("Bearer ab1cd, shard 1"), "Bearer ${TOKEN}, shard ${SHARD}"; got != want {
			t.Fatalf("redact = %q, want %q", got, want)
		}

		// Two values that begin at the same place: the pair earlier in the list
		// wins, so the longer one must come first.
		redact = cli.DryRunRedactor(map[string]string{
			"authorization": "Bearer ${TOKEN}", "x-pre": "${PRE}",
		}, func(name string) (string, bool) { return map[string]string{"TOKEN": "ab1cd", "PRE": "ab"}[name], true })
		if got, want := redact("Bearer ab1cd"), "Bearer ${TOKEN}"; got != want {
			t.Fatalf("redact = %q, want %q", got, want)
		}

		// Occurrences that overlap without one holding the other: the leftmost
		// match is not the longest, and cutting at it would leave 1cd of a token.
		redact = cli.DryRunRedactor(map[string]string{
			"authorization": "Bearer ${TOKEN}", "x-shard": "${SHARD}",
		}, func(name string) (string, bool) {
			return map[string]string{"TOKEN": "ab1cd", "SHARD": "xab"}[name], true
		})
		if got, want := redact("xab1cd"), "${TOKEN}"; got != want {
			t.Fatalf("redact = %q, want %q", got, want)
		}
		if got := redact("xab1cd"); strings.Contains(got, "1cd") {
			t.Fatalf("a piece of the token is left: %q", got)
		}

		// A value that is the name of another variable must not eat the
		// placeholder just put in: a second pass would make ${${LT_B}}.
		redact = cli.DryRunRedactor(map[string]string{"a": "${LT_A}", "b": "${LT_B}"}, lookup)
		if got, want := redact("xyz12345"), "${LT_A}"; got != want {
			t.Fatalf("redact = %q, want %q", got, want)
		}
	}
}

// A secret with a quote or a backslash is also in the reply's JSON in its
// escaped form.
// Ground: boundary — the value of a metadata key is printable ASCII, quote and backslash included (app.go).
func TestDryRunRedactor_ReplacesTheRawAndTheJSONEscapedForm(t *testing.T) {
	redact := cli.DryRunRedactor(
		map[string]string{"authorization": "${LT_T}"},
		func(string) (string, bool) { return `ab"c\d`, true },
	)

	if got, want := redact(`token ab"c\d`), `token ${LT_T}`; got != want {
		t.Errorf("raw form: %q, want %q", got, want)
	}
	if got, want := redact(`{"u":"ab\"c\\d"}`), `{"u":"${LT_T}"}`; got != want {
		t.Errorf("escaped form: %q, want %q", got, want)
	}
}

// Names come from what was substituted. "$${NAME}" is the literal "${NAME}":
// the variable was never sent, and its value may well be in the reply.
// Ground: contract — "$${" writes a literal (app.go); an unset or empty variable has no value to hide.
func TestDryRunRedactor_OnlySubstitutedNamesAreReplaced(t *testing.T) {
	env := map[string]string{"LT_HOME": "/home/acme", "LT_TOKEN": "tok3n", "LT_EMPTY": ""}
	lookup := func(name string) (string, bool) { v, ok := env[name]; return v, ok }

	redact := cli.DryRunRedactor(map[string]string{
		"x-note":        "$${LT_HOME}",
		"authorization": "Bearer ${LT_TOKEN}",
		"x-empty":       "${LT_EMPTY}",
		"x-unset":       "${LT_UNSET}",
		"x-again":       "${LT_TOKEN}",
	}, lookup)

	if got, want := redact("home /home/acme, tok3n, tok3n"), "home /home/acme, ${LT_TOKEN}, ${LT_TOKEN}"; got != want {
		t.Errorf("redact = %q, want %q", got, want)
	}
	if got := redact("nothing to hide"); got != "nothing to hide" {
		t.Errorf("an empty or unset value changed the text: %q", got)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[len(s)-n:]
}
