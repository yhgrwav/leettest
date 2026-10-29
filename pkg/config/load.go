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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

var ErrReadConfig = errors.New("cannot read config")

// LoadFile reads the config file, applies defaults and validates the result.
func LoadFile(path string) (*MasterConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrReadConfig, path, err)
	}

	cfg, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	cfg.App.relativeTo(dir)

	if err := cfg.App.resolveProto(dir); err != nil {
		return nil, err
	}

	if err := cfg.readDatasets(dir); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Parse reads the config from raw YAML, applies defaults and validates the result.
func Parse(raw []byte) (*MasterConfig, error) {
	if err := checkNumbers(raw); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadConfig, err)
	}
	if err := checkTree(raw); err != nil {
		return nil, err
	}

	var cfg MasterConfig
	if err := yaml.UnmarshalWithOptions(raw, &cfg, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadConfig, err)
	}

	sum := sha256.Sum256(raw)
	cfg.SHA256 = hex.EncodeToString(sum[:])

	if err := cfg.resolve(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := cfg.resolvePass(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (m *MasterConfig) resolve() error {
	if err := m.resolveLabels(); err != nil {
		return err
	}
	m.App.ResolveTLS()
	if err := m.App.resolveMetadata(); err != nil {
		return err
	}
	if err := m.App.resolveMaxResponseSize(); err != nil {
		return err
	}
	if err := m.App.resolveConnections(); err != nil {
		return err
	}
	if m.App.RawMode == nil {
		m.App.Mode = ModeLocal
	} else if v := *m.App.RawMode; v == ModeLocal || v == ModeReal {
		m.App.Mode = v
	} else {
		return fmt.Errorf("%w: %s", ErrInvalidMode, v)
	}
	if err := m.App.resolveCompression(); err != nil {
		return err
	}
	for i := range m.Load.Calls {
		m.Load.Calls[i].ResolveTimeout()
		if raw := m.Load.Calls[i].RawDataset; raw != nil {
			m.Load.Calls[i].Dataset = *raw
		}
	}

	if b := m.Load.Breakpoint; b != nil {
		if err := b.resolveErrorsLimit(); err != nil {
			return err
		}
	}

	if err := m.Load.resolveStopOnErrors(); err != nil {
		return err
	}

	if err := m.resolveExport(); err != nil {
		return err
	}

	address, err := m.App.Target.CreateConnectionString()
	if err != nil {
		return err
	}
	m.App.Address = address

	return nil
}

func (m *MasterConfig) Validate() error {
	var errs []error

	if m.Name != nil && strings.TrimSpace(*m.Name) == "" {
		errs = append(errs, ErrEmptyName)
	}
	if err := m.App.validateTLSFiles(); err != nil {
		errs = append(errs, err)
	}
	if err := m.App.validateProto(); err != nil {
		errs = append(errs, err)
	}
	if m.App.Mode == ModeReal && m.Load.Breakpoint == nil && m.Load.Ramp == 0 {
		errs = append(errs, ErrRealNeedsRamp)
	}
	if m.App.Mode == ModeReal && m.Load.Breakpoint == nil && m.Load.StopOnErrors.Thousandths == 0 {
		errs = append(errs, ErrRealNeedsStop)
	}
	if err := m.Load.Validate(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}
