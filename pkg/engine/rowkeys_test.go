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
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

var rkStages = []Stage{{StartRPS: 1, TargetRPS: 1, Duration: time.Second}}

func rkPlain(method string) Call {
	return Call{Method: method, Timeout: time.Second, Stages: rkStages}
}

// rkFlow is scenario name over methods: the first is the head, the rest steps.
func rkFlow(name string, methods ...string) Call {
	steps := make([]Step, 0, len(methods)-1)
	for _, m := range methods[1:] {
		steps = append(steps, Step{Method: m, Timeout: time.Second})
	}

	return Call{Method: methods[0], Scenario: name, Next: steps, Timeout: time.Second, Stages: rkStages}
}

// Ground: contract — a report row is a call or a step, not a method (verdict
// reviews/scenario-same-method.md §2). A plain call's key is always its method;
// a head or step has its method for a key when the method is not repeated in
// the run and "<method> (<scenario>, step <n>)" when it is, n from 1 with the
// head as step 1. A key written on the first occurrence only (M2), left off a
// plain call that repeats (M3), counted inside one call alone (M4) or numbered
// from 0 (M5) turns a row of the table red.
func TestRowKeys(t *testing.T) {
	const (
		get    = "/pkg.Svc/Get"
		update = "/pkg.Svc/Update"
		other  = "/pkg.Svc/Other"
	)

	for _, tc := range []struct {
		name  string
		calls []Call
		want  [][]string
	}{
		{
			"no repeats: keys are the methods",
			[]Call{rkPlain(get), rkFlow("s1", update, other)},
			[][]string{{get}, {update, other}},
		},
		{
			"head and step 3 repeat",
			[]Call{rkFlow("s1", get, update, get)},
			[][]string{{get + " (s1, step 1)", update, get + " (s1, step 3)"}},
		},
		{
			"head repeated as step 2",
			[]Call{rkFlow("s1", get, get)},
			[][]string{{get + " (s1, step 1)", get + " (s1, step 2)"}},
		},
		{
			"the same method in two scenarios",
			[]Call{rkFlow("s1", get, update), rkFlow("s2", other, get)},
			[][]string{{get + " (s1, step 1)", update}, {other, get + " (s2, step 2)"}},
		},
		{
			"a plain call and a step: the plain one stays bare",
			[]Call{rkPlain(get), rkFlow("s1", other, get)},
			[][]string{{get}, {other, get + " (s1, step 2)"}},
		},
		{
			"a plain call and a head: the plain one stays bare",
			[]Call{rkFlow("s1", get, update), rkPlain(get)},
			[][]string{{get + " (s1, step 1)", update}, {get}},
		},
		{
			"three times in one scenario, another method once",
			[]Call{rkFlow("s1", get, update, get, other, get)},
			[][]string{{get + " (s1, step 1)", update, get + " (s1, step 3)", other, get + " (s1, step 5)"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RowKeys(tc.calls)
			if len(got) != len(tc.want) {
				t.Fatalf("keys for %d calls, want %d", len(got), len(tc.want))
			}
			for i := range tc.want {
				if !slices.Equal(got[i], tc.want[i]) {
					t.Errorf("call %d: keys %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Ground: contract — verdict §1 and §3: a method may repeat in a scenario, in
// two scenarios and as a plain call and a step; two plain calls of one method
// still share a row and are refused, and the names of scenarios, methods and
// row keys are one namespace (a scenario named as a key of another row, M10).
// The text of a refusal names both places, as before.
func TestCheckOptions_RepeatedMethods(t *testing.T) {
	for _, tc := range []struct {
		name  string
		calls []Call
		want  error
		text  []string // what the message must name
	}{
		{"twice in one scenario", []Call{rkFlow("s1", "a/One", "a/Two", "a/One")}, nil, nil},
		{"head repeated as a step", []Call{rkFlow("s1", "a/One", "a/One")}, nil, nil},
		{"in two scenarios", []Call{rkFlow("s1", "a/One", "a/Two"), rkFlow("s2", "a/Three", "a/Two")}, nil, nil},
		{"a plain call and a step", []Call{rkPlain("a/Two"), rkFlow("s1", "a/One", "a/Two")}, nil, nil},
		{"a plain call and a head", []Call{rkPlain("a/One"), rkFlow("s1", "a/One", "a/Two")}, nil, nil},
		{"two plain calls of one method", []Call{rkPlain("a/One"), rkPlain("a/Two"), rkPlain("a/One")}, ErrDuplicateMethod, []string{"a/One", "call 2", "call 0"}},
		{
			"a scenario named as the key of a repeated row",
			[]Call{rkFlow("s1", "a/X", "a/X"), rkFlow("a/X (s1, step 2)", "a/P", "a/Q")},
			ErrDuplicateMethod, []string{"a/X (s1, step 2)", "call 1", "call 0"},
		},
		{
			"a plain call whose method is the key of a repeated row",
			[]Call{rkFlow("s1", "a/X", "a/X"), rkPlain("a/X (s1, step 2)")},
			ErrDuplicateMethod, []string{"a/X (s1, step 2)", "call 1", "call 0"},
		},
		{
			"a scenario named as the method of a repeated step",
			[]Call{rkFlow("s1", "a/X", "a/Y", "a/X"), rkFlow("a/X", "a/P", "a/Q")},
			ErrDuplicateMethod, []string{"a/X", "call 1", "call 0"},
		},
		{
			"a scenario named as a repeated method, the scenario first",
			[]Call{rkFlow("a/X", "a/P", "a/Q"), rkFlow("s1", "a/X", "a/Y", "a/X")},
			ErrDuplicateMethod, []string{"a/X", "call 1", "call 0"},
		},
		{"two scenarios, one name", []Call{rkFlow("s1", "a/One", "a/One"), rkFlow("s1", "a/Three", "a/Four")}, ErrDuplicateMethod, []string{"s1", "call 1", "call 0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckOptions(Options{Calls: tc.calls, MaxInFlight: 100})
			if tc.want == nil {
				if err != nil {
					t.Fatalf("err = %v, want none", err)
				}

				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			for _, part := range tc.text {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("%q does not name %q", err, part)
				}
			}
		})
	}
}

// Ground: contract — verdict §5: the in-flight budget names a call without a
// deadline by its row key, so a repeated step is told apart from its twin.
func TestCheckOptions_BudgetNamesTheRowKeyOfARepeatedStep(t *testing.T) {
	calls := []Call{rkFlow("s1", "a/Get", "a/Update", "a/Get")}
	calls[0].Next[1].Timeout = 0

	err := CheckOptions(Options{Calls: calls, MaxInFlight: 100})

	var budget *InFlightBudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("err = %v, want *InFlightBudgetError", err)
	}
	if want := []string{"a/Get (s1, step 3)"}; !slices.Equal(budget.Unbounded, want) {
		t.Errorf("unbounded %q, want %q", budget.Unbounded, want)
	}
}
