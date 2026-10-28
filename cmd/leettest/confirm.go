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
	"os"
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// stdinIsTerminal says whether the process's stdin is a terminal; tests
// replace it, as they do stderrIsTerminal.
var stdinIsTerminal = cli.StdinInteractive

// confirmInput is where the answer to the question of a real-mode run is read
// from; tests replace it.
var confirmInput io.Reader = os.Stdin

// confirmReal is the gate of a real-mode run: before the first call to the
// target it prints the plan and, on a terminal, asks. With yes it prints the
// plan and goes on; off a terminal without yes it refuses, because nobody can
// answer. Local mode passes without a word.
func confirmReal(ctx context.Context, stderr io.Writer, cfg *config.MasterConfig, calls []engine.Call,
	target string, yes, askable bool,
) error {
	if cfg.App.Mode != config.ModeReal {
		return nil
	}

	plan := realPlan(cfg, calls, target)

	if yes {
		fmt.Fprintln(stderr, cli.ConfirmText(plan))

		return nil
	}

	if !askable {
		return errors.New("app.mode: real sends load to a real target and asks first, " +
			"but there is no terminal to answer on: add -yes to start it")
	}

	return cli.Confirm(ctx, confirmInput, stderr, plan)
}

// realPlan reads the numbers of the plan from the calls as the engine will run
// them: the calls start together, so the peak is the sum of their rates and
// the duration is the longest call's stages (the ramp is the first of them).
func realPlan(cfg *config.MasterConfig, calls []engine.Call, target string) cli.RealPlan {
	if bp := cfg.Load.Breakpoint; bp != nil {
		return cli.RealPlan{Target: target, Search: true, From: int(bp.From), To: int(bp.To)}
	}

	plan := cli.RealPlan{Target: target, Ramp: cfg.Load.Ramp}

	for i := range calls {
		var peak int

		var total time.Duration

		for _, st := range calls[i].Stages {
			peak = max(peak, st.TargetRPS)
			total += st.Duration
		}

		plan.PeakRPS += peak
		plan.Duration = max(plan.Duration, total)
	}

	return plan
}
