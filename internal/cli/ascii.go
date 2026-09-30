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
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

const backslash = 0x5c

// asciiText returns s with every character outside printable ASCII, newline
// and tab written as a backslash, u and four lowercase hex digits; past U+FFFF
// as a UTF-16 surrogate pair, as JSON does. Control characters are escaped
// too, so text from the target cannot drive the terminal.
func asciiText(s string) string {
	clean := true
	for i := range len(s) {
		if !plainByte(s[i]) {
			clean = false

			break
		}
	}
	if clean {
		return s
	}

	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x80 && plainByte(byte(r)):
			b.WriteRune(r)
		case r > 0xffff:
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&b, "%cu%04x%cu%04x", backslash, hi, backslash, lo)
		default:
			fmt.Fprintf(&b, "%cu%04x", backslash, r)
		}
	}

	return b.String()
}

func plainByte(c byte) bool {
	return (c >= 0x20 && c < 0x7f) || c == '\n' || c == '\t'
}

// asciiWriter is the one way the text report reaches its writer: whatever a
// piece of the report prints, the output is ASCII. Each Write is escaped on
// its own, so a character split across two writes would come out as two
// replacement characters; fmt writes a whole formatted string at once.
type asciiWriter struct {
	w io.Writer
}

func (a asciiWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(a.w, asciiText(string(p))); err != nil {
		return 0, err
	}

	return len(p), nil
}
