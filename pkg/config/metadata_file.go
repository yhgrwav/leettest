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
	"io"
	"io/fs"
	"maps"
	"os"
	"strings"
)

// FileValue is what one ${file:PATH} of app.metadata held when it was read:
// Path as written in the config, Value the file's content with the one
// trailing line break cut. The value is a secret: it is for the caller that
// redacts it, never for a message.
type FileValue struct {
	Path, Value string
}

const (
	fileOpen = "${file:"

	// maxMetadataFile is the most a file may hold; one byte more is read to see
	// that it holds more, so a path to /dev/zero ends the read at once.
	maxMetadataFile = 64 << 10
)

// valuePart is a piece of a metadata value: literal text (${NAME} already
// filled from the environment), or the reference ${file:path}.
type valuePart struct {
	text string
	// path is the reference as written, "" for literal text; from is where it
	// is read: path, or path under the directory of the config.
	path, from string
}

// fileHeader is a header whose value reads at least one file.
type fileHeader struct {
	key   string
	parts []valuePart
}

// splitFileRefs cuts value into literal text and ${file:PATH} references. A
// reference is read up to the first '}', so the path takes no '$' or '{'.
// "$${file:" is the text "${file:". The problem, when there is one, is the
// text that follows the key in the error.
func splitFileRefs(value string) (parts []valuePart, problem string) {
	var lit strings.Builder

	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, valuePart{text: lit.String()})
			lit.Reset()
		}
	}

	rest := value

	for {
		i := strings.Index(rest, fileOpen)
		if i < 0 {
			lit.WriteString(rest)

			break
		}

		if i > 0 && rest[i-1] == '$' {
			lit.WriteString(rest[:i-1])
			lit.WriteString(fileOpen)

			rest = rest[i+len(fileOpen):]

			continue
		}

		body := rest[i+len(fileOpen):]

		end := strings.IndexByte(body, '}')
		if end < 0 {
			return nil, fileOpen + " is not closed: write the path and a closing }"
		}

		if end == 0 {
			return nil, fileOpen + "} needs a path"
		}

		path := body[:end]
		if strings.ContainsAny(path, "${") {
			return nil, fmt.Sprintf("file path %q must not contain '$' or '{': paths are taken as written", path)
		}

		lit.WriteString(rest[:i])
		flush()

		parts = append(parts, valuePart{path: path, from: path})
		rest = body[end+1:]
	}

	flush()

	return parts, ""
}

// HasMetadata reports whether app.metadata has any header, one that reads a
// file included.
func (a *App) HasMetadata() bool {
	return len(a.Metadata) > 0 || len(a.fileHeaders) > 0
}

// ReadMetadata reads every ${file:PATH} of app.metadata now and returns a new
// map of all headers (Metadata and the assembled ones) and the values read.
// Without a file reference it is a copy of Metadata and reads nothing.
// Headers are read by ascending key and the first failure is returned, so the
// same files fail with the same message; no value is part of an error.
func (a *App) ReadMetadata() (map[string]string, []FileValue, error) {
	out := make(map[string]string, len(a.Metadata)+len(a.fileHeaders))
	maps.Copy(out, a.Metadata)

	var files []FileValue

	for _, h := range a.fileHeaders {
		var value strings.Builder

		for _, p := range h.parts {
			if p.path == "" {
				value.WriteString(p.text)

				continue
			}

			content, reason := readMetadataFile(p.from)
			if reason != "" {
				return nil, nil, fmt.Errorf("%w: key %q: file %s: %s", ErrInvalidMetadata, h.key, p.path, reason)
			}

			files = append(files, FileValue{Path: p.path, Value: content})
			value.WriteString(content)
		}

		out[h.key] = value.String()
	}

	return out, files, nil
}

// readMetadataFile returns the value a file holds, or the reason it cannot be
// one. The reason carries neither the path nor the content.
func readMetadataFile(path string) (value, reason string) {
	info, err := os.Stat(path)
	if err != nil {
		return "", osReason(err)
	}

	if !info.Mode().IsRegular() {
		return "", "is not a regular file"
	}

	f, err := os.Open(path)
	if err != nil {
		return "", osReason(err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxMetadataFile+1))
	if err != nil {
		return "", osReason(err)
	}

	if len(data) > maxMetadataFile {
		return "", "is larger than 64 KiB"
	}

	text := string(data)
	if rest, cut := strings.CutSuffix(text, "\r\n"); cut {
		text = rest
	} else if rest, cut := strings.CutSuffix(text, "\n"); cut {
		text = rest
	}

	switch {
	case text == "":
		return "", "is empty"
	case strings.ContainsAny(text, "\r\n"):
		return "", "has a line break inside the value (one at the end is cut)"
	case !printableASCII(text):
		return "", "has a byte outside printable ASCII (%x20-%x7E)"
	}

	return text, ""
}

// osReason is the operating system's words for a failed open or read, without
// the path the error carries: the message names it once, as written.
func osReason(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}

	return err.Error()
}
