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
	"crypto/sha256"
	"encoding/hex"
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

// readDatasets reads the file of every call and every step that names one, from
// dir when its path is relative. A dataset's error names the call or the step and the path as the config
// wrote it, never the joined one and never the content of a line: it may be a
// secret.
func (m *MasterConfig) readDatasets(dir string) error {
	var errs []error

	for i := range m.Load.Calls {
		call := &m.Load.Calls[i]
		if call.RawDataset != nil {
			records, sum, err := readDataset(datasetPath(dir, call.Dataset), call.Dataset)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: dataset %w", call.where(i), err))
			} else {
				call.Records = records
				call.DatasetSHA256 = sum
			}
		}

		for k := range call.Steps {
			errs = append(errs, call.readStepDataset(dir, i, k)...)
		}
	}

	return errors.Join(errs...)
}

func datasetPath(dir, written string) string {
	if filepath.IsAbs(written) {
		return written
	}

	return filepath.Join(dir, written)
}

// readStepDataset reads the file of step k of call i, named by the step's path.
// The records of a file are not templated, so braces in one, in a step of a
// templated call that left templates out, are refused: the user may think they
// work. templates: false on the step says they are text.
func (c *Call) readStepDataset(dir string, i, k int) []error {
	step := &c.Steps[k]
	if step.RawDataset == nil {
		return nil
	}

	at := fmt.Sprintf("load.calls[%d].steps[%d]", i, k)

	records, sum, err := readDataset(datasetPath(dir, step.Dataset), step.Dataset)
	if err != nil {
		return []error{fmt.Errorf("%s: dataset %w", at, err)}
	}

	if c.TemplatesOn() && step.Templates == nil {
		for _, r := range records {
			if bytes.Contains(r.JSON, []byte("{{")) {
				return []error{fmt.Errorf("%w: %s.dataset: %s:%d: the record has {{ and the call has templates: true; "+
					"the records of a file are not templated; set templates: false on the step to send the braces as text",
					ErrScenario, at, step.Dataset, r.Line)}
			}
		}
	}

	step.Records = records
	step.DatasetSHA256 = sum

	return nil
}

// readDataset reads the whole file at once, so that a line has no length limit,
// and splits it on line breaks. A byte order mark at the start and a carriage
// return at the end of a line are not part of a record; a line of blanks is
// skipped; every other line must be JSON, which is all that is checked here —
// its fit to the method's message needs the schema. The records stay bytes: no
// number passes through a float64. written is the path as the config has it.
// sum is the sha256 of the file as it is on disk, a byte order mark included,
// in hex: what sha256sum prints.
func readDataset(path, written string) (records []DatasetRecord, sum string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			// Its text carries the joined path, which the user did not write.
			err = pathErr.Err
		}

		return nil, "", fmt.Errorf("%s: %w", written, err)
	}

	digest := sha256.Sum256(raw)
	sum = hex.EncodeToString(digest[:])
	raw, err = UTF8Text(raw, written)
	if err != nil {
		return nil, "", err
	}

	for n, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(bytes.Trim(line, " \t\r")) == 0 {
			continue
		}
		if !json.Valid(line) {
			return nil, "", fmt.Errorf("%s:%d: not JSON", written, n+1)
		}

		records = append(records, DatasetRecord{Line: n + 1, JSON: line[:len(line):len(line)]})
	}

	if len(records) == 0 {
		return nil, "", fmt.Errorf("%s: no requests", written)
	}

	return records, sum, nil
}
