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

// Package errstop watches a run for a rise in failures and says when to stop
// it: once a second it asks the engine how each method did over the last few
// whole seconds and trips when the share of failed calls passes the limit the
// user wrote as load.stop_on_errors. The engine does not know it: the watch
// only reads, through Source, and stopping is the caller's, in the callback.
// Off (no limit, or 100%) there is no watch at all: New returns nil, and the
// send loop is the same as without the feature.
package errstop
