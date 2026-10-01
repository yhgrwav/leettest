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

//go:build !linux

package engine

import (
	"context"
	"time"
)

// preciseSleep: no microsecond sleep here; the schedule keeps the Go timer.
// macOS (kqueue takes a nanosecond timeout) is not measured.
const preciseSleep = false

// sleepPrecisely spins to until; exactScheduleFor keeps it unused here.
func sleepPrecisely(ctx context.Context, until time.Time) error {
	for time.Now().Before(until) {
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	return nil
}
