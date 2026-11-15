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
	"fmt"
	"io"
	"maps"
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// shortHashLen is how much of the config's hash the text report shows.
const shortHashLen = 12

// Passport is what a report says of where it came from: the config's bytes and
// the machine that generated the load. There is no host name in it: it is
// often a person's.
type Passport struct {
	// ConfigSHA256 is the sha256 of the config file's bytes, hex.
	ConfigSHA256 string
	OS           string
	Arch         string
	NumCPU       int
	GOMAXPROCS   int
	// CPUQuota is the cgroup's CPU limit in cores as the cgroup says it; nil
	// when there is none. A value that is not a positive finite number (0, NaN,
	// ±Inf, below 0) is read as none everywhere (usableQuota).
	CPUQuota  *float64
	GoVersion string
	// Labels are the user's key=value pairs, the config's and the flag's.
	Labels map[string]string
}

// usableQuota is the passport's CPU quota in cores; false for none and for a
// value no cgroup limit can be: 0, below 0, NaN, ±Inf. A library caller builds
// the passport and can hand any of them; they are no quota, never a limit.
func usableQuota(q *float64) (float64, bool) {
	if q == nil || !(*q > 0) || math.IsInf(*q, 0) {
		return 0, false
	}

	return *q, true
}

// quotaCoresText is a quota in cores as the passport prints it.
func quotaCoresText(q float64) string {
	return strconv.FormatFloat(q, 'f', -1, 64)
}

// NewPassport reads the machine now; configSHA256 and labels are the run's. A
// quota that is not usable (usableQuota) is stored as none.
func NewPassport(configSHA256 string, labels map[string]string, quota *float64) *Passport {
	if _, ok := usableQuota(quota); !ok {
		quota = nil
	}

	return &Passport{
		ConfigSHA256: configSHA256,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
		GOMAXPROCS:   runtime.GOMAXPROCS(0),
		CPUQuota:     quota,
		GoVersion:    runtime.Version(),
		Labels:       labels,
	}
}

// printPassport writes the machine line and, with labels, the labels line.
func printPassport(w io.Writer, p *Passport) {
	if p == nil {
		return
	}

	var line strings.Builder
	fmt.Fprintf(&line, "machine: %s/%s, %d CPU, GOMAXPROCS %d", p.OS, p.Arch, p.NumCPU, p.GOMAXPROCS)
	if q, ok := usableQuota(p.CPUQuota); ok {
		fmt.Fprintf(&line, ", quota %s", quotaCoresText(q))
	}
	fmt.Fprintf(&line, ", %s; config sha256:%s", p.GoVersion, p.ConfigSHA256[:min(shortHashLen, len(p.ConfigSHA256))])
	fmt.Fprintln(w, line.String())

	if len(p.Labels) == 0 {
		return
	}
	pairs := make([]string, 0, len(p.Labels))
	for _, k := range slices.Sorted(maps.Keys(p.Labels)) {
		pairs = append(pairs, k+"="+p.Labels[k])
	}
	fmt.Fprintf(w, "labels: %s\n", strings.Join(pairs, ", "))
}

// jsonPassport is Passport as JSON has it. Labels is always an object, its
// keys in alphabetical order.
type jsonPassport struct {
	ConfigSHA256 string `json:"config_sha256"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	NumCPU       int    `json:"num_cpu"`
	GOMAXPROCS   int    `json:"gomaxprocs"`
	// CPUQuota is the cgroup's CPU limit in cores; null without one.
	CPUQuota  *float64          `json:"cpu_quota"`
	GoVersion string            `json:"go_version"`
	Labels    map[string]string `json:"labels"`
}

// passportJSON is nil for a report without a passport (a library caller, a
// step of a search), which leaves the key out.
func passportJSON(p *Passport) *jsonPassport {
	if p == nil {
		return nil
	}
	labels := make(map[string]string, len(p.Labels))
	maps.Copy(labels, p.Labels)

	var quota *float64
	if q, ok := usableQuota(p.CPUQuota); ok {
		quota = &q
	}

	return &jsonPassport{
		ConfigSHA256: p.ConfigSHA256, OS: p.OS, Arch: p.Arch, NumCPU: p.NumCPU, GOMAXPROCS: p.GOMAXPROCS,
		CPUQuota: quota, GoVersion: p.GoVersion, Labels: labels,
	}
}
