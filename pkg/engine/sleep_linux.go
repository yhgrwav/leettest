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

package engine

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// preciseSleep: clock_nanosleep sleeps to a microsecond.
	preciseSleep = true
	// sleepChunk is the longest single microsecond sleep: a cancel waits
	// for it to end. It also bounds the harm of a RawSyscall here: a raw
	// call keeps the P for the whole sleep, and at 50µs that barely shows
	// (measured, #142), so the Syscall that hands the P on would go
	// unmissed. Raise the chunk and a RawSyscall would hold the P for all of
	// it; TestSleepChunk_StaysShort keeps it at 50µs.
	sleepChunk = 50 * time.Microsecond
)

// sleepPrecisely sleeps to until with clock_nanosleep on CLOCK_MONOTONIC, the
// clock Go's monotonic time reads on Linux (runtime·nanotime1), in chunks of
// sleepChunk so that a cancel is seen. unix.ClockNanosleep is a Syscall, not a
// RawSyscall: the runtime hands the P on while it sleeps.
func sleepPrecisely(ctx context.Context, until time.Time) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		left := time.Until(until)
		if left <= 0 {
			return nil
		}

		var now unix.Timespec
		if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &now); err != nil {
			return err
		}
		wake := unix.NsecToTimespec(now.Nano() + int64(min(left, sleepChunk)))
		err := unix.ClockNanosleep(unix.CLOCK_MONOTONIC, unix.TIMER_ABSTIME, &wake, nil)
		if err != nil && !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}
