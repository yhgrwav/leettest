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
	"net"
	"strings"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// maxNamed is how many connections a line names before it says "and <m> more".
const maxNamed = 5

// severalConnections says the run had more than one connection and the engine
// counted each: the only case with something to tell per connection. With one,
// every report is as it was before connections: N.
func severalConnections(c *engine.Connections) bool {
	return c != nil && len(c.Each) >= 2
}

// resolvedHost is the host of a target LeetTest resolved itself, "" for an IP
// address or a target with a scheme: those are printed as given, the name was
// not looked up here.
func resolvedHost(target string, c *engine.Connections) string {
	if len(c.Resolved) == 0 || len(c.Resolved) == 1 && c.Resolved[0] == target {
		return ""
	}

	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return ""
	}

	return host
}

// connectionsBlock is the paragraph that tells each connection apart, or ""
// with one: which backend behind the name is slow or dead.
func connectionsBlock(target string, c *engine.Connections) string {
	if !severalConnections(c) {
		return ""
	}

	var b strings.Builder

	if host := resolvedHost(target, c); host != "" {
		addresses := fmt.Sprintf("%d addresses", len(c.Resolved))
		if len(c.Resolved) == 1 {
			addresses = "1 address"
		}

		fmt.Fprintf(&b, "Connections: %d to %s of %s", len(c.Each), addresses, host)
	} else {
		fmt.Fprintf(&b, "Connections: %d to %s", len(c.Each), target)
	}

	// Each column is as wide as its widest cell: the number, the calls and the
	// share failed line up on their right edge, the address and p99 on their left.
	rows := make([][4]string, len(c.Each))
	waits := make([]string, len(c.Each))

	var width [4]int

	for i := range c.Each {
		l := &c.Each[i]
		rows[i] = [4]string{
			fmt.Sprint(i + 1), l.Address, plural(l.Calls, "call"),
			shareFailed(l.Failed, l.Calls) + " failed",
		}

		if waited := l.StreamWaited + l.NotSentStream; waited > 0 {
			waits[i] = fmt.Sprintf("%d waited for a stream", waited)
		}

		for j, cell := range rows[i] {
			width[j] = max(width[j], len(cell))
		}
	}

	p99s := make([]string, len(c.Each))
	p99Width := 0

	for i := range c.Each {
		p99s[i] = "p99 " + formatQuantile(c.Each[i].P99)
		p99Width = max(p99Width, len(p99s[i]))
	}

	for i, row := range rows {
		line := fmt.Sprintf("  %*s  %-*s  %*s  %*s  %-*s", width[0], row[0], width[1], row[1], width[2], row[2], width[3], row[3],
			p99Width, p99s[i])
		if waits[i] != "" {
			line += "  " + waits[i]
		}

		b.WriteString("\n" + strings.TrimRight(line, " "))
	}

	return b.String()
}

// shareFailed is the share of a connection's calls that failed, to one
// decimal, and never more than is known: a connection with a failure does not
// read 0.0%, one with a call that held does not read 100.0%.
func shareFailed(failed, calls int) string {
	switch {
	case calls == 0:
		return "-"
	case failed > 0 && failed*2000 < calls:
		return "<0.1%"
	case failed < calls && (calls-failed)*2000 <= calls:
		return ">99.9%"
	}

	return fmt.Sprintf("%.1f%%", 100*float64(failed)/float64(calls))
}

// connectionNotes are the two ways connections can be spread badly over the
// addresses the name gave: more connections than addresses that do not divide
// evenly load some backends more, and more addresses than connections leave
// some without a call. Nothing for an IP address or a target with a scheme:
// there is one address.
func connectionNotes(target string, c *engine.Connections) []string {
	if !severalConnections(c) {
		return nil
	}

	host := resolvedHost(target, c)
	if host == "" {
		return nil
	}

	n, m := len(c.Each), len(c.Resolved)
	perAddress := make(map[string]int, m)

	for i := range c.Each {
		perAddress[c.Each[i].Address]++
	}

	var notes []string

	if n > m && n%m != 0 {
		var over []string

		for _, addr := range c.Resolved {
			// Over the even share n/m, compared without a division.
			if k := perAddress[addr]; k*m > n {
				over = append(over, fmt.Sprintf("%s carries %d of %d", addr, k, n))
			}
		}

		notes = append(notes, fmt.Sprintf("%d connections over %d addresses: %s, the backends are loaded unevenly.",
			n, m, strings.Join(over, ", ")))
	}

	if m > n {
		var unused []string

		for _, addr := range c.Resolved {
			if perAddress[addr] == 0 {
				unused = append(unused, addr)
			}
		}

		notes = append(notes, fmt.Sprintf("%s resolved to %d addresses; %d connections load %d of them: %s got no calls.",
			host, m, n, m-len(unused), strings.Join(unused, ", ")))
	}

	return notes
}

// oneConnectionSearchNote warns that a search over one connection found the
// breaking point of one backend when the service sits behind an L4 balancer.
const oneConnectionSearchNote = "one connection: behind an L4 balancer this measures one backend; see connections"

// connectionsStreamLine is the connections line of the stream notes at several
// connections, and the limit changes under it, one line per connection that
// changed its limit. Never the scalar limits: they are connection 1's.
func connectionsStreamLine(c *engine.Connections) string {
	line := fmt.Sprintf("connections: %d", c.Open)
	if c.Reconnects > 0 {
		line += fmt.Sprintf(" (reconnects: %d)", c.Reconnects)
	}

	switch announced := announcedBy(c); {
	case c.InFlightAnnounced:
		lowest, highest := lastLimitRange(c)
		if lowest == highest {
			line += fmt.Sprintf("; target stream limit %d on each (%d in all)", lowest, c.InFlightLimit)
		} else {
			line += fmt.Sprintf("; target stream limits %d to %d (%d in all)", lowest, highest, c.InFlightLimit)
		}
	case announced == 0:
		line += "; no stream limit announced"
	default:
		line += fmt.Sprintf("; stream limit announced by %d of %d connections", announced, c.Open)
	}

	line += "."

	changed := 0

	for i := range c.Each {
		l := &c.Each[i]
		if l.LimitChanges == 0 {
			continue
		}

		changed++

		if changed > maxNamed {
			continue
		}

		line += fmt.Sprintf("\nconnection %d: target stream limit %d to %d, changed %s",
			i+1, l.FirstLimit, l.LastLimit, plural(l.LimitChanges, "time"))
	}

	switch more := changed - maxNamed; {
	case more == 1:
		line += "\nand 1 more connection changed its stream limit"
	case more > 1:
		line += fmt.Sprintf("\nand %d more connections changed their stream limit", more)
	}

	return line
}

// announcedBy is how many connections announced a stream limit at their last
// handshake.
func announcedBy(c *engine.Connections) int {
	n := 0

	for i := range c.Each {
		if c.Each[i].LimitAnnounced {
			n++
		}
	}

	return n
}

// lastLimitRange is the lowest and the highest of the connections' last
// limits: what the target allows each, which may differ behind a balancer.
func lastLimitRange(c *engine.Connections) (lowest, highest uint32) {
	lowest = c.Each[0].LastLimit

	for i := range c.Each {
		lowest = min(lowest, c.Each[i].LastLimit)
		highest = max(highest, c.Each[i].LastLimit)
	}

	return lowest, highest
}

// connectionsThatWaited names the connections whose calls waited for a stream
// or expired waiting for one, numbered from 1, as a phrase for a verdict; ""
// when none did.
func connectionsThatWaited(c *engine.Connections) string {
	var numbers []string

	for i := range c.Each {
		if c.Each[i].StreamWaited+c.Each[i].NotSentStream > 0 {
			numbers = append(numbers, fmt.Sprint(i+1))
		}
	}

	if len(numbers) == 0 {
		return ""
	}

	if len(numbers) == 1 {
		return "connection " + numbers[0]
	}

	list := strings.Join(numbers[:min(len(numbers), maxNamed)], ", ")

	if more := len(numbers) - maxNamed; more > 0 {
		list += fmt.Sprintf(" and %d more", more)
	}

	return "connections " + list
}

// inFlightBound is the most calls the run could have in flight when every
// connection announced a limit.
func inFlightBound(c *engine.Connections) (int, bool) {
	if severalConnections(c) {
		return c.InFlightLimit, c.InFlightAnnounced
	}

	return c.Open * int(c.LastLimit), c.LimitAnnounced
}
