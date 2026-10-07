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

package cli

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

// connectionsJSON is the "connections" object of the JSON report of a run.
func connectionsJSON(t *testing.T, conns *engine.Connections) map[string]any {
	t.Helper()

	obj, ok := writeJSON(t, jsonRun(engine.Report{Connections: conns}))["connections"].(map[string]any)
	if !ok {
		t.Fatalf("connections is not an object")
	}

	return obj
}

// connectionKeys are the keys of the object: the five of schema v1 and the three added with
// connections: N. The same at N = 1 and at N = 3; what differs is which are null.
var connectionKeys = []string{
	"in_flight_limit", "limit_changes", "open", "per_connection", "reconnects", "resolved", "first_limit", "last_limit",
}

func hasExactlyTheConnectionKeys(t *testing.T, name string, obj map[string]any) {
	t.Helper()

	if got, want := slices.Sorted(maps.Keys(obj)), slices.Sorted(slices.Values(connectionKeys)); !slices.Equal(got, want) {
		t.Errorf("%s: keys %v, want %v", name, got, want)
	}
}

// Ground: contract — schema v1 only grows: with one connection the object is what it was plus
// three keys. resolved and per_connection are null (there is nothing per connection to say);
// in_flight_limit is the one connection's limit, null when none was announced — 0 is a limit a
// target may announce, so not 0. Inverse of the N = 3 test's "per_connection is an array".
// Mutation "per_connection non-null at N = 1" turns it red.
func TestJSON_ConnectionsOfOne(t *testing.T) {
	announced := connectionsJSON(t, &engine.Connections{
		Open: 1, LimitAnnounced: true, FirstLimit: 4, LastLimit: 4, InFlightLimit: 4, InFlightAnnounced: true,
	})
	hasExactlyTheConnectionKeys(t, "one connection, a limit announced", announced)

	for key, want := range map[string]any{
		"open": 1.0, "first_limit": 4.0, "last_limit": 4.0, "in_flight_limit": 4.0, "resolved": nil, "per_connection": nil,
	} {
		if got := announced[key]; got != want {
			t.Errorf("one connection: %s = %v, want %v", key, got, want)
		}
	}

	silent := connectionsJSON(t, &engine.Connections{Open: 1})
	hasExactlyTheConnectionKeys(t, "one connection, no limit", silent)

	for _, key := range []string{"first_limit", "last_limit", "in_flight_limit", "resolved", "per_connection"} {
		if got := silent[key]; got != nil {
			t.Errorf("no limit announced: %s = %v, want null", key, got)
		}
	}
}

// severalConns is three connections: one that announced 4 and never changed it, one that
// announced 1 after 4 and is slow, one that announced nothing and carried nothing.
func severalConns() *engine.Connections {
	steady, slow, idle := callsOn(addr1, 400, 0), callsOn(addr2, 400, 50), callsOn(addr1, 0, 0)

	steady.LimitAnnounced, steady.FirstLimit, steady.LastLimit = true, 4, 4
	steady.P99 = exactUS(3200)

	slow.LimitAnnounced, slow.FirstLimit, slow.LastLimit, slow.LimitChanges = true, 4, 1, 3
	slow.StreamWaited, slow.NotSentStream = 30, 7
	slow.P99 = metrics.Quantile{Value: 1_000_000_000, Defined: true} // a lower bound: it timed out

	return several([]string{addr1, addr2}, steady, slow, idle)
}

// Ground: contract — with several connections: resolved lists the addresses; per_connection has
// an object per connection with the numbers the text block prints; the run-level first_limit and
// last_limit are null (one connection's limit is not the run's, and a 0 that stood for "not
// known" would read as "the target allows none"); in_flight_limit is the sum, null while any
// connection announced no limit. Per connection the limits follow the run's rule: null when never
// announced, first set with last null when only a change was seen. Mutations "first/last_limit
// read from the scalar fields" (0, not null) and "in_flight_limit is the sum of what was
// announced" (5, not null) turn it red.
func TestJSON_ConnectionsOfSeveral(t *testing.T) {
	obj := connectionsJSON(t, severalConns())
	hasExactlyTheConnectionKeys(t, "three connections", obj)

	for key, want := range map[string]any{
		"open": 3.0, "reconnects": 0.0, "limit_changes": 3.0,
		"first_limit": nil, "last_limit": nil, "in_flight_limit": nil,
	} {
		if got := obj[key]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}

	if got := obj["resolved"]; !reflect.DeepEqual(got, []any{addr1, addr2}) {
		t.Errorf("resolved = %v, want [%s %s]", got, addr1, addr2)
	}

	rows, ok := obj["per_connection"].([]any)
	if !ok || len(rows) != 3 {
		t.Fatalf("per_connection = %v, want an array of 3", obj["per_connection"])
	}

	for i, want := range []map[string]any{
		{
			"address": addr1, "calls": 400.0, "failed": 0.0, "stream_waited": 0.0, "not_sent_stream": 0.0,
			"p99": map[string]any{"us": 3200.0, "lower_bound": false}, "first_limit": 4.0, "last_limit": 4.0, "limit_changes": 0.0,
		},
		{
			"address": addr2, "calls": 400.0, "failed": 50.0, "stream_waited": 30.0, "not_sent_stream": 7.0,
			"p99": map[string]any{"us": 1e6, "lower_bound": true}, "first_limit": 4.0, "last_limit": 1.0, "limit_changes": 3.0,
		},
		{
			"address": addr1, "calls": 0.0, "failed": 0.0, "stream_waited": 0.0, "not_sent_stream": 0.0,
			"p99": nil, "first_limit": nil, "last_limit": nil, "limit_changes": 0.0,
		},
	} {
		if !reflect.DeepEqual(rows[i], want) {
			t.Errorf("per_connection[%d] = %v, want %v", i, rows[i], want)
		}
	}
}

// Ground: contract — when every connection announced a limit, in_flight_limit is their sum, and
// the run-level first_limit and last_limit are still null: the sum is the bound on calls in
// flight, not a stream limit. A connection that changed its limit but whose last handshake
// announced none has its first limit and no last.
func TestJSON_ConnectionsOfSeveralAllAnnounced(t *testing.T) {
	obj := connectionsJSON(t, several(nil, limited(addr1, 4, 4, 0), limited(addr2, 4, 1, 3)))

	if got := obj["in_flight_limit"]; got != 5.0 {
		t.Errorf("in_flight_limit = %v, want 5 (4 + 1)", got)
	}
	for _, key := range []string{"first_limit", "last_limit"} {
		if got := obj[key]; got != nil {
			t.Errorf("%s = %v, want null at several connections", key, got)
		}
	}

	lapsed := callsOn(addr2, 10, 0)
	lapsed.FirstLimit, lapsed.LimitChanges = 4, 1

	obj = connectionsJSON(t, several(nil, limited(addr1, 4, 4, 0), lapsed))

	rows, _ := obj["per_connection"].([]any)
	if len(rows) != 2 {
		t.Fatalf("per_connection = %v, want an array of 2", obj["per_connection"])
	}

	row, _ := rows[1].(map[string]any)
	if row["first_limit"] != 4.0 || row["last_limit"] != nil {
		t.Errorf("a connection that lost its limit: first %v last %v, want 4 and null", row["first_limit"], row["last_limit"])
	}
	if obj["in_flight_limit"] != nil {
		t.Errorf("in_flight_limit = %v, want null while a connection announced none", obj["in_flight_limit"])
	}
}
