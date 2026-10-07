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

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// writeDataset writes a file called users.jsonl in a directory of its own and
// returns its path with forward slashes, which a YAML scalar and Windows both read.
func writeDataset(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "users.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}

	return filepath.ToSlash(path)
}

// jsonAt walks a decoded JSON document by keys.
func jsonAt(t *testing.T, v any, path ...string) any {
	t.Helper()

	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%v: not an object at %q", path, p)
		}
		if v, ok = m[p]; !ok {
			t.Fatalf("%v: no field %q", path, p)
		}
	}

	return v
}

// The whole path: a dataset of 3 records, the real CLI, a target with
// reflection. The target sees the bodies of the records in the file's order and
// wrapping, a blank line in the file sends nothing, and the report says how the
// file was used (7 requests over 3 records: 3 of 3, each up to 3 times).
// Ground: contract — end to end, the order of records on the wire.
func TestRun_DatasetBodiesReachTheTargetInOrder(t *testing.T) {
	target := startReflectingTarget(t)
	dataset := writeDataset(t, "{\"responseSize\":1}\n{\"responseSize\":2}\n\n{\"responseSize\":3}\n")
	cfg := dataConfig(t, target.addr,
		"    - method: "+unaryCallMethod+"\n      rps: 10\n      duration: 700ms\n      dataset: '"+dataset+"'\n")

	res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	var got []int32
	for _, req := range target.recorder.allSimple() {
		got = append(got, req.GetResponseSize())
	}
	if want := []int32{1, 2, 3, 1, 2, 3, 1}; !slices.Equal(got, want) {
		t.Errorf("the target saw response_size %v, want %v", got, want)
	}
	if want := "  data: 3 of 3 requests from users.jsonl, each used up to 3 times\n"; !strings.Contains(res.stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
	}
}

// -fake runs no schema: the config's records are counted, not encoded, and the
// run does not panic on the empty bodies it sends. The path stays as written,
// relative to the config file, in the report and the JSON.
// Ground: contract — what -fake promises of a dataset: n from the config.
func TestRun_FakeWithDataset(t *testing.T) {
	cfg := writeConfigWith(t, closedPort(t), checkMethod, plaintext, "      dataset: users.jsonl\n")
	if err := os.WriteFile(filepath.Join(filepath.Dir(cfg), "users.jsonl"),
		[]byte("{\"service\":\"a\"}\n{\"service\":\"b\"}\n{\"service\":\"c\"}\n"), 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}

	res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	// 50 rps for 300ms: 15 requests over 3 records.
	if want := "  data: 3 of 3 requests from users.jsonl, each used up to 5 times\n"; !strings.Contains(res.stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
	}

	res = runCLI(t.Context(), t, 10*time.Second, "-fake", "-output", "json", "-c", cfg)
	if res.err != nil {
		t.Fatalf("json run: %v", res.err)
	}
	d, ok := jsonAt(t, decodeOnly(t, res.stdout), "methods").([]any)[0].(map[string]any)["dataset"].(map[string]any)
	if !ok {
		t.Fatalf("dataset is not an object:\n%s", res.stdout)
	}
	if d["file"] != "users.jsonl" || d["records"] != 3.0 || d["used"] != 3.0 || d["used_max"] != 5.0 {
		t.Errorf("dataset = %v, want users.jsonl as written, 3 records, used 3, used_max 5", d)
	}
}

// A call without a dataset beside one that has it: JSON null for it, no line.
// Ground: contract — the JSON of a method with no dataset is null, on the real path.
func TestRun_JSONDatasetIsNullWithoutOne(t *testing.T) {
	res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-output", "json",
		"-c", writeConfig(t, closedPort(t), checkMethod, plaintext))
	if res.err != nil {
		t.Fatalf("run: %v", res.err)
	}

	method := jsonAt(t, decodeOnly(t, res.stdout), "methods").([]any)[0]
	if v := jsonAt(t, method, "dataset"); v != nil {
		t.Errorf("dataset = %v, want null", v)
	}

	res = runCLI(t.Context(), t, 10*time.Second, "-fake", "-c", writeConfig(t, closedPort(t), checkMethod, plaintext))
	if res.err != nil {
		t.Fatalf("run: %v", res.err)
	}
	if strings.Contains(res.stdout, "data:") {
		t.Errorf("a run with no dataset prints a data line:\n%s", res.stdout)
	}
}

// The contents of a record are in no output of the run: a secret in a field of
// the wrong type or a key the message lacks (found against the schema, from the
// target's reflection) and in a line that is not JSON (found at load, with -fake
// too) — the error names the line and the message, nothing of the value. Exit 1.
// Ground: contract — secrets in a dataset are as safe as in data; protojson's own text quotes them.
func TestRun_DatasetValueIsNeverPrinted(t *testing.T) {
	const secret = "S3CR3T-token-4821"

	cases := map[string]struct {
		line, want string
		fake       bool
	}{
		"a wrong type":      {`{"responseSize":"` + secret + `"}`, "users.jsonl:2: does not fit grpc.testing.SimpleRequest", false},
		"an unknown key":    {`{"` + secret + `":1}`, "users.jsonl:2: does not fit grpc.testing.SimpleRequest", false},
		"a line cut short":  {`{"responseSize":"` + secret, "users.jsonl:2: not JSON", false},
		"cut short on fake": {`{"responseSize":"` + secret, "users.jsonl:2: not JSON", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			target := startReflectingTarget(t)
			dataset := writeDataset(t, "{\"responseSize\":1}\n"+tc.line+"\n")
			cfg := dataConfig(t, target.addr,
				"    - method: "+unaryCallMethod+"\n      rps: 10\n      duration: 300ms\n      dataset: '"+dataset+"'\n")
			args := []string{"-c", cfg}
			if tc.fake {
				args = append([]string{"-fake"}, args...)
			}

			res := runCLI(t.Context(), t, 10*time.Second, args...)

			if exitCode(res.err) != 1 {
				t.Fatalf("exit code %d, want 1: a config error\nerr %v\nstderr %s", exitCode(res.err), res.err, res.stderr)
			}
			all := res.err.Error() + res.stdout + res.stderr
			if !strings.Contains(all, tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, all)
			}
			if strings.Contains(all, secret) {
				t.Errorf("the record's content is in the output:\n%s", all)
			}
			if got := len(target.recorder.allSimple()); got != 0 {
				t.Errorf("the target saw %d requests: the run must not start", got)
			}
		})
	}
}
