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
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

// searchSetup is what run prepared for a search: the connection is made and
// the calls carry their bodies.
type searchSetup struct {
	cfg         *config.MasterConfig
	configPath  string
	target      string
	sender      engine.Sender
	calls       []engine.Call
	unchecked   []cli.Unchecked
	maxInFlight int
	// capSet: -max-in-flight was given; otherwise each step gets its own.
	capSet  bool
	connect time.Duration
	json    bool
	// jsonFile is the -json-file path, empty when not given.
	jsonFile    string
	interactive bool
	stepBefore  time.Duration
	// source is where the method came from; nil under -fake.
	source *cli.MethodsSource
	// passport is the config and the machine of the search.
	passport *cli.Passport
}

// searchScreen says whether a search draws its full screen: only in a
// terminal and never with -output json; otherwise the stderr lines.
func searchScreen(interactive, json bool) bool {
	return interactive && !json
}

// searchExitCode is the exit code of a search that ended with outcome:
// findings are 0, an invalid search 2 as an invalid run, a soft stop 3.
func searchExitCode(outcome breakpoint.Outcome) int {
	switch outcome {
	case breakpoint.Invalid:
		return 2
	case breakpoint.Stopped:
		return 3
	default:
		return 0
	}
}

// searchResult is what a finished search returns: its own outcome's error, and
// when that is none, what the pass limits decide.
func searchResult(outcome breakpoint.Outcome, limits []threshold.Result) error {
	switch searchExitCode(outcome) {
	case 2:
		return ErrInvalidRun
	case 3:
		return ErrIncomplete
	}

	return limitsResult(limits)
}

// runSearch runs the breaking-point search over the one call, a step at a
// time on the one connection, and prints its report. The first Ctrl+C ends
// the current run gently and stops the search; the second cuts the run's
// calls off; the third exits without a report.
func runSearch(ctx context.Context, abort context.CancelFunc, stopper *atomic.Pointer[cli.Stopper], s searchSetup,
	stdout, stderr io.Writer,
) error {
	call := s.calls[0]
	maxInFlight := 0
	if s.capSet {
		maxInFlight = s.maxInFlight
	}
	plan := s.cfg.Load.Breakpoint.Plan(call.Timeout, maxInFlight)

	searchCtx, stopSearch := context.WithCancel(ctx)
	defer stopSearch()

	screen := searchScreen(s.interactive, s.json)
	method := strings.TrimPrefix(call.Method, "/")

	// Who ended the search first: a key pressed, or a dataset that ran out.
	var stopWhy stopReason
	stopFor := stopWhy.stopFor

	var current atomic.Pointer[engine.Engine]
	st := newSearchStopper(stderr, screen, abort, stopSearch, stopFor, &current)
	stopper.Store(st)

	maxResponse := searchMaxResponse(s.cfg)

	program, feed, obs, err := openSearchScreen(s, screen, method, plan, st, stderr)
	if err != nil {
		return err
	}

	cores := cli.AvailableCPU()
	runner := &searchRunner{
		ctx: ctx, searchCtx: searchCtx, s: s, screen: screen, maxResponse: maxResponse,
		current: &current, feed: feed, obs: obs, plan: plan, cores: cores, stderr: stderr,
		stopFor: stopFor, stopSearch: stopSearch,
	}

	started := time.Now()
	err = runSearchPlan(screen, program, st, abort, runner.search)
	if err != nil {
		return err
	}

	info, _ := debug.ReadBuildInfo()
	reason := ""
	if runner.res.Outcome == breakpoint.Stopped {
		reason = stopWhy.get()
	}
	report := newBreakpointRun(s, method, started, plan, runner.res, runner.runs, reason, info)
	data, err := printSearchReport(stdout, s, report)
	if err != nil {
		return err
	}
	st.Finish()

	if err := saveJSONFile(s.jsonFile, data); err != nil {
		return err
	}

	return searchResult(runner.res.Outcome, report.Thresholds)
}

func newSearchStopper(stderr io.Writer, screen bool, abort, stopSearch context.CancelFunc,
	stopFor func(string), current *atomic.Pointer[engine.Engine],
) *cli.Stopper {
	return cli.NewStopper(
		func() {
			stopFor(cli.StopRequested)
			if !screen {
				fmt.Fprintln(stderr, "stopping: the search ends with this run; waiting for its calls in flight. Ctrl+C again to cut them off")
			}
			stopSearch()
			if eng := current.Load(); eng != nil {
				eng.Stop()
			}
		},
		func() {
			stopFor(cli.StopRequested)
			if !screen {
				fmt.Fprintln(stderr, "aborting: requests in flight are cut off and counted as aborted. Ctrl+C again to exit without a report")
			}
			abort()
		},
		exitNow,
		time.Second,
	)
}

func searchMaxResponse(cfg *config.MasterConfig) string {
	var maxResponse string
	if raw := cfg.App.RawMaxResponseSize; raw != nil {
		maxResponse = strings.TrimSpace(*raw)
	}

	return maxResponse
}

func openSearchScreen(s searchSetup, screen bool, method string, plan breakpoint.Plan, st *cli.Stopper,
	stderr io.Writer,
) (*tea.Program, *cli.SearchFeed, breakpoint.Observer, error) {
	var (
		program *tea.Program
		feed    *cli.SearchFeed
		obs     breakpoint.Observer
	)
	if screen {
		settings, err := screenSettings()
		if err != nil {
			return nil, nil, breakpoint.Observer{}, err
		}
		program, feed = cli.NewSearchProgram(s.target, method, plan, s.connect, settings, st)
		obs = feed.Observer()
	} else {
		fmt.Fprintln(stderr, cli.PlanLine(plan, s.connect))
	}

	return program, feed, obs, nil
}

// searchRunner runs one step of the search and the plan over those steps.
type searchRunner struct {
	ctx         context.Context
	searchCtx   context.Context
	s           searchSetup
	screen      bool
	maxResponse string
	current     *atomic.Pointer[engine.Engine]
	feed        *cli.SearchFeed
	obs         breakpoint.Observer
	plan        breakpoint.Plan
	cores       float64
	stderr      io.Writer
	stopFor     func(string)
	stopSearch  context.CancelFunc
	runs        []cli.RunReport
	res         breakpoint.Result
}

func (r *searchRunner) step(_ context.Context, rps int, settle, hold time.Duration, need int) (engine.Report, error) {
	calls := slices.Clone(r.s.calls)
	calls[0].Stages = []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: hold}}
	before := clockStep()
	eng, err := engine.New(engine.Options{
		Calls: calls, Sender: r.s.sender, MaxInFlight: need, Warmup: settle, WaitFloor: engine.WaitFloorFor(before),
	})
	if err != nil {
		return engine.Report{}, err
	}
	if r.current.Swap(eng) == nil {
		runStarting(eng)
	}
	if r.searchCtx.Err() != nil {
		eng.Stop()
	}
	// Before Run: the schedule is anchored when the run starts, so the
	// screen taking its time here delays no call.
	if r.feed != nil {
		r.feed.Starting(eng)
	}
	// Each step is a run of its own, with readings of its own.
	meter := cli.NewCPUMeter(cli.ProcessCPU, time.Now, time.Second)
	stopMeter := meter.Start()
	// The run's own context is the command's: a stop of the search ends it
	// gently through Stop, only an abort cuts its calls off.
	runErr := eng.Run(r.ctx)
	stopMeter()
	report := eng.Report()
	run := cli.RunReport{
		Report: report, Unchecked: r.s.unchecked, MaxResponse: r.maxResponse,
		ClockStep: max(before, clockStep()), ClockStepBefore: before,
		GeneratorCPU: meter.Result(report, r.cores),
		Settings:     searchSettings(r.s, need, settle),
	}
	r.runs = append(r.runs, run)
	rp := run.Report
	if !r.screen {
		fmt.Fprintf(r.stderr, "%d rps: sent %d of %d, failed %d\n", rps, rp.Sent, rp.Scheduled, rp.Failed)
	}

	if rp.DatasetExhausted != nil {
		// The same path as Ctrl-C: this run is not judged, the search ends as stopped.
		r.stopFor(cli.StopDatasetExhausted)
		r.stopSearch()
	}

	switch {
	case runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, engine.ErrInFlightCapExceeded):
		return rp, runErr
	case rp.RequestRejected:
		return rp, breakpoint.ErrRequestErrors
	case cli.ClockTooCoarse(run):
		return rp, breakpoint.ErrClockStep
	}

	return rp, nil
}

func (r *searchRunner) search() error {
	var err error
	r.res, err = breakpoint.SearchWith(r.searchCtx, r.plan, r.step, r.obs)
	if err == nil && r.feed != nil {
		r.feed.Done(r.res)
	}

	return err
}

func runSearchPlan(screen bool, program *tea.Program, st *cli.Stopper, abort context.CancelFunc,
	search func() error,
) error {
	if screen {
		return cli.RunLive(program, st, search, abort)
	}

	return search()
}

func newBreakpointRun(s searchSetup, method string, started time.Time, plan breakpoint.Plan,
	res breakpoint.Result, runs []cli.RunReport, reason string, info *debug.BuildInfo,
) cli.BreakpointRun {
	report := cli.BreakpointRun{
		StopReason: reason,
		Target:     s.target, Name: cli.ServiceLabel(s.cfg, s.configPath), Version: versionString(version, info), Method: method,
		StartedAt: started, Plan: plan, Result: res, Runs: aligned(res.Steps, runs, searchSettings(s, s.maxInFlight, plan.Settle)),
		MethodsSource: s.source, Connections: s.cfg.App.Connections, Mode: s.cfg.App.Mode,
		Compression: s.cfg.App.Compression, Passport: s.passport,
	}
	if b := s.cfg.Load.Breakpoint; b != nil && b.RawPass != nil {
		report.Thresholds = []threshold.Result{threshold.EvaluateSearch(res, b.Pass)}
	}

	return report
}

func printSearchReport(stdout io.Writer, s searchSetup, report cli.BreakpointRun) ([]byte, error) {
	var data []byte
	if s.json || s.jsonFile != "" {
		var err error
		if data, err = encodeJSON(func(w io.Writer) error { return cli.WriteBreakpointJSON(w, report) }); err != nil {
			return nil, err
		}
	}
	if s.json {
		if _, err := stdout.Write(data); err != nil {
			return nil, fmt.Errorf("write the JSON report: %w", err)
		}
	} else {
		cli.PrintBreakpoint(stdout, report)
	}

	return data, nil
}

// searchSettings are what a step of the search is started with: the cap, the
// configured connections and the settle as its warm-up.
func searchSettings(s searchSetup, maxInFlight int, settle time.Duration) *cli.RunSettings {
	return &cli.RunSettings{MaxInFlight: maxInFlight, Connections: s.cfg.App.Connections, Warmup: settle, Compression: s.cfg.App.Compression, Mode: s.cfg.App.Mode}
}

// screenSettings are the screen's settings, set up on first use as for a
// plain run.
func screenSettings() (*cli.Settings, error) {
	settings, err := cli.LoadSettings()
	if err != nil {
		return nil, err
	}
	if !settings.Configured() {
		if err := runSetup(settings); err != nil {
			return nil, err
		}
	}

	return settings, nil
}

// aligned pairs each step with the run it made, in order; a step that never
// ran (a cap too low for it) gets its empty engine report.
// A step that never ran carries refused, the settings it was tried with.
func aligned(steps []breakpoint.Step, runs []cli.RunReport, refused *cli.RunSettings) []cli.RunReport {
	out := make([]cli.RunReport, 0, len(steps))
	for i := range steps {
		if steps[i].Report.StartedAt.IsZero() || len(runs) == 0 {
			out = append(out, cli.RunReport{Report: steps[i].Report, Settings: refused})

			continue
		}
		out = append(out, runs[0])
		runs = runs[1:]
	}

	return out
}

// firstRuns is calls as the first run will hand them out: a plain run's calls
// as they are, a search's one call held at its first rate for the plan's hold.
func firstRuns(cfg *config.MasterConfig, calls []engine.Call) []engine.Call {
	if cfg.Load.Breakpoint == nil || len(calls) == 0 {
		return calls
	}

	plan := cfg.Load.Breakpoint.Plan(calls[0].Timeout, 0)
	rates, err := plan.Rates()
	if err != nil || len(rates) == 0 {
		return calls
	}

	first := slices.Clone(calls)
	first[0].Stages = []engine.Stage{{StartRPS: rates[0], TargetRPS: rates[0], Duration: plan.Hold}}

	return first
}
