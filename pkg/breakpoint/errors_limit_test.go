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

package breakpoint

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/share"
)

const quick = 20 * time.Millisecond

// failing is a target that answers fast and fails failed of sent calls at
// every rate.
func failing(sent, failed int) func(int) engine.Report {
	return func(int) engine.Report { return report(sent, failed, quick, quick) }
}

// clientSide says some of the report's failures were the client's: the target
// did not decide them. They stay counted in Failed, as the engine counts them.
func clientSide(r engine.Report, rejected, clientError, badResponse int) engine.Report {
	m := &r.Methods[0]
	m.Rejected.Count, m.ClientError, m.BadResponse.Count = rejected, clientError, badResponse

	return r
}

// from195 is a target that holds below 195 rps and answers with step from it.
func from195(step func() engine.Report) func(int) engine.Report {
	return func(rps int) engine.Report {
		if rps >= 195 {
			return step()
		}

		return capacity(10000)(rps)
	}
}

func percent(text string, thousandths int64) share.Percent {
	return share.Percent{Thousandths: thousandths, Text: text}
}

// Ground: contract — the share of failed calls that breaks a step is the
// user's, not a constant: a stricter limit breaks a target that fails 0.5%, a
// looser one holds a target that fails 2%, and with none set 2% still breaks.
func TestSearch_TheErrorsLimitIsTheUsers(t *testing.T) {
	strict := plan
	strict.ErrorsLimit = percent("0.1%", 100)

	var asked []int
	res, _ := Search(t.Context(), strict, fake(failing(1000, 5), &asked))
	if res.Outcome != BrokeAtFirst || res.Broke != 100 || res.Cause != CauseErrors {
		t.Errorf("0.5%% failed at 0.1%%: %v at %d (%v), want BrokeAtFirst 100 CauseErrors", res.Outcome, res.Broke, res.Cause)
	}
	if why := res.Steps[0].Why; !strings.Contains(why, "over errors_limit 0.1%") {
		t.Errorf("why %q, want it to name the limit it is over", why)
	}

	loose := plan
	loose.ErrorsLimit = percent("5%", 5000)

	asked = nil
	res, _ = Search(t.Context(), loose, fake(failing(1000, 20), &asked))
	rates, _ := loose.Rates()
	if res.Outcome != HeldThroughout || res.Held != rates[len(rates)-1] {
		t.Errorf("2%% failed at 5%%: %v held %d, want HeldThroughout %d", res.Outcome, res.Held, rates[len(rates)-1])
	}

	asked = nil
	res, _ = Search(t.Context(), plan, fake(failing(1000, 20), &asked))
	if res.Outcome != BrokeAtFirst || res.Cause != CauseErrors {
		t.Errorf("2%% failed with no limit set: %v (%v), want the default 1%% to break it", res.Outcome, res.Cause)
	}
}

// Ground: contract — the plan without a limit holds the default; a limit
// written as 0% is a limit, not an absent one.
func TestPlan_NoErrorsLimitIsTheDefault(t *testing.T) {
	if got := (Plan{}).EffectiveErrorsLimit(); got != share.Default {
		t.Errorf("no limit: %+v, want %+v", got, share.Default)
	}
	zero := percent("0%", 0)
	if got := (Plan{ErrorsLimit: zero}).EffectiveErrorsLimit(); got != zero {
		t.Errorf("0%%: %+v, want it kept, not replaced by the default", got)
	}
	five := percent("5%", 5000)
	if got := (Plan{ErrorsLimit: five}).EffectiveErrorsLimit(); got != five {
		t.Errorf("5%%: %+v, want it kept", got)
	}
}

// Ground: boundary — the share printed in the reason is on the side of the
// limit the verdict is: 1001 of 100000 is over 1% where a tenth would print
// 1.0%; 13 of 10000 is over 0.12% where a tenth would print 0.1%.
func TestSearch_TheReasonNeverPrintsTheShareOnTheWrongSide(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		limit                share.Percent
		sent, bad            int
		wantShare, wantLimit string
		wantCounts           string
	}{
		{"default limit", share.Percent{}, 100000, 1001, "(1.1%", "over errors_limit 1%", "failed 1001 of 100000 calls"},
		{"a limit of two digits", percent("0.12%", 120), 10000, 13, "(0.13%", "over errors_limit 0.12%", "failed 13 of 10000 calls"},
	} {
		p := Plan{From: 100, To: 125, Settle: plan.Settle, Hold: plan.Hold, ErrorsLimit: tc.limit}
		var asked []int
		target := func(rps int) engine.Report {
			if rps == 100 {
				return report(1000, 0, quick, quick)
			}

			return report(tc.sent, tc.bad, quick, quick)
		}
		res, _ := Search(t.Context(), p, fake(target, &asked))
		i := slices.IndexFunc(res.Steps, func(s Step) bool { return s.RPS == 125 && s.Kind == RateStep })
		if i < 0 {
			t.Fatalf("%s: no step at 125: %+v", tc.name, res.Steps)
		}
		for _, want := range []string{tc.wantShare, tc.wantLimit, tc.wantCounts} {
			if why := res.Steps[i].Why; !strings.Contains(why, want) {
				t.Errorf("%s: why %q, want it to contain %q", tc.name, why, want)
			}
		}
	}
}

// Ground: contract — failures the client had are not the target's: with
// them over the limit and the target's own within it, the step is the run's
// limit (what held below stays held, nothing is repeated), not a break; the
// target's own over the limit still break it, the client's named in the reason.
func TestSearch_FailuresOfTheClientAreNotTheTargetsBreak(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target func(rps int) engine.Report
	}{
		{"rejected requests", from195(func() engine.Report { return clientSide(report(1000, 20, quick, quick), 20, 0, 0) })},
		{"client errors", from195(func() engine.Report { return clientSide(report(1000, 20, quick, quick), 0, 20, 0) })},
		{"our bad responses", from195(func() engine.Report { return clientSide(report(1000, 20, quick, quick), 0, 0, 20) })},
		{"a mix, the target's own within the limit", from195(func() engine.Report { return clientSide(report(1000, 15, quick, quick), 4, 3, 3) })},
	} {
		var asked []int
		res, err := Search(t.Context(), plan, fake(tc.target, &asked))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if res.Outcome != RunLimit || res.Held != 156 || res.Broke != 195 || res.Cause != CauseClientErrors {
			t.Errorf("%s: %v held %d at %d (%v), want RunLimit 156 195 CauseClientErrors", tc.name, res.Outcome, res.Held, res.Broke, res.Cause)
		}
		if want := []int{100, 125, 156, 195}; !slices.Equal(asked, want) {
			t.Errorf("%s: ran %v, want %v: a run limit is not repeated", tc.name, asked, want)
		}
		last := res.Steps[len(res.Steps)-1]
		if last.Broken || !strings.Contains(last.Why, "client side") || !strings.Contains(last.Why, "errors_limit 1%") {
			t.Errorf("%s: last step broken %v why %q, want not broken, a reason naming the client's share and the limit", tc.name, last.Broken, last.Why)
		}
	}

	// 30 failed, 10 of them the client's: the target's 20 are over 1% of 1000.
	var asked []int
	mixed := from195(func() engine.Report { return clientSide(report(1000, 30, quick, quick), 10, 0, 0) })
	res, _ := Search(t.Context(), plan, fake(mixed, &asked))
	if res.Outcome != BrokeBetween || res.Broke != 195 || res.Cause != CauseErrors {
		t.Errorf("target's own over: %v at %d (%v), want BrokeBetween 195 CauseErrors", res.Outcome, res.Broke, res.Cause)
	}
	if why := res.Steps[len(res.Steps)-1].Why; !strings.Contains(why, "10 client side") || !strings.Contains(why, "over errors_limit 1%") {
		t.Errorf("why %q, want the client's 10 named beside the limit", why)
	}
}

// Ground: boundary — a recovery probe is judged by its p99 when the target's
// own failures are within the limit: a probe of 1000 calls where the client
// rejects 2% of them says nothing bad of the target. A probe whose target
// fails over the limit has not recovered.
func TestSearch_AProbeWithFailuresOfTheClientRecovers(t *testing.T) {
	probeOf := func(probe engine.Report) func(int) engine.Report {
		var calls int

		return func(rps int) engine.Report {
			calls++
			if rps == 100 && calls > 6 {
				return probe
			}

			return capacity(270)(rps)
		}
	}

	var asked []int
	clients := probeOf(clientSide(report(1000, 20, quick, quick), 20, 0, 0))
	res, _ := Search(t.Context(), plan, fake(clients, &asked))
	if want := []int{100, 125, 156, 195, 244, 305, 100, 305}; !slices.Equal(asked, want) {
		t.Errorf("ran %v, want %v: the probe recovered, then the repeat", asked, want)
	}
	if res.Outcome != BrokeBetween || res.Broke != 305 || res.Cause != CauseErrors {
		t.Errorf("%v at %d (%v), want BrokeBetween 305 CauseErrors, not NoRecovery", res.Outcome, res.Broke, res.Cause)
	}

	asked = nil
	target := probeOf(report(1000, 20, quick, quick))
	res, _ = Search(t.Context(), plan, fake(target, &asked))
	if want := []int{100, 125, 156, 195, 244, 305, 100, 100, 100, 100, 100}; !slices.Equal(asked, want) {
		t.Errorf("ran %v, want %v: five probes the target failed, no repeat", asked, want)
	}
	if res.Cause != CauseNoRecovery {
		t.Errorf("cause %v, want CauseNoRecovery", res.Cause)
	}
}

// Ground: characterization — a call not sent for the connection breaks a step
// at any errors_limit, by the wait rule that is judged before the failures
// (cause connection). The share counts such calls too, but a search never
// reaches it with one.
func TestSearch_ACallNotSentForTheConnectionBreaksAtAnyErrorsLimit(t *testing.T) {
	p := plan
	p.ErrorsLimit = percent("5%", 5000)

	target := from195(func() engine.Report {
		r := capacity(10000)(195)
		r.NotSent, r.NotSentConnection, r.ConnectionTailCalls, r.ConnectionCauseCalls = 1, 1, 1, 1

		return r
	})
	var asked []int
	res, _ := Search(t.Context(), p, fake(target, &asked))
	if res.Outcome != BrokeBetween || res.Held != 156 || res.Broke != 195 || res.Cause != CauseConnection {
		t.Errorf("%v held %d at %d (%v), want BrokeBetween 156 195 CauseConnection", res.Outcome, res.Held, res.Broke, res.Cause)
	}
}
