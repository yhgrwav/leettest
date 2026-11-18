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
	"time"
)

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

// printMetadataRefresh writes the line under the settings echo; nothing when
// the run had no refresh. The error is the read's own text: a key, a file and
// a reason, never a value.
func printMetadataRefresh(w io.Writer, r *MetadataRefresh) {
	if r == nil {
		return
	}

	fmt.Fprintf(w, "metadata refresh: every %s, %d reads, %d changed, %d failed", r.Every, r.Reads, r.Changed, r.Failures)

	if r.Failures > 0 {
		if r.FailingAtEnd {
			fmt.Fprint(w, " (failing at the end)")
		}

		fmt.Fprintf(w, "; the last good value stayed in use; last error: %s", r.LastError)
	}

	fmt.Fprintln(w)
}

// jsonMetadataRefresh is MetadataRefresh as JSON has it: the interval in whole
// microseconds, last_error null until a read failed.
type jsonMetadataRefresh struct {
	IntervalUS   int64   `json:"interval_us"`
	Reads        int     `json:"reads"`
	Changed      int     `json:"changed"`
	Failures     int     `json:"failures"`
	FailingAtEnd bool    `json:"failing_at_end"`
	LastError    *string `json:"last_error"`
}

// metadataRefreshJSON is nil, which JSON writes as null, without a refresh.
func metadataRefreshJSON(r *MetadataRefresh) *jsonMetadataRefresh {
	if r == nil {
		return nil
	}

	out := &jsonMetadataRefresh{
		IntervalUS: micros(r.Every), Reads: r.Reads, Changed: r.Changed,
		Failures: r.Failures, FailingAtEnd: r.FailingAtEnd,
	}
	if r.LastError != "" {
		out.LastError = &r.LastError
	}

	return out
}
