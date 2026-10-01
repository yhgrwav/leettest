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
	"testing"
	"time"
)

// Ground: contract — the chunk bounds how long a sleep can keep its P if the
// syscall is ever made raw, and how long a cancel waits; neither may grow
// unnoticed.
func TestSleepChunk_StaysShort(t *testing.T) {
	if sleepChunk > 50*time.Microsecond {
		t.Errorf("sleep chunk %v, want at most 50µs", sleepChunk)
	}
}
