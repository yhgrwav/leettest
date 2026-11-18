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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
)

// Ground: contract (spec §4.6) — the report's part about the closes is the
// sender's count, field for field, with app.idle_timeout as configured beside
// it; nil when there is no sender (-fake). The four numbers differ, so a swap
// shows. Mutation "ours and peer swapped" turns it red.
func TestClosesOf_IsTheSendersCount(t *testing.T) {
	if got := closesOf(nil, time.Minute); got != nil {
		t.Errorf("closesOf(nil) = %+v, want nil", got)
	}

	src := func() grpcsender.CloseStats {
		return grpcsender.CloseStats{IdleOurs: 12, IdlePeer: 3, Busy: 7, PeerIdleMin: 58300 * time.Millisecond}
	}

	want := &cli.CloseStats{IdleOurs: 12, IdlePeer: 3, Busy: 7, PeerIdleMin: 58300 * time.Millisecond, IdleTimeout: 50 * time.Second}
	if got := closesOf(src, 50*time.Second); !reflect.DeepEqual(got, want) {
		t.Errorf("closesOf = %+v, want %+v", got, want)
	}
}

// Ground: contract — the wiring: a run against a live target reports the
// closes of its sender, so a run in which nothing closed says 0, not null (null
// is "not counted", the -fake run). Mutation "the CLI does not pass the closes
// to the report" turns it red.
func TestRun_TheReportCarriesTheClosesOfTheSender(t *testing.T) {
	target := startTarget(t)
	path := writeConfig(t, target.addr, checkMethod, plaintext)

	res := runCLI(t.Context(), t, 10*time.Second, "-output", "json", "-c", path)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	conns, _ := decodeOnly(t, res.stdout)["connections"].(map[string]any)
	if conns == nil {
		t.Fatalf("connections = nil")
	}

	for _, key := range []string{"closed_idle_ours", "closed_idle_peer", "closed_busy"} {
		if got := conns[key]; got != 0.0 {
			t.Errorf("connections.%s = %#v, want 0: nothing was closed", key, got)
		}
	}

	if v, ok := conns["peer_idle_min_us"]; !ok || v != nil {
		t.Errorf("connections.peer_idle_min_us = %#v (present %v), want null", v, ok)
	}

	res = runCLI(t.Context(), t, 10*time.Second, "-c", path)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	if strings.Contains(res.stdout, "\nclosed ") {
		t.Errorf("a line about closes in a run that closed nothing:\n%s", res.stdout)
	}
}

// Ground: contract — the same for a search: one sender for the whole search,
// its count once, in the top-level closes object. Mutation "the search
// does not pass the closes" turns it red.
func TestSearch_TheReportCarriesTheClosesOfTheSender(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := writeSearch(t, target.addr, checkMethod)

	res := runCLI(t.Context(), t, 60*time.Second, "-output", "json", "-c", cfg)
	if res.err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", res.err, res.stderr)
	}

	closes, _ := decodeOnly(t, res.stdout)["closes"].(map[string]any)
	if closes == nil {
		t.Fatalf("closes = nil in the search's JSON, want the closes of the sender")
	}

	for _, key := range []string{"closed_idle_ours", "closed_idle_peer", "closed_busy"} {
		if got := closes[key]; got != 0.0 {
			t.Errorf("closes.%s = %#v, want 0: nothing was closed", key, got)
		}
	}
}
