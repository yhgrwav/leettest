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
)

// timeoutConfig is a one-call config for a target nothing listens on: a
// refusal at load time never reaches it.
func timeoutConfig(timeout string) string {
	return `app:
  target:
    ip: 127.0.0.1
    port: 50051
  tls: false
load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 10
      duration: 1s
      timeout: ` + timeout + `
`
}

// Ground: contract — a timeout of 2h is refused before anything runs, like a
// timeout of 0s is: the same exit code (1, a config error; 2 is a finished
// run that proved invalid), the key and the limit on stderr, nothing on stdout.
func TestRun_ATimeoutOfTwoHoursIsAConfigError(t *testing.T) {
	_, _, reference := runConfigFile(t, timeoutConfig("0s"))
	if reference != 1 {
		t.Fatalf("premise: a timeout of 0s exits %d, want 1", reference)
	}

	stdout, stderr, code := runConfigFile(t, timeoutConfig("2h"))
	if code != reference {
		t.Errorf("exit code %d, want %d as for timeout: 0s\nstderr:\n%s", code, reference, stderr)
	}
	for _, want := range []string{"timeout", "2h0m0s", "is not below 1h"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not name %q:\n%s", want, stderr)
		}
	}
	if stdout != "" {
		t.Errorf("stdout is not empty: nothing may be run\n%s", stdout)
	}
}
