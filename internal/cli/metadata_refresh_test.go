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
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/yhgrwav/leettest/pkg/config"
)

// mrFileApp is the app of a config whose only header reads a file: HasMetadata
// is true and Metadata is empty, because the value is not known until it is read.
func mrFileApp(t *testing.T) config.App {
	t.Helper()

	cfg, err := config.Parse([]byte("app:\n  target:\n    ip: localhost\n    port: 50051\n" +
		"  metadata:\n    authorization: '${file:token.txt}'\n" +
		"load:\n  calls:\n    - method: wallet.v1.Wallet/One\n      rps: 10\n      duration: 1s\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !cfg.App.HasMetadata() || len(cfg.App.Metadata) != 0 {
		t.Fatalf("premise: HasMetadata %v, len(Metadata) %d; want a file-only app.metadata",
			cfg.App.HasMetadata(), len(cfg.App.Metadata))
	}

	return cfg.App
}

// Ground: contract (spec §4, C4) — the guard that stops a run whose credentials
// the target refused asks HasMetadata, not len(Metadata): a header that reads a
// file is metadata the run sends. Mutation M22 (len(Metadata) > 0) dies here.
func TestMetadataFile_AFileOnlyMetadataStopsOnUnauthenticatedAtACall(t *testing.T) {
	cfg, calls := loadOf(config.Call{Method: "wallet.v1.Wallet/One"})
	cfg.App = mrFileApp(t)
	resolver := &fakeResolver{err: status.Error(codes.Unauthenticated, "echo s3cret-token")}

	unchecked, err := AttachData(t.Context(), resolver, cfg, calls)
	if !errors.Is(err, ErrCredentialsRejected) {
		t.Fatalf("err = %v, unchecked %v; want ErrCredentialsRejected: the token from the file was refused", err, unchecked)
	}

	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("the error repeats the target's message: %v", err)
	}
}

// Ground: contract (spec §4, C4) — the same for a step of a scenario.
func TestMetadataFile_AFileOnlyMetadataStopsOnUnauthenticatedAtAStep(t *testing.T) {
	cfg := scenarioConfig()
	cfg.App = mrFileApp(t)

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	empty := (&emptypb.Empty{}).ProtoReflect().Descriptor()
	resolver := &stepRefusingResolver{
		known: fakeResolver{known: map[string]protoreflect.MessageDescriptor{
			wsFirst: empty, wsSecond: empty, wsThird: empty, wsPing: empty,
		}},
		refuse: map[string]error{wsSecond: status.Error(codes.Unauthenticated, "echo s3cret-token")},
	}

	unchecked, _, err := AttachMethods(t.Context(), resolver, cfg, calls)
	if !errors.Is(err, ErrCredentialsRejected) {
		t.Fatalf("err = %v, unchecked %v; want ErrCredentialsRejected at the step", err, unchecked)
	}

	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("the error repeats the target's message: %v", err)
	}
}

func mrNoEnv(string) (string, bool) { return "", false }

// Ground: contract (spec §4.2, C5) — the value of a file is replaced by
// ${file:PATH} in the raw form and in the form it has inside a JSON string; the
// value itself, not the header it sits in. Mutation M23 (no file values, or
// no JSON-escaped form) dies on the first and second rows.
func TestDryRunRedactor_AFileValueIsReplacedByItsPlaceholder(t *testing.T) {
	cases := []struct {
		name, value, text, want string
	}{
		{"raw", "tok-abc", "denied tok-abc here", "denied ${file:token.txt} here"},
		{"JSON-escaped", `ab"cd\ef`, `{"msg":"ab\"cd\\ef"}`, `{"msg":"${file:token.txt}"}`},
		{"the raw form of a value with a quote", `ab"cd\ef`, `say ab"cd\ef`, `say ${file:token.txt}`},
		{"the prefix is not a secret", "tok-abc", "Bearer tok-abc", "Bearer ${file:token.txt}"},
		{"every occurrence", "tok-abc", "tok-abc, tok-abc", "${file:token.txt}, ${file:token.txt}"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			redact := DryRunRedactor(nil, mrNoEnv, []config.FileValue{{Path: "token.txt", Value: c.value}})

			if got := redact(c.text); got != c.want {
				t.Errorf("redact(%q) = %q, want %q", c.text, got, c.want)
			}
		})
	}
}

// Ground: contract (spec §4.2, C5) — a value of a file and a ${ENV} value that
// overlap are one piece, and the longest wins, whichever of the two it is.
func TestDryRunRedactor_TheLongestOfAFileAndAnEnvironmentValueWins(t *testing.T) {
	raw := map[string]string{"x-key": "${LONG}"}

	cases := []struct {
		name, env, file, text, want string
	}{
		{"the variable is longer", "tok-abc-longer", "tok-abc", "x tok-abc-longer y tok-abc z", "x ${LONG} y ${file:token.txt} z"},
		{"the file is longer", "tok-abc", "tok-abc-longer", "x tok-abc-longer y", "x ${file:token.txt} y"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lookup := func(name string) (string, bool) { return c.env, name == "LONG" }
			redact := DryRunRedactor(raw, lookup, []config.FileValue{{Path: "token.txt", Value: c.file}})

			if got := redact(c.text); got != c.want {
				t.Errorf("redact(%q) = %q, want %q", c.text, got, c.want)
			}
		})
	}
}

// Ground: contract (spec §4.2) — an empty form hides nothing: a file's value
// is never empty (the read refuses it), and a stray empty one must not turn
// every position of the text into a placeholder.
func TestDryRunRedactor_AnEmptyFileValueIsSkipped(t *testing.T) {
	redact := DryRunRedactor(nil, mrNoEnv, []config.FileValue{{Path: "token.txt", Value: ""}})

	if got := redact("abc"); got != "abc" {
		t.Errorf("redact(abc) = %q, want it untouched", got)
	}
}

// mrRun is a run of the report with its settings and the refresh given.
func mrRun(refresh *MetadataRefresh) RunReport {
	return RunReport{
		Report: echoReport(), Name: "x", MetadataRefresh: refresh,
		Settings: &RunSettings{MaxInFlight: 5000, Connections: 1},
	}
}

const (
	mrSettingsLine = "settings: warm-up 0s, max in flight 5000, connections 1"
	mrLastError    = `key "authorization": file token.txt: is empty`
)

// Ground: contract (spec §5.1) — the line under settings: says every interval,
// the reads, the changed and the failed; the tail appears with the first
// failure, and "(failing at the end)" only when the last read failed.
func TestMetadataRefresh_TheLineUnderSettings(t *testing.T) {
	cases := []struct {
		name    string
		refresh MetadataRefresh
		want    string
	}{
		{
			"no failure", MetadataRefresh{Every: 5 * time.Minute, Reads: 12, Changed: 3},
			"metadata refresh: every 5m0s, 12 reads, 3 changed, 0 failed",
		},
		{
			"failing at the end",
			MetadataRefresh{Every: 5 * time.Minute, Reads: 12, Changed: 3, Failures: 2, FailingAtEnd: true, LastError: mrLastError},
			"metadata refresh: every 5m0s, 12 reads, 3 changed, 2 failed (failing at the end); " +
				"the last good value stayed in use; last error: " + mrLastError,
		},
		{
			"recovered",
			MetadataRefresh{Every: 5 * time.Minute, Reads: 12, Changed: 3, Failures: 2, LastError: mrLastError},
			"metadata refresh: every 5m0s, 12 reads, 3 changed, 2 failed; " +
				"the last good value stayed in use; last error: " + mrLastError,
		},
		{
			"a whole interval is told as a Duration", MetadataRefresh{Every: time.Second, Reads: 1},
			"metadata refresh: every 1s, 1 reads, 0 changed, 0 failed",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := echoLines(mrRun(&c.refresh))

			i := indexOfPrefix(lines, "settings:")
			if i < 0 || lines[i] != mrSettingsLine || i+1 >= len(lines) {
				t.Fatalf("no settings line in:\n%s", strings.Join(lines, "\n"))
			}

			if lines[i+1] != c.want {
				t.Errorf("the line under settings:\n%q\nwant\n%q", lines[i+1], c.want)
			}
		})
	}
}

// Ground: contract (spec §5.1: "сразу под settings:") — the refresh line comes
// before the stop_on_errors echo, so it is the line under settings:. Spec text
// does not name the order against stop_on_errors; this is the literal reading.
func TestMetadataRefresh_TheLineSitsBeforeTheStopLimitEcho(t *testing.T) {
	run := mrRun(&MetadataRefresh{Every: 5 * time.Minute, Reads: 12, Changed: 3})
	run.Settings.StopOnErrors = "5%"

	lines := echoLines(run)

	i := indexOfPrefix(lines, "settings:")
	if i < 0 || i+2 >= len(lines) {
		t.Fatalf("no settings line in:\n%s", strings.Join(lines, "\n"))
	}

	if want := "metadata refresh: every 5m0s, 12 reads, 3 changed, 0 failed"; lines[i+1] != want {
		t.Errorf("line after settings: %q, want %q", lines[i+1], want)
	}

	if want := "stop_on_errors: 5%"; lines[i+2] != want {
		t.Errorf("line after the refresh line: %q, want %q", lines[i+2], want)
	}
}

// Ground: contract (spec §5, C6) — refresh off means nothing new: no line, in
// a text report that is the release's byte for byte. Mutation M26 (a line when
// off) dies here.
func TestMetadataRefresh_OffPrintsNoLine(t *testing.T) {
	text := strings.Join(echoLines(mrRun(nil)), "\n")

	if strings.Contains(text, "metadata refresh") {
		t.Errorf("a refresh line with refresh off:\n%s", text)
	}
}

// Ground: contract (spec §4.1) — the report never holds a value: the line names
// the key, the path and the class of the cause, which the read gave, and the
// line says only what it was given.
func TestMetadataRefresh_TheLineIsMadeOfTheCountersAndTheErrorOnly(t *testing.T) {
	text := strings.Join(echoLines(mrRun(&MetadataRefresh{
		Every: time.Second, Reads: 3, Failures: 3, FailingAtEnd: true, LastError: mrLastError,
	})), "\n")

	if !strings.Contains(text, "last error: "+mrLastError+"\n") {
		t.Errorf("the last error is not as given, ending the line:\n%s", text)
	}
}

// Ground: contract (spec §5.1) — the search report has the same line under its
// settings line, before the blank line that closes the echo.
func TestMetadataRefresh_ASearchTellsItUnderItsSettings(t *testing.T) {
	run := echoSearch(bpPlan, 2)
	run.MetadataRefresh = &MetadataRefresh{Every: 5 * time.Minute, Reads: 12, Changed: 3}

	want := "settings: settle 1s, hold 5s, max in flight per step, connections 2\n" +
		"metadata refresh: every 5m0s, 12 reads, 3 changed, 0 failed\n\n"
	if out := echoSearchText(run); !strings.Contains(out, "\nname: wallet\n"+want) {
		t.Errorf("no %q after the name in:\n%s", want, out)
	}

	run.MetadataRefresh = nil
	if out := echoSearchText(run); strings.Contains(out, "metadata refresh") {
		t.Errorf("a refresh line with refresh off:\n%s", out)
	}
}

// mrObject is the metadata_refresh key of a document: whether it is there, and
// what it holds.
func mrObject(t *testing.T, doc map[string]any) (value any, present bool) {
	t.Helper()

	value, present = doc["metadata_refresh"]

	return value, present
}

// Ground: contract (spec §5.2, C6) — the key is always there, null with refresh
// off (null is not 0), in a run's JSON and in a search's. Mutation M26's JSON
// half and a missing key die here.
func TestMetadataRefresh_JSONIsNullWhenOff(t *testing.T) {
	docs := map[string]map[string]any{
		"run":    writeJSON(t, jsonRun(echoReport())),
		"search": searchJSONOf(t, echoSearch(bpPlan, 2)),
	}

	for name, doc := range docs {
		v, present := mrObject(t, doc)
		if !present {
			t.Errorf("%s: no metadata_refresh key: it is always there, null when off", name)

			continue
		}

		if v != nil {
			t.Errorf("%s: metadata_refresh = %v, want null", name, v)
		}

		if got := doc["schema_version"]; got != 1.0 {
			t.Errorf("%s: schema_version = %v, want 1: the key is additive", name, got)
		}
	}
}

// Ground: contract (spec §5.2) — the object: the interval in microseconds, the
// counters, whether the last read failed, and the last error: null until a
// read failed, kept after a recovery.
func TestMetadataRefresh_JSONObject(t *testing.T) {
	cases := []struct {
		name    string
		refresh MetadataRefresh
		want    map[string]any
	}{
		{
			"no failure", MetadataRefresh{Every: 5 * time.Minute, Reads: 12, Changed: 3},
			map[string]any{"interval_us": 300_000_000.0, "reads": 12.0, "changed": 3.0, "failures": 0.0, "failing_at_end": false, "last_error": nil},
		},
		{
			"failing at the end",
			MetadataRefresh{Every: 1500 * time.Millisecond, Reads: 5, Changed: 1, Failures: 2, FailingAtEnd: true, LastError: mrLastError},
			map[string]any{"interval_us": 1_500_000.0, "reads": 5.0, "changed": 1.0, "failures": 2.0, "failing_at_end": true, "last_error": mrLastError},
		},
		{
			"recovered keeps the last error",
			MetadataRefresh{Every: time.Second, Reads: 5, Changed: 2, Failures: 1, LastError: mrLastError},
			map[string]any{"interval_us": 1_000_000.0, "reads": 5.0, "changed": 2.0, "failures": 1.0, "failing_at_end": false, "last_error": mrLastError},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := jsonRun(echoReport())
			run.Run.MetadataRefresh = &c.refresh

			search := echoSearch(bpPlan, 2)
			search.MetadataRefresh = &c.refresh

			for name, doc := range map[string]map[string]any{"run": writeJSON(t, run), "search": searchJSONOf(t, search)} {
				obj, ok := doc["metadata_refresh"].(map[string]any)
				if !ok {
					t.Errorf("%s: metadata_refresh = %v, want an object", name, doc["metadata_refresh"])

					continue
				}

				if len(obj) != len(c.want) {
					t.Errorf("%s: fields %v, want exactly %v", name, obj, c.want)
				}

				for k, want := range c.want {
					if got, present := obj[k]; !present || got != want {
						t.Errorf("%s: metadata_refresh.%s = %v (present %v), want %v", name, k, got, present, want)
					}
				}
			}
		})
	}
}

// Ground: contract (spec §5.3, C7) — compare does not read the object: reports
// with it, without it and with another one compare as they did, with no
// difference named and the same verdict. The reports are the JSON of a run with
// the key put in, so the test does not need the type that carries it.
func TestMetadataRefresh_CompareIgnoresTheObject(t *testing.T) {
	plain := func(t *testing.T) RunFile { t.Helper(); return crBase().file(t, "base.json") }
	withObject := func(t *testing.T, path string, obj map[string]any) RunFile {
		t.Helper()

		file := crNew().file(t, path)

		raw, err := json.Marshal(file.Report)
		if err != nil {
			t.Fatal(err)
		}

		var doc map[string]any
		if err = json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}

		doc["metadata_refresh"] = obj

		if raw, err = json.Marshal(doc); err != nil {
			t.Fatal(err)
		}

		var out JSONReport
		if err = json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}

		return RunFile{Path: path, Report: out}
	}

	obj := map[string]any{
		"interval_us": 1_000_000, "reads": 9, "changed": 2, "failures": 4, "failing_at_end": true, "last_error": "x",
	}

	var without, with bytes.Buffer

	wantResult := CompareRuns(&without, plain(t), crNew().file(t, "new.json"), crLimit(t, "-max-p99-rise", "20%"), crLimit(t, "-max-errors-rise", "1%"))
	gotResult := CompareRuns(&with, plain(t), withObject(t, "new.json", obj), crLimit(t, "-max-p99-rise", "20%"), crLimit(t, "-max-errors-rise", "1%"))

	if gotResult != wantResult {
		t.Errorf("comparison %+v, want %+v as without the object", gotResult, wantResult)
	}

	if with.String() != without.String() {
		t.Errorf("output with the object:\n%s\nwithout it:\n%s", with.String(), without.String())
	}

	if lines := crDiffs(with.String()); len(lines) != 0 {
		t.Errorf("a difference named for the object: %v", lines)
	}
}
