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

// Command leettest runs a load test described by a YAML config against a
// gRPC target, prints a report and exits non-zero when the run fails.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/clock"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/errstop"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

const defaultMaxInFlight = 5000

// ErrIncomplete says the run ended before its plan. The report is printed and
// honest, but it covers less than was asked for, so the exit code is not zero:
// a pipeline must not pass on a three-minute run of a ten-minute plan.
// ErrInvalidRun says the run's numbers say nothing about the target: the
// generator hit its own cap. The report prints the verdict; the code carries
// it to a pipeline.
var ErrInvalidRun = errors.New("the run is invalid: its numbers do not describe the target")

var ErrIncomplete = errors.New("the run stopped before its planned end; the report covers only the part that ran")

// ErrThresholdsFailed says the run finished and a limit in pass: was not met.
var ErrThresholdsFailed = errors.New("a pass limit was not met")

// ErrCompareUndecided says leettest compare could not decide: a limit is
// unknown, or the reports are not comparable (the same code, its own text).
var ErrCompareUndecided = errors.New("compare: a limit could not be decided")

// ErrCompareFailed says a limit given to leettest compare was not met.
var ErrCompareFailed = errors.New("compare: a limit was not met")

// exitNow is the way out that depends on nothing: the third stop, or an abort
// that has not finished in time.
var exitNow = func() {
	fmt.Fprintln(os.Stderr, "leettest: aborted without a report")
	sig, _ := lastSignal.Load().(os.Signal)
	discardJSONFile()
	exitProcess(abortCode(sig))
}

// exitProcess is os.Exit; tests replace it to see the exit without leaving.
var exitProcess = os.Exit

// lastSignal is what asked the run to stop, so an abort without a report
// exits the way that signal would have: a script reads 143 from an
// orchestrator's SIGTERM and 130 from a person's Ctrl+C.
var lastSignal atomic.Value

// stderrIsTerminal says whether w is the process terminal; tests replace it
// to stand in for one.
var stderrIsTerminal = func(w io.Writer) bool {
	return w == io.Writer(os.Stderr) && cli.Interactive()
}

// runSetup is the first-run dialog; tests replace it to see whether it was asked.
var runSetup = cli.RunSetup

// liveView says whether the run draws the live screen: on a terminal, unless
// -plain asks for the progress lines.
func liveView(plain, terminal bool) bool {
	return !plain && terminal
}

// runStarting is called once presses go to the stopper; tests use it to press
// during the run rather than during the connection.
var runStarting = func(*engine.Engine) {}

func main() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	stops, aborts := make(chan struct{}), make(chan struct{})
	go func() {
		for sig := range signals {
			lastSignal.Store(sig)

			if sig == syscall.SIGTERM {
				aborts <- struct{}{}
			} else {
				stops <- struct{}{}
			}
		}
	}()

	err := run(context.Background(), stops, aborts, os.Args[1:], os.Stdout, os.Stderr)
	signal.Stop(signals)

	if err != nil && !errors.Is(err, flag.ErrHelp) {
		printError(os.Stderr, err)
	}

	os.Exit(exitCode(err))
}

// printError writes a failed run's error as it came: the final screen's short
// verdict sends the reader here for the details.
func printError(w io.Writer, err error) {
	fmt.Fprintf(w, "leettest: %v\n", err)
}

// exitCode maps the outcome onto codes a pipeline can tell apart. Whether the
// target is fast enough is among them only for a run with a pass: section; a
// finished run without one exits 0 however the target answered.
//
//	0   the plan ran to its end and the report is complete, and every limit
//	    in pass: held
//	1   no report as asked: the run never started (bad flags, bad config,
//	    no connection), or the -json-file write failed; for -dry-run also a
//	    call that did not succeed, and a stop in the middle (nothing is
//	    measured, so no other code applies)
//	2   the run is invalid: its numbers do not describe the target, or a
//	    limit in pass: cannot be decided from them
//	3   the run stopped before its planned end; the report covers less
//	4   the run finished and a limit in pass: was not met
//	130 aborted without a report after Ctrl+C
//	143 aborted without a report after SIGTERM
//
// The codes are ranked, not summed: a run that is both invalid and short
// reports 2, because numbers that do not describe the target are worse news
// than covering less of the plan. A missed limit on a finished run is 4 even
// beside a limit that could not be decided: a miss is a finding, an unknown
// is not. 130 and 143 are not outcomes of a run at all — they are the exit
// that prints nothing, so a stop that did print a report is 3 whichever key
// ended it.
//
// leettest compare uses 0, 1, 2 and 4 with the same meaning: 2 is reports
// that cannot be compared or a limit that cannot be decided, 4 a limit missed.
func exitCode(err error) int {
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, ErrInvalidRun), errors.Is(err, ErrCompareUndecided):
		return 2
	case errors.Is(err, ErrIncomplete):
		return 3
	case errors.Is(err, ErrThresholdsFailed), errors.Is(err, ErrCompareFailed):
		return 4
	default:
		return 1
	}
}

// abortCode is the shell's convention for a process killed by a signal:
// 128 plus the signal's number.
func abortCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}

	return 130
}

// run is the whole command. The live view is used only when stderr is the
// process terminal, so tests passing their own writers always get plain output.
//
// Each value on stops is one press of Ctrl+C, each value on aborts one SIGTERM.
// Before the run starts either cancels the connection; during the run they go
// to the three-stage stopper.
func run(ctx context.Context, stops, aborts <-chan struct{}, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "compare" {
		return runCompare(args[1:], stdout, stderr)
	}

	ctx, abort := context.WithCancel(ctx)
	defer abort()

	var stopper atomic.Pointer[cli.Stopper]
	// SIGTERM comes from an orchestrator, not a keyboard: no Ctrl+C hint then.
	var terminated atomic.Bool

	finished := make(chan struct{})
	defer close(finished)

	go func() {
		for {
			select {
			case <-finished:
				return
			case <-stops:
				if s := stopper.Load(); s != nil {
					s.Press()
				} else {
					abort()
				}
			case <-aborts:
				terminated.Store(true)
				if s := stopper.Load(); s != nil {
					s.Abort()
				} else {
					abort()
				}
			}
		}
	}()

	terminal := stderrIsTerminal(stderr)
	// The stopper writes from the signal goroutine while run writes too.
	stderr = &lockedWriter{w: stderr}

	flags, done, err := parseFlags(args, terminal, stdout, stderr)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	cfg, passport, err := loadConfig(flags.configPath, flags.flagLabels)
	if err != nil {
		return err
	}

	sender, grpcSender, target, err := buildSender(cfg, flags.fake, flags.fakeDelay, flags.fakeJitter, flags.fakeFail)
	if err != nil {
		return err
	}

	if flags.dryRun {
		return runDryRun(ctx, stdout, stderr, cfg, sender, grpcSender, flags)
	}

	// Measure, run, measure: the floor of "waited" is set by the step before
	// the run, the report prints the larger of the two.
	stepBefore := clockStep()

	opts, calls, search, err := engineOptions(cfg, sender, flags.maxInFlight, stepBefore)
	if err != nil {
		return err
	}

	// Created before the network: a path that cannot be used ends the command
	// before any call. Removed on every way out that has no report.
	if flags.jsonFile != "" {
		if reserveErr := reserveJSONFile(flags.jsonFile); reserveErr != nil {
			return reserveErr
		}
		defer discardJSONFile()
	}

	// Methods nothing could be checked against: named again with the report,
	// where a full-screen run does not scroll them away.
	var (
		unchecked []cli.Unchecked
		// Where the methods came from; nil when none was resolved (-fake).
		source *cli.MethodsSource
	)

	if grpcSender != nil {
		defer func() { _ = grpcSender.Close() }()

		unchecked, source, _, err = connectAndCheckMethods(ctx, stderr, grpcSender, cfg, calls, flags.connectTimeout)
		if err != nil {
			return err
		}
	}

	if search {
		return runBreakpointSearch(ctx, abort, &stopper, flags.flagSet, flags.fake, flags.connectTimeout, searchSetup{
			cfg: cfg, configPath: flags.configPath, target: target, sender: sender, calls: calls,
			unchecked: unchecked, maxInFlight: flags.maxInFlight, json: flags.output == "json",
			jsonFile: flags.jsonFile, interactive: flags.interactive, stepBefore: stepBefore,
			source: source, passport: passport,
		}, stdout, stderr)
	}

	eng, err := engine.New(opts)
	if err != nil {
		return err
	}

	settings, err := loadSettings(flags.interactive, stderr)
	if err != nil {
		return err
	}

	// The live view holds the terminal in raw mode; leaving without restoring it
	// would leave the user a broken console.
	var view atomic.Pointer[tea.Program]
	exit := func() {
		if p := view.Load(); p != nil {
			p.Kill()
		}
		exitNow()
	}

	s := cli.NewStopper(
		// Each message goes out before its action: once the action lets run
		// return, nothing may write to stderr any more.
		func() {
			if !flags.interactive {
				fmt.Fprintln(stderr, "stopping: no new requests; waiting for those in flight. Ctrl+C again to cut them off")
			}
			eng.Stop()
		},
		func() {
			if !flags.interactive {
				fmt.Fprint(stderr, "aborting: requests in flight are cut off and counted as aborted")
				if !terminated.Load() {
					fmt.Fprint(stderr, ". Ctrl+C again to exit without a report")
				}
				fmt.Fprintln(stderr)
			}
			abort()
		},
		exit,
		time.Second,
	)
	stopper.Store(s)
	runStarting(eng)

	// The process's CPU time is read beside the run, from before the engine
	// starts to after it returns, and laid on the run's seconds afterwards.
	cores := cli.AvailableCPU()
	meter := cli.NewCPUMeter(cli.ProcessCPU, time.Now, time.Second)

	// A watch is made only for a limit that can trip; without one nothing runs
	// beside the engine.
	var tripped atomic.Pointer[errstop.Trip]
	watch := errstop.New(eng, cfg.Load.StopOnErrors, func(t errstop.Trip) {
		tripped.Store(&t)
		if !flags.interactive {
			fmt.Fprintln(stderr, cli.StopLine(t))
		}
		eng.Stop()
	})

	var stepAfter atomic.Int64
	start := func() error {
		stopMeter := meter.Start()
		stopWatch := startWatch(ctx, watch)
		err := eng.Run(ctx)
		stopWatch()
		stopMeter()
		stepAfter.Store(int64(clockStep()))

		return err
	}

	reportOf := func() cli.RunReport {
		return buildReport(reportParts{
			eng: eng, stepBefore: stepBefore, stepAfter: &stepAfter, meter: meter, cores: cores,
			cfg: cfg, configPath: flags.configPath, passport: passport, maxInFlight: flags.maxInFlight,
			unchecked: unchecked, source: source, tripped: &tripped,
		})
	}

	var runErr error
	label := cli.ServiceLabel(cfg, flags.configPath)

	if flags.interactive {
		program := cli.NewProgram(target, label, eng, cli.ExcludedWindow(cfg), settings, s, reportOf)
		view.Store(program)
		runErr = cli.RunLive(program, s, start, abort)
	} else if runErr = cli.RunPlain(stderr, target, eng, start); runErr != nil &&
		!errors.Is(runErr, context.Canceled) && !errors.Is(runErr, engine.ErrInFlightCapExceeded) {
		return runErr
	}

	// The live screen held the terminal while the watch tripped: the line goes
	// out once it is closed.
	if t := tripped.Load(); t != nil && flags.interactive {
		fmt.Fprintln(stderr, cli.StopLine(*t))
	}

	report := reportOf()
	result := runResult(report, runErr)

	if err := writeReport(stdout, s, label, target, flags.output, flags.jsonFile, report, result); err != nil {
		return err
	}

	return result
}

// runFlags are the flags a run was started with, checked.
type runFlags struct {
	configPath     string
	maxInFlight    int
	connectTimeout time.Duration
	fake           bool
	fakeDelay      time.Duration
	fakeJitter     time.Duration
	fakeFail       float64
	dryRun         bool
	yes            bool
	output         string
	jsonFile       string
	interactive    bool
	flagLabels     map[string]string
	flagSet        *flag.FlagSet
}

// parseFlags reads the command line and checks it before anything else. done
// says the command already ran and run has nothing left to do.
func parseFlags(args []string, terminal bool, stdout, stderr io.Writer) (runFlags, bool, error) {
	flags := flag.NewFlagSet("leettest", flag.ContinueOnError)
	flags.SetOutput(stderr)

	var (
		configPath     = flags.String("c", "", "path to the config file")
		maxInFlight    = flags.Int("max-in-flight", defaultMaxInFlight, "cap on requests waiting for a reply")
		connectTimeout = flags.Duration("connect-timeout", 10*time.Second, "how long to wait for a target that accepts the connection but does not answer")
		fake           = flags.Bool("fake", false, "load the built-in fake target instead of the one in the config")
		fakeDelay      = flags.Duration("fake-delay", 25*time.Millisecond, "latency of the fake target, with -fake")
		fakeJitter     = flags.Duration("fake-jitter", 10*time.Millisecond, "random spread added to the fake latency, with -fake")
		fakeFail       = flags.Float64("fake-fail-ratio", 0, "share of fake replies that fail, 0 to 1, with -fake")
		dryRun         = flags.Bool("dry-run", false, "send one request per call, print the request and the reply, measure nothing")
		yes            = flags.Bool("yes", false, "accepted and ignored for now; reserved for the confirmation of a real-mode run")
		showVersion    = flags.Bool("version", false, "print the version and exit")
		output         = flags.String("output", "text", "report format on stdout: text, or json for scripts")
		jsonFile       = flags.String("json-file", "", "also write the JSON report to this new file")
		plain          = flags.Bool("plain", false, "no live screen: progress lines on stderr, as when stderr is not a terminal")
		labelArgs      labelFlags
	)
	flags.Var(&labelArgs, "label", "a key=value label for the report, repeatable; wins over the config's labels")

	if err := flags.Parse(args); err != nil {
		return runFlags{}, false, err
	}

	interactive := liveView(*plain, terminal)
	if *showVersion {
		info, _ := debug.ReadBuildInfo()
		fmt.Fprintf(stdout, "leettest %s\n", versionString(version, info))

		return runFlags{}, true, nil
	}
	if *configPath == "" {
		flags.Usage()

		return runFlags{}, false, errors.New("no config given, use -c")
	}
	if *output != "text" && *output != "json" {
		return runFlags{}, false, fmt.Errorf("-output %q: want text or json", *output)
	}
	if err := checkDryRunFlags(flags, *dryRun); err != nil {
		return runFlags{}, false, err
	}
	if err := checkFakeFlags(flags, *fake); err != nil {
		return runFlags{}, false, err
	}
	flagLabels, err := parseLabels(labelArgs)
	if err != nil {
		return runFlags{}, false, err
	}

	return runFlags{
		configPath:     *configPath,
		maxInFlight:    *maxInFlight,
		connectTimeout: *connectTimeout,
		fake:           *fake,
		fakeDelay:      *fakeDelay,
		fakeJitter:     *fakeJitter,
		fakeFail:       *fakeFail,
		dryRun:         *dryRun,
		yes:            *yes,
		output:         *output,
		jsonFile:       *jsonFile,
		interactive:    interactive,
		flagLabels:     flagLabels,
		flagSet:        flags,
	}, false, nil
}

// loadConfig reads the config and builds the passport.
func loadConfig(path string, flagLabels map[string]string) (*config.MasterConfig, *cli.Passport, error) {
	cfg, err := config.LoadFile(path)
	if err != nil {
		return nil, nil, err
	}
	// The machine as the run starts; the config is read once, and its hash is
	// of the bytes that were read.
	passport := cli.NewPassport(cfg.SHA256, mergeLabels(cfg.Labels, flagLabels), cpuQuota())

	return cfg, passport, nil
}

// buildSender makes the sender a run sends with, fake or real, and the target
// the report names.
func buildSender(cfg *config.MasterConfig, fake bool, fakeDelay, fakeJitter time.Duration,
	fakeFail float64,
) (sender engine.Sender, grpcSender *grpcsender.Sender, target string, err error) {
	target = string(cfg.App.Address)

	if fake {
		target = cli.FakeTarget
		sender = engine.FakeSender{Delay: fakeDelay, Jitter: fakeJitter, FailRatio: fakeFail}
	} else {
		var senderOpts grpcsender.Options
		if senderOpts, err = senderOptions(&cfg.App); err != nil {
			return nil, nil, "", err
		}
		grpcSender = grpcsender.New(senderOpts)
		sender = grpcSender
	}

	return sender, grpcSender, target, nil
}

// engineOptions builds the engine's options and checks them before the network.
func engineOptions(cfg *config.MasterConfig, sender engine.Sender, maxInFlight int,
	stepBefore time.Duration,
) (engine.Options, []engine.Call, bool, error) {
	calls, err := cli.CallsFromConfig(cfg)
	if err != nil {
		return engine.Options{}, nil, false, err
	}

	// A file that cannot cover the run is a config error: the target never sees
	// a half run. A search is checked against its first run, whose warm-up is
	// inside the hold; later runs end the search if the file runs out.
	if fitErr := cli.CheckDatasetsFit(firstRuns(cfg, calls)); fitErr != nil {
		return engine.Options{}, nil, false, fitErr
	}

	opts := engine.Options{
		Calls:       calls,
		Sender:      sender,
		MaxInFlight: maxInFlight,
		Warmup:      cli.ExcludedWindow(cfg),
		WaitFloor:   engine.WaitFloorFor(stepBefore),
	}

	// A config error does not wait for the network: checked before connecting.
	// A search has no rate of its own: each step's engine is checked as it
	// is built, and a cap too low for a step ends the search there.
	search := cfg.Load.Breakpoint != nil
	if err = engine.CheckOptions(opts); err != nil && !search {
		return opts, calls, search, withBudgetAdvice(err, opts)
	}

	return opts, calls, search, nil
}

// connectAndCheckMethods connects to the target and checks every method before
// the run, so an unreachable target or a body that does not fit is an error
// rather than a report full of failures. methods are the ones it resolved, one
// per call, nil for an unchecked one.
func connectAndCheckMethods(ctx context.Context, stderr io.Writer, sender *grpcsender.Sender,
	cfg *config.MasterConfig, calls []engine.Call, timeout time.Duration,
) ([]cli.Unchecked, *cli.MethodsSource, []*descriptor.Method, error) {
	var (
		unchecked []cli.Unchecked
		source    *cli.MethodsSource
		methods   []*descriptor.Method
	)

	// Files are read before the connection: a mistake in them does not
	// wait for the network, and no reflection call is made for them.
	files, fileSource, err := cli.FileResolver(cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	source = fileSource
	if files != nil {
		if unchecked, methods, err = cli.AttachMethods(ctx, files, cfg, calls); err != nil {
			return nil, nil, nil, err
		}
	}

	if err := connect(ctx, stderr, sender, &cfg.App, timeout); err != nil {
		return nil, nil, nil, err
	}

	if files == nil {
		// Bodies need the schema, and the schema needs the connection.
		source = cli.ReflectionSource()
		dataCtx, cancelData := context.WithTimeout(ctx, timeout)
		var dataErr error
		unchecked, methods, dataErr = cli.AttachMethods(dataCtx, descriptor.NewReflectionResolver(sender.Conn()), cfg, calls)
		cancelData()

		if dataErr != nil {
			return nil, nil, nil, dataErr
		}
	}
	for _, m := range unchecked {
		fmt.Fprintf(stderr, "%s: %v; the method was not checked before the run and sends an "+
			"empty message\n", m.Method, m.Err)
	}

	return unchecked, source, methods, nil
}

// runBreakpointSearch runs a search with what run prepared, each step's engine
// checked against its own rate as it is built.
func runBreakpointSearch(ctx context.Context, abort context.CancelFunc, stopper *atomic.Pointer[cli.Stopper],
	flags *flag.FlagSet, fake bool, connectTimeout time.Duration, setup searchSetup,
	stdout, stderr io.Writer,
) error {
	capSet := false
	flags.Visit(func(f *flag.Flag) { capSet = capSet || f.Name == "max-in-flight" })
	connectWithin := connectTimeout
	if fake {
		connectWithin = 0
	}

	setup.capSet = capSet
	setup.connect = connectWithin

	return runSearch(ctx, abort, stopper, setup, stdout, stderr)
}

// loadSettings reads the stored settings, prints their warnings and, on a
// terminal with none stored, asks the first-run dialog.
func loadSettings(interactive bool, stderr io.Writer) (*cli.Settings, error) {
	settings, err := cli.LoadSettings()
	if err != nil {
		return nil, err
	}

	for _, warning := range settings.Deprecations() {
		fmt.Fprintln(stderr, warning)
	}

	if !settings.Configured() {
		if interactive {
			if err := runSetup(settings); err != nil {
				return nil, err
			}
		} else {
			settings.Mode = string(cli.ModeDark)
			settings.Palette = cli.Palettes()[0].Name
		}
	}

	return settings, nil
}

// reportParts is what a finished run's report is made of beside the engine's
// own numbers: the clock steps, the CPU meter and the config.
type reportParts struct {
	eng         *engine.Engine
	stepBefore  time.Duration
	stepAfter   *atomic.Int64
	meter       *cli.CPUMeter
	cores       float64
	cfg         *config.MasterConfig
	configPath  string
	passport    *cli.Passport
	maxInFlight int
	unchecked   []cli.Unchecked
	source      *cli.MethodsSource
	// tripped is where the stop watch left its trip; nil pointer inside until
	// it trips.
	tripped *atomic.Pointer[errstop.Trip]
}

// startWatch runs the watch beside the engine, none when there is none, and
// returns what ends it and waits for it.
func startWatch(ctx context.Context, watch *errstop.Watch) (stop func()) {
	if watch == nil {
		return func() {}
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		watch.Run(ctx)
	}()

	return func() {
		cancel()
		<-done
	}
}

// buildReport is one report for the final screen and for stdout.
func buildReport(p reportParts) cli.RunReport {
	report := p.eng.Report()

	var maxResponse string
	if raw := p.cfg.App.RawMaxResponseSize; raw != nil {
		maxResponse = strings.TrimSpace(*raw)
	}

	run := cli.RunReport{
		Report: report, Unchecked: p.unchecked, MaxResponse: maxResponse, MethodsSource: p.source,
		ClockStep: max(p.stepBefore, time.Duration(p.stepAfter.Load())), ClockStepBefore: p.stepBefore,
		GeneratorCPU: p.meter.Result(report, p.cores),
		Name:         cli.ServiceLabel(p.cfg, p.configPath),
		Passport:     p.passport,
		Settings: &cli.RunSettings{
			MaxInFlight: p.maxInFlight, Connections: p.cfg.App.Connections, Warmup: p.cfg.Load.Warmup,
			Ramp: p.cfg.Load.Ramp, Compression: p.cfg.App.Compression, Mode: p.cfg.App.Mode,
			StopOnErrors: p.cfg.Load.StopOnErrors.Text,
		},
	}
	if p.tripped != nil {
		run.Stop = p.tripped.Load()
	}
	// The limits are checked against the run as it ended, the clock step
	// included: it can make the run invalid.
	if limits := methodLimits(p.cfg); len(limits) > 0 {
		run.Thresholds = threshold.Evaluate(report, runState(run), limits)
	}

	return run
}

// writeReport encodes the report, writes it to stdout in the format asked for
// and saves it to -json-file.
func writeReport(stdout io.Writer, s *cli.Stopper, label, target, output, jsonFile string,
	report cli.RunReport, result error,
) error {
	// Only a run that happened has a report: on exit 1 stdout stays empty,
	// and a script reads the exit code first. One encoding feeds stdout and
	// the file, so they hold the same bytes.
	var data []byte
	if outcome, ok := outcomeOf(result); ok && (output == "json" || jsonFile != "") {
		info, _ := debug.ReadBuildInfo()

		var err error
		if data, err = encodeJSON(func(w io.Writer) error {
			return cli.WriteJSON(w, cli.JSONRun{
				Target: target, Name: label, Version: versionString(version, info), Outcome: outcome,
				StartedAt: report.StartedAt, Run: report,
			})
		}); err != nil {
			return err
		}
	}
	if output == "json" {
		if _, err := stdout.Write(data); err != nil {
			return fmt.Errorf("write the JSON report: %w", err)
		}
	} else {
		cli.PrintReport(stdout, target, report)
	}
	s.Finish()

	// A report that was asked for and not saved is not a pass: exit 1.
	if err := saveJSONFile(jsonFile, data); err != nil {
		return err
	}

	return nil
}

// outcomeOf names a run's result in the JSON report, the same as its exit code
// says: 0 and 4 complete (a missed limit is a finding of a finished run), 2
// invalid, 3 incomplete. Any other error has no report.
func outcomeOf(result error) (string, bool) {
	switch exitCode(result) {
	case 0, 4:
		return cli.OutcomeComplete, true
	case 2:
		return cli.OutcomeInvalid, true
	case 3:
		return cli.OutcomeIncomplete, true
	default:
		return "", false
	}
}

// checkFakeFlags rejects tuning of the fake target when it is not in use: the
// flags would change nothing, and nothing would say so.
func checkFakeFlags(flags *flag.FlagSet, fake bool) error {
	if fake {
		return nil
	}

	var err error

	flags.Visit(func(f *flag.Flag) {
		if err == nil && strings.HasPrefix(f.Name, "fake-") {
			err = fmt.Errorf("-%s tunes the fake target and does nothing without -fake", f.Name)
		}
	})

	return err
}

// withBudgetAdvice turns the engine's numbers into the two settings that fix
// them. The engine knows neither the config fields nor the flags. opts are
// the ones err came from.
func withBudgetAdvice(err error, opts engine.Options) error {
	var budget *engine.InFlightBudgetError
	if !errors.As(err, &budget) || len(budget.Unbounded) > 0 || budget.PeakRPS == 0 {
		return err
	}

	// The formula only bounds the answer from above: each call rounds its own
	// rps × timeout up. The check itself says which timeout fits, so the
	// advice is the largest one it accepts, to the millisecond unless that
	// would round it to zero.
	passes := func(timeout time.Duration) bool {
		trial := opts
		trial.Calls = slices.Clone(opts.Calls)
		for i := range trial.Calls {
			trial.Calls[i].Timeout = timeout
		}

		return !errors.Is(engine.CheckOptions(trial), engine.ErrInFlightBudget)
	}

	upper := time.Duration(budget.Cap-budget.Reserved) * time.Second / time.Duration(budget.PeakRPS)
	step := time.Millisecond
	if upper < 2*time.Millisecond {
		step = time.Microsecond
	}
	fits := upper.Truncate(step) + step
	for fits > 0 && !passes(fits) {
		fits -= step
	}
	if fits <= 0 {
		return fmt.Errorf("%w\nno timeout fits this cap: run with -max-in-flight %d", err, budget.Need)
	}

	return fmt.Errorf("%w\nset timeout to at most %s for every call, or run with -max-in-flight %d",
		err, fits, budget.Need)
}

// senderOptions reads the certificate files the config names, before any
// connection: a wrong path is a config error, not a failed handshake.
func senderOptions(app *config.App) (grpcsender.Options, error) {
	opts := grpcsender.Options{
		Target:           string(app.Address),
		TLS:              app.UseTLS,
		Metadata:         app.Metadata,
		ServerName:       app.ServerName,
		MaxResponseBytes: app.MaxResponseBytes,
		Connections:      app.Connections,
		Compression:      app.Compression,
	}

	if app.CA != "" {
		raw, err := os.ReadFile(app.CA)
		if err != nil {
			return opts, fmt.Errorf("app.ca: %w", err)
		}

		opts.RootCAs = x509.NewCertPool()
		if !opts.RootCAs.AppendCertsFromPEM(raw) {
			return opts, fmt.Errorf("app.ca: %s holds no PEM certificate", app.CA)
		}
	}

	if app.Cert != "" {
		if err := refuseEncryptedKey(app.Key); err != nil {
			return opts, err
		}

		cert, err := tls.LoadX509KeyPair(app.Cert, app.Key)
		if err != nil {
			return opts, fmt.Errorf("app.cert %s, app.key %s: %w", app.Cert, app.Key, err)
		}
		opts.Certificates = []tls.Certificate{cert}
	}

	return opts, nil
}

// refuseEncryptedKey names a password-protected key for what it is; the
// parser would only say it cannot parse it. A file it cannot read is left to
// the loader, which names it.
func refuseEncryptedKey(path string) error {
	raw, _ := os.ReadFile(path)

	for block, rest := pem.Decode(raw); block != nil; block, rest = pem.Decode(rest) {
		if strings.Contains(block.Type, "ENCRYPTED") || strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED") {
			return fmt.Errorf("app.key %s: encrypted keys are not supported; decrypt it first", path)
		}
	}

	return nil
}

// connect reaches the target before the run, so an unreachable one is an error
// with its address rather than a report full of failures.
func connect(ctx context.Context, stderr io.Writer, sender *grpcsender.Sender, app *config.App,
	timeout time.Duration,
) error {
	mode := "without TLS"
	if app.UseTLS {
		mode = "over TLS"
	}

	fmt.Fprintf(stderr, "connecting to %s %s ...\n", app.Address, mode)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := sender.Connect(ctx)

	text := ""
	if err != nil {
		text = err.Error()
	}

	// The hints read the errors' text: crypto/tls and grpc-go give these
	// cases no type of their own that survives the connection error.
	switch {
	case err == nil:
		return nil
	case errors.Is(err, grpcsender.ErrClosedAfterHandshake):
		return fmt.Errorf("%w (%s)", err, mode)
	case app.UseTLS && (strings.Contains(text, "certificate is valid for") || strings.Contains(text, "doesn't contain any IP SANs")):
		return fmt.Errorf("%w\nThe target's certificate does not name %s; if it names another host, set app.server_name to it",
			err, app.Address)
	case app.UseTLS && strings.Contains(text, "does not look like a TLS handshake"):
		return fmt.Errorf("%w\nThe target does not speak TLS; set app.tls: false", err)
	case !app.UseTLS && strings.Contains(text, "server preface"):
		return fmt.Errorf("%w\nTLS is off (app.tls: false), and the target closed the connection; if it uses TLS, set app.tls: true", err)
	case app.UseTLS && app.TLS == nil:
		// The commonest first-run failure: a plaintext local service and TLS on
		// by default, which the handshake error alone does not explain.
		return fmt.Errorf("%w\nTLS is on because app.tls is not set; for a plaintext server set app.tls: false", err)
	default:
		return fmt.Errorf("%w (%s)", err, mode)
	}
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.w.Write(p)
}

// runResult is what run returns once the report is printed. A cap hit is not
// an error: the report printed its verdict, and the run is incomplete. A run
// that is invalid or short stays so whatever its limits say; a finished one is
// decided by them: a missed limit is ErrThresholdsFailed, one that could not
// be decided ErrInvalidRun.
func runResult(run cli.RunReport, runErr error) error {
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, engine.ErrInFlightCapExceeded) {
		return runErr
	}

	switch runState(run) {
	case threshold.Invalid:
		return ErrInvalidRun
	case threshold.Incomplete:
		return ErrIncomplete
	case threshold.Complete:
	}

	return limitsResult(run.Thresholds)
}

// runState is how the run ended, which decides whether its limits are
// checked: an invalid run before a short one, as in the exit code.
func runState(run cli.RunReport) threshold.State {
	switch {
	case run.CapHit != nil || run.RequestRejected || cli.ClockTooCoarse(run):
		return threshold.Invalid
	case run.Incomplete:
		return threshold.Incomplete
	}

	return threshold.Complete
}

// limitsResult is what the limits of a finished run or search decide: a miss
// first, then an undecided one; nil when none was set or all held.
func limitsResult(results []threshold.Result) error {
	switch threshold.Overall(results) {
	case threshold.VerdictFail:
		return ErrThresholdsFailed
	case threshold.VerdictUnknown:
		return ErrInvalidRun
	case threshold.VerdictPass:
	}

	return nil
}

// methodLimits are the limits the config set, by the method as the report
// names it.
func methodLimits(cfg *config.MasterConfig) []threshold.MethodLimits {
	var limits []threshold.MethodLimits

	for i := range cfg.Load.Calls {
		if call := &cfg.Load.Calls[i]; call.RawPass != nil {
			limits = append(limits, threshold.MethodLimits{Name: "/" + call.Method, Limits: call.Pass})
		}
	}

	return limits
}

// clockStep measures the host clock; the end-to-end tests fix it so that a
// coarse developer clock does not turn the runs they check into invalid ones.
var clockStep = func() time.Duration { return clock.StepOf(time.Now) }

// cpuQuota is the CPU limit of this machine's cgroup in cores, nil without
// one; the tests fix it, as a runner may have a limit and a laptop has none.
var cpuQuota = cli.HostCPUQuota
