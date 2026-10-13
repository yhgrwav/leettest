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

//go:build windows

package cli

import (
	"fmt"
	"syscall"
	"time"
)

// ProcessCPU is the CPU time the process has used so far, user and system.
// The clock behind it steps about 15.6 ms.
func ProcessCPU() (time.Duration, error) {
	self, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errCPUNotRead, err)
	}
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(self, &created, &exited, &kernel, &user); err != nil {
		return 0, fmt.Errorf("%w: %w", errCPUNotRead, err)
	}

	return filetimeDuration(kernel) + filetimeDuration(user), nil
}

// filetimeDuration reads a span in units of 100 ns.
func filetimeDuration(ft syscall.Filetime) time.Duration {
	return time.Duration(uint64(ft.HighDateTime)<<32|uint64(ft.LowDateTime)) * 100 * time.Nanosecond
}
