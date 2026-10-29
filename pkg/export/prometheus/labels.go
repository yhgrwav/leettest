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

// CheckLabelName reports why name cannot be the name of a user's constant
// label, or nil. A name is valid when it matches [a-zA-Z_][a-zA-Z0-9_]*, does
// not start with "__" (Prometheus reserves it) and is not one of the labels
// the series bring themselves: method, category, cause, le. The error names
// the label.
//
// STUB (prometheus-export, layer 3): accepts everything.
func CheckLabelName(_ string) error {
	return nil
}
