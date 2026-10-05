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

package stressplan

import (
	"strings"
	"testing"
)

func TestTagOnReleaseBranch(t *testing.T) {
	for _, c := range []struct {
		contains string
		ok       bool
	}{
		{"  origin/release/v0.2\n", true},
		{"  origin/main\n  origin/release/v0.2\n", true},
		// The head of main is not a release: its count and rehearsal ran nowhere.
		{"  origin/main\n", false},
		{"", false},
		// A branch that only looks like one.
		{"  origin/feat/release/x\n  origin/release-notes\n", false},
	} {
		err := TagOnReleaseBranch(c.contains)
		if (err == nil) != c.ok {
			t.Errorf("TagOnReleaseBranch(%q) = %v, want ok %v", c.contains, err, c.ok)
		}
		if err != nil && !strings.Contains(err.Error(), "release/") {
			t.Errorf("TagOnReleaseBranch(%q): %q does not say a release/* branch is needed", c.contains, err)
		}
	}
}
