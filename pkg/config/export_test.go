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

const exportBase = `
app:
  target:
    ip: localhost
    port: 50051
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 10
      duration: 5s
`

func exportYAML(section string) []byte {
	return []byte(exportBase + section)
}

// Ground: Т11 — the keys of the spec become the section. Green on the stub: the
// structs exist and the strict reader fills them; it holds the key names for the
// code.
func TestExport_TheSectionIsRead(t *testing.T) {
	cfg, err := config.Parse(exportYAML("export:\n  prometheus:\n    listen: 127.0.0.1:9464\n    linger: 5s\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if cfg.Export == nil || cfg.Export.Prometheus == nil {
		t.Fatalf("Export = %+v, want export.prometheus read", cfg.Export)
	}
	if got := cfg.Export.Prometheus.Listen; got != "127.0.0.1:9464" {
		t.Errorf("Listen = %q, want 127.0.0.1:9464", got)
	}
}

// Ground: Т11, strict config (П13) — a key the section does not know is an error
// naming it, not a run without the export the user thought they asked for.
// Green on the stub: the strict reader already refuses it.
func TestExport_AnUnknownKeyIsAnError(t *testing.T) {
	for _, section := range []string{
		"export:\n  prometheus:\n    listen: 127.0.0.1:9464\n    lingre: 5s\n",
		"export:\n  prometeus:\n    listen: 127.0.0.1:9464\n",
		"export:\n  influx:\n    listen: 127.0.0.1:9464\n",
	} {
		if _, err := config.Parse(exportYAML(section)); err == nil {
			t.Errorf("Parse accepted an unknown key in %q", section)
		}
	}
}

// Ground: Р1 — absent means off. Green on the stub: nothing is read yet.
func TestExport_WithoutTheSectionNothingIsOn(t *testing.T) {
	cfg, err := config.Parse([]byte(exportBase))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if cfg.Export != nil {
		t.Errorf("Export = %+v without an export: section, want nil", cfg.Export)
	}
}

// Ground: reviewer's R6 — the endpoint lingers 15 s when the config does not say.
func TestExport_LingerDefaultsToFifteenSeconds(t *testing.T) {
	cfg, err := config.Parse(exportYAML("export:\n  prometheus:\n    listen: 127.0.0.1:9464\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := cfg.Export.Prometheus.Linger; got != 15*time.Second {
		t.Errorf("Linger = %v, want 15s", got)
	}
	if config.DefaultLinger != 15*time.Second {
		t.Errorf("DefaultLinger = %v, want 15s", config.DefaultLinger)
	}
}

// Ground: reviewer's R6 — linger: 0 stops at once; it is a value, not "left out",
// so it must not become the default (this is what CI sets to not pay 15 s per run).
// Green on the stub, which leaves Linger at 0: it holds the line against the
// shortcut "zero means unset" in the code.
func TestExport_LingerZeroIsNoLinger(t *testing.T) {
	cfg, err := config.Parse(exportYAML("export:\n  prometheus:\n    listen: 127.0.0.1:9464\n    linger: 0s\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := cfg.Export.Prometheus.Linger; got != 0 {
		t.Errorf("Linger = %v after linger: 0s, want 0", got)
	}
}

// Ground: boundary — a written linger is kept as written, up to the hour.
func TestExport_LingerAsWrittenIsKept(t *testing.T) {
	cfg, err := config.Parse(exportYAML("export:\n  prometheus:\n    listen: 127.0.0.1:9464\n    linger: 2m\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := cfg.Export.Prometheus.Linger; got != 2*time.Minute {
		t.Errorf("Linger = %v, want 2m", got)
	}
}

// Ground: Т11 — "linger < 0 is an error" before the start, with the key named.
func TestExport_ANegativeLingerIsRefused(t *testing.T) {
	_, err := config.Parse(exportYAML("export:\n  prometheus:\n    listen: 127.0.0.1:9464\n    linger: -1s\n"))

	if !errors.Is(err, config.ErrInvalidExport) {
		t.Fatalf("Parse(linger: -1s) = %v, want ErrInvalidExport", err)
	}
	if !strings.Contains(err.Error(), "linger") {
		t.Errorf("the error %q does not name linger", err)
	}
}

// Ground: Т11, impossible input — a listen address that cannot be listened on is
// refused with the config, naming the key, not left to the bind.
func TestExport_AListenWithoutAPortIsRefused(t *testing.T) {
	for _, listen := range []string{"localhost", "127.0.0.1", "9464", ":notaport", "127.0.0.1:70000", "127.0.0.1:-1", "http://127.0.0.1:9464"} {
		_, err := config.Parse(exportYAML("export:\n  prometheus:\n    listen: \"" + listen + "\"\n"))
		if !errors.Is(err, config.ErrInvalidExport) {
			t.Errorf("Parse(listen: %q) = %v, want ErrInvalidExport", listen, err)

			continue
		}
		if !strings.Contains(err.Error(), "listen") {
			t.Errorf("the error for %q does not name listen: %v", listen, err)
		}
	}
}

// Ground: boundary — what a listen address may be: a loopback or any host with a
// port, an empty host (all interfaces), port 0 (a free one), an IPv6 literal.
// Green on the stub, which accepts everything: it holds the line against a check
// that is too strict.
func TestExport_ValidListenAddressesAreAccepted(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:9464", ":9464", "0.0.0.0:9464", "127.0.0.1:0", "[::1]:9464", "localhost:9464"} {
		if _, err := config.Parse(exportYAML("export:\n  prometheus:\n    listen: \"" + listen + "\"\n")); err != nil {
			t.Errorf("Parse(listen: %q) = %v, want accepted", listen, err)
		}
	}
}

// Ground: boundary — the flag alone may turn the export on, so a section with a
// linger and no listen is valid for the config; the command line decides whether
// an address comes from somewhere (cmd test).
func TestExport_AListenLeftOutIsLeftToTheCommandLine(t *testing.T) {
	cfg, err := config.Parse(exportYAML("export:\n  prometheus:\n    linger: 3s\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := cfg.Export.Prometheus.Linger; got != 3*time.Second {
		t.Errorf("Linger = %v, want 3s", got)
	}
}

// Ground: Т11, reviewer's R1(б) — the search builds a new engine per step, so the
// counters restart every hold and there is one report for the whole search;
// export beside it is refused for now, with a text that says so.
func TestExport_BesideABreakpointSearchIsRefused(t *testing.T) {
	raw := searchYAML + "export:\n  prometheus:\n    listen: 127.0.0.1:9464\n"

	_, err := config.Parse([]byte(raw))
	if !errors.Is(err, config.ErrInvalidExport) {
		t.Fatalf("Parse(search + export) = %v, want ErrInvalidExport", err)
	}
	if !strings.Contains(err.Error(), "breakpoint") {
		t.Errorf("the error %q does not name the breakpoint", err)
	}
}

// Ground: edge — the refusal is for a search, not for the section: the same
// search without export: still parses. Green on the stub: it holds the line against
// a refusal that is too wide.
func TestExport_ASearchWithoutExportStillParses(t *testing.T) {
	if _, err := config.Parse([]byte(searchYAML)); err != nil {
		t.Errorf("a search without export: = %v, want accepted", err)
	}
}
