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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// The files below are built from explicit bytes, never from an editor-saved
// fixture: an editor may add or drop the mark.
const (
	utf8BOM = "\xEF\xBB\xBF"
	// utf16Refusal is the sentence every user file kind answers a UTF-16 file with.
	utf16Refusal = "file is UTF-16; save it as UTF-8"
)

// utf16LE and utf16BE are s as Notepad's "Unicode" and "Unicode big endian"
// save it: the mark, then two bytes a character. s is ASCII.
func utf16LE(s string) string {
	b := []byte{0xFF, 0xFE}
	for _, r := range s {
		b = append(b, byte(r), 0)
	}

	return string(b)
}

func utf16BE(s string) string {
	b := []byte{0xFE, 0xFF}
	for _, r := range s {
		b = append(b, 0, byte(r))
	}

	return string(b)
}

func writeConfigFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

const bomMetadataYAML = `
app:
  target:
    ip: localhost
    port: 50051
  tls: false
  metadata:
    x-team: payments
load:
  warmup: 5s
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
      data: '{"wallet_id": "w-42"}'
`

// Kind: YAML config. Before the fix a leading mark fails (the YAML reader takes
// it for part of the first key). A config saved with Notepad's default behaves
// like the same config without the mark: every field the same, the passport's
// hash that of the bytes on disk (docs: «the same as sha256sum load.yaml»), so
// two configs that differ only in the mark are still told apart by compare.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoadFile_ALeadingBOMIsNotPartOfTheConfig(t *testing.T) {
	for name, content := range map[string]string{
		"minimal":         validYAML,
		"metadata, data":  bomMetadataYAML,
		"CRLF line ends":  strings.ReplaceAll(bomMetadataYAML, "\n", "\r\n"),
		"comment on top":  "# written in Notepad\n" + validYAML,
		"document marker": "---\n" + validYAML,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()

			plain, err := config.LoadFile(writeConfigFile(t, dir, "plain.yaml", content))
			if err != nil {
				t.Fatalf("fixture: the config without a mark does not load: %v", err)
			}

			marked, err := config.LoadFile(writeConfigFile(t, dir, "marked.yaml", utf8BOM+content))
			if err != nil {
				t.Fatalf("a config with a leading mark: %v", err)
			}

			if marked.SHA256 != sha256Hex(utf8BOM+content) {
				t.Errorf("SHA256 = %s, want %s: the hash of the file as it lies on disk, the mark included",
					marked.SHA256, sha256Hex(utf8BOM+content))
			}
			if marked.SHA256 == plain.SHA256 {
				t.Error("the hash of the file with a mark equals the one without: the bytes differ, the hash must too")
			}

			plain.SHA256, marked.SHA256 = "", ""
			if !reflect.DeepEqual(plain, marked) {
				t.Errorf("a config with a mark differs from the same one without:\nplain:  %+v\nmarked: %+v", plain, marked)
			}
		})
	}
}

// A file that is not valid says the same, in the same words and at the same
// line and column, with the mark or without it.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoadFile_AnInvalidConfigWithABOMGivesTheBOMLessError(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field": strings.Replace(validYAML, "rps: 800", "rsp: 800", 1),
		"bad value":     strings.Replace(validYAML, "rps: 800", "rps: -3", 1),
		"syntax":        strings.Replace(validYAML, "rps: 800", "rps: [800", 1),
		"no calls":      "app:\n  target:\n    ip: localhost\n    port: 50051\nload:\n  calls: []\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()

			_, plainErr := config.LoadFile(writeConfigFile(t, dir, "plain.yaml", content))
			if plainErr == nil {
				t.Fatal("fixture: the invalid config loads")
			}

			_, markedErr := config.LoadFile(writeConfigFile(t, dir, "marked.yaml", utf8BOM+content))
			if markedErr == nil {
				t.Fatal("a config with a mark and an error loaded")
			}

			if markedErr.Error() != plainErr.Error() {
				t.Errorf("error with a mark:\n%s\nerror without:\n%s", markedErr, plainErr)
			}
		})
	}
}

// A file of nothing but the mark is as empty as an empty file; the check for
// the length that comes before the strip is this row.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoadFile_AFileOfOnlyABOMIsEmpty(t *testing.T) {
	for name, content := range map[string]string{
		"only the mark":       utf8BOM,
		"mark and line break": utf8BOM + "\n",
		"mark and comment":    utf8BOM + "# nothing here\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()

			_, wantErr := config.LoadFile(writeConfigFile(t, dir, "empty.yaml", strings.TrimPrefix(content, utf8BOM)))
			if !errors.Is(wantErr, config.ErrEmptyConfig) {
				t.Fatalf("fixture: the file without the mark = %v, want ErrEmptyConfig", wantErr)
			}

			_, err := config.LoadFile(writeConfigFile(t, dir, "marked.yaml", content))
			if !errors.Is(err, config.ErrEmptyConfig) || err.Error() != wantErr.Error() {
				t.Errorf("error = %v, want %v", err, wantErr)
			}
		})
	}

	empty := writeConfigFile(t, t.TempDir(), "zero.yaml", "")
	if _, err := config.LoadFile(empty); !errors.Is(err, config.ErrEmptyConfig) {
		t.Errorf("a file of zero bytes: error = %v, want ErrEmptyConfig", err)
	}
}

// Only the leading mark goes. One in the middle is a character like any other:
// here in a method name, which keeps it, with or without a mark at the start.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoadFile_OnlyTheLeadingBOMIsStripped(t *testing.T) {
	const want = "wallet.v1.WalletService/Get" + utf8BOM + "Balance"

	inName := strings.Replace(validYAML, "wallet.v1.WalletService/GetBalance", want, 1)

	for name, content := range map[string]string{
		"in the middle":      inName,
		"leading and middle": utf8BOM + inName,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.LoadFile(writeConfigFile(t, t.TempDir(), "load.yaml", content))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := cfg.Load.Calls[0].Method; got != want {
				t.Errorf("method = %q, want %q: a mark that is not at the start is data", got, want)
			}
		})
	}
}

// A UTF-16 file is refused, naming the file and the way out: nothing guesses
// an encoding and nothing decodes it.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoadFile_AUTF16ConfigIsRefused(t *testing.T) {
	for name, content := range map[string]string{
		"little endian":     utf16LE(validYAML),
		"big endian":        utf16BE(validYAML),
		"little endian bom": utf16LE(""),
		"big endian bom":    utf16BE(""),
	} {
		t.Run(name, func(t *testing.T) {
			path := writeConfigFile(t, t.TempDir(), "load.yaml", content)

			_, err := config.LoadFile(path)
			if err == nil {
				t.Fatal("a UTF-16 config loaded")
			}
			if !strings.Contains(err.Error(), utf16Refusal) {
				t.Errorf("error = %q, want it to say %q", err, utf16Refusal)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error = %q, want it to name the file %s", err, path)
			}
		})
	}
}
