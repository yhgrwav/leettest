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
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// Ground: contract — a key written with no value is refused with its line, its
// own sentinel and its own hint, word for word. The table of checkTree for
// keys with no value (app.mode, load.stop_on_errors, every pass path) is one
// place; this pins what it prints. app.metadata_refresh and app.idle_timeout
// are pinned in their own tests.
func TestNulls_NoValueMessagesAreExact(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  []byte
		want string
		is   error
	}{
		{"app.mode", modeConfig("  mode:\n", "  ramp: 30s\n"),
			"line 6: app.mode must be local or real, or left out: no value", config.ErrInvalidMode},
		{"load.stop_on_errors", stopConfig("", "  stop_on_errors: ~\n"),
			"line 7: invalid load.stop_on_errors: has no value: write a percent such as 5%, or leave the key out",
			config.ErrInvalidStopOnErrors},
		{"pass of a call", callPass("", "      pass:\n"),
			"line 10: invalid pass: load.calls[0].pass has no value: write the limit or leave it out", config.ErrThreshold},
		{"a limit of a call", callPass("", "      pass:\n        p99:\n        errors: 1%\n"),
			"line 11: invalid pass: load.calls[0].pass.p99 has no value: write the limit or leave it out", config.ErrThreshold},
		{"pass of the search", searchPass("", "    pass:\n"),
			"line 16: invalid pass: load.breakpoint.pass has no value: write the limit or leave it out", config.ErrThreshold},
		{"a limit of the search", searchPass("", "    pass:\n      held_rps: ~\n"),
			"line 17: invalid pass: load.breakpoint.pass.held_rps has no value: write the limit or leave it out", config.ErrThreshold},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse(tc.cfg)
			if err == nil {
				t.Fatalf("accepted, want %q", tc.want)
			}
			if !errors.Is(err, tc.is) {
				t.Errorf("error %v, want %v", err, tc.is)
			}
			if err.Error() != tc.want {
				t.Errorf("error %q, want exactly %q", err.Error(), tc.want)
			}
		})
	}
}

// Ground: contract — two keys with no value in one file are both named, in
// the order of the file: the checks do not stop at the first.
func TestNulls_TwoKeysWithNoValueAreBothNamedInFileOrder(t *testing.T) {
	const want = "line 3: invalid app.idle_timeout: has no value: write a duration such as 5m, or leave the key out\n" +
		"line 4: invalid app.metadata_refresh: has no value: write a duration such as 5m, or leave the key out"

	_, err := config.Parse(withApp("  idle_timeout:\n  metadata_refresh:\n"))
	if err == nil {
		t.Fatalf("accepted, want %q", want)
	}
	if err.Error() != want {
		t.Errorf("error %q, want exactly %q", err.Error(), want)
	}
}
