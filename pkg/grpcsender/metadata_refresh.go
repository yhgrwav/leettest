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

package grpcsender

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// ErrMetadataRefresh is a MetadataRefresh that cannot work: Every not above
// zero, or no Read. Connect fails with it.
var ErrMetadataRefresh = errors.New("invalid metadata refresh")

// refreshCloseWait is how long Close waits for the refresh goroutine to end: a
// Read stuck on a hung file system must not hang Close after the report.
const refreshCloseWait = time.Second

// MetadataRefresh makes the sender read Options.Metadata again during the run.
type MetadataRefresh struct {
	// Every is the interval between reads, above zero. The first read is Every
	// after Connect: Options.Metadata is the first value.
	Every time.Duration
	// Read returns the whole map, lowercase keys, a fresh map each call. It
	// must not return values in its error.
	Read func() (map[string]string, error)
}

// RefreshStats counts what the refresh did since Connect.
type RefreshStats struct {
	// Reads are the attempts after the start; Changed the successful ones that
	// gave a map different from the published one; Failures the unsuccessful.
	Reads, Changed, Failures int
	// FailingAtEnd is true when the last attempt failed.
	FailingAtEnd bool
	// LastError is the text of the last failure; it stays after a recovery.
	LastError string
}

// MetadataRefresh reports what the refresh has done so far; false when the
// sender has none. Safe to call during the run.
func (s *Sender) MetadataRefresh() (RefreshStats, bool) {
	return RefreshStats{}, false
}

// dynamicMetadata is the metadata of every call when it can be replaced: one
// object for all connections, the published map never changed.
type dynamicMetadata struct {
	p *atomic.Pointer[map[string]string]
}

func (d *dynamicMetadata) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return nil, nil
}

// RequireTransportSecurity is false for the same reason as staticMetadata's.
func (*dynamicMetadata) RequireTransportSecurity() bool { return false }
