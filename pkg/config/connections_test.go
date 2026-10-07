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

package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// Ground: contract — pkg/config is a library API; a config that says nothing about connections
// runs over one, as before.
func TestLoad_ConnectionsDefaultsToOne(t *testing.T) {
	cfg, err := config.Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if cfg.App.Connections != 1 {
		t.Errorf("Connections = %d, want 1 when app.connections is left out", cfg.App.Connections)
	}
}

// Ground: boundary — the range is 1 to 256 inclusive: the smallest, a few, and the largest are
// read as written. A mutation moving the upper bound down to 255 (256 refused) or the lower up to 2 (1 refused)
// turns it red; one moving the upper bound up to 257 is OutOfRange's.
func TestLoad_ConnectionsAreReadWithinTheRange(t *testing.T) {
	for _, tc := range []struct {
		written string
		want    int
	}{{"1", 1}, {"2", 2}, {"37", 37}, {"256", 256}} {
		cfg, err := config.Parse(withApp("  connections: " + tc.written + "\n"))
		if err != nil {
			t.Errorf("connections: %s: %v", tc.written, err)

			continue
		}
		if cfg.App.Connections != tc.want {
			t.Errorf("connections: %s: Connections = %d, want %d", tc.written, cfg.App.Connections, tc.want)
		}
	}
}

// Ground: contract — a refused value is printed as the user wrote it, so the user can see why a
// number that looks whole is refused: 2.0 is not printed as 2, 1e2 and 0x10 are not read as the
// numbers they stand for. A quoted value is printed without its quotes. Same rule as rps.
func TestLoad_ConnectionsRefusedAsWritten(t *testing.T) {
	for _, tc := range []struct {
		written string
		text    string
	}{
		{"2.0", "2.0"},
		{"2.5", "2.5"},
		{"two", "two"},
		{"0", "0"},
		{"257", "257"},
		{"1e2", "1e2"},
		{"0x10", "0x10"},
		{`"2.0"`, "2.0"},
	} {
		_, err := config.Parse(withApp("  connections: " + tc.written + "\n"))
		if !errors.Is(err, config.ErrInvalidConnections) {
			t.Errorf("connections: %s: err = %v, want ErrInvalidConnections", tc.written, err)

			continue
		}
		if !strings.HasSuffix(err.Error(), ": "+tc.text) {
			t.Errorf("connections: %s: the error %q does not end with %q", tc.written, err, ": "+tc.text)
		}
	}
}

// Ground: contract — a quoted whole number is a number, as rps: "2" is.
func TestLoad_ConnectionsQuotedIsANumber(t *testing.T) {
	for _, written := range []string{`"2"`, `'2'`} {
		cfg, err := config.Parse(withApp("  connections: " + written + "\n"))
		if err != nil {
			t.Errorf("connections: %s: %v", written, err)

			continue
		}
		if cfg.App.Connections != 2 {
			t.Errorf("connections: %s: Connections = %d, want 2", written, cfg.App.Connections)
		}
	}
}

// Ground: contract — the key with no value is the same as the key left out: 1. Green on the
// current code by design; the inverse mutation (nothing becomes an error) turns it red.
func TestLoad_ConnectionsNullIsOne(t *testing.T) {
	for _, written := range []string{"", " null", " ~"} {
		cfg, err := config.Parse(withApp("  connections:" + written + "\n"))
		if err != nil {
			t.Errorf("connections:%s: %v", written, err)

			continue
		}
		if cfg.App.Connections != 1 {
			t.Errorf("connections:%s: Connections = %d, want 1", written, cfg.App.Connections)
		}
	}
}

// Ground: boundary — a count outside 1 to 256 or not a whole number is refused before the run,
// by the field's name and its range: 0 would be "no connection", 257 one past the top, "two" and
// 2.5 are not integers. Each is its own error, not a silent 1.
func TestLoad_ConnectionsOutOfRange(t *testing.T) {
	for _, written := range []string{"0", "-1", "257", "100000", "two", "2.5"} {
		_, err := config.Parse(withApp("  connections: " + written + "\n"))
		if !errors.Is(err, config.ErrInvalidConnections) {
			t.Errorf("connections: %s: err = %v, want ErrInvalidConnections", written, err)

			continue
		}
		for _, claim := range []string{"app.connections", "1", "256"} {
			if !strings.Contains(err.Error(), claim) {
				t.Errorf("connections: %s: the error %q does not name %q", written, err, claim)
			}
		}
	}
}
