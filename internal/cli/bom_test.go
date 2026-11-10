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

package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Files are built from explicit bytes, never from an editor-saved fixture.
const (
	bomUTF8 = "\xEF\xBB\xBF"
	// bomUTF16Refusal is the sentence every user file kind answers a UTF-16 file with.
	bomUTF16Refusal = "file is UTF-16; save it as UTF-8"
)

func bomUTF16LE(s string) string {
	b := []byte{0xFF, 0xFE}
	for _, r := range s {
		b = append(b, byte(r), 0)
	}

	return string(b)
}

func bomUTF16BE(s string) string {
	b := []byte{0xFE, 0xFF}
	for _, r := range s {
		b = append(b, 0, byte(r))
	}

	return string(b)
}

func bomWrite(t *testing.T, dir, name, content string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}

// --- the report file leettest compare reads --------------------------------

const (
	bomRunReport    = `{"schema_version":1,"mode":"run","leettest_version":"v0.3.0","target":"10.0.0.1:50051","sent":1200,"failed":3}`
	bomSearchReport = `{"schema_version":1,"mode":"breakpoint","leettest_version":"v0.3.0","target":"10.0.0.1:50051","outcome":"broke_between"}`
)

// Kind: JSON report read by compare. Before the fix a leading mark is "not a
// JSON report: invalid character 'ï' looking for beginning of value", for a
// file leettest wrote itself and an editor re-saved. A report with the mark
// reads as the one without, a plain run and a breakpoint search alike.
func TestReadReport_ALeadingBOMIsNotPartOfTheReport(t *testing.T) {
	for name, content := range map[string]string{
		"plain run":         bomRunReport,
		"breakpoint search": bomSearchReport,
		"indented, CRLF":    strings.ReplaceAll("{\n  \"schema_version\": 1,\n  \"mode\": \"run\",\n  \"sent\": 7\n}\n", "\n", "\r\n"),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()

			plain, err := ReadReport(bomWrite(t, dir, "plain.json", content))
			if err != nil {
				t.Fatalf("fixture: the report without a mark does not read: %v", err)
			}

			marked, err := ReadReport(bomWrite(t, dir, "marked.json", bomUTF8+content))
			if err != nil {
				t.Fatalf("a report with a leading mark: %v", err)
			}

			if !reflect.DeepEqual(plain, marked) {
				t.Errorf("a report with a mark reads differently:\nplain:  %+v\nmarked: %+v", plain, marked)
			}
		})
	}
}

// A file that is not a report says the same with the mark as without it; the
// path is the only difference, and each file has its own.
func TestReadReport_AnInvalidReportWithABOMGivesTheBOMLessError(t *testing.T) {
	for name, content := range map[string]string{
		"not JSON":       `{"schema_version":1,"mode":"run"`,
		"other schema":   `{"schema_version":9,"mode":"run"}`,
		"unknown mode":   `{"schema_version":1,"mode":"soak"}`,
		"wrong type":     `{"schema_version":1,"mode":"run","sent":"many"}`,
		"line one error": "{\n\"schema_version\": 1,\n\"mode\": run\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()

			plainPath := bomWrite(t, filepath.Join(dir, "a"), "r.json", content)
			markedPath := bomWrite(t, filepath.Join(dir, "b"), "r.json", bomUTF8+content)

			_, plainErr := ReadReport(plainPath)
			if plainErr == nil {
				t.Fatal("fixture: the invalid report reads")
			}

			_, markedErr := ReadReport(markedPath)
			if markedErr == nil {
				t.Fatal("a report with a mark and an error read")
			}

			got := strings.ReplaceAll(markedErr.Error(), markedPath, "FILE")
			want := strings.ReplaceAll(plainErr.Error(), plainPath, "FILE")

			if got != want {
				t.Errorf("error with a mark:\n%s\nerror without:\n%s", got, want)
			}
		})
	}
}

// A file of only the mark is as empty as an empty file: the same error, not a
// panic on a slice that is shorter than the mark.
func TestReadReport_AFileOfOnlyABOMIsEmpty(t *testing.T) {
	dir := t.TempDir()

	emptyPath := bomWrite(t, filepath.Join(dir, "a"), "r.json", "")
	markedPath := bomWrite(t, filepath.Join(dir, "b"), "r.json", bomUTF8)

	_, wantErr := ReadReport(emptyPath)
	if wantErr == nil {
		t.Fatal("fixture: an empty report reads")
	}

	_, err := ReadReport(markedPath)
	if err == nil {
		t.Fatal("a report of only a mark read")
	}

	if got, want := strings.ReplaceAll(err.Error(), markedPath, "FILE"), strings.ReplaceAll(wantErr.Error(), emptyPath, "FILE"); got != want {
		t.Errorf("error = %q, want the empty file's %q", got, want)
	}
}

// Only the leading mark goes: one between two tokens is no JSON, with or
// without a mark at the start.
func TestReadReport_OnlyTheLeadingBOMIsStripped(t *testing.T) {
	const inside = `{"schema_version":1,` + bomUTF8 + `"mode":"run"}`

	for name, content := range map[string]string{
		"in the middle":      inside,
		"leading and middle": bomUTF8 + inside,
		"two leading":        bomUTF8 + bomUTF8 + bomRunReport,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadReport(bomWrite(t, t.TempDir(), "r.json", content)); err == nil {
				t.Error("a mark that is data was dropped: the report read")
			}
		})
	}
}

// A UTF-16 report is refused, naming the file: PowerShell's `>` writes one.
func TestReadReport_AUTF16ReportIsRefused(t *testing.T) {
	for name, content := range map[string]string{
		"little endian":     bomUTF16LE(bomRunReport),
		"big endian":        bomUTF16BE(bomRunReport),
		"little endian bom": bomUTF16LE(""),
		"big endian bom":    bomUTF16BE(""),
	} {
		t.Run(name, func(t *testing.T) {
			path := bomWrite(t, t.TempDir(), "r.json", content)

			_, err := ReadReport(path)
			if err == nil {
				t.Fatal("a UTF-16 report read")
			}
			if !strings.Contains(err.Error(), bomUTF16Refusal) {
				t.Errorf("error = %q, want it to say %q", err, bomUTF16Refusal)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error = %q, want it to name the file %s", err, path)
			}
		})
	}
}

// --- the settings file -----------------------------------------------------

// bomSettingsAt points the settings at a fresh directory and writes the file.
func bomSettingsAt(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	t.Setenv("HOME", dir)

	path, err := settingsPath()
	if err != nil {
		t.Fatalf("settings path: %v", err)
	}

	return bomWrite(t, filepath.Dir(path), filepath.Base(path), content)
}

const bomSettings = "mode: dark\npalette: ember\n"

// Kind: settings file (settings.yaml in the user's config directory). Before the
// fix a leading mark makes every command fail to start while the file is there.
func TestLoadSettings_ALeadingBOMIsNotPartOfTheSettings(t *testing.T) {
	bomSettingsAt(t, bomSettings)

	plain, err := LoadSettings()
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	path := bomSettingsAt(t, bomUTF8+bomSettings)

	marked, err := LoadSettings()
	if err != nil {
		t.Fatalf("settings with a leading mark: %v", err)
	}

	if marked.Path() != path {
		t.Fatalf("path = %s, want %s", marked.Path(), path)
	}

	if marked.Mode != plain.Mode || marked.Palette != plain.Palette || marked.Lang != plain.Lang || !marked.Configured() {
		t.Errorf("settings with a mark = %+v, want those of the file without: %+v", marked, plain)
	}
}

// An invalid file says the same with the mark as without it, line numbers
// included; only the file's own path may differ, and here it does not.
func TestLoadSettings_AnInvalidFileWithABOMGivesTheBOMLessError(t *testing.T) {
	const content = "mode: dark\npalette: [ember\n"

	bomSettingsAt(t, content)

	_, wantErr := LoadSettings()
	if wantErr == nil {
		t.Fatal("fixture: the invalid settings load")
	}

	bomSettingsAt(t, bomUTF8+content)

	_, err := LoadSettings()
	if err == nil || err.Error() != wantErr.Error() {
		t.Errorf("error with a mark:\n%v\nerror without:\n%v", err, wantErr)
	}
}

// A file of only the mark is as empty as an empty file.
func TestLoadSettings_AFileOfOnlyABOMIsEmpty(t *testing.T) {
	bomSettingsAt(t, "")

	want, wantErr := LoadSettings()

	bomSettingsAt(t, bomUTF8)

	got, err := LoadSettings()
	if (err == nil) != (wantErr == nil) || (err != nil && err.Error() != wantErr.Error()) {
		t.Fatalf("error = %v, want that of the empty file: %v", err, wantErr)
	}

	if err == nil && (got.Configured() != want.Configured() || got.Mode != want.Mode || got.Palette != want.Palette) {
		t.Errorf("settings = %+v, want those of the empty file: %+v", got, want)
	}
}

// Only the leading mark goes: one inside a value stays in it.
func TestLoadSettings_OnlyTheLeadingBOMIsStripped(t *testing.T) {
	const want = "em" + bomUTF8 + "ber"

	for name, content := range map[string]string{
		"in the middle":      "mode: dark\npalette: \"" + want + "\"\n",
		"leading and middle": bomUTF8 + "mode: dark\npalette: \"" + want + "\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			bomSettingsAt(t, content)

			s, err := LoadSettings()
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if s.Palette != want {
				t.Errorf("palette = %q, want %q: a mark that is not at the start is data", s.Palette, want)
			}
		})
	}
}

// A UTF-16 settings file is refused, naming the file.
func TestLoadSettings_AUTF16FileIsRefused(t *testing.T) {
	for name, content := range map[string]string{
		"little endian": bomUTF16LE(bomSettings),
		"big endian":    bomUTF16BE(bomSettings),
		"bom only":      bomUTF16LE(""),
	} {
		t.Run(name, func(t *testing.T) {
			path := bomSettingsAt(t, content)

			_, err := LoadSettings()
			if err == nil {
				t.Fatal("a UTF-16 settings file loaded")
			}
			if !strings.Contains(err.Error(), bomUTF16Refusal) {
				t.Errorf("error = %q, want it to say %q", err, bomUTF16Refusal)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error = %q, want it to name the file %s", err, path)
			}
		})
	}
}
