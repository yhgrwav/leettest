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

package main

import "sync/atomic"

// stopReason holds who ended a search; the first to call stopFor wins.
type stopReason struct {
	why atomic.Pointer[string]
}

func (r *stopReason) stopFor(reason string) { r.why.Store(&reason) }

// get is the reason, or "" when nobody stopped the search.
func (r *stopReason) get() string {
	if why := r.why.Load(); why != nil {
		return *why
	}

	return ""
}
