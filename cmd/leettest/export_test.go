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
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// syncBuf is a writer a test may read while the run still writes to it.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.b.String()
}

// liveRun is a command line run going on in the background, so that a test can
// scrape it while it goes and while it lingers.
type liveRun struct {
	stdout, stderr *syncBuf
	stops, aborts  chan struct{}
	cancel         context.CancelFunc
	over           chan struct{}
	err            error
}

// startLive starts run with the given arguments. It is cancelled and waited
// for when the test ends, so a failed test leaves no listener behind.
func startLive(t *testing.T, args ...string) *liveRun {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	ctx, cancel := context.WithCancel(t.Context())
	lr := &liveRun{
		stdout: &syncBuf{}, stderr: &syncBuf{},
		stops: make(chan struct{}), aborts: make(chan struct{}),
		cancel: cancel, over: make(chan struct{}),
	}

	go func() {
		lr.err = run(ctx, lr.stops, lr.aborts, args, lr.stdout, lr.stderr)
		close(lr.over)
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case <-lr.over:
		case <-time.After(15 * time.Second):
			t.Error("the run has not returned 15s after its context was cancelled")
		}
	})

	return lr
}

func (lr *liveRun) finished() bool {
	select {
	case <-lr.over:
		return true
	default:
		return false
	}
}

// wait returns the run's error once it has returned, failing the test at limit.
func (lr *liveRun) wait(t *testing.T, limit time.Duration) error {
	t.Helper()

	select {
	case <-lr.over:
		return lr.err
	case <-time.After(limit):
		logGoroutines(t)
		t.Fatalf("the run has not returned after %v; stderr:\n%s", limit, lr.stderr.String())

		return nil
	}
}

var listeningLine = regexp.MustCompile(`metrics: listening on http://([^/\s]+)/metrics`)

// addr is the address the run says it listens on. It waits for the line, and
// fails at once if the run ended without ever printing it.
func (lr *liveRun) addr(t *testing.T) string {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := listeningLine.FindStringSubmatch(lr.stderr.String()); m != nil {
			return m[1]
		}
		if lr.finished() {
			t.Fatalf("the run ended without saying 'metrics: listening on http://ADDR/metrics'; stderr:\n%s", lr.stderr.String())
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("no 'metrics: listening on http://ADDR/metrics' line in 10s; stderr:\n%s", lr.stderr.String())

	return ""
}

// report waits for the JSON report on stdout. A run that lingers has written it
// and has not returned.
func (lr *liveRun) report(t *testing.T) map[string]any {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if out := lr.stdout.String(); out != "" && json.Valid([]byte(out)) {
			return decodeOnly(t, out)
		}
		if lr.finished() {
			t.Fatalf("the run returned (%v) before the test saw its report; stdout:\n%s", lr.err, lr.stdout.String())
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("no JSON report in 30s; stdout:\n%s", lr.stdout.String())

	return nil
}

var scrapeClient = &http.Client{Timeout: 5 * time.Second}

// fetch is one request; it reports a failure instead of failing a test, so a
// goroutine may call it.
func fetch(ctx context.Context, method, url string) (code int, header http.Header, body string, err error) {
	req, err := http.NewRequestWithContext(ctx, method, url, http.NoBody)
	if err != nil {
		return 0, nil, "", err
	}
	resp, err := scrapeClient.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, "", err
	}

	return resp.StatusCode, resp.Header, string(raw), nil
}

func get(t *testing.T, method, url string) (code int, header http.Header, body string) {
	t.Helper()

	code, header, body, err := fetch(t.Context(), method, url)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}

	return code, header, body
}

// press sends one Ctrl-C or SIGTERM to the run, failing the test if nobody takes
// it: a run that is not waiting for a stop is the thing being checked.
func press(t *testing.T, ch chan<- struct{}) {
	t.Helper()

	select {
	case ch <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing took the press: the run is not waiting for a stop")
	}
}

func scrape(t *testing.T, addr string) string {
	t.Helper()

	code, _, body := get(t, http.MethodGet, "http://"+addr+"/metrics")
	if code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200; body:\n%s", code, body)
	}

	return body
}

// valueOf is the value of the one sample of name whose labels contain every
// fragment, such as `method="a/B"` or `category="success"`.
func valueOf(t *testing.T, body, name string, fragments ...string) float64 {
	t.Helper()

	v, n := findValue(body, name, fragments...)
	if n != 1 {
		t.Fatalf("%d samples of %s with %v, want 1; body:\n%s", n, name, fragments, body)
	}

	return v
}

// findValue is the value of the sample of name whose labels contain every
// fragment, and how many such samples there are.
func findValue(body, name string, fragments ...string) (value float64, found int) {
lines:
	for _, line := range strings.Split(body, "\n") {
		var labels string
		switch {
		case strings.HasPrefix(line, name+"{"):
			labels = line[len(name)+1 : strings.LastIndex(line, "}")]
		case strings.HasPrefix(line, name+" "):
		default:
			continue
		}
		for _, f := range fragments {
			if !strings.Contains(labels, f) {
				continue lines
			}
		}
		v, err := strconv.ParseFloat(line[strings.LastIndex(line, " ")+1:], 64)
		if err != nil {
			continue
		}
		found++
		value = v
	}

	return value, found
}

// sampleLines are the lines of a body that are not comments.
func sampleLines(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}

	return out
}

func writeExportConfig(t *testing.T, rps int, duration, warmup, appLines, topLines string) string {
	t.Helper()

	cfg := fmt.Sprintf(`app:
  target:
    ip: localhost
    port: 1
%sload:
  warmup: %s
  calls:
    - method: %s
      rps: %d
      duration: %s
%s`, appLines, warmup, checkMethod, rps, duration, topLines)

	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func exportSection(linger string) string {
	return "export:\n  prometheus:\n    listen: 127.0.0.1:0\n    linger: " + linger + "\n"
}

// Ground: Т4 (reviewer's В8), the purpose of the export — a scrape in the middle
// of a run sees the rate the config plans and calls that grow. Two scrapes at
// least a second apart (the render cache), the second with more successes.
func TestExportRun_AScrapeMidRunSeesThePlannedRateAndGrowingCalls(t *testing.T) {
	lr := startLive(t, "-fake", "-fake-delay", "1ms", "-c", writeExportConfig(t, 40, "6s", "0s", "", exportSection("0s")))
	addr := lr.addr(t)
	method := `method="` + checkMethod + `"`

	// Before Run reserves the methods the body has no samples; that is not yet a failure.
	successes := func() float64 {
		n, _ := findValue(scrape(t, addr), "leettest_calls_total", method, `category="success"`)

		return n
	}
	waitFor := func(above float64) float64 {
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if n := successes(); n > above {
				return n
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatalf("calls_total{success} did not rise above %v in 8s", above)

		return 0
	}

	first := waitFor(0)
	if planned := valueOf(t, scrape(t, addr), "leettest_planned_rps", method); planned != 40 {
		t.Errorf("planned_rps = %v in the middle of a 40 rps run, want 40", planned)
	}
	if second := waitFor(first); second <= first {
		t.Errorf("calls_total{success} = %v after %v, want it to grow", second, first)
	}

	if err := lr.wait(t, 30*time.Second); err != nil {
		t.Errorf("run = %v, want nil", err)
	}
}

// Ground: Т4 (reviewer's В8) — after the run, in the linger, each of the eleven
// categories, warmup_sent, not_sent and the histogram's count equal the report's
// numbers for the method. The report is on stdout while the run still lingers.
func TestExportRun_AfterTheRunTheEndpointEqualsTheReport(t *testing.T) {
	lr := startLive(t, "-fake", "-fake-delay", "1ms", "-fake-fail-ratio", "0.3", "-output", "json", "-label", "commit=abc",
		"-c", writeExportConfig(t, 100, "1500ms", "300ms", "", "labels:\n  env: stage\n"+exportSection("30s")))
	addr := lr.addr(t)
	report := lr.report(t)

	if lr.finished() {
		t.Fatal("the run returned together with its report: it must linger with the endpoint up")
	}
	body := scrape(t, addr)

	m := jsonAt(t, report, "methods").([]any)[0]
	num := func(path ...string) float64 { return jsonAt(t, m, path...).(float64) }
	method := `method="` + checkMethod + `"`

	want := map[string]float64{
		"success":       num("observations") - num("censored"),
		"request_error": num("request_error", "count"),
		"overload":      num("overload", "count"),
		"failure":       num("failure", "count"),
		"timed_out":     num("timed_out"),
		"cut_off":       num("cut_off"),
		"unreachable":   num("unreachable"),
		"client_error":  num("client_error"),
		"bad_response":  num("bad_response", "count"),
		"aborted":       num("aborted"),
		"unclassified":  num("unclassified"),
	}
	for category, n := range want {
		t.Run("calls_total/"+category, func(t *testing.T) {
			if got := valueOf(t, body, "leettest_calls_total", method, `category="`+category+`"`); got != n {
				t.Errorf("calls_total{%s} = %v, the report says %v", category, got, n)
			}
		})
	}

	t.Run("latency count is the success count", func(t *testing.T) {
		if got := valueOf(t, body, "leettest_latency_seconds_count", method); got != want["success"] {
			t.Errorf("latency_seconds_count = %v, the report's successes are %v", got, want["success"])
		}
	})
	t.Run("latency +Inf is the success count", func(t *testing.T) {
		if got := valueOf(t, body, "leettest_latency_seconds_bucket", method, `le="+Inf"`); got != want["success"] {
			t.Errorf("latency_seconds_bucket{le=+Inf} = %v, the report's successes are %v", got, want["success"])
		}
	})
	for _, cause := range []string{"generator", "connection", "stream"} {
		t.Run("not_sent_total/"+cause, func(t *testing.T) {
			if got, wantN := valueOf(t, body, "leettest_not_sent_total", method, `cause="`+cause+`"`), num("not_sent_"+cause); got != wantN {
				t.Errorf("not_sent_total{%s} = %v, the report says %v", cause, got, wantN)
			}
		})
	}
	t.Run("warmup_sent_total", func(t *testing.T) {
		if got, wantN := valueOf(t, body, "leettest_warmup_sent_total", method), num("warmup_sent"); got != wantN {
			t.Errorf("warmup_sent_total = %v, the report says %v", got, wantN)
		}
	})
	t.Run("warmup is off after the run", func(t *testing.T) {
		if got := valueOf(t, body, "leettest_warmup"); got != 0 {
			t.Errorf("warmup = %v after the run, want 0", got)
		}
	})
	t.Run("planned_rps is zero after the run", func(t *testing.T) {
		if got := valueOf(t, body, "leettest_planned_rps", method); got != 0 {
			t.Errorf("planned_rps = %v after the run, want 0", got)
		}
	})
	t.Run("in_flight is zero after the run", func(t *testing.T) {
		if got := valueOf(t, body, "leettest_in_flight"); got != 0 {
			t.Errorf("in_flight = %v after the run, want 0", got)
		}
	})

	// Ground: reviewer's В2 and Р1 — the passport's labels, the config's and the
	// flag's, are constant labels on every series, after the series' own labels,
	// sorted by name; no run label.
	t.Run("every series carries the labels", func(t *testing.T) {
		lines := sampleLines(body)
		if len(lines) == 0 {
			t.Fatal("no samples")
		}
		for _, line := range lines {
			if !strings.Contains(line, `commit="abc",env="stage"}`) {
				t.Errorf("%q lacks the labels commit=abc,env=stage at its end", line)
			}
			if strings.Contains(line, `run="`) {
				t.Errorf("%q has a run label", line)
			}
		}
	})

	// Ground: reviewer's Р6 — the stderr line that says the endpoint stays.
	t.Run("stderr says how long it serves", func(t *testing.T) {
		if !strings.Contains(lr.stderr.String(), "metrics: serving final values for 30s at "+addr) {
			t.Errorf("stderr has no 'metrics: serving final values for 30s at %s':\n%s", addr, lr.stderr.String())
		}
	})

	press(t, lr.stops)
	if err := lr.wait(t, 10*time.Second); err != nil {
		t.Errorf("run = %v, want the exit status of the run itself", err)
	}
}

// Ground: reviewer's Р6 — Ctrl-C and SIGTERM in the linger end it at once and
// leave the run's exit code as it was: not 130, not 143, no error of its own.
func TestExportRun_AStopInTheLingerEndsItWithTheRunsOwnExitCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		hit  func(lr *liveRun) chan<- struct{}
	}{
		{"ctrl-c", func(lr *liveRun) chan<- struct{} { return lr.stops }},
		{"sigterm", func(lr *liveRun) chan<- struct{} { return lr.aborts }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lr := startLive(t, "-fake", "-fake-delay", "1ms", "-output", "json",
				"-c", writeExportConfig(t, 50, "500ms", "0s", "", exportSection("1h")))
			lr.addr(t)
			lr.report(t)

			press(t, tc.hit(lr))

			err := lr.wait(t, 10*time.Second)
			if err != nil {
				t.Errorf("run = %v after a stop in the linger, want nil, the run's own status", err)
			}
			if code := exitCode(err); code != 0 {
				t.Errorf("exit code %d, want 0", code)
			}
		})
	}
}

// Ground: reviewer's Р6, order — the report and the exit status are fixed before
// the linger starts: with the linger running, stdout already holds a complete
// report and nothing more is printed to it. The status the run ends with is that
// of a run whose plan was cut (3), not changed by the linger.
func TestExportRun_AStoppedRunKeepsItsStatusThroughTheLinger(t *testing.T) {
	cfg := writeExportConfig(t, 50, "1m", "0s", "", exportSection("1h"))
	lr := startLive(t, "-fake", "-fake-delay", "1ms", "-output", "json", "-c", cfg)
	lr.addr(t)

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(scrapeOrEmpty(t, lr), `category="success"} `) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	press(t, lr.stops)

	lr.report(t)
	if lr.finished() {
		t.Fatal("a stopped run returned together with its report, it must linger")
	}

	// The press that ends the linger is not the stopper's second press: the
	// run's own status (3, incomplete) is what comes out, not an abort.
	press(t, lr.stops)
	err := lr.wait(t, 10*time.Second)
	if exitCode(err) != 3 {
		t.Errorf("exit code %d (%v) after a stopped run lingered and was stopped, want 3", exitCode(err), err)
	}
}

func scrapeOrEmpty(t *testing.T, lr *liveRun) string {
	t.Helper()

	m := listeningLine.FindStringSubmatch(lr.stderr.String())
	if m == nil {
		return ""
	}

	return scrape(t, m[1])
}

// Ground: reviewer's Р1 and В8/Т8 — the listener is open before Run: a scrape
// before the first call is 200 with the HELP and TYPE lines of the nine families
// and no sample (methods are reserved inside Run).
func TestExportRun_AScrapeBeforeRunIsTheHeadersAlone(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	letGo := func() { releaseOnce.Do(func() { close(release) }) }
	runStarting = func(*engine.Engine) {
		select {
		case <-release:
		case <-time.After(10 * time.Second):
		}
	}
	t.Cleanup(func() { runStarting = func(*engine.Engine) {} })

	lr := startLive(t, "-fake", "-fake-delay", "1ms", "-c", writeExportConfig(t, 20, "500ms", "0s", "", exportSection("0s")))
	// Registered after startLive's, so it runs before it: the run must be let go
	// before the test waits for it.
	t.Cleanup(letGo)
	addr := lr.addr(t)

	code, header, body := get(t, http.MethodGet, "http://"+addr+"/metrics")
	letGo()

	if code != http.StatusOK {
		t.Fatalf("GET /metrics before Run = %d, want 200", code)
	}
	if ct := header.Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if n := strings.Count(body, "# TYPE "); n != 9 {
		t.Errorf("%d TYPE lines before Run, want 9 families:\n%s", n, body)
	}
	if lines := sampleLines(body); len(lines) != 0 {
		t.Errorf("%d samples before Run, want none: %q", len(lines), lines)
	}

	if err := lr.wait(t, 20*time.Second); err != nil {
		t.Errorf("run = %v, want nil", err)
	}
}

// Ground: reviewer's Р1 — the port opens before the target is asked: while the
// run waits for a target that accepts the connection and says nothing, the
// endpoint already answers.
func TestExportRun_TheEndpointAnswersWhileTheRunConnects(t *testing.T) {
	host, port, err := net.SplitHostPort(silentTarget(t))
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	cfg := fmt.Sprintf("app:\n  target:\n    ip: %s\n    port: %s\n  tls: false\nload:\n  calls:\n    - method: %s\n      rps: 10\n      duration: 200ms\n%s",
		host, port, checkMethod, exportSection("0s"))
	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lr := startLive(t, "-connect-timeout", "20s", "-c", path)
	addr := lr.addr(t)

	body := scrape(t, addr)
	if !strings.Contains(body, "# TYPE leettest_calls_total counter") {
		t.Errorf("the scrape during the connection has no calls_total family:\n%s", body)
	}
	if lr.finished() {
		t.Error("the run ended while it was to wait for the silent target: the scrape did not happen during the connection")
	}
}

// Ground: Т9 (reviewer's В8) — nothing secret is in the body: not a metadata
// value, written or read from the environment, and not the address of the
// target. The labels of the run are the one thing the user puts there on purpose.
func TestExportRun_NoSecretAndNoTargetAddressInTheBody(t *testing.T) {
	t.Setenv("LEETTEST_PROBE_TOKEN", "env-secret-7731")

	app := "  metadata:\n    authorization: Bearer written-secret-4417\n    x-token: ${LEETTEST_PROBE_TOKEN}\n"
	cfg := fmt.Sprintf("app:\n  target:\n    ip: 203.0.113.77\n    port: 5443\n  tls: false\n%sload:\n  calls:\n    - method: %s\n      rps: 50\n      duration: 600ms\n%s",
		app, checkMethod, exportSection("30s"))
	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lr := startLive(t, "-fake", "-fake-delay", "1ms", "-output", "json", "-c", path)
	addr := lr.addr(t)
	lr.report(t)

	body := scrape(t, addr)
	if !strings.Contains(body, "leettest_calls_total") {
		t.Fatalf("the body has no calls_total, nothing to check it against:\n%s", body)
	}
	for _, secret := range []string{"written-secret-4417", "env-secret-7731", "203.0.113.77", "authorization", "x-token"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(secret)) {
			t.Errorf("the body holds %q", secret)
		}
	}
	if strings.Contains(lr.stderr.String(), "written-secret-4417") || strings.Contains(lr.stderr.String(), "env-secret-7731") {
		t.Errorf("stderr holds a secret:\n%s", lr.stderr.String())
	}
}

// Ground: Т10 (reviewer's В8) — a scrape storm changes neither the report nor
// the load: the same run with a storm of scrapes and without gives the same
// planned calls (sent plus not sent), and the bodies served in the storm are at
// most one per second, however many scrapes came (the render cache).
func TestExportRun_AScrapeStormDoesNotChangeTheReportAndRendersOncePerSecond(t *testing.T) {
	plan := func(report map[string]any) float64 {
		return jsonAt(t, report, "sent").(float64) + jsonAt(t, report, "not_sent").(float64)
	}
	args := func(section string) []string {
		return []string{"-fake", "-fake-delay", "1ms", "-output", "json", "-c", writeExportConfig(t, 50, "3s", "0s", "", section)}
	}

	quiet := startLive(t, args(exportSection("30s"))...)
	quiet.addr(t)
	quietPlan := plan(quiet.report(t))
	press(t, quiet.stops)
	if err := quiet.wait(t, 10*time.Second); err != nil {
		t.Fatalf("quiet run = %v", err)
	}

	loud := startLive(t, args(exportSection("30s"))...)
	addr := loud.addr(t)

	var (
		mu     sync.Mutex
		bodies = map[string]struct{}{}
		count  int
		wg     sync.WaitGroup
	)
	stormStarted := time.Now()
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Since(stormStarted) < 2500*time.Millisecond {
				_, _, b, err := fetch(t.Context(), http.MethodGet, "http://"+addr+"/metrics")
				if err != nil {
					continue
				}

				mu.Lock()
				bodies[b] = struct{}{}
				count++
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	distinct, scrapes := len(bodies), count
	mu.Unlock()

	if scrapes < 200 {
		t.Fatalf("the storm made %d scrapes in 2.5s, want at least 200 for the check to mean anything", scrapes)
	}
	// 2.5 s hold three renders at one a second; a fourth is the final one when the
	// run ends inside the storm on a slow machine.
	if distinct > 4 {
		t.Errorf("%d different bodies from %d scrapes in 2.5s, want at most 4 (one render a second)", distinct, scrapes)
	}

	loudPlan := plan(loud.report(t))
	press(t, loud.stops)
	if err := loud.wait(t, 10*time.Second); err != nil {
		t.Fatalf("loud run = %v", err)
	}
	if loudPlan != quietPlan {
		t.Errorf("the run under the storm planned %v calls (sent + not sent), the quiet one %v", loudPlan, quietPlan)
	}
}

// Ground: Т11 (reviewer's В1, В3) — everything known before the run is exit 1,
// before any call and with no report: the text names what is wrong.
func TestExportRun_RefusalsBeforeTheRunAreExitOne(t *testing.T) {
	lingerNegative := writeExportConfig(t, 10, "300ms", "0s", "", "export:\n  prometheus:\n    listen: 127.0.0.1:0\n    linger: -1s\n")
	withLabels := func(labels string) string {
		return writeExportConfig(t, 10, "300ms", "0s", "", "labels:\n"+labels+exportSection("0s"))
	}
	search := writeSearch(t, closedPort(t), checkMethod)
	searchWithExport := func() string {
		raw, err := os.ReadFile(search)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		path := filepath.Join(t.TempDir(), "leettest.yaml")
		if err := os.WriteFile(path, append(raw, []byte("\n"+exportSection("0s"))...), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		return path
	}()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"a search beside export", []string{"-fake", "-c", searchWithExport}, "breakpoint"},
		{"dry-run beside export", []string{"-dry-run", "-c", writeExportConfig(t, 10, "300ms", "0s", "", exportSection("0s"))}, "dry-run"},
		{"a negative linger", []string{"-fake", "-c", lingerNegative}, "linger"},
		{"a label with a dot", []string{"-fake", "-c", withLabels("  commit.sha: abc\n")}, "commit.sha"},
		{"a label with a dash", []string{"-fake", "-c", withLabels("  ci-env: x\n")}, "ci-env"},
		{"a label starting with a digit", []string{"-fake", "-c", withLabels("  1x: x\n")}, "1x"},
		{"a label starting with two underscores", []string{"-fake", "-c", withLabels("  __name: x\n")}, "__name"},
		{"a label named method", []string{"-fake", "-c", withLabels("  method: x\n")}, "method"},
		{"a label named category", []string{"-fake", "-c", withLabels("  category: x\n")}, "category"},
		{"a label named cause", []string{"-fake", "-c", withLabels("  cause: x\n")}, "cause"},
		{"a label named le", []string{"-fake", "-c", withLabels("  le: x\n")}, "le"},
		{"a flag label with a dot", []string{"-fake", "-label", "commit.sha=abc", "-c", writeExportConfig(t, 10, "300ms", "0s", "", exportSection("0s"))}, "commit.sha"},
		{"a flag label named method", []string{"-fake", "-label", "method=abc", "-c", writeExportConfig(t, 10, "300ms", "0s", "", exportSection("0s"))}, "method"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := runCLI(t.Context(), t, 20*time.Second, tc.args...)

			if res.err == nil {
				t.Fatalf("run = nil, want an error; stdout:\n%s", res.stdout)
			}
			if code := exitCode(res.err); code != 1 {
				t.Errorf("exit code %d (%v), want 1", code, res.err)
			}
			if !strings.Contains(res.err.Error(), tc.want) {
				t.Errorf("the error %q does not name %q", res.err, tc.want)
			}
			if res.stdout != "" {
				t.Errorf("stdout is not empty on a refusal: %q", res.stdout)
			}
			if strings.Contains(res.stderr, "metrics: listening") {
				t.Errorf("the endpoint was opened before the refusal:\n%s", res.stderr)
			}
		})
	}
}

// Ground: Т11 — the labels the endpoint refuses are fine without it. Green on the
// stub: it holds the line against checking the labels of every run.
func TestExportRun_LabelsPrometheusWouldRefuseAreFineWithoutExport(t *testing.T) {
	res := runCLI(t.Context(), t, 30*time.Second, "-fake", "-fake-delay", "1ms", "-label", "commit.sha=abc",
		"-c", writeExportConfig(t, 20, "300ms", "0s", "", "labels:\n  ci-env: x\n  method: y\n"))

	if res.err != nil {
		t.Errorf("run = %v, want nil: labels only matter with export:", res.err)
	}
}

// Ground: Т11 (reviewer's В1) — a port that is taken ends the command before any
// call to the target: the error names the address, and is not a connection
// error (the target is a closed port: had the run gone to it first, that is
// what would be said).
func TestExportRun_ATakenPortIsRefusedBeforeTheTargetIsAsked(t *testing.T) {
	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })
	taken := lis.Addr().String()

	host, port, _ := net.SplitHostPort(closedPort(t))
	cfg := fmt.Sprintf("app:\n  target:\n    ip: %s\n    port: %s\n  tls: false\nload:\n  calls:\n    - method: %s\n      rps: 10\n      duration: 200ms\nexport:\n  prometheus:\n    listen: %s\n    linger: 0s\n",
		host, port, checkMethod, taken)
	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	res := runCLI(t.Context(), t, 20*time.Second, "-c", path)

	if res.err == nil {
		t.Fatal("run = nil with the export port taken, want an error")
	}
	if code := exitCode(res.err); code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
	if !strings.Contains(res.err.Error(), taken) {
		t.Errorf("the error %q does not name the address %s", res.err, taken)
	}
	if strings.Contains(strings.ToLower(res.err.Error()+res.stderr), "connect") {
		t.Errorf("the run went to the target before it bound the port:\nerror: %v\nstderr:\n%s", res.err, res.stderr)
	}
}

// Ground: reviewer's Р1 — -metrics-listen overrides the config's listen; the
// config's address stays free.
func TestExportRun_TheFlagOverridesTheConfigsAddress(t *testing.T) {
	configured := closedPort(t)
	flagged := closedPort(t)
	cfg := writeExportConfig(t, 20, "800ms", "0s", "",
		"export:\n  prometheus:\n    listen: "+configured+"\n    linger: 0s\n")

	lr := startLive(t, "-fake", "-fake-delay", "1ms", "-metrics-listen", flagged, "-c", cfg)

	if got := lr.addr(t); got != flagged {
		t.Errorf("the run listens on %s, want %s from -metrics-listen", got, flagged)
	}
	if err := lr.wait(t, 20*time.Second); err != nil {
		t.Errorf("run = %v, want nil", err)
	}
}

// Ground: the brief's open point, decided here — the flag alone turns the export
// on, and a run so started lingers the default 15 s. The decision is in the
// report to the reviewer.
func TestExportRun_TheFlagAloneTurnsTheExportOnWithTheDefaultLinger(t *testing.T) {
	lr := startLive(t, "-fake", "-fake-delay", "1ms", "-output", "json", "-metrics-listen", "127.0.0.1:0",
		"-c", writeExportConfig(t, 20, "400ms", "0s", "", ""))
	addr := lr.addr(t)
	lr.report(t)

	if lr.finished() {
		t.Fatal("the run returned together with its report, want a linger of 15s")
	}
	if !strings.Contains(scrape(t, addr), "leettest_calls_total") {
		t.Error("no calls_total in the final scrape")
	}
	if !strings.Contains(lr.stderr.String(), "metrics: serving final values for 15s at "+addr) {
		t.Errorf("stderr has no 'serving final values for 15s':\n%s", lr.stderr.String())
	}

	press(t, lr.stops)
	if err := lr.wait(t, 10*time.Second); err != nil {
		t.Errorf("run = %v, want nil", err)
	}
}

// Ground: reviewer's Р6 — linger: 0 stops at once: the run returns with its
// report and the port is free again.
func TestExportRun_LingerZeroReturnsWithTheReport(t *testing.T) {
	lr := startLive(t, "-fake", "-fake-delay", "1ms", "-output", "json",
		"-c", writeExportConfig(t, 20, "400ms", "0s", "", exportSection("0s")))
	addr := lr.addr(t)

	if err := lr.wait(t, 20*time.Second); err != nil {
		t.Fatalf("run = %v, want nil", err)
	}
	if strings.Contains(lr.stderr.String(), "serving final values") {
		t.Errorf("stderr announces a linger that linger: 0s does not have:\n%s", lr.stderr.String())
	}

	if _, _, _, err := fetch(t.Context(), http.MethodGet, "http://"+addr+"/metrics"); err == nil {
		t.Error("the endpoint still answers after the run returned with linger: 0s")
	}
}

// Ground: reviewer's Р7 and strategy (off is absent) — a run without export says
// nothing about metrics and opens nothing. Green on the stub.
func TestExportRun_WithoutExportNothingIsSaidAboutMetrics(t *testing.T) {
	res := runCLI(t.Context(), t, 30*time.Second, "-fake", "-fake-delay", "1ms",
		"-c", writeExportConfig(t, 20, "300ms", "0s", "", ""))

	if res.err != nil {
		t.Fatalf("run = %v", res.err)
	}
	if strings.Contains(strings.ToLower(res.stderr+res.stdout), "metrics") {
		t.Errorf("a run without export: mentions metrics:\n%s\n%s", res.stderr, res.stdout)
	}
}
