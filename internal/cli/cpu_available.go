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
	"io/fs"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
)

var errNoQuota = errors.New("no cgroup CPU quota")

// AvailableCPU is AvailableCores of this process on this host.
func AvailableCPU() float64 {
	procs := runtime.GOMAXPROCS(0)
	if runtime.GOOS != "linux" {
		return float64(procs)
	}

	return AvailableCores(os.DirFS("/"), procs)
}

// AvailableCores is the CPU the process may use: gomaxprocs, or the cgroup
// quota when that is smaller. root is the file system the cgroup files are
// read from.
//
// The quota is found the way the Go runtime finds it (internal/runtime/cgroup):
// the process's own cgroup from proc/self/cgroup, its mount from
// proc/self/mountinfo, and in that cgroup alone cpu.max (cgroup v2) or
// cpu.cfs_quota_us and cpu.cfs_period_us (v1) - not in the groups above it.
// Anything unreadable, zero or negative is no quota. Then the share can come
// out low, by up to 2x: Go gives a process in a limited cgroup at least 2 Ps.
func AvailableCores(root fs.FS, gomaxprocs int) float64 {
	procs := float64(gomaxprocs)

	quota, err := cgroupQuota(root)
	if err != nil || quota <= 0 {
		return procs
	}

	return min(procs, quota)
}

// cgroupQuota is the CPU limit of the process's cgroup in cores.
func cgroupQuota(root fs.FS) (float64, error) {
	cgroups, err := fs.ReadFile(root, "proc/self/cgroup")
	if err != nil {
		return 0, err
	}
	version, group, err := cpuCgroup(string(cgroups))
	if err != nil {
		return 0, err
	}
	mounts, err := fs.ReadFile(root, "proc/self/mountinfo")
	if err != nil {
		return 0, err
	}
	dir, err := cpuMount(string(mounts), version, group)
	if err != nil {
		return 0, err
	}
	dir = strings.TrimPrefix(dir, "/")

	read := func(name string) (string, error) {
		raw, readErr := fs.ReadFile(root, path.Join(dir, name))

		return string(raw), readErr
	}
	if version == 1 {
		quota, quotaErr := read("cpu.cfs_quota_us")
		if quotaErr != nil {
			return 0, quotaErr
		}
		period, periodErr := read("cpu.cfs_period_us")
		if periodErr != nil {
			return 0, periodErr
		}

		return quotaOf(quota, period)
	}
	limit, err := read("cpu.max")
	if err != nil {
		return 0, err
	}
	quota, period, ok := strings.Cut(strings.TrimSuffix(limit, "\n"), " ")
	if !ok {
		return 0, errNoQuota
	}

	return quotaOf(quota, period)
}

// quotaOf is quota / period in cores; "max" and a quota under zero are no
// limit, as are zero and garbage.
func quotaOf(quota, period string) (float64, error) {
	q, err := strconv.ParseInt(strings.TrimSpace(quota), 10, 64)
	if err != nil || q <= 0 {
		return 0, errNoQuota
	}
	p, err := strconv.ParseInt(strings.TrimSpace(period), 10, 64)
	if err != nil || p <= 0 {
		return 0, errNoQuota
	}

	return float64(q) / float64(p), nil
}

// cpuCgroup is the process's cgroup that holds the CPU controller, and the
// version of its hierarchy. A v1 hierarchy with the cpu controller wins over
// the v2 one.
func cpuCgroup(proc string) (version int, group string, err error) {
	for line := range strings.Lines(proc) {
		parts := strings.SplitN(strings.TrimSuffix(line, "\n"), ":", 3)
		if len(parts) != 3 || !strings.HasPrefix(parts[2], "/") {
			return 0, "", errNoQuota
		}
		switch {
		case parts[0] == "0":
			version, group = 2, parts[2]
		case hasCPU(parts[1]):
			return 1, parts[2], nil
		}
	}
	if version == 0 {
		return 0, "", errNoQuota
	}

	return version, group, nil
}

// hasCPU says whether the comma-separated controllers list has "cpu".
func hasCPU(controllers string) bool {
	for _, c := range strings.Split(controllers, ",") {
		if c == "cpu" {
			return true
		}
	}

	return false
}

// cpuMount is where the cgroup is in the file system: the mount point of the
// first cgroup mount that reaches it, and the cgroup's path below that mount's
// own root. When the mount is rooted in the cgroup itself, as in a container
// with the host's cgroup namespace, the files are on the mount point.
func cpuMount(mountinfo string, version int, group string) (string, error) {
	want := "cgroup2"
	if version == 1 {
		want = "cgroup"
	}

	for line := range strings.Lines(mountinfo) {
		fields := strings.Fields(line)
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i

				break
			}
		}
		// root and mount point are fields 4 and 5, the file system type and
		// its super options follow the separator.
		if len(fields) < 6 || sep < 6 || len(fields) < sep+4 || fields[sep+1] != want {
			continue
		}
		if version == 1 && !hasCPU(fields[sep+3]) {
			continue
		}
		root, mount := unescapeMount(fields[3]), unescapeMount(fields[4])
		if !strings.HasPrefix(root, "/") || !hasPathPrefix(group, root) {
			continue
		}

		rel := group[len(root):]
		if root == "/" {
			rel = group
		}
		if hasPathPrefix(rel, "/..") {
			continue
		}

		return path.Join(mount, rel), nil
	}

	return "", errNoQuota
}

// hasPathPrefix says whether p is prefix or lies below it, on a path boundary.
func hasPathPrefix(p, prefix string) bool {
	if prefix == "/" {
		return true
	}

	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

// unescapeMount undoes the octal escapes /proc/self/mountinfo puts in a path
// (\040 for a space).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}

	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3

				continue
			}
		}
		b.WriteByte(s[i])
	}

	return b.String()
}
