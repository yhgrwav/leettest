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
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// The only dataset format is JSON Lines: a file of one JSON request a line.
// There is no CSV header whose first name could carry the mark; the same
// silent difference here would be a first record that is not the one written,
// so the first record is compared byte for byte.

const datasetLines = "{\"id\":1}\n{\"id\":2}\r\n\r\n{\"id\":3}\n"

func datasetRecords(t *testing.T, content string) (*config.MasterConfig, error) {
	t.Helper()

	return loadDataset(t, t.TempDir(), "      dataset: users.jsonl\n", map[string]string{"users.jsonl": content})
}

// Guard (the dataset reader strips the mark today; this test keeps it so): a
// dataset with a leading mark gives the records of the one without, the same
// lines, the first without the mark.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetBOMGuardRecordsEqualTheBOMLessOnes(t *testing.T) {
	plain, err := datasetRecords(t, datasetLines)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	marked, err := datasetRecords(t, utf8BOM+datasetLines)
	if err != nil {
		t.Fatalf("a dataset with a leading mark: %v", err)
	}

	want, got := plain.Load.Calls[0].Records, marked.Load.Calls[0].Records
	if len(got) != len(want) {
		t.Fatalf("records = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i].Line != want[i].Line || string(got[i].JSON) != string(want[i].JSON) {
			t.Errorf("record %d = line %d %q, want line %d %q", i, got[i].Line, got[i].JSON, want[i].Line, want[i].JSON)
		}
	}

	if first := got[0].JSON; strings.HasPrefix(string(first), utf8BOM) {
		t.Errorf("the first record carries the mark: % x", []byte(first))
	}

	if sum := marked.Load.Calls[0].DatasetSHA256; sum != sha256Hex(utf8BOM+datasetLines) {
		t.Errorf("DatasetSHA256 = %s, want %s: the file as it lies on disk, the mark included", sum, sha256Hex(utf8BOM+datasetLines))
	}
}

// Guard: an invalid line is named with the same line number and words with the
// mark at the start of the file as without it.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetBOMGuardAnInvalidLineIsTheSameError(t *testing.T) {
	const content = "{\"id\":1}\n{\"id\":2}\nnot json\n"

	_, plainErr := datasetRecords(t, content)
	_, markedErr := datasetRecords(t, utf8BOM+content)

	if plainErr == nil || markedErr == nil || markedErr.Error() != plainErr.Error() {
		t.Errorf("error with a mark: %v\nerror without: %v", markedErr, plainErr)
	}
}

// Guard: a file of only the mark has no requests, as an empty one has none.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetBOMGuardAFileOfOnlyTheMarkHasNoRequests(t *testing.T) {
	_, plainErr := datasetRecords(t, "")
	_, markedErr := datasetRecords(t, utf8BOM)

	if plainErr == nil || markedErr == nil || markedErr.Error() != plainErr.Error() {
		t.Errorf("error with only a mark: %v\nerror of an empty file: %v", markedErr, plainErr)
	}
}

// Guard: a mark that is not at the start is data, so the line is not JSON, and
// a second mark after the first one stays too.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_DatasetBOMGuardOnlyTheLeadingMarkIsStripped(t *testing.T) {
	for name, content := range map[string]string{
		"in the middle":      "{\"id\":1}\n" + utf8BOM + "{\"id\":2}\n",
		"leading and middle": utf8BOM + "{\"id\":1}\n" + utf8BOM + "{\"id\":2}\n",
		"two leading":        utf8BOM + utf8BOM + "{\"id\":1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := datasetRecords(t, content)
			if err == nil || !strings.Contains(err.Error(), "not JSON") {
				t.Errorf("error = %v, want a line named as not JSON", err)
			}
		})
	}
}

// A UTF-16 dataset is refused, naming the file as the config wrote it. Today
// its first line is "not JSON", which sends the user looking at a file that
// looks fine in the editor.
// Ground: contract — pkg/config is a library API; callers get this without our CLI.
func TestLoad_AUTF16DatasetIsRefused(t *testing.T) {
	for name, content := range map[string]string{
		"little endian":     utf16LE(datasetLines),
		"big endian":        utf16BE(datasetLines),
		"little endian bom": utf16LE(""),
		"big endian bom":    utf16BE(""),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := datasetRecords(t, content)
			if err == nil {
				t.Fatal("a UTF-16 dataset loaded")
			}
			if !strings.Contains(err.Error(), utf16Refusal) {
				t.Errorf("error = %q, want it to say %q", err, utf16Refusal)
			}
			if !strings.Contains(err.Error(), "users.jsonl") {
				t.Errorf("error = %q, want it to name users.jsonl", err)
			}
		})
	}
}
