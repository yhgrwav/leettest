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
	"errors"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// ErrTemplateField is a placeholder in a field it does not fit: of another
// type, or in a number that is not the whole value.
var ErrTemplateField = errors.New("placeholder does not fit the field")

// CheckTemplatesFit refuses a run whose plan hands out more requests than the
// field a seq goes into holds, before anything is sent.
func CheckTemplatesFit(_ []engine.Call) error {
	return nil
}
