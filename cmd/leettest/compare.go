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

// The flags of leettest compare, each the limit of one check.
const (
	flagCapacityDrop = "max-capacity-drop"
	flagP99Rise      = "max-p99-rise"
	flagErrorsRise   = "max-errors-rise"
)

// compareError is an error of leettest compare with a text of its own that
// still is its kind: errors.Is finds the sentinel the exit code is read from.
type compareError struct {
	text string
	kind error
}

func (e *compareError) Error() string { return e.text }

func (e *compareError) Unwrap() error { return e.kind }

// runCompare is leettest compare: args are what follows the word compare.
// Flags come before the two files, which are two breakpoint searches or two
// plain runs. The exit codes: 0 within the limits or none given, 1 no
// comparison, 2 reports that cannot be compared or a limit that cannot be
// decided, 4 a limit missed (also beside one that cannot be decided).
func runCompare(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("leettest compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	limitText := map[string]*string{
		flagCapacityDrop: flags.String(flagCapacityDrop, "", "breakpoint searches: fail when the capacity fell by more than this share, such as 10%"),
		flagP99Rise:      flags.String(flagP99Rise, "", "plain runs: fail when a method's p99 rose by more than this share, such as 20%"),
		flagErrorsRise:   flags.String(flagErrorsRise, "", "plain runs: fail when a method's share of failed calls rose by more than this many points, such as 1%"),
	}

	output := flags.String("output", "text", "report format: text or json")

	if err := flags.Parse(args); err != nil {
		return err
	}

	// Stub for the red tests: json mode writes an empty object, the text goes
	// nowhere.
	if *output == "json" {
		if err := cli.WriteCompareJSON(stdout, cli.CompareReport{}); err != nil {
			return err
		}

		stdout = io.Discard
	}

	if flags.NArg() != 2 {
		return errors.New("compare: want two report files, base then new")
	}

	limits := map[string]*share.Percent{}

	var parseErr error

	flags.Visit(func(f *flag.Flag) {
		if parseErr != nil || limitText[f.Name] == nil {
			return
		}

		p, err := share.Parse(*limitText[f.Name], "-"+f.Name)
		if err != nil {
			parseErr = fmt.Errorf("compare: %w", err)

			return
		}

		limits[f.Name] = &p
	})

	if parseErr != nil {
		return parseErr
	}

	var reports [2]cli.ComparedReport

	for i, path := range flags.Args() {
		report, err := cli.ReadReport(path)
		if err != nil {
			return fmt.Errorf("compare: %w", err)
		}

		reports[i] = report
	}

	base, nw := flags.Arg(0), flags.Arg(1)

	if reports[0].Mode != reports[1].Mode {
		return fmt.Errorf("compare: %s is a %s, %s a %s: compare reads two of one kind",
			base, cli.KindOf(reports[0].Mode), nw, cli.KindOf(reports[1].Mode))
	}

	var res cli.Comparison

	if reports[0].Mode == cli.ModeRun {
		if limits[flagCapacityDrop] != nil {
			return errors.New("compare: -" + flagCapacityDrop + " applies to breakpoint searches")
		}

		res = cli.CompareRuns(stdout,
			cli.RunFile{Path: base, Report: reports[0].Run}, cli.RunFile{Path: nw, Report: reports[1].Run},
			limits[flagP99Rise], limits[flagErrorsRise])
	} else {
		for _, name := range []string{flagP99Rise, flagErrorsRise} {
			if limits[name] != nil {
				return errors.New("compare: -" + name + " applies to plain runs")
			}
		}

		res = cli.CompareSearches(stdout,
			cli.SearchFile{Path: base, Report: reports[0].Search}, cli.SearchFile{Path: nw, Report: reports[1].Search},
			limits[flagCapacityDrop])
	}

	return compareResult(res)
}

// compareResult is the error a finished comparison ends with, nil when every
// limit asked for held.
func compareResult(res cli.Comparison) error {
	switch {
	case res.Judged && res.Verdict == compare.Fail:
		return ErrCompareFailed
	case !res.Comparable:
		return &compareError{text: "compare: reports are not comparable: nothing decided", kind: ErrCompareUndecided}
	case res.Judged && res.Verdict == compare.Unknown:
		return ErrCompareUndecided
	}

	return nil
}
