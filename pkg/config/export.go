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

package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// DefaultLinger is how long the exporter keeps serving the final values after
// the run when the config does not say.
const DefaultLinger = 15 * time.Second

// ErrInvalidExport is an export: section that cannot be used: an address that
// is not host:port, a negative linger, or a search (load.breakpoint) beside it.
var ErrInvalidExport = errors.New("invalid export")

// Export is the export: section; nil leaves every exporter off.
type Export struct {
	Prometheus *Prometheus `yaml:"prometheus"`
}

// Prometheus is export.prometheus: a /metrics endpoint served during the run.
type Prometheus struct {
	// Listen is the host:port the endpoint listens on, such as 127.0.0.1:9464;
	// port 0 takes a free one. Empty when only the -metrics-listen flag turns
	// the export on.
	Listen string `yaml:"listen"`
	// RawLinger is linger as written, nil when it was left out. Linger is how
	// long the endpoint keeps the final values after the run: DefaultLinger
	// when left out, 0 for none.
	RawLinger *time.Duration `yaml:"linger"`
	Linger    time.Duration  `yaml:"-"`
}

// resolveExport reads export: and refuses what cannot run. It wraps
// ErrInvalidExport.
func (m *MasterConfig) resolveExport() error {
	if m.Export == nil || m.Export.Prometheus == nil {
		return nil
	}
	p := m.Export.Prometheus

	if m.Load.Breakpoint != nil {
		return fmt.Errorf("%w: export cannot run beside load.breakpoint: the search builds a new engine per step", ErrInvalidExport)
	}

	if p.RawLinger == nil {
		p.Linger = DefaultLinger
	} else {
		p.Linger = *p.RawLinger
	}
	if p.Linger < 0 {
		return fmt.Errorf("%w: export.prometheus.linger %s must not be negative", ErrInvalidExport, p.Linger)
	}

	if p.Listen != "" {
		if err := checkListen(p.Listen); err != nil {
			return fmt.Errorf("%w: export.prometheus.listen %q: %w", ErrInvalidExport, p.Listen, err)
		}
	}

	return nil
}

func checkListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	if !validListenHost(host) {
		return fmt.Errorf("host %q is not a host name or an IP address", host)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("port %q is not a whole number from 0 to 65535", port)
	}

	return nil
}

func validListenHost(host string) bool {
	if host == "" || net.ParseIP(host) != nil {
		return true
	}
	for _, r := range host {
		letter := r == '_' || r == '.' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !letter {
			return false
		}
	}

	return true
}
