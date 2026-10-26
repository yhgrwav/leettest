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

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
)

// pendingJSONFile is the path of the file created for -json-file and still
// empty. Whoever leaves the process without a report removes it: the run's
// defer, or exitNow, which skips every defer.
var pendingJSONFile atomic.Pointer[string]

// reserveJSONFile creates the empty file before anything is sent: a path that
// exists, or cannot be created, ends the command before the target sees a
// call, and a repeated command never meets half of an earlier one.
func reserveJSONFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("-json-file %s: exists; remove it or choose another path", path)
		}

		return fmt.Errorf("-json-file: %w", err)
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(path)

		return fmt.Errorf("-json-file %s: %w", path, err)
	}

	pendingJSONFile.Store(&path)

	return nil
}

// discardJSONFile removes the reserved file unless a report was saved into it.
func discardJSONFile() {
	if p := pendingJSONFile.Swap(nil); p != nil {
		_ = os.Remove(*p)
	}
}

// encodeJSON runs a writer of the report once into memory.
func encodeJSON(write func(io.Writer) error) ([]byte, error) {
	var buf bytes.Buffer
	if err := write(&buf); err != nil {
		return nil, fmt.Errorf("write the JSON report: %w", err)
	}

	return buf.Bytes(), nil
}

// saveJSONFile puts the report into the reserved file. Nothing to save, or no
// file asked for, is not an error: the reserved file is removed by
// discardJSONFile.
func saveJSONFile(path string, data []byte) error {
	if path == "" || data == nil {
		return nil
	}
	if err := writeJSONFile(path, data); err != nil {
		return fmt.Errorf("write the JSON report to %s: %w", path, err)
	}

	pendingJSONFile.Store(nil)

	return nil
}

// writeJSONFile is replaced by tests to fail the write.
var writeJSONFile = writeFileAtomic

// writeFileAtomic writes next to path and renames over it, so a reader sees
// the old empty file or the whole report. The temporary name is unique: a
// fixed one could exist already.
func writeFileAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if fillErr := fillTemp(tmp, path, data); fillErr != nil {
		return fillErr
	}

	return os.Rename(tmp.Name(), path)
}

// fillTemp writes data, gives it the mode of the reserved file, makes it
// durable and closes it.
func fillTemp(tmp *os.File, path string, data []byte) error {
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	reserved, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := tmp.Chmod(reserved.Mode().Perm()); err != nil {
		return err
	}

	return tmp.Close()
}
