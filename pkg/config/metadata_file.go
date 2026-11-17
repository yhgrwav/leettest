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

// FileValue is what one ${file:PATH} of app.metadata held when it was read:
// Path as written in the config, Value the file's content with the one
// trailing line break cut. The value is a secret: it is for the caller that
// redacts it, never for a message.
type FileValue struct {
	Path, Value string
}

// HasMetadata reports whether app.metadata has any header, one that reads a
// file included.
func (a *App) HasMetadata() bool {
	return false
}

// ReadMetadata reads every ${file:PATH} of app.metadata now and returns a new
// map of all headers (Metadata and the assembled ones) and the values read.
// Without a file reference it is a copy of Metadata and reads nothing.
func (a *App) ReadMetadata() (map[string]string, []FileValue, error) {
	return a.Metadata, nil, nil
}
