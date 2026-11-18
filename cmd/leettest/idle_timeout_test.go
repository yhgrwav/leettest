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
	"context"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
)

// Ground: contract (spec §2.1, §6 U1 (5)) — app.idle_timeout reaches the sender
// as written, and 0, the grpc-go default, when the key is left out.
func TestIdleTimeout_TheConfigReachesTheSender(t *testing.T) {
	for _, want := range []time.Duration{50 * time.Second, 0} {
		opts, _, err := senderOptions(&config.App{IdleTimeout: want}, false)
		if err != nil {
			t.Fatalf("senderOptions: %v", err)
		}
		if opts.IdleTimeout != want {
			t.Errorf("Options.IdleTimeout = %s with app.IdleTimeout %s", opts.IdleTimeout, want)
		}
	}
}

// withAppLine adds an app line to a config written by writeSearch.
func withAppLine(t *testing.T, path, line string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := strings.Replace(string(data), "  tls: false\n", "  tls: false\n"+line, 1)
	if got == string(data) {
		t.Fatalf("no tls line to put %q after:\n%s", line, data)
	}
	if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

const idleRefusal = "app.idle_timeout 5s is shorter than -connect-timeout 10s: " +
	"the idle timer would cut the first connection; raise app.idle_timeout or lower -connect-timeout"

// Ground: contract (spec §2.3) — an idle timeout shorter than -connect-timeout
// is a config error before any connection, in a run, a dry run and a search:
// exit 1, the text names the key and the flag.
func TestIdleTimeout_ShorterThanTheConnectTimeoutIsRefusedBeforeConnecting(t *testing.T) {
	search := writeSearch(t, closedPort(t), checkMethod)
	withAppLine(t, search, "  idle_timeout: 5s\n")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"run", []string{"-c", writeEchoConfig(t, "  idle_timeout: 5s\n", ""), "-yes"}},
		{"dry run", []string{"-c", writeEchoConfig(t, "  idle_timeout: 5s\n", ""), "-dry-run"}},
		{"search", []string{"-c", search, "-yes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runCLI(t.Context(), t, 20*time.Second, append(tc.args, "-connect-timeout", "10s")...)

			if got := exitCode(res.err); got != 1 {
				t.Fatalf("exit %d (%v), want 1\nstderr:\n%s", got, res.err, res.stderr)
			}
			if !strings.Contains(res.err.Error(), idleRefusal) {
				t.Errorf("error %q, want it to say %q", res.err, idleRefusal)
			}
			if strings.Contains(res.stderr, "connecting to") {
				t.Errorf("a connection was started before the refusal:\n%s", res.stderr)
			}
		})
	}
}

// Ground: boundary (spec §2.3) — an idle timeout equal to -connect-timeout is
// allowed, as checkIdle allows it: the dry run goes through.
func TestIdleTimeout_EqualToTheConnectTimeoutIsAccepted(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dryConfig(t, target.addr, "  idle_timeout: 10s\n", dryCall(unaryCallMethod, ""))

	res := runCLI(t.Context(), t, 20*time.Second, "-c", cfg, "-dry-run", "-connect-timeout", "10s")

	if res.err != nil {
		t.Errorf("idle_timeout 10s with -connect-timeout 10s: %v\nstderr:\n%s", res.err, res.stderr)
	}
}

// Ground: contract (spec §2.3) — with -fake the key is accepted and does
// nothing: no check against -connect-timeout.
func TestIdleTimeout_FakeIgnoresTheCheck(t *testing.T) {
	text, _ := echoRun(t, 20*time.Second, "-c", writeEchoConfig(t, "  idle_timeout: 5s\n", ""), "-connect-timeout", "10s")

	if !strings.Contains(text, "settings: ") {
		t.Errorf("-fake with idle_timeout 5s gave no report:\n%s", text)
	}
}

// Ground: contract (spec §2.6) — a run echoes app.idle_timeout in its text
// settings line and in settings.idle_timeout_us, and says nothing when the key
// is left out (null in JSON).
func TestIdleTimeout_ARunEchoesItOnlyWhenSet(t *testing.T) {
	text, js := echoRun(t, 20*time.Second, "-c", writeEchoConfig(t, "  idle_timeout: 50s\n", ""))

	if !strings.Contains(text, "\nsettings: warm-up 0s, max in flight 5000, connections 1, idle_timeout 50s\n") {
		t.Errorf("the text report lacks idle_timeout in the settings line:\n%s", text)
	}
	if got := fieldNumber(t, js, "settings", "idle_timeout_us"); got != 50000000 {
		t.Errorf("settings.idle_timeout_us = %v, want 50000000", got)
	}

	text, js = echoRun(t, 20*time.Second, "-c", writeEchoConfig(t, "", ""))

	if strings.Contains(text, "idle_timeout") {
		t.Errorf("idle_timeout is printed with the key left out:\n%s", text)
	}
	if got, present := js["settings"].(map[string]any)["idle_timeout_us"]; !present || got != nil {
		t.Errorf("settings.idle_timeout_us = %#v (present %v) with the key left out, want null", got, present)
	}
}

// Ground: contract (spec §2.6) — each run of a search carries it in its
// settings.
func TestIdleTimeout_ASearchRunsCarryIt(t *testing.T) {
	cfg := writeSearch(t, closedPort(t), checkMethod)
	withAppLine(t, cfg, "  idle_timeout: 50s\n")

	_, js := echoRun(t, 60*time.Second, "-c", cfg)

	runs := js["breakpoint"].(map[string]any)["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("the search reports no runs")
	}
	for i, r := range runs {
		settings := r.(map[string]any)["report"].(map[string]any)["settings"].(map[string]any)
		if settings["idle_timeout_us"] != 50000000.0 {
			t.Errorf("runs[%d].report.settings.idle_timeout_us = %v, want 50000000", i, settings["idle_timeout_us"])
		}
	}
}

// Ground: contract (spec §6 U1 (8)) — end to end from the config text: a
// connection with no call on it for longer than app.idle_timeout is closed by
// the client, and the next call reconnects, succeeds and waited for it.
func TestIdleTimeout_AnIdleConnectionIsClosedAndTheNextCallReconnects(t *testing.T) {
	target := startTarget(t)
	cfg, err := config.LoadFile(dryConfig(t, target.addr, "  idle_timeout: 1s\n", dryCall(checkMethod, "")))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	opts, _, err := senderOptions(&cfg.App, false)
	if err != nil {
		t.Fatalf("senderOptions: %v", err)
	}
	// The first dial is at once; every later one takes redial, as a target
	// across a network does. On loopback a reconnect takes well under a
	// millisecond and ConnWait reads 0 or 0.5 ms from one run to the next
	// (measured, 3 reconnecting calls: 1 had a wait), so the wait is made
	// long enough for the assertion to be deterministic.
	const redial = 200 * time.Millisecond

	var dials atomic.Int32

	opts.DialOptions = append(opts.DialOptions, grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		if dials.Add(1) > 1 {
			time.Sleep(redial)
		}

		return new(net.Dialer).DialContext(ctx, "tcp", addr)
	}))

	sender := grpcsender.New(opts)
	t.Cleanup(func() { _ = sender.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := sender.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	send := func() engine.Outcome {
		t.Helper()

		sendCtx, sendCancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer sendCancel()

		out, sendErr := sender.Send(sendCtx, engine.Request{Method: "/" + checkMethod, ScheduledAt: time.Now()})
		if sendErr != nil {
			t.Fatalf("send: %v", sendErr)
		}

		return out
	}

	first := send()
	time.Sleep(1500 * time.Millisecond)
	second := send()

	if first.Category != engine.CategorySuccess || second.Category != engine.CategorySuccess {
		t.Errorf("categories %v and %v, want both success (%v, %v)", first.Category, second.Category, first.Err, second.Err)
	}
	if got, _ := sender.Connections(); got.Reconnects < 1 {
		t.Errorf("reconnects %d after a 1.5s pause with idle_timeout 1s, want at least 1", got.Reconnects)
	}
	if second.ConnWait < redial*3/4 {
		t.Errorf("the call after the pause has ConnWait %v, want about the redial of %v", second.ConnWait, redial)
	}
	if first.ConnWait != 0 {
		t.Errorf("the first call has ConnWait %v on a connection made by Connect, want 0", first.ConnWait)
	}
}
