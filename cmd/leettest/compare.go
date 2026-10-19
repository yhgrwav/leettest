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
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/compare"
	"github.com/yhgrwav/leettest/pkg/share"
)

// runCompare is leettest compare: args are what follows the word compare.
// Flags come before the two files. The exit codes are those of a search with
// pass: limits: 0 within the limit or none given, 2 undecidable, 4 past the
// limit, 1 no comparison.
func runCompare(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("leettest compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	maxDrop := flags.String("max-capacity-drop", "", "fail when the capacity fell by more than this share, such as 10%")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("compare: want two report files, base then new")
	}

	var limit *share.Percent
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "max-capacity-drop" {
			limit = &share.Percent{}
		}
	})
	if limit != nil {
		p, err := share.Parse(*maxDrop, "-max-capacity-drop")
		if err != nil {
			return fmt.Errorf("compare: %w", err)
		}
		*limit = p
	}

	var files [2]cli.SearchFile
	for i, path := range flags.Args() {
		report, err := cli.ReadSearchReport(path)
		if err != nil {
			return fmt.Errorf("compare: %w", err)
		}
		files[i] = cli.SearchFile{Path: path, Report: report}
	}

	res := cli.CompareSearches(stdout, files[0], files[1], limit)

	switch {
	case !res.Comparable || res.Judged && res.Verdict == compare.Unknown:
		return fmt.Errorf("compare: nothing decided: %w", ErrInvalidRun)
	case res.Judged && res.Verdict == compare.Fail:
		return fmt.Errorf("compare: capacity fell past -max-capacity-drop: %w", ErrThresholdsFailed)
	}

	return nil
}
