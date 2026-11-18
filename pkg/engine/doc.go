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

// Package engine generates load: it schedules requests at the configured
// stages and hands them to a worker pool, which sends each through a Sender;
// every Result is recorded in Stats. Stats keeps the per-method counts, the latency
// distributions (built on pkg/metrics), the per-second timeline and the time
// windows, and gives the run Report and the views a live screen and an export
// read. The engine knows nothing about gRPC: the Sender does.
package engine
