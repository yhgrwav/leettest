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

package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DatasetRecord is one request of a dataset file: the JSON of its line as
// written, and the number of that line, from 1, blank lines counted.
type DatasetRecord struct {
	Line int
	JSON json.RawMessage
}

// byteOrderMark is what PowerShell writes at the start of a UTF-8 file.
var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// readDatasets reads the file of every call that names one, from dir when its
// path is relative. A dataset's error names the call and the path as the config
// wrote it, never the joined one and never the content of a line: it may be a
// secret.
func (m *MasterConfig) readDatasets(dir string) error {
	var errs []error

	for i := range m.Load.Calls {
		call := &m.Load.Calls[i]
		if call.RawDataset == nil {
			continue
		}

		path := call.Dataset
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}

		records, err := readDataset(path, call.Dataset)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: dataset %w", call.where(i), err))

			continue
		}
		call.Records = records
	}

	return errors.Join(errs...)
}

// readDataset reads the whole file at once, so that a line has no length limit,
// and splits it on line breaks. A byte order mark at the start and a carriage
// return at the end of a line are not part of a record; a line of blanks is
// skipped; every other line must be JSON, which is all that is checked here —
// its fit to the method's message needs the schema. The records stay bytes: no
// number passes through a float64. written is the path as the config has it.
func readDataset(path, written string) ([]DatasetRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			// Its text carries the joined path, which the user did not write.
			err = pathErr.Err
		}

		return nil, fmt.Errorf("%s: %w", written, err)
	}

	raw = bytes.TrimPrefix(raw, byteOrderMark)

	var records []DatasetRecord

	for n, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(bytes.Trim(line, " \t\r")) == 0 {
			continue
		}
		if !json.Valid(line) {
			return nil, fmt.Errorf("%s:%d: not JSON", written, n+1)
		}

		records = append(records, DatasetRecord{Line: n + 1, JSON: line[:len(line):len(line)]})
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("%s: no requests", written)
	}

	return records, nil
}
