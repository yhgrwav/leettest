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

import "time"

// MetadataRefresh is what the metadata refresh did during a run or a search:
// the interval app.metadata_refresh set and the sender's counters at the end.
// A report without refresh carries nil.
type MetadataRefresh struct {
	Every                    time.Duration
	Reads, Changed, Failures int
	// FailingAtEnd is true when the last read failed.
	FailingAtEnd bool
	// LastError is the text of the last failed read; it stays after a recovery.
	LastError string
}
