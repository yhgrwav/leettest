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
// read as written. A mutation moving the upper bound to 257 (or the lower to 2) turns it red.
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
