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

// Command compare-perf measures LeetTest, ghz and k6 with one ruler: the
// reference stand. Each tool runs in its own Docker container limited to one
// core, the stand in another, and every number is read from what the stand
// saw arrive and answer, not from what the tool says about itself, except the
// latencies row 3 compares against the stand's own hold time.
//
// Row 1 is the highest rate a tool holds, row 2 how evenly it spaces calls at
// a fixed rate, row 3 how much latency it adds to a stand that answers after
// a fixed delay. It needs Docker and Go, runs from the repository root and is
// not run in CI. -plan prints every command and setting without running any.
// Raw outputs go to -out, which is not committed.
package main
