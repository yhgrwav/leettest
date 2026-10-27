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

import "github.com/yhgrwav/leettest/pkg/spike"

// StopErrorSpike is the JSON stop_reason of a run that load.stop_on_errors
// ended.
const StopErrorSpike = "error_spike"

// jsonStop is where load.stop_on_errors ended a run, on whose numbers: the
// method, its failed and total calls over the last five seconds, the limit as
// written, and the time since the start in whole microseconds.
type jsonStop struct {
	Method string `json:"method"`
	Failed int    `json:"failed"`
	Total  int    `json:"total"`
	Limit  string `json:"limit"`
	AtUS   int64  `json:"at_us"`
}

// StopLine is the line printed when the run is stopped, and again in the
// report's notes: when, which method, its share to the digit that puts it
// over the limit, the counts, the rate calls were sent at and the number in
// flight.
func StopLine(_ spike.Trip) string {
	return ""
}
