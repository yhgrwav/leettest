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
	"slices"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/engine"
)

const oneConnectionNote = "one connection: behind an L4 balancer this measures one backend; see connections"

// ranOver is a search result whose every run went over conns.
func ranOver(res breakpoint.Result, conns *engine.Connections) breakpoint.Result {
	res.Steps = slices.Clone(res.Steps)
	for i := range res.Steps {
		res.Steps[i].Report.Connections = conns
	}

	return res
}

// Ground: contract — a search that found where the target breaks, over one connection, says what
// that number is: behind an L4 balancer one connection loads one backend, so it is that backend's
// breaking point, not the service's. The note is in the search's own notes, in the text and in
// JSON, in the same words. Only for an outcome that names a break (broke, broke_at_first) and only
// at one connection: with several there is nothing to warn of, and a search that found no break
// or did not finish is about something else. Mutations "the note at N = 3", "the note for held_all"
// and "no note for broke_at_first" turn it red.
func TestBreakpoint_OneConnectionNote(t *testing.T) {
	one := &engine.Connections{Open: 1}
	three := several([]string{addr1}, callsOn(addr1, 10, 0), callsOn(addr1, 10, 0), callsOn(addr1, 10, 0))

	for _, tc := range []struct {
		name  string
		res   breakpoint.Result
		conns *engine.Connections
		want  bool
	}{
		{"broke, one connection", outcomes["broke"].res, one, true},
		{"broke at the first step, one connection", outcomes["broke_at_first"].res, one, true},
		{"broke, three connections", outcomes["broke"].res, three, false},
		{"broke, the sender said nothing of its connections", outcomes["broke"].res, nil, false},
		{"held every step, one connection", outcomes["held_all"].res, one, false},
		{"the run gave out, one connection", outcomes["run_limit"].res, one, false},
		{"stopped, one connection", outcomes["stopped"].res, one, false},
		{"invalid, one connection", outcomes["invalid"].res, one, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := ranOver(tc.res, tc.conns)

			if got := strings.Contains(printedSearch(res), "\n  - "+oneConnectionNote+"\n"); got != tc.want {
				t.Errorf("the note in the text report: %v, want %v:\n%s", got, tc.want, printedSearch(res))
			}

			notes, _ := searchJSON(t, res)["breakpoint"].(map[string]any)["notes"].([]any)
			if got := slices.Contains(notes, any(oneConnectionNote)); got != tc.want {
				t.Errorf("the note in the JSON notes: %v, want %v: %v", got, tc.want, notes)
			}
		})
	}
}
