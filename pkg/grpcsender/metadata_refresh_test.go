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
	"maps"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

// --- helpers --------------------------------------------------------------

// tokenTarget records the authorization and user-agent of every call.
type tokenTarget struct {
	grpc_health_v1.UnimplementedHealthServer

	mu    sync.Mutex
	auth  []string
	agent []string
}

func (s *tokenTarget) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (
	*grpc_health_v1.HealthCheckResponse, error,
) {
	md, _ := metadata.FromIncomingContext(ctx)

	s.mu.Lock()
	s.auth = append(s.auth, strings.Join(md.Get("authorization"), ","))
	s.agent = append(s.agent, strings.Join(md.Get("user-agent"), ","))
	s.mu.Unlock()

	return &grpc_health_v1.HealthCheckResponse{}, nil
}

// last is the authorization and user-agent of the latest call; empty before one.
func (s *tokenTarget) last() (auth, agent string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.auth) == 0 {
		return "", ""
	}

	return s.auth[len(s.auth)-1], s.agent[len(s.agent)-1]
}

// seen is the authorization of every call so far, in order.
func (s *tokenTarget) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.auth...)
}

// bearer is the n-th token of a test: "Bearer t<n>".
func bearer(n int) string { return "Bearer t" + strconv.Itoa(n) }

func tokenNumber(auth string) (int, bool) {
	rest, ok := strings.CutPrefix(auth, "Bearer t")
	if !ok {
		return 0, false
	}

	n, err := strconv.Atoi(rest)

	return n, err == nil
}

// tokenSource is the Read of a test: it counts its calls and answers by call
// number, the first call being 1.
type tokenSource struct {
	mu     sync.Mutex
	calls  int
	script func(call int) (map[string]string, error)
}

func (s *tokenSource) Read() (map[string]string, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()

	return s.script(n)
}

func (s *tokenSource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.calls
}

func (s *tokenSource) every(d time.Duration) *MetadataRefresh {
	return &MetadataRefresh{Every: d, Read: s.Read}
}

// same answers every call with the one authorization value.
func same(auth string) func(int) (map[string]string, error) {
	return func(int) (map[string]string, error) { return map[string]string{"authorization": auth}, nil }
}

// gate holds a Read until the test opens it; the test's end opens it too, so a
// failed assertion leaves nothing blocked.
type gate struct {
	once sync.Once
	ch   chan struct{}
}

func newGate(t *testing.T) *gate {
	t.Helper()

	g := &gate{ch: make(chan struct{})}
	t.Cleanup(g.open)

	return g
}

func (g *gate) open() { g.once.Do(func() { close(g.ch) }) }

func (g *gate) wait() { <-g.ch }

// eventually polls cond until it holds, failing the test at the ceiling.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()

	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()

	for !cond() {
		select {
		case <-deadline.C:
			t.Fatalf("not within 5s: %s", what)
		case <-tick.C:
		}
	}
}

func sendOne(t *testing.T, sender *Sender) {
	t.Helper()

	out, err := sender.Send(bounded(t), request(time.Now()))
	if err != nil || out.Code != "OK" {
		t.Fatalf("send: %v, code %v", err, out.Code)
	}
}

// sendUntilLast sends calls until one carries the authorization want.
func sendUntilLast(t *testing.T, sender *Sender, target *tokenTarget, want string) {
	t.Helper()

	eventually(t, "a call carrying "+want, func() bool {
		sendOne(t, sender)

		auth, _ := target.last()

		return auth == want
	})
}

// statsOf is the sender's refresh counters; a sender that has none fails the test.
// Every snapshot is checked against the invariants the counters keep together.
func statsOf(t *testing.T, sender *Sender) RefreshStats {
	t.Helper()

	st, ok := sender.MetadataRefresh()
	if !ok {
		t.Fatal("MetadataRefresh reports no refresh on a sender that was given one")
	}

	if st.Changed+st.Failures > st.Reads {
		t.Fatalf("stats %+v: changed + failures is more than reads", st)
	}

	if st.FailingAtEnd && st.Failures == 0 {
		t.Fatalf("stats %+v: failing at the end with no failure", st)
	}

	if (st.LastError != "") != (st.Failures > 0) {
		t.Fatalf("stats %+v: last error and failures disagree", st)
	}

	return st
}

// publishedOf is the map calls carry now; a sender with no dynamic metadata fails the test.
func publishedOf(t *testing.T, sender *Sender) map[string]string {
	t.Helper()

	if sender.meta == nil || sender.meta.p == nil {
		t.Fatal("a sender that was given a MetadataRefresh has no dynamic metadata")
	}

	p := sender.meta.p.Load()
	if p == nil {
		t.Fatal("the dynamic metadata holds no map")
	}

	return *p
}

// sameMap reports whether a and b are one map, not two with equal content.
func sameMap(a, b map[string]string) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// Exact texts of the failures the sender itself makes (spec round 1, point 4).
const (
	errKeysChanged  = "the set of metadata keys changed: it is fixed when the run starts"
	errAgentChanged = "the user-agent changed: it is set when the connection is made"
)

// runSenders has n goroutines send calls one after another for d.
func runSenders(t *testing.T, sender *Sender, n int, d time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), d)
	defer cancel()

	var wg sync.WaitGroup

	for range n {
		wg.Go(func() {
			for ctx.Err() == nil {
				callCtx, stop := context.WithTimeout(t.Context(), 5*time.Second)
				out, err := sender.Send(callCtx, request(time.Now()))

				stop()

				if err != nil || out.Code != "OK" {
					t.Errorf("send: %v, code %v", err, out.Code)

					return
				}
			}
		})
	}

	wg.Wait()
}

// ownGoroutines counts the goroutines this package started: the ones whose
// stack says "created by .../pkg/grpcsender.". gRPC's own goroutines come and
// go on their own schedule and are left out; the package's are started and
// ended by Connect and Close alone. It counts a goroutine started by a plain
// go statement in the package: one started through wg.Go, errgroup or
// context.AfterFunc has "created by" naming sync or another package and would
// count as none. Comparisons "none left" use <=, not ==: a goroutine of an
// earlier test may still be dying.
func ownGoroutines() int {
	buf := make([]byte, 1<<20)

	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]

			break
		}

		buf = make([]byte, 2*len(buf))
	}

	count := 0

	for line := range strings.SplitSeq(string(buf), "\n") {
		if strings.HasPrefix(line, "created by ") && strings.Contains(line, "/pkg/grpcsender.") {
			count++
		}
	}

	return count
}

// --- S1: a replaced value is seen by later calls ---------------------------

// Ground: contract — Options.MetadataRefresh: the sender reads again every Every and a call made
// after a replacement carries the new value; the values come in order, never back to an old one.
func TestMetadataRefresh_ACallAfterAReadCarriesTheNewValue(t *testing.T) {
	target := &tokenTarget{}
	opts := listen(t, target)
	opts.Metadata = map[string]string{"authorization": bearer(1)}
	src := &tokenSource{script: func(n int) (map[string]string, error) {
		return map[string]string{"authorization": bearer(n + 1)}, nil
	}}
	opts.MetadataRefresh = src.every(20 * time.Millisecond)
	sender := connected(t, opts)

	// The read after the first one gave t3: the value was replaced twice.
	sendUntilLast(t, sender, target, bearer(3))

	for range 20 {
		sendOne(t, sender)
	}

	prev := 0

	for i, v := range target.seen() {
		n, ok := tokenNumber(v)
		if !ok || n < prev {
			t.Fatalf("call %d carried %q after t%d: a value came back", i, v, prev)
		}

		prev = n
	}

	st := statsOf(t, sender)
	if st.Changed < 2 || st.Failures != 0 || st.FailingAtEnd || st.LastError != "" || st.Reads < st.Changed {
		t.Errorf("stats %+v, want at least 2 changed, no failures, no last error", st)
	}
}

// --- S2: off is absent ------------------------------------------------------

// Ground: contract — without MetadataRefresh the sender starts nothing and reports no refresh;
// with it exactly one goroutine of the package is added, and Close takes it away.
func TestMetadataRefresh_OffAddsNothingToTheProcess(t *testing.T) {
	plain := listen(t, &tokenTarget{})
	plain.Metadata = map[string]string{"authorization": bearer(1)}

	src := &tokenSource{script: same(bearer(2))}
	refreshing := listen(t, &tokenTarget{})
	refreshing.Metadata = map[string]string{"authorization": bearer(1)}
	refreshing.MetadataRefresh = src.every(time.Hour)

	start := ownGoroutines()

	a := connected(t, plain)
	base := ownGoroutines() - start

	if _, ok := a.MetadataRefresh(); ok {
		t.Error("a sender without MetadataRefresh reports one")
	}

	if a.meta != nil {
		t.Error("a sender without MetadataRefresh has dynamic metadata: the call path is not the static one")
	}

	mid := ownGoroutines()
	b := connected(t, refreshing)

	if extra := ownGoroutines() - mid - base; extra != 1 {
		t.Errorf("a refreshing sender started %d goroutines of its own beyond a plain one's, want 1", extra)
	}

	if b.meta == nil || b.meta.p == nil {
		t.Error("a refreshing sender has no dynamic metadata")
	}

	if n := src.count(); n != 0 {
		t.Errorf("Read was called %d times before the first interval", n)
	}

	_ = a.Close()
	_ = b.Close()

	eventually(t, "the package's goroutines gone after Close", func() bool { return ownGoroutines() <= start })
}

// --- S3: a failed read keeps the last good value ---------------------------

// Ground: contract — a Read that fails keeps the published value, counts the failure and keeps
// the last error; a later good read ends FailingAtEnd and leaves the count, whether it brings the
// same value (nothing changed) or a new one (replaced). Reads counts a finished attempt: a Read
// still running is not counted.
func TestMetadataRefresh_AFailedReadKeepsTheLastGoodValue(t *testing.T) {
	const emptyFile = "token file is empty"

	hold := newGate(t)
	holdNew := newGate(t)
	target := &tokenTarget{}
	opts := listen(t, target)
	opts.Metadata = map[string]string{"authorization": bearer(1)}
	src := &tokenSource{script: func(n int) (map[string]string, error) {
		switch {
		case n <= 3:
			return nil, errors.New(emptyFile)
		case n == 4:
			hold.wait()

			return map[string]string{"authorization": bearer(1)}, nil
		case n == 5:
			holdNew.wait()
		}

		return map[string]string{"authorization": bearer(2)}, nil
	}}
	opts.MetadataRefresh = src.every(10 * time.Millisecond)
	sender := connected(t, opts)

	eventually(t, "three failed reads", func() bool { return statsOf(t, sender).Failures >= 3 })

	for range 5 {
		sendOne(t, sender)
	}

	for i, v := range target.seen() {
		if v != bearer(1) {
			t.Errorf("call %d carried %q while the reads failed, want the last good value %q", i, v, bearer(1))
		}
	}

	st := statsOf(t, sender)
	// The fourth Read is stuck on the gate: not a finished attempt, so Reads is exactly 3.
	if st.Failures != 3 || !st.FailingAtEnd || st.LastError != emptyFile || st.Changed != 0 || st.Reads != 3 {
		t.Errorf("stats %+v, want 3 reads, 3 failures, failing at the end, last error %q, nothing changed", st, emptyFile)
	}

	// The fourth Read brings the value already published: the failing ends, nothing is replaced.
	hold.open()
	eventually(t, "the recovery counted", func() bool { return !statsOf(t, sender).FailingAtEnd })

	st = statsOf(t, sender)
	if st.Failures != 3 || st.Changed != 0 || st.LastError != emptyFile {
		t.Errorf("stats %+v after a good read of the same value, want 3 failures, none changed, the last error kept", st)
	}

	if auth, _ := target.last(); auth != bearer(1) {
		t.Errorf("the last call carried %q, want %q until a new value is read", auth, bearer(1))
	}

	// The fifth Read brings a new value: it is replaced and counted.
	holdNew.open()
	sendUntilLast(t, sender, target, bearer(2))
	eventually(t, "the replacement counted", func() bool { return statsOf(t, sender).Changed == 1 })

	st = statsOf(t, sender)
	if st.Failures != 3 || st.FailingAtEnd || st.LastError != emptyFile {
		t.Errorf("stats %+v after the replacement, want 3 failures kept, not failing, the last error kept", st)
	}
}

// --- S4: a read that changes the shape is a failure ------------------------

// Ground: contract — the keys of the map and the user-agent are fixed at Connect (user-agent is a
// connection option): a read with another key set, a missing key, no map or another user-agent is a
// failure, the old value stays; the same shape with a new token is taken, user-agent still once.
func TestMetadataRefresh_AChangedShapeIsAFailure(t *testing.T) {
	hold := newGate(t)
	target := &tokenTarget{}
	opts := listen(t, target)
	opts.Metadata = map[string]string{"authorization": bearer(1), "user-agent": "bench/1"}
	src := &tokenSource{script: func(n int) (map[string]string, error) {
		switch n {
		case 1:
			return map[string]string{"authorization": bearer(9), "user-agent": "bench/1", "x-extra": "1"}, nil
		case 2:
			return map[string]string{"user-agent": "bench/1"}, nil
		case 3:
			return map[string]string{"authorization": bearer(9), "user-agent": "bench/2"}, nil
		case 4:
			return nil, nil
		case 5:
			hold.wait()
		}

		return map[string]string{"authorization": bearer(2), "user-agent": "bench/1"}, nil
	}}
	opts.MetadataRefresh = src.every(10 * time.Millisecond)
	sender := connected(t, opts)

	eventually(t, "four refused reads", func() bool { return statsOf(t, sender).Failures >= 4 })

	for range 3 {
		sendOne(t, sender)
	}

	auth, agent := target.last()
	if auth != bearer(1) || !strings.HasPrefix(agent, "bench/1 ") || strings.Contains(agent, ",") {
		t.Errorf("a call carried %q / %q after refused reads, want %q and one user-agent of bench/1", auth, agent, bearer(1))
	}

	st := statsOf(t, sender)
	if st.Failures != 4 || st.Changed != 0 || !st.FailingAtEnd || st.LastError == "" {
		t.Errorf("stats %+v, want 4 failures, none changed, failing at the end, a last error", st)
	}

	hold.open()
	sendUntilLast(t, sender, target, bearer(2))

	_, agent = target.last()
	if !strings.HasPrefix(agent, "bench/1 ") || strings.Contains(agent, ",") {
		t.Errorf("user-agent %q after the refresh, want bench/1's once", agent)
	}

	if st = statsOf(t, sender); st.Failures != 4 || st.Changed != 1 {
		t.Errorf("stats %+v after the good read, want 4 failures and 1 changed", st)
	}
}

// Ground: contract — each kind of changed shape is one failure with the sender's own text: another
// key set (extra key, missing key, no map) says the keys are fixed, the same keys with another
// user-agent says the user-agent is set at connection; the key set is checked first. The old value
// stays on the calls.
func TestMetadataRefresh_EachChangedShapeIsRefusedWithItsText(t *testing.T) {
	for _, tt := range []struct {
		name string
		bad  map[string]string
		want string
	}{
		{"an extra key", map[string]string{"authorization": bearer(9), "user-agent": "bench/1", "x-extra": "1"}, errKeysChanged},
		{"a missing key", map[string]string{"user-agent": "bench/1"}, errKeysChanged},
		{"no map", nil, errKeysChanged},
		{"another user-agent", map[string]string{"authorization": bearer(9), "user-agent": "bench/2"}, errAgentChanged},
		{"another key set and another user-agent", map[string]string{"x-other": "1", "user-agent": "bench/2"}, errKeysChanged},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hold := newGate(t)
			target := &tokenTarget{}
			opts := listen(t, target)
			opts.Metadata = map[string]string{"authorization": bearer(1), "user-agent": "bench/1"}
			src := &tokenSource{script: func(n int) (map[string]string, error) {
				switch n {
				case 1:
					return tt.bad, nil
				case 2:
					hold.wait()
				}

				return map[string]string{"authorization": bearer(2), "user-agent": "bench/1"}, nil
			}}
			opts.MetadataRefresh = src.every(10 * time.Millisecond)
			sender := connected(t, opts)

			eventually(t, "the refused read", func() bool { return statsOf(t, sender).Failures >= 1 })

			for range 3 {
				sendOne(t, sender)
			}

			st := statsOf(t, sender)
			if st.Failures != 1 || st.LastError != tt.want || st.Changed != 0 || !st.FailingAtEnd {
				t.Errorf("stats %+v, want 1 failure with last error %q, none changed, failing at the end", st, tt.want)
			}

			for i, v := range target.seen() {
				if v != bearer(1) {
					t.Errorf("call %d carried %q after a refused read, want %q", i, v, bearer(1))
				}
			}
		})
	}
}

// --- S5: the same value is not a change ------------------------------------

// Ground: contract — Changed counts reads that gave a map different from the published one: the
// same value read again is a read, not a change.
func TestMetadataRefresh_TheSameValueIsNotAChange(t *testing.T) {
	var current atomic.Value

	current.Store(bearer(1))

	opts := listen(t, &tokenTarget{})
	opts.Metadata = map[string]string{"authorization": bearer(1)}
	src := &tokenSource{script: func(int) (map[string]string, error) {
		auth, _ := current.Load().(string)

		return map[string]string{"authorization": auth}, nil
	}}
	opts.MetadataRefresh = src.every(5 * time.Millisecond)
	sender := connected(t, opts)

	eventually(t, "five reads", func() bool { return statsOf(t, sender).Reads >= 5 })

	if st := statsOf(t, sender); st.Changed != 0 || st.Failures != 0 {
		t.Errorf("stats %+v after reads of the same value, want none changed", st)
	}

	current.Store(bearer(2))
	eventually(t, "the change counted", func() bool { return statsOf(t, sender).Changed == 1 })

	reads := statsOf(t, sender).Reads
	eventually(t, "three more reads", func() bool { return statsOf(t, sender).Reads >= reads+3 })

	if st := statsOf(t, sender); st.Changed != 1 {
		t.Errorf("Changed = %d after the new value was read again, want 1", st.Changed)
	}
}

// --- S6: every connection sees the replacement -----------------------------

// Ground: contract — Connections: N share one published value: a replacement reaches calls on
// every connection. Three backends, one connection each; calls take the connections in turn.
func TestMetadataRefresh_EveryConnectionCarriesTheNewValue(t *testing.T) {
	targets := []*tokenTarget{{}, {}, {}}
	backends := []*backend{
		newBackend(t, "10.0.0.1:443", targets[0]),
		newBackend(t, "10.0.0.2:443", targets[1]),
		newBackend(t, "10.0.0.3:443", targets[2]),
	}
	src := &tokenSource{script: same(bearer(2))}
	sender := mustConnect(t, 3, backends, Options{
		Metadata:        map[string]string{"authorization": bearer(1)},
		MetadataRefresh: src.every(20 * time.Millisecond),
	})

	eventually(t, "the new value on all three connections", func() bool {
		for range 3 {
			sendOne(t, sender)
		}

		for _, target := range targets {
			if auth, _ := target.last(); auth != bearer(2) {
				return false
			}
		}

		return true
	})

	for i, target := range targets {
		if len(target.seen()) == 0 {
			t.Errorf("backend %d saw no call: the calls did not take the connections in turn", i)
		}
	}
}

// --- S7: Close and Connect -------------------------------------------------

// Ground: contract — Close stops the refresh: after it returns Read is not called again.
func TestMetadataRefresh_CloseStopsTheReads(t *testing.T) {
	opts := listen(t, &tokenTarget{})
	opts.Metadata = map[string]string{"authorization": bearer(1)}
	src := &tokenSource{script: same(bearer(1))}
	opts.MetadataRefresh = src.every(10 * time.Millisecond)
	sender := connected(t, opts)

	eventually(t, "three reads", func() bool { return src.count() >= 3 })

	began := time.Now()
	if err := sender.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// An idle goroutine is stopped at once: Close does not sit out the wait it allows a stuck Read.
	if took := time.Since(began); took >= refreshCloseWait/2 {
		t.Errorf("Close took %v with no Read in flight, want well under %v", took, refreshCloseWait)
	}

	reads := src.count()

	st1, ok := sender.MetadataRefresh()
	if !ok || st1.Reads < 3 {
		t.Fatalf("after Close: %+v, %v, want the counters kept with at least 3 reads", st1, ok)
	}

	<-time.After(5 * 10 * time.Millisecond * 2)

	if got := src.count(); got != reads {
		t.Errorf("Read was called %d more times after Close returned", got-reads)
	}

	if st2, _ := sender.MetadataRefresh(); st2 != st1 {
		t.Errorf("counters moved after Close returned: %+v, then %+v", st1, st2)
	}
}

// Ground: contract — Close does not wait for a Read stuck on a hung file system longer than
// refreshCloseWait; once the Read returns the goroutine leaves without publishing or reading again.
func TestMetadataRefresh_CloseDoesNotWaitForAHungRead(t *testing.T) {
	hang := newGate(t)
	entered := make(chan struct{}, 1)
	opts := listen(t, &tokenTarget{})
	opts.Metadata = map[string]string{"authorization": bearer(1)}
	src := &tokenSource{script: func(int) (map[string]string, error) {
		select {
		case entered <- struct{}{}:
		default:
		}

		hang.wait()

		return map[string]string{"authorization": bearer(2)}, nil
	}}
	opts.MetadataRefresh = src.every(10 * time.Millisecond)
	sender := connected(t, opts)

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Read was not called")
	}

	// Close runs in its own goroutine: a Close that waits for ever fails here in 5 s, not at the
	// timeout of the whole test binary.
	closed := make(chan time.Duration, 1)

	go func() {
		began := time.Now()
		_ = sender.Close()
		closed <- time.Since(began)
	}()

	// While Close waits, the counters are readable: they are not behind the lock Close holds. The
	// pause is to let Close take its lock first, not an assertion.
	<-time.After(100 * time.Millisecond)

	readable := make(chan struct{})

	go func() {
		_, _ = sender.MetadataRefresh()

		close(readable)
	}()

	select {
	case <-readable:
	case <-time.After(500 * time.Millisecond):
		t.Error("MetadataRefresh blocked while Close waited for the Read")
	}

	select {
	case took := <-closed:
		if took > refreshCloseWait+time.Second {
			t.Errorf("Close took %v on a hung Read, want about refreshCloseWait (%v) at most", took, refreshCloseWait)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return in 5s on a hung Read")
	}

	reads := src.count()

	hang.open()

	<-time.After(5 * 10 * time.Millisecond * 2)

	if got := src.count(); got != reads {
		t.Errorf("Read was called %d more times after Close and the release", got-reads)
	}

	// The Read that was stuck has returned a value after Close: it is neither counted nor published.
	if st, ok := sender.MetadataRefresh(); !ok || st != (RefreshStats{}) {
		t.Errorf("stats %+v, %v after the stuck Read returned, want nothing counted", st, ok)
	}
}

// Ground: contract — Close waits for a Read that is in flight (within refreshCloseWait): when it
// returns, that Read has finished, and what it returned is neither published nor counted.
func TestMetadataRefresh_CloseWaitsForAReadInFlight(t *testing.T) {
	var finished atomic.Bool

	entered := make(chan struct{}, 1)
	opts := listen(t, &tokenTarget{})
	opts.Metadata = map[string]string{"authorization": bearer(1)}
	src := &tokenSource{script: func(n int) (map[string]string, error) {
		if n > 1 {
			return map[string]string{"authorization": bearer(1)}, nil
		}

		entered <- struct{}{}

		time.Sleep(200 * time.Millisecond)
		finished.Store(true)

		return map[string]string{"authorization": bearer(2)}, nil
	}}
	opts.MetadataRefresh = src.every(10 * time.Millisecond)
	sender := connected(t, opts)

	published := publishedOf(t, sender)

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Read was not called")
	}

	began := time.Now()
	if err := sender.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	took := time.Since(began)

	if !finished.Load() {
		t.Error("Close returned before the Read in flight had finished")
	}

	if took < 100*time.Millisecond || took >= refreshCloseWait {
		t.Errorf("Close took %v, want it to wait for the Read: between 100ms and %v", took, refreshCloseWait)
	}

	if st, ok := sender.MetadataRefresh(); !ok || st != (RefreshStats{}) {
		t.Errorf("stats %+v, %v after Close, want the Read that finished during it not counted", st, ok)
	}

	if !sameMap(publishedOf(t, sender), published) {
		t.Error("a value read while Close waited was published")
	}
}

// Ground: contract — the map a call carries is never changed in place: a replacement is a new
// map; the first value is a copy of Options.Metadata and every published map is a copy of what Read
// returned, so neither the caller's nor the implementor's map is the one the calls read.
func TestMetadataRefresh_ThePublishedMapIsNeverChangedInPlace(t *testing.T) {
	var (
		mu       sync.Mutex
		returned []map[string]string
	)

	opts := listen(t, &tokenTarget{})
	opts.Metadata = map[string]string{"authorization": bearer(1)}
	src := &tokenSource{script: func(n int) (map[string]string, error) {
		m := map[string]string{"authorization": bearer(n + 1)}

		mu.Lock()
		returned = append(returned, m)
		mu.Unlock()

		return m, nil
	}}
	opts.MetadataRefresh = src.every(5 * time.Millisecond)
	sender := connected(t, opts)

	first := publishedOf(t, sender)
	snap := maps.Clone(first)

	eventually(t, "two replacements", func() bool { return statsOf(t, sender).Changed >= 2 })

	if !reflect.DeepEqual(first, snap) {
		t.Errorf("the first published map is now %v, was %v: it was changed in place", first, snap)
	}

	last := publishedOf(t, sender)
	if sameMap(last, first) {
		t.Error("the published map is still the first one after two replacements")
	}

	if sameMap(first, opts.Metadata) {
		t.Error("the first published map is Options.Metadata itself, want a copy made at Connect")
	}

	mu.Lock()
	defer mu.Unlock()

	for i, m := range returned {
		for _, p := range []map[string]string{first, last} {
			if sameMap(p, m) {
				t.Errorf("a published map is the map read %d returned, want a copy made before publishing", i+1)
			}
		}
	}
}

// Ground: contract — Close before Connect starts no goroutine; Close twice is safe with a refresh.
func TestMetadataRefresh_CloseBeforeConnectAndTwiceIsSafe(t *testing.T) {
	opts := listen(t, &tokenTarget{})
	opts.Metadata = map[string]string{"authorization": bearer(1)}
	src := &tokenSource{script: same(bearer(1))}
	opts.MetadataRefresh = src.every(10 * time.Millisecond)

	never := New(opts)
	if err := never.Close(); err != nil {
		t.Errorf("close before connect: %v", err)
	}

	<-time.After(5 * 10 * time.Millisecond)

	if n := src.count(); n != 0 {
		t.Errorf("Read was called %d times by a sender that was never connected", n)
	}

	sender := connected(t, opts)

	eventually(t, "a read", func() bool { return src.count() >= 1 })

	for i := range 2 {
		if err := sender.Close(); err != nil {
			t.Errorf("close %d: %v", i+1, err)
		}
	}
}

// Ground: contract — a Connect that fails leaves no refresh behind: the goroutine starts only once
// the connections are published.
func TestMetadataRefresh_AFailedConnectLeavesNoGoroutine(t *testing.T) {
	src := &tokenSource{script: same(bearer(1))}
	start := ownGoroutines()
	sender := New(Options{
		Target:          closedPort(t),
		Metadata:        map[string]string{"authorization": bearer(1)},
		MetadataRefresh: src.every(10 * time.Millisecond),
	})
	t.Cleanup(func() { _ = sender.Close() })

	if err := connectWithin(withoutDeadline(t), t, sender, 3*time.Second); err == nil {
		t.Fatal("connect to a closed port succeeded")
	}

	<-time.After(5 * 10 * time.Millisecond * 2)

	if n := src.count(); n != 0 {
		t.Errorf("Read was called %d times after a failed Connect", n)
	}

	eventually(t, "no goroutine of the package left", func() bool { return ownGoroutines() <= start })
}

// Ground: contract — a second Connect on a connected sender is a no-op: one goroutine reads, at
// one read per Every, not two.
func TestMetadataRefresh_ASecondConnectStartsNoSecondGoroutine(t *testing.T) {
	const every = 40 * time.Millisecond

	opts := listen(t, &tokenTarget{})
	opts.Metadata = map[string]string{"authorization": bearer(1)}
	src := &tokenSource{script: same(bearer(1))}
	opts.MetadataRefresh = src.every(every)
	sender := connected(t, opts)

	if err := sender.Connect(bounded(t)); err != nil {
		t.Fatalf("second connect: %v", err)
	}

	eventually(t, "three reads", func() bool { return src.count() >= 3 })

	before := src.count()
	began := time.Now()

	<-time.After(10 * every)

	got := src.count() - before
	elapsed := time.Since(began)

	// A ticker gives at most one read per Every: the window holds elapsed/every reads, one more at
	// the edge. Under load the wake-up is late, and the bound follows the time really passed. Two
	// goroutines would give twice as many.
	if limit := int(elapsed/every) + 1; got > limit {
		t.Errorf("%d reads in %v at one per %v (at most %d): more than one goroutine is reading", got, elapsed, every, limit)
	}
}

// --- S8: publication under load and copies --------------------------------

// Ground: contract — with the value replaced every millisecond under eight senders every call
// carries a value that was published, none a mix or an empty one (run under -race: the published
// map is never changed in place).
func TestMetadataRefresh_ManySendersSeeOnlyPublishedValues(t *testing.T) {
	const distinct = 10

	target := &tokenTarget{}
	opts := listen(t, target)
	opts.Metadata = map[string]string{"authorization": bearer(0)}
	src := &tokenSource{script: func(n int) (map[string]string, error) {
		return map[string]string{"authorization": bearer(n % distinct)}, nil
	}}
	opts.MetadataRefresh = src.every(time.Millisecond)
	sender := connected(t, opts)

	runSenders(t, sender, 8, 200*time.Millisecond)

	values := map[string]bool{}

	for _, v := range target.seen() {
		values[v] = true

		if n, ok := tokenNumber(v); !ok || n >= distinct {
			t.Fatalf("a call carried %q, which was never published", v)
		}
	}

	if len(values) < 2 {
		t.Errorf("the calls saw %d distinct values in 200 ms of replacements, want the replacements seen", len(values))
	}
}

// Ground: contract — Read may reuse its map: the sender copies the result before publishing, so
// what the implementor writes into it later never reaches a call. Read here writes "EVIL" into its
// reused map and holds it there before the real value; a sender that published the map itself shows
// EVIL to the calls in that window (and races with them under -race).
func TestMetadataRefresh_AReusedReadResultIsCopied(t *testing.T) {
	const distinct = 10

	shared := map[string]string{"authorization": bearer(0)}
	target := &tokenTarget{}
	opts := listen(t, target)
	opts.Metadata = map[string]string{"authorization": bearer(0)}
	src := &tokenSource{script: func(n int) (map[string]string, error) {
		shared["authorization"] = "EVIL"

		time.Sleep(3 * time.Millisecond)

		shared["authorization"] = bearer(n % distinct)

		return shared, nil
	}}
	opts.MetadataRefresh = src.every(5 * time.Millisecond)
	sender := connected(t, opts)

	runSenders(t, sender, 4, 300*time.Millisecond)

	values := map[string]bool{}

	for _, v := range target.seen() {
		values[v] = true

		if n, ok := tokenNumber(v); !ok || n >= distinct {
			t.Fatalf("a call carried %q: the Read result was published without a copy", v)
		}
	}

	if len(values) < 2 {
		t.Errorf("the calls saw %d distinct values, want the replacements seen", len(values))
	}
}

// Ground: contract — the caller's Options.Metadata and the maps Read returned are not changed by
// the sender (user-agent is taken out for the dial option on a copy).
func TestMetadataRefresh_CallersMapsAreNotChanged(t *testing.T) {
	md := map[string]string{"authorization": bearer(1), "user-agent": "bench/1", "x-api-key": "k"}
	want := maps.Clone(md)

	var (
		mu       sync.Mutex
		returned []map[string]string
		wanted   []map[string]string
	)

	opts := listen(t, &tokenTarget{})
	opts.Metadata = md
	src := &tokenSource{script: func(n int) (map[string]string, error) {
		m := map[string]string{"authorization": bearer(n + 1), "user-agent": "bench/1", "x-api-key": "k"}

		mu.Lock()
		returned = append(returned, m)
		wanted = append(wanted, maps.Clone(m))
		mu.Unlock()

		return m, nil
	}}
	opts.MetadataRefresh = src.every(5 * time.Millisecond)
	sender := connected(t, opts)

	eventually(t, "three reads", func() bool { return src.count() >= 3 })

	if err := sender.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if !reflect.DeepEqual(md, want) {
		t.Errorf("the caller's Options.Metadata is now %v, was %v", md, want)
	}

	mu.Lock()
	defer mu.Unlock()

	for i, m := range returned {
		if !reflect.DeepEqual(m, wanted[i]) {
			t.Errorf("the map of read %d is now %v, was %v", i+1, m, wanted[i])
		}
	}
}

// --- S9: the call path ------------------------------------------------------

// Ground: hot path — gRPC asks the credentials for the metadata of every call: dynamicMetadata
// returns the published map itself, with no lock and no allocation, and the next one after a swap.
func TestDynamicMetadata_ReturnsThePublishedMapWithoutAllocating(t *testing.T) {
	published := map[string]string{"authorization": bearer(1)}
	p := &atomic.Pointer[map[string]string]{}
	p.Store(&published)

	creds := &dynamicMetadata{p: p}

	got, err := creds.GetRequestMetadata(t.Context())
	if err != nil || !reflect.DeepEqual(got, published) {
		t.Fatalf("GetRequestMetadata = %v, %v, want %v", got, err, published)
	}

	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(published).Pointer() {
		t.Error("GetRequestMetadata returned a copy, want the published map itself")
	}

	if creds.RequireTransportSecurity() {
		t.Error("RequireTransportSecurity is true: a plaintext target behind a mesh takes tokens too")
	}

	allocs := testing.AllocsPerRun(1000, func() { _, _ = creds.GetRequestMetadata(t.Context()) })
	if allocs != 0 {
		t.Errorf("GetRequestMetadata allocates %v per call, want 0", allocs)
	}

	next := map[string]string{"authorization": bearer(2)}
	p.Store(&next)

	if got, _ = creds.GetRequestMetadata(t.Context()); got["authorization"] != bearer(2) {
		t.Errorf("after the swap GetRequestMetadata = %v, want %v", got, next)
	}
}

// BenchmarkGetRequestMetadata is the cost of the call path, static against dynamic: the brain's
// reading of it is in the report, not a gate.
// Ground: hot path — the metadata lookup gRPC makes per call.
func BenchmarkGetRequestMetadata(b *testing.B) {
	md := map[string]string{"authorization": bearer(1), "x-api-key": "k"}

	b.Run("static", func(b *testing.B) {
		creds := staticMetadata(md)

		b.ReportAllocs()

		for b.Loop() {
			_, _ = creds.GetRequestMetadata(b.Context())
		}
	})

	b.Run("dynamic", func(b *testing.B) {
		p := &atomic.Pointer[map[string]string]{}
		p.Store(&md)

		creds := &dynamicMetadata{p: p}

		b.ReportAllocs()

		for b.Loop() {
			_, _ = creds.GetRequestMetadata(b.Context())
		}
	})
}

// --- S10: the option itself -------------------------------------------------

// Ground: contract — a MetadataRefresh with no interval above zero or no Read is refused by
// Connect with ErrMetadataRefresh, and the sender stays unconnected.
func TestConnect_MetadataRefreshNeedsAnIntervalAndARead(t *testing.T) {
	read := same(bearer(1))
	src := &tokenSource{script: read}

	for _, tt := range []struct {
		name    string
		refresh *MetadataRefresh
	}{
		{"zero interval", &MetadataRefresh{Every: 0, Read: src.Read}},
		{"negative interval", &MetadataRefresh{Every: -time.Second, Read: src.Read}},
		{"no Read", &MetadataRefresh{Every: time.Second}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := listen(t, &tokenTarget{})
			opts.Metadata = map[string]string{"authorization": bearer(1)}
			opts.MetadataRefresh = tt.refresh

			sender := New(opts)
			t.Cleanup(func() { _ = sender.Close() })

			if err := sender.Connect(bounded(t)); !errors.Is(err, ErrMetadataRefresh) {
				t.Fatalf("connect: %v, want ErrMetadataRefresh", err)
			}

			if _, err := sender.Send(bounded(t), request(time.Now())); !errors.Is(err, ErrNotConnected) {
				t.Errorf("send after the refused connect: %v, want ErrNotConnected", err)
			}
		})
	}

	if n := src.count(); n != 0 {
		t.Errorf("Read was called %d times by a Connect that refused the option", n)
	}
}

// Ground: contract — Options.Metadata is the first value: Connect does not read, no Read happens
// before the first interval, and calls until then carry the value given; the sender does report a
// refresh, with nothing counted yet.
func TestMetadataRefresh_TheFirstValueIsOptionsMetadata(t *testing.T) {
	target := &tokenTarget{}
	opts := listen(t, target)
	opts.Metadata = map[string]string{"authorization": bearer(1)}
	src := &tokenSource{script: same(bearer(2))}
	opts.MetadataRefresh = src.every(time.Hour)
	sender := connected(t, opts)

	for range 3 {
		sendOne(t, sender)
	}

	for i, v := range target.seen() {
		if v != bearer(1) {
			t.Errorf("call %d carried %q, want Options.Metadata's %q", i, v, bearer(1))
		}
	}

	if n := src.count(); n != 0 {
		t.Errorf("Read was called %d times before the first interval", n)
	}

	if st := statsOf(t, sender); st != (RefreshStats{}) {
		t.Errorf("stats %+v before the first interval, want zero", st)
	}
}
