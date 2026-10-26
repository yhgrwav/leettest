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
	"runtime/debug"

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
	opts, err := parseCompareOptions(args, stdout, stderr)
	if err != nil {
		return err
	}

	base, nw, err := comparePaths(opts.flags)
	if err != nil {
		return err
	}

	limits, err := parseCompareLimits(opts.flags, opts.limitText)
	if err != nil {
		return err
	}

	reports, err := readCompareReports(opts.flags)
	if err != nil {
		return err
	}

	res, rep, err := compareReports(opts, reports, limits, base, nw)
	if err != nil {
		return err
	}

	if opts.output == "json" {
		if err := writeCompareJSON(stdout, rep); err != nil {
			return err
		}
	}

	return compareResult(res)
}

// compareOptions is what the flags of leettest compare hold once parsed.
type compareOptions struct {
	flags     *flag.FlagSet
	output    string
	textOut   io.Writer
	limitText map[string]*string
}

func parseCompareOptions(args []string, stdout, stderr io.Writer) (*compareOptions, error) {
	flags := flag.NewFlagSet("leettest compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	limitText := map[string]*string{
		flagCapacityDrop: flags.String(flagCapacityDrop, "", "breakpoint searches: fail when the capacity fell by more than this share, such as 10%"),
		flagP99Rise:      flags.String(flagP99Rise, "", "plain runs: fail when a method's p99 rose by more than this share, such as 20%"),
		flagErrorsRise:   flags.String(flagErrorsRise, "", "plain runs: fail when a method's share of failed calls rose by more than this many points, such as 1%"),
	}

	output := flags.String("output", "text", "report format: text or json")

	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	if *output != "text" && *output != "json" {
		return nil, fmt.Errorf("compare: -output %q: want text or json", *output)
	}

	// The text is built either way: json mode takes the object from the same
	// comparison and lets the text go nowhere.
	textOut := stdout
	if *output == "json" {
		textOut = io.Discard
	}

	return &compareOptions{flags: flags, output: *output, textOut: textOut, limitText: limitText}, nil
}

func comparePaths(flags *flag.FlagSet) (base, nw string, err error) {
	if flags.NArg() != 2 {
		return "", "", errors.New("compare: want two report files, base then new")
	}

	return flags.Arg(0), flags.Arg(1), nil
}

func parseCompareLimits(flags *flag.FlagSet, limitText map[string]*string) (map[string]*share.Percent, error) {
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
		return nil, parseErr
	}

	return limits, nil
}

func readCompareReports(flags *flag.FlagSet) ([2]cli.ComparedReport, error) {
	var reports [2]cli.ComparedReport

	for i, path := range flags.Args() {
		report, err := cli.ReadReport(path)
		if err != nil {
			return reports, fmt.Errorf("compare: %w", err)
		}

		reports[i] = report
	}

	return reports, nil
}

func compareReports(opts *compareOptions, reports [2]cli.ComparedReport, limits map[string]*share.Percent, base, nw string) (cli.Comparison, cli.CompareReport, error) {
	if reports[0].Mode != reports[1].Mode {
		return cli.Comparison{}, cli.CompareReport{}, fmt.Errorf("compare: %s is a %s, %s a %s: compare reads two of one kind",
			base, cli.KindOf(reports[0].Mode), nw, cli.KindOf(reports[1].Mode))
	}

	var (
		res cli.Comparison
		rep cli.CompareReport
	)

	if reports[0].Mode == cli.ModeRun {
		if limits[flagCapacityDrop] != nil {
			return cli.Comparison{}, cli.CompareReport{}, errors.New("compare: -" + flagCapacityDrop + " applies to breakpoint searches")
		}

		res, rep = cli.CompareRunsReport(opts.textOut,
			cli.RunFile{Path: base, Report: reports[0].Run}, cli.RunFile{Path: nw, Report: reports[1].Run},
			limits[flagP99Rise], limits[flagErrorsRise])
	} else {
		for _, name := range []string{flagP99Rise, flagErrorsRise} {
			if limits[name] != nil {
				return cli.Comparison{}, cli.CompareReport{}, errors.New("compare: -" + name + " applies to plain runs")
			}
		}

		res, rep = cli.CompareSearchesReport(opts.textOut,
			cli.SearchFile{Path: base, Report: reports[0].Search}, cli.SearchFile{Path: nw, Report: reports[1].Search},
			limits[flagCapacityDrop])
	}

	return res, rep, nil
}

func writeCompareJSON(w io.Writer, rep cli.CompareReport) error {
	info, _ := debug.ReadBuildInfo()

	return cli.WriteCompareJSON(w, cli.NewCompareReport(versionString(version, info), rep))
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
