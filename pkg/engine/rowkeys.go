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
	"slices"
	"strconv"
)

// RowKeys is the key of the report row of every call and every step of calls:
// [i][0] is call i (the head of a scenario), [i][k+1] is calls[i].Next[k].
// A plain call's key is always its Method. A head or step of a scenario has
// its Method for a key when that method appears once among all calls and steps
// of the run, and "<method> (<scenario>, step <n>)" when it appears more than
// once, n counted from 1 with the head as step 1.
//
// A bare name is never left to a repeated method: a scenario added before a
// plain call would otherwise rename the plain call's row.
func RowKeys(calls []Call) [][]string {
	seen := make(map[string]int, len(calls))
	for i := range calls {
		seen[calls[i].Method]++

		for k := range calls[i].Next {
			seen[calls[i].Next[k].Method]++
		}
	}

	keys := make([][]string, len(calls))
	for i := range calls {
		call := &calls[i]

		keys[i] = make([]string, 0, len(call.Next)+1)
		keys[i] = append(keys[i], rowKeyOf(call.Method, call.Scenario, 1, seen))

		for k := range call.Next {
			keys[i] = append(keys[i], rowKeyOf(call.Next[k].Method, call.Scenario, k+2, seen))
		}
	}

	return keys
}

// rowKeyOf is the key of the row of method, step n of scenario; a plain call
// has no scenario, so its key is its method.
func rowKeyOf(method, scenario string, n int, seen map[string]int) string {
	if scenario == "" || seen[method] < 2 {
		return method
	}

	return method + " (" + scenario + ", step " + strconv.Itoa(n) + ")"
}

// rowKey is the key of the call's row: the one New stored, or Method for a call
// built by hand and never given to New.
func (c *Call) rowKey() string {
	if c.key != "" {
		return c.key
	}

	return c.Method
}

func (s *Step) rowKey() string {
	if s.key != "" {
		return s.key
	}

	return s.Method
}

// keyAt is the key of step k of an iteration of the call, 0 being the call.
func (c *Call) keyAt(k int) string {
	if k == 0 {
		return c.rowKey()
	}

	return c.Next[k-1].rowKey()
}

// keyedCalls is calls with the key of every row stored on its call or step;
// calls itself when every key is already there, a copy otherwise: the caller's
// slice and steps are not touched.
func keyedCalls(calls []Call) []Call {
	keys := RowKeys(calls)

	var out []Call

	for i := range calls {
		call := &calls[i]

		var next []Step

		for k := range call.Next {
			if keys[i][k+1] == call.Next[k].rowKey() {
				continue
			}
			if next == nil {
				next = slices.Clone(call.Next)
			}
			next[k].key = keys[i][k+1]
		}

		if keys[i][0] == call.rowKey() && next == nil {
			continue
		}
		if out == nil {
			out = slices.Clone(calls)
		}
		out[i].key = keys[i][0]
		if next != nil {
			out[i].Next = next
		}
	}

	if out == nil {
		return calls
	}

	return out
}
