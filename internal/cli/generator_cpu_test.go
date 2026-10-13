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
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// The generator's CPU: the CLI reads the process's CPU time beside the run,
// lays the readings on the engine's second grid (second i is [StartedAt+i s,
// StartedAt+(i+1) s), warm-up included, as methods[].seconds and silent_from_s)
// and says how much of the CPU the process could use it took.

var cpuStart = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

// cpuNull marks, in a list of wanted seconds, an entry that must be null.
var cpuNull = math.NaN()

// cpuSecs is a span of f seconds, to the nanosecond: 0.8949 s is 894900000 ns,
// not a float's neighbour of it.
func cpuSecs(f float64) time.Duration {
	return time.Duration(math.Round(f * 1e9))
}

// cpuAt is the reading "cpu seconds of CPU time used, wall seconds after the
// run's StartedAt".
func cpuAt(wall, cpu float64) CPUSample {
	return CPUSample{At: cpuStart.Add(cpuSecs(wall)), CPU: cpuSecs(cpu)}
}

// perSecond is one reading on every second edge from 0: the process used
// shares[i] of cores in second i.
func perSecond(cores float64, shares ...float64) []CPUSample {
	out := []CPUSample{cpuAt(0, 0)}
	var used time.Duration
	for i, s := range shares {
		used += cpuSecs(s * cores)
		out = append(out, CPUSample{At: cpuStart.Add(time.Duration(i+1) * time.Second), CPU: used})
	}

	return out
}

// cpuReport is a run of stages that total planned seconds, the first warmup
// of them warm-up, that ran for ran seconds; the longest of its methods has n
// seconds on the grid, the other one second.
func cpuReport(planned, warmup, ran float64, n int) engine.Report {
	return engine.Report{
		StartedAt: cpuStart, Planned: cpuSecs(planned), Warmup: cpuSecs(warmup), Duration: cpuSecs(ran),
		Methods: []engine.MethodReport{
			{Method: "/pkg.S/M", Seconds: make([]engine.Second, n)},
			{Method: "/pkg.S/N", Seconds: make([]engine.Second, 1)},
		},
	}
}

func layCPU(t *testing.T, report engine.Report, cores float64, samples []CPUSample) *GeneratorCPU {
	t.Helper()

	got := LayGeneratorCPU(report, samples, cores)
	if got == nil {
		t.Fatalf("LayGeneratorCPU = nil for %d readings, want a measurement", len(samples))
	}

	return got
}

func wantCPUSeconds(t *testing.T, got []*float64, want ...float64) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("seconds has %d entries, want %d: %s", len(got), len(want), showSeconds(got))
	}
	for i, w := range want {
		switch {
		case math.IsNaN(w):
			if got[i] != nil {
				t.Errorf("seconds[%d] = %.2f, want null", i, *got[i])
			}
		case got[i] == nil:
			t.Errorf("seconds[%d] = null, want %.2f", i, w)
		case math.Abs(*got[i]-w) > 1e-9:
			t.Errorf("seconds[%d] = %v, want %.2f", i, *got[i], w)
		}
	}
}

func showSeconds(s []*float64) string {
	parts := make([]string, len(s))
	for i, v := range s {
		parts[i] = "null"
		if v != nil {
			parts[i] = fmt.Sprintf("%.2f", *v)
		}
	}

	return "[" + strings.Join(parts, " ") + "]"
}

func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// oneSecond is a run of exactly one second in which the process used cpu
// seconds of CPU time of the given cores.
func oneSecond(t *testing.T, cpu, cores float64) *GeneratorCPU {
	t.Helper()

	return layCPU(t, cpuReport(1, 0, 1, 1), cores, []CPUSample{cpuAt(0, 0), cpuAt(1, cpu)})
}

var shareCases = []struct {
	name      string
	cpu       float64
	cores     float64
	share     float64
	busy      int
	firstBusy int // -1: none
}{
	{"exactly 0.9", 0.9, 1, 0.90, 1, 0},
	{"0.895 rounds up to 0.90", 0.895, 1, 0.90, 1, 0},
	{"0.8949 rounds down to 0.89", 0.8949, 1, 0.89, 0, -1},
	{"two cores 1.8 s", 1.8, 2, 0.90, 1, 0},
	{"0.285 is a half and rounds up", 0.285, 1, 0.29, 0, -1},
	{"half the core", 0.5, 1, 0.50, 0, -1},
	{"above the core is kept as measured", 1.3, 1, 1.30, 1, 0},
}

// Ground: boundary — a share on the edge of a rounding (0.895, 0.285, 0.8949)
// is chosen by the test; no run lands there on its own.
func TestGeneratorCPU_ShareIsRoundedHalfUpToTwoDecimals(t *testing.T) {
	for _, tc := range shareCases {
		got := oneSecond(t, tc.cpu, tc.cores)

		if !near(got.Share, tc.share) {
			t.Errorf("%s: share = %v, want %.2f", tc.name, got.Share, tc.share)
		}
		wantCPUSeconds(t, got.Seconds, tc.share)
		if got.AvailableCores != tc.cores {
			t.Errorf("%s: available cores = %v, want %v", tc.name, got.AvailableCores, tc.cores)
		}
	}
}

// The value a reader sees is the value that was compared: a second printed
// 0.90 is busy, one printed 0.89 is not.
//
// Ground: boundary — see above.
func TestGeneratorCPU_ASecondIsBusyWhenItsRoundedShareIsNinetyPercent(t *testing.T) {
	for _, tc := range shareCases {
		got := oneSecond(t, tc.cpu, tc.cores)

		if got.BusySeconds != tc.busy {
			t.Errorf("%s: busy seconds = %d, want %d", tc.name, got.BusySeconds, tc.busy)
		}
		if tc.firstBusy < 0 {
			if got.FirstBusy != nil {
				t.Errorf("%s: first busy second = %d, want none", tc.name, *got.FirstBusy)
			}
		} else if got.FirstBusy == nil || *got.FirstBusy != tc.firstBusy {
			t.Errorf("%s: first busy second = %v, want %d", tc.name, got.FirstBusy, tc.firstBusy)
		}
		if got.Counted != 1 {
			t.Errorf("%s: counted seconds = %d, want 1", tc.name, got.Counted)
		}
	}
}

// Readings land late on a busy P: the CPU between two readings is spread over
// the wall time between them, so a late reading moves none of it to the wrong
// second.
//
// Ground: boundary — a reading straddling a second edge.
func TestGeneratorCPU_ACPUIntervalIsSplitByWallTimeAcrossTheSecondsItCovers(t *testing.T) {
	// 0.0 s -> 1.5 s at one core: 1.0 CPU-second in the first second, 0.5 in
	// the half second of the next, the share of both is 1.0.
	got := layCPU(t, cpuReport(1.5, 0, 1.5, 2), 1, []CPUSample{cpuAt(0, 0), cpuAt(1.5, 1.5)})
	wantCPUSeconds(t, got.Seconds, 1.0, 1.0)
	if got.BusySeconds != 2 || got.Counted != 2 {
		t.Errorf("busy %d of %d counted seconds, want 2 of 2", got.BusySeconds, got.Counted)
	}

	// A busy half second, then a slower one that ends past the edge: the
	// second 0 is 0.5 CPU-s from the first interval and the half of the second
	// interval's 0.5 CPU-s that falls before 1.0 s.
	got = layCPU(t, cpuReport(1.5, 0, 1.5, 2), 1, []CPUSample{cpuAt(0, 0), cpuAt(0.5, 0.5), cpuAt(1.5, 1.0)})
	wantCPUSeconds(t, got.Seconds, 0.75, 0.50)
}

// Two readings at one instant are an interval of no wall time: no share is
// worked out over it.
func TestGeneratorCPU_TwoReadingsAtOneInstantGiveNoNaN(t *testing.T) {
	got := layCPU(t, cpuReport(2, 0, 2, 2), 1,
		[]CPUSample{cpuAt(0, 0), cpuAt(1, 0.5), cpuAt(1, 0.5), cpuAt(2, 1.0)})

	wantCPUSeconds(t, got.Seconds, 0.50, 0.50)
	if math.IsNaN(got.Share) || math.IsInf(got.Share, 0) || !near(got.Share, 0.50) {
		t.Errorf("share = %v, want 0.50", got.Share)
	}
}

// Stages total 7 s, the first 2 of them warm-up (load.warmup counts toward the
// duration), the timeouts run on 1 s past the plan: 8 seconds on the grid,
// the planned ones after the warm-up are 2..6.
//
// Ground: boundary — which seconds the plan's edges include.
func TestGeneratorCPU_TheGridRunsToTheEndOfTheRunWarmupIncluded(t *testing.T) {
	got := layCPU(t, cpuReport(7, 2, 8, 8), 1, perSecond(1, .3, .3, .5, .5, .5, .5, .5, .3))

	// The longest methods[].seconds is the length, not the first or the last.
	wantCPUSeconds(t, got.Seconds, .3, .3, .5, .5, .5, .5, .5, .3)
}

func TestGeneratorCPU_OnlyThePlannedSecondsAfterWarmupAreCounted(t *testing.T) {
	// The warm-up (0, 1) and the timeout tail (7) are at 0.99; the counted
	// seconds 2..6 at 0.50.
	got := layCPU(t, cpuReport(7, 2, 8, 8), 1, perSecond(1, .99, .99, .5, .5, .5, .5, .5, .99))

	if got.Counted != 5 {
		t.Errorf("counted seconds = %d, want 5 (seconds 2..6)", got.Counted)
	}
	if got.BusySeconds != 0 || got.FirstBusy != nil {
		t.Errorf("busy %d, first %v; the warm-up and the tail are busy but not counted", got.BusySeconds, got.FirstBusy)
	}
	if !near(got.Share, 0.50) {
		t.Errorf("share = %v, want 0.50 over the counted seconds only", got.Share)
	}
}

// Warm-up of 1.5 s: second 1 is half warm-up and not counted.
func TestGeneratorCPU_TheSecondWhereWarmupEndsIsNotCounted(t *testing.T) {
	got := layCPU(t, cpuReport(7, 1.5, 8, 8), 1, perSecond(1, .99, .99, .5, .5, .5, .5, .5, .99))

	if got.Counted != 5 {
		t.Errorf("counted seconds = %d, want 5 (seconds 2..6)", got.Counted)
	}
	if got.BusySeconds != 0 || got.FirstBusy != nil {
		t.Errorf("busy %d, first %v; second 1 is part warm-up", got.BusySeconds, got.FirstBusy)
	}
}

func TestGeneratorCPU_FirstBusyIsASecondFromTheStartOfTheRun(t *testing.T) {
	got := layCPU(t, cpuReport(7, 2, 8, 8), 1, perSecond(1, .3, .3, .5, .95, .95, .5, .5, .3))

	if got.BusySeconds != 2 {
		t.Errorf("busy seconds = %d, want 2 (seconds 3 and 4)", got.BusySeconds)
	}
	if got.FirstBusy == nil || *got.FirstBusy != 3 {
		t.Errorf("first busy second = %v, want 3, counted from the start with the warm-up", got.FirstBusy)
	}
	if got.Counted != 5 || !near(got.Share, 0.68) {
		t.Errorf("counted %d, share %v; want 5 and 0.68", got.Counted, got.Share)
	}
}

// A 60 s plan stopped at 10 s: the seconds counted are the ones that ran, not
// the ones planned.
func TestGeneratorCPU_AStoppedRunIsCountedToWhereItStopped(t *testing.T) {
	report := cpuReport(60, 2, 10, 10)
	report.Incomplete = true

	got := layCPU(t, report, 1, perSecond(1, .5, .5, .5, .5, .5, .5, .5, .5, .5, .5))

	if got.Counted != 8 {
		t.Errorf("counted seconds = %d, want 8 (seconds 2..9 of the 10 that ran)", got.Counted)
	}
	for i, s := range got.Seconds {
		if s == nil {
			t.Errorf("seconds[%d] = null in a run that covered it", i)
		}
	}
	if !near(got.Share, 0.50) {
		t.Errorf("share = %v, want 0.50", got.Share)
	}
}

// Readings that stop before the end of the grid leave the rest null, never 0.
func TestGeneratorCPU_ASecondNoReadingCoversIsNullNotZero(t *testing.T) {
	got := layCPU(t, cpuReport(7, 2, 10, 10), 1, perSecond(1, .3, .3, .5, .95, .95, .5, .5))

	wantCPUSeconds(t, got.Seconds, .3, .3, .5, .95, .95, .5, .5, cpuNull, cpuNull, cpuNull)
	if got.Counted != 5 || !near(got.Share, 0.68) {
		t.Errorf("counted %d, share %v; want 5 and 0.68, the tail does not touch them", got.Counted, got.Share)
	}
}

func TestGeneratorCPU_ItTakesTwoReadingsToMeasure(t *testing.T) {
	report := cpuReport(1, 0, 1, 1)

	if got := LayGeneratorCPU(report, nil, 1); got != nil {
		t.Errorf("no readings measured %+v, want nil", got)
	}
	if got := LayGeneratorCPU(report, []CPUSample{cpuAt(0, 0)}, 1); got != nil {
		t.Errorf("one reading measured %+v, want nil", got)
	}
	got := layCPU(t, report, 1, []CPUSample{cpuAt(0, 0), cpuAt(1, 0.5)})
	if !near(got.Share, 0.50) {
		t.Errorf("two readings: share = %v, want 0.50", got.Share)
	}
}

// The plan ends mid-second: its last piece is measured over its own wall
// time, and under half a second it joins the second before. A piece of 10 ms
// would read 0 or 1.56 on a clock that steps 15.6 ms.
//
// Ground: boundary — the half second.
func TestGeneratorCPU_ALastSecondUnderHalfASecondJoinsTheOneBefore(t *testing.T) {
	// 3.3 s plan: 0.5 CPU-s in each whole second, 0.3 in the last 0.3 s, which
	// alone would be a share of 1.0 and a busy second.
	got := layCPU(t, cpuReport(3.3, 0, 3.3, 4), 1,
		[]CPUSample{cpuAt(0, 0), cpuAt(1, 0.5), cpuAt(2, 1.0), cpuAt(3, 1.5), cpuAt(3.3, 1.8)})

	if got.Counted != 3 {
		t.Errorf("counted seconds = %d, want 3: the 0.3 s piece joins the third", got.Counted)
	}
	if got.BusySeconds != 0 {
		t.Errorf("busy seconds = %d, want 0: a piece of 0.3 s is no second", got.BusySeconds)
	}
	// Second 2 over 1.3 s of wall: (0.5 + 0.3) / 1.3 = 0.6154. The entry of the
	// piece itself is null: the value read and the value compared are one.
	wantCPUSeconds(t, got.Seconds, .5, .5, .62, cpuNull)
	if !near(got.Share, 0.55) {
		t.Errorf("share = %v, want 0.55 (1.8 CPU-s over 3.3 s)", got.Share)
	}
}

func TestGeneratorCPU_ALastSecondOfHalfASecondOrMoreStandsAlone(t *testing.T) {
	for _, tail := range []float64{0.5, 0.6} {
		got := layCPU(t, cpuReport(3+tail, 0, 3+tail, 4), 1,
			[]CPUSample{cpuAt(0, 0), cpuAt(1, 0.5), cpuAt(2, 1.0), cpuAt(3, 1.5), cpuAt(3+tail, 1.5+tail)})

		if got.Counted != 4 {
			t.Errorf("tail %v s: counted seconds = %d, want 4", tail, got.Counted)
		}
		wantCPUSeconds(t, got.Seconds, .5, .5, .5, 1.0)
		if got.BusySeconds != 1 || got.FirstBusy == nil || *got.FirstBusy != 3 {
			t.Errorf("tail %v s: busy %d, first %v; want the last second busy", tail, got.BusySeconds, got.FirstBusy)
		}
	}
}

// A run under a second has a share, but no second to call busy.
func TestGeneratorCPU_ARunUnderASecondHasAShareAndNoBusySeconds(t *testing.T) {
	got := layCPU(t, cpuReport(0.6, 0, 0.6, 1), 1, []CPUSample{cpuAt(0, 0), cpuAt(0.6, 0.6)})

	if !near(got.Share, 1.0) {
		t.Errorf("share = %v, want 1.00 from the whole 0.6 s", got.Share)
	}
	if got.BusySeconds != 0 || got.FirstBusy != nil {
		t.Errorf("busy %d, first %v; want none in a run under a second", got.BusySeconds, got.FirstBusy)
	}
	wantCPUSeconds(t, got.Seconds, 1.0)
}

func TestGeneratorCPU_ARunOfNoWallTimeNeverWritesNaNToJSON(t *testing.T) {
	report := cpuReport(0, 0, 0, 0)
	run := jsonRun(report)
	run.Run.GeneratorCPU = LayGeneratorCPU(report, []CPUSample{cpuAt(0, 0), cpuAt(0, 0)}, 1)

	// WriteJSON fails on a NaN or an Inf; the key is there, null or an object.
	out := writeJSON(t, run)
	field(t, out, "generator_cpu")
}

// Meter: the CLI goroutine that records the readings.

// cpuScript hands out the moments and the CPU times one call at a time; a
// read at index failAt fails (-1: none).
type cpuScript struct {
	times  []time.Duration
	cpus   []time.Duration
	failAt int32
	nowN   atomic.Int32
	readN  atomic.Int32
}

func (s *cpuScript) now() time.Time {
	i := int(s.nowN.Add(1)) - 1
	if i >= len(s.times) {
		i = len(s.times) - 1
	}

	return cpuStart.Add(s.times[i])
}

func (s *cpuScript) read() (time.Duration, error) {
	i := s.readN.Add(1) - 1
	if i == s.failAt {
		return 0, errCPUNotRead
	}
	if int(i) >= len(s.cpus) {
		i = int32(len(s.cpus) - 1)
	}

	return s.cpus[i], nil
}

func fiveReadings(failAt int32) *cpuScript {
	return &cpuScript{
		times:  []time.Duration{0, time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second},
		cpus:   []time.Duration{0, cpuSecs(.5), cpuSecs(1), cpuSecs(1.5), cpuSecs(2)},
		failAt: failAt,
	}
}

func TestCPUMeter_TicksBecomeTheMeasurement(t *testing.T) {
	s := fiveReadings(-1)
	m := NewCPUMeter(s.read, s.now, time.Hour)
	for range 5 {
		m.Tick()
	}

	got := m.Result(cpuReport(4, 0, 4, 4), 1)
	if got == nil {
		t.Fatal("Result = nil after five good readings")
	}
	wantCPUSeconds(t, got.Seconds, .5, .5, .5, .5)
	if !near(got.Share, 0.50) {
		t.Errorf("share = %v, want 0.50", got.Share)
	}
}

// A partial series would give a share over other seconds than the line says,
// so one failed read, first or last or between, leaves the whole null.
func TestCPUMeter_AFailedReadLeavesNoMeasurementAtAll(t *testing.T) {
	for _, failAt := range []int32{0, 2, 4} {
		s := fiveReadings(failAt)
		m := NewCPUMeter(s.read, s.now, time.Hour)
		for range 5 {
			m.Tick()
		}

		if got := m.Result(cpuReport(4, 0, 4, 4), 1); got != nil {
			t.Errorf("read %d failed: Result = %+v, want nil", failAt, got)
		}
	}
}

// Start takes a reading at once, Stop the last one: a run shorter than the
// tick still has its two.
func TestCPUMeter_StartTakesTheFirstReadingAndStopTheLast(t *testing.T) {
	s := &cpuScript{times: []time.Duration{0, time.Second}, cpus: []time.Duration{0, cpuSecs(.6)}, failAt: -1}
	m := NewCPUMeter(s.read, s.now, time.Hour)

	stop := m.Start()
	stop()

	if n := s.readN.Load(); n != 2 {
		t.Errorf("%d readings taken, want 2: one at Start, one at Stop", n)
	}
	got := m.Result(cpuReport(1, 0, 1, 1), 1)
	if got == nil {
		t.Fatal("Result = nil after Start and Stop")
	}
	if !near(got.Share, 0.60) {
		t.Errorf("share = %v, want 0.60", got.Share)
	}
}

// The CPU time of this process: burning a core for a while raises it.
func TestProcessCPU_RisesWithWork(t *testing.T) {
	before, err := ProcessCPU()
	if err != nil {
		t.Fatalf("ProcessCPU: %v", err)
	}
	for end := time.Now().Add(150 * time.Millisecond); time.Now().Before(end); {
	}
	after, err := ProcessCPU()
	if err != nil {
		t.Fatalf("ProcessCPU: %v", err)
	}

	// Half of the spin, for a clock that steps 15.6 ms.
	if after-before < 75*time.Millisecond {
		t.Errorf("CPU time rose by %v over 150 ms of spinning, want at least 75ms", after-before)
	}
}

// Available CPU, read as the Go runtime reads it (Go 1.27.1
// internal/runtime/cgroup/cgroup_linux.go and cgroup.go): the process's leaf
// cgroup from /proc/self/cgroup, its mount from /proc/self/mountinfo, v2
// cpu.max or v1 cpu.cfs_quota_us and cpu.cfs_period_us.

const (
	v2Mount = "29 22 0:26 %s /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime shared:4 - cgroup2 cgroup2 rw,nsdelegate\n"
	v1Mount = "31 22 0:27 %s /sys/fs/cgroup/cpu,cpuacct rw,nosuid,nodev,noexec,relatime shared:9 - cgroup cgroup rw,cpu,cpuacct\n"
)

func mountLine(format, root string) string {
	return strings.Replace(format, "%s", root, 1)
}

// Ground: signal go1.27.1 internal/runtime/cgroup — what the runtime finds
// as the quota is what we call available; a different path reads a different
// file, and the share is off by up to 2x.
func TestAvailableCores_ReadsTheQuotaTheWayTheGoRuntimeDoes(t *testing.T) {
	const gomax = 4

	for _, tc := range []struct {
		name  string
		gomax int
		files map[string]string
		want  float64
	}{
		{"v2 at the root", gomax, map[string]string{
			"proc/self/cgroup":      "0::/\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": "150000 100000\n",
		}, 1.5},
		{"v2 in a leaf, root says no limit", gomax, map[string]string{
			"proc/self/cgroup":                             "0::/system.slice/x.service\n",
			"proc/self/mountinfo":                          mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max":                        "max 100000\n",
			"sys/fs/cgroup/system.slice/x.service/cpu.max": "100000 100000\n",
		}, 1},
		{"v2 mounted at the leaf itself", gomax, map[string]string{
			"proc/self/cgroup":                             "0::/system.slice/x.service\n",
			"proc/self/mountinfo":                          mountLine(v2Mount, "/system.slice/x.service"),
			"sys/fs/cgroup/cpu.max":                        "200000 100000\n",
			"sys/fs/cgroup/system.slice/x.service/cpu.max": "max 100000\n",
		}, 2},
		{"v1 at the root", gomax, map[string]string{
			"proc/self/cgroup":                            "12:memory:/\n11:cpu,cpuacct:/\n",
			"proc/self/mountinfo":                         mountLine(v1Mount, "/"),
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us":  "200000\n",
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us": "100000\n",
		}, 2},
		{"v1 in a leaf, root says no limit", gomax, map[string]string{
			"proc/self/cgroup":                                       "11:cpu,cpuacct:/docker/abc\n",
			"proc/self/mountinfo":                                    mountLine(v1Mount, "/"),
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us":             "-1\n",
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us":            "100000\n",
			"sys/fs/cgroup/cpu,cpuacct/docker/abc/cpu.cfs_quota_us":  "150000\n",
			"sys/fs/cgroup/cpu,cpuacct/docker/abc/cpu.cfs_period_us": "100000\n",
		}, 1.5},
		{"a quota under one core", gomax, map[string]string{
			"proc/self/cgroup":      "0::/\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": "50000 100000\n",
		}, 0.5},
		{"quota above GOMAXPROCS", gomax, map[string]string{
			"proc/self/cgroup":      "0::/\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": "800000 100000\n",
		}, gomax},
		{"GOMAXPROCS under the quota", 1, map[string]string{
			"proc/self/cgroup":      "0::/\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": "200000 100000\n",
		}, 1},
		{"v2 max", gomax, map[string]string{
			"proc/self/cgroup":      "0::/\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": "max 100000\n",
		}, gomax},
		{"v1 minus one", gomax, map[string]string{
			"proc/self/cgroup":                            "11:cpu,cpuacct:/\n",
			"proc/self/mountinfo":                         mountLine(v1Mount, "/"),
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us":  "-1\n",
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us": "100000\n",
		}, gomax},
		{"v2 garbage", gomax, map[string]string{
			"proc/self/cgroup":      "0::/\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": "lots of cpu\n",
		}, gomax},
		{"v2 zero quota", gomax, map[string]string{
			"proc/self/cgroup":      "0::/\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": "0 100000\n",
		}, gomax},
		{"v2 zero period", gomax, map[string]string{
			"proc/self/cgroup":      "0::/\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": "100000 0\n",
		}, gomax},
		{"v2 negative quota", gomax, map[string]string{
			"proc/self/cgroup":      "0::/\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": "-100000 100000\n",
		}, gomax},
		{"v1 zero quota", gomax, map[string]string{
			"proc/self/cgroup":                            "11:cpu,cpuacct:/\n",
			"proc/self/mountinfo":                         mountLine(v1Mount, "/"),
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us":  "0\n",
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us": "100000\n",
		}, gomax},
		{"v1 zero period", gomax, map[string]string{
			"proc/self/cgroup":                            "11:cpu,cpuacct:/\n",
			"proc/self/mountinfo":                         mountLine(v1Mount, "/"),
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us":  "100000\n",
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us": "0\n",
		}, gomax},
		{"v1 garbage", gomax, map[string]string{
			"proc/self/cgroup":                            "11:cpu,cpuacct:/\n",
			"proc/self/mountinfo":                         mountLine(v1Mount, "/"),
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us":  "plenty\n",
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us": "100000\n",
		}, gomax},
		{"cgroup file is garbage", gomax, map[string]string{
			"proc/self/cgroup":      "nonsense\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": "100000 100000\n",
		}, gomax},
		{"no cpu.max file", gomax, map[string]string{
			"proc/self/cgroup":    "0::/\n",
			"proc/self/mountinfo": mountLine(v2Mount, "/"),
		}, gomax},
		{"no cgroup files at all", gomax, map[string]string{}, gomax},
	} {
		root := fstest.MapFS{}
		for name, body := range tc.files {
			root[name] = &fstest.MapFile{Data: []byte(body)}
		}

		if got := AvailableCores(root, tc.gomax); got != tc.want {
			t.Errorf("%s: available = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAvailableCPU_IsAtLeastSomethingAndNeverMoreThanGOMAXPROCS(t *testing.T) {
	got := AvailableCPU()

	if got <= 0 {
		t.Errorf("available CPU = %v, want above 0", got)
	}
	if procs := float64(runtime.GOMAXPROCS(0)); got > procs {
		t.Errorf("available CPU = %v, want at most GOMAXPROCS %v", got, procs)
	}
}
