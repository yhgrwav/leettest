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
	"errors"
	"fmt"
)

// ErrUTF16 says a file is UTF-16: it is refused, never decoded or guessed.
var ErrUTF16 = errors.New("file is UTF-16; save it as UTF-8")

// byteOrderMark is what Notepad and PowerShell write at the start of a UTF-8 file.
var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

// UTF8Text is the text of a user file read as bytes: raw without a single
// leading UTF-8 byte order mark. A mark anywhere else, or a second one right
// after the first, is data. A file that starts with a UTF-16 mark (FF FE or
// FE FF) is refused with ErrUTF16, and the error names the file as name. Call
// it once, at load; hash the file before it, not after.
func UTF8Text(raw []byte, name string) ([]byte, error) {
	if len(raw) >= 2 && (raw[0] == 0xFF && raw[1] == 0xFE || raw[0] == 0xFE && raw[1] == 0xFF) {
		return nil, fmt.Errorf("%s: %w", name, ErrUTF16)
	}

	if len(raw) >= len(byteOrderMark) && bytes.Equal(raw[:len(byteOrderMark)], byteOrderMark) {
		return raw[len(byteOrderMark):], nil
	}

	return raw, nil
}
