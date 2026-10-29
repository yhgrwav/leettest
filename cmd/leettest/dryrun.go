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
	"strings"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/config"
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

	printer := &cli.DryRunPrinter{
		W: stdout, Total: len(calls), Redact: cli.DryRunRedactor(cfg.App.RawMetadata, os.LookupEnv),
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
