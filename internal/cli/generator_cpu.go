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
	"time"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// CPUSample is one reading of the process's CPU time (user plus system) at
// the moment At.
type CPUSample struct {
	At  time.Time
	CPU time.Duration
}

// GeneratorCPU is how much of the CPU available to the process the generator
// used, laid on the run's second grid.
type GeneratorCPU struct {
	// AvailableCores is the smaller of GOMAXPROCS and the cgroup quota.
	AvailableCores float64
	// Share is the CPU time over the counted seconds divided by their wall
	// time and the available cores, rounded half-up to 2 decimals.
	Share float64
	// BusySeconds counts the counted seconds whose rounded share is 0.90 or
	// more; FirstBusy is the first of them, from the start of the run.
	BusySeconds int
	FirstBusy   *int
	// Seconds[i] is the share of second i from the start of the run, warm-up
	// included; nil where no pair covers the second.
	Seconds []*float64
	// Counted is how many seconds Share, BusySeconds and the report line
	// speak of.
	Counted int
}

// LayGeneratorCPU lays the samples on the run's second grid.
func LayGeneratorCPU(_ engine.Report, _ []CPUSample, _ float64) *GeneratorCPU {
	return nil
}

// CPUMeter records the process's CPU time beside a run.
type CPUMeter struct{}

// NewCPUMeter returns a meter that reads the CPU time with read and the
// moment with now, and ticks every interval once started.
func NewCPUMeter(_ func() (time.Duration, error), _ func() time.Time, _ time.Duration) *CPUMeter {
	return &CPUMeter{}
}

// Tick takes one sample. A failed read ends the measurement for good.
func (*CPUMeter) Tick() {}

// Start takes a sample and then one every interval until the returned stop,
// which takes the last one.
func (*CPUMeter) Start() (stop func()) {
	return func() {}
}

// Result lays the samples on the grid of the report, or nil when a read
// failed.
func (*CPUMeter) Result(_ engine.Report, _ float64) *GeneratorCPU {
	return nil
}

var errCPUNotRead = errors.New("process CPU time not read")

// ProcessCPU is the CPU time the process has used so far.
func ProcessCPU() (time.Duration, error) {
	return 0, errCPUNotRead
}

// AvailableCores is the CPU the process may use: GOMAXPROCS, or the cgroup
// quota when that is smaller. root is the file system the cgroup files are
// read from.
func AvailableCores(_ fs.FS, _ int) float64 {
	return 0
}

// AvailableCPU is AvailableCores of this process.
func AvailableCPU() float64 {
	return 0
}
