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
	"testing"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// A library caller that has no labels passes none: the JSON still says an
// object, {} and not null. Ground: contract — "labels is always an object".
func TestPassportJSON_NoLabelsIsAnEmptyObject(t *testing.T) {
	run := jsonRun(engine.Report{})
	run.Run.Passport = NewPassport("0123456789abcdef", nil, nil)

	labels := field(t, writeJSON(t, run), "passport", "labels")

	obj, ok := labels.(map[string]any)
	if !ok || len(obj) != 0 {
		t.Errorf("labels = %#v, want an empty object", labels)
	}
}

// A report without a passport has no key, not a null one.
func TestPassportJSON_NoPassportLeavesTheKeyOut(t *testing.T) {
	out := writeJSON(t, jsonRun(engine.Report{}))

	if _, has := out["passport"]; has {
		t.Errorf("passport = %v, want the key left out", out["passport"])
	}
}
