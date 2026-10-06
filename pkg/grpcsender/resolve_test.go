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

package grpcsender

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Ground: contract — what the system resolver answers becomes addresses at the target's port,
// each once, in the order given: connection i takes address i mod M, so the order is the
// resolver's preference and a repeated address would load one backend twice. IPv6 gets its
// brackets. Mutations "no dedupe" and "sorted" turn it red. Not covered: the call of
// net.DefaultResolver itself, which a test cannot make answer.
func TestJoinAddresses_KeepsTheResolversOrderAndEachAddressOnce(t *testing.T) {
	got := joinAddresses([]string{"10.0.0.2", "10.0.0.1", "10.0.0.2", "::1"}, "443")

	want := []string{"10.0.0.2:443", "10.0.0.1:443", "[::1]:443"}
	if !slices.Equal(got, want) {
		t.Errorf("addresses %v, want %v", got, want)
	}
}

// Ground: contract — the limits of the connections meet in one report without being mixed:
// changes are added over the connections and kept apart in the entries, and the scalar limit
// fields stay 0 because no one limit is the run's. Link 1 changed its limit twice, link 2 once.
// Mutation "LimitChanges of the first connection" turns it red.
func TestMergeLinks_LimitChangesAreAddedAndKeptApart(t *testing.T) {
	links := []*link{newLink("10.0.0.1:443"), newLink("10.0.0.2:443")}

	for _, limit := range []uint32{4, 5, 6} {
		links[0].tracker.announced(handshake{limit: limit, announced: true})
	}

	for _, limit := range []uint32{1, 2} {
		links[1].tracker.announced(handshake{limit: limit, announced: true})
	}

	got, ok := mergeLinks(links, []string{"a", "b"})
	if !ok {
		t.Fatal("not known after handshakes were read")
	}

	if got.LimitChanges != 3 || got.Each[0].LimitChanges != 2 || got.Each[1].LimitChanges != 1 {
		t.Errorf("changes %d in all, %d and %d by connection, want 3, 2 and 1",
			got.LimitChanges, got.Each[0].LimitChanges, got.Each[1].LimitChanges)
	}

	if got.FirstLimit != 0 || got.LastLimit != 0 {
		t.Errorf("first %d last %d, want 0: one connection's limit is not the run's", got.FirstLimit, got.LastLimit)
	}

	if e := got.Each; e[0].FirstLimit != 4 || e[0].LastLimit != 6 || e[1].FirstLimit != 1 || e[1].LastLimit != 2 {
		t.Errorf("entries %+v, want 4 to 6 and 1 to 2", e)
	}

	if got.InFlightLimit != 8 || !got.InFlightAnnounced {
		t.Errorf("in flight %d (announced %v), want the last limits added, 6 + 2", got.InFlightLimit, got.InFlightAnnounced)
	}
}

// Ground: contract — with two connections server_name still replaces the name TLS verifies
// and sends as SNI, as with one, while :authority stays the target as written. The certificate
// has only api.internal, the target is leettest.test. Mutation "ServerName not passed at two
// connections" turns it red: the connect fails on the name.
func TestConnect_ServerNameStillReplacesTheVerifiedNameAtSeveralConnections(t *testing.T) {
	const (
		target = "leettest.test:443"
		cn     = "api.internal"
	)

	cert, pool := selfSignedFor(t, cn)

	server := []grpc.ServerOption{grpc.Creds(credentials.NewServerTLSFromCert(&cert))}
	bs := pair(t, slowTarget{}, server, server)

	sender := mustConnect(t, 2, bs, Options{Target: target, TLS: true, RootCAs: pool, ServerName: cn})

	for range 2 {
		if _, err := sender.Send(bounded(t), request(time.Now())); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	for _, b := range bs {
		if seen := b.seenAuthorities(); len(seen) != 1 || seen[0] != target {
			t.Errorf("%s saw :authority %q, want %q once", b.addr, seen, target)
		}
	}
}

// Ground: contract — at several connections the target is split into host and port by us, so a
// target with no port is a failed start that names the target and the cause, and asks nothing
// of the resolver. One connection leaves it to grpc-go, which takes port 443.
func TestConnect_ATargetWithNoPortFailsAtSeveralConnections(t *testing.T) {
	_, err := connectTo(t, 2, nil, Options{
		Target: "backends.test",
		Lookup: func(context.Context, string) ([]string, error) {
			t.Error("lookup called for a target with no port")

			return nil, errors.New("not without a port")
		},
	})
	if err == nil {
		t.Fatal("connect succeeded, want an error for a target with no port")
	}

	for _, want := range []string{"connect to backends.test", "missing port"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q, want it to contain %q", err, want)
		}
	}
}
