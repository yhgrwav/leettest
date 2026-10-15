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

// Ground: contract — pkg/config is a library API; app.compression is gzip, or
// left out for none.
func TestLoad_CompressionIsGzipOrLeftOut(t *testing.T) {
	cfg, err := config.Parse(withApp("  compression: gzip\n"))
	if err != nil {
		t.Fatalf("parse gzip: %v", err)
	}
	if cfg.App.Compression != "gzip" {
		t.Errorf("Compression = %q, want gzip", cfg.App.Compression)
	}

	cfg, err = config.Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("parse without it: %v", err)
	}
	if cfg.App.Compression != "" {
		t.Errorf("Compression = %q, want none when left out", cfg.App.Compression)
	}
}

// Ground: contract — anything but gzip is refused, and the text names the
// value as written. Case counts: the encoding's registered name is lowercase.
func TestLoad_CompressionOtherValuesAreRefused(t *testing.T) {
	for _, written := range []string{"snappy", "deflate", "GZIP", "none", `""`, "true", "1"} {
		_, err := config.Parse(withApp("  compression: " + written + "\n"))
		if !errors.Is(err, config.ErrInvalidCompression) {
			t.Errorf("compression: %s: error %v, want ErrInvalidCompression", written, err)
		}
	}
}
