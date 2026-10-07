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
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/metrics"
)

const (
	addr1 = "10.0.0.1:443"
	addr2 = "10.0.0.2:443"
	addr3 = "10.0.0.3:443"
	addr4 = "10.0.0.4:443"
)

// exactUS is an exact percentile of us microseconds.
func exactUS(us int) metrics.Quantile {
	return metrics.Quantile{Value: time.Duration(us) * time.Microsecond, Exact: true, Defined: true}
}

// callsOn is a connection of the run that carried calls and failed failed of them.
func callsOn(addr string, calls, failed int) engine.LinkReport {
	return engine.LinkReport{Address: addr, Calls: calls, Failed: failed}
}

// limited is a connection whose last handshake announced last, the first one first, and that
// changed its limit changes times.
func limited(addr string, first, last uint32, changes int) engine.LinkReport {
	return engine.LinkReport{
		Address: addr, Calls: 100, LimitAnnounced: true, FirstLimit: first, LastLimit: last, LimitChanges: changes,
	}
}

// several is what a sender with len(each) connections says of them, as grpcsender's mergeLinks
// builds it: the scalar limits stay 0 (one connection's limit is not the run's), the in-flight
// bound is the sum of the last limits and known only when every connection announced one.
func several(resolved []string, each ...engine.LinkReport) *engine.Connections {
	c := &engine.Connections{Open: len(each), Resolved: resolved, Each: each, InFlightAnnounced: true}

	for _, l := range each {
		c.LimitChanges += l.LimitChanges

		if l.LimitAnnounced {
			c.InFlightLimit += int(l.LastLimit)
		} else {
			c.InFlightAnnounced = false
		}
	}

	if !c.InFlightAnnounced {
		c.InFlightLimit = 0
	}

	c.LimitAnnounced = c.InFlightAnnounced

	return c
}

// overConnections is a run report whose sender had several connections.
func overConnections(conns *engine.Connections) RunReport {
	return RunReport{Report: engine.Report{Duration: 10 * time.Second, Planned: 10 * time.Second, Connections: conns}}
}

func printedTo(target string, run RunReport) string {
	var out strings.Builder
	PrintReport(&out, target, run)

	return out.String()
}

// blockOf is the lines of the "Connections:" paragraph of a printed report.
func blockOf(t *testing.T, text string) []string {
	t.Helper()

	var lines []string

	in := false

	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\n")

		switch {
		case strings.HasPrefix(line, "Connections: "):
			in = true

			lines = append(lines, line)
		case in && line == "":
			return lines
		case in:
			lines = append(lines, line)
		}
	}

	if !in {
		t.Fatalf("no Connections block in:\n%s", text)
	}

	return lines
}

// flat is a line with its runs of spaces made one and the ends trimmed: what a column's width
// does not change.
func flat(line string) string { return strings.Join(strings.Fields(line), " ") }

// oneLine is a note's line breaks made spaces: where a long sentence wraps is not its meaning.
func oneLine(s string) string { return strings.ReplaceAll(s, "\n", " ") }

// streamLines are all the notes of a report, line by line, in order: whether the lines of one
// block are one note or several is not what is checked.
func streamLines(report engine.Report) []string {
	return strings.Split(strings.Join(reportNotes(report, ""), "\n"), "\n")
}
