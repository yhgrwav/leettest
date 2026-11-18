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
	"io"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
	"github.com/yhgrwav/leettest/pkg/engine"
	shr "github.com/yhgrwav/leettest/pkg/share"
)

// The tests call the short forms below. The product calls the long ones
// (reportNotesAfter, CompareRunsReport, AttachMethods), which take the arguments
// a test has no use for or return what it throws away.

// reportNotes is reportNotesAfter for a run without a ramp, outside a search.
func reportNotes(report engine.Report, maxResponse string) []string {
	return reportNotesAfter(report, maxResponse, 0, false, cpuNotMeasuredLine)
}

// CompareRuns is CompareRunsReport without the report it printed.
func CompareRuns(w io.Writer, base, nw RunFile, p99Limit, errorsLimit *shr.Percent) Comparison {
	c, _ := CompareRunsReport(w, base, nw, p99Limit, errorsLimit)

	return c
}

// AttachData is AttachMethods without the methods it resolved.
func AttachData(
	ctx context.Context, resolver descriptor.Resolver, cfg *config.MasterConfig,
	calls []engine.Call,
) (unchecked []Unchecked, err error) {
	unchecked, _, err = AttachMethods(ctx, resolver, cfg, calls)

	return unchecked, err
}
