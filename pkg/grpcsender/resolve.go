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

package grpcsender

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// hasScheme says the target names its own resolver, as dns:///host:443.
func hasScheme(target string) bool { return strings.Contains(target, "://") }

// addresses is the address each of n connections goes to, and the addresses
// the target stands for. A target with a scheme is its own resolver's: every
// connection takes it as given. Otherwise the name is resolved once, here, and
// connection i takes address i mod len: the sender never looks the name up
// again, and a reconnect goes to the same address.
func (s *Sender) addresses(ctx context.Context, n int) (each, resolved []string, err error) {
	target := s.opts.Target

	if hasScheme(target) {
		resolved = []string{target}
	} else if resolved, err = s.resolve(ctx, target); err != nil {
		return nil, nil, err
	}

	each = make([]string, n)
	for i := range each {
		each[i] = resolved[i%len(resolved)]
	}

	return each, resolved, nil
}

// resolve is the addresses of a target without a scheme: an IP literal is its
// own, a name is looked up.
func (s *Sender) resolve(ctx context.Context, target string) ([]string, error) {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", target, err)
	}

	if _, parseErr := netip.ParseAddr(host); parseErr == nil {
		return []string{target}, nil
	}

	lookup := s.opts.Lookup
	if lookup == nil {
		lookup = defaultLookup
	}

	resolved, err := lookup(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: resolve %s: %w", target, host, err)
	}

	if len(resolved) == 0 {
		return nil, fmt.Errorf("connect to %s: resolve %s: no address", target, host)
	}

	return resolved, nil
}

// defaultLookup resolves the host of target by the system resolver.
func defaultLookup(ctx context.Context, target string) ([]string, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}

	return joinAddresses(ips, port), nil
}

// joinAddresses makes host:port addresses of what a lookup answered, each once,
// in the order the resolver gave them: the first of a name's addresses is the
// resolver's own preference.
func joinAddresses(ips []string, port string) []string {
	seen := make(map[string]struct{}, len(ips))
	out := make([]string, 0, len(ips))

	for _, ip := range ips {
		if _, dup := seen[ip]; dup {
			continue
		}

		seen[ip] = struct{}{}
		out = append(out, net.JoinHostPort(ip, port))
	}

	return out
}
