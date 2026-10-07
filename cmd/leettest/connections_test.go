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
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
)

// withConnections is the plaintext app line and a connections line.
func withConnections(n string) string { return plaintext + "\n  connections: " + n }

// Ground: contract — app.connections reaches the sender: a config with 2 gives a run over two
// connections, through the CLI that reads the file, and the report shows both. At the IP address
// of the config (ip and port) each is the target as given, and the calls split between them.
// Mutation "cmd does not pass Connections to the sender" turns it red.
func TestRun_ConnectionsReachTheSender(t *testing.T) {
	target := startTarget(t)
	path := writeConfig(t, target.addr, checkMethod, withConnections("2"))

	res := runCLI(t.Context(), t, 10*time.Second, "-output", "json", "-c", path)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	out := decodeOnly(t, res.stdout)
	conns, _ := out["connections"].(map[string]any)

	if conns == nil || conns["open"] != 2.0 {
		t.Fatalf("connections = %v, want two open", out["connections"])
	}
	if got := conns["resolved"]; !reflect.DeepEqual(got, []any{target.addr}) {
		t.Errorf("resolved = %v, want [%s]: an IP address is its own", got, target.addr)
	}

	rows, _ := conns["per_connection"].([]any)
	if len(rows) != 2 {
		t.Fatalf("per_connection = %v, want two connections", conns["per_connection"])
	}

	total := 0.0
	for i, r := range rows {
		row, _ := r.(map[string]any)
		calls, _ := row["calls"].(float64)

		if row["address"] != target.addr || calls == 0 {
			t.Errorf("connection %d: %v, want the target's address and some calls: they round-robin", i+1, row)
		}

		total += calls
	}

	if want := out["sent"].(float64) + out["not_sent"].(float64); total != want {
		t.Errorf("connections carried %v calls, the run measured %v", total, want)
	}

	res = runCLI(t.Context(), t, 10*time.Second, "-c", path)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	if want := "Connections: 2 to " + target.addr + "\n  1  " + target.addr; !strings.Contains(res.stdout, want) {
		t.Errorf("the text report has no block opening with %q:\n%s", want, res.stdout)
	}
}

// Ground: contract — one connection stays as it was: the JSON object has the new keys, null,
// not missing and not an empty array, and the text report has no block.
func TestRun_OneConnectionHasNoPerConnectionReport(t *testing.T) {
	target := startTarget(t)
	path := writeConfig(t, target.addr, checkMethod, plaintext)

	res := runCLI(t.Context(), t, 10*time.Second, "-output", "json", "-c", path)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	conns, _ := decodeOnly(t, res.stdout)["connections"].(map[string]any)
	if conns == nil || conns["open"] != 1.0 {
		t.Fatalf("connections = %v, want one open", conns)
	}

	for _, key := range []string{"resolved", "per_connection"} {
		if v, ok := conns[key]; !ok || v != nil {
			t.Errorf("connections.%s = %v (present: %v), want null", key, v, ok)
		}
	}
	// With one connection it is that connection's limit, which this target may not announce.
	if _, ok := conns["in_flight_limit"]; !ok {
		t.Errorf("connections has no in_flight_limit key")
	}

	res = runCLI(t.Context(), t, 10*time.Second, "-c", path)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	if strings.Contains(res.stdout, "Connections:") {
		t.Errorf("a Connections block at one connection:\n%s", res.stdout)
	}
}

// Ground: boundary — a count outside 1 to 256 fails as a config error before anything connects:
// the target is a closed port, and the error is the config's, not a refused connection.
func TestRun_ConnectionsOutOfRangeFailsBeforeConnecting(t *testing.T) {
	for _, n := range []string{"0", "257", "two"} {
		res := runCLI(t.Context(), t, 3*time.Second, "-c", writeConfig(t, closedPort(t), checkMethod, withConnections(n)))

		if !errors.Is(res.err, config.ErrInvalidConnections) {
			t.Errorf("connections: %s: err = %v, want ErrInvalidConnections", n, res.err)
		}
	}
}

// Ground: contract — -fake ignores connections but still reads the file by the same rules: a bad
// count is an error, a good one changes nothing and adds no block (the fake target has no
// connections to tell apart).
func TestRun_FakeValidatesConnectionsAndIgnoresThem(t *testing.T) {
	res := runCLI(t.Context(), t, 3*time.Second,
		"-fake", "-c", writeConfig(t, closedPort(t), checkMethod, withConnections("0")))
	if !errors.Is(res.err, config.ErrInvalidConnections) {
		t.Errorf("connections: 0 under -fake: err = %v, want ErrInvalidConnections", res.err)
	}
}

// The inverse of "-fake builds a block of connections": the config is accepted and the report
// has none.
func TestRun_FakeWithSeveralConnectionsPrintsNoBlock(t *testing.T) {
	res := runCLI(t.Context(), t, 10*time.Second,
		"-fake", "-c", writeConfig(t, closedPort(t), checkMethod, withConnections("2")))
	if res.err != nil {
		t.Fatalf("run: %v, want connections: 2 accepted under -fake", res.err)
	}

	if strings.Contains(res.stdout, "Connections:") {
		t.Errorf("a Connections block under -fake:\n%s", res.stdout)
	}
}
