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
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"
)

// ErrMetadataRefresh is a MetadataRefresh that cannot work: Every not above
// zero, or no Read. Connect fails with it.
var ErrMetadataRefresh = errors.New("invalid metadata refresh")

// refreshCloseWait is how long Close waits for the refresh goroutine to end: a
// Read stuck on a hung file system must not hang Close after the report.
const refreshCloseWait = time.Second

// MetadataRefresh makes the sender read Options.Metadata again during the run.
type MetadataRefresh struct {
	// Every is the interval between reads, above zero. The first read is Every
	// after Connect: Options.Metadata is the first value.
	Every time.Duration
	// Read returns the whole map, lowercase keys, a fresh map each call. It
	// must not return values in its error.
	Read func() (map[string]string, error)
}

// RefreshStats counts what the refresh did since Connect.
type RefreshStats struct {
	// Reads are the attempts after the start; Changed the successful ones that
	// gave a map different from the published one; Failures the unsuccessful.
	Reads, Changed, Failures int
	// FailingAtEnd is true when the last attempt failed.
	FailingAtEnd bool
	// LastError is the text of the last failure; it stays after a recovery.
	LastError string
}

// MetadataRefresh reports what the refresh has done so far; false when the
// sender has none. Safe to call at any moment, also during Close: the counters
// have a lock of their own.
func (s *Sender) MetadataRefresh() (RefreshStats, bool) {
	return s.counters.snapshot(), s.opts.MetadataRefresh != nil
}

// dynamicMetadata is the metadata of every call when it can be replaced: one
// object for all connections, the published map never changed.
type dynamicMetadata struct {
	p *atomic.Pointer[map[string]string]
}

// GetRequestMetadata is on the path of every call: one atomic load, no lock,
// no allocation. The map is the published one itself, which nothing changes.
func (d *dynamicMetadata) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return *d.p.Load(), nil
}

// RequireTransportSecurity is false for the same reason as staticMetadata's.
func (*dynamicMetadata) RequireTransportSecurity() bool { return false }

// Texts of the failures the sender itself makes; no key or value is in them.
const (
	failKeys  = "the set of metadata keys changed: it is fixed when the run starts"
	failAgent = "the user-agent changed: it is set when the connection is made"
)

// refreshCounters is RefreshStats under a lock of its own, never Sender.mu: Close
// holds that one while it waits for the refresh, and the report reads the
// counters meanwhile. stopped is set by Close; from then on nothing is counted
// or published, which is what keeps a Read that returns late out of the report.
type refreshCounters struct {
	mu      sync.Mutex
	stopped bool
	stats   RefreshStats
}

func (c *refreshCounters) snapshot() RefreshStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.stats
}

// refresher is the goroutine that reads again, and what it checks a read against.
type refresher struct {
	every time.Duration
	read  func() (map[string]string, error)
	meta  *dynamicMetadata
	// keys, agent and hasAgent are the shape fixed at Connect: user-agent is
	// a dial option and cannot follow a read.
	keys     map[string]struct{}
	agent    string
	hasAgent bool

	counters *refreshCounters
	cancel   context.CancelFunc
	done     chan struct{}
}

// newRefresher validates Options.MetadataRefresh and builds the metadata every
// call will carry. It reads nothing: Options.Metadata is the first value, and
// the copy of it is what is published, so the caller's map is never the one the
// calls read.
func (s *Sender) newRefresher() (*refresher, error) {
	cfg := s.opts.MetadataRefresh
	if cfg == nil {
		return nil, nil
	}

	if cfg.Every <= 0 {
		return nil, fmt.Errorf("%w: every is %v, want above zero", ErrMetadataRefresh, cfg.Every)
	}

	if cfg.Read == nil {
		return nil, fmt.Errorf("%w: no Read", ErrMetadataRefresh)
	}

	perCall, agent, hasAgent := splitUserAgent(s.opts.Metadata)

	first := maps.Clone(perCall)
	if first == nil {
		first = map[string]string{}
	}

	keys := make(map[string]struct{}, len(first))
	for k := range first {
		keys[k] = struct{}{}
	}

	p := &atomic.Pointer[map[string]string]{}
	p.Store(&first)

	return &refresher{
		every:    cfg.Every,
		read:     cfg.Read,
		meta:     &dynamicMetadata{p: p},
		keys:     keys,
		agent:    agent,
		hasAgent: hasAgent,
		counters: &s.counters,
		done:     make(chan struct{}),
	}, nil
}

// begin starts the goroutine. It is a plain go statement of this package: the
// goroutine count a test makes by the "created by" line relies on it.
func (r *refresher) begin() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	go r.run(ctx)
}

// stop ends the refresh: nothing is counted or published after it, and it waits
// for the goroutine for at most refreshCloseWait. A Read still stuck then is
// left to return on its own; it finds the refresh stopped and does nothing.
func (r *refresher) stop() {
	r.counters.mu.Lock()
	r.counters.stopped = true
	r.counters.mu.Unlock()

	r.cancel()

	wait := time.NewTimer(refreshCloseWait)
	defer wait.Stop()

	select {
	case <-r.done:
	case <-wait.C:
	}
}

func (r *refresher) run(ctx context.Context) {
	defer close(r.done)

	tick := time.NewTicker(r.every)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}

		if ctx.Err() != nil {
			return
		}

		r.once()
	}
}

// once makes one attempt and counts it when it is over, not when it starts: a
// Read that has not returned is not a finished read.
func (r *refresher) once() {
	read, err := r.read()

	var (
		next   map[string]string
		reason string
	)

	switch {
	case err != nil:
		reason = err.Error()
		if reason == "" {
			reason = "read failed"
		}
	default:
		next, reason = r.check(read)
	}

	c := r.counters

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stopped {
		return
	}

	c.stats.Reads++

	if reason != "" {
		c.stats.Failures++
		c.stats.FailingAtEnd = true
		c.stats.LastError = reason

		return
	}

	c.stats.FailingAtEnd = false

	if !maps.Equal(next, *r.meta.p.Load()) {
		r.meta.p.Store(&next)
		c.stats.Changed++
	}
}

// check turns what Read returned into the map to publish: a copy, since the
// implementor may reuse its own, without user-agent, which goes out as a dial
// option. A read of another shape gives the text of the failure instead: the key
// set first, then the user-agent.
func (r *refresher) check(read map[string]string) (next map[string]string, reason string) {
	next = maps.Clone(read)
	agent, hasAgent := next["user-agent"]
	delete(next, "user-agent")

	if len(next) != len(r.keys) {
		return nil, failKeys
	}

	for k := range next {
		if _, ok := r.keys[k]; !ok {
			return nil, failKeys
		}
	}

	if hasAgent != r.hasAgent || agent != r.agent {
		return nil, failAgent
	}

	return next, ""
}
