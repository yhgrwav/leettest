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
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
)

const (
	datasetMethod = "wallet.v1.Wallet/One"
	datasetPath   = "data/users.jsonl"
	recName       = "leettest.test.Rec"
)

// recMessage is a request with an int32, a string and a map: the shapes a
// record can get wrong in three different ways.
func recMessage(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()

	field := func(name string, num int32, kind descriptorpb.FieldDescriptorProto_Type, label descriptorpb.FieldDescriptorProto_Label, typeName string) *descriptorpb.FieldDescriptorProto {
		f := &descriptorpb.FieldDescriptorProto{
			Name: proto.String(name), Number: proto.Int32(num), Type: kind.Enum(), Label: label.Enum(),
		}
		if typeName != "" {
			f.TypeName = proto.String(typeName)
		}

		return f
	}
	const (
		optional = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
		repeated = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
		str      = descriptorpb.FieldDescriptorProto_TYPE_STRING
		i32      = descriptorpb.FieldDescriptorProto_TYPE_INT32
		i64      = descriptorpb.FieldDescriptorProto_TYPE_INT64
		msg      = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	)

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name: proto.String("leettest/test/rec.proto"), Package: proto.String("leettest.test"), Syntax: proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Rec"),
			Field: []*descriptorpb.FieldDescriptorProto{
				field("id", 1, i32, optional, ""),
				field("name", 2, str, optional, ""),
				field("attrs", 3, msg, repeated, ".leettest.test.Rec.AttrsEntry"),
				field("big", 4, i64, optional, ""),
			},
			NestedType: []*descriptorpb.DescriptorProto{{
				Name:    proto.String("AttrsEntry"),
				Field:   []*descriptorpb.FieldDescriptorProto{field("key", 1, str, optional, ""), field("value", 2, str, optional, "")},
				Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
			}},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("build the test file: %v", err)
	}

	return file.Messages().ByName("Rec")
}

// datasetCalls is a call with records on the given lines of its file.
func datasetCalls(records ...config.DatasetRecord) config.Call {
	return config.Call{Method: datasetMethod, Dataset: datasetPath, Records: records}
}

func rec(line int, json string) config.DatasetRecord {
	return config.DatasetRecord{Line: line, JSON: []byte(json)}
}

func recResolver(t *testing.T) *fakeResolver {
	t.Helper()

	return &fakeResolver{known: map[string]protoreflect.MessageDescriptor{datasetMethod: recMessage(t)}}
}

// wire is the message with fields set as the schema says, through the
// descriptor and not through protojson: what the sender must put on the wire.
func wire(t *testing.T, id int32, name string, attrs map[string]string) []byte {
	t.Helper()

	desc := recMessage(t)
	msg := dynamicpb.NewMessage(desc)
	if id != 0 {
		msg.Set(desc.Fields().ByName("id"), protoreflect.ValueOfInt32(id))
	}
	if name != "" {
		msg.Set(desc.Fields().ByName("name"), protoreflect.ValueOfString(name))
	}
	for k, v := range attrs {
		msg.Mutable(desc.Fields().ByName("attrs")).Map().Set(protoreflect.ValueOfString(k).MapKey(), protoreflect.ValueOfString(v))
	}

	body, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	return body
}

// Ground: contract — each record becomes its own request body before the run:
// by the method's schema, in file order, blank lines (here 2 and 5) in no body.
func TestAttachData_DatasetEachRecordEncoded(t *testing.T) {
	cfg, calls := loadOf(datasetCalls(
		rec(1, `{"id":1,"name":"a"}`),
		rec(3, `{"id":2}`),
		rec(4, `{"name":"c","attrs":{"k":"v"}}`),
	))

	unchecked, err := AttachData(t.Context(), recResolver(t), cfg, calls)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if len(unchecked) != 0 {
		t.Errorf("unchecked = %v, want none", unchecked)
	}

	want := [][]byte{
		wire(t, 1, "a", nil),
		wire(t, 2, "", nil),
		wire(t, 0, "c", map[string]string{"k": "v"}),
	}
	if len(calls[0].Payloads) != len(want) {
		t.Fatalf("payloads = %d, want %d", len(calls[0].Payloads), len(want))
	}
	for i := range want {
		if !bytes.Equal(calls[0].Payloads[i], want[i]) {
			t.Errorf("payload %d = %x, want %x", i, calls[0].Payloads[i], want[i])
		}
	}
}

// A record is the line's bytes into protojson: an integer above 2^53 arrives
// as written, as in data.
// Ground: contract — the same rule data has (RequestBody), kept for a record.
func TestAttachData_DatasetRecordKeepsAnInt64Exactly(t *testing.T) {
	// 2^53 + 1: a float64 anywhere on the way would make it 2^53.
	const big int64 = 9_007_199_254_740_993

	desc := recMessage(t)
	cfg, calls := loadOf(datasetCalls(rec(1, `{"big": 9007199254740993}`)))

	if _, err := AttachData(t.Context(), recResolver(t), cfg, calls); err != nil {
		t.Fatalf("attach: %v", err)
	}

	msg := dynamicpb.NewMessage(desc)
	msg.Set(desc.Fields().ByName("big"), protoreflect.ValueOfInt64(big))
	want, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(calls[0].Payloads) != 1 || !bytes.Equal(calls[0].Payloads[0], want) {
		t.Errorf("payloads = %x, want %x: the integer exactly", calls[0].Payloads, want)
	}
}

// A record that does not fit its message stops the run before it, named by the
// line it stood on (blank lines counted) and the message: not the value, not
// the key, not protojson's words. The first such record is the one named.
// Ground: contract — the text is ours; the prefix is data's (ErrRequestData).
func TestAttachData_DatasetLineThatDoesNotFitNamesItsLine(t *testing.T) {
	want := "request data does not fit the method: wallet.v1.Wallet/One: dataset data/users.jsonl:4: does not fit " + recName

	for name, bad := range map[string]string{
		"a wrong type":       `{"id":"abc"}`,
		"an unknown field":   `{"nope":1}`,
		"a duplicate field":  `{"id":1,"id":2}`,
		"a number":           `42`,
		"an array":           `[1]`,
		"null":               `null`,
		"a string":           `"x"`,
		"a value out of int": `{"id":99999999999}`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, calls := loadOf(datasetCalls(rec(1, `{"id":1}`), rec(4, bad), rec(6, `{"id":"later"}`)))

			_, err := AttachData(t.Context(), recResolver(t), cfg, calls)

			if !errors.Is(err, ErrRequestData) {
				t.Fatalf("error = %v, want %v", err, ErrRequestData)
			}
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
		})
	}
}

// Without reflection a record cannot be built, and a call with a dataset must
// not fall back to the empty message a call with no data runs with: a run of
// 1000 empty requests under a report that says "data from users.jsonl".
// Ground: contract — dataset is in the same refusal as data (data.go switch).
func TestAttachData_DatasetNoReflectionIsAnError(t *testing.T) {
	cfg, calls := loadOf(datasetCalls(rec(1, `{"id":1}`)))
	resolver := &fakeResolver{err: descriptor.ErrReflectionUnsupported}

	unchecked, err := AttachData(t.Context(), resolver, cfg, calls)

	if !errors.Is(err, descriptor.ErrReflectionUnsupported) {
		t.Fatalf("error = %v, want %v", err, descriptor.ErrReflectionUnsupported)
	}
	if !strings.Contains(err.Error(), datasetMethod) {
		t.Errorf("error %q does not name the method", err)
	}
	if len(unchecked) != 0 {
		t.Errorf("unchecked = %v, want none: a refusal, not a warning", unchecked)
	}
	if len(calls[0].Payloads) != 0 {
		t.Errorf("payloads = %d, want none built from an unknown schema", len(calls[0].Payloads))
	}
}

// Ground: contract — the same as for data (TestAttachData_AMethodWithDataFailsWhenReflectionIsUnusable).
func TestAttachData_DatasetResolverTimeoutIsAnError(t *testing.T) {
	for name, resolveErr := range map[string]error{
		"timed out": status.Error(codes.DeadlineExceeded, "reflection did not answer"),
		"refused":   status.Error(codes.PermissionDenied, "reflection needs a token"),
	} {
		t.Run(name, func(t *testing.T) {
			cfg, calls := loadOf(datasetCalls(rec(1, `{"id":1}`)))

			unchecked, err := AttachData(t.Context(), &fakeResolver{err: resolveErr}, cfg, calls)

			if err == nil {
				t.Fatal("the run was allowed to start without the schema its records need")
			}
			if !strings.Contains(err.Error(), datasetMethod) {
				t.Errorf("error %q does not name the method", err)
			}
			if len(unchecked) != 0 {
				t.Errorf("unchecked = %v, want none", unchecked)
			}
			if len(calls[0].Payloads) != 0 {
				t.Errorf("payloads = %d, want none", len(calls[0].Payloads))
			}
		})
	}
}

// What a record holds is never printed: a secret in a field of the wrong type,
// in a line cut short, in a key the message does not have and in a key of a map
// given twice — protojson quotes each of them in its own errors.
// Ground: signal google.golang.org/protobuf v1.36.12 — protojson's errors quote
// the value (decode.go:349, :630) and an unknown key (:113); the test goes red
// if our text lets any of that through, whichever way a dependency words it.
func TestAttachData_DatasetNeverPrintsTheValue(t *testing.T) {
	const secret = "S3CR3T-token-4821"

	for name, bad := range map[string]string{
		"a wrong type":           `{"id":"` + secret + `"}`,
		"a line cut short":       `{"name":"` + secret,
		"an unknown key":         `{"` + secret + `":1}`,
		"a map key given twice":  `{"attrs":{"` + secret + `":"a","` + secret + `":"b"}}`,
		"a message for a string": `{"name":{"` + secret + `":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, calls := loadOf(datasetCalls(rec(1, `{"id":1}`), rec(2, bad)))

			_, err := AttachData(t.Context(), recResolver(t), cfg, calls)

			if err == nil {
				t.Fatal("a record that does not fit was accepted")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error %q holds what the record held", err)
			}
		})
	}
}

const datasetConfigYAML = `
app:
  target:
    ip: localhost
    port: 50051
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 10
      duration: 1s
      dataset: data/users.jsonl
`

// A config from Parse names a dataset and holds none of its records: calls
// built from it would send one empty request per call under a report of the
// file. It is an error where the calls are built, in the form of a config
// error, and the engine never sees it.
// Ground: contract — pkg/config.Parse is public and reads no file; LoadFile is
// the way to records.
func TestCalls_DatasetNotReadIsAnError(t *testing.T) {
	cfg, err := config.Parse([]byte(datasetConfigYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	calls, err := CallsFromConfig(cfg)

	want := "wallet.v1.WalletService/GetBalance: dataset data/users.jsonl: not read; load the config with LoadFile"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
	if len(calls) != 0 {
		t.Errorf("calls = %d, want none with an error", len(calls))
	}
}

// -fake runs no schema: the engine's call carries n empty payloads, n from the
// config, a counter of its own, and the path as written for the report. A call
// without a dataset carries none of it.
// Ground: contract — what the engine and AttachData are handed.
func TestCalls_DatasetGivesNEmptyPayloadsAndACounter(t *testing.T) {
	cfg := &config.MasterConfig{Load: config.Load{Calls: []config.Call{
		{Method: "a.B/One", RPS: 5, Duration: time.Second, Dataset: "data/one.jsonl",
			Records: []config.DatasetRecord{rec(1, `{}`), rec(2, `{}`), rec(5, `{}`)}},
		{Method: "a.B/Two", RPS: 5, Duration: time.Second},
		{Method: "a.B/Three", RPS: 5, Duration: time.Second, Dataset: "data/three.jsonl",
			Records: []config.DatasetRecord{rec(1, `{}`)}},
	}}}

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}

	one, two, three := calls[0], calls[1], calls[2]
	if len(one.Payloads) != 3 {
		t.Fatalf("a.B/One payloads = %d, want 3, one per record", len(one.Payloads))
	}
	for i, p := range one.Payloads {
		if len(p) != 0 {
			t.Errorf("payload %d = %x, want empty until AttachData fills it", i, p)
		}
	}
	if one.Dataset == nil || one.Dataset.File != "data/one.jsonl" || one.Dataset.Counter == nil {
		t.Errorf("a.B/One: dataset %+v, want the path as written and a counter", one.Dataset)
	}
	if two.Dataset != nil || len(two.Payloads) != 0 {
		t.Errorf("a.B/Two: dataset %+v, payloads %d, want none of it", two.Dataset, len(two.Payloads))
	}
	if three.Dataset == nil || one.Dataset == nil || one.Dataset.Counter == three.Dataset.Counter {
		t.Error("two dataset calls share one counter")
	}
	if len(three.Payloads) != 1 {
		t.Errorf("a.B/Three payloads = %d, want 1", len(three.Payloads))
	}
}

// reportWith is a run report of the given methods, each sent 10.
func reportWith(methods ...engine.MethodReport) RunReport {
	for i := range methods {
		methods[i].Sent = 10
	}

	return RunReport{Report: engine.Report{Sent: 10 * len(methods), Methods: methods}}
}

func datasetOf(file string, records, used, usedMax int) *engine.DatasetReport {
	return &engine.DatasetReport{File: file, Records: records, Used: used, UsedMax: usedMax}
}

// withDatasets is res with each step's first method given the dataset report
// of: a copy all the way down, the fixtures are shared by other tests.
func withDatasets(res breakpoint.Result, of func(step int) *engine.DatasetReport) breakpoint.Result {
	steps := slices.Clone(res.Steps)
	for i := range steps {
		steps[i].Report.Methods = slices.Clone(steps[i].Report.Methods)
		steps[i].Report.Methods[0].Dataset = of(i)
	}
	res.Steps = steps

	return res
}

// lineAfter is the line right after the first one that starts with prefix.
func lineAfter(t *testing.T, out, prefix string) string {
	t.Helper()

	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	t.Fatalf("no line starts with %q:\n%s", prefix, out)

	return ""
}

// The line under a method's row, exact, in each of its forms: what was used of
// the file and how often the most used record went out, singular where it is
// one, plain digits like the rest of stdout.
// Ground: contract — text that carries a claim: how much of the file the run
// used, and never that the target saw that many distinct requests.
func TestReport_DatasetLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    *engine.DatasetReport
		want string
	}{
		{"records used more than once", datasetOf("data/users.jsonl", 3, 3, 5),
			"  data: 3 of 3 requests from users.jsonl, each used up to 5 times"},
		{"each used once", datasetOf("data/users.jsonl", 3, 3, 1),
			"  data: 3 of 3 requests from users.jsonl, each used up to 1 time"},
		{"fewer sent than there are records", datasetOf("data/users.jsonl", 10, 4, 1),
			"  data: 4 of 10 requests from users.jsonl, each used up to 1 time"},
		{"a file of one record", datasetOf("data/users.jsonl", 1, 1, 7),
			"  data: 1 of 1 request from users.jsonl, each used up to 7 times"},
		{"a file of one record, used once", datasetOf("data/users.jsonl", 1, 1, 1),
			"  data: 1 of 1 request from users.jsonl, each used up to 1 time"},
		{"nothing used", datasetOf("data/users.jsonl", 3, 0, 0),
			"  data: none of 3 requests from users.jsonl used"},
		{"nothing used, one record", datasetOf("data/users.jsonl", 1, 0, 0),
			"  data: none of 1 request from users.jsonl used"},
		{"no thousands separator", datasetOf("data/users.jsonl", 1234, 1000, 2),
			"  data: 1000 of 1234 requests from users.jsonl, each used up to 2 times"},
		{"a name with dots, in a directory above", datasetOf("../shared/users.v2.jsonl", 2, 2, 3),
			"  data: 2 of 2 requests from users.v2.jsonl, each used up to 3 times"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			PrintReport(&out, "localhost:50051", reportWith(engine.MethodReport{Method: "/pkg.S/M", Dataset: tc.d}))

			if got := lineAfter(t, out.String(), "pkg.S/M "); got != tc.want {
				t.Errorf("line under the method = %q, want %q", got, tc.want)
			}
			if n := strings.Count(out.String(), "  data: "); n != 1 {
				t.Errorf("%d data lines, want 1:\n%s", n, out.String())
			}
		})
	}
}

// The line belongs to its method's row, ahead of the answer rows under it, and
// a method without a dataset has none.
// Ground: contract — where the line stands and for whom.
func TestReport_DatasetLineStandsUnderItsOwnMethod(t *testing.T) {
	var out strings.Builder
	PrintReport(&out, "localhost:50051", reportWith(
		engine.MethodReport{Method: "/pkg.S/Plain"},
		engine.MethodReport{Method: "/pkg.S/WithData", Dataset: datasetOf("users.jsonl", 3, 3, 2), Overload: engine.RefusalLatency{Count: 2}},
	))

	if got := lineAfter(t, out.String(), "pkg.S/Plain "); strings.Contains(got, "data:") {
		t.Errorf("the method with no dataset has a data line: %q", got)
	}
	if got, want := lineAfter(t, out.String(), "pkg.S/WithData "), "  data: 3 of 3 requests from users.jsonl, each used up to 2 times"; got != want {
		t.Errorf("line under the row = %q, want %q", got, want)
	}
	if !strings.Contains(out.String(), "  overload") {
		t.Fatalf("test setup: no answer row to stand ahead of:\n%s", out.String())
	}
	if data, answer := strings.Index(out.String(), "  data: "), strings.Index(out.String(), "  overload"); data > answer {
		t.Errorf("the data line stands after the answer rows:\n%s", out.String())
	}
}

// The text of the search carries the line once, for the whole search: the last
// run's count is the search's. It stands under the headline block, before the
// blank line that opens the table. No dataset, no line.
// Ground: contract — the one place the search's text says what data it ran on.
func TestBreakpoint_DatasetLine(t *testing.T) {
	const want = "  data: 3 of 3 requests from users.jsonl, each used up to 7 times"

	for name, res := range map[string]breakpoint.Result{"held_all": outcomes["held_all"].res, "broke": outcomes["broke"].res} {
		t.Run(name, func(t *testing.T) {
			last := len(res.Steps) - 1
			res = withDatasets(res, func(i int) *engine.DatasetReport {
				if i == last {
					return datasetOf("data/users.jsonl", 3, 3, 7)
				}

				return datasetOf("data/users.jsonl", 3, 3, 1+i)
			})

			out := printedSearch(res)

			if n := strings.Count(out, "  data: "); n != 1 {
				t.Fatalf("%d data lines, want 1:\n%s", n, out)
			}
			shape := regexp.MustCompile(`(?s)\Abreaking point: pkg\.Svc/Do\n(  [^\n]+\n){1,2}` + regexp.QuoteMeta(want) + `\n\n`)
			if !shape.MatchString(out) {
				t.Errorf("the line is not the last of the headline block, before the blank line:\n%s", out)
			}
		})
	}

	for name, tc := range outcomes {
		if strings.Contains(printedSearch(tc.res), "  data: ") {
			t.Errorf("%s: a search with no dataset prints a data line", name)
		}
	}
}

// The line is the last run's: a dataset only on an earlier run prints nothing,
// and one only on the last run prints it with that run's count.
// Ground: contract — the line is of the call the search ran, which is the last report's.
func TestBreakpoint_DatasetLineOnlyFromTheLastRun(t *testing.T) {
	steps := len(outcomes["held_all"].res.Steps)

	earlier := withDatasets(outcomes["held_all"].res, func(i int) *engine.DatasetReport {
		if i == 0 {
			return datasetOf("data/users.jsonl", 3, 3, 1)
		}

		return nil
	})
	if out := printedSearch(earlier); strings.Contains(out, "  data: ") {
		t.Errorf("a line from a run that is not the last:\n%s", out)
	}

	lastOnly := withDatasets(outcomes["held_all"].res, func(i int) *engine.DatasetReport {
		if i == steps-1 {
			return datasetOf("data/users.jsonl", 3, 3, 4)
		}

		return nil
	})
	if want, out := "  data: 3 of 3 requests from users.jsonl, each used up to 4 times\n", printedSearch(lastOnly); !strings.Contains(out, want) {
		t.Errorf("no %q for a dataset on the last run:\n%s", want, out)
	}
}

// The method object of a plain run: the object, or null; four keys and no more.
// Ground: contract — the JSON names are an interface (schema_v1.txt).
func TestJSON_Dataset(t *testing.T) {
	out := writeJSON(t, jsonRun(engine.Report{Methods: []engine.MethodReport{
		{Method: "/pkg.S/Data", Sent: 1, Dataset: datasetOf("data/users.jsonl", 3, 3, 5)},
		{Method: "/pkg.S/Plain", Sent: 1},
	}}))

	methods := field(t, out, "methods").([]any)

	obj, ok := field(t, methods[0], "dataset").(map[string]any)
	if !ok {
		t.Fatalf("dataset = %v, want an object", field(t, methods[0], "dataset"))
	}
	want := map[string]any{"file": "data/users.jsonl", "records": 3.0, "used": 3.0, "used_max": 5.0}
	if len(obj) != len(want) {
		t.Errorf("dataset = %v, want exactly %v", obj, want)
	}
	for k, v := range want {
		if obj[k] != v {
			t.Errorf("dataset.%s = %v, want %v", k, obj[k], v)
		}
	}

	if v := field(t, methods[1], "dataset"); v != nil {
		t.Errorf("a method with no dataset: dataset = %v, want null", v)
	}
}

// In a search every run's own method object carries the count to that run's
// end, not the search's final one; the search's own object has no new field.
// Ground: contract — "cumulative to that run's end", the JSON of D3.
func TestJSON_DatasetIsCumulativePerRun(t *testing.T) {
	res := withDatasets(outcomes["held_all"].res, func(i int) *engine.DatasetReport {
		return datasetOf("data/users.jsonl", 3, min(3, 4*(i+1)), (4*(i+1)+2)/3)
	})

	out := searchJSON(t, res)

	runs := field(t, out, "breakpoint", "runs").([]any)
	if len(runs) != len(res.Steps) {
		t.Fatalf("runs = %d, want %d", len(runs), len(res.Steps))
	}
	for i, r := range runs {
		methods := field(t, r, "report", "methods").([]any)
		got := field(t, methods[0], "dataset", "used_max")
		if want := float64((4*(i+1) + 2) / 3); got != want {
			t.Errorf("run %d: used_max = %v, want %v, the count to the end of that run", i, got, want)
		}
	}

	for k := range out {
		switch k {
		case "schema_version", "mode", "leettest_version", "target", "method", "started_at", "breakpoint":
		default:
			t.Errorf("top level has %q, a field no schema has", k)
		}
	}
}
