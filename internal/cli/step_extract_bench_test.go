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
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// BenchmarkExtract_DecodeAndBuild is what a step that reads an extract costs the
// worker on top of the engine's iteration (BenchmarkIterate_Extract): decode the
// reply of the step before, take two values and build the next body from them,
// with the real dynamicpb decode and the template's slots. The cost is the next
// step's start lag, on the generator side. A number for the report, not a gate.
func BenchmarkExtract_DecodeAndBuild(b *testing.B) {
	call := attachWide(b, extractConfig(
		map[string]string{"x": "text_value", "n": "n32"},
		readerStep(map[string]any{"note": "w-{{extract.x}}-{{seq}}", "count": "{{extract.n}}"}),
	))

	reply := wideBody(b, func(set func(string, protoreflect.Value), _ func(string, protoreflect.Value)) {
		set("text_value", protoreflect.ValueOfString("wallet-1234"))
		set("n32", protoreflect.ValueOfInt32(98765))
	})
	vals := make([]string, call.Values)

	b.ReportAllocs()
	b.ResetTimer()

	for i := range b.N {
		if err := call.Extract(reply, vals); err != nil {
			b.Fatalf("Extract() = %v", err)
		}

		_ = call.Next[0].Build(uint64(i), vals) //nolint:gosec // i >= 0
	}
}
