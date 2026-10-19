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
	"errors"
	"fmt"
	"maps"
	"slices"
	"unicode"
	"unicode/utf8"
)

const (
	maxLabelKey   = 64
	maxLabelValue = 256
)

// CheckLabel is the rule for a label, wherever it is written: the key is 1 to
// 64 characters of A-Z a-z 0-9 _ . -, the value is UTF-8 of at most 256 bytes
// without control characters, and may be empty. The error says what is wrong
// and leaves it to the caller to say where the label was written.
func CheckLabel(key, value string) error {
	if key == "" || len(key) > maxLabelKey {
		return fmt.Errorf("the key must be 1 to %d characters", maxLabelKey)
	}
	for i := range len(key) {
		if !labelKeyByte(key[i]) {
			return errors.New("the key may have only letters, digits, _, . and -")
		}
	}
	if len(value) > maxLabelValue {
		return fmt.Errorf("the value is %d bytes, at most %d", len(value), maxLabelValue)
	}
	if !utf8.ValidString(value) {
		return errors.New("the value is not UTF-8")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return errors.New("the value has a control character")
		}
	}

	return nil
}

func labelKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '.' || c == '-'
}

// resolveLabels checks labels: and fills Labels. A value must be a YAML
// string, quoted when it looks like a number, a bool or null: the parser would
// spell 1.10 as 1.1 and the report must not say a version the user did not write.
func (m *MasterConfig) resolveLabels() error {
	m.Labels = make(map[string]string, len(m.RawLabels))

	var errs []error
	for _, key := range slices.Sorted(maps.Keys(m.RawLabels)) {
		value, isString := m.RawLabels[key].(string)
		if !isString {
			errs = append(errs, fmt.Errorf("%w: labels.%s must be a string: put the value in quotes", ErrInvalidLabel, key))

			continue
		}
		if err := CheckLabel(key, value); err != nil {
			errs = append(errs, fmt.Errorf("%w: labels.%s: %w", ErrInvalidLabel, key, err))

			continue
		}
		m.Labels[key] = value
	}

	return errors.Join(errs...)
}
