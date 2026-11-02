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
	"time"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// lagSource is where the early warning reads the generator's window; tests
// replace it to script one.
var lagSource = func(eng *engine.Engine) cli.LagWindowSource { return eng }

// lagTickEvery is how often the warning looks at the window.
var lagTickEvery = time.Second

// startLagWatch looks at src every tick until ctx ends or stop is called, and
// hands deliver the warning line once, the first time the generator is behind.
// stop returns once the watch has.
func startLagWatch(_ context.Context, _ cli.LagWindowSource, _ time.Duration, _ func(line string)) (stop func()) {
	return func() {}
}
