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
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

const datasetMethod = "wallet.v1.WalletService/GetBalance"

// datasetYAML is a config with one call; callLines are more fields of that call.
func datasetYAML(callLines string) string {
	return `
app:
  target:
    ip: localhost
    port: 50051
load:
  calls:
    - method: ` + datasetMethod + `
      rps: 10
      duration: 1s
` + callLines
}

// loadDataset writes a config and the files next to it, and loads the config
// by path: only LoadFile reads a dataset. files maps a path under the config's
// directory to its content.
func loadDataset(t *testing.T, dir, callLines string, files map[string]string) (*config.MasterConfig, error) {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cfgPath := filepath.Join(dir, "leettest.yaml")
	if err := os.WriteFile(cfgPath, []byte(datasetYAML(callLines)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return config.LoadFile(cfgPath)
}

func rawRecords(call config.Call) []string {
	out := make([]string, len(call.Records))
	for i, r := range call.Records {
		out[i] = string(r.JSON)
	}

	return out
}

// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetReadsEachLine(t *testing.T) {
	cfg, err := loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n",
		map[string]string{"users.jsonl": "{\"id\":1}\n{\"id\":2}\n{\"id\":3}\n"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := rawRecords(cfg.Load.Calls[0])
	if want := []string{`{"id":1}`, `{"id":2}`, `{"id":3}`}; !slices.Equal(got, want) {
		t.Errorf("records = %q, want %q", got, want)
	}
}

// A file with no newline after its last line is the same as one with it.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetLastLineNeedsNoNewline(t *testing.T) {
	cfg, err := loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n",
		map[string]string{"users.jsonl": "{\"id\":1}\n{\"id\":2}"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got, want := rawRecords(cfg.Load.Calls[0]), []string{`{"id":1}`, `{"id":2}`}; !slices.Equal(got, want) {
		t.Errorf("records = %q, want %q", got, want)
	}
}

// The path is the config file's, not the working directory's: the run is
// started from anywhere, and an absolute path is used as it is.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetPathRelativeToConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	dir := t.TempDir()
	cfg, err := loadDataset(t, dir, "      dataset: data/users.jsonl\n",
		map[string]string{"data/users.jsonl": "{\"id\":7}\n"})
	if err != nil {
		t.Fatalf("relative path: %v", err)
	}
	if got, want := rawRecords(cfg.Load.Calls[0]), []string{`{"id":7}`}; !slices.Equal(got, want) {
		t.Errorf("records = %q, want %q", got, want)
	}

	other := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	if writeErr := os.WriteFile(other, []byte("{\"id\":8}\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	cfg, err = loadDataset(t, t.TempDir(), "      dataset: '"+filepath.ToSlash(other)+"'\n", nil)
	if err != nil {
		t.Fatalf("absolute path: %v", err)
	}
	if got, want := rawRecords(cfg.Load.Calls[0]), []string{`{"id":8}`}; !slices.Equal(got, want) {
		t.Errorf("records = %q, want %q", got, want)
	}
}

// What the user wrote is what errors and the JSON report print: LoadFile joins
// the path to the config's directory only to open the file.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetPathAsWrittenKept(t *testing.T) {
	cfg, err := loadDataset(t, t.TempDir(), "      dataset: data/users.jsonl\n",
		map[string]string{"data/users.jsonl": "{}\n"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got := cfg.Load.Calls[0].Dataset; got != "data/users.jsonl" {
		t.Errorf("Dataset = %q, want the path as written", got)
	}
}

// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetAndDataIsAnError(t *testing.T) {
	want := "call 0 (" + datasetMethod + "): data and dataset are both set; use one"

	// The file does not exist: the clash is found before any file is read.
	_, err := loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n      data:\n        id: 1\n", nil)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("LoadFile error = %v, want it to say %q", err, want)
	}

	_, err = config.Parse([]byte(datasetYAML("      dataset: users.jsonl\n      data:\n        id: 1\n")))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Parse error = %v, want it to say %q", err, want)
	}
}

// A dataset that gives the run nothing to send is refused, whatever it holds
// instead: no records, only blank lines, a path that is empty.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetEmptyIsAnError(t *testing.T) {
	for name, content := range map[string]string{
		"an empty file":        "",
		"only blank lines":     "\n  \n\t\n\r\n",
		"only a byte order":    "\xEF\xBB\xBF",
		"a line break and BOM": "\xEF\xBB\xBF\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n", map[string]string{"users.jsonl": content})
			want := "call 0 (" + datasetMethod + "): dataset users.jsonl: no requests"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("error = %v, want it to say %q", err, want)
			}
		})
	}
}

// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestParse_DatasetEmptyPathIsAnError(t *testing.T) {
	_, err := config.Parse([]byte(datasetYAML("      dataset: \"\"\n")))
	if err == nil || !strings.Contains(err.Error(), datasetMethod) || !strings.Contains(err.Error(), "dataset") {
		t.Errorf("error = %v, want one naming the method and dataset", err)
	}
}

// The file is not there: the error names the path as written and carries the
// os error, so errors.Is(fs.ErrNotExist) holds, and it does not show the
// joined path.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetMissingFileNamesThePathAsWritten(t *testing.T) {
	dir := t.TempDir()
	_, err := loadDataset(t, dir, "      dataset: data/absent.jsonl\n", nil)
	if err == nil {
		t.Fatal("LoadFile accepted a dataset that is not there")
	}

	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error = %v, want the os error behind it (fs.ErrNotExist)", err)
	}
	if want := "call 0 (" + datasetMethod + "): dataset data/absent.jsonl: "; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to say %q", err, want)
	}
	if strings.Contains(err.Error(), dir) {
		t.Errorf("error = %q shows the joined path; the path as written is what the user can find", err)
	}
}

// A line that is not JSON is named by its line in the file, blank lines
// counted, and the error carries nothing of it: the line may hold a secret.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetNotJSONNamesItsLine(t *testing.T) {
	const secret = "hunter2-the-token"

	for name, content := range map[string]string{
		"a broken line after a blank one": "{\"id\":1}\n\n{\"id\": \"" + secret + "\n",
		"text":                            "{\"id\":1}\n   \nnot json " + secret + "\n",
		"two values on a line":            "{\"id\":1}\n\n{\"a\":\"" + secret + "\"} {\"b\":1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n", map[string]string{"users.jsonl": content})
			want := "call 0 (" + datasetMethod + "): dataset users.jsonl:3: not JSON"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want it to say %q", err, want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error %q holds the content of the line", err)
			}
		})
	}
}

// The first line wrong is the one named: the user fixes one and runs again.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetNotJSONNamesTheFirstBadLine(t *testing.T) {
	_, err := loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n",
		map[string]string{"users.jsonl": "{}\n{bad\n{}\n{worse\n"})
	if err == nil || !strings.Contains(err.Error(), "users.jsonl:2: not JSON") {
		t.Errorf("error = %v, want the first bad line, 2", err)
	}
}

// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetBlankLinesSkipped(t *testing.T) {
	cfg, err := loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n",
		map[string]string{"users.jsonl": "\n{\"id\":1}\n   \n\t\n{\"id\":2}\n\n"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	call := cfg.Load.Calls[0]
	if got, want := rawRecords(call), []string{`{"id":1}`, `{"id":2}`}; !slices.Equal(got, want) {
		t.Fatalf("records = %q, want %q", got, want)
	}
	// The line of a record is its place in the file, not among the records: a
	// later error names the line the user can go to.
	if call.Records[0].Line != 2 || call.Records[1].Line != 5 {
		t.Errorf("lines = %d and %d, want 2 and 5: 1-based, blank lines counted", call.Records[0].Line, call.Records[1].Line)
	}
}

// Windows editors write CRLF and PowerShell writes a byte order mark; neither
// is part of a record. A mark anywhere but the file's start is.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetCRLFAndBOM(t *testing.T) {
	cfg, err := loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n",
		map[string]string{"users.jsonl": "\xEF\xBB\xBF{\"id\":1}\r\n{\"id\":2}\r\n\r\n{\"id\":3}\r"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, want := rawRecords(cfg.Load.Calls[0]), []string{`{"id":1}`, `{"id":2}`, `{"id":3}`}; !slices.Equal(got, want) {
		t.Errorf("records = %q, want %q", got, want)
	}

	_, err = loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n",
		map[string]string{"users.jsonl": "{}\n\xEF\xBB\xBF{}\n"})
	if err == nil || !strings.Contains(err.Error(), "users.jsonl:2: not JSON") {
		t.Errorf("a mark on line 2: error = %v, want the line named as not JSON", err)
	}
}

// A request of 100 KB is legal and a line is not limited to a scanner's 64 KiB.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetLineOfOneMebibyte(t *testing.T) {
	line := `{"body":"` + strings.Repeat("a", 1<<20) + `"}`

	cfg, err := loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n",
		map[string]string{"users.jsonl": "{\"id\":1}\n" + line + "\n{\"id\":3}\n"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	recs := cfg.Load.Calls[0].Records
	if len(recs) != 3 {
		t.Fatalf("records = %d, want 3", len(recs))
	}
	if string(recs[1].JSON) != line {
		t.Errorf("the long record is %d bytes, want %d, unchanged", len(recs[1].JSON), len(line))
	}
	if string(recs[2].JSON) != `{"id":3}` {
		t.Errorf("the record after it = %q", recs[2].JSON)
	}
}

// A record is the line's bytes, not a decoded value: an integer above 2^53 and
// the spelling of the line arrive as written, as they do from data.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetRawBytesKept(t *testing.T) {
	const line = `{"id" :  9007199254740993, "ratio": 1.0, "name":"\u00e9"}`

	cfg, err := loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n", map[string]string{"users.jsonl": line + "\n"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got := rawRecords(cfg.Load.Calls[0]); len(got) != 1 || got[0] != line {
		t.Errorf("records = %q, want the line byte for byte", got)
	}
}

// Parse takes no file and no path apart: LoadFile calls it before it knows the
// config's directory, so a relative path must pass it. The path is kept as
// written and no record is there; a caller that built calls from this would be
// told so (internal/cli TestCalls_DatasetNotReadIsAnError).
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestParse_DatasetNotRead(t *testing.T) {
	for _, path := range []string{"data/users.jsonl", "/abs/users.jsonl", "C:/abs/users.jsonl"} {
		cfg, err := config.Parse([]byte(datasetYAML("      dataset: " + path + "\n")))
		if err != nil {
			t.Fatalf("%s: parse: %v", path, err)
		}

		call := cfg.Load.Calls[0]
		if call.Dataset != path {
			t.Errorf("Dataset = %q, want %q as written", call.Dataset, path)
		}
		if len(call.Records) != 0 {
			t.Errorf("%s: %d records after Parse, which reads no file", path, len(call.Records))
		}
	}
}
