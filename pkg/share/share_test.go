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

package share_test

import (
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/share"
)

// Ground: contract — one grammar for every key that takes a share: whole
// percent with up to three digits after the point, a sign, at most 100%. A
// refusal names the key it was read for.
func TestParse_OneGrammarForEveryKey(t *testing.T) {
	for text, want := range map[string]int64{
		"1%": 1000, "0.5%": 500, "0.125%": 125, "100%": 100000, "0%": 0, "99.999%": 99999,
	} {
		got, err := share.Parse(text, "breakpoint.errors_limit")
		if err != nil || got.Thousandths != want || got.Text != text {
			t.Errorf("%q = %+v, %v, want %d thousandths, text as written", text, got, err, want)
		}
	}

	for _, text := range []string{"0.1255%", "1", "101%", " 1%", "1% ", "1 %", "", "%", "-1%", "1.%", ".5%", "100.001%", "1e1%", "1,5%"} {
		_, err := share.Parse(text, "breakpoint.errors_limit")
		if err == nil || !strings.Contains(err.Error(), "breakpoint.errors_limit") {
			t.Errorf("%q: err %v, want a refusal naming breakpoint.errors_limit", text, err)
		}
	}

	if _, err := share.Parse("101%", "pass.errors"); err == nil || !strings.Contains(err.Error(), "pass.errors") {
		t.Errorf("err %v, want the refusal to name the key it was asked for, pass.errors", err)
	}
}

// Ground: boundary — exactly at the limit holds (≤), one call over breaks;
// the default is 1%.
func TestDecide_ExactlyAtTheLimitHolds(t *testing.T) {
	for _, tc := range []struct {
		name               string
		bad, client, total int
		limit              share.Percent
		want               share.Verdict
	}{
		{"10 of 1000 at 1%", 10, 0, 1000, share.Default, share.Holds},
		{"11 of 1000 at 1%", 11, 0, 1000, share.Default, share.Breaks},
		{"none", 0, 0, 1000, share.Default, share.Holds},
		{"0.1% limit, 1 of 1000", 1, 0, 1000, share.Percent{Thousandths: 100, Text: "0.1%"}, share.Holds},
		{"0.1% limit, 2 of 1000", 2, 0, 1000, share.Percent{Thousandths: 100, Text: "0.1%"}, share.Breaks},
		{"0% limit, one failure", 1, 0, 100000, share.Percent{Thousandths: 0, Text: "0%"}, share.Breaks},
		{"0% limit, none", 0, 0, 100000, share.Percent{Thousandths: 0, Text: "0%"}, share.Holds},
		{"100% limit, all failed", 1000, 0, 1000, share.Percent{Thousandths: 100000, Text: "100%"}, share.Holds},
		{"nothing sent", 0, 0, 0, share.Default, share.Holds},
	} {
		if got := share.Decide(tc.bad, tc.client, tc.total, tc.limit); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Ground: boundary — what the client got wrong says nothing of the target:
// over the limit with those, within it without them, nothing is decided; over
// it without them, the target broke.
func TestDecide_FailuresOfTheClientDecideNothing(t *testing.T) {
	for _, tc := range []struct {
		name               string
		bad, client, total int
		want               share.Verdict
	}{
		{"all within the limit", 10, 10, 1000, share.Holds},
		{"all client's, over", 20, 20, 1000, share.Undecided},
		{"target's within, all over", 14, 5, 1000, share.Undecided},
		{"target's exactly at the limit", 15, 5, 1000, share.Undecided},
		{"target's one over", 16, 5, 1000, share.Breaks},
		{"target's alone over", 20, 0, 1000, share.Breaks},
	} {
		if got := share.Decide(tc.bad, tc.client, tc.total, share.Default); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Ground: contract — the printed share never lands on the wrong side of the
// printed limit: a tenth of a percent when that is enough, more digits when
// it is not, rounded up for Breaks.
func TestFormat_NeverOnTheWrongSideOfTheLimit(t *testing.T) {
	limit := func(text string, thousandths int64) share.Percent {
		return share.Percent{Thousandths: thousandths, Text: text}
	}
	for _, tc := range []struct {
		name       string
		bad, total int
		limit      share.Percent
		verdict    share.Verdict
		want       string
	}{
		{"plain", 11, 1000, share.Default, share.Breaks, "1.1%"},
		{"at the limit", 10, 1000, share.Default, share.Holds, "1.0%"},
		{"1.001% breaks, a tenth would say 1.0%", 1001, 100000, share.Default, share.Breaks, "1.1%"},
		{"0.13% breaks 0.12%, a tenth would say 0.1%", 13, 10000, limit("0.12%", 120), share.Breaks, "0.13%"},
		{"0.15% holds 0.15%, a tenth would round up to 0.2%", 15, 10000, limit("0.15%", 150), share.Holds, "0.15%"},
		{"undecided is a tenth", 20, 1000, share.Default, share.Undecided, "2.0%"},
		{"100%", 1000, 1000, limit("100%", 100000), share.Holds, "100.0%"},
	} {
		if got := share.Format(tc.bad, tc.total, tc.limit, tc.verdict); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
