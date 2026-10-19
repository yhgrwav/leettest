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

// Package share is the one rule for a share of failed calls against a limit
// the user wrote as a percent: how it is read, which side of the limit a
// run's numbers fall on, and how the share is printed without landing on the
// wrong side of the limit. A pass threshold and a breaking-point search both
// decide by it.
package share
