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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

var updateFrames = flag.Bool("update-frames", false, "rewrite testdata/frames from the current screen")

// frameStates are the plain run's screen in each state it has, set on the
// model the way the run sets it.
func frameStates(t *testing.T) []struct {
	name string
	set  func(m *model)
} {
	t.Helper()

	running := func(m *model) {
		m.snapshot = engine.Snapshot{
			Elapsed: 12 * time.Second, Total: 30 * time.Second, Sent: 500, Failed: 3, InFlight: 4, RPS: 50,
			P50: exact(10), P90: exact(20), P99: exact(95),
			Methods: []engine.MethodSnapshot{
				{Method: "pkg.Svc/One", Sent: 300, Failed: 2},
				{Method: "pkg.Svc/Two", Sent: 200, Failed: 1},
			},
		}
		h := &history{}
		h.push(10, exact(10), exact(20), exact(95))
		h.push(12, exact(11), exact(22), exact(30))
		m.overall = *h
	}

	return []struct {
		name string
		set  func(m *model)
	}{
		{"warmup", func(m *model) {
			m.warmup = 5 * time.Second
			m.snapshot = engine.Snapshot{Elapsed: 2 * time.Second, Total: 30 * time.Second, Warmup: 5 * time.Second, WarmupSent: 40, InFlight: 3}
		}},
		{"running", running},
		{"help", func(m *model) { running(m); m.showHelp = true }},
		{"stopping", func(m *model) { running(m); m.stop() }},
		{"done", func(m *model) { running(m); m.report = widestReport(); m.done = true }},
		{"settings", func(m *model) { running(m); m.active = m.settingsTab() }},
		{"settings-editing", func(m *model) { running(m); m.active = m.settingsTab(); m.editing = true }},
	}
}

// Ground: contract — the plain run's screen as it is before the view.go
// refactor (verdict 2026-10-03): every state, every tab, widths 40, 80 and
// 120, byte for byte. A change here is a change of the screen, never a side
// effect of moving code. -update-frames rewrites the files.
func TestFrames_ThePlainRunsScreenIsUnchanged(t *testing.T) {
	for _, st := range frameStates(t) {
		m := testModel(t)
		m.frame = 0
		// The settings tab shows the file; the test's temporary one differs
		// per run and per system.
		m.settings.path = "/config/leettest/settings.yaml"
		st.set(m)

		var out strings.Builder
		tabs := []int{m.active}
		if m.active == 0 && !m.done {
			tabs = tabs[:0]
			for tab := range m.tabs {
				tabs = append(tabs, tab)
			}
		}
		for _, tab := range tabs {
			m.active = tab
			for _, width := range []int{40, 80, 120} {
				m.width, m.height = width, 40
				fmt.Fprintf(&out, "=== tab %d, width %d\n%s\n", tab, width, m.View())
			}
		}

		path := filepath.Join("testdata", "frames", st.name+".txt")
		if *updateFrames {
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(out.String()), 0o600); err != nil {
				t.Fatal(err)
			}

			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run with -update-frames once)", st.name, err)
		}
		if got := out.String(); got != string(want) {
			t.Errorf("%s: the screen changed; first difference at byte %d", st.name, firstDiff(got, string(want)))
		}
	}
}

func firstDiff(a, b string) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}

	return min(len(a), len(b))
}
