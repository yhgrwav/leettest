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
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const prefix = "leetperf"

// flags is every setting of a measurement; plan prints them all.
type flags struct {
	out        string
	rows       []int
	tools      []string
	runs       int
	duration   time.Duration
	rateStart  int
	rateFactor float64
	rateMax    int
	fixedRate  int
	latRate    int
	latDelay   time.Duration
	toolCPUs   int
	standCPUs  int
	ctlCPUs    int
	k6VUs      int
	image      string
	goImage    string
	k6Image    string
	ghzVersion string
	keep       bool
	plan       bool
}

func parseFlags(args []string, usage io.Writer) (flags, error) {
	var f flags
	var rows, tools string

	fs := flag.NewFlagSet("compare-perf", flag.ContinueOnError)
	fs.SetOutput(usage)
	fs.StringVar(&f.out, "out", filepath.Join("test", "compare-perf", "out"), "directory for binaries, raw outputs and tables")
	fs.StringVar(&rows, "rows", "1,2,3", "rows to measure: 1 rate per core, 2 schedule accuracy, 3 the generator's share of latency")
	fs.StringVar(&tools, "tools", "leettest,ghz,k6", "tools to measure, comma-separated")
	fs.IntVar(&f.runs, "runs", 3, "runs of each tool at each point")
	fs.DurationVar(&f.duration, "duration", 10*time.Second, "length of every run, at least 4s: the first and last seconds are not judged")
	fs.IntVar(&f.rateStart, "rate-start", 1000, "row 1: first rate of the ladder, calls a second")
	fs.Float64Var(&f.rateFactor, "rate-factor", 1.25, "row 1: each step multiplies the rate by this")
	fs.IntVar(&f.rateMax, "rate-max", 200000, "row 1: no step above this rate")
	fs.IntVar(&f.fixedRate, "fixed-rate", 1000, "row 2: the one rate")
	fs.IntVar(&f.latRate, "latency-rate", 500, "row 3: the one rate, moderate")
	fs.DurationVar(&f.latDelay, "latency-delay", 20*time.Millisecond, "row 3: how long the stand holds every answer")
	fs.IntVar(&f.toolCPUs, "tool-cpus", 1, "cores of a tool's container, and its GOMAXPROCS")
	fs.IntVar(&f.standCPUs, "stand-cpus", 4, "cores of the stand's container")
	fs.IntVar(&f.ctlCPUs, "control-cpus", 4, "row 1 control: the fastest tool again with this many cores, from its highest held rate up, "+
		"to tell the tool's limit from the stand's; 0 for no control")
	fs.IntVar(&f.k6VUs, "k6-vus", 100, "k6's virtual users, all pre-allocated; k6 opens a connection for each, the others use one")
	fs.StringVar(&f.image, "image", "debian:stable-slim", "image the stand, LeetTest and ghz run in")
	fs.StringVar(&f.goImage, "go-image", "golang:1.27.1", "image that builds ghz")
	fs.StringVar(&f.k6Image, "k6-image", "grafana/k6:2.3.0", "k6 image; its entrypoint is k6")
	fs.StringVar(&f.ghzVersion, "ghz-version", "v0.121.0", "ghz version, installed with go install")
	fs.BoolVar(&f.keep, "keep-arrivals", false, "keep every run's arrivals file (about 9 bytes a call)")
	fs.BoolVar(&f.plan, "plan", false, "print every setting and command and the number of runs, run nothing")

	if err := fs.Parse(args); err != nil {
		return f, err
	}

	for _, r := range strings.Split(rows, ",") {
		if len(r) != 1 || r[0] < '1' || r[0] > '3' {
			return f, fmt.Errorf("-rows %q: want 1, 2 and 3 separated by commas", rows)
		}
		f.rows = append(f.rows, int(r[0]-'0'))
	}
	f.tools = strings.Split(tools, ",")
	for _, t := range f.tools {
		if !slices.Contains([]string{leettest, ghz, k6}, t) {
			return f, fmt.Errorf("-tools: unknown tool %q, want leettest, ghz or k6", t)
		}
	}

	switch {
	case f.duration < minDuration:
		return f, fmt.Errorf("-duration %v is under %v", f.duration, minDuration)
	case f.runs < 1, f.toolCPUs < 1, f.standCPUs < 1, f.k6VUs < 1, f.rateStart < 1, f.fixedRate < 1, f.latRate < 1:
		return f, errors.New("-runs, -tool-cpus, -stand-cpus, -k6-vus and the rates must be at least 1")
	case f.ctlCPUs != 0 && f.ctlCPUs <= f.toolCPUs:
		return f, errors.New("-control-cpus must be above -tool-cpus, or 0 for no control")
	case f.rateFactor <= 1:
		return f, errors.New("-rate-factor must be above 1")
	case f.rateMax < f.rateStart:
		return f, errors.New("-rate-max is below -rate-start")
	case f.latDelay <= 0:
		return f, errors.New("-latency-delay must be positive: row 3 subtracts the stand's own time")
	}

	return f, nil
}

func (f flags) base() setup {
	return setup{
		Duration: f.duration, ToolCPUs: f.toolCPUs, StandCPUs: f.standCPUs, K6VUs: f.k6VUs,
		Image: f.image, K6Image: f.k6Image,
	}
}

// ladder is the rates row 1 steps through, the same for every tool: each
// step is start times factor to the step, rounded to 50 and at least 50 above
// the one before.
func ladder(start int, factor float64, top int) []int {
	const round = 50

	rates := []int{start}
	for k := 1; ; k++ {
		next := int(math.Round(float64(start)*math.Pow(factor, float64(k))/round) * round)
		next = max(next, rates[len(rates)-1]+round)
		if next > top {
			return rates
		}
		rates = append(rates, next)
	}
}

// runFunc does one run of a tool and returns what was measured; the harness
// uses execute, a test a script.
type runFunc func(ctx context.Context, tool string, s setup, label string) (runOut, error)

// harness is the state shared by every run.
type harness struct {
	f             flags
	out, binMount string
	network       string
	prefix        string
	n             int
	keepArrivals  bool
	run           runFunc
	// hostCPUs is the Docker VM's CPU count, read once.
	hostCPUs int
}

func (h *harness) vmCPUs(ctx context.Context) (int, error) {
	if h.hostCPUs == 0 {
		n, err := vmCPUs(ctx)
		if err != nil {
			return 0, err
		}
		h.hostCPUs = n
	}

	return h.hostCPUs, nil
}

// runLabel names a run's folder. The cores are in it: the same tool at the
// same rate with another number of cores is another run, never to be written
// over the first.
func runLabel(row int, tool string, cpus int, rest ...int) string {
	parts := make([]string, 0, 3+len(rest))
	parts = append(parts, fmt.Sprintf("row%d", row), tool, fmt.Sprintf("c%d", cpus))
	for _, n := range rest {
		parts = append(parts, strconv.Itoa(n))
	}

	return strings.Join(parts, "-")
}

func main() { os.Exit(realMain()) }

func realMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "compare-perf:", err)

		return 1
	}

	return 0
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	f, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}

	planText := plan(f)
	if f.plan {
		_, err = io.WriteString(stdout, planText)

		return err
	}

	return measure(ctx, f, planText, stdout)
}

// measure builds, runs the chosen rows and writes the tables.
func measure(ctx context.Context, f flags, planText string, stdout io.Writer) error {
	bin := filepath.Join(f.out, "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(f.out, "plan.txt"), []byte(planText), 0o600); err != nil {
		return err
	}
	if _, err := io.WriteString(stdout, planText); err != nil {
		return err
	}

	old, err := leftovers(ctx, prefix)
	if err != nil {
		return err
	}
	if len(old) > 0 {
		return fmt.Errorf("containers of an earlier run are still there (%v): docker rm -f them first", old)
	}
	err = prepare(ctx, f, bin)
	if err != nil {
		return err
	}

	binMount, err := mountPath(bin)
	if err != nil {
		return err
	}
	h := &harness{f: f, out: f.out, binMount: binMount, network: prefix + "-net", prefix: prefix, keepArrivals: f.keep}
	h.run = h.execute
	_, _, _ = docker(ctx, "network", "rm", h.network)
	if _, err = dockerOK(ctx, "network", "create", h.network); err != nil {
		return err
	}
	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, _, _ = docker(cleanCtx, "network", "rm", h.network)
	}()

	var summary strings.Builder
	var runErr error
	for _, row := range f.rows {
		switch row {
		case 1:
			runErr = h.row1(ctx, &summary, stdout)
		case 2:
			runErr = h.row2(ctx, &summary, stdout)
		case 3:
			runErr = h.row3(ctx, &summary, stdout)
		}
		if runErr != nil {
			break
		}
	}

	fmt.Fprint(stdout, "\n", summary.String())
	writeErr := os.WriteFile(filepath.Join(f.out, "summary.md"), []byte(summary.String()), 0o600)

	left, leftErr := leftovers(context.Background(), prefix)
	if len(left) > 0 {
		leftErr = errors.Join(leftErr, fmt.Errorf("containers left behind: %v", left))
	}

	return errors.Join(runErr, writeErr, leftErr)
}

// prepare builds the Linux binaries and makes sure the images are there.
func prepare(ctx context.Context, f flags, bin string) error {
	if err := linuxBuild(ctx, "./test/stand/cmd/stand", filepath.Join(bin, "stand")); err != nil {
		return err
	}
	if slices.Contains(f.tools, leettest) {
		if err := linuxBuild(ctx, "./cmd/leettest", filepath.Join(bin, "leettest")); err != nil {
			return err
		}
	}
	images := []string{f.image}
	if slices.Contains(f.tools, ghz) {
		if err := ensureImage(ctx, f.goImage); err != nil {
			return err
		}
		if err := installGhz(ctx, f.goImage, f.ghzVersion, bin); err != nil {
			return err
		}
	}
	if slices.Contains(f.tools, k6) {
		images = append(images, f.k6Image)
	}
	for _, image := range images {
		if err := ensureImage(ctx, image); err != nil {
			return err
		}
	}

	return nil
}

func ensureImage(ctx context.Context, image string) error {
	if _, _, err := docker(ctx, "image", "inspect", image); err == nil {
		return nil
	}
	_, err := dockerOK(ctx, "pull", image)

	return err
}

// rotated is tools with the order shifted by i, so no tool always runs first
// or last.
func rotated(tools []string, i int) []string {
	out := make([]string, len(tools))
	for j := range tools {
		out[j] = tools[(i+j)%len(tools)]
	}

	return out
}
