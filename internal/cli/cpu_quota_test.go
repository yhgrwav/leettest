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
	"testing/fstest"
)

// Ground: decisions «Run passport» (Р5) — the passport's cpu_quota is the
// limit the cgroup sets, found the way AvailableCores finds it, but not cut to
// GOMAXPROCS: a run with a quota of 8 on 4 Ps and a run with a quota of 4 are
// different machines, and null is "no limit", never a number.
func TestCPUQuota_IsTheCgroupLimitOrNothing(t *testing.T) {
	v2 := func(max string) map[string]string {
		return map[string]string{
			"proc/self/cgroup":      "0::/\n",
			"proc/self/mountinfo":   mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max": max,
		}
	}

	for _, tc := range []struct {
		name  string
		files map[string]string
		want  float64 // 0: no quota, nil
	}{
		{"v2", v2("150000 100000\n"), 1.5},
		{"v2 above any GOMAXPROCS", v2("800000 100000\n"), 8},
		{"v2 under one core", v2("50000 100000\n"), 0.5},
		{"v1", map[string]string{
			"proc/self/cgroup":                            "11:cpu,cpuacct:/\n",
			"proc/self/mountinfo":                         mountLine(v1Mount, "/"),
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us":  "200000\n",
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us": "100000\n",
		}, 2},
		{"v2 in a leaf, root says no limit", map[string]string{
			"proc/self/cgroup":                             "0::/system.slice/x.service\n",
			"proc/self/mountinfo":                          mountLine(v2Mount, "/"),
			"sys/fs/cgroup/cpu.max":                        "max 100000\n",
			"sys/fs/cgroup/system.slice/x.service/cpu.max": "100000 100000\n",
		}, 1},
		{"v2 max", v2("max 100000\n"), 0},
		{"v2 garbage", v2("lots of cpu\n"), 0},
		{"v2 zero quota", v2("0 100000\n"), 0},
		{"v2 zero period", v2("100000 0\n"), 0},
		{"v1 minus one", map[string]string{
			"proc/self/cgroup":                            "11:cpu,cpuacct:/\n",
			"proc/self/mountinfo":                         mountLine(v1Mount, "/"),
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us":  "-1\n",
			"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us": "100000\n",
		}, 0},
		{"no cgroup files at all", map[string]string{}, 0},
	} {
		root := fstest.MapFS{}
		for name, body := range tc.files {
			root[name] = &fstest.MapFile{Data: []byte(body)}
		}

		got := CPUQuota(root)
		switch {
		case tc.want == 0 && got != nil:
			t.Errorf("%s: quota = %v, want nil", tc.name, *got)
		case tc.want != 0 && (got == nil || *got != tc.want):
			t.Errorf("%s: quota = %v, want %v", tc.name, got, tc.want)
		}
	}
}
