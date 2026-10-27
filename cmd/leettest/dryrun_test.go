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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const emptyCallMethod = "grpc.testing.TestService/EmptyCall"

// dryCall is a call as a load config writes it: rps and duration far past what
// a dry run may use, so that a dry run that sent more than one request, or
// waited for the plan, shows.
func dryCall(method, extra string) string {
	return "    - method: " + method + "\n      rps: 100\n      duration: 5s\n" + extra
}

// dryConfig is a plaintext config of the target with appLines (two-space
// indented, under app:) and the calls as written.
func dryConfig(t *testing.T, addr, appLines, calls string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}

	cfg := fmt.Sprintf("app:\n  target:\n    ip: %s\n    port: %s\n  tls: false\n%sload:\n  calls:\n%s",
		host, port, appLines, calls)

	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// echoTarget is a test service that gives the caller's authorization header
// back: in a reply field from UnaryCall, in the message of the error from
// EmptyCall.
type echoTarget struct {
	testgrpc.UnimplementedTestServiceServer

	// extra goes after the header in UnaryCall's username.
	extra string

	mu    sync.Mutex
	auths []string
}

func (e *echoTarget) authorization(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	auth := strings.Join(md.Get("authorization"), ",")

	e.mu.Lock()
	e.auths = append(e.auths, auth)
	e.mu.Unlock()

	return auth
}

func (e *echoTarget) UnaryCall(ctx context.Context, _ *testgrpc.SimpleRequest) (*testgrpc.SimpleResponse, error) {
	return &testgrpc.SimpleResponse{Username: e.authorization(ctx) + " " + e.extra}, nil
}

func (e *echoTarget) EmptyCall(ctx context.Context, _ *testgrpc.Empty) (*testgrpc.Empty, error) {
	return nil, status.Errorf(codes.Unauthenticated, "invalid token %q", e.authorization(ctx))
}

func (e *echoTarget) received() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]string(nil), e.auths...)
}

// startEchoTarget serves echoTarget with reflection on a real TCP port.
func startEchoTarget(t *testing.T, extra string) (string, *echoTarget) {
	t.Helper()

	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	echo := &echoTarget{extra: extra}
	srv := grpc.NewServer()
	testgrpc.RegisterTestServiceServer(srv, echo)
	reflection.Register(srv)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String(), echo
}

// A dry run sends each call of the config once and returns: the plan's rps and
// duration play no part, and the screen holds a block per call and no report.
// Ground: contract — the flag's one promise, one request per call, nothing measured.
func TestRun_DryRunSendsOneRequestPerCall(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dryConfig(t, target.addr, "", dryCall(checkMethod, "")+dryCall(unaryCallMethod, ""))

	began := time.Now()
	res := runCLI(t.Context(), t, 3*time.Second, "-c", cfg, "-dry-run")
	elapsed := time.Since(began)

	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	if got := target.health.calls.Load(); got != 1 {
		t.Errorf("Health/Check got %d requests, want 1", got)
	}
	if got := len(target.recorder.allSimple()); got != 1 {
		t.Errorf("UnaryCall got %d requests, want 1", got)
	}
	if elapsed >= 3*time.Second {
		t.Errorf("a dry run of calls planned for 5 s took %v", elapsed)
	}

	for _, want := range []string{
		"dry run: 2 calls, one request each, nothing measured\n",
		"1/2 " + checkMethod + ": success, OK, ",
		"  request: {}\n  response: {\"status\":\"SERVING\"}\n",
		"2/2 " + unaryCallMethod + ": success, OK, ",
		"  request: {}\n  response: {}\n",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
		}
	}
	for _, not := range []string{"run finished", "metadata:"} {
		if strings.Contains(res.stdout, not) {
			t.Errorf("stdout has %q: a dry run writes no report and this config has no metadata:\n%s", not, res.stdout)
		}
	}
}

// A call the target answers with an error makes the step fail, so that a CI
// step «dry run, then load» does not start the load.
// Ground: contract — exit 1 = no report as asked, CI reads it as don't start the load (Spec 5).
func TestRun_DryRunFailsWhenACallFails(t *testing.T) {
	target := startReflectingTarget(t)
	// The test service leaves EmptyCall unimplemented: UNIMPLEMENTED from the target.
	cfg := dryConfig(t, target.addr, "", dryCall(emptyCallMethod, ""))

	res := runCLI(t.Context(), t, 3*time.Second, "-c", cfg, "-dry-run")

	if got := exitCode(res.err); got != 1 {
		t.Fatalf("exit code %d, want 1\nerr %v\nstdout:\n%s", got, res.err, res.stdout)
	}
	if all := res.err.Error() + res.stderr; !strings.Contains(all, "dry run: 1 of 1 calls did not succeed") {
		t.Errorf("no verdict line:\n%s", all)
	}
	for _, want := range []string{
		"1/1 " + emptyCallMethod + ": ",
		"Unimplemented sent by the target",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
		}
	}
}

// A dry run is its own process: its request is the first record of the file,
// whatever the order the load would take.
// Ground: contract — "the first record in file order, whatever dataset_order and dataset_seed" (Spec 3).
func TestRun_DryRunTakesTheFirstRecord(t *testing.T) {
	target := startReflectingTarget(t)
	// With this seed a run's first request is record 2.
	dataset := writeDataset(t, lines(3))
	call := dryCall(unaryCallMethod, "      dataset: '"+dataset+"'\n      dataset_order: random\n      dataset_seed: 1\n")
	cfg := dryConfig(t, target.addr, "", call)

	res := runCLI(t.Context(), t, 3*time.Second, "-c", cfg, "-dry-run")
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	sent := target.recorder.allSimple()
	if len(sent) != 1 || sent[0].GetResponseSize() != 1 {
		t.Fatalf("the target got %v, want exactly record 1 ({responseSize:1})", sent)
	}
	if want := "  request (record 1 of 3): {\"response_size\":1}\n"; !strings.Contains(res.stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
	}
}

// The target is handed the secret and the screen never has it, wherever the
// target echoes it: the metadata line shows ${NAME}, and so do the reply and
// the message. Replaced in one pass, longest value first: the second variable's
// value is a piece of the token, and Go does not order a map.
// Ground: contract — #81, secrets never printed; the target's echo is the way around a metadata-only check.
func TestRun_DryRunNeverPrintsAnEnvSecret(t *testing.T) {
	t.Setenv("LT_DRY_TOKEN", "s3cr3t")
	t.Setenv("LT_DRY_SHARD", "cr3")
	// "$${...}" is not a reference: it is sent as the text "${LT_DRY_HOME}" and
	// its variable stays out of the replacement.
	t.Setenv("LT_DRY_HOME", "/home/acme")

	addr, echo := startEchoTarget(t, "/home/acme")
	app := "  metadata:\n    authorization: Bearer ${LT_DRY_TOKEN}\n    x-shard: ${LT_DRY_SHARD}\n    x-note: $${LT_DRY_HOME}\n"
	cfg := dryConfig(t, addr, app, dryCall(unaryCallMethod, "")+dryCall(emptyCallMethod, ""))

	const runs = 16
	for i := range runs {
		res := runCLI(t.Context(), t, 3*time.Second, "-c", cfg, "-dry-run")

		if got := exitCode(res.err); got != 1 {
			t.Fatalf("run %d: exit code %d, want 1 (EmptyCall is refused)\nerr %v\nstdout:\n%s", i, got, res.err, res.stdout)
		}

		all := res.stdout + res.stderr + res.err.Error()
		if strings.Contains(all, "s3cr3t") {
			t.Fatalf("run %d: the secret is on the screen:\n%s", i, all)
		}

		for _, want := range []string{
			"  response: {\"username\":\"Bearer ${LT_DRY_TOKEN} /home/acme\"}\n",
			"  message: invalid token \"Bearer ${LT_DRY_TOKEN}\"\n",
			"authorization: Bearer ${LT_DRY_TOKEN}",
		} {
			if !strings.Contains(res.stdout, want) {
				t.Fatalf("run %d: stdout lacks %q:\n%s", i, want, res.stdout)
			}
		}
	}

	got := echo.received()
	if len(got) != 2*runs {
		t.Fatalf("the target got %d calls, want %d", len(got), 2*runs)
	}
	for _, auth := range got {
		if auth != "Bearer s3cr3t" {
			t.Fatalf("the target got authorization %q, want the expanded Bearer s3cr3t", auth)
		}
	}
}

// What a run refuses before the network, a dry run refuses with the same text:
// a CI step «dry run, then load» must not pass the first and fail the second.
// Ground: contract — the checks of engineOptions (dataset fit, in-flight budget) run before the network.
func TestRun_DryRunRefusesWhatTheRunRefuses(t *testing.T) {
	dataset := writeDataset(t, lines(3))

	for _, tc := range []struct {
		name  string
		calls string
		says  string
	}{
		{
			"a file the run would use up",
			dryCall(unaryCallMethod, "      dataset: '"+dataset+"'\n      dataset_reuse: false\n"),
			"has 3 records",
		},
		{
			"a cap the plan outgrows",
			"    - method: " + unaryCallMethod + "\n      rps: 10000\n      duration: 1s\n      timeout: 1s\n",
			"max-in-flight",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := startReflectingTarget(t)
			cfg := dryConfig(t, target.addr, "", tc.calls)

			plain := runCLI(t.Context(), t, 5*time.Second, "-c", cfg)
			if exitCode(plain.err) != 1 || !strings.Contains(plain.err.Error(), tc.says) {
				t.Fatalf("without the flag: exit %d, err %v: the premise of the test is a run that refuses", exitCode(plain.err), plain.err)
			}

			dry := runCLI(t.Context(), t, 5*time.Second, "-c", cfg, "-dry-run")
			if got := exitCode(dry.err); got != 1 {
				t.Fatalf("dry run: exit code %d, want 1\nerr %v\nstdout:\n%s", got, dry.err, dry.stdout)
			}
			if dry.err.Error() != plain.err.Error() {
				t.Errorf("dry run says:\n%v\nthe run says:\n%v", dry.err, plain.err)
			}
			if n := len(target.recorder.allSimple()); n != 0 || target.health.calls.Load() != 0 {
				t.Errorf("the target got requests from a dry run that was refused")
			}
		})
	}

	// The cap is the user's to raise: the same plan passes with room.
	t.Run("the cap raised", func(t *testing.T) {
		target := startReflectingTarget(t)
		cfg := dryConfig(t, target.addr, "", "    - method: "+unaryCallMethod+"\n      rps: 10000\n      duration: 1s\n      timeout: 1s\n")

		res := runCLI(t.Context(), t, 5*time.Second, "-c", cfg, "-dry-run", "-max-in-flight", "20000")
		if res.err != nil {
			t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
		}
		if n := len(target.recorder.allSimple()); n != 1 {
			t.Errorf("the target got %d requests, want 1", n)
		}
	})
}

// A flag that would change nothing is refused, as -fake-delay is without
// -fake: nothing would say the script's -output json was ignored. -c,
// -connect-timeout and -max-in-flight do apply.
// Ground: contract — checkFakeFlags' rule: «would change nothing, and nothing would say so».
func TestRun_DryRunRefusesFlagsThatChangeNothing(t *testing.T) {
	const noReport = ": a dry run sends one request per call and writes no report"

	for _, tc := range []struct {
		flag string
		args []string
		says string
	}{
		{"-fake", []string{"-fake"}, "-fake with -dry-run: the fake target has no replies to show"},
		{"-fake-delay", []string{"-fake-delay", "1ms"}, "-fake-delay with -dry-run" + noReport},
		{"-fake-jitter", []string{"-fake-jitter", "1ms"}, "-fake-jitter with -dry-run" + noReport},
		{"-fake-fail-ratio", []string{"-fake-fail-ratio", "0.5"}, "-fake-fail-ratio with -dry-run" + noReport},
		{"-output json", []string{"-output", "json"}, "-output with -dry-run" + noReport},
		{"-output text", []string{"-output", "text"}, "-output with -dry-run" + noReport},
		{"-json-file", []string{"-json-file", filepath.Join(t.TempDir(), "run.json")}, "-json-file with -dry-run" + noReport},
		{"-label", []string{"-label", "env=ci"}, "-label with -dry-run" + noReport},
		{"-plain", []string{"-plain"}, "-plain with -dry-run" + noReport},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			target := startReflectingTarget(t)
			cfg := dryConfig(t, target.addr, "", dryCall(checkMethod, ""))

			res := runCLI(t.Context(), t, 3*time.Second, append([]string{"-c", cfg, "-dry-run"}, tc.args...)...)

			if got := exitCode(res.err); got != 1 {
				t.Fatalf("exit code %d, want 1\nerr %v\nstdout:\n%s", got, res.err, res.stdout)
			}
			if !strings.Contains(res.err.Error(), tc.says) {
				t.Errorf("error %q does not say %q", res.err, tc.says)
			}
			if res.stdout != "" {
				t.Errorf("a refused dry run printed:\n%s", res.stdout)
			}
			if n := target.health.calls.Load(); n != 0 {
				t.Errorf("the target got %d requests", n)
			}
			if tc.flag == "-json-file" {
				if _, err := os.Stat(tc.args[1]); err == nil {
					t.Errorf("%s was created by a refused dry run", tc.args[1])
				}
			}
		})
	}

	for _, args := range [][]string{{"-connect-timeout", "5s"}, {"-max-in-flight", "1000"}} {
		t.Run("accepts "+args[0], func(t *testing.T) {
			target := startReflectingTarget(t)
			cfg := dryConfig(t, target.addr, "", dryCall(checkMethod, ""))

			res := runCLI(t.Context(), t, 3*time.Second, append([]string{"-c", cfg, "-dry-run"}, args...)...)

			if res.err != nil {
				t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
			}
			if n := target.health.calls.Load(); n != 1 {
				t.Errorf("the target got %d requests, want 1", n)
			}
		})
	}
}

// A config with a search is a config: each call gets its one request and the
// search is not run.
// Ground: contract — "a load.breakpoint config is accepted, the search is not run" (Spec 1).
func TestRun_DryRunOnASearchConfig(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := writeSearch(t, target.addr, checkMethod)

	res := runCLI(t.Context(), t, 5*time.Second, "-c", cfg, "-dry-run")

	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}
	if n := target.health.calls.Load(); n != 1 {
		t.Errorf("the target got %d requests, want 1", n)
	}
	if !strings.Contains(res.stdout, "1/1 "+checkMethod+": success, OK, ") {
		t.Errorf("no block for the call:\n%s", res.stdout)
	}
	if all := res.stdout + res.stderr; strings.Contains(all, "breakpoint: up to") || strings.Contains(all, "run finished") {
		t.Errorf("the search ran:\n%s", all)
	}
}

// Ctrl+C in the middle of the loop: the call in flight is cut, the next is not
// sent, the stderr says how far it got, and the exit is 1 (no report as asked).
// K is the number of blocks printed, and this call printed none; neither the
// verdict line nor the metadata line comes.
// Ground: contract — Ctrl+C during a dry run (stops, not a SIGTERM) ends the loop; the call in flight blocks 5 s.
func TestRun_DryRunStopsOnCtrlC(t *testing.T) {
	target := startSlowTarget(t, 5*time.Second)
	cfg := dryConfig(t, target.addr, "  metadata:\n    x-tenant: acme\n", dryCall(checkMethod, "")+dryCall(unaryCallMethod, ""))

	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	stops := make(chan struct{})
	var stdout, stderr bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- run(t.Context(), stops, nil, []string{"-c", cfg, "-dry-run"}, &stdout, &stderr)
	}()

	deadline := time.After(5 * time.Second)
	for target.health.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("the first request never reached the target")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// The reader of stops lives only until run returns: a run that returned
	// before the press must fail the test, not hang it.
	pressed := time.Now()

	var err error

	select {
	case stops <- struct{}{}:
	case err = <-done:
		t.Fatalf("run returned before Ctrl+C: %v", err)
	case <-time.After(time.Second):
		t.Fatal("Ctrl+C was not taken within a second")
	}

	select {
	case err = <-done:
	case <-time.After(time.Second):
		t.Fatal("the dry run did not stop within a second of Ctrl+C")
	}
	t.Logf("stopped %v after the press", time.Since(pressed))

	if got := exitCode(err); got != 1 {
		t.Errorf("exit code %d, want 1\nerr %v", got, err)
	}
	if err == nil || !strings.Contains(err.Error()+stderr.String(), "dry run: stopped after 0 of 2 calls") {
		t.Errorf("no stop line:\nerr %v\nstderr:\n%s", err, stderr.String())
	}
	if all := stdout.String() + stderr.String(); strings.Contains(all, "did not succeed") || strings.Contains(all, "metadata:") {
		t.Errorf("a stopped dry run printed the end of a finished one:\n%s", all)
	}
	if n := len(target.recorder.allSimple()); n != 0 {
		t.Errorf("the second call reached the target %d times after Ctrl+C", n)
	}
}
