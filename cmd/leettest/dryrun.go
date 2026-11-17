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
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/grpcsender"
)

// checkDryRunFlags rejects the flags that a dry run would ignore, as
// checkFakeFlags does for the fake target's: they would change nothing, and
// nothing would say so. -max-in-flight is not among them (it decides whether
// the run's budget check passes), nor are -c and -connect-timeout.
func checkDryRunFlags(flags *flag.FlagSet, dryRun bool) error {
	if !dryRun {
		return nil
	}

	var err error

	flags.Visit(func(f *flag.Flag) {
		switch {
		case err != nil:
		case f.Name == "fake":
			err = errors.New("-fake with -dry-run: the fake target has no replies to show")
		case f.Name == "output", f.Name == "json-file", f.Name == "label", f.Name == "plain",
			strings.HasPrefix(f.Name, "fake-"):
			err = fmt.Errorf("-%s with -dry-run: a dry run sends one request per call and writes no report", f.Name)
		}
	})

	return err
}

// runDryRun is -dry-run: one request per call, the request and the reply on
// screen, nothing measured. The checks a run makes before the network are made
// here too, so that a CI step «dry run, then load» does not pass the first and
// fail the second.
func runDryRun(ctx context.Context, stdout, stderr io.Writer, cfg *config.MasterConfig, sender engine.Sender,
	grpcSender *grpcsender.Sender, flags runFlags,
) error {
	_, calls, _, err := engineOptions(cfg, sender, flags.maxInFlight, 0)
	if err != nil {
		return err
	}

	defer func() { _ = grpcSender.Close() }()

	_, _, methods, err := connectAndCheckMethods(ctx, stderr, grpcSender, cfg, calls, flags.connectTimeout)
	if err != nil {
		return err
	}

	if fitErr := cli.CheckTemplatesFit(firstRuns(cfg, calls)); fitErr != nil {
		return fitErr
	}

	scenarios := 0

	for i := range cfg.Load.Calls {
		if cfg.Load.Calls[i].Scenario != "" {
			scenarios++
		}
	}

	var infos [][]*descriptor.Method

	if scenarios > 0 {
		if infos, err = stepInfos(ctx, cfg, grpcSender, flags.connectTimeout); err != nil {
			return err
		}
	}

	printer := &cli.DryRunPrinter{
		W: stdout, Total: len(calls), Scenarios: scenarios, Redact: cli.DryRunRedactor(cfg.App.RawMetadata, os.LookupEnv),
	}
	printer.Header()

	stopped := func() error {
		return fmt.Errorf("dry run: stopped after %d of %d calls", printer.Printed(), len(calls))
	}

	failed := 0

	for i := range calls {
		if ctx.Err() != nil {
			return stopped()
		}

		if cfgCall := &cfg.Load.Calls[i]; cfgCall.Scenario != "" {
			steps, err := runDryScenario(ctx, grpcSender, calls[i])
			if err != nil {
				if ctx.Err() != nil {
					return stopped()
				}

				return fmt.Errorf("dry run: scenario %s: %w", cfgCall.Scenario, err)
			}

			if !printDryScenario(printer, cfgCall, &calls[i], steps, methods[i], infos[i]) {
				failed++
			}

			continue
		}

		call := dryRunCall(&cfg.Load.Calls[i], &calls[i])
		call.Info = methods[i]

		started := time.Now()
		req := engine.Request{Method: calls[i].Method, Payload: call.Request, ScheduledAt: started, KeepResponse: true}

		// The budget runs from the moment of sending, as a run's runs from its schedule.
		if calls[i].Timeout > 0 {
			req.Deadline = started.Add(calls[i].Timeout)
		}

		outcome, err := grpcSender.Send(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return stopped()
			}

			return fmt.Errorf("dry run: %s: %w", call.Method, err)
		}

		call.Started, call.Returned, call.Outcome = started, time.Now(), outcome
		printer.Call(call)

		if outcome.Category != engine.CategorySuccess {
			failed++
		}
	}

	printer.Metadata(lowerKeys(cfg.App.RawMetadata))

	if failed > 0 {
		return fmt.Errorf("dry run: %d of %d calls did not succeed", failed, len(calls))
	}

	return nil
}

// dryStep is one step of a scenario's dry iteration: what went out, when, and
// how the worker judged the reply, the reply itself kept in Outcome.Response.
type dryStep struct {
	Request           engine.Request
	Started, Returned time.Time
	Outcome           engine.Outcome
}

// stepRecorder is the Sender the worker is given for a dry iteration: it keeps
// each request as it went and the reply, which the worker drops, and asks the
// sender to return the reply.
type stepRecorder struct {
	inner engine.Sender

	mu   sync.Mutex
	sent []sentStep
}

type sentStep struct {
	req               engine.Request
	started, returned time.Time
	response          []byte
}

func (r *stepRecorder) Send(ctx context.Context, req engine.Request) (engine.Outcome, error) {
	req.KeepResponse = true
	started := time.Now()
	out, err := r.inner.Send(ctx, req)

	r.mu.Lock()
	r.sent = append(r.sent, sentStep{req: req, started: started, returned: time.Now(), response: out.Response})
	r.mu.Unlock()

	return out, err
}

// oneIteration is call planned for exactly one iteration, starting now, and
// counting from the first record and the first seq whatever order or counter the
// config gave: the call, and the steps with a dataset, get counters of their
// own. The caller's call is not touched.
func oneIteration(call engine.Call) engine.Call {
	call.Stages = []engine.Stage{{StartRPS: 1, TargetRPS: 1, Duration: time.Second}}

	if call.Dataset != nil {
		ref := *call.Dataset
		ref.Counter = engine.NewRecordCounter()
		call.Dataset = &ref
	}

	if call.Template != nil {
		tpl := *call.Template
		tpl.Counter = engine.NewRecordCounter()
		call.Template = &tpl
	}

	call.Next = slices.Clone(call.Next)
	for k := range call.Next {
		if ref := call.Next[k].Dataset; ref != nil {
			own := *ref
			own.Counter = engine.NewRecordCounter()
			call.Next[k].Dataset = &own
		}
	}

	return call
}

// runDryScenario runs one iteration of call, the scenario, through the engine's
// own dispatcher and worker, so that the bodies, the extracted values and the
// judging of each reply are the run's: step 1, then each step after it while the
// one before succeeded. It returns the steps that were sent, in order. A run
// cut by ctx returns its error and no steps: a step cut by our own cancel is not
// an answer of the target.
func runDryScenario(ctx context.Context, sender engine.Sender, call engine.Call) ([]dryStep, error) {
	call = oneIteration(call)
	rec := &stepRecorder{inner: sender}

	reqs := make(chan engine.Request, 1)
	// One iteration is the steps and its own result: nothing blocks the worker.
	results := make(chan engine.Result, len(call.Next)+2)
	dispatched := make(chan error, 1)

	go func() {
		err := engine.NewDispatcher([]engine.Call{call}).Run(ctx, reqs)
		close(reqs)
		dispatched <- err
	}()

	poolErr := engine.NewWorkerPool(rec, 1).Run(ctx, reqs, results)
	close(results)

	if dispatchErr := <-dispatched; dispatchErr != nil && ctx.Err() == nil {
		return nil, dispatchErr
	}

	if poolErr != nil && ctx.Err() == nil {
		return nil, poolErr
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	var steps []dryStep

	for res := range results {
		if res.Iteration {
			continue
		}

		if len(steps) >= len(rec.sent) {
			return nil, errors.New("the worker returned a step that was not sent")
		}

		sent := rec.sent[len(steps)]
		out := res.Outcome
		out.Response = sent.response
		steps = append(steps, dryStep{Request: sent.req, Started: sent.started, Returned: sent.returned, Outcome: out})
	}

	return steps, nil
}

// printDryScenario prints a block per step sent, then the steps after a failed
// one as not reached; it reports whether every step succeeded.
func printDryScenario(printer *cli.DryRunPrinter, cfgCall *config.Call, call *engine.Call, steps []dryStep,
	callInfo *descriptor.Method, stepInfo []*descriptor.Method,
) bool {
	total := 1 + len(call.Next)
	ok := len(steps) == total

	for k := range steps {
		info := callInfo
		if k > 0 {
			info = stepInfo[k-1]
		}

		printer.Call(dryStepBlock(cfgCall, call, k, &steps[k], info))

		ok = ok && steps[k].Outcome.Category == engine.CategorySuccess
	}

	for k := len(steps); k < total; k++ {
		printer.NotReached(cfgCall.Scenario, k+1, total, cfgCall.Steps[k-1].Method)
	}

	return ok
}

// dryStepBlock is the screen block of step k of a scenario, 0 being the call.
func dryStepBlock(cfgCall *config.Call, call *engine.Call, k int, step *dryStep, info *descriptor.Method) cli.DryRunCall {
	out := cli.DryRunCall{
		Method: cfgCall.Method, Info: info, Request: step.Request.Payload,
		Outcome: step.Outcome, Started: step.Started, Returned: step.Returned,
		Scenario: cfgCall.Scenario, Step: k + 1, Steps: 1 + len(call.Next),
	}

	dataset, payloads := call.Dataset, len(call.Payloads)

	if k > 0 {
		out.Method = cfgCall.Steps[k-1].Method
		dataset, payloads = call.Next[k-1].Dataset, len(call.Next[k-1].Payloads)
	}

	if dataset != nil && payloads > 0 {
		out.Record, out.Records = 1, payloads
	}

	return out
}

// stepInfos are the descriptors of the steps' methods, for the screen only:
// the files named by the config, otherwise the target's reflection. A method
// that cannot be resolved has none, as a plain call that was not checked.
func stepInfos(ctx context.Context, cfg *config.MasterConfig, grpcSender *grpcsender.Sender,
	timeout time.Duration,
) ([][]*descriptor.Method, error) {
	resolver, _, err := cli.FileResolver(cfg)
	if err != nil {
		return nil, err
	}

	if resolver == nil {
		resolver = descriptor.NewReflectionResolver(grpcSender.Conn())
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	infos := make([][]*descriptor.Method, len(cfg.Load.Calls))

	for i := range cfg.Load.Calls {
		for _, step := range cfg.Load.Calls[i].Steps {
			info, resolveErr := resolver.Resolve(ctx, step.Method)
			if resolveErr != nil {
				info = nil
			}

			infos[i] = append(infos[i], info)
		}
	}

	return infos, nil
}

// dryRunCall is the call's request: its data, or the first record of its
// dataset in file order, whatever the order a run would take.
func dryRunCall(cfgCall *config.Call, call *engine.Call) cli.DryRunCall {
	out := cli.DryRunCall{Method: cfgCall.Method, Request: call.Payload}

	if call.Dataset != nil && len(call.Payloads) > 0 {
		out.Request, out.Record, out.Records = call.Payloads[0], 1, len(call.Payloads)
	}

	// The first request of a run is number 1; the dry run counts nothing.
	if call.Template != nil && call.Template.Build != nil {
		out.Request = call.Template.Build(1)
	}

	return out
}

// lowerKeys are the metadata keys as they go out, with the values as written.
func lowerKeys(raw map[string]string) map[string]string {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[strings.ToLower(k)] = v
	}

	return out
}
