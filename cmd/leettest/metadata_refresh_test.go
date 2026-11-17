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
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
)

const (
	mrLastError = `key "authorization": file token.txt: is empty`
	mrStderr    = "metadata refresh failed: " + mrLastError +
		"; the last good value stays in use (the report counts every failed read)"
)

// mrTarget is a plaintext health service that accepts any token and keeps the
// authorization of every call, in the order the calls came.
type mrTarget struct {
	addr string

	mu   sync.Mutex
	seen []string
}

func startMRTarget(t *testing.T) *mrTarget {
	t.Helper()

	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	target := &mrTarget{addr: lis.Addr().String()}

	srv := grpc.NewServer(grpc.UnaryInterceptor(
		func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			md, _ := metadata.FromIncomingContext(ctx)

			target.mu.Lock()
			target.seen = append(target.seen, strings.Join(md.Get("authorization"), ","))
			target.mu.Unlock()

			return h(ctx, req)
		}))
	grpc_health_v1.RegisterHealthServer(srv, &health{})

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return target
}

func (m *mrTarget) tokens() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]string(nil), m.seen...)
}

// mrWorkDir is a directory that is the working directory of the test, holding
// token.txt with value: a relative ${file:token.txt} is read from there.
func mrWorkDir(t *testing.T, value string) string {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

// mrConfig writes a plaintext config of one call of the given duration, with
// the app lines after app.tls.
func mrConfig(t *testing.T, addr, appLines, duration string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}

	cfg := "app:\n  target:\n    ip: " + host + "\n    port: " + port + "\n  tls: false\n" + appLines +
		"load:\n  calls:\n    - method: " + checkMethod + "\n      rps: 50\n      duration: " + duration + "\n"

	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err = os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

const mrFileApp = "  metadata:\n    authorization: '${file:token.txt}'\n"

// mrReplace puts value in path after delay, whole: the new file is written
// beside it and renamed over it, so a read never meets half of it. Windows
// refuses a rename over a file that is open, hence the retries.
func mrReplace(t *testing.T, path string, delay time.Duration, value string) (done <-chan struct{}) {
	t.Helper()

	finished := make(chan struct{})

	go func() {
		defer close(finished)

		time.Sleep(delay)

		tmp := path + ".new"
		if err := os.WriteFile(tmp, []byte(value), 0o600); err != nil {
			t.Errorf("write %s: %v", tmp, err)

			return
		}

		var err error
		for range 50 {
			if err = os.Rename(tmp, path); err == nil {
				return
			}

			time.Sleep(10 * time.Millisecond)
		}

		t.Errorf("rename over %s: %v", path, err)
	}()

	return finished
}

// Ground: contract (spec §6, C1) — a header that reads a file reaches the
// target with the file's value, the line break at the end cut. Without refresh
// the report has no refresh line and the value is never printed.
func TestMetadataFile_ARunSendsTheValueOfTheFile(t *testing.T) {
	mrWorkDir(t, "tok-one\n")
	target := startMRTarget(t)

	res := runCLI(t.Context(), t, 10*time.Second, "-c", mrConfig(t, target.addr, mrFileApp, "300ms"))
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	got := target.tokens()
	if len(got) == 0 {
		t.Fatal("the target received no call")
	}

	for i, tok := range got {
		if tok != "tok-one" {
			t.Fatalf("call %d carried authorization %q, want the value of the file, tok-one", i, tok)
		}
	}

	if all := res.stdout + res.stderr; strings.Contains(all, "tok-one") {
		t.Errorf("the value of the file is on the screen:\n%s", all)
	}
}

// Ground: contract (spec §6, C1) — a value changed in the file after the start
// reaches the target within the interval, in order: once the new value has
// come, the old one does not. The report counts the change.
func TestMetadataRefresh_ANewValueReachesTheTargetDuringTheRun(t *testing.T) {
	dir := mrWorkDir(t, "tok-one\n")
	target := startMRTarget(t)
	cfg := mrConfig(t, target.addr, mrFileApp+"  metadata_refresh: 1s\n", "3500ms")

	done := mrReplace(t, filepath.Join(dir, "token.txt"), 1300*time.Millisecond, "tok-two\n")

	res := runCLI(t.Context(), t, 20*time.Second, "-c", cfg)
	<-done

	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	got := target.tokens()
	if len(got) == 0 {
		t.Fatal("the target received no call")
	}

	if got[0] != "tok-one" {
		t.Errorf("the first call carried %q, want tok-one", got[0])
	}

	if last := got[len(got)-1]; last != "tok-two" {
		t.Errorf("the last call carried %q, want tok-two: the new value did not arrive within the run", last)
	}

	seenNew := false

	for i, tok := range got {
		switch tok {
		case "tok-two":
			seenNew = true
		case "tok-one":
			if seenNew {
				t.Fatalf("call %d carried tok-one after tok-two had come", i)
			}
		default:
			t.Fatalf("call %d carried %q", i, tok)
		}
	}

	line := regexp.MustCompile(`(?m)^metadata refresh: every 1s, \d+ reads, 1 changed, 0 failed$`)
	if !line.MatchString(res.stdout) {
		t.Errorf("no refresh line with one change in the report:\n%s", res.stdout)
	}

	if strings.Contains(res.stdout+res.stderr, "tok-") {
		t.Errorf("a value is on the screen:\n%s\n%s", res.stdout, res.stderr)
	}
}

// Ground: contract (spec §4.1, §6.4, C3) — a read that fails keeps the last good
// value in use, the stderr line is one, at the first failure, with the last
// error, and the report says the refresh was failing at the end.
// Mutation M25 (a stderr line per failed read) dies on the count.
func TestMetadataRefresh_AFailedReadIsOneLineOnStderrAndInTheReport(t *testing.T) {
	dir := mrWorkDir(t, "tok-one\n")
	target := startMRTarget(t)
	cfg := mrConfig(t, target.addr, mrFileApp+"  metadata_refresh: 1s\n", "5s")

	go func() {
		time.Sleep(1300 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "token.txt"), nil, 0o600)
	}()

	res := runCLI(t.Context(), t, 20*time.Second, "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	var lines []string

	for l := range strings.SplitSeq(res.stderr, "\n") {
		if strings.HasPrefix(l, "metadata refresh failed: ") {
			lines = append(lines, l)
		}
	}

	if len(lines) != 1 || lines[0] != mrStderr {
		t.Errorf("stderr lines %q, want exactly one: %q\nstderr:\n%s", lines, mrStderr, res.stderr)
	}

	report := regexp.MustCompile(`(?m)^metadata refresh: every 1s, \d+ reads, 0 changed, \d+ failed \(failing at the end\); ` +
		`the last good value stayed in use; last error: ` + regexp.QuoteMeta(mrLastError) + `$`)
	if !report.MatchString(res.stdout) {
		t.Errorf("no refresh line with the failure in the report:\n%s", res.stdout)
	}

	for i, tok := range target.tokens() {
		if tok != "tok-one" {
			t.Fatalf("call %d carried %q, want the last good value tok-one", i, tok)
		}
	}
}

// Ground: contract (spec §5.2, §6.5) — -output json keeps stdout one JSON
// document with the counters in it; the interval is in microseconds.
func TestMetadataRefresh_JSONCarriesTheCounters(t *testing.T) {
	mrWorkDir(t, "tok-one\n")
	target := startMRTarget(t)
	cfg := mrConfig(t, target.addr, mrFileApp+"  metadata_refresh: 1s\n", "1500ms")

	res := runCLI(t.Context(), t, 15*time.Second, "-c", cfg, "-output", "json")
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, res.stdout)
	}

	obj, ok := doc["metadata_refresh"].(map[string]any)
	if !ok {
		t.Fatalf("metadata_refresh = %v, want an object", doc["metadata_refresh"])
	}

	if obj["interval_us"] != 1_000_000.0 {
		t.Errorf("interval_us = %v, want 1000000", obj["interval_us"])
	}

	if reads, _ := obj["reads"].(float64); reads < 1 {
		t.Errorf("reads = %v, want at least one in 1.5 s at a 1 s interval", obj["reads"])
	}

	if obj["failures"] != 0.0 || obj["failing_at_end"] != false || obj["last_error"] != nil {
		t.Errorf("a refresh with no failure: %v", obj)
	}
}

// Ground: contract (spec §5, C6) — refresh off: no line in the text report and
// the key is null in the JSON; a file in the header changes neither.
// Mutation M26 (a line when off) dies on the text half.
func TestMetadataRefresh_OffAddsNothingToTheReport(t *testing.T) {
	mrWorkDir(t, "tok-one\n")
	target := startMRTarget(t)

	text := runCLI(t.Context(), t, 10*time.Second, "-c", mrConfig(t, target.addr, mrFileApp, "300ms"))
	if text.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", text.err, text.stderr)
	}

	if strings.Contains(text.stdout, "metadata refresh") {
		t.Errorf("a refresh line with refresh off:\n%s", text.stdout)
	}

	js := runCLI(t.Context(), t, 10*time.Second, "-c", mrConfig(t, target.addr, mrFileApp, "300ms"), "-output", "json")
	if js.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", js.err, js.stderr)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(js.stdout), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v", err)
	}

	if v, present := doc["metadata_refresh"]; !present || v != nil {
		t.Errorf("metadata_refresh = %v (present %v), want the key there and null", v, present)
	}

	if strings.Contains(text.stderr+js.stderr, "metadata refresh") {
		t.Errorf("a refresh line on stderr with refresh off:\n%s\n%s", text.stderr, js.stderr)
	}
}

// Ground: contract (spec §4.1, §6.2, C2) — a file that cannot be read is a
// config error before any connection: exit 1, ErrInvalidMetadata, the key, the
// file and the reason, never what the file holds, never a byte position.
// Mutation M24 (the content or the position in the error) dies on the secret.
func TestMetadataFile_AnUnreadableFileFailsBeforeConnecting(t *testing.T) {
	cases := []struct {
		name    string
		content *string
		reason  string
	}{
		{"missing", nil, ""},
		{"empty", ptr(""), "is empty"},
		{"a line break inside", ptr(secretMark + "\nsecond"), "has a line break inside the value (one at the end is cut)"},
		{"a byte outside printable ASCII", ptr(secretMark + "\u00e9"), "has a byte outside printable ASCII (%x20-%x7E)"},
		{"larger than 64 KiB", ptr(strings.Repeat("a", 64*1024+1)), "is larger than 64 KiB"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := mrWorkDir(t, "")
			if c.content == nil {
				if err := os.Remove(filepath.Join(dir, "token.txt")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte(*c.content), 0o600); err != nil {
				t.Fatal(err)
			}

			res := runCLI(t.Context(), t, 5*time.Second, "-c", mrConfig(t, closedPort(t), mrFileApp, "300ms"))
			if res.err == nil {
				t.Fatal("run succeeded")
			}

			if code := exitCode(res.err); code != 1 {
				t.Errorf("exit code %d, want 1", code)
			}

			if !errors.Is(res.err, config.ErrInvalidMetadata) {
				t.Fatalf("err = %v, want ErrInvalidMetadata", res.err)
			}

			want := `key "authorization": file token.txt: ` + c.reason
			if !strings.Contains(res.err.Error(), want) {
				t.Errorf("err = %q, want it to hold %q", res.err, want)
			}

			if strings.Contains(res.err.Error()+res.stdout+res.stderr, secretMark) {
				t.Errorf("the content of the file is in the output: %v", res.err)
			}

			if strings.Contains(res.stderr, "connecting to") {
				t.Errorf("it tried to connect first:\n%s", res.stderr)
			}
		})
	}
}

func ptr(s string) *string { return &s }

// Ground: contract (spec §6.1) — senderOptions reads the files: the first value
// is Options.Metadata, the values come back for the redactor, and with refresh
// the options carry a Read that gives the whole map again.
func TestSenderOptions_AFileIsReadAndRereadOnRefresh(t *testing.T) {
	dir := mrWorkDir(t, "tok-one\n")

	cfg, err := config.Parse([]byte("app:\n  target:\n    ip: localhost\n    port: 50051\n" +
		"  metadata:\n    x-static: plain\n    authorization: '${file:token.txt}'\n  metadata_refresh: 1s\n" +
		"load:\n  calls:\n    - method: a.B/C\n      rps: 1\n      duration: 1s\n"))
	if err != nil {
		t.Fatal(err)
	}

	opts, files, err := senderOptions(&cfg.App, false)
	if err != nil {
		t.Fatalf("senderOptions: %v", err)
	}

	if got := opts.Metadata["authorization"]; got != "tok-one" || opts.Metadata["x-static"] != "plain" {
		t.Errorf("Options.Metadata = %v, want authorization tok-one and x-static plain", opts.Metadata)
	}

	if len(files) != 1 || files[0] != (config.FileValue{Path: "token.txt", Value: "tok-one"}) {
		t.Errorf("files = %v, want token.txt holding tok-one", files)
	}

	if opts.MetadataRefresh == nil || opts.MetadataRefresh.Every != time.Second || opts.MetadataRefresh.Read == nil {
		t.Fatalf("Options.MetadataRefresh = %+v, want Every 1s and a Read", opts.MetadataRefresh)
	}

	if err = os.WriteFile(filepath.Join(dir, "token.txt"), []byte("tok-two"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := opts.MetadataRefresh.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got["authorization"] != "tok-two" || got["x-static"] != "plain" || len(got) != 2 {
		t.Errorf("Read = %v, want the whole map with the new value", got)
	}

	if err = os.WriteFile(filepath.Join(dir, "token.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = opts.MetadataRefresh.Read()
	if err == nil || err.Error() != mrLastError {
		t.Errorf("Read of an empty file: err = %v, want exactly %q: the key, the file, the reason", err, mrLastError)
	}
}

// Ground: contract (spec §6.1, M27) — a dry run reads the files once, hands the
// values back for the redactor, and starts no refresh. Without refresh in the
// config there is none either.
func TestSenderOptions_ADryRunAndAnOffConfigHaveNoRefresh(t *testing.T) {
	mrWorkDir(t, "tok-one\n")

	parse := func(refresh string) *config.MasterConfig {
		cfg, err := config.Parse([]byte("app:\n  target:\n    ip: localhost\n    port: 50051\n" +
			"  metadata:\n    authorization: '${file:token.txt}'\n" + refresh +
			"load:\n  calls:\n    - method: a.B/C\n      rps: 1\n      duration: 1s\n"))
		if err != nil {
			t.Fatal(err)
		}

		return cfg
	}

	withRefresh := parse("  metadata_refresh: 1s\n")

	opts, files, err := senderOptions(&withRefresh.App, true)
	if err != nil {
		t.Fatal(err)
	}

	if opts.MetadataRefresh != nil {
		t.Errorf("a dry run carries a refresh: %+v", opts.MetadataRefresh)
	}

	if opts.Metadata["authorization"] != "tok-one" || len(files) != 1 {
		t.Errorf("a dry run: Metadata %v, files %v, want the value read once and returned", opts.Metadata, files)
	}

	off := parse("")

	opts, _, err = senderOptions(&off.App, false)
	if err != nil {
		t.Fatal(err)
	}

	if opts.MetadataRefresh != nil {
		t.Errorf("a config without metadata_refresh carries a refresh: %+v", opts.MetadataRefresh)
	}

	if opts.Metadata["authorization"] != "tok-one" {
		t.Errorf("Metadata = %v, want the file's value as the only one", opts.Metadata)
	}
}

// Ground: contract (spec §6.1) — buildSender hands the sender the refresh and
// the files; a dry run's sender has none, a fake has no gRPC sender at all.
func TestBuildSender_TheRefreshFollowsTheMode(t *testing.T) {
	mrWorkDir(t, "tok-one\n")

	cfg, err := config.Parse([]byte("app:\n  target:\n    ip: localhost\n    port: 50051\n" +
		"  metadata:\n    authorization: '${file:token.txt}'\n  metadata_refresh: 1s\n" +
		"load:\n  calls:\n    - method: a.B/C\n      rps: 1\n      duration: 1s\n"))
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name        string
		flags       runFlags
		wantRefresh bool
		wantFiles   int
		wantSender  bool
	}{
		{"real", runFlags{}, true, 1, true},
		{"dry run", runFlags{dryRun: true}, false, 1, true},
		{"fake", runFlags{fake: true}, false, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, gs, _, files, buildErr := buildSender(cfg, c.flags)
			if buildErr != nil {
				t.Fatal(buildErr)
			}

			if (gs != nil) != c.wantSender {
				t.Fatalf("gRPC sender present = %v, want %v", gs != nil, c.wantSender)
			}

			if len(files) != c.wantFiles {
				t.Errorf("files = %v, want %d", files, c.wantFiles)
			}

			if gs == nil {
				return
			}

			defer func() { _ = gs.Close() }()

			if _, ok := gs.MetadataRefresh(); ok != c.wantRefresh {
				t.Errorf("the sender refreshes = %v, want %v", ok, c.wantRefresh)
			}
		})
	}
}

// Ground: contract (spec §4.2, C5) — a dry run hands the target the value of the
// file and shows it nowhere: the metadata line, the reply and the error message
// show ${file:PATH}, the prefix "Bearer " left as it is. A refresh set in the
// config is not started: the run returns at once and says nothing about it.
// Mutation M23 (a redactor without the file values) and M27 die here.
func TestRun_DryRunNeverPrintsTheValueOfAFile(t *testing.T) {
	mrWorkDir(t, "f1le-s3cr3t\n")

	addr, echo := startEchoTarget(t, "")
	app := "  metadata:\n    authorization: Bearer ${file:token.txt}\n  metadata_refresh: 1s\n"
	cfg := dryConfig(t, addr, app, dryCall(unaryCallMethod, "")+dryCall(emptyCallMethod, ""))

	res := runCLI(t.Context(), t, 5*time.Second, "-c", cfg, "-dry-run")
	if got := exitCode(res.err); got != 1 {
		t.Fatalf("exit code %d, want 1 (EmptyCall is refused)\nerr %v\nstdout:\n%s", got, res.err, res.stdout)
	}

	all := res.stdout + res.stderr + res.err.Error()
	if strings.Contains(all, "f1le-s3cr3t") {
		t.Fatalf("the value of the file is on the screen:\n%s", all)
	}

	for _, want := range []string{
		"  response: {\"username\":\"Bearer ${file:token.txt} \"}\n",
		"  message: invalid token \"Bearer ${file:token.txt}\"\n",
		"authorization: Bearer ${file:token.txt}",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, res.stdout)
		}
	}

	if strings.Contains(all, "metadata refresh") {
		t.Errorf("a dry run speaks of the refresh:\n%s", all)
	}

	for _, auth := range echo.received() {
		if auth != "Bearer f1le-s3cr3t" {
			t.Fatalf("the target got authorization %q, want Bearer f1le-s3cr3t", auth)
		}
	}

	if len(echo.received()) != 2 {
		t.Errorf("the target got %d calls, want 2", len(echo.received()))
	}
}

// syncLines collects what the watch delivers.
type syncLines struct {
	mu    sync.Mutex
	lines []string
}

func (s *syncLines) add(line string) {
	s.mu.Lock()
	s.lines = append(s.lines, line)
	s.mu.Unlock()
}

func (s *syncLines) get() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.lines...)
}

// scripted is a refreshSource that answers from a list, the last answer
// again once the list is spent, and counts its calls.
type scripted struct {
	mu    sync.Mutex
	stats []grpcsender.RefreshStats
	calls int
}

func (s *scripted) read() (grpcsender.RefreshStats, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	i := min(s.calls, len(s.stats)-1)
	s.calls++

	return s.stats[i], true
}

func (s *scripted) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.calls
}

// Ground: contract (spec §6.4, C3) — the watch hands deliver the line once, at
// the first failure, however many reads fail after it or whether the refresh
// recovers; and it is stopped when stop returns: the source is not asked again.
// Mutation M25 (a line per failure) dies on the count.
func TestRefreshWatch_DeliversTheFirstFailureOnce(t *testing.T) {
	src := &scripted{stats: []grpcsender.RefreshStats{
		{Reads: 1},
		{Reads: 2, Failures: 1, FailingAtEnd: true, LastError: mrLastError},
		{Reads: 3, Failures: 2, FailingAtEnd: true, LastError: "key \"authorization\": file token.txt: later"},
		{Reads: 4, Failures: 2, LastError: "key \"authorization\": file token.txt: later"},
	}}

	var got syncLines

	stop := startRefreshWatch(t.Context(), src.read, 5*time.Millisecond, got.add)

	deadline := time.Now().Add(3 * time.Second)
	for len(got.get()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	for src.count() < 8 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	stop()

	if lines := got.get(); len(lines) != 1 || lines[0] != mrStderr {
		t.Fatalf("delivered %q, want exactly one: %q (the first failure's error)", lines, mrStderr)
	}

	after := src.count()

	time.Sleep(60 * time.Millisecond)

	if now := src.count(); now != after {
		t.Errorf("the source was asked %d more times after stop returned", now-after)
	}
}

// Ground: contract (spec §6.4) — a source that says it does not refresh
// delivers nothing and the watch ends with stop.
func TestRefreshWatch_ASourceWithoutRefreshSaysNothing(t *testing.T) {
	var got syncLines

	stop := startRefreshWatch(t.Context(), func() (grpcsender.RefreshStats, bool) {
		return grpcsender.RefreshStats{Failures: 5, LastError: "x"}, false
	}, 5*time.Millisecond, got.add)

	time.Sleep(50 * time.Millisecond)
	stop()

	if lines := got.get(); len(lines) != 0 {
		t.Errorf("delivered %q for a source with no refresh", lines)
	}
}

// Ground: contract (spec §6.4) — the line goes to stderr at once without a live
// view, once however many arrive; with a live view it is held until the view
// has gone, then printed once; after the view has gone it is printed at once.
func TestRefreshNotice_WhenTheLineIsPrinted(t *testing.T) {
	const line = "metadata refresh failed: x; the last good value stays in use (the report counts every failed read)"

	t.Run("no live view", func(t *testing.T) {
		var stderr bytes.Buffer

		n := &refreshNotice{stderr: &stderr}
		n.deliver(line)
		n.deliver("a second one")

		if got := stderr.String(); got != line+"\n" {
			t.Errorf("stderr = %q, want the first line once", got)
		}

		n.afterView()

		if got := stderr.String(); got != line+"\n" {
			t.Errorf("stderr after afterView = %q, want it unchanged", got)
		}
	})

	t.Run("a live view holds it", func(t *testing.T) {
		var stderr bytes.Buffer

		n := &refreshNotice{stderr: &stderr, live: true}
		n.deliver(line)

		if got := stderr.String(); got != "" {
			t.Fatalf("stderr = %q while the view is up, want nothing: the view owns the screen", got)
		}

		n.afterView()
		n.afterView()

		if got := stderr.String(); got != line+"\n" {
			t.Errorf("stderr = %q after the view, want the line once", got)
		}
	})

	t.Run("a live view and no failure", func(t *testing.T) {
		var stderr bytes.Buffer

		n := &refreshNotice{stderr: &stderr, live: true}
		n.afterView()

		if got := stderr.String(); got != "" {
			t.Errorf("stderr = %q with no failure, want nothing", got)
		}
	})

	t.Run("after the view", func(t *testing.T) {
		var stderr bytes.Buffer

		n := &refreshNotice{stderr: &stderr, live: true}
		n.afterView()
		n.deliver(line)

		if got := stderr.String(); got != line+"\n" {
			t.Errorf("stderr = %q, want the line at once: the view is gone", got)
		}
	})
}
