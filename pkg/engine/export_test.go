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

package engine

import (
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/metrics"
)

// exportCeiling is how long a test waits for what it started: a bound against a
// hang, not a pace.
const exportCeiling = 20 * time.Second

var exportBounds = []time.Duration{time.Millisecond, 10 * time.Millisecond, 100 * time.Millisecond, time.Second}

// exportOrder is the eleven categories in the order of the contract, which is
// the order of ExportMethod.Calls.
var exportOrder = [ExportCategories]Category{
	ExportSuccess:      CategorySuccess,
	ExportRequestError: CategoryClientFault,
	ExportOverload:     CategoryOverload,
	ExportFailure:      CategoryServerFault,
	ExportTimedOut:     CategoryTimeout,
	ExportCutOff:       CategoryCutOff,
	ExportUnreachable:  CategoryUnreachable,
	ExportClientError:  CategoryClientError,
	ExportBadResponse:  CategoryBadResponse,
	ExportAborted:      CategoryAborted,
	ExportUnclassified: CategoryUnknown,
}

// exportCall is a call of method scheduled at start+at that took latency and
// ended in category, with a deadline a second after its schedule.
func exportCall(start time.Time, at time.Duration, method string, category Category, latency time.Duration) Result {
	scheduled := start.Add(at)

	return Result{
		Method:      method,
		ScheduledAt: scheduled,
		BegunAt:     scheduled,
		Deadline:    scheduled.Add(time.Second),
		Outcome:     Outcome{SentAt: scheduled, DoneAt: scheduled.Add(latency), Category: category},
	}
}

// exportUnsent is a call that timed out before it went out, held back by blocker.
func exportUnsent(start time.Time, at time.Duration, method string, blocker Blocker) Result {
	r := exportCall(start, at, method, CategoryTimeout, time.Second)
	r.NotSent = true
	r.NotSentOn = blocker

	return r
}

func exportOf(stats *Stats, calls ...Call) ExportView {
	var view ExportView
	stats.ExportInto(&view, metrics.NewBuffer(), exportBounds, calls)

	return view
}

// onlyMethod is the one method the view must hold.
func onlyMethod(t *testing.T, view ExportView) ExportMethod {
	t.Helper()

	if len(view.Methods) != 1 {
		t.Fatalf("the view holds %d methods, want 1", len(view.Methods))
	}

	return view.Methods[0]
}

func startedStats(warmup time.Duration, methods ...string) (*Stats, time.Time) {
	stats := NewStats()
	start := time.Now()
	stats.Reserve(time.Minute, methods...)
	stats.Start(start, warmup)

	return stats, start
}

// reportCalls is the Report's numbers for one method in the order of Calls.
// The Report's Latencies hold the censored observations too, so a success is
// what is left of them.
func reportCalls(m MethodReport) [ExportCategories]int64 {
	return [ExportCategories]int64{
		ExportSuccess:      int64(m.Latencies - m.Censored),
		ExportRequestError: int64(m.Rejected.Count),
		ExportOverload:     int64(m.Overload.Count),
		ExportFailure:      int64(m.Failure.Count),
		ExportTimedOut:     int64(m.TimedOut),
		ExportCutOff:       int64(m.CutOff),
		ExportUnreachable:  int64(m.Unanswered),
		ExportClientError:  int64(m.ClientError),
		ExportBadResponse:  int64(m.BadResponse.Count),
		ExportAborted:      int64(m.Aborted),
		ExportUnclassified: int64(m.Unclassified),
	}
}

// Ground: contract — the eleven category names and their order (CLAUDE.md,
// categories): every category is counted under its own index. The counts differ
// (1..11), so two categories swapped are told apart.
func TestStatsExportInto_EachCategoryIsCountedUnderItsOwnIndex(t *testing.T) {
	stats, start := startedStats(0, "a")

	var want [ExportCategories]int64
	for i, category := range exportOrder {
		want[i] = int64(i + 1)
		for range i + 1 {
			stats.Record(exportCall(start, 0, "a", category, 5*time.Millisecond))
		}
	}

	if got := onlyMethod(t, exportOf(stats)).Calls; got != want {
		t.Errorf("calls by category = %v, want %v", got, want)
	}
}

// Ground: contract — the exporter shows what the report shows: sources of truth
// for report numbers (WORKFLOW §5). A mixed run over two methods: each method's
// eleven numbers equal the Report's.
func TestStatsExportInto_AgreesWithTheReportPerMethod(t *testing.T) {
	stats, start := startedStats(0, "a", "b")

	for i := range 330 {
		method := "b"
		if i%3 == 0 {
			method = "a"
		}
		stats.Record(exportCall(start, 0, method, exportOrder[i%ExportCategories], time.Duration(i%40+1)*time.Millisecond))
	}
	stats.Finish(start.Add(time.Second))

	view := exportOf(stats)
	report := stats.Report()
	if len(view.Methods) != len(report.Methods) || len(report.Methods) != 2 {
		t.Fatalf("the view holds %d methods, the report %d, want 2 each", len(view.Methods), len(report.Methods))
	}

	for i, m := range report.Methods {
		t.Run(m.Method, func(t *testing.T) {
			if got, want := view.Methods[i].Calls, reportCalls(m); got != want {
				t.Errorf("calls by category = %v, the report has %v", got, want)
			}
		})
	}
}

// Ground: contract — success is the measured calls only: a timeout or an abort
// is censored in the same distribution, but it is not a success.
func TestStatsExportInto_SuccessCountsOnlyMeasuredCalls(t *testing.T) {
	stats, start := startedStats(0, "a")

	for range 3 {
		stats.Record(exportCall(start, 0, "a", CategorySuccess, 5*time.Millisecond))
	}
	for range 4 {
		stats.Record(exportCall(start, 0, "a", CategoryTimeout, time.Second))
	}
	for range 2 {
		stats.Record(exportCall(start, 0, "a", CategoryAborted, 300*time.Millisecond))
	}

	if got := onlyMethod(t, exportOf(stats)).Calls[ExportSuccess]; got != 3 {
		t.Errorf("success = %d, want 3: the timeouts and aborts are censored, not measured", got)
	}
}

// Ground: contract — one copy of the distribution gives both numbers, so
// +Inf == calls_total{success} in any scrape.
func TestStatsExportInto_TheHistogramCountIsTheSuccessCount(t *testing.T) {
	stats, start := startedStats(0, "a")

	for i := range 7 {
		stats.Record(exportCall(start, 0, "a", CategorySuccess, time.Duration(i+1)*time.Millisecond))
	}
	stats.Record(exportCall(start, 0, "a", CategoryTimeout, time.Second))

	m := onlyMethod(t, exportOf(stats))
	if m.Count != 7 || m.Calls[ExportSuccess] != m.Count {
		t.Errorf("histogram count %d, success %d; want 7 and the same", m.Count, m.Calls[ExportSuccess])
	}
}

// Ground: contract — latency_seconds is successes only (docs/decisions.md,
// categories): a call that was censored, refused, cut off or never reached the
// target has no latency of its own to show.
func TestStatsExportInto_OnlySuccessesAreInTheHistogram(t *testing.T) {
	stats, start := startedStats(0, "a")

	for _, category := range exportOrder {
		if category == CategorySuccess {
			continue
		}
		for range 5 {
			stats.Record(exportCall(start, 0, "a", category, 5*time.Millisecond))
		}
	}

	m := onlyMethod(t, exportOf(stats))
	if m.Count != 0 || m.Sum != 0 || !slices.Equal(m.Cumulative, make([]int64, len(exportBounds))) {
		t.Errorf("histogram of a run without successes: count %d, sum %v, cumulative %v; want all zero", m.Count, m.Sum, m.Cumulative)
	}
}

// Ground: contract — buckets are cumulative counts of successes at or under
// each bound; the bound list comes from the caller (the exporter's edges).
func TestStatsExportInto_CumulativeFollowsTheBounds(t *testing.T) {
	stats, start := startedStats(0, "a")

	for _, latency := range []time.Duration{500 * time.Microsecond, 5 * time.Millisecond, 5 * time.Millisecond, 50 * time.Millisecond, 500 * time.Millisecond, 5 * time.Second} {
		stats.Record(exportCall(start, 0, "a", CategorySuccess, latency))
	}

	m := onlyMethod(t, exportOf(stats))
	if want := []int64{1, 3, 4, 5}; !slices.Equal(m.Cumulative, want) || m.Count != 6 {
		t.Errorf("cumulative %v, count %d; want %v and 6: the 5s success is only in the total", m.Cumulative, m.Count, want)
	}
}

// Ground: review verdict V2 — _sum comes from the HDR buckets within 0.1%.
func TestStatsExportInto_SumIsTheSumOfTheLatencies(t *testing.T) {
	stats, start := startedStats(0, "a")

	for _, latency := range []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond} {
		stats.Record(exportCall(start, 0, "a", CategorySuccess, latency))
	}

	const want = 60 * time.Millisecond
	if got := onlyMethod(t, exportOf(stats)).Sum; got < want-want/1000 || got > want+want/1000 {
		t.Errorf("sum = %v, want %v within 0.1%%", got, want)
	}
}

// Ground: edge — methods do not mix: each has its own distribution.
func TestStatsExportInto_EachMethodHasItsOwnHistogram(t *testing.T) {
	stats, start := startedStats(0, "a", "b")

	stats.Record(exportCall(start, 0, "a", CategorySuccess, 5*time.Millisecond))
	for range 3 {
		stats.Record(exportCall(start, 0, "b", CategorySuccess, 50*time.Millisecond))
	}

	view := exportOf(stats)
	if len(view.Methods) != 2 {
		t.Fatalf("the view holds %d methods, want 2", len(view.Methods))
	}
	if a, b := view.Methods[0], view.Methods[1]; a.Count != 1 || b.Count != 3 ||
		!slices.Equal(a.Cumulative, []int64{0, 1, 1, 1}) || !slices.Equal(b.Cumulative, []int64{0, 0, 3, 3}) {
		t.Errorf("a: count %d %v; b: count %d %v; want 1 [0 1 1 1] and 3 [0 0 3 3]", a.Count, a.Cumulative, b.Count, b.Cumulative)
	}
}

// Ground: contract — a call that never left the generator is counted by the
// client-side cause that kept it, in the order generator, connection, stream; a
// call of an unknown cause (a defect of the sender) is in none of them.
func TestStatsExportInto_NotSentCallsAreCountedByCause(t *testing.T) {
	stats, start := startedStats(0, "a")

	for range 2 {
		stats.Record(exportUnsent(start, 0, "a", BlockedOnGenerator))
	}
	for range 3 {
		stats.Record(exportUnsent(start, 0, "a", BlockedOnConnection))
	}
	for range 4 {
		stats.Record(exportUnsent(start, 0, "a", BlockedOnStream))
	}
	for range 5 {
		stats.Record(exportUnsent(start, 0, "a", BlockedUnknown))
	}

	if got, want := onlyMethod(t, exportOf(stats)).NotSent, [ExportCauses]int64{2, 3, 4}; got != want {
		t.Errorf("not sent by cause (generator, connection, stream) = %v, want %v", got, want)
	}
}

// Ground: contract — a call that never went out is not a timed_out call: that
// category is for calls that went out and ran into their deadline (Stats.Record).
func TestStatsExportInto_NotSentCallsAreNotTimedOut(t *testing.T) {
	stats, start := startedStats(0, "a")

	for range 2 {
		stats.Record(exportCall(start, 0, "a", CategoryTimeout, time.Second))
	}
	for range 3 {
		stats.Record(exportUnsent(start, 0, "a", BlockedOnConnection))
	}

	if got := onlyMethod(t, exportOf(stats)).Calls[ExportTimedOut]; got != 2 {
		t.Errorf("timed_out = %d, want 2: the three unsent calls are not among them", got)
	}
}

// Ground: contract (#88) — warm-up is excluded from every counter and from the
// histogram; the three categories here would each show it.
func TestStatsExportInto_WarmupCallsAreInNoCategory(t *testing.T) {
	stats, start := startedStats(time.Second, "a")

	for _, category := range []Category{CategorySuccess, CategoryOverload, CategoryTimeout} {
		for range 5 {
			stats.Record(exportCall(start, 100*time.Millisecond, "a", category, 5*time.Millisecond))
		}
	}
	for range 3 {
		stats.Record(exportCall(start, 2*time.Second, "a", CategorySuccess, 5*time.Millisecond))
	}

	var want [ExportCategories]int64
	want[ExportSuccess] = 3
	if got := onlyMethod(t, exportOf(stats)).Calls; got != want {
		t.Errorf("calls by category = %v, want %v: only the three after the warm-up", got, want)
	}
}

// Ground: contract (#88) — the warm-up's histogram is empty too.
func TestStatsExportInto_WarmupCallsAreNotInTheHistogram(t *testing.T) {
	stats, start := startedStats(time.Second, "a")

	for range 5 {
		stats.Record(exportCall(start, 100*time.Millisecond, "a", CategorySuccess, 5*time.Millisecond))
	}
	for range 3 {
		stats.Record(exportCall(start, 2*time.Second, "a", CategorySuccess, 5*time.Millisecond))
	}

	m := onlyMethod(t, exportOf(stats))
	if m.Count != 3 || !slices.Equal(m.Cumulative, []int64{0, 3, 3, 3}) {
		t.Errorf("histogram: count %d, cumulative %v; want 3 and [0 3 3 3]", m.Count, m.Cumulative)
	}
}

// Ground: contract — warmup_sent_total is Stats.warmupSent's rule: the calls
// that went out, whatever came back (the report's WarmupSent); an unsent one
// did not go out.
func TestStatsExportInto_WarmupSentCountsTheCallsThatWentOut(t *testing.T) {
	stats, start := startedStats(time.Second, "a")

	for range 5 {
		stats.Record(exportCall(start, 100*time.Millisecond, "a", CategorySuccess, 5*time.Millisecond))
	}
	for range 2 {
		stats.Record(exportCall(start, 100*time.Millisecond, "a", CategoryOverload, 5*time.Millisecond))
	}
	for range 4 {
		stats.Record(exportUnsent(start, 100*time.Millisecond, "a", BlockedOnConnection))
	}

	if got := onlyMethod(t, exportOf(stats)).WarmupSent; got != 7 {
		t.Errorf("warmup sent = %d, want 7: five successes and two overloads went out, the four unsent did not", got)
	}
}

// Ground: contract (#88) — an unsent call of the warm-up is no more in the
// causes than a sent one is in the categories.
func TestStatsExportInto_WarmupNotSentCallsAreInNoCause(t *testing.T) {
	stats, start := startedStats(time.Second, "a")

	for _, blocker := range []Blocker{BlockedOnGenerator, BlockedOnConnection, BlockedOnStream} {
		stats.Record(exportUnsent(start, 100*time.Millisecond, "a", blocker))
	}
	stats.Record(exportCall(start, 2*time.Second, "a", CategorySuccess, 5*time.Millisecond))

	if got := onlyMethod(t, exportOf(stats)).NotSent; got != [ExportCauses]int64{} {
		t.Errorf("not sent by cause = %v, want all zero: the three were in the warm-up", got)
	}
}

// Ground: contract — methods come by name, the order Report and the live view
// use, so two scrapes list them alike.
func TestStatsExportInto_MethodsComeByName(t *testing.T) {
	stats, start := startedStats(0)

	for _, name := range []string{"pkg.Svc/Put", "pkg.Svc/Get", "pkg.Svc/Delete"} {
		stats.Record(exportCall(start, 0, name, CategorySuccess, time.Millisecond))
	}

	var got []string
	for _, m := range exportOf(stats).Methods {
		got = append(got, m.Name)
	}
	if want := []string{"pkg.Svc/Delete", "pkg.Svc/Get", "pkg.Svc/Put"}; !slices.Equal(got, want) {
		t.Errorf("methods = %v, want %v", got, want)
	}
}

// Ground: edge — empty. A method of the plan with no call yet is in the view
// with zeros, so its series exist from the first scrape on (a rate() over a
// series that appears late loses its first step).
func TestStatsExportInto_AMethodWithoutCallsIsThereWithZeros(t *testing.T) {
	stats, _ := startedStats(0, "a", "b")

	view := exportOf(stats)
	if len(view.Methods) != 2 {
		t.Fatalf("the view holds %d methods, want the two reserved", len(view.Methods))
	}
	for _, m := range view.Methods {
		if m.Calls != [ExportCategories]int64{} || m.Count != 0 || !slices.Equal(m.Cumulative, make([]int64, len(exportBounds))) {
			t.Errorf("%s: calls %v, count %d, cumulative %v; want all zero, one cumulative per bound", m.Name, m.Calls, m.Count, m.Cumulative)
		}
	}
}

// Ground: contract — a scrape before the run began: Started is false (the
// exporter renders the HELP and TYPE lines only). Green on the stub, which
// leaves the view empty: it holds the line for the code.
func TestStatsExportInto_BeforeStartTheViewIsNotStarted(t *testing.T) {
	stats := NewStats()
	stats.Reserve(time.Minute, "a")

	if exportOf(stats).Started {
		t.Error("Started = true for a Stats that was never started")
	}
}

// Ground: contract — Start flips it.
func TestStatsExportInto_AfterStartTheViewIsStarted(t *testing.T) {
	stats, _ := startedStats(0, "a")

	if !exportOf(stats).Started {
		t.Error("Started = false for a started Stats")
	}
}

// Ground: contract — the warmup gauge is 1 while the run is in its warm-up.
func TestStatsExportInto_InsideTheWarmupTheViewSaysSo(t *testing.T) {
	stats, _ := startedStats(time.Hour, "a")

	view := exportOf(stats)
	if !view.Started {
		t.Fatal("not Started")
	}
	if !view.Warmup {
		t.Error("Warmup = false inside an hour of warm-up that began a moment ago")
	}
}

// Ground: edge — the boundary: the clock is past the warm-up.
func TestStatsExportInto_AfterTheWarmupTheViewSaysSo(t *testing.T) {
	stats := NewStats()
	stats.Reserve(time.Minute, "a")
	stats.Start(time.Now().Add(-2*time.Second), time.Second)

	view := exportOf(stats)
	if !view.Started {
		t.Fatal("not Started")
	}
	if view.Warmup {
		t.Error("Warmup = true two seconds into a one-second warm-up")
	}
}

// Ground: edge — a run without warm-up is never in it.
func TestStatsExportInto_WithoutAWarmupTheViewIsNeverInIt(t *testing.T) {
	stats, _ := startedStats(0, "a")

	view := exportOf(stats)
	if !view.Started {
		t.Fatal("not Started")
	}
	if view.Warmup {
		t.Error("Warmup = true for a run without warm-up")
	}
}

// Ground: edge — impossible-looking but real: a run aborted inside its warm-up
// is over, and the gauge does not stay 1 for the linger.
func TestStatsExportInto_ARunThatEndedInsideTheWarmupIsNotInIt(t *testing.T) {
	stats, start := startedStats(10*time.Second, "a")
	stats.Finish(start.Add(time.Second))

	view := exportOf(stats)
	if !view.Started {
		t.Fatal("not Started")
	}
	if view.Warmup {
		t.Error("Warmup = true after the run ended")
	}
}

// Ground: review verdict V1 — planned_rps of each method is the rate of its own
// stages now.
func TestStatsExportInto_PlannedRPSIsTheRateOfTheMethodsStagesNow(t *testing.T) {
	stats, _ := startedStats(0, "a", "b")
	calls := []Call{
		{Method: "a", Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Hour}}},
		{Method: "b", Stages: []Stage{{StartRPS: 7, TargetRPS: 7, Duration: time.Hour}}},
	}

	view := exportOf(stats, calls...)
	if len(view.Methods) != 2 {
		t.Fatalf("the view holds %d methods, want 2", len(view.Methods))
	}
	if a, b := view.Methods[0].PlannedRPS, view.Methods[1].PlannedRPS; a != 100 || b != 7 {
		t.Errorf("planned rps a=%v b=%v, want 100 and 7", a, b)
	}
}

// Ground: review verdict V1 — the clock of planned_rps is the run's own
// (elapsed since Start), so a ramp five seconds in offers half its target.
func TestStatsExportInto_PlannedRPSFollowsTheRampWithTheElapsedTime(t *testing.T) {
	stats := NewStats()
	stats.Reserve(time.Minute, "a")
	stats.Start(time.Now().Add(-5*time.Second), 0)

	m := onlyMethod(t, exportOf(stats, Call{Method: "a", Stages: []Stage{{StartRPS: 0, TargetRPS: 100, Duration: 10 * time.Second}}}))
	if m.PlannedRPS < 50 || m.PlannedRPS > 60 {
		t.Errorf("planned rps five seconds into a 0→100 ramp of ten = %v, want 50 (a moment more is fine, up to 60)", m.PlannedRPS)
	}
}

// Ground: review verdict V1 — "0 after the plan ends", also when the run ended:
// a Finish is the end, and the clock stops there.
func TestStatsExportInto_PlannedRPSIsZeroOnceTheRunEnded(t *testing.T) {
	stats, start := startedStats(0, "a")
	stats.Finish(start.Add(500 * time.Millisecond))

	m := onlyMethod(t, exportOf(stats, Call{Method: "a", Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Hour}}}))
	if m.PlannedRPS != 0 {
		t.Errorf("planned rps of a run that has ended = %v, want 0: nothing is offered any more", m.PlannedRPS)
	}
}

// Ground: edge — a Stop ends the sending early (EndSending): the plan was cut,
// nothing is offered while the calls in flight finish, so the gauge says 0 and
// does not go on quoting the plan's hour.
func TestStatsExportInto_PlannedRPSIsZeroOnceSendingEnded(t *testing.T) {
	stats := NewStats()
	stats.Reserve(time.Minute, "a")
	start := time.Now().Add(-5 * time.Second)
	stats.Start(start, 0)
	stats.EndSending(start.Add(time.Second))

	m := onlyMethod(t, exportOf(stats, Call{Method: "a", Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Hour}}}))
	if m.PlannedRPS != 0 {
		t.Errorf("planned rps four seconds after the sending ended = %v, want 0", m.PlannedRPS)
	}
}

// Ground: edge — a method the plan does not know (a Stats alone, or a call
// recorded under another name) has no plan: 0, not a panic.
func TestStatsExportInto_AMethodOutsideThePlanHasNoPlannedRPS(t *testing.T) {
	stats, start := startedStats(0)
	stats.Record(exportCall(start, 0, "x", CategorySuccess, time.Millisecond))

	m := onlyMethod(t, exportOf(stats, Call{Method: "a", Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Hour}}}))
	if m.PlannedRPS != 0 {
		t.Errorf("planned rps of a method outside the plan = %v, want 0", m.PlannedRPS)
	}
}

// hammered records from four goroutines while two read the view all the time,
// and returns how many views were not one consistent copy, and the last view.
func hammered(t *testing.T) (torn int64, final ExportMethod) {
	t.Helper()

	stats, start := startedStats(0, "a")

	const writers, each = 4, 500

	var (
		wg      sync.WaitGroup
		readers sync.WaitGroup
		bad     atomic.Int64
	)
	for range writers {
		wg.Go(func() {
			for range each {
				stats.Record(exportCall(start, 0, "a", CategorySuccess, 5*time.Millisecond))
			}
		})
	}

	stop := make(chan struct{})
	for range 2 {
		readers.Go(func() {
			var view ExportView
			buf := metrics.NewBuffer()

			for {
				select {
				case <-stop:
					return
				default:
				}

				stats.ExportInto(&view, buf, exportBounds, nil)
				if len(view.Methods) != 1 {
					bad.Add(1)

					continue
				}
				// One copy of the distribution: the three numbers are one.
				if m := view.Methods[0]; m.Calls[ExportSuccess] != m.Count || m.Cumulative[len(m.Cumulative)-1] > m.Count {
					bad.Add(1)
				}
			}
		})
	}

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(stop)
		readers.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(exportCeiling):
		t.Fatalf("the writers and readers have not finished in %v", exportCeiling)
	}

	return bad.Load(), onlyMethod(t, exportOf(stats))
}

// Ground: concurrency (WORKFLOW §3) — a scrape next to the recording sees one
// copy: success, the histogram's count and its buckets are consistent in every
// view. Meaningful under -race too.
func TestStatsExportInto_NextToRecordEveryViewIsOneCopy(t *testing.T) {
	if torn, _ := hammered(t); torn != 0 {
		t.Errorf("%d views were torn or empty, want none", torn)
	}
}

// Ground: concurrency — and at the end nothing is lost: 2000 successes in, 2000
// out, in the counter and in the histogram.
func TestStatsExportInto_NextToRecordTheEndIsExact(t *testing.T) {
	_, final := hammered(t)
	if final.Calls[ExportSuccess] != 2000 || final.Count != 2000 {
		t.Errorf("success %d, histogram count %d, want 2000 each", final.Calls[ExportSuccess], final.Count)
	}
}

// Ground: concurrency — the elapsed time is read under the lock the end of the
// run is written under (killed by -race: a mutant that read it outside the lock).
func TestStatsExportInto_NextToTheEndOfTheRunTheReadIsLocked(t *testing.T) {
	stats, _ := startedStats(0, "a")

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 2 {
		readers.Go(func() {
			var view ExportView
			buf := metrics.NewBuffer()
			calls := []Call{{Method: "a", Stages: []Stage{{Duration: time.Minute, StartRPS: 10, TargetRPS: 10}}}}

			for {
				select {
				case <-stop:
					return
				default:
				}

				stats.ExportInto(&view, buf, exportBounds, calls)
			}
		})
	}

	for range 200 {
		stats.EndSending(time.Now())
		stats.Finish(time.Now())
		time.Sleep(50 * time.Microsecond)
	}
	close(stop)
	readers.Wait()
}

// Ground: contract of the name — "Into": a repeated call refills the same view
// in place, the methods and each method's buckets keep their backing arrays, so
// a scrape every second does not hand the garbage collector a new view each time.
// Pinned by identity, not by an allocation count: the library's own reading of a
// histogram allocates, and that is not this test's business.
func TestStatsExportInto_RefillsTheViewInPlace(t *testing.T) {
	stats, start := startedStats(0, "a", "b")
	stats.Record(exportCall(start, 0, "a", CategorySuccess, 5*time.Millisecond))

	var view ExportView
	buf := metrics.NewBuffer()
	stats.ExportInto(&view, buf, exportBounds, nil)
	if len(view.Methods) != 2 {
		t.Fatalf("the view holds %d methods, want 2", len(view.Methods))
	}
	if len(view.Methods[0].Cumulative) != len(exportBounds) {
		t.Fatalf("the first method holds %d buckets, want %d", len(view.Methods[0].Cumulative), len(exportBounds))
	}
	methods, buckets := &view.Methods[0], &view.Methods[0].Cumulative[0]

	stats.Record(exportCall(start, 0, "a", CategorySuccess, 5*time.Millisecond))
	stats.ExportInto(&view, buf, exportBounds, nil)

	if &view.Methods[0] != methods || &view.Methods[0].Cumulative[0] != buckets {
		t.Error("a second ExportInto built a new methods slice or new buckets, want the first ones refilled")
	}
	if view.Methods[0].Count != 2 {
		t.Errorf("the refilled view counts %d successes, want 2", view.Methods[0].Count)
	}
}

// Ground: contract — looking is not touching: a scrape storm over a finished
// run leaves the report as it was (the exporter takes copies, it changes no
// counter). Green on the stub, which reads nothing: it holds the line for the code.
func TestStatsExportInto_DoesNotChangeTheReport(t *testing.T) {
	stats, start := startedStats(0, "a", "b")
	for i := range 200 {
		stats.Record(exportCall(start, 0, []string{"a", "b"}[i%2], exportOrder[i%ExportCategories], time.Duration(i%30+1)*time.Millisecond))
	}
	stats.Finish(start.Add(time.Second))

	before := stats.Report()

	var view ExportView
	buf := metrics.NewBuffer()
	for range 100 {
		stats.ExportInto(&view, buf, exportBounds, nil)
	}

	if after := stats.Report(); !reflect.DeepEqual(before, after) {
		t.Error("the report changed after 100 views")
	}
}

func engineOf(t *testing.T, calls ...Call) *Engine {
	t.Helper()

	eng, err := New(Options{Calls: calls, Sender: FakeSender{Delay: time.Millisecond}, MaxInFlight: 1000})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return eng
}

// Ground: review verdict V1 — the engine passes its own calls as the plan:
// planned_rps comes from the config's stages, per method.
func TestEngineExportInto_PlannedRPSComesFromTheEnginesCalls(t *testing.T) {
	eng := engineOf(t,
		Call{Method: "a", Timeout: time.Second, Stages: []Stage{{StartRPS: 100, TargetRPS: 100, Duration: time.Hour}}},
		Call{Method: "b", Timeout: time.Second, Stages: []Stage{{StartRPS: 7, TargetRPS: 7, Duration: time.Hour}}},
	)
	eng.stats.Reserve(time.Minute, "a", "b")
	eng.stats.Start(time.Now(), 0)

	var view ExportView
	eng.ExportInto(&view, metrics.NewBuffer(), exportBounds)
	if len(view.Methods) != 2 {
		t.Fatalf("the view holds %d methods, want 2", len(view.Methods))
	}
	if a, b := view.Methods[0].PlannedRPS, view.Methods[1].PlannedRPS; a != 100 || b != 7 {
		t.Errorf("planned rps a=%v b=%v, want 100 and 7", a, b)
	}
}

// Ground: contract — in_flight is the worker pool's count, the same number the
// live view shows.
func TestEngineExportInto_InFlightIsTheWorkersCount(t *testing.T) {
	eng := engineOf(t, Call{Method: "a", Timeout: time.Second, Stages: []Stage{{StartRPS: 10, TargetRPS: 10, Duration: time.Hour}}})
	eng.stats.Reserve(time.Minute, "a")
	eng.stats.Start(time.Now(), 0)
	eng.pool.inFlight.Add(3)

	var view ExportView
	eng.ExportInto(&view, metrics.NewBuffer(), exportBounds)
	if view.InFlight != 3 {
		t.Errorf("in flight = %d, want 3", view.InFlight)
	}
}

// Ground: contract — a scrape before Run: the engine is built, the clock has
// not started. Green on the stub, which leaves the view empty: it holds the
// line for the code (Started stays false, no method is listed).
func TestEngineExportInto_BeforeRunTheViewIsNotStarted(t *testing.T) {
	eng := engineOf(t, Call{Method: "a", Timeout: time.Second, Stages: []Stage{{StartRPS: 10, TargetRPS: 10, Duration: time.Hour}}})

	var view ExportView
	eng.ExportInto(&view, metrics.NewBuffer(), exportBounds)
	if view.Started {
		t.Error("Started = true before Run")
	}
}

// Ground: concurrency + contract — a real run read all the way through: the
// view is taken from another goroutine every millisecond while Run goes (the
// race detector sees any read of Run's own fields), and once Run is over the
// last view equals the report, with the plan over (planned_rps 0).
func TestEngineExportInto_NextToRunTheLastViewIsTheReport(t *testing.T) {
	eng := engineOf(t, Call{Method: "m", Timeout: time.Second, Stages: []Stage{{StartRPS: 200, TargetRPS: 200, Duration: 300 * time.Millisecond}}})

	done := make(chan error, 1)
	go func() { done <- eng.Run(t.Context()) }()

	var view ExportView
	buf := metrics.NewBuffer()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()

	ceiling := time.After(exportCeiling)
	for running := true; running; {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			running = false
		case <-tick.C:
			eng.ExportInto(&view, buf, exportBounds)
		case <-ceiling:
			t.Fatalf("Run has not returned in %v", exportCeiling)
		}
	}

	eng.ExportInto(&view, buf, exportBounds)
	report := eng.Report()
	if len(view.Methods) != 1 || len(report.Methods) != 1 {
		t.Fatalf("the view holds %d methods, the report %d, want 1 each", len(view.Methods), len(report.Methods))
	}
	if report.Methods[0].Latencies == 0 {
		t.Fatal("the run measured nothing")
	}

	if got, want := view.Methods[0].Calls, reportCalls(report.Methods[0]); got != want {
		t.Errorf("calls by category = %v, the report has %v", got, want)
	}
}

// Ground: contract — and the plan is over in that last view.
func TestEngineExportInto_AfterRunPlannedRPSIsZero(t *testing.T) {
	eng := engineOf(t, Call{Method: "m", Timeout: time.Second, Stages: []Stage{{StartRPS: 200, TargetRPS: 200, Duration: 200 * time.Millisecond}}})
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var view ExportView
	eng.ExportInto(&view, metrics.NewBuffer(), exportBounds)
	m := onlyMethod(t, view)
	if !view.Started || m.PlannedRPS != 0 {
		t.Errorf("after Run: Started %v, planned rps %v; want true and 0", view.Started, m.PlannedRPS)
	}
}
