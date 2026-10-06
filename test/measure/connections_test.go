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

package measure

import (
	"context"
	"io"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
	"github.com/yhgrwav/leettest/test/stand"
)

// Two backends of backendCapacity each hold offeredRPS together; one alone
// does not. Round robin is structural: of 1200 calls each connection takes its
// share exactly. The latencies are not, which is why only the loosest bound on
// p99 is asserted.
const (
	backendCapacity = 200
	offeredRPS      = 300
	measureRun      = 4 * time.Second
	measureTimeout  = time.Second
	// A name that resolves to nothing: the addresses come from the test's own lookup.
	backendsName = "backends.test:443"
)

// backends starts two stands of a known capacity on loopback TCP.
func backends(t *testing.T) []*stand.Stand {
	t.Helper()

	var out []*stand.Stand
	for range 2 {
		lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}

		s := stand.StartOn(lis, stand.Capacity(backendCapacity, time.Millisecond))
		t.Cleanup(s.Stop)
		out = append(out, s)
	}

	return out
}

func targets(bs []*stand.Stand) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Target())
	}

	return out
}

// lookupTo is DNS answering the same addresses for any name.
func lookupTo(addrs ...string) func(context.Context, string) ([]string, error) {
	return func(context.Context, string) ([]string, error) { return slices.Clone(addrs), nil }
}

// l4Proxy is one address in front of backends that hands each new TCP
// connection to the next backend in turn and copies bytes both ways: what a
// Kubernetes ClusterIP or an NLB does, blind to the calls inside.
func l4Proxy(t *testing.T, backends ...string) string {
	t.Helper()

	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var (
		next atomic.Uint32
		wg   sync.WaitGroup
	)

	t.Cleanup(func() {
		_ = lis.Close()
		wg.Wait()
	})

	wg.Go(func() {
		for {
			in, err := lis.Accept()
			if err != nil {
				return
			}

			to := backends[int(next.Add(1)-1)%len(backends)]

			wg.Go(func() {
				out, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", to)
				if err != nil {
					_ = in.Close()

					return
				}

				// Either side ending ends both, as a proxy that drops a
				// half-closed connection does.
				done := make(chan struct{}, 2)

				go func() { _, _ = io.Copy(out, in); done <- struct{}{} }()
				go func() { _, _ = io.Copy(in, out); done <- struct{}{} }()

				<-done
				_ = in.Close()
				_ = out.Close()
				<-done
			})
		}
	})

	return lis.Addr().String()
}

// measureSpread offers offeredRPS through a sender with opts and returns how
// many calls each backend received, with the run's report. afterConnect runs
// once the sender is connected and before the first call.
func measureSpread(t *testing.T, bs []*stand.Stand, opts grpcsender.Options, afterConnect func()) ([]int, engine.Report) {
	t.Helper()

	sender := grpcsender.New(opts)
	if err := sender.Connect(t.Context()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	t.Cleanup(func() { _ = sender.Close() })

	if afterConnect != nil {
		afterConnect()
	}

	eng, err := engine.New(engine.Options{
		Calls:       []engine.Call{load(bs[0].Method(), offeredRPS, measureRun, measureTimeout)},
		Sender:      sender,
		MaxInFlight: budget(offeredRPS, measureTimeout),
	})
	if err != nil {
		t.Fatalf("build the engine: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), ceiling)
	defer cancel()

	if err := eng.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	var got []int
	for _, b := range bs {
		got = append(got, len(b.Arrivals()))
	}

	report := eng.Report()
	m := report.Methods[0]
	t.Logf("calls per backend %v; sent %d, timed out %d, unanswered %d, p99 %v (exact %v), connections %+v",
		got, m.Sent, m.TimedOut, m.Unanswered, m.P99.Value, m.P99.Exact, report.Connections)

	return got, report
}

// checkEven asserts the 600/600 split of a run over two backends and that the
// target held it: nothing timed out and the tail stayed short.
func checkEven(t *testing.T, got []int, report engine.Report) {
	t.Helper()

	if !slices.Equal(got, []int{600, 600}) {
		t.Errorf("calls per backend %v, want [600 600]: round robin over 1200 calls is exact", got)
	}

	m := report.Methods[0]
	if m.TimedOut != 0 {
		t.Errorf("%d calls timed out: two backends of %d rps hold %d rps", m.TimedOut, backendCapacity, offeredRPS)
	}
	if !m.P99.Defined || m.P99.Value >= 50*time.Millisecond {
		t.Errorf("p99 %+v, want under 50ms: the load is half of each backend's capacity", m.P99)
	}
}

// Ground: contract — a name with two addresses and two connections loads both, evenly. Today
// one connection takes every call to one of them, which is 1200 and 0. Mutations "always link
// 0" and "every connection dials address 0" turn it red.
func TestConnections_TwoAddressesSplitTheLoad(t *testing.T) {
	bs := backends(t)

	got, report := measureSpread(t, bs, grpcsender.Options{
		Target: backendsName, Connections: 2, Lookup: lookupTo(targets(bs)...),
	}, nil)

	checkEven(t, got, report)

	each := report.Connections.Each
	if len(each) != 2 || each[0].Address != bs[0].Target() || each[1].Address != bs[1].Target() ||
		each[0].Calls != 600 || each[1].Calls != 600 {
		t.Errorf("entries %+v, want 600 calls each, to %v", each, targets(bs))
	}
}

// Ground: contract — one L4 address in front of two backends: DNS shows one address, and each
// connection the proxy takes lands on the next backend. Two connections reach both. Mutation
// "every connection dials address 0" leaves this green: there is only one address; "always
// link 0" turns it red.
func TestConnections_AnL4BalancerSplitsTheLoad(t *testing.T) {
	bs := backends(t)
	proxy := l4Proxy(t, targets(bs)...)

	got, report := measureSpread(t, bs, grpcsender.Options{Target: proxy, Connections: 2}, nil)

	checkEven(t, got, report)
}

// Ground: contract — more connections than addresses: the addresses are taken in turn, so the
// first carries two of three and the report says so: 800 calls to it and 400 to the other.
func TestConnections_ThreeOverTwoAddressesAre2To1(t *testing.T) {
	bs := backends(t)

	got, report := measureSpread(t, bs, grpcsender.Options{
		Target: backendsName, Connections: 3, Lookup: lookupTo(targets(bs)...),
	}, nil)

	if !slices.Equal(got, []int{800, 400}) {
		t.Errorf("calls per backend %v, want [800 400]: three connections over two addresses", got)
	}

	each := report.Connections.Each
	want := []string{bs[0].Target(), bs[1].Target(), bs[0].Target()}
	if len(each) != 3 {
		t.Fatalf("%d entries, want 3", len(each))
	}
	for i, e := range each {
		if e.Address != want[i] || e.Calls != 400 {
			t.Errorf("entry %d: %s with %d calls, want %s with 400", i+1, e.Address, e.Calls, want[i])
		}
	}
}

// Ground: contract — a connection to a backend that is gone is not rerouted to the other: the
// claim is "no reroute", not that every call fails the same way. Backend 1 stops after Connect;
// backend 0 serves exactly its share, 600, and none of connection 2's calls succeeds anywhere.
// Calls on it fail as unreachable, or as timed out while it was being redialled. Mutation "skip
// a link in TRANSIENT_FAILURE" turns it red.
func TestConnections_ADeadBackendIsNotRerouted(t *testing.T) {
	bs := backends(t)

	got, report := measureSpread(t, bs, grpcsender.Options{
		Target: backendsName, Connections: 2, Lookup: lookupTo(targets(bs)...),
	}, bs[1].Stop)

	if got[0] != 600 {
		t.Errorf("backend 1 served %d calls, want exactly 600: its own share, nothing of the dead one's", got[0])
	}

	each := report.Connections.Each
	if len(each) != 2 {
		t.Fatalf("%d entries, want 2", len(each))
	}

	if each[0].Calls != 600 || each[0].Failed != 0 {
		t.Errorf("connection 1: %d calls, %d failed, want 600 and 0: a healthy backend is not touched by the dead one", each[0].Calls, each[0].Failed)
	}
	if each[1].Calls != 600 || each[1].Failed != 600 {
		t.Errorf("connection 2: %d calls, %d failed, want 600 and 600", each[1].Calls, each[1].Failed)
	}

	// Not UnsentTimedOut: it counts the unsent calls held back by the generator on
	// both connections too, and those are nobody's failure.
	m := report.Methods[0]
	if m.Unanswered+m.TimedOut+report.NotSentConnection != each[1].Failed {
		t.Errorf("%d unreachable, %d timed out, %d unsent waiting for a connection: want all %d failures to be one of them",
			m.Unanswered, m.TimedOut, report.NotSentConnection, each[1].Failed)
	}
}
