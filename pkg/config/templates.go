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

import "errors"

var (
	// ErrTemplatesValue is a templates that is not true or false, or has no value.
	ErrTemplatesValue = errors.New("templates must be true or false")
	// ErrTemplatesNoPlaceholders is templates: true on a call whose data has none.
	ErrTemplatesNoPlaceholders = errors.New("templates: true without placeholders in data")
	// ErrTemplatesUnknown is a {{ in data with templates on that is not {{uuid}} or {{seq}}.
	ErrTemplatesUnknown = errors.New("unknown placeholder in data")
	// ErrTemplatesKey is a {{ in a key of data with templates on.
	ErrTemplatesKey = errors.New("placeholder in a key of data")
	// ErrTemplatesWithDataset is templates: true on a call with a dataset.
	ErrTemplatesWithDataset = errors.New("templates work with data only")
)

// TemplatesOn says the call's data is a template.
func (c Call) TemplatesOn() bool {
	return false
}

// TemplatePlaceholders are the placeholders the call's data uses, sorted; nil
// when templates are off.
func (c Call) TemplatePlaceholders() []string {
	return nil
}
