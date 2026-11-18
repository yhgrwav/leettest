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
	"slices"
	"strconv"
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
// authorization of every call, in the order the calls came, with the time each
// came at.
type mrTarget struct {
	addr string

	mu   sync.Mutex
	seen []string
	at   []time.Time
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
			target.at = append(target.at, time.Now())
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

// arrivals are the times the calls of tokens() came at, in the same order.
func (m *mrTarget) arrivals() []time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]time.Time(nil), m.at...)
}

// mrDir is a directory holding token.txt with value, for a config written next
// to it: a relative ${file:token.txt} is read from the config's directory
// (spec §1.2). The working directory of the test is another, empty one, so a
// read from the cwd finds nothing.
func mrDir(t *testing.T, value string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())

	return dir
}

// mrWorkDir is mrDir with the test's working directory in it: config.Parse
// leaves a relative path as written, and senderOptions reads it from the cwd.
func mrWorkDir(t *testing.T, value string) string {
	t.Helper()

	dir := mrDir(t, value)
	t.Chdir(dir)

	return dir
}

// mrConfig writes a plaintext config of one call of the given duration into
// dir, next to the token file, with the app lines after app.tls.
func mrConfig(t *testing.T, dir, addr, appLines, duration string) string {
	t.Helper()

	return mrConfigWith(t, dir, addr, appLines, duration, "")
}

// mrConfigWith is mrConfig with top-level lines after the calls.
func mrConfigWith(t *testing.T, dir, addr, appLines, duration, topLines string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}

	cfg := "app:\n  target:\n    ip: " + host + "\n    port: " + port + "\n  tls: false\n" + appLines +
		"load:\n  calls:\n    - method: " + checkMethod + "\n      rps: 50\n      duration: " + duration + "\n" + topLines

	path := filepath.Join(dir, "leettest.yaml")
	if err = os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// mrStaleWindow is how long after the first call with a new value a call with
// the old one may still reach the target, taken from the run itself and not
// guessed: the new value is published before any call that carries it takes it,
// so a call that carries the old one took it earlier and was then in flight, at
// the longest, for the slowest call of the run. maxLatency is the report's
// latency.max, counted from the call's moment, so it covers the wait before the
// call took its header as well; margin covers what the report rounds: 1% of the
// value (the histogram's resolution), the clock step both ways, 1 ms. A quiet
// machine gives a few milliseconds, a loaded one more, and the window with it.
func mrStaleWindow(maxLatency, clockStep time.Duration) time.Duration {
	return maxLatency + maxLatency/100 + 2*clockStep + time.Millisecond
}

// mrRunFacts are the two numbers of a JSON report the stale window comes from,
// and the refresh counters.
type mrRunFacts struct {
	maxLatency time.Duration
	// maxIsBound is true when the slowest call did not finish, so that its
	// latency is not the longest a call stayed in flight.
	maxIsBound bool
	clockStep  time.Duration
	refresh    map[string]any
}

func mrReadFacts(t *testing.T, stdout string) mrRunFacts {
	t.Helper()

	var doc struct {
		ClockStepNS     int64          `json:"clock_step_ns"`
		MetadataRefresh map[string]any `json:"metadata_refresh"`
		Methods         []struct {
			Latency struct {
				Max *struct {
					NS         int64 `json:"ns"`
					LowerBound bool  `json:"lower_bound"`
				} `json:"max"`
			} `json:"latency"`
		} `json:"methods"`
	}

	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}

	if len(doc.Methods) != 1 || doc.Methods[0].Latency.Max == nil {
		t.Fatalf("want one method with a latency.max in the report:\n%s", stdout)
	}

	slowest := doc.Methods[0].Latency.Max

	return mrRunFacts{
		maxLatency: time.Duration(slowest.NS),
		maxIsBound: slowest.LowerBound,
		clockStep:  time.Duration(doc.ClockStepNS),
		refresh:    doc.MetadataRefresh,
	}
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
	dir := mrDir(t, "tok-one\n")
	target := startMRTarget(t)

	res := runCLI(t.Context(), t, 10*time.Second, "-c", mrConfig(t, dir, target.addr, mrFileApp, "300ms"))
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
// reaches the target within the interval and stays: once the new value has come,
// the old one does not come back (calls already in flight at the swap aside).
// The report counts the change.
//
// The test is about what the target receives, not about how well the generator
// keeps its schedule: on a machine loaded by other tests (a full `make check`)
// the run ends as invalid (generator_late, exit 2) with the report printed, and
// that is the product being right. Only an invalid run is let through, as in the
// failed-read tests below; any other error still fails the test.
func TestMetadataRefresh_ANewValueReachesTheTargetDuringTheRun(t *testing.T) {
	dir := mrDir(t, "tok-one\n")
	target := startMRTarget(t)
	cfg := mrConfig(t, dir, target.addr, mrFileApp+"  metadata_refresh: 1s\n", "3500ms")

	done := mrReplace(t, filepath.Join(dir, "token.txt"), 1300*time.Millisecond, "tok-two\n")

	res := runCLI(t.Context(), t, 20*time.Second, "-c", cfg, "-output", "json")
	<-done

	if res.err != nil && !errors.Is(res.err, ErrInvalidRun) {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	facts := mrReadFacts(t, res.stdout)

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

	// Calls overlap, so a call that took the old value just before the swap can
	// reach the target after one that took the new. That lasts as long as the
	// slowest call of the run stayed in flight (mrStaleWindow), no longer: an old
	// value after that is a value that came back.
	arrivals := target.arrivals()
	firstNew := slices.Index(got, "tok-two")
	window := mrStaleWindow(facts.maxLatency, facts.clockStep)
	t.Logf("latency.max %v, clock step %v: an old value is let through for %v after the first new one", facts.maxLatency, facts.clockStep, window)

	for i, tok := range got {
		switch tok {
		case "tok-two":
		case "tok-one":
			if firstNew < 0 || i < firstNew || facts.maxIsBound {
				continue
			}

			if late := arrivals[i].Sub(arrivals[firstNew]); late > window {
				t.Fatalf("call %d carried tok-one %v after tok-two had come, longer than the slowest call of the run "+
					"stayed in flight (latency.max %v, window %v)", i, late, facts.maxLatency, window)
			}
		default:
			t.Fatalf("call %d carried %q", i, tok)
		}
	}

	if facts.maxIsBound {
		t.Logf("the slowest call did not finish (latency.max %v is a lower bound): the order of old and new values is not judged", facts.maxLatency)
	}

	if facts.refresh["changed"] != 1.0 || facts.refresh["failures"] != 0.0 || facts.refresh["interval_us"] != 1_000_000.0 {
		t.Errorf("metadata_refresh in the report = %v, want every 1 s, 1 changed, 0 failed", facts.refresh)
	}

	if strings.Contains(res.stdout+res.stderr, "tok-") {
		t.Errorf("a value is on the screen:\n%s\n%s", res.stdout, res.stderr)
	}
}

// Ground: contract (spec §5) — the text report carries the refresh line
// with the change counted. The test above reads the counters from the JSON
// report, which also has the latency the stale window comes from; this one keeps
// the line of the text report that it used to check. An invalid run is let
// through for the reason given there.
func TestMetadataRefresh_TheTextReportCountsTheChange(t *testing.T) {
	dir := mrDir(t, "tok-one\n")
	target := startMRTarget(t)
	cfg := mrConfig(t, dir, target.addr, mrFileApp+"  metadata_refresh: 1s\n", "2500ms")

	done := mrReplace(t, filepath.Join(dir, "token.txt"), 800*time.Millisecond, "tok-two\n")

	res := runCLI(t.Context(), t, 20*time.Second, "-c", cfg)
	<-done

	if res.err != nil && !errors.Is(res.err, ErrInvalidRun) {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	line := regexp.MustCompile(`(?m)^metadata refresh: every 1s, \d+ reads, 1 changed, 0 failed$`)
	if !line.MatchString(res.stdout) {
		t.Errorf("no refresh line with one change in the report:\n%s", res.stdout)
	}
}

// Ground: contract (spec §4.1, §6.4, C3) — a read that fails keeps the last good
// value in use, the stderr line is one, at the first failure, with the last
// error, and the report says the refresh was failing at the end.
// Mutation M25 (a stderr line per failed read) dies on the count.
func TestMetadataRefresh_AFailedReadIsOneLineOnStderrAndInTheReport(t *testing.T) {
	dir := mrDir(t, "tok-one\n")
	target := startMRTarget(t)
	cfg := mrConfig(t, dir, target.addr, mrFileApp+"  metadata_refresh: 1s\n", "5s")

	go func() {
		time.Sleep(1300 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "token.txt"), nil, 0o600)
	}()

	res := runCLI(t.Context(), t, 20*time.Second, "-c", cfg)
	if res.err != nil && !errors.Is(res.err, ErrInvalidRun) {
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

	report := regexp.MustCompile(`(?m)^metadata refresh: every 1s, \d+ reads, 0 changed, (\d+) failed \(failing at the end\); ` +
		`the last good value stayed in use; last error: ` + regexp.QuoteMeta(mrLastError) + `$`)

	m := report.FindStringSubmatch(res.stdout)
	if m == nil {
		t.Fatalf("no refresh line with the failure in the report:\n%s", res.stdout)
	}

	// The file stays empty for 3 or more reads: the report counts each of them
	// while stderr said it once.
	if failed, _ := strconv.Atoi(m[1]); failed < 2 {
		t.Errorf("the report counts %d failed reads, want at least 2: one line on stderr must stand for several", failed)
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
	dir := mrDir(t, "tok-one\n")
	target := startMRTarget(t)
	cfg := mrConfig(t, dir, target.addr, mrFileApp+"  metadata_refresh: 1s\n", "1500ms")

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
	dir := mrDir(t, "tok-one\n")
	target := startMRTarget(t)

	text := runCLI(t.Context(), t, 10*time.Second, "-c", mrConfig(t, dir, target.addr, mrFileApp, "300ms"))
	if text.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", text.err, text.stderr)
	}

	if strings.Contains(text.stdout, "metadata refresh") {
		t.Errorf("a refresh line with refresh off:\n%s", text.stdout)
	}

	js := runCLI(t.Context(), t, 10*time.Second, "-c", mrConfig(t, dir, target.addr, mrFileApp, "300ms"), "-output", "json")
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
// Mutation M24 (the content or the position in the error) dies on the secret and
// on the end of the message: the reason is the last thing it says.
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
			dir := mrDir(t, "")
			if c.content == nil {
				if err := os.Remove(filepath.Join(dir, "token.txt")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte(*c.content), 0o600); err != nil {
				t.Fatal(err)
			}

			res := runCLI(t.Context(), t, 5*time.Second, "-c", mrConfig(t, dir, closedPort(t), mrFileApp, "300ms"))
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
			if c.reason == "" {
				// The reason is the text of the operating system.
				if !strings.Contains(res.err.Error(), want) {
					t.Errorf("err = %q, want it to hold %q", res.err, want)
				}
			} else if !strings.HasSuffix(res.err.Error(), want) {
				t.Errorf("err = %q, want it to end with %q: nothing after the reason", res.err, want)
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

// mrDryConfig is dryConfig written into dir, next to the token file.
func mrDryConfig(t *testing.T, dir, addr, appLines, calls string) string {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}

	cfg := "app:\n  target:\n    ip: " + host + "\n    port: " + port + "\n  tls: false\n" + appLines +
		"load:\n  calls:\n" + calls

	path := filepath.Join(dir, "leettest.yaml")
	if err = os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

// Ground: contract (spec §4.2, C5) — a dry run hands the target the value of the
// file and shows it nowhere: the metadata line, the reply and the error message
// show ${file:PATH}, the prefix "Bearer " left as it is. A refresh set in the
// config is not started: the run returns at once and says nothing about it.
// Mutation M23 (a redactor without the file values) and M27 die here.
func TestRun_DryRunNeverPrintsTheValueOfAFile(t *testing.T) {
	dir := mrDir(t, "f1le-s3cr3t\n")

	addr, echo := startEchoTarget(t, "")
	app := "  metadata:\n    authorization: Bearer ${file:token.txt}\n  metadata_refresh: 1s\n"
	cfg := mrDryConfig(t, dir, addr, app, dryCall(unaryCallMethod, "")+dryCall(emptyCallMethod, ""))

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

// Ground: contract (spec §4.2, C5) — the token a file holds is as secret as one
// written in the config: whether the run succeeds, the config is refused, the
// target refuses the token, or the file's value is invalid, it is on no screen
// and in no error. The file sits next to the config, the cwd is elsewhere.
// Mutation M24 (the content in the error) dies on the last two.
func TestRun_TheTokenOfAFileIsNeverPrinted(t *testing.T) {
	dir, g := mutual(t)
	t.Chdir(t.TempDir())

	const tlsLines = "  ca: server.pem\n  cert: client.pem\n  key: client.key\n"
	const fileLine = "  metadata:\n    authorization: '${file:token.txt}'\n"

	scenarios := []struct {
		name, content, app string
		wantOK             bool
	}{
		{"success", secretToken, tlsLines + fileLine, true},
		// The token sits in a header the config refuses for another reason.
		{"config error", secretToken, tlsLines + fileLine + "    grpc-bad: '${file:token.txt}'\n", false},
		{"target refuses", secretToken + "-wrong", tlsLines + fileLine, false},
		{"bad value", secretToken + "é", tlsLines + fileLine, false},
		{"line break inside", secretToken + "\nsecond", tlsLines + fileLine, false},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte(sc.content), 0o600); err != nil {
				t.Fatal(err)
			}

			res := runCLI(t.Context(), t, 10*time.Second, "-c", guardedConfig(t, dir, g.addr, sc.app, ""))

			if sc.wantOK && res.err != nil {
				t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
			}

			if !sc.wantOK && res.err == nil {
				t.Fatalf("run succeeded\nstdout:\n%s", res.stdout)
			}

			all := res.stdout + res.stderr
			if res.err != nil {
				all += res.err.Error()
			}

			if strings.Contains(all, secretMark) {
				t.Errorf("the token of the file appears in the output:\n%s", all)
			}
		})
	}
}

// Ground: contract (spec §4.1, §4.2, C3, C5) — a read at refresh that fails on
// a file holding a secret says why and never what: the report, the JSON and
// stderr hold the reason only, one line on stderr, and the target keeps getting
// the last good value, never the secret.
// Mutation M24 (the content in the error of a reread) dies here.
func TestMetadataRefresh_ASecretInAFailedReadIsNeverPrinted(t *testing.T) {
	dir := mrDir(t, "tok-one\n")
	target := startMRTarget(t)
	cfg := mrConfig(t, dir, target.addr, mrFileApp+"  metadata_refresh: 1s\n", "4s")
	path := filepath.Join(dir, "token.txt")

	done := make(chan struct{})

	go func() {
		defer close(done)

		<-mrReplace(t, path, 1300*time.Millisecond, secretMark+"é")
		<-mrReplace(t, path, time.Second, secretMark+"\nsecond")
	}()

	res := runCLI(t.Context(), t, 20*time.Second, "-c", cfg, "-output", "json")
	<-done

	if res.err != nil && !errors.Is(res.err, ErrInvalidRun) {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	if all := res.stdout + res.stderr; strings.Contains(all, secretMark) {
		t.Fatalf("the secret of the file is on the screen:\n%s", all)
	}

	reasons := []string{
		`key "authorization": file token.txt: has a byte outside printable ASCII (%x20-%x7E)`,
		`key "authorization": file token.txt: has a line break inside the value (one at the end is cut)`,
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, res.stdout)
	}

	obj, ok := doc["metadata_refresh"].(map[string]any)
	if !ok {
		t.Fatalf("metadata_refresh = %v, want an object", doc["metadata_refresh"])
	}

	if failures, _ := obj["failures"].(float64); failures < 1 || obj["failing_at_end"] != true {
		t.Errorf("metadata_refresh = %v, want failures and failing at the end", obj)
	}

	if last, _ := obj["last_error"].(string); !slices.Contains(reasons, last) {
		t.Errorf("last_error = %q, want the key, the file and the reason, one of %q", last, reasons)
	}

	var lines []string

	for l := range strings.SplitSeq(res.stderr, "\n") {
		if strings.HasPrefix(l, "metadata refresh failed: ") {
			lines = append(lines, l)
		}
	}

	if len(lines) != 1 {
		t.Fatalf("stderr lines %q, want exactly one\nstderr:\n%s", lines, res.stderr)
	}

	const tail = "; the last good value stays in use (the report counts every failed read)"

	reason := strings.TrimSuffix(strings.TrimPrefix(lines[0], "metadata refresh failed: "), tail)
	if !slices.Contains(reasons, reason) {
		t.Errorf("stderr line %q, want the line with one of the reasons %q", lines[0], reasons)
	}

	for i, tok := range target.tokens() {
		if tok != "tok-one" {
			t.Fatalf("call %d carried %q, want the last good value tok-one", i, tok)
		}
	}
}

// Ground: contract (spec §4.2, C5) — the scrape body has no value from a file,
// no path of one and no header name, while the file's value reaches the target.
func TestExportRun_NoFileTokenInTheBody(t *testing.T) {
	dir := mrDir(t, secretMark)
	target := startMRTarget(t)
	cfg := mrConfigWith(t, dir, target.addr, mrFileApp+"  metadata_refresh: 1s\n", "600ms", exportSection("30s"))

	lr := startLive(t, "-output", "json", "-c", cfg)
	addr := lr.addr(t)
	lr.report(t)

	body := scrape(t, addr)
	if !strings.Contains(body, "leettest_calls_total") {
		t.Fatalf("the body has no calls_total, nothing to check it against:\n%s", body)
	}

	for _, secret := range []string{secretMark, "token.txt", "authorization"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(secret)) {
			t.Errorf("the body holds %q", secret)
		}
	}

	if got := target.tokens(); len(got) == 0 || got[0] != secretMark {
		t.Errorf("the target saw %q, want the value of the file first: the run did not use it", got)
	}

	if strings.Contains(lr.stderr.String(), secretMark) {
		t.Errorf("stderr holds the secret:\n%s", lr.stderr.String())
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

	// The watch delivers from its goroutine while the view leaves in main's:
	// in either order the line is printed once and the race detector is quiet.
	t.Run("the view leaves while a line arrives", func(t *testing.T) {
		for range 200 {
			var stderr syncBuf

			n := &refreshNotice{stderr: &stderr, live: true}

			var wg sync.WaitGroup

			wg.Add(2)

			go func() { defer wg.Done(); n.deliver(line) }()
			go func() { defer wg.Done(); n.afterView() }()

			wg.Wait()

			if got := stderr.String(); got != line+"\n" {
				t.Fatalf("stderr = %q, want the line once whichever came first", got)
			}
		}
	})
}
