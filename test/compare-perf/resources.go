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
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A run's numbers are worth something only if neither the stand nor the host
// was the limit, so every run records what its containers and the Docker VM
// were doing and the summary marks the runs where one of them was.
//
// The thresholds are hypotheses, not measured limits: that a stand using 90%
// of its CPU limit in a second, or a tool throttled in 10% of the cgroup's
// periods, or a load average above the VM's CPU count, has begun to shape the
// numbers. A marked run stays in the table, the mark beside it.
const (
	standBusyPercent = 90
	throttledPercent = 10
)

// The marks a run can carry, and the checks they come from.
const (
	markStand     = "stand at its cores"
	markThrottled = "tool throttled"
	markHost      = "host busy"
)

// Files a run's folder gets; the first column of each is a Unix time in
// milliseconds, the second the container's role.
const (
	statsFile  = "docker-stats.csv"
	cgroupFile = "cgroup-cpu.csv"
	hostFile   = "host.csv"

	roleStand = "stand"
	roleTool  = "tool"
)

// resources is what a run's resource files say. Peaks are zero where the
// source could not be read, and Unread names the check that then did not run.
type resources struct {
	// Recorded is false for a run that has no resource files at all (a test's
	// stand-in), so a table does not say "none" about nothing.
	Recorded bool

	// CPU is docker stats' percent of one core: a stand with 4 cores can show 400.
	StandPeakCPU, ToolPeakCPU float64
	StandPeakMem, ToolPeakMem int64

	ToolPeriods, ToolThrottled, ToolThrottledUsec int64

	LoadBefore, LoadAfter float64
	HostCPUs              int

	Marks  []string
	Unread []string
}

// marked reports whether the run carries any mark or has a check unread.
func (r resources) marked() bool { return len(r.Marks) > 0 || len(r.Unread) > 0 }

// text is the run's marks and unread checks in one line, empty when clean.
func (r resources) text() string {
	parts := slices.Clone(r.Marks)
	for _, u := range r.Unread {
		parts = append(parts, u+" unread")
	}

	return strings.Join(parts, ", ")
}

// analyseResources reads the three files of a run's folder. A file that is
// missing or has a row it cannot read leaves its checks unread, never clean.
func analyseResources(dir string, standCPUs int) resources {
	r := resources{Recorded: true}

	stats, err := readStats(filepath.Join(dir, statsFile))
	cg, cgErr := readCgroup(filepath.Join(dir, cgroupFile))
	host, hostErr := readHost(filepath.Join(dir, hostFile))
	seconds := runSeconds(cg, host)
	standSeen := false
	standReadings := map[int64]bool{}
	for _, s := range stats {
		switch s.role {
		case roleStand:
			standSeen = true
			standReadings[s.ms] = true
			r.StandPeakCPU = max(r.StandPeakCPU, s.cpu)
			r.StandPeakMem = max(r.StandPeakMem, s.mem)
		case roleTool:
			r.ToolPeakCPU = max(r.ToolPeakCPU, s.cpu)
			r.ToolPeakMem = max(r.ToolPeakMem, s.mem)
		}
	}
	// 100 percent is one core: the stand is at its cores at 90% of standCPUs × 100.
	// A peak over a few readings of a long run says nothing about the rest of it.
	if err != nil || !standSeen || 2*float64(len(standReadings)) < seconds {
		r.Unread = append(r.Unread, "stand CPU")
	} else if r.StandPeakCPU*100 >= float64(standBusyPercent*standCPUs*100) {
		r.Marks = append(r.Marks, markStand)
	}

	tool := slices.DeleteFunc(cg, func(c cgroupRow) bool { return c.role != roleTool })
	switch {
	case cgErr != nil || len(tool) < 2 || tool[len(tool)-1].periods <= tool[0].periods:
		r.Unread = append(r.Unread, "tool throttling")
	default:
		first, last := tool[0], tool[len(tool)-1]
		r.ToolPeriods = last.periods - first.periods
		r.ToolThrottled = last.throttled - first.throttled
		r.ToolThrottledUsec = last.usec - first.usec
		if r.ToolThrottled*100 >= throttledPercent*r.ToolPeriods {
			r.Marks = append(r.Marks, markThrottled)
		}
	}

	// The load before and the load after: one of them alone is half a reading.
	phases := map[string]bool{}
	for _, h := range host {
		phases[h.phase] = true
	}
	if hostErr != nil || !phases["before"] || !phases["after"] {
		r.Unread = append(r.Unread, "host load")
	} else {
		busy := false
		for _, h := range host {
			r.HostCPUs = h.cpus
			if h.phase == "before" {
				r.LoadBefore = h.load
			} else {
				r.LoadAfter = h.load
			}
			busy = busy || h.load > float64(h.cpus)
		}
		if busy {
			r.Marks = append(r.Marks, markHost)
		}
	}

	return r
}

// runSeconds is the length of the run as its files saw it: from the first
// timestamp of the cgroup and host files to the last. Zero when they hold none.
func runSeconds(cg []cgroupRow, host []hostRow) float64 {
	var first, last int64
	seen := false
	note := func(ms int64) {
		if !seen {
			first, last, seen = ms, ms, true
		}
		first, last = min(first, ms), max(last, ms)
	}
	for _, c := range cg {
		note(c.ms)
	}
	for _, h := range host {
		note(h.ms)
	}

	return float64(last-first) / 1000
}

type statsRow struct {
	ms   int64
	role string
	cpu  float64
	mem  int64
}

type cgroupRow struct {
	ms                       int64
	role                     string
	periods, throttled, usec int64
}

type hostRow struct {
	ms    int64
	phase string
	load  float64
	cpus  int
}

// readRecords reads a CSV of the run's folder without its header line.
func readRecords(path, header string, fields int) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out [][]string
	rd := csv.NewReader(f)
	rd.FieldsPerRecord = fields
	for {
		rec, err := rd.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		if rec[0] == header {
			continue
		}
		out = append(out, rec)
	}
}

func readStats(path string) ([]statsRow, error) {
	recs, err := readRecords(path, "unix_ms", 4)
	if err != nil {
		return nil, err
	}
	var out []statsRow
	for _, rec := range recs {
		ms, err0 := strconv.ParseInt(rec[0], 10, 64)
		cpu, err1 := strconv.ParseFloat(rec[2], 64)
		mem, err2 := strconv.ParseInt(rec[3], 10, 64)
		if err := errors.Join(err0, err1, err2); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		out = append(out, statsRow{ms: ms, role: rec[1], cpu: cpu, mem: mem})
	}

	return out, nil
}

func readCgroup(path string) ([]cgroupRow, error) {
	recs, err := readRecords(path, "unix_ms", 5)
	if err != nil {
		return nil, err
	}
	var out []cgroupRow
	for _, rec := range recs {
		var n [4]int64
		var errs []error
		for i, col := range []int{0, 2, 3, 4} {
			var e error
			n[i], e = strconv.ParseInt(rec[col], 10, 64)
			errs = append(errs, e)
		}
		if err := errors.Join(errs...); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		out = append(out, cgroupRow{ms: n[0], role: rec[1], periods: n[1], throttled: n[2], usec: n[3]})
	}
	slices.SortStableFunc(out, func(a, b cgroupRow) int { return int(a.ms - b.ms) })

	return out, nil
}

func readHost(path string) ([]hostRow, error) {
	recs, err := readRecords(path, "phase", 4)
	if err != nil {
		return nil, err
	}
	var out []hostRow
	for _, rec := range recs {
		ms, err0 := strconv.ParseInt(rec[1], 10, 64)
		load, err1 := strconv.ParseFloat(rec[2], 64)
		cpus, err2 := strconv.Atoi(rec[3])
		if err := errors.Join(err0, err1, err2); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		out = append(out, hostRow{ms: ms, phase: rec[0], load: load, cpus: cpus})
	}

	return out, nil
}

// parseStatsLine reads one line of `docker stats --no-stream --format
// "{{.Name}};{{.CPUPerc}};{{.MemUsage}}"`.
func parseStatsLine(line string) (name string, cpu float64, mem int64, err error) {
	parts := strings.Split(strings.TrimSpace(line), ";")
	if len(parts) != 3 {
		return "", 0, 0, fmt.Errorf("docker stats line %q: want name;cpu;memory", line)
	}
	cpu, err = strconv.ParseFloat(strings.TrimSuffix(parts[1], "%"), 64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("docker stats cpu %q: %w", parts[1], err)
	}
	used, _, _ := strings.Cut(parts[2], "/")
	mem, err = parseBytes(strings.TrimSpace(used))

	return parts[0], cpu, mem, err
}

// parseBytes reads docker's "12.5MiB", "340kB", "0B".
func parseBytes(s string) (int64, error) {
	units := []struct {
		suffix string
		scale  float64
	}{
		{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
		{"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"B", 1},
	}
	for _, u := range units {
		if num, ok := strings.CutSuffix(s, u.suffix); ok {
			v, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, fmt.Errorf("docker memory %q: %w", s, err)
			}

			return int64(v * u.scale), nil
		}
	}

	return 0, fmt.Errorf("docker memory %q: unknown unit", s)
}

// parseCPUStat reads a cgroup v2 cpu.stat.
func parseCPUStat(text string) (periods, throttled, usec int64, err error) {
	found := 0
	for _, line := range strings.Split(text, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		var dst *int64
		switch key {
		case "nr_periods":
			dst = &periods
		case "nr_throttled":
			dst = &throttled
		case "throttled_usec":
			dst = &usec
		default:
			continue
		}
		if *dst, err = strconv.ParseInt(val, 10, 64); err != nil {
			return 0, 0, 0, fmt.Errorf("cpu.stat %s: %w", key, err)
		}
		found++
	}
	if found != 3 {
		return 0, 0, 0, errors.New("cpu.stat has no nr_periods, nr_throttled and throttled_usec")
	}

	return periods, throttled, usec, nil
}

// parseLoadavg reads the one-minute load of /proc/loadavg.
func parseLoadavg(text string) (float64, error) {
	f := strings.Fields(text)
	if len(f) == 0 {
		return 0, errors.New("empty /proc/loadavg")
	}

	return strconv.ParseFloat(f[0], 64)
}

// dockerFunc is docker for the sampler; a test answers it from a script.
type dockerFunc func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)

// sampler writes a run's resource files from the moment it starts to the one
// it stops: once a second the cpu and memory of the run's containers and the
// cgroup's cpu.stat, and the Docker VM's load before and after. Whatever a
// call fails to read in a tick (the tool container not yet there, or gone) is
// skipped; the analysis then works with what was read.
type sampler struct {
	dir     string
	roles   map[string]string // container name to role
	hostCPU int
	docker  dockerFunc
	now     func() time.Time

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// startSampler begins sampling at once; the load before is read in the first
// call, before the tool container starts.
func startSampler(ctx context.Context, dir, standName, toolName string, hostCPU int) (*sampler, error) {
	s := &sampler{
		dir: dir, roles: map[string]string{standName: roleStand, toolName: roleTool},
		hostCPU: hostCPU, docker: docker, now: time.Now,
	}
	if err := s.begin(); err != nil {
		return nil, err
	}
	s.host(ctx, "before")
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel, s.done = cancel, make(chan struct{})
	go func() {
		defer close(s.done)
		s.loop(loopCtx)
	}()

	return s, nil
}

// begin writes the header lines.
func (s *sampler) begin() error {
	return errors.Join(
		os.WriteFile(filepath.Join(s.dir, statsFile), []byte("unix_ms,role,cpu_percent,mem_bytes\n"), 0o600),
		os.WriteFile(filepath.Join(s.dir, cgroupFile), []byte("unix_ms,role,nr_periods,nr_throttled,throttled_usec\n"), 0o600),
		os.WriteFile(filepath.Join(s.dir, hostFile), []byte("phase,unix_ms,loadavg1,ncpus\n"), 0o600),
	)
}

// loop starts a stats call every second without waiting for the one before:
// docker stats --no-stream itself takes about two seconds (it samples twice),
// and waiting for it would leave a reading every two.
func (s *sampler) loop(ctx context.Context) {
	var calls sync.WaitGroup
	defer calls.Wait()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		calls.Add(1)
		go func() {
			defer calls.Done()
			s.statsTick(ctx)
		}()
		s.cgroupTick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// stop ends the loop (a stats call still under way is dropped: it would span
// the run's end), reads the stand's cgroup a last time while it is still up,
// and the load after. It runs on a context of its own so an interrupt still
// leaves the files.
func (s *sampler) stop() {
	s.cancel()
	<-s.done
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.cgroupTick(ctx)
	s.host(ctx, "after")
}

func (s *sampler) append(file, row string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, file), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(row)
}

// tick takes one reading of every container that answers.
func (s *sampler) tick(ctx context.Context) {
	s.statsTick(ctx)
	s.cgroupTick(ctx)
}

// statsTick is stamped when docker answers: the percent is of the seconds
// before that.
func (s *sampler) statsTick(ctx context.Context) {
	out, _, err := s.docker(ctx, "stats", "--no-stream", "--format", "{{.Name}};{{.CPUPerc}};{{.MemUsage}}")
	if err != nil {
		return
	}
	ms := s.now().UnixMilli()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, cpu, mem, err := parseStatsLine(line)
		if role, ours := s.roles[name]; err == nil && ours {
			s.append(statsFile, fmt.Sprintf("%d,%s,%s,%d\n", ms, role, strconv.FormatFloat(cpu, 'f', 2, 64), mem))
		}
	}
}

func (s *sampler) cgroupTick(ctx context.Context) {
	ms := s.now().UnixMilli()
	for name, role := range s.roles {
		out, _, err := s.docker(ctx, "exec", name, "cat", "/sys/fs/cgroup/cpu.stat")
		if err != nil {
			continue
		}
		if periods, throttled, usec, err := parseCPUStat(string(out)); err == nil {
			s.append(cgroupFile, fmt.Sprintf("%d,%s,%d,%d,%d\n", ms, role, periods, throttled, usec))
		}
	}
}

// host reads the VM's load through the stand's container: /proc/loadavg is
// the VM's, not the container's.
func (s *sampler) host(ctx context.Context, phase string) {
	for name, role := range s.roles {
		if role != roleStand {
			continue
		}
		out, _, err := s.docker(ctx, "exec", name, "cat", "/proc/loadavg")
		if err != nil {
			return
		}
		if load, err := parseLoadavg(string(out)); err == nil {
			s.append(hostFile, fmt.Sprintf("%s,%d,%s,%d\n", phase, s.now().UnixMilli(), strconv.FormatFloat(load, 'f', 2, 64), s.hostCPU))
		}
	}
}

// vmCPUs is the number of CPUs of the Docker VM.
func vmCPUs(ctx context.Context) (int, error) {
	out, err := dockerOK(ctx, "info", "--format", "{{.NCPU}}")
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(out)
}

// marksCell is the table cell of the runs' resource marks: which run carried
// which mark.
func marksCell(rs []runOut) string {
	var parts []string
	recorded := false
	for i := range rs {
		recorded = recorded || rs[i].Resources.Recorded
		if rs[i].Resources.marked() {
			parts = append(parts, fmt.Sprintf("run %d: %s", i+1, rs[i].Resources.text()))
		}
	}
	switch {
	case len(parts) > 0:
		return strings.Join(parts, "; ")
	case !recorded:
		return "not recorded"
	}

	return "none"
}

// marksSuffix ends a progress line of a run that carries a mark.
func marksSuffix(r runOut) string {
	if !r.Resources.marked() {
		return ""
	}

	return " [" + r.Resources.text() + "]"
}

// marksNote is the sentence under each table that says what the marks are.
func marksNote() string {
	return fmt.Sprintf("Resource marks: %q when the stand used %d%% or more of its CPU limit in any sampled second, %q when "+
		"the tool's container was throttled in %d%% or more of the cgroup's periods, %q when the Docker VM's one-minute load "+
		"average before or after the run was above its CPU count; the thresholds are hypotheses. A marked run stays in the "+
		"table. The files are in the run's folder (%s, %s, %s).",
		markStand, standBusyPercent, markThrottled, throttledPercent, markHost, statsFile, cgroupFile, hostFile)
}
