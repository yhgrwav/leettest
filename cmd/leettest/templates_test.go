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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// templatedData is the data of a UnaryCall: a number of the request in an int32
// and a string with a uuid and the number in it.
const templatedData = "      templates: true\n" +
	"      data:\n" +
	"        response_size: '{{seq}}'\n" +
	"        response_status:\n" +
	"          message: 'order-{{uuid}}-{{seq}}'\n"

var orderMessageShape = regexp.MustCompile(`^order-([0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})-(\d+)$`)

// The whole path: the real CLI, a target with reflection. Each of the 7
// requests of the run arrives with its own number, once, the uuid in its text
// is its own, and the report says what was counted.
// Ground: contract — end to end, what the target sees and what the report
// claims of it (brief points 3 and 7, amendment Д).
func TestRun_TemplatesReachTheTargetOneSeqAndOneUuidPerRequest(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dataConfig(t, target.addr,
		"    - method: "+unaryCallMethod+"\n      rps: 10\n      duration: 700ms\n"+templatedData)

	res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	seen := target.recorder.allSimple()
	var sizes []int
	uuids := map[string]bool{}
	for _, req := range seen {
		sizes = append(sizes, int(req.GetResponseSize()))

		m := orderMessageShape.FindStringSubmatch(req.GetResponseStatus().GetMessage())
		if m == nil {
			t.Errorf("message %q is not order-<uuid v4>-<seq>", req.GetResponseStatus().GetMessage())

			continue
		}
		if m[2] != strconv.Itoa(int(req.GetResponseSize())) {
			t.Errorf("the request has seq %d in a number and %s in a string: one request, one seq", req.GetResponseSize(), m[2])
		}
		uuids[m[1]] = true
	}

	slices.Sort(sizes)
	if want := []int{1, 2, 3, 4, 5, 6, 7}; !slices.Equal(sizes, want) {
		t.Errorf("the target saw seq %v, want %v each once", sizes, want)
	}
	if len(uuids) != len(seen) {
		t.Errorf("%d distinct uuids in %d requests", len(uuids), len(seen))
	}
	if want := "  data: templated, seq uuid (last seq 7, ramp and warm-up counted)\n"; !strings.Contains(res.stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
	}
}

// Ground: contract (brief point 7) — the JSON names the placeholders used,
// sorted; a call without templates has null, the key present.
func TestRun_TemplatesInTheJSON(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dataConfig(t, target.addr,
		"    - method: "+unaryCallMethod+"\n      rps: 10\n      duration: 300ms\n"+templatedData+
			"    - method: "+checkMethod+"\n      rps: 10\n      duration: 300ms\n")

	res := runCLI(t.Context(), t, 10*time.Second, "-output", "json", "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	out := decodeOnly(t, res.stdout)
	methods := jsonAt(t, out, "methods").([]any)
	if len(methods) != 2 {
		t.Fatalf("%d methods, want 2", len(methods))
	}

	for _, m := range methods {
		name := jsonAt(t, m, "method")
		got := jsonAt(t, m, "templates")

		switch name {
		case unaryCallMethod:
			list, _ := got.([]any)
			if len(list) != 2 || list[0] != "seq" || list[1] != "uuid" {
				t.Errorf("%v: templates = %v, want [seq uuid]", name, got)
			}
		case checkMethod:
			if got != nil {
				t.Errorf("%v: templates = %v, want null", name, got)
			}
		default:
			t.Errorf("unexpected method %v", name)
		}
	}
	if v := jsonAt(t, out, "schema_version"); v != 1.0 {
		t.Errorf("schema_version = %v, want 1", v)
	}
}

// Ground: contract (brief point 1) — without the key the body is as it was:
// the braces go out as text, the report says nothing of templates.
// Mutation: placeholders work at templates: false.
func TestRun_WithoutTheKeyBracesGoOutAsText(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dataConfig(t, target.addr,
		"    - method: "+unaryCallMethod+"\n      rps: 10\n      duration: 300ms\n"+
			"      data:\n        response_status:\n          message: 'order-{{uuid}}-{{seq}}'\n")

	res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	seen := target.recorder.allSimple()
	if len(seen) == 0 {
		t.Fatal("the target saw no request")
	}
	for _, req := range seen {
		if got := req.GetResponseStatus().GetMessage(); got != "order-{{uuid}}-{{seq}}" {
			t.Errorf("message = %q, want the text as written", got)
		}
	}
	if strings.Contains(res.stdout, "templated") {
		t.Errorf("the report speaks of templates for a call without them:\n%s", res.stdout)
	}
}

// A search is many runs of one process: the number of the call goes on through
// the steps, as the record of a dataset does, so that no step repeats a key the
// step before it used. The text says it once, for the whole search.
// Ground: contract (amendment Б) — the counter lives in the Call and survives
// the steps of a search.
// Mutation: the seq counter resets between search steps.
func TestRun_ASearchGoesOnCountingThroughItsSteps(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := writeSearchWith(t, target.addr, quickSearch, "200ms",
		"      templates: true\n      data:\n        response_size: '{{seq}}'\n", unaryCallMethod)

	res := runCLI(t.Context(), t, 60*time.Second, "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	var sizes []int
	for _, req := range target.recorder.allSimple() {
		sizes = append(sizes, int(req.GetResponseSize()))
	}
	slices.Sort(sizes)

	// Steps of 50, 63 and 79 rps hand out 15, 19 and 24 requests: more than any
	// one step does, and every number once.
	if len(sizes) <= 24 {
		t.Fatalf("the target saw %d requests: the search did not run past its first steps\n%s", len(sizes), res.stdout)
	}
	for i, got := range sizes {
		if got != i+1 {
			t.Fatalf("the target saw seq %v, want 1..%d each once: a step started counting over", sizes, len(sizes))
		}
	}
	if want := "last seq " + strconv.Itoa(len(sizes)) + ", ramp and warm-up counted)"; strings.Count(res.stdout, want) != 1 {
		t.Errorf("stdout lacks %q exactly once:\n%s", want, res.stdout)
	}
}

// A dry run sends one request, number 1, and shows it as it went.
// Ground: contract (brief point 7, amendment Е) — -dry-run sends seq 1.
func TestRun_DryRunSendsSeqOneAndShowsTheRequest(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dryConfig(t, target.addr, "", dryCall(unaryCallMethod, templatedData))

	res := runCLI(t.Context(), t, 5*time.Second, "-c", cfg, "-dry-run")
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	seen := target.recorder.allSimple()
	if len(seen) != 1 {
		t.Fatalf("the target saw %d requests, want 1", len(seen))
	}

	req := seen[0]
	m := orderMessageShape.FindStringSubmatch(req.GetResponseStatus().GetMessage())
	if req.GetResponseSize() != 1 || m == nil || m[2] != "1" {
		t.Fatalf("the target saw response_size %d, message %q; want seq 1 in both", req.GetResponseSize(), req.GetResponseStatus().GetMessage())
	}
	for _, want := range []string{`"response_size":1`, req.GetResponseStatus().GetMessage()} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("the request line lacks %q, the request as it went:\n%s", want, res.stdout)
		}
	}
}

// A config that cannot be templated is refused before a request leaves: exit 1,
// the method and the path of the field in the message.
// Ground: contract (brief point 2, amendments Г, В) — the errors of the unit.
func TestRun_TemplateConfigErrorsAreRefusedBeforeTheFirstRequest(t *testing.T) {
	dataset := writeDataset(t, lines(3))

	for _, tc := range []struct {
		name string
		call string
		want []string
	}{
		{
			"an unknown placeholder",
			"      templates: true\n      data:\n        response_size: '{{seq}}'\n        response_status:\n          message: 'a-{{nope}}'\n",
			[]string{unaryCallMethod, "data.response_status.message", "{{nope}}", "{{uuid}}", "{{seq}}"},
		},
		{
			"a placeholder with spaces",
			"      templates: true\n      data:\n        response_status:\n          message: '{{ uuid }}'\n",
			[]string{unaryCallMethod, "data.response_status.message", "{{ uuid }}"},
		},
		{
			"templates without a placeholder",
			"      templates: true\n      data:\n        response_size: 5\n",
			[]string{unaryCallMethod, "templates: true"},
		},
		{
			"a placeholder in a bool",
			"      templates: true\n      data:\n        response_size: '{{seq}}'\n        fill_username: '{{seq}}'\n",
			[]string{unaryCallMethod, "data.fill_username"},
		},
		{
			"uuid in a number",
			"      templates: true\n      data:\n        response_size: '{{uuid}}'\n",
			[]string{unaryCallMethod, "data.response_size"},
		},
		{
			"templates with a dataset",
			"      templates: true\n      dataset: '" + dataset + "'\n",
			[]string{"templates work with data only"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := startReflectingTarget(t)
			cfg := dataConfig(t, target.addr,
				"    - method: "+unaryCallMethod+"\n      rps: 10\n      duration: 300ms\n"+tc.call)

			res := runCLI(t.Context(), t, 10*time.Second, "-c", cfg)

			if got := exitCode(res.err); got != 1 {
				t.Fatalf("exit code %d, want 1\nerr %v\nstderr:\n%s", got, res.err, res.stderr)
			}
			all := res.err.Error() + res.stderr
			for _, want := range tc.want {
				if !strings.Contains(all, want) {
					t.Errorf("the error lacks %q:\n%s", want, all)
				}
			}
			if n := len(target.recorder.allSimple()); n != 0 {
				t.Errorf("the target saw %d requests of a refused config", n)
			}
		})
	}
}
