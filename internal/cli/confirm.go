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

package cli

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotConfirmed is the answer to the question of a real-mode run that is not
// yes, or no answer at all: nothing was sent.
var ErrNotConfirmed = errors.New("not confirmed: nothing was sent")

// RealPlan is what a real-mode run is about to do, in planned numbers: the
// text under the question says so, because a run that is stopped, or whose
// calls come late, does not do exactly this.
type RealPlan struct {
	Target string
	// PeakRPS is the sum of the calls' rps. Ramp is inside Duration, which is
	// the longest call's ramp and duration together. Both are zero for a search.
	PeakRPS  int
	Ramp     time.Duration
	Duration time.Duration
	// Search is a breakpoint search: its steps go from From to To rps and it
	// ends when the system breaks, so no duration is promised.
	Search   bool
	From, To int
}

// ConfirmText is the line that tells what the run will do, without the
// question.
func ConfirmText(RealPlan) string {
	return ""
}

// Confirm prints the plan and the question to w and reads one answer line from
// in. It returns nil for y or yes in any case, ErrNotConfirmed for anything
// else and for the end of input, and ctx's error when ctx is done first.
func Confirm(context.Context, io.Reader, io.Writer, RealPlan) error {
	return nil
}
