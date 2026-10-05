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
	"fmt"
	"regexp"
	"strings"
)

var versionTag = regexp.MustCompile(`^v(\d+)\.(\d+)\.\d+(-rc\.\d+)?$`)

// TagOnReleaseBranch checks tag vX.Y.Z[-rc.N] against the output of
// `git branch -r --points-at <tag commit>`: the commit must be the tip of
// origin/release/vX.Y, the branch whose count and rehearsal ran. Being merely
// contained in a release branch is not enough: a later branch contains every
// older commit.
func TagOnReleaseBranch(tag, pointsAt string) error {
	m := versionTag.FindStringSubmatch(tag)
	if m == nil {
		return fmt.Errorf("tag %q is not vX.Y.Z[-rc.N]: a tag goes on the tip of origin/release/vX.Y", tag)
	}
	want := "origin/release/v" + m[1] + "." + m[2]
	for _, line := range strings.Split(pointsAt, "\n") {
		if strings.TrimSpace(line) == want {
			return nil
		}
	}
	return fmt.Errorf("tag %s must be the tip of %s; branches at its commit: %q", tag, want, strings.TrimSpace(pointsAt))
}
