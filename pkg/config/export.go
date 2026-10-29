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
//
// STUB (prometheus-export, layer 5): reads nothing and refuses nothing.
func (m *MasterConfig) resolveExport() error {
	return nil
}
