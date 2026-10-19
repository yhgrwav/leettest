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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fileSHA is the sha256 of the file's bytes, as 64 hex characters.
func fileSHA(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return bytesSHA(raw)
}

func bytesSHA(raw []byte) string {
	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:])
}

// withTopLines writes a copy of the config with lines in front of it, in a
// directory of its own, and returns the copy's path.
func withTopLines(t *testing.T, cfg, lines string) string {
	t.Helper()

	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read %s: %v", cfg, err)
	}
	path := filepath.Join(t.TempDir(), "leettest.yaml")
	if err := os.WriteFile(path, append([]byte(lines), raw...), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func passportCfg(t *testing.T) string {
	t.Helper()

	return writeConfig(t, closedPort(t), checkMethod, plaintext)
}

func passportSearchCfg(t *testing.T) string {
	t.Helper()

	return writeSearchOf(t, closedPort(t), quickSearch, "200ms", checkMethod)
}

// fakeRun is a finished run on the fake target; the JSON of it when asked for.
func fakeRun(t *testing.T, args ...string) (map[string]any, result) {
	t.Helper()

	res := runCLI(t.Context(), t, 30*time.Second, append([]string{"-fake"}, args...)...)
	wantExit(t, res, 0)
	if !slices.Contains(args, "json") {
		return nil, res
	}

	return decodeOnly(t, res.stdout), res
}

// fakeSearch is a search on the fake target. A host with a coarse clock ends
// it as an invalid search (exit 2); the passport is printed all the same, and
// only "no report" (exit 1) is wrong here.
func fakeSearch(t *testing.T, args ...string) result {
	t.Helper()

	res := runCLI(t.Context(), t, 60*time.Second, append([]string{"-fake"}, args...)...)
	if code := exitCode(res.err); code != 0 && code != 2 {
		t.Fatalf("search: exit %d (%v)\nstderr: %s", code, res.err, res.stderr)
	}

	return res
}

func passportOf(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()

	p, ok := jsonAt(t, doc, "passport").(map[string]any)
	if !ok {
		t.Fatalf("passport is not an object: %v", doc["passport"])
	}

	return p
}

func labelsOf(t *testing.T, doc map[string]any) map[string]string {
	t.Helper()

	raw, ok := jsonAt(t, passportOf(t, doc), "labels").(map[string]any)
	if !ok {
		t.Fatalf("passport.labels is not an object: %v", passportOf(t, doc)["labels"])
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		s, isString := v.(string)
		if !isString {
			t.Fatalf("label %q is %v, want a string", k, v)
		}
		out[k] = s
	}

	return out
}

func wantLabels(t *testing.T, doc map[string]any, want map[string]string) {
	t.Helper()

	if got := labelsOf(t, doc); !maps.Equal(got, want) {
		t.Errorf("passport.labels = %v, want %v", got, want)
	}
}

// Ground: contract — the passport of a run, at the top of the report: the
// config's bytes and the machine that generated the load. Nothing else: the
// host's name is often a person's.
func TestPassport_ARunCarriesTheConfigAndTheMachine(t *testing.T) {
	cfg := passportCfg(t)
	doc, res := fakeRun(t, "-output", "json", "-c", cfg)

	if doc["schema_version"] != 1.0 {
		t.Errorf("schema_version = %v, want 1 (fields were added)", doc["schema_version"])
	}
	p := passportOf(t, doc)

	want := []string{"arch", "config_sha256", "cpu_quota", "go_version", "gomaxprocs", "labels", "num_cpu", "os"}
	got := slices.Sorted(maps.Keys(p))
	if !slices.Equal(got, want) {
		t.Errorf("passport fields = %v, want exactly %v", got, want)
	}
	if p["config_sha256"] != fileSHA(t, cfg) {
		t.Errorf("config_sha256 = %v, want the sha256 of the config file %s", p["config_sha256"], fileSHA(t, cfg))
	}
	if p["os"] != runtime.GOOS || p["arch"] != runtime.GOARCH || p["go_version"] != runtime.Version() {
		t.Errorf("os/arch/go_version = %v/%v/%v, want %s/%s/%s",
			p["os"], p["arch"], p["go_version"], runtime.GOOS, runtime.GOARCH, runtime.Version())
	}
	if p["num_cpu"] != float64(runtime.NumCPU()) || p["gomaxprocs"] != float64(runtime.GOMAXPROCS(0)) {
		t.Errorf("num_cpu/gomaxprocs = %v/%v, want %d/%d", p["num_cpu"], p["gomaxprocs"], runtime.NumCPU(), runtime.GOMAXPROCS(0))
	}
	// A CI runner may have a quota and a laptop has none: the type is checked
	// here, the value in TestCPUQuota on a file system of the test's own.
	if q, ok := p["cpu_quota"]; ok && q != nil {
		if n, isNumber := q.(float64); !isNumber || n <= 0 {
			t.Errorf("cpu_quota = %v, want null or a number above 0", q)
		}
	}
	if !strings.Contains(res.stdout, `"labels":{}`) {
		t.Errorf("no labels must print as an empty object:\n%s", res.stdout)
	}
	wantLabels(t, doc, map[string]string{})
}

// Ground: contract — a search has one passport at its top; the steps inside
// are runs of the same machine and config and carry none.
func TestPassport_ASearchCarriesOneAtTheTop(t *testing.T) {
	cfg := passportSearchCfg(t)
	res := fakeSearch(t, "-output", "json", "-c", cfg)
	doc := decodeOnly(t, res.stdout)

	p := passportOf(t, doc)
	if p["config_sha256"] != fileSHA(t, cfg) || p["os"] != runtime.GOOS || p["go_version"] != runtime.Version() {
		t.Errorf("passport = %v, want the sha256 of %s and this machine", p, cfg)
	}
	wantLabels(t, doc, map[string]string{})

	runs, ok := jsonAt(t, doc, "breakpoint", "runs").([]any)
	if !ok || len(runs) == 0 {
		t.Fatalf("breakpoint.runs is empty:\n%s", res.stdout)
	}
	for i, r := range runs {
		report, isObject := jsonAt(t, r, "report").(map[string]any)
		if !isObject {
			continue
		}
		if _, has := report["passport"]; has {
			t.Errorf("runs[%d].report has a passport of its own, want none inside a search", i)
		}
	}
}

// Ground: contract — a dataset is hashed beside its file name, as its bytes
// were read: a call of its own, not the passport's.
func TestPassport_ADatasetCarriesItsHash(t *testing.T) {
	for name, content := range map[string]string{
		"one": "{\"service\":\"a\"}\n{\"service\":\"b\"}\n",
		"two": "{\"service\":\"a\"}\n{\"service\":\"c\"}\n",
	} {
		cfg := writeConfigWith(t, closedPort(t), checkMethod, plaintext, "      dataset: users.jsonl\n")
		if err := os.WriteFile(filepath.Join(filepath.Dir(cfg), "users.jsonl"), []byte(content), 0o600); err != nil {
			t.Fatalf("write dataset: %v", err)
		}

		doc, _ := fakeRun(t, "-output", "json", "-c", cfg)
		d, ok := jsonAt(t, doc, "methods").([]any)[0].(map[string]any)["dataset"].(map[string]any)
		if !ok {
			t.Fatalf("%s: dataset is not an object", name)
		}
		if d["file"] != "users.jsonl" || d["sha256"] != bytesSHA([]byte(content)) {
			t.Errorf("%s: dataset = %v, want file users.jsonl and sha256 %s", name, d, bytesSHA([]byte(content)))
		}
	}
}

// Ground: decisions «Run passport» — the hash is of the file's bytes, as they
// are: a comment is a different config, the path is not part of it. (г) says
// "the configs differ" for a comment, a false alarm and not a lie.
func TestPassport_TheHashIsOfTheBytesNotThePath(t *testing.T) {
	cfg := passportCfg(t)
	commented := withTopLines(t, cfg, "# only a comment\n")
	copied := withTopLines(t, cfg, "")

	base, _ := fakeRun(t, "-output", "json", "-c", cfg)
	other, _ := fakeRun(t, "-output", "json", "-c", commented)
	same, _ := fakeRun(t, "-output", "json", "-c", copied)

	baseSHA := passportOf(t, base)["config_sha256"]
	if baseSHA != fileSHA(t, cfg) {
		t.Fatalf("config_sha256 = %v, want %s", baseSHA, fileSHA(t, cfg))
	}
	if got := passportOf(t, other)["config_sha256"]; got != fileSHA(t, commented) || got == baseSHA {
		t.Errorf("a comment: config_sha256 = %v, want %s, different from %v", got, fileSHA(t, commented), baseSHA)
	}
	if got := passportOf(t, same)["config_sha256"]; got != baseSHA {
		t.Errorf("a copy in another directory: config_sha256 = %v, want %v", got, baseSHA)
	}
}

// Ground: contract — labels from the config and from -label meet in one
// object, keys in alphabetical order; the flag wins a key both have (decisions
// «Run passport», Р2: the flag refines the file, as helm --set and docker -e).
func TestPassport_LabelsFromTheConfigAndTheFlag(t *testing.T) {
	cfg := passportCfg(t)
	withConfig := withTopLines(t, cfg, "labels:\n  env: staging\n")

	for _, tc := range []struct {
		name string
		cfg  string
		args []string
		want map[string]string
		raw  string
	}{
		{"both", withConfig, []string{"-label", "commit=abc"},
			map[string]string{"commit": "abc", "env": "staging"}, `"labels":{"commit":"abc","env":"staging"}`},
		{"the flag wins", withConfig, []string{"-label", "commit=abc", "-label", "env=prod"},
			map[string]string{"commit": "abc", "env": "prod"}, `"labels":{"commit":"abc","env":"prod"}`},
		{"the config only", withConfig, nil,
			map[string]string{"env": "staging"}, `"labels":{"env":"staging"}`},
		{"the flag only", cfg, []string{"-label", "commit=abc"},
			map[string]string{"commit": "abc"}, `"labels":{"commit":"abc"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append(slices.Clone(tc.args), "-c", tc.cfg)

			doc, res := fakeRun(t, append([]string{"-output", "json"}, args...)...)
			wantLabels(t, doc, tc.want)
			if !strings.Contains(res.stdout, tc.raw) {
				t.Errorf("stdout lacks %s:\n%s", tc.raw, res.stdout)
			}
		})
	}
}

// Ground: contract — what a label may be, at the edges: the key to 64 of
// [A-Za-z0-9_.-], the value to 256 bytes (bytes, not letters), empty and
// UTF-8 allowed, an "=" inside the value belongs to the value.
func TestPassport_LabelsAtTheEdgesOfTheRules(t *testing.T) {
	key64 := strings.Repeat("k", 64)
	value256 := strings.Repeat("v", 256)
	wide128 := strings.Repeat("ё", 128)

	cfg := withTopLines(t, passportCfg(t), "labels:\n  "+key64+": "+value256+"\n  wide: "+wide128+"\n")
	doc, _ := fakeRun(t, "-output", "json", "-c", cfg,
		"-label", "a.b_c-D9=1", "-label", "empty=", "-label", "who=Егор", "-label", "eq=a=b")

	wantLabels(t, doc, map[string]string{
		key64: value256, "wide": wide128, "a.b_c-D9": "1", "empty": "", "who": "Егор", "eq": "a=b",
	})
}

// Ground: contract — a label that breaks the rules is an error before the
// start (exit 1, no report) that names where it was written: labels.<key> in
// the config, -label and the offending text on the command line.
func TestPassport_ABadLabelIsExit1BeforeTheStart(t *testing.T) {
	key65 := strings.Repeat("k", 65)
	value257 := strings.Repeat("v", 257)
	wide129 := strings.Repeat("ё", 129)
	base := passportCfg(t)

	cases := []struct {
		name  string
		yaml  string
		flag  []string
		wants []string
	}{
		{name: "a space in the key", yaml: "  \"a b\": x\n", flag: []string{"-label", "a b=1"}, wants: []string{"a b"}},
		{name: "a 65-character key", yaml: "  " + key65 + ": x\n", flag: []string{"-label", key65 + "=1"}, wants: []string{key65}},
		{name: "a 257-byte value", yaml: "  big: " + value257 + "\n", flag: []string{"-label", "big=" + value257}, wants: []string{"big"}},
		{name: "257 bytes in 129 letters", yaml: "  wide: " + wide129 + "\n", flag: []string{"-label", "wide=" + wide129}, wants: []string{"wide"}},
		{name: "a newline", yaml: "  nl: \"a\\nb\"\n", flag: []string{"-label", "nl=a\nb"}, wants: []string{"nl"}},
		{name: "a tab", yaml: "  tab: \"a\\tb\"\n", flag: []string{"-label", "tab=a\tb"}, wants: []string{"tab"}},
		{name: "not UTF-8", flag: []string{"-label", "bad=\xff"}, wants: []string{"bad"}},
		{name: "no equals sign", flag: []string{"-label", "nokey"}, wants: []string{"nokey"}},
		{name: "an empty key", yaml: "  \"\": v\n", flag: []string{"-label", "=v"}, wants: nil},
		{name: "an empty argument", flag: []string{"-label", ""}, wants: nil},
		{name: "the same key twice", flag: []string{"-label", "dup=1", "-label", "dup=2"}, wants: []string{"dup"}},
	}

	for _, tc := range cases {
		if tc.yaml != "" {
			t.Run("config: "+tc.name, func(t *testing.T) {
				cfg := withTopLines(t, base, "labels:\n"+tc.yaml)

				res := runCLI(t.Context(), t, 10*time.Second, "-fake", "-c", cfg)

				wantExit(t, res, 1)
				if res.stdout != "" {
					t.Errorf("a report was printed:\n%s", res.stdout)
				}
				wants := []string{"labels"}
				for _, w := range tc.wants {
					wants = append(wants, "labels."+w)
				}
				for _, w := range wants {
					if res.err == nil || !strings.Contains(res.err.Error(), w) {
						t.Errorf("error %v lacks %q", res.err, w)
					}
				}
			})
		}
		if tc.flag != nil {
			t.Run("flag: "+tc.name, func(t *testing.T) {
				res := runCLI(t.Context(), t, 10*time.Second, append([]string{"-fake", "-c", base}, tc.flag...)...)

				wantExit(t, res, 1)
				if res.stdout != "" {
					t.Errorf("a report was printed:\n%s", res.stdout)
				}
				for _, w := range append([]string{"-label"}, tc.wants...) {
					if res.err == nil || !strings.Contains(res.err.Error(), w) {
						t.Errorf("error %v lacks %q", res.err, w)
					}
				}
				if res.err != nil && strings.Contains(res.err.Error(), "not defined") {
					t.Errorf("error %v: the flag is unknown, want it parsed and refused", res.err)
				}
			})
		}
	}
}

func machineLine(t *testing.T, stdout string) string {
	t.Helper()

	for line := range strings.Lines(stdout) {
		if strings.HasPrefix(line, "machine: ") {
			return strings.TrimSuffix(line, "\n")
		}
	}
	t.Fatalf("no \"machine:\" line in:\n%s", stdout)

	return ""
}

// Ground: contract — one line on the machine in the header of a run and of a
// search: platform, CPUs, Go, the config's hash cut to 12 hex. "quota" only
// when the machine has one, and the same number the JSON gives.
func TestPassport_TheTextSaysTheMachineOnOneLine(t *testing.T) {
	for _, tc := range []struct {
		name, table string
		search      bool
	}{
		{"a run", "method ", false},
		{"a search", "  kind ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg string
			var jsonRes, textRes result
			if tc.search {
				cfg = passportSearchCfg(t)
				jsonRes = fakeSearch(t, "-output", "json", "-c", cfg)
				textRes = fakeSearch(t, "-c", cfg)
			} else {
				cfg = passportCfg(t)
				_, jsonRes = fakeRun(t, "-output", "json", "-c", cfg)
				_, textRes = fakeRun(t, "-c", cfg)
			}
			quota := passportOf(t, decodeOnly(t, jsonRes.stdout))["cpu_quota"]

			line := machineLine(t, textRes.stdout)
			shape := regexp.MustCompile(`^machine: ` + regexp.QuoteMeta(runtime.GOOS+"/"+runtime.GOARCH) +
				fmt.Sprintf(`, %d CPU, GOMAXPROCS %d`, runtime.NumCPU(), runtime.GOMAXPROCS(0)) +
				`(, quota [0-9.]+)?, ` + regexp.QuoteMeta(runtime.Version()) + `; config sha256:([0-9a-f]{12})$`)
			m := shape.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("machine line %q does not match %s", line, shape)
			}
			if want := fileSHA(t, cfg)[:12]; m[2] != want {
				t.Errorf("the hash in %q is %s, want %s", line, m[2], want)
			}
			if q, has := quota.(float64); has {
				if want := ", quota " + strconv.FormatFloat(q, 'f', -1, 64); m[1] != want {
					t.Errorf("quota in %q is %q, want %q (the JSON says %v)", line, m[1], want, q)
				}
			} else if m[1] != "" {
				t.Errorf("%q names a quota the JSON does not have", line)
			}

			before, _, found := strings.Cut(textRes.stdout, line)
			if !found || strings.Contains(before, tc.table) {
				t.Errorf("the machine line is not in the header, above the table %q:\n%s", tc.table, textRes.stdout)
			}
			if strings.Contains(textRes.stdout, "labels:") {
				t.Errorf("a labels line without labels:\n%s", textRes.stdout)
			}
		})
	}
}

// Ground: contract — labels get a line of their own under the machine line,
// only when there are any; the same set the JSON has, in the same order.
func TestPassport_TheTextListsTheLabelsUnderTheMachine(t *testing.T) {
	cfg := withTopLines(t, passportCfg(t), "labels:\n  env: staging\n")
	search := withTopLines(t, passportSearchCfg(t), "labels:\n  env: staging\n")

	_, run := fakeRun(t, "-c", cfg, "-label", "commit=abc")
	for name, res := range map[string]result{
		"a run":    run,
		"a search": fakeSearch(t, "-c", search, "-label", "commit=abc"),
	} {
		lines := strings.Split(res.stdout, "\n")
		i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "machine: ") })
		if i < 0 || i+1 >= len(lines) || lines[i+1] != "labels: commit=abc, env=staging" {
			t.Errorf("%s: want \"labels: commit=abc, env=staging\" right under the machine line:\n%s", name, res.stdout)
		}
	}

	// The flag wins in the text too.
	_, res := fakeRun(t, "-c", cfg, "-label", "env=prod")
	if !strings.Contains(res.stdout, "\nlabels: env=prod\n") || strings.Contains(res.stdout, "staging") {
		t.Errorf("the flag must win in the text:\n%s", res.stdout)
	}
}

// Ground: contract — the labels of a search are in its passport.
func TestPassport_ASearchTakesTheLabels(t *testing.T) {
	cfg := withTopLines(t, passportSearchCfg(t), "labels:\n  env: staging\n")

	doc := decodeOnly(t, fakeSearch(t, "-output", "json", "-c", cfg, "-label", "env=prod", "-label", "commit=abc").stdout)

	wantLabels(t, doc, map[string]string{"commit": "abc", "env": "prod"})
}
