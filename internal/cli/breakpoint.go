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
	"io"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
)

// BreakpointRun is what the breaking-point report needs: the search's plan
// and result, and the run's header.
type BreakpointRun struct {
	Target    string
	Version   string
	Method    string
	StartedAt time.Time
	Plan      breakpoint.Plan
	Result    breakpoint.Result
}

// PlanLine is the line printed before the first step: how many steps and the
// longest the search can take.
func PlanLine(_ breakpoint.Plan) string {
	return ""
}

// PrintBreakpoint writes the search's text report.
func PrintBreakpoint(_ io.Writer, _ BreakpointRun) {}

// WriteBreakpointJSON writes the search's report as one JSON object and a
// newline.
func WriteBreakpointJSON(w io.Writer, _ BreakpointRun) error {
	_, err := io.WriteString(w, "{}\n")

	return err
}
