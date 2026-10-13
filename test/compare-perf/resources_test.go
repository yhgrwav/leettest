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
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// runDir writes the three files of a run's folder from their rows (the header
// lines are added); an empty string leaves the file out.
func runDir(t *testing.T, stats, cgroup, host string) string {
	t.Helper()

	dir := t.TempDir()
	for _, f := range []struct{ name, header, rows string }{
		{statsFile, "unix_ms,role,cpu_percent,mem_bytes\n", stats},
		{cgroupFile, "unix_ms,role,nr_periods,nr_throttled,throttled_usec\n", cgroup},
		{hostFile, "phase,unix_ms,loadavg1,ncpus\n", host},
	} {
		if f.rows == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.header+f.rows), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// quiet is a run in which nothing is wrong: the stand at 150 of 400, the tool
// at its one core, 1% of the periods throttled, a load of 3 on 8 CPUs.
const (
	quietStats = "1000,stand,150.00,1000000\n1000,tool,100.00,2000000\n2000,stand,140.00,1000000\n2000,tool,100.00,2000000\n"
	quietCg    = "1000,tool,1000,2,500\n2000,tool,1100,3,900\n1000,stand,500,0,0\n2000,stand,600,0,0\n"
	quietHost  = "before,900,2.00,8\nafter,2100,3.00,8\n"
)

func TestAnalyseResources_AQuietRunHasNoMark(t *testing.T) {
	r := analyseResources(runDir(t, quietStats, quietCg, quietHost), 4)

	if r.marked() || r.text() != "" {
		t.Errorf("marks %v, unread %v, want none", r.Marks, r.Unread)
	}
	if !r.Recorded || r.StandPeakCPU != 150 || r.ToolPeakCPU != 100 || r.HostCPUs != 8 || r.LoadAfter != 3 || r.LoadBefore != 2 {
		t.Errorf("numbers not read: %+v", r)
	}
	if r.ToolPeriods != 100 || r.ToolThrottled != 1 || r.ToolThrottledUsec != 400 {
		t.Errorf("the throttling difference = %d of %d periods, %d us, want 1 of 100, 400", r.ToolThrottled, r.ToolPeriods, r.ToolThrottledUsec)
	}
}

func TestAnalyseResources_StandAtItsCores(t *testing.T) {
	// 90% of 4 cores is 360. Another second of the same run is lower, the
	// tool at its own core is no mark, and the stand's limit is its own cores:
	// 150 of 400 does not mark (quiet run), 360 of 400 does.
	for _, c := range []struct {
		name   string
		cpu    string
		cores  int
		marked bool
	}{
		{"exactly 90%", "360.00", 4, true},
		{"just under", "359.90", 4, false},
		{"above", "395.00", 4, true},
		{"the limit is the stand's cores", "180.00", 2, true},
		{"the same stand number under more cores", "180.00", 4, false},
	} {
		stats := "1000,stand,50.00,1\n2000,stand," + c.cpu + ",1\n3000,stand,60.00,1\n1000,tool,100.00,1\n"
		got := slices.Contains(analyseResources(runDir(t, stats, quietCg, quietHost), c.cores).Marks, markStand)
		if got != c.marked {
			t.Errorf("%s: %q = %v, want %v", c.name, markStand, got, c.marked)
		}
	}
}

func TestAnalyseResources_TheToolAtItsOwnCoreIsNotTheStandAtItsCores(t *testing.T) {
	stats := "1000,stand,50.00,1\n1000,tool,100.00,1\n2000,tool,100.00,1\n"
	if r := analyseResources(runDir(t, stats, quietCg, quietHost), 1); r.marked() {
		t.Errorf("marks %v unread %v: the tool's 100%% of its core is expected, only the stand's CPU is judged", r.Marks, r.Unread)
	}
}

func TestAnalyseResources_ToolThrottled(t *testing.T) {
	for _, c := range []struct {
		name   string
		cg     string
		marked bool
	}{
		{"exactly 10% of the periods", "1000,tool,0,0,0\n2000,tool,1000,100,5000\n", true},
		{"just under", "1000,tool,0,0,0\n2000,tool,1000,99,5000\n", false},
		{"the difference, not the totals: 47% in all, 5% in the run", "1000,tool,1000,900,0\n2000,tool,2000,950,0\n", false},
		{"the difference again: 1% in all, 20% in the run", "1000,tool,5000,10,0\n2000,tool,5100,30,0\n", true},
		{"the first and the last read, not the ones between", "1000,tool,0,0,0\n1500,tool,500,400,0\n2000,tool,1000,50,0\n", false},
	} {
		got := slices.Contains(analyseResources(runDir(t, quietStats, c.cg, quietHost), 4).Marks, markThrottled)
		if got != c.marked {
			t.Errorf("%s: %q = %v, want %v", c.name, markThrottled, got, c.marked)
		}
	}
}

func TestAnalyseResources_OnlyTheToolsThrottlingCounts(t *testing.T) {
	cg := "1000,tool,0,0,0\n2000,tool,1000,0,0\n1000,stand,0,0,0\n2000,stand,1000,900,0\n"
	if r := analyseResources(runDir(t, quietStats, cg, quietHost), 4); slices.Contains(r.Marks, markThrottled) {
		t.Errorf("marks %v: the stand throttled is not the tool throttled", r.Marks)
	}
}

func TestAnalyseResources_HostBusy(t *testing.T) {
	for _, c := range []struct {
		name   string
		host   string
		marked bool
	}{
		{"load equal to the CPU count is not above it", "before,1,8.00,8\nafter,2,8.00,8\n", false},
		{"above after the run", "before,1,2.00,8\nafter,2,8.01,8\n", true},
		{"above before the run", "before,1,9.50,8\nafter,2,1.00,8\n", true},
		{"half the CPUs is nothing", "before,1,4.00,8\nafter,2,4.00,8\n", false},
		{"the VM's CPU count of the file", "before,1,3.00,2\nafter,2,3.00,2\n", true},
	} {
		got := slices.Contains(analyseResources(runDir(t, quietStats, quietCg, c.host), 4).Marks, markHost)
		if got != c.marked {
			t.Errorf("%s: %q = %v, want %v", c.name, markHost, got, c.marked)
		}
	}
}

func TestAnalyseResources_AMarkedRunNamesEveryMark(t *testing.T) {
	stats := "1000,stand,380.00,1\n"
	cg := "1000,tool,0,0,0\n2000,tool,100,50,9\n"
	host := "before,1,12.00,8\nafter,2,12.00,8\n"

	got := analyseResources(runDir(t, stats, cg, host), 4).text()

	for _, want := range []string{markStand, markThrottled, markHost} {
		if !strings.Contains(got, want) {
			t.Errorf("text %q lacks %q", got, want)
		}
	}
}

func TestAnalyseResources_WhatCannotBeReadIsUnreadNeverClean(t *testing.T) {
	for _, c := range []struct {
		name            string
		stats, cg, host string
		unread          []string
	}{
		{"no file at all", "", "", "", []string{"stand CPU", "tool throttling", "host load"}},
		{"no stand row", "1000,tool,100.00,1\n", quietCg, quietHost, []string{"stand CPU"}},
		{"one cgroup read of the tool", quietStats, "1000,tool,1000,2,500\n", quietHost, []string{"tool throttling"}},
		{"no period passed", quietStats, "1000,tool,1000,2,500\n2000,tool,1000,2,500\n", quietHost, []string{"tool throttling"}},
		{"a row that is not a number", "1000,stand,lots,1\n", quietCg, quietHost, []string{"stand CPU"}},
		{"no load", quietStats, quietCg, "", []string{"host load"}},
		{"no load after the run", quietStats, quietCg, "before,900,2.00,8\n", []string{"host load"}},
		{"no load before the run", quietStats, quietCg, "after,2100,3.00,8\n", []string{"host load"}},
		{"four stand readings in a 10 s run", statsEvery(1000, 4), tenSecondCg, tenSecondHost, []string{"stand CPU"}},
	} {
		r := analyseResources(runDir(t, c.stats, c.cg, c.host), 4)
		if !slices.Equal(r.Unread, c.unread) {
			t.Errorf("%s: unread %v, want %v", c.name, r.Unread, c.unread)
		}
		if !r.marked() || !strings.Contains(r.text(), "unread") {
			t.Errorf("%s: text %q, want it to say unread", c.name, r.text())
		}
	}
}

// Half of the run's seconds is enough to judge the stand by; one reading less
// is not. Both sides, so that neither the bound nor "any reading at all" passes.
func TestAnalyseResources_StandReadingsForHalfTheRunAreEnough(t *testing.T) {
	for _, c := range []struct {
		readings int
		unread   bool
	}{{5, false}, {4, true}, {10, false}} {
		r := analyseResources(runDir(t, statsEvery(1000, c.readings), tenSecondCg, tenSecondHost), 4)
		if got := slices.Contains(r.Unread, "stand CPU"); got != c.unread {
			t.Errorf("%d readings of 10 s: unread %v, want %v", c.readings, r.Unread, c.unread)
		}
	}
}

// statsEvery is n stand readings, one every second from ms on, the stand at
// 100% of 400.
func statsEvery(ms int64, n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "%d,stand,100.00,1000000\n", ms+int64(i)*1000)
	}

	return b.String()
}

// A run of 10 s by its other files: the tool's cgroup and the load.
const (
	tenSecondCg   = "1000,tool,1000,2,500\n11000,tool,1100,3,900\n"
	tenSecondHost = "before,1000,2.00,8\nafter,11000,3.00,8\n"
)

func TestParseStatsLine(t *testing.T) {
	name, cpu, mem, err := parseStatsLine("leetperf-stand-1;231.45%;12.5MiB / 7.658GiB\n")
	if err != nil || name != "leetperf-stand-1" || cpu != 231.45 || mem != 13107200 {
		t.Errorf("got %q %v %d %v", name, cpu, mem, err)
	}
	for in, want := range map[string]int64{"0B": 0, "340kB": 340000, "2GiB": 2 << 30, "1.5MB": 1500000, "10KiB": 10240} {
		if got, err := parseBytes(in); err != nil || got != want {
			t.Errorf("parseBytes(%q) = %d, %v, want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "a;b", "n;--;1MiB / 2GiB", "n;1.00%;lots / 2GiB"} {
		if _, _, _, err := parseStatsLine(bad); err == nil {
			t.Errorf("parseStatsLine(%q) read", bad)
		}
	}
}

func TestParseCPUStat(t *testing.T) {
	text := "usage_usec 8123\nuser_usec 7000\nsystem_usec 1123\nnr_periods 40\nnr_throttled 7\nthrottled_usec 61000\nnr_bursts 0\n"
	p, th, us, err := parseCPUStat(text)
	if err != nil || p != 40 || th != 7 || us != 61000 {
		t.Errorf("got %d %d %d %v", p, th, us, err)
	}
	if _, _, _, err := parseCPUStat("usage_usec 1\n"); err == nil {
		t.Error("a cpu.stat without throttling counters was read")
	}
	if load, err := parseLoadavg("3.52 2.10 1.00 4/812 9021\n"); err != nil || load != 3.52 {
		t.Errorf("loadavg = %v, %v", load, err)
	}
}

// scriptedDocker answers the sampler's docker calls: the stand and the tool
// both exist, a third container of somebody else too.
func scriptedDocker(toolUp *bool, cgroupFails bool) dockerFunc {
	return func(_ context.Context, args ...string) ([]byte, []byte, error) {
		switch {
		case args[0] == "stats":
			out := "other;300.00%;1MiB / 2GiB\nleetperf-stand-1;200.00%;10MiB / 2GiB\n"
			if *toolUp {
				out += "leetperf-leettest-1;99.00%;20MiB / 2GiB\n"
			}

			return []byte(out), nil, nil
		case args[0] == "exec" && args[2] == "cat" && args[3] == "/proc/loadavg":
			return []byte("1.50 1.00 1.00 1/1 1\n"), nil, nil
		case args[0] == "exec" && args[3] == "/sys/fs/cgroup/cpu.stat":
			if cgroupFails || (args[1] == "leetperf-leettest-1" && !*toolUp) {
				return nil, []byte("no such container"), errors.New("exit 1")
			}

			return []byte("nr_periods 10\nnr_throttled 1\nthrottled_usec 7\n"), nil, nil
		}

		return nil, nil, errors.New("unexpected docker call")
	}
}

func TestSampler_WritesFilesTheAnalysisReadsAndSkipsWhatDoesNotAnswer(t *testing.T) {
	dir := t.TempDir()
	toolUp := false
	s := &sampler{
		dir: dir, roles: map[string]string{"leetperf-stand-1": roleStand, "leetperf-leettest-1": roleTool},
		hostCPU: 8, docker: scriptedDocker(&toolUp, false),
		now: func() time.Time { return time.UnixMilli(5000) },
	}
	if err := s.begin(); err != nil {
		t.Fatal(err)
	}
	s.host(context.Background(), "before")
	s.tick(context.Background()) // the tool's container is not there yet
	toolUp = true
	s.tick(context.Background())
	s.host(context.Background(), "after")

	stats, err := readStats(filepath.Join(dir, statsFile))
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for _, r := range stats {
		roles = append(roles, r.role)
	}
	slices.Sort(roles)
	if want := []string{roleStand, roleStand, roleTool}; !slices.Equal(roles, want) {
		t.Errorf("stats rows of %v, want %v (the other container is not ours, the tool is read once it is up)", roles, want)
	}
	cg, err := readCgroup(filepath.Join(dir, cgroupFile))
	if err != nil || len(cg) != 3 {
		t.Errorf("cgroup rows %v, %v, want 3 (stand twice, tool once)", cg, err)
	}
	host, err := readHost(filepath.Join(dir, hostFile))
	if err != nil || len(host) != 2 || host[0].phase != "before" || host[1].phase != "after" || host[0].cpus != 8 || host[0].load != 1.5 {
		t.Errorf("host rows %+v, %v", host, err)
	}
	if r := analyseResources(dir, 4); !r.Recorded || r.StandPeakCPU != 200 || r.ToolPeakCPU != 99 || r.HostCPUs != 8 {
		t.Errorf("analysis of what the sampler wrote: %+v", r)
	}
}

func TestSampler_NothingReadLeavesHeadersAndTheAnalysisUnread(t *testing.T) {
	dir := t.TempDir()
	toolUp := true
	s := &sampler{
		dir: dir, roles: map[string]string{"leetperf-stand-1": roleStand, "leetperf-leettest-1": roleTool},
		hostCPU: 8, docker: scriptedDocker(&toolUp, true), now: time.Now,
	}
	if err := s.begin(); err != nil {
		t.Fatal(err)
	}
	s.tick(context.Background())

	r := analyseResources(dir, 4)
	if !slices.Contains(r.Unread, "tool throttling") || slices.Contains(r.Unread, "stand CPU") {
		t.Errorf("unread %v, want the throttling only (stats answered, cpu.stat did not)", r.Unread)
	}
}

func marked(text string) runOut {
	return runOut{Resources: resources{Recorded: true, Marks: []string{text}}}
}

func TestMarksCell_NamesTheRunsAndSaysNoneOrNotRecorded(t *testing.T) {
	clean := runOut{Resources: resources{Recorded: true}}

	if got := marksCell([]runOut{clean, marked(markStand), clean, marked(markHost)}); got != "run 2: "+markStand+"; run 4: "+markHost {
		t.Errorf("cell = %q", got)
	}
	if got := marksCell([]runOut{clean, clean}); got != "none" {
		t.Errorf("cell = %q, want none", got)
	}
	if got := marksCell([]runOut{{}, {}}); got != "not recorded" {
		t.Errorf("cell = %q: a run that has no files must not be called clean", got)
	}
	unread := runOut{Resources: resources{Recorded: true, Unread: []string{"host load"}}}
	if got := marksCell([]runOut{clean, unread}); !strings.Contains(got, "run 2: host load unread") {
		t.Errorf("cell = %q", got)
	}
}

func TestTables_AMarkedRunStaysInTheTableWithItsMark(t *testing.T) {
	f, _ := parseFlags([]string{"-tools", "ghz"}, io.Discard)
	rs := []runOut{
		{
			Reports:   toolResult{P50: 21 * time.Millisecond, P99: 30 * time.Millisecond},
			Stand:     standSummary{HeldP50: 20 * time.Millisecond, HeldP99: 25 * time.Millisecond},
			Resources: resources{Recorded: true},
		},
		{
			Reports:   toolResult{P50: 22 * time.Millisecond, P99: 31 * time.Millisecond},
			Stand:     standSummary{HeldP50: 20 * time.Millisecond, HeldP99: 25 * time.Millisecond},
			Resources: resources{Recorded: true, Marks: []string{markStand}},
		},
	}
	point1 := point{rate: 1000, runs: []runOut{{Verdict: verdict{Held: true, Share: 1}, Resources: rs[1].Resources}}}

	var row1, row2, row3 strings.Builder
	row1Table(&row1, f, map[string][]point{ghz: {point1}}, nil)
	rs2 := slices.Clone(rs)
	for i := range rs2 {
		rs2[i].Spacing = &spacing{}
	}
	row2Table(&row2, f, map[string][]runOut{ghz: rs2})
	row3Table(&row3, f, map[string][]runOut{ghz: rs})

	for name, text := range map[string]string{"row 1": row1.String(), "row 2": row2.String(), "row 3": row3.String()} {
		if !strings.Contains(text, "resource marks") || !strings.Contains(text, markStand) || !strings.Contains(text, "hypotheses") {
			t.Errorf("%s lacks the marks column, the mark or the word on the thresholds:\n%s", name, text)
		}
	}
	// Both runs are in row 3's medians: 21 and 22 ms give a 1 ms [0..2] range around 22 - 20.
	if got := rowOf(row3.String(), "| ghz |"); !strings.Contains(got, "run 2: "+markStand) || !strings.Contains(got, "[1ms–2ms]") {
		t.Errorf("row 3: %q, want run 2 marked and both runs counted", got)
	}
}

func TestMarksSuffix(t *testing.T) {
	if got := marksSuffix(marked(markHost)); got != " ["+markHost+"]" {
		t.Errorf("suffix = %q", got)
	}
	if got := marksSuffix(runOut{Resources: resources{Recorded: true}}); got != "" {
		t.Errorf("suffix = %q for a clean run", got)
	}
}

func TestPlan_SaysWhatIsRecordedAndTheThresholds(t *testing.T) {
	f, _ := parseFlags([]string{"-plan"}, io.Discard)
	text := plan(f)

	for _, want := range []string{"docker stats", "cpu.stat", "90%", "10%", "hypotheses"} {
		if !strings.Contains(text, want) {
			t.Errorf("plan lacks %q", want)
		}
	}
}

// testdata/resources-live is what the sampler wrote in one live run on Docker
// Desktop (LeetTest at 1000 rps for 10 s, stand 4 cores, tool 1, a VM of 24
// CPUs): the file formats as Docker produced them.
func TestAnalyseResources_TheFilesOfALiveRun(t *testing.T) {
	r := analyseResources(filepath.Join("testdata", "resources-live"), 4)

	if r.marked() {
		t.Errorf("marks %v unread %v, want a clean run (stand ~34 of 400, tool ~31 of 100, nothing throttled, load 0 of 24)", r.Marks, r.Unread)
	}
	if r.HostCPUs != 24 || r.StandPeakCPU != 34.37 || r.ToolPeakCPU != 31.59 || r.ToolPeriods != 98 || r.ToolThrottled != 0 {
		t.Errorf("numbers: %+v", r)
	}
}
