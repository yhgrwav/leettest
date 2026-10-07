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

package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrNoCalls = errors.New("engine has no calls")
	// The report is per method: two calls to one would merge into a row
	// stating one call's rate and timeout for both.
	ErrDuplicateMethod = errors.New("method appears in more than one call")
	ErrFakeFailure     = errors.New("fake sender failure")
)

type Call struct {
	Method string
	// Payload is the request when the call has no dataset.
	Payload []byte
	// Payloads are the requests of a dataset call, one record each; request
	// number S goes out with Payloads[S mod len]. Nil or empty: Payload.
	Payloads [][]byte
	// Dataset is set for a call whose Payloads are a file's records: what the
	// report says of them. A pointer, so that the Call stays small to copy.
	Dataset      *DatasetRef
	Timeout      time.Duration
	Stages       []Stage
	KeepResponse bool
}

// DatasetRef is what the engine knows of a call's dataset besides the records.
type DatasetRef struct {
	// File is the path as the config wrote it; the report carries it.
	File string
	// Counter is S of the call, shared by every run that carries this Call
	// (copies of it included); nil gives each run a count of its own.
	Counter *RecordCounter
}

// RecordCounter counts the requests of one call the dispatcher has handed out,
// for as long as it lives: a run continues the count of the one before it.
// STUB of the red commit: it never counts.
type RecordCounter struct {
	n atomic.Int64
}

// NewRecordCounter is a counter at zero.
func NewRecordCounter() *RecordCounter {
	return &RecordCounter{}
}

// Handed is how many requests have been handed out so far.
func (c *RecordCounter) Handed() int {
	return int(c.n.Load())
}

type Options struct {
	Calls       []Call
	Sender      Sender
	MaxInFlight int
	Warmup      time.Duration
	// WaitFloor replaces StreamWaitFloor when set: see WaitFloorFor.
	WaitFloor time.Duration
}

type Engine struct {
	opts  Options
	stats *Stats
	pool  *WorkerPool

	// targets is each method's highest planned rate, worked out once: the
	// live view asks for it on every frame.
	targets map[string]int

	stopOnce sync.Once
	stopped  chan struct{}
	// incomplete is set when the run ended before its plan: stopped or aborted.
	incomplete atomic.Bool
	// capHit is set when the run ended on the in-flight cap.
	capHit atomic.Pointer[InFlightCapError]
	// startedAt is when Run started, for the moment of a cap hit.
	startedAt time.Time
	// links is the address of each connection when the sender has two or more,
	// asked once in New; nil otherwise.
	links []string
}

// CheckOptions validates everything about the calls and limits that New does,
// without a sender. A caller can reject a bad config before paying for a
// connection, and build the engine once the request bodies are ready.
func CheckOptions(opts Options) error {
	if len(opts.Calls) == 0 {
		return ErrNoCalls
	}
	if opts.MaxInFlight < 1 {
		return fmt.Errorf("%w: %d", ErrInvalidInFlightCap, opts.MaxInFlight)
	}
	first := make(map[string]int, len(opts.Calls))
	for i, call := range opts.Calls {
		if j, ok := first[call.Method]; ok {
			return fmt.Errorf("call %d: %w: %s, as call %d", i, ErrDuplicateMethod, call.Method, j)
		}
		first[call.Method] = i
	}

	return checkInFlightBudget(opts.Calls, opts.MaxInFlight)
}

func New(opts Options) (*Engine, error) {
	if opts.Sender == nil {
		return nil, ErrNoSender
	}
	if err := CheckOptions(opts); err != nil {
		return nil, err
	}

	e := &Engine{
		opts:    opts,
		stats:   NewStats(),
		pool:    NewWorkerPool(opts.Sender, opts.MaxInFlight),
		stopped: make(chan struct{}),
	}
	if opts.WaitFloor > 0 {
		e.stats.SetWaitFloor(opts.WaitFloor)
	}
	e.targets = e.targetRates()

	// The sender has connected by now: it is asked for its connections once.
	if reporter, ok := opts.Sender.(LinkReporter); ok {
		if links := reporter.Links(); len(links) > 1 {
			e.links = slices.Clone(links)
			e.stats.SetLinks(len(links))
		}
	}

	return e, nil
}

// Stop ends the run gently: nothing new is scheduled, and calls in flight run
// to their own deadline and are recorded as usual. Run then returns nil, but
// the report is marked incomplete. Cancelling Run's context aborts instead.
// Safe to call at any time and more than once.
func (e *Engine) Stop() {
	e.stopOnce.Do(func() { close(e.stopped) })
}

// Snapshot is SnapshotInto with fresh memory: for an occasional look.
func (e *Engine) Snapshot() Snapshot {
	var snapshot Snapshot
	e.SnapshotInto(&snapshot, NewLiveBuffer(), true)

	return snapshot
}

// SnapshotInto is Stats.SnapshotInto plus what the engine knows: calls in
// flight, the planned length, each method's target rate.
func (e *Engine) SnapshotInto(dst *Snapshot, buf *LiveBuffer, percentiles bool) {
	e.stats.SnapshotInto(dst, buf, percentiles)
	dst.InFlight = e.pool.inFlightCount()
	dst.Total = e.plannedDuration()

	for i := range dst.Methods {
		dst.Methods[i].TargetRPS = e.targets[dst.Methods[i].Method]
	}
}

func (e *Engine) targetRates() map[string]int {
	rates := make(map[string]int, len(e.opts.Calls))

	for _, call := range e.opts.Calls {
		for _, stage := range call.Stages {
			if stage.TargetRPS > rates[call.Method] {
				rates[call.Method] = stage.TargetRPS
			}
		}
	}

	return rates
}

// Calls returns the calls this engine was built for.
func (e *Engine) Calls() []Call {
	return e.opts.Calls
}

func (e *Engine) plannedDuration() time.Duration {
	var longest time.Duration

	for _, call := range e.opts.Calls {
		var total time.Duration
		for _, stage := range call.Stages {
			total += stage.Duration
		}
		if total > longest {
			longest = total
		}
	}

	return longest
}

// timelineSlack covers what a lagging generator begins past the plan. The
// drain ends within the longest timeout and an abort records the abort moment,
// so only a generator more than this far behind lands outside — and a run
// that far behind is invalid by its lag anyway.
const timelineSlack = 10 * time.Second

func (e *Engine) longestTimeout() time.Duration {
	var longest time.Duration
	for _, call := range e.opts.Calls {
		longest = max(longest, call.Timeout)
	}

	return longest
}

// Report is meant for after Run returns: it copies the per-second timelines
// under the lock every worker records through. For live data use Snapshot.
func (e *Engine) Report() Report {
	report := e.stats.Report()
	report.Incomplete = e.incomplete.Load()
	report.Planned = e.plannedDuration()
	for _, call := range e.opts.Calls {
		report.Scheduled += ScheduledAfter(call.Stages, e.opts.Warmup)
	}
	report.StartedAt = e.startedAt

	report.Connections = e.connections()

	if hit := e.capHit.Load(); hit != nil {
		report.CapHit = &CapHit{At: hit.At.Sub(e.startedAt), Unsent: 1, OverDeadline: hit.OverDeadline}
	}

	for i := range report.Methods {
		m := &report.Methods[i]
		for _, call := range e.opts.Calls {
			if call.Method != m.Method {
				continue
			}

			m.Timeout = call.Timeout
			m.RPSLow, m.RPSHigh = ratesOver(call.Stages, 0, math.MaxInt64)
			if m.SilentFrom != nil {
				m.SilentPlannedLow, m.SilentPlannedHigh = ratesInWindow(call.Stages, *m.SilentFrom)
			}
		}
	}

	return report
}

// connections is what the sender says about its connections, with the engine's
// count of what each of several carried joined in. Nil when the sender tells
// nothing and has one connection.
func (e *Engine) connections() *Connections {
	var (
		conns Connections
		known bool
	)

	if r, ok := e.opts.Sender.(ConnectionReporter); ok {
		conns, known = r.Connections()
	}

	counts := e.stats.LinkCounts()
	if counts == nil {
		if known {
			return &conns
		}

		return nil
	}

	if !known {
		// A sender that cannot vouch for its handshakes, such as one with
		// credentials of the caller's, still has these connections: they are
		// shown with no limit, which was not read.
		conns = Connections{Open: len(e.links)}

		for _, addr := range e.links {
			if !slices.Contains(conns.Resolved, addr) {
				conns.Resolved = append(conns.Resolved, addr)
			}
		}
	}

	// Never the sender's own slice: the engine adds its counts to a copy.
	each := make([]LinkReport, len(counts))
	if len(conns.Each) == len(counts) {
		copy(each, conns.Each)
	}

	for i, c := range counts {
		if each[i].Address == "" {
			each[i].Address = e.links[i]
		}

		each[i].Calls, each[i].Failed, each[i].StreamWaited, each[i].P99 = c.Calls, c.Failed, c.StreamWaited, c.P99
	}

	conns.Each = each

	return &conns
}

// ScheduledAfter counts the ticks the schedule of stages places at or after
// warmup: the calls the run measures.
func ScheduledAfter(stages []Stage, warmup time.Duration) int {
	var at time.Duration
	n := 0
	for _, stage := range stages {
		for i := 0; ; i++ {
			offset := time.Duration(i) * time.Second / time.Duration(stage.TargetRPS)
			if offset >= stage.Duration {
				break
			}
			if at+offset >= warmup {
				n++
			}
		}
		at += stage.Duration
	}

	return n
}

// ratesOver is the lowest and highest planned rate of the stages that overlap
// [from, to).
func ratesOver(stages []Stage, from, to time.Duration) (low, high int) {
	var at time.Duration

	first := true
	for _, stage := range stages {
		start, end := at, at+stage.Duration
		at = end
		if end <= from || start >= to {
			continue
		}

		lo, hi := min(stage.StartRPS, stage.TargetRPS), max(stage.StartRPS, stage.TargetRPS)
		if first {
			low, high, first = lo, hi, false

			continue
		}
		low, high = min(low, lo), max(high, hi)
	}

	return low, high
}

// ratesInWindow is the lowest and highest planned rate of the stages that
// overlap second max(0, from-1); 0, 0 when none does or their rate is 0.
func ratesInWindow(stages []Stage, from int) (low, high int) {
	second := time.Duration(max(0, from-1)) * time.Second
	return ratesOver(stages, second, second+time.Second)
}

// Run executes the plan once; an Engine is not reused. Cancelling ctx aborts
// the run, Stop ends it gently.
func (e *Engine) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	requests := make(chan Request, e.opts.MaxInFlight)
	results := make(chan Result, e.opts.MaxInFlight)

	methods := make([]string, 0, len(e.opts.Calls))
	for _, call := range e.opts.Calls {
		methods = append(methods, call.Method)
	}
	e.stats.Reserve(e.plannedDuration()+e.longestTimeout()+timelineSlack, methods...)
	// Measured before moment zero: on a coarse clock it takes milliseconds.
	dispatcher := NewDispatcher(e.opts.Calls)
	e.startedAt = time.Now()
	dispatcher.start = e.startedAt
	e.stats.Start(e.startedAt, e.opts.Warmup)
	// Nothing is scheduled past the plan, so rates never divide by more than
	// it; a stop reports an earlier moment below.
	e.stats.EndSending(e.startedAt.Add(e.plannedDuration()))

	scheduleCtx, stopScheduling := context.WithCancel(runCtx)
	defer stopScheduling()

	go func() {
		select {
		case <-e.stopped:
			e.stats.EndSending(time.Now())
			stopScheduling()
		case <-scheduleCtx.Done():
			e.stats.EndSending(time.Now())
		}
	}()

	var (
		schedulers  sync.WaitGroup
		scheduleMu  sync.Mutex
		scheduleErr error
	)

	schedulers.Add(1)

	go func() {
		defer schedulers.Done()

		err := dispatcher.Run(scheduleCtx, requests)
		if err != nil && ctx.Err() == nil && isStopped(e.stopped) {
			// Stopped by Stop, not by a failure: the plan was cut short.
			e.incomplete.Store(true)
			return
		}
		if err != nil {
			scheduleMu.Lock()
			scheduleErr = err
			scheduleMu.Unlock()
		}
	}()

	go func() {
		schedulers.Wait()
		close(requests)
	}()

	var collector sync.WaitGroup

	collector.Add(1)
	go func() {
		defer collector.Done()

		for result := range results {
			e.stats.Record(result)
		}
	}()

	sendErr := e.pool.Run(runCtx, requests, results)
	cancel()

	close(results)
	collector.Wait()

	e.stats.Finish(time.Now())

	if ctx.Err() != nil {
		e.incomplete.Store(true)
	}

	if capErr := (*InFlightCapError)(nil); errors.As(sendErr, &capErr) {
		e.capHit.Store(capErr)
		e.incomplete.Store(true)
	}

	if sendErr != nil {
		return sendErr
	}

	scheduleMu.Lock()
	defer scheduleMu.Unlock()

	return scheduleErr
}

func isStopped(stopped <-chan struct{}) bool {
	select {
	case <-stopped:
		return true
	default:
		return false
	}
}
