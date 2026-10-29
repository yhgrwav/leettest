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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
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

// withWarmup is the config at path with a warm-up in its load.
func withWarmup(t *testing.T, path, warmup string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	changed := strings.Replace(string(data), "load:\n", "load:\n  warmup: "+warmup+"\n", 1)
	if changed == string(data) {
		t.Fatal("the config has no load: line to put the warm-up under")
	}
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// Ground: contract (brief point 7, reviewer T4) — the JSON names the
// placeholders used, sorted, and the last seq handed out, which is more than
// the counted requests by the warm-up's; a call without templates has null,
// the key present.
func TestRun_TemplatesInTheJSON(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dataConfig(t, target.addr,
		"    - method: "+unaryCallMethod+"\n      rps: 20\n      duration: 500ms\n"+templatedData+
			"    - method: "+checkMethod+"\n      rps: 10\n      duration: 300ms\n")
	withWarmup(t, cfg, "200ms")

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
		case "/" + unaryCallMethod:
			obj, ok := got.(map[string]any)
			if !ok {
				t.Errorf("%v: templates = %v, want an object", name, got)

				continue
			}
			if list, _ := obj["placeholders"].([]any); len(list) != 2 || list[0] != "seq" || list[1] != "uuid" {
				t.Errorf("%v: placeholders = %v, want [seq uuid]", name, obj["placeholders"])
			}

			sent, warm := jsonAt(t, m, "sent").(float64), jsonAt(t, m, "warmup_sent").(float64)
			if warm == 0 {
				t.Errorf("%v: no warm-up request went out: the test needs some", name)
			}
			// 20 rps for 500 ms hand out 10 requests, the warm-up's among them.
			if obj["last_seq"] != 10.0 || obj["last_seq"] != sent+warm || obj["last_seq"].(float64) <= sent {
				t.Errorf("%v: last_seq = %v, sent %v, warm-up %v; want 10 = sent + warm-up, more than sent", name, obj["last_seq"], sent, warm)
			}
		case "/" + checkMethod:
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

// A run the guard stopped is a soft stop, like Stop or an exhausted file: the
// report is printed, the exit code is 3, and the error of the guard is not what
// the command returns instead of it.
// Ground: contract (reviewer T1) — exit 3 and a report, not exit 1 and nothing.
// Mutation: the guard stops the run, the command returns its error, the report is lost.
func TestRunResult_ASeqOverflowIsASoftStop(t *testing.T) {
	run := cli.RunReport{Report: engine.Report{
		Incomplete:  true,
		SeqOverflow: &engine.SeqOverflow{Method: "/pkg.S/M", Limit: 2147483647},
	}}
	runErr := fmt.Errorf("%w: /pkg.S/M, limit 2147483647", engine.ErrSeqOverflow)

	err := runResult(run, runErr)
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("runResult = %v, want ErrIncomplete", err)
	}
	if got := exitCode(err); got != 3 {
		t.Errorf("exit code %d, want 3", got)
	}
}

// Ground: contract (reviewer T2) — the plan is checked against the field
// before the first request: a config that hands out more than int32 holds is
// exit 1 and the target sees nothing. The message names the method, the limit
// and how many requests the plan hands out (1e6 rps for 3 h).
// Mutation: CheckTemplatesFit is not called; seq wraps in int32.
func TestRun_APlanPastTheFieldIsRefusedBeforeTheFirstRequest(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dataConfig(t, target.addr,
		"    - method: "+unaryCallMethod+"\n      rps: 1000000\n      duration: 3h\n      timeout: 100ms\n"+
			"      templates: true\n      data:\n        response_size: '{{seq}}'\n")

	res := runCLI(t.Context(), t, 10*time.Second, "-max-in-flight", "5000000", "-c", cfg)

	if got := exitCode(res.err); got != 1 {
		t.Fatalf("exit code %d, want 1\nerr %v\nstderr:\n%s", got, res.err, res.stderr)
	}

	all := res.err.Error() + res.stderr
	for _, want := range []string{strings.TrimPrefix(unaryCallMethod, "/"), "2147483647", "10800000000"} {
		if !strings.Contains(all, want) {
			t.Errorf("the error lacks %q (method, limit, requests the plan hands out):\n%s", want, all)
		}
	}
	if n := len(target.recorder.allSimple()); n != 0 {
		t.Errorf("the target saw %d requests of a refused plan", n)
	}
}

// seqSearch runs the breaking-point search over a call whose {{seq}} field holds
// 3: the first step (50 rps) hands out 3 requests and the guard stops it before
// the 4th. It is the one path where the guard fires in a real command, because
// a plain run is refused earlier by CheckTemplatesFit. The calls are made here,
// not by the CLI, so that the limit can be as small as the test needs.
func seqSearch(t *testing.T, asJSON bool) (stdout string, err error) {
	t.Helper()

	cfg, err := config.LoadFile(writeSearch(t, closedPort(t), checkMethod))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	calls, err := cli.CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}
	calls[0].Template = engine.NewTemplate([]string{"seq"}, 3, nil)

	ctx, abort := context.WithCancel(t.Context())
	defer abort()

	var (
		out, errOut bytes.Buffer
		stopper     atomic.Pointer[cli.Stopper]
	)
	err = runSearch(ctx, abort, &stopper, searchSetup{
		cfg: cfg, target: "fake", sender: engine.FakeSender{}, calls: calls, json: asJSON,
	}, &out, &errOut)

	return out.String(), err
}

// A search whose {{seq}} outgrows its field ends as stopped, with its report:
// not as an error that throws the hours of data away, and not as a step that
// "broke the system" (the field's limit is not the target's).
// Ground: contract (reviewer R1, deviation 1) — the same guarantee as T1, for
// the one path where the guard really fires.
// Mutation: the step's filter lets ErrSeqOverflow through as an ordinary error
// (the search returns it, no report); stopFor(StopSeqOverflow) is gone (the
// search is stopped with no reason, no line, null in JSON).
func TestSearch_ASeqPastItsFieldEndsTheSearchStoppedWithAReport(t *testing.T) {
	out, err := seqSearch(t, false)
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("the search returned %v, want ErrIncomplete (exit 3, the report printed)", err)
	}
	if got := exitCode(err); got != 3 {
		t.Errorf("exit code %d, want 3", got)
	}
	for _, want := range []string{"stopped at the first step", "{{seq}} of " + strings.TrimPrefix(checkMethod, "/") + " outgrew its field (limit 3) at 50 rps"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}

	out, err = seqSearch(t, true)
	if exitCode(err) != 3 {
		t.Fatalf("json: the search returned %v (exit %d), want exit 3", err, exitCode(err))
	}
	bp, ok := jsonAt(t, decodeOnly(t, out), "breakpoint").(map[string]any)
	if !ok {
		t.Fatalf("json: no breakpoint object:\n%s", out)
	}
	if bp["outcome"] != "stopped" || bp["stop_reason"] != "seq_overflow" {
		t.Errorf("breakpoint = outcome %v stop_reason %v, want stopped, seq_overflow", bp["outcome"], bp["stop_reason"])
	}
	if v, present := bp["broke_rps"]; !present || v != nil {
		t.Errorf("broke_rps = %v (present %v), want null with the key present: the field's limit is not a break of the target", v, present)
	}
	if runs, _ := bp["runs"].([]any); len(runs) != 1 {
		t.Errorf("%d runs listed, want 1: the step the guard stopped", len(runs))
	}
}

// -fake has no schema: the numbers are counted and no body is built, as with a
// dataset; the syntax of the placeholders is still checked, because that needs
// no schema.
// Ground: contract (reviewer T5) — what -fake promises of templates.
// Mutation: -fake builds a body; -fake skips the syntax check.
func TestRun_FakeCountsTemplatesAndBuildsNothing(t *testing.T) {
	cfg := writeConfigWith(t, closedPort(t), checkMethod, plaintext,
		"      templates: true\n      data:\n        service: 'wallet-{{uuid}}-{{seq}}'\n")

	res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	// 50 rps for 300 ms: 15 requests.
	if want := "  data: templated, seq uuid (last seq 15, ramp and warm-up counted)\n"; !strings.Contains(res.stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
	}

	res = runCLI(t.Context(), t, 10*time.Second, "-fake", "-output", "json", "-c", cfg)
	if res.err != nil {
		t.Fatalf("json run: %v", res.err)
	}
	obj, ok := jsonAt(t, decodeOnly(t, res.stdout), "methods").([]any)[0].(map[string]any)["templates"].(map[string]any)
	if !ok || obj["last_seq"] != 15.0 {
		t.Errorf("templates = %v, want an object with last_seq 15", obj)
	}

	bad := writeConfigWith(t, closedPort(t), checkMethod, plaintext,
		"      templates: true\n      data:\n        service: 'wallet-{{nope}}'\n")

	res = runCLI(t.Context(), t, 10*time.Second, "-fake", "-c", bad)
	if got := exitCode(res.err); got != 1 {
		t.Fatalf("a wrong placeholder under -fake: exit code %d, want 1\nerr %v", got, res.err)
	}
	if all := res.err.Error() + res.stderr; !strings.Contains(all, "{{nope}}") {
		t.Errorf("the error does not name the placeholder:\n%s", all)
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
			[]string{"templates work with `data` only"},
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
