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
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/engine"
)

// A library caller builds the passport (Passport is a plain struct): a CPU quota
// that is not a positive finite number of cores is no quota. The cgroup reader
// (CPUQuota) already gives nil for such a file; these are the values that
// reach the report some other way.
func unusableQuotas() map[string]float64 {
	return map[string]float64{
		"zero": 0, "negative": -1, "NaN": math.NaN(), "+Inf": math.Inf(1), "-Inf": math.Inf(-1),
	}
}

// Ground: contract — the JSON is valid JSON and cpu_quota is a number or null.
func TestPassportJSON_AnUnusableQuotaIsNullAndTheOutputIsValid(t *testing.T) {
	for name, q := range unusableQuotas() {
		t.Run(name, func(t *testing.T) {
			run := jsonRun(engine.Report{})
			run.Run.Passport = limitPassport("linux", 8, 8, quota(q))

			var buf bytes.Buffer
			if err := WriteJSON(&buf, run); err != nil {
				t.Fatalf("WriteJSON: %v (a quota that is no number must not break the report)", err)
			}
			for _, bad := range []string{"NaN", "Inf"} {
				if strings.Contains(buf.String(), bad) {
					t.Errorf("the JSON has %q:\n%s", bad, buf.String())
				}
			}

			if got := passportJSON(run.Run.Passport).CPUQuota; got != nil {
				t.Errorf("passportJSON CPUQuota = %v, want nil (null)", *got)
			}
		})
	}
}

// A quota that is a number of cores stays as it is, fractions included.
func TestPassportJSON_AUsableQuotaStaysAsItIs(t *testing.T) {
	for _, q := range []float64{0.5, 1.5, 4} {
		got := passportJSON(limitPassport("linux", 8, 8, quota(q))).CPUQuota
		if got == nil || *got != q {
			t.Errorf("passportJSON CPUQuota for %v = %v, want it unchanged", q, got)
		}
	}
}

func TestPassportText_AnUnusableQuotaIsNotPrinted(t *testing.T) {
	for name, q := range unusableQuotas() {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			printPassport(&out, limitPassport("linux", 8, 8, quota(q)))

			if strings.Contains(out.String(), "quota") {
				t.Errorf("the machine line speaks of a quota:\n%s", out.String())
			}
		})
	}
}

func TestPassportText_AUsableQuotaIsPrinted(t *testing.T) {
	var out bytes.Buffer
	printPassport(&out, limitPassport("linux", 8, 8, quota(0.5)))

	if want := "8 CPU, GOMAXPROCS 8, quota 0.5, "; !strings.Contains(out.String(), want) {
		t.Errorf("no %q in:\n%s", want, out.String())
	}
}

// The passport built by the library holds no unusable quota at all.
func TestNewPassport_AnUnusableQuotaIsNone(t *testing.T) {
	for name, q := range unusableQuotas() {
		t.Run(name, func(t *testing.T) {
			if got := NewPassport("0123456789abcdef", nil, quota(q)).CPUQuota; got != nil {
				t.Errorf("CPUQuota = %v, want nil", *got)
			}
		})
	}

	if got := NewPassport("0123456789abcdef", nil, quota(0.5)).CPUQuota; got == nil || *got != 0.5 {
		t.Errorf("CPUQuota for 0.5 = %v, want it unchanged", got)
	}
}
