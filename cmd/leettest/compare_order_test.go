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

package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/share"
)

// Ground: contract — when a call breaks several checks, the first failed check
// in the order of runCompare speaks.
func TestCompare_TheFirstFailedCheckSpeaks(t *testing.T) {
	dir := t.TempDir()
	runPath := okRun().write(t, dir, "run.json")
	searchPath := brokeAt(1000).write(t, dir, "search.json")
	missing := filepath.Join(dir, "missing.json")

	_, badPercent := share.Parse("10", "-max-p99-rise")
	if badPercent == nil {
		t.Fatal("share.Parse accepted 10 for -max-p99-rise")
	}

	for _, tc := range []struct {
		name   string
		args   []string
		exact  string
		prefix string
		has    []string
		hasNot []string
	}{
		{
			"a bad -output before the one file",
			[]string{"-output", "xml", runPath},
			`compare: -output "xml": want text or json`, "", nil, nil,
		},
		{
			"one file before a bad percent",
			[]string{"-max-p99-rise", "10", runPath},
			"compare: want two report files, base then new", "", nil, nil,
		},
		{
			"a bad percent before the files are read",
			[]string{"-max-p99-rise", "10", missing, runPath},
			"", "", []string{badPercent.Error()}, []string{missing},
		},
		{
			"a missing file before a limit of the other kind",
			[]string{"-max-p99-rise", "20%", missing, searchPath},
			"", "compare: ", []string{missing}, []string{"applies to plain runs"},
		},
		{
			"two kinds before a limit of the other kind",
			[]string{"-max-p99-rise", "20%", searchPath, runPath},
			"", "", []string{"compare reads two of one kind"}, []string{"applies to plain runs"},
		},
	} {
		res := runCLI(t.Context(), t, 10*time.Second, append([]string{"compare"}, tc.args...)...)

		wantExit(t, res, 1)

		if res.stdout != "" {
			t.Errorf("%s: stdout %q, want nothing", tc.name, res.stdout)
		}

		if res.err == nil {
			t.Errorf("%s: no error", tc.name)

			continue
		}

		msg := res.err.Error()
		if tc.exact != "" && msg != tc.exact {
			t.Errorf("%s: error %q, want %q", tc.name, msg, tc.exact)
		}
		if !strings.HasPrefix(msg, tc.prefix) {
			t.Errorf("%s: error %q, want it to start with %q", tc.name, msg, tc.prefix)
		}
		for _, has := range tc.has {
			if !strings.Contains(msg, has) {
				t.Errorf("%s: error %q, want it to say %q", tc.name, msg, has)
			}
		}
		for _, hasNot := range tc.hasNot {
			if strings.Contains(msg, hasNot) {
				t.Errorf("%s: error %q, want it not to say %q", tc.name, msg, hasNot)
			}
		}
	}
}
