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

import (
	"strings"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/grpcsender"
)

// Ground: contract — the closes of a report carry the EFFECTIVE idle timeout: the configured one,
// else the sender's default, flagged as the default; the settings stay "as configured" (null in
// JSON with the key left out), so the JSON contract does not change. Mutations "closesOf passes the
// configured 0 on" and "the settings echo the default" turn it red.
func TestClosesOf_LeftOutKeyIsTheDefaultAndJSONStaysNull(t *testing.T) {
	src := func() grpcsender.CloseStats { return grpcsender.CloseStats{IdleOurs: 1} }

	got := closesOf(src, 0)
	if got == nil || got.IdleTimeout != grpcsender.DefaultIdleTimeout || !got.IdleDefault {
		t.Errorf("closesOf with the key left out = %+v, want IdleTimeout %v and IdleDefault", got, grpcsender.DefaultIdleTimeout)
	}

	got = closesOf(src, 50*time.Second)
	if got == nil || got.IdleTimeout != 50*time.Second || got.IdleDefault {
		t.Errorf("closesOf with the key set = %+v, want IdleTimeout 50s and no IdleDefault", got)
	}

	text, js := echoRun(t, 20*time.Second, "-c", writeEchoConfig(t, "", ""))

	if v, present := js["settings"].(map[string]any)["idle_timeout_us"]; !present || v != nil {
		t.Errorf("settings.idle_timeout_us = %#v (present %v) with the key left out, want null", v, present)
	}

	if strings.Contains(text, "idle_timeout") {
		t.Errorf("the text names idle_timeout with the key left out:\n%s", text)
	}
}
