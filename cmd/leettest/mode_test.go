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
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const needsRampText = "app.mode: real needs load.ramp (a ramp from 0 to rps), e.g. load.ramp: 30s"

// setMode adds app.mode to a config that has a «  tls: false» line.
func setMode(t *testing.T, path, mode string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := strings.Replace(string(data), "  tls: false\n", "  tls: false\n  mode: "+mode+"\n", 1)
	if got == string(data) {
		t.Fatalf("no tls line to put the mode after:\n%s", data)
	}
	if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// setRamp adds load.ramp to a config that has a «load:» line.
func setRamp(t *testing.T, path, ramp string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := strings.Replace(string(data), "load:\n", "load:\n  ramp: "+ramp+"\n", 1)
	if got == string(data) {
		t.Fatalf("no load line to put the ramp after:\n%s", data)
	}
	if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// Ground: contract — a run in real without a ramp is a config error before
// anything is sent, exit 1, with the one message that says what to write; the
// report is not written and the target hears nothing. A fake run is refused
// alike: -fake changes where the calls go, not what the config means.
func TestRun_RealWithoutARampIsRefused(t *testing.T) {
	for _, args := range [][]string{nil, {"-fake"}} {
		target := startReflectingTarget(t)
		cfg := dryConfig(t, target.addr, "  mode: real\n", dryCall(unaryCallMethod, ""))

		res := runCLI(t.Context(), t, 10*time.Second, append([]string{"-c", cfg}, args...)...)

		if got := exitCode(res.err); got != 1 {
			t.Fatalf("%v: exit code %d, want 1\nerr %v\nstdout:\n%s", args, got, res.err, res.stdout)
		}
		if all := fmt.Sprint(res.err) + res.stderr; !strings.Contains(all, needsRampText) {
			t.Errorf("%v: no %q in:\n%s", args, needsRampText, all)
		}
		if res.stdout != "" {
			t.Errorf("%v: stdout is not empty, a refused config writes no report:\n%s", args, res.stdout)
		}
		if n := len(target.recorder.allSimple()); n != 0 || target.health.calls.Load() != 0 {
			t.Errorf("%v: the target got requests from a config that was refused", args)
		}
	}
}

// Ground: contract — a mode that is not local or real is exit 1 and the error
// names the value as written.
func TestRun_AnUnknownModeIsRefused(t *testing.T) {
	cfg := writeEchoConfig(t, "  mode: staging\n", "")

	res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-c", cfg)

	if got := exitCode(res.err); got != 1 {
		t.Fatalf("exit code %d, want 1\nerr %v\nstdout:\n%s", got, res.err, res.stdout)
	}
	if all := fmt.Sprint(res.err) + res.stderr; !strings.Contains(all, "app.mode must be local or real") || !strings.Contains(all, "staging") {
		t.Errorf("the error does not name the rule and the value:\n%s", all)
	}
}

// Ground: contract — -dry-run in real validates the config as a run does: the
// same text, the same exit 1, nothing sent. A CI step «dry run, then load» must
// not pass the first and fail the second.
func TestRun_DryRunInRealWithoutARampIsRefusedLikeARun(t *testing.T) {
	target := startReflectingTarget(t)
	cfg := dryConfig(t, target.addr, "  mode: real\n", dryCall(unaryCallMethod, ""))

	plain := runCLI(t.Context(), t, 20*time.Second, "-c", cfg)
	if exitCode(plain.err) != 1 || !strings.Contains(fmt.Sprint(plain.err), needsRampText) {
		t.Fatalf("without the flag: exit %d, err %v: the premise of the test is a run that refuses", exitCode(plain.err), plain.err)
	}

	dry := runCLI(t.Context(), t, 5*time.Second, "-c", cfg, "-dry-run")

	if got := exitCode(dry.err); got != 1 {
		t.Fatalf("dry run: exit code %d, want 1\nerr %v\nstdout:\n%s", got, dry.err, dry.stdout)
	}
	if fmt.Sprint(dry.err) != fmt.Sprint(plain.err) {
		t.Errorf("dry run says:\n%v\nthe run says:\n%v", dry.err, plain.err)
	}
	if n := len(target.recorder.allSimple()); n != 0 || target.health.calls.Load() != 0 {
		t.Errorf("the target got requests from a dry run that was refused")
	}
}

// Ground: contract — a dry run in real with a ramp works and asks nothing,
// with -yes or without it: it sends one request per call, so there is nothing
// to confirm, and -yes is accepted with it so that one command line serves
// the dry run and the load. (Green on the stub: guards the requirement that
// the implementation asks no confirmation of a dry run.)
func TestRun_DryRunInRealAsksNoConfirmation(t *testing.T) {
	for _, extra := range [][]string{nil, {"-yes"}} {
		target := startReflectingTarget(t)
		cfg := dryConfig(t, target.addr, "  mode: real\n", dryCall(unaryCallMethod, ""))
		setRamp(t, cfg, "30s")
		setStop(t, cfg, "5%")

		res := runCLI(t.Context(), t, 5*time.Second, append([]string{"-c", cfg, "-dry-run"}, extra...)...)

		if res.err != nil {
			t.Fatalf("%v: run: %v\nstderr:\n%s", extra, res.err, res.stderr)
		}
		if n := len(target.recorder.allSimple()); n != 1 {
			t.Errorf("%v: the target got %d requests, want 1", extra, n)
		}
		all := strings.ToLower(res.stdout + res.stderr)
		for _, ask := range []string{"[y/n]", "confirm", "(yes/no)"} {
			if strings.Contains(all, ask) {
				t.Errorf("%v: the dry run asks (%q):\n%s", extra, ask, all)
			}
		}
	}
}

// Ground: contract — a run tells its mode in the text header and in
// settings.mode of the JSON; left out is local, and the top-level mode of the
// JSON stays «run». real comes with its ramp.
func TestRun_TheReportTellsTheMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode string // the config's line, "" for left out
		ramp string
		want string
	}{
		{"left out", "", "", "local"},
		{"local", "  mode: local\n", "", "local"},
		{"real", "  mode: real\n", "  ramp: 1s\n  stop_on_errors: 5%\n", "real"},
	} {
		cfg := writeEchoConfig(t, tc.mode, tc.ramp)
		text, js := echoRun(t, 30*time.Second, "-c", cfg)

		if want := "\nname: wallet\nmode: " + tc.want + "\nsettings: "; !strings.Contains(text, want) {
			t.Errorf("%s: the text report lacks %q:\n%s", tc.name, want, text)
		}
		if got := jsonAt(t, js, "settings", "mode"); got != tc.want {
			t.Errorf("%s: settings.mode = %v, want %q", tc.name, got, tc.want)
		}
		if got := js["mode"]; got != "run" {
			t.Errorf("%s: mode = %v, want run: the kind of report", tc.name, got)
		}
	}
}

// Ground: contract — a search tells its mode the same way. real asks the search
// for no ramp: it steps up from breakpoint.from by itself (the confirmation
// that protects it is unit 3's).
func TestRun_ASearchTellsTheMode(t *testing.T) {
	for _, mode := range []string{"local", "real"} {
		cfg := writeSearch(t, closedPort(t), checkMethod)
		withName(t, cfg, "name: wallet\n")
		setMode(t, cfg, mode)

		text, js := echoRun(t, 60*time.Second, "-c", cfg)

		if want := "\nname: wallet\nmode: " + mode + "\nsettings: settle "; !strings.Contains(text, want) {
			t.Errorf("%s: the search's text lacks %q:\n%s", mode, want, text)
		}
		if got := jsonAt(t, js, "settings", "mode"); got != mode {
			t.Errorf("%s: settings.mode = %v, want %q", mode, got, mode)
		}
		if got := js["mode"]; got != "breakpoint" {
			t.Errorf("%s: mode = %v, want breakpoint: the kind of report", mode, got)
		}
		runs := jsonAt(t, js, "breakpoint", "runs").([]any)
		if len(runs) == 0 {
			t.Fatalf("%s: the search reports no runs", mode)
		}
		for i, r := range runs {
			if got := jsonAt(t, r, "report", "settings", "mode"); got != mode {
				t.Errorf("%s: runs[%d].report.settings.mode = %v, want %q", mode, i, got, mode)
			}
		}
	}
}

// Ground: contract — -yes is accepted in local and changes nothing: the same
// config runs to the same exit and the same settings, with it and without it,
// and nothing is asked. The flag stays in the plan for real (unit 3), so
// a script written for real runs unchanged against local.
func TestRun_YesChangesNothingInLocal(t *testing.T) {
	for _, mode := range []string{"", "  mode: local\n"} {
		cfg := writeEchoConfig(t, mode, "")

		without := runCLI(t.Context(), t, 30*time.Second, "-fake", "-c", cfg, "-output", "json")
		with := runCLI(t.Context(), t, 30*time.Second, "-fake", "-c", cfg, "-output", "json", "-yes")

		if without.err != nil || with.err != nil {
			t.Fatalf("mode %q: without %v, with -yes %v: both must run", mode, without.err, with.err)
		}
		a, b := decodeOnly(t, without.stdout), decodeOnly(t, with.stdout)
		if !reflect.DeepEqual(a["settings"], b["settings"]) {
			t.Errorf("mode %q: settings differ:\n%v\n%v", mode, a["settings"], b["settings"])
		}
		if got := jsonAt(t, b, "settings", "mode"); got != "local" {
			t.Errorf("mode %q: with -yes settings.mode = %v, want local", mode, got)
		}
		if strings.Contains(strings.ToLower(with.stderr), "confirm") {
			t.Errorf("mode %q: -yes in local asks or says something about confirming:\n%s", mode, with.stderr)
		}
	}
}
