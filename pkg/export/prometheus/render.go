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

package prometheus

import "io"

// Render writes v in the text exposition format 0.0.4: nine metric families
// in a fixed order, each with its HELP and TYPE lines, and methods in the
// order of v.Methods. A series' own labels (method, category or cause, le)
// come first and the user's labels after them, sorted by name.
//
// STUB (prometheus-export, layer 3): writes nothing.
func Render(_ io.Writer, _ View) error {
	return nil
}
