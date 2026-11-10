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

// yamlLibraryText is what a file the YAML library cannot decode says: the
// reason, no panic text, no stack.
const yamlLibraryText = "cannot read config: the YAML library could not decode this file"

// A tag with no value (`!`) where a list is expected makes goccy/go-yaml
// v1.19.2 dereference a nil node (ArrayNodeIter.Len, called from
// Decoder.decodeSlice). Found by FuzzParse. Go strings carry explicit \n.
// Ground: contract — pkg/config is a library API; callers get an error, never a panic.
func TestParse_YAMLLibraryPanic(t *testing.T) {
	for name, raw := range map[string]string{
		"the fuzz input": "00A:00000000:\n    0A: 00000000A\n0000000A: 10000\nload:\n  00000A: 0A\n  calls:    ! ",
		"shortest known": "load:\n  calls: !\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := parseNoPanic(t, raw)

			if cfg != nil {
				t.Errorf("Parse(%q) config = %+v, want nil", raw, cfg)
			}
			if err == nil {
				t.Fatalf("Parse(%q) returned no error", raw)
			}
			if !errors.Is(err, config.ErrReadConfig) {
				t.Errorf("Parse(%q) error = %v, want it to wrap %v", raw, err, config.ErrReadConfig)
			}
			if !strings.Contains(err.Error(), yamlLibraryText) {
				t.Errorf("Parse(%q) error = %q, want it to say %q", raw, err, yamlLibraryText)
			}
			for _, internal := range []string{"ast.", "yaml.", "nil pointer", "runtime error", "goroutine", ".go:", "panic"} {
				if strings.Contains(err.Error(), internal) {
					t.Errorf("Parse(%q) error %q names an internal thing: %q", raw, err, internal)
				}
			}
		})
	}
}
