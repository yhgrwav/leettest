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

// RowKeys is the key of the report row of every call and every step of calls:
// [i][0] is call i (the head of a scenario), [i][k+1] is calls[i].Next[k].
// A plain call's key is always its Method. A head or step of a scenario has
// its Method for a key when that method appears once among all calls and steps
// of the run, and "<method> (<scenario>, step <n>)" when it appears more than
// once, n counted from 1 with the head as step 1.
//
// Red-test stub: every key is the bare method.
func RowKeys(calls []Call) [][]string {
	keys := make([][]string, len(calls))
	for i := range calls {
		keys[i] = make([]string, 0, len(calls[i].Next)+1)
		keys[i] = append(keys[i], calls[i].Method)

		for k := range calls[i].Next {
			keys[i] = append(keys[i], calls[i].Next[k].Method)
		}
	}

	return keys
}
