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
		tag, pointsAt string
		ok            bool
	}{
		{"v0.2.0", "  origin/release/v0.2\n", true},
		{"v0.2.0-rc.1", "  origin/release/v0.2\n", true},
		// Right after the cut the branch tip is also main's head.
		{"v0.1.0", "  origin/main\n  origin/release/v0.1\n", true},
		{"v0.2.1", "  origin/release/v0.2\n", true},
		// Another version's branch: v0.1 code under the name v0.2.0.
		{"v0.2.0", "  origin/release/v0.1\n", false},
		// A prefix is not the branch.
		{"v0.10.0", "  origin/release/v0.1\n", false},
		{"v0.1.0", "  origin/release/v0.10\n", false},
		// The head of main is no release: no count, no rehearsal.
		{"v0.2.0", "  origin/main\n", false},
		// Not the tip: the branch moved past the commit.
		{"v0.2.0", "", false},
		{"v0.2.0", "  origin/feat/release/v0.2\n", false},
		{"not-a-version", "  origin/release/v0.2\n", false},
	} {
		err := TagOnReleaseBranch(c.tag, c.pointsAt)
		if (err == nil) != c.ok {
			t.Errorf("TagOnReleaseBranch(%q, %q) = %v, want ok %v", c.tag, c.pointsAt, err, c.ok)
		}
		if err != nil && !strings.Contains(err.Error(), "release/") {
			t.Errorf("TagOnReleaseBranch(%q, %q): %q does not name the release branch it needs", c.tag, c.pointsAt, err)
		}
	}
}
