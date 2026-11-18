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
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
)

// Ground: contract (spec §2) — app.metadata_refresh is a duration of 1s or more,
// as other durations of the config are written; the value is read as written and
// kept, and the key left out means each file is read once at the start.
func TestMetadataRefresh_AcceptedAndRead(t *testing.T) {
	for _, tc := range []struct {
		text string
		want time.Duration
	}{
		{"1s", time.Second},
		{"5m", 5 * time.Minute},
		{"1m30s", 90 * time.Second},
		{"24h", 24 * time.Hour},
	} {
		t.Run(tc.text, func(t *testing.T) {
			cfg := parseApp(t, "  metadata_refresh: "+tc.text+"\n"+tokenHeader)

			if cfg.App.MetadataRefresh != tc.want {
				t.Errorf("MetadataRefresh = %s, want %s", cfg.App.MetadataRefresh, tc.want)
			}
			if cfg.App.RawMetadataRefresh == nil || *cfg.App.RawMetadataRefresh != tc.want {
				t.Errorf("RawMetadataRefresh = %v, want it as written, %s", cfg.App.RawMetadataRefresh, tc.want)
			}
		})
	}
}

// Ground: guard (spec §2, green before the code) — the key left out is not a
// refresh: nil and 0, with or without a file in the metadata.
func TestMetadataRefresh_LeftOutIsNone(t *testing.T) {
	for _, lines := range []string{"", tokenHeader} {
		cfg := parseApp(t, lines)

		if cfg.App.RawMetadataRefresh != nil || cfg.App.MetadataRefresh != 0 {
			t.Errorf("metadata_refresh left out: Raw %v, MetadataRefresh %s, want nil and 0",
				cfg.App.RawMetadataRefresh, cfg.App.MetadataRefresh)
		}
	}
}

// Ground: contract (spec §2) — below 1s is refused, 0s and a negative one too:
// there is no 0 that means off (leave the key out to read once). The value is
// printed as a duration is.
func TestMetadataRefresh_BelowTheMinimumIsRefused(t *testing.T) {
	for _, text := range []string{"999ms", "500ms", "1ms", "0s", "-1s"} {
		t.Run(text, func(t *testing.T) {
			_, err := config.Parse(withApp("  metadata_refresh: " + text + "\n" + tokenHeader))
			if !errors.Is(err, config.ErrInvalidMetadataRefresh) {
				t.Fatalf("error = %v, want ErrInvalidMetadataRefresh", err)
			}

			d, perr := time.ParseDuration(text)
			if perr != nil {
				t.Fatalf("test input %q: %v", text, perr)
			}
			if want := "invalid app.metadata_refresh: " + d.String() + " is below the minimum of 1s"; !strings.Contains(err.Error(), want) {
				t.Errorf("error %q, want it to say %q", err, want)
			}
		})
	}
}

// Ground: guard (spec §2, green before the code) — a bare number is refused the
// way load.ramp refuses it: the decoder will not read it as a duration, with or
// without quotes, and says so as a config that cannot be read.
func TestMetadataRefresh_ABareNumberIsRefusedLikeRamp(t *testing.T) {
	for _, literal := range []string{"30", `"30"`, "1.5"} {
		t.Run(literal, func(t *testing.T) {
			_, errRamp := config.Parse([]byte(strings.Replace(rampYAML, "RAMP", "  ramp: "+literal+"\n", 1)))
			if !errors.Is(errRamp, config.ErrReadConfig) {
				t.Fatalf("ramp: %s gives %v, the premise of this test is gone", literal, errRamp)
			}

			_, err := config.Parse(withApp("  metadata_refresh: " + literal + "\n" + tokenHeader))
			if !errors.Is(err, config.ErrReadConfig) {
				t.Errorf("metadata_refresh: %s gives %v, want the error ramp gives (ErrReadConfig)", literal, err)
			}
		})
	}
}

// Ground: contract (spec §2) — a refresh with nothing to read again is refused
// rather than ignored: no metadata, literal headers only, a ${ENV} (fixed for
// the process), and the escape "$${file:x}" and ${file} (the variable named file)
// all read no file.
func TestMetadataRefresh_NeedsAFileReference(t *testing.T) {
	t.Setenv("LT_TOKEN", "abc")
	t.Setenv("file", "abc")

	const want = "invalid app.metadata_refresh: no value in app.metadata reads a file; leave the key out"

	for _, tc := range []struct{ name, metadata string }{
		{"no metadata", ""},
		{"an empty map", "  metadata: {}\n"},
		{"a literal header", "  metadata:\n    x-a: b\n"},
		{"an environment variable", "  metadata:\n    authorization: 'Bearer ${LT_TOKEN}'\n"},
		{"the escaped reference", "  metadata:\n    x-a: '$${file:x}'\n"},
		{"the variable named file", "  metadata:\n    x-a: '${file}'\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse(withApp("  metadata_refresh: 5s\n" + tc.metadata))
			if !errors.Is(err, config.ErrInvalidMetadataRefresh) {
				t.Fatalf("error = %v, want ErrInvalidMetadataRefresh", err)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q, want it to say %q", err, want)
			}
		})
	}

	// One header that reads a file is enough, whatever the others are.
	cfg := parseApp(t, "  metadata_refresh: 5s\n  metadata:\n    x-a: b\n    authorization: 'Bearer ${file:token.txt}'\n")
	if cfg.App.MetadataRefresh != 5*time.Second {
		t.Errorf("MetadataRefresh = %s, want 5s", cfg.App.MetadataRefresh)
	}
}

// Ground: contract (spec §2) — a key with no value (`metadata_refresh:`, `~`,
// `null`) is refused with the line, because the decoder would read it as left
// out and the file would be read once without a word. The first inserted line of
// withApp is line 3 of the file.
func TestMetadataRefresh_NoValueIsRefusedWithTheLine(t *testing.T) {
	const want = "line 3: invalid app.metadata_refresh: has no value: write a duration such as 5m, or leave the key out"

	for _, tc := range []struct{ name, line, metadata string }{
		{"empty", "  metadata_refresh:\n", tokenHeader},
		{"tilde", "  metadata_refresh: ~\n", tokenHeader},
		{"null", "  metadata_refresh: null\n", tokenHeader},
		{"empty without metadata", "  metadata_refresh:\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse(withApp(tc.line + tc.metadata))
			if !errors.Is(err, config.ErrInvalidMetadataRefresh) {
				t.Fatalf("error = %v, want ErrInvalidMetadataRefresh", err)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q, want it to say %q", err, want)
			}
		})
	}
}
