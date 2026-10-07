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
	capSet      bool
	connect     time.Duration
	json        bool
	interactive bool
	stepBefore  time.Duration
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

	var current atomic.Pointer[engine.Engine]
	st := cli.NewStopper(
		func() {
			if !screen {
				fmt.Fprintln(stderr, "stopping: the search ends with this run; waiting for its calls in flight. Ctrl+C again to cut them off")
			}
			stopSearch()
			if eng := current.Load(); eng != nil {
				eng.Stop()
			}
		},
		func() {
			if !screen {
				fmt.Fprintln(stderr, "aborting: requests in flight are cut off and counted as aborted. Ctrl+C again to exit without a report")
			}
			abort()
		},
		exitNow,
		time.Second,
	)
	stopper.Store(st)

	var maxResponse string
	if raw := s.cfg.App.RawMaxResponseSize; raw != nil {
		maxResponse = strings.TrimSpace(*raw)
	}

	var (
		program *tea.Program
		feed    *cli.SearchFeed
		obs     breakpoint.Observer
	)
	if screen {
		settings, err := screenSettings()
		if err != nil {
			return err
		}
		program, feed = cli.NewSearchProgram(s.target, method, plan, s.connect, settings, st)
		obs = feed.Observer()
	} else {
		fmt.Fprintln(stderr, cli.PlanLine(plan, s.connect))
	}

	var runs []cli.RunReport
	step := func(_ context.Context, rps int, settle, hold time.Duration, need int) (engine.Report, error) {
		calls := slices.Clone(s.calls)
		calls[0].Stages = []engine.Stage{{StartRPS: rps, TargetRPS: rps, Duration: hold}}
		before := clockStep()
		eng, err := engine.New(engine.Options{
			Calls: calls, Sender: s.sender, MaxInFlight: need, Warmup: settle, WaitFloor: engine.WaitFloorFor(before),
		})
		if err != nil {
			return engine.Report{}, err
		}
		if current.Swap(eng) == nil {
			runStarting(eng)
		}
		if searchCtx.Err() != nil {
			eng.Stop()
		}
		// Before Run: the schedule is anchored when the run starts, so the
		// screen taking its time here delays no call.
		if feed != nil {
			feed.Starting(eng)
		}
		// The run's own context is the command's: a stop of the search ends it
		// gently through Stop, only an abort cuts its calls off.
		runErr := eng.Run(ctx)
		run := cli.RunReport{
			Report: eng.Report(), Unchecked: s.unchecked, MaxResponse: maxResponse,
			ClockStep: max(before, clockStep()), ClockStepBefore: before,
		}
		runs = append(runs, run)
		r := run.Report
		if !screen {
			fmt.Fprintf(stderr, "%d rps: sent %d of %d, failed %d\n", rps, r.Sent, r.Scheduled, r.Failed)
		}

		switch {
		case runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, engine.ErrInFlightCapExceeded):
			return r, runErr
		case r.RequestRejected:
			return r, breakpoint.ErrRequestErrors
		case cli.ClockTooCoarse(run):
			return r, breakpoint.ErrClockStep
		}

		return r, nil
	}

	started := time.Now()
	var res breakpoint.Result
	search := func() error {
		var err error
		res, err = breakpoint.SearchWith(searchCtx, plan, step, obs)
		if err == nil && feed != nil {
			feed.Done(res)
		}

		return err
	}
	var err error
	if screen {
		err = cli.RunLive(program, st, search, abort)
	} else {
		err = search()
	}
	if err != nil {
		return err
	}

	info, _ := debug.ReadBuildInfo()
	report := cli.BreakpointRun{
		Target: s.target, Version: versionString(version, info), Method: method,
		StartedAt: started, Plan: plan, Result: res, Runs: aligned(res.Steps, runs),
	}
	if s.json {
		if err := cli.WriteBreakpointJSON(stdout, report); err != nil {
			return fmt.Errorf("write the JSON report: %w", err)
		}
	} else {
		cli.PrintBreakpoint(stdout, report)
	}
	st.Finish()

	switch searchExitCode(res.Outcome) {
	case 2:
		return ErrInvalidRun
	case 3:
		return ErrIncomplete
	}

	return nil
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
func aligned(steps []breakpoint.Step, runs []cli.RunReport) []cli.RunReport {
	out := make([]cli.RunReport, 0, len(steps))
	for i := range steps {
		if steps[i].Report.StartedAt.IsZero() || len(runs) == 0 {
			out = append(out, cli.RunReport{Report: steps[i].Report})

			continue
		}
		out = append(out, runs[0])
		runs = runs[1:]
	}

	return out
}
