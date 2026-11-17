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

package config_test

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// tokenHeader is the app.metadata of the common case: a prefix and a file.
const tokenHeader = "  metadata:\n    authorization: 'Bearer ${file:token.txt}'\n"

// inTempDir makes a new directory the working directory of the test: a path
// written without a directory is read from there under Parse.
func inTempDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	return dir
}

func putFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// clip quotes s, cut after 40 bytes: a failure over a 64 KiB value stays readable.
func clip(s string) string {
	if len(s) > 40 {
		return strconv.Quote(s[:40]) + "... (" + strconv.Itoa(len(s)) + " bytes)"
	}

	return strconv.Quote(s)
}

func parseApp(t *testing.T, appLines string) *config.MasterConfig {
	t.Helper()

	cfg, err := config.Parse(withApp(appLines))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return cfg
}

// Ground: contract (spec §1.1, §1.3) — a header value is literals and ${file:PATH}
// references in a row; a header with a reference is not in App.Metadata (it is
// not known until it is read), RawMetadata stays as written, and ReadMetadata
// returns every header, the assembled one included, and what each file held.
func TestMetadataFile_AReferenceIsAssembledWithItsPrefix(t *testing.T) {
	inTempDir(t)
	putFile(t, "token.txt", "abc\n")

	cfg := parseApp(t, tokenHeader+"    x-plain: p\n")

	if _, in := cfg.App.Metadata["authorization"]; in {
		t.Errorf("Metadata %v holds the header that reads a file", cfg.App.Metadata)
	}
	if cfg.App.Metadata["x-plain"] != "p" {
		t.Errorf("Metadata %v, want the plain header x-plain: p", cfg.App.Metadata)
	}
	if got := cfg.App.RawMetadata["authorization"]; got != "Bearer ${file:token.txt}" {
		t.Errorf("RawMetadata authorization = %q, want it as written", got)
	}
	if !cfg.App.HasMetadata() {
		t.Error("HasMetadata = false with two headers")
	}

	got, files, err := cfg.App.ReadMetadata()
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if want := map[string]string{"authorization": "Bearer abc", "x-plain": "p"}; !maps.Equal(got, want) {
		t.Errorf("ReadMetadata map = %v, want %v", got, want)
	}
	if want := []config.FileValue{{Path: "token.txt", Value: "abc"}}; !slices.Equal(files, want) {
		t.Errorf("ReadMetadata files = %v, want %v", files, want)
	}
}

// topSecret is in the content of every file below that the read refuses: no
// message may carry it.
const topSecret = "TOPSECRET"

var bytePosition = regexp.MustCompile(`(?i)(byte|position|offset|index|column)\s*[#:]?\s*\d`)

// Ground: contract (spec §1.3) — the value is the whole file with exactly one
// trailing line break cut ("\n" or "\r\n"); spaces stay. What is empty after
// that, has a line break inside (a second trailing one too), a byte outside
// %x20-%x7E, is over 64 KiB is refused with one reason, the key and the path
// once. The message holds neither the content nor the place of a byte (spec §4.1).
func TestMetadataFile_TheValueIsTheWholeFileMinusOneTrailingBreak(t *testing.T) {
	const (
		lineBreak = "has a line break inside the value (one at the end is cut)"
		outside   = "has a byte outside printable ASCII (%x20-%x7E)"
		empty     = "is empty"
		large     = "is larger than 64 KiB"
	)

	for _, tc := range []struct {
		name, content, want, why string
	}{
		{name: "no break", content: "abc", want: "abc"},
		{name: "lf", content: "abc\n", want: "abc"},
		{name: "crlf", content: "abc\r\n", want: "abc"},
		{name: "spaces stay", content: " abc ", want: " abc "},
		{name: "spaces stay before the break", content: " abc \n", want: " abc "},
		{name: "exactly 64 KiB", content: strings.Repeat("a", 64<<10), want: strings.Repeat("a", 64<<10)},
		{name: "two breaks", content: topSecret + "\n\n", why: lineBreak},
		{name: "two crlf", content: topSecret + "\r\n\r\n", why: lineBreak},
		{name: "break inside", content: topSecret + "\nx", why: lineBreak},
		{name: "only a break", content: "\n", why: empty},
		{name: "empty file", content: "", why: empty},
		{name: "non-ASCII", content: topSecret + "é", why: outside},
		{name: "byte order mark", content: "\xef\xbb\xbf" + topSecret, why: outside},
		{name: "64 KiB and a byte", content: topSecret + strings.Repeat("a", 64<<10+1-len(topSecret)), why: large},
		// The limit is the size of the file, the line break in it included.
		{name: "64 KiB and a trailing break", content: strings.Repeat("a", 64<<10) + "\n", why: large},
		// The order of the checks: size, empty, line break, byte range.
		{name: "size beats the byte range", content: topSecret + "é" + strings.Repeat("a", 64<<10), why: large},
		{name: "a break beats the byte range", content: "x\né", why: lineBreak},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inTempDir(t)
			putFile(t, "token.txt", tc.content)

			cfg := parseApp(t, tokenHeader)
			got, files, err := cfg.App.ReadMetadata()

			if tc.why == "" {
				if err != nil {
					t.Fatalf("ReadMetadata: %v", err)
				}
				if got["authorization"] != "Bearer "+tc.want {
					t.Errorf("authorization = %s, want %s", clip(got["authorization"]), clip("Bearer "+tc.want))
				}
				if want := []config.FileValue{{Path: "token.txt", Value: tc.want}}; !slices.Equal(files, want) {
					t.Errorf("files = %d values, want one: path token.txt, value %s", len(files), clip(tc.want))
				}

				return
			}

			if !errors.Is(err, config.ErrInvalidMetadata) {
				t.Fatalf("error = %v, want ErrInvalidMetadata", err)
			}

			msg := err.Error()
			if want := `invalid metadata: key "authorization": file token.txt: ` + tc.why; !strings.Contains(msg, want) {
				t.Errorf("error %q, want it to say %q", msg, want)
			}
			if n := strings.Count(msg, "token.txt"); n != 1 {
				t.Errorf("the path is in the error %d times, want once: %q", n, msg)
			}
			if strings.Contains(msg, topSecret) {
				t.Errorf("the error carries the content of the file: %q", msg)
			}
			if bytePosition.MatchString(msg) {
				t.Errorf("the error says where the byte is: %q", msg)
			}
		})
	}
}

// Ground: contract (spec §1.3) — a path that is not a regular file (a directory;
// a FIFO and a device the same way) and one that cannot be read are refused with
// the key and the path once; the config itself does not read the file, so Parse
// of a config naming a missing file succeeds and the first read fails.
func TestMetadataFile_NotAFileAndAMissingFile(t *testing.T) {
	inTempDir(t)
	if err := os.Mkdir("secrets", 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, tc := range []struct{ name, path, why string }{
		{"a directory", "secrets", "is not a regular file"},
		{"a missing file", "nope.txt", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := parseApp(t, "  metadata:\n    authorization: 'Bearer ${file:"+tc.path+"}'\n")

			_, _, err := cfg.App.ReadMetadata()
			if !errors.Is(err, config.ErrInvalidMetadata) {
				t.Fatalf("error = %v, want ErrInvalidMetadata", err)
			}

			msg := err.Error()
			if want := `invalid metadata: key "authorization": file ` + tc.path + ": " + tc.why; !strings.Contains(msg, want) {
				t.Errorf("error %q, want it to say %q", msg, want)
			}
			if n := strings.Count(msg, tc.path); n != 1 {
				t.Errorf("the path is in the error %d times, want once: %q", n, msg)
			}
		})
	}
}

// Ground: contract (spec §1.1) — "$${file:x}" is the text "${file:x}", the escape
// of ${ENV}; no file is read for it, and a real reference beside it is.
func TestMetadataFile_DoubledDollarWritesALiteralReference(t *testing.T) {
	inTempDir(t)
	putFile(t, "token.txt", "abc")

	cfg := parseApp(t, "  metadata:\n    x-lit: '$${file:x}'\n    x-both: '$${file:x} ${file:token.txt}'\n")

	if got := cfg.App.Metadata["x-lit"]; got != "${file:x}" {
		t.Errorf("x-lit = %q, want the literal ${file:x}", got)
	}

	got, files, err := cfg.App.ReadMetadata()
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if want := map[string]string{"x-lit": "${file:x}", "x-both": "${file:x} abc"}; !maps.Equal(got, want) {
		t.Errorf("ReadMetadata map = %v, want %v", got, want)
	}
	if want := []config.FileValue{{Path: "token.txt", Value: "abc"}}; !slices.Equal(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}
}

// Ground: guard (spec §1.1, green before the code) — ${file} with no colon is
// the environment variable named file, as it was.
func TestMetadataFile_NoColonIsAnEnvironmentVariable(t *testing.T) {
	t.Setenv("file", "tok")

	cfg := parseApp(t, "  metadata:\n    x-env: '${file}'\n")

	if got := cfg.App.Metadata["x-env"]; got != "tok" {
		t.Errorf("x-env = %q, want the variable's value", got)
	}

	if got := config.SubstitutedEnvNames("${file} ${file:x} $${file}"); !slices.Equal(got, []string{"file"}) {
		t.Errorf("SubstitutedEnvNames = %v, want only the variable ${file}", got)
	}
}

// Ground: contract (spec §1.2, round 1 F1) — a reference needs a path, and the
// path takes no '$' or '{': ${file:${DIR}/t} is an error, not a feature. The path
// in the message is the text up to the first '}', as written. An unescaped
// "${file:" with no '}' after it is an error too: left as text it would send the
// words of the config to the target as a token.
func TestMetadataFile_BadReferencesAreRefused(t *testing.T) {
	const (
		taken     = "must not contain '$' or '{': paths are taken as written"
		notClosed = `invalid metadata: key "authorization": ${file: is not closed: write the path and a closing }`
	)

	for _, tc := range []struct{ name, value, want string }{
		{"no path", "${file:}", `invalid metadata: key "authorization": ${file:} needs a path`},
		{"not closed", "${file:token.txt", notClosed},
		{"not closed, no path", "${file:", notClosed},
		{"dollar", "${file:a$b}", `invalid metadata: key "authorization": file path "a$b" ` + taken},
		{"nested", "${file:${DIR}/t}", `invalid metadata: key "authorization": file path "${DIR" ` + taken},
		{"brace", "${file:a{b}", `invalid metadata: key "authorization": file path "a{b" ` + taken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse(withApp("  metadata:\n    authorization: 'Bearer " + tc.value + "'\n"))
			if !errors.Is(err, config.ErrInvalidMetadata) {
				t.Fatalf("error = %v, want ErrInvalidMetadata", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q, want it to say %q", err, tc.want)
			}
		})
	}
}

// Ground: contract (spec §1.1) — a value takes two references and a ${ENV} in a
// row, in the order written.
func TestMetadataFile_TwoReferencesAndAVariableInOneValue(t *testing.T) {
	t.Setenv("LT_PREFIX", "Bearer")
	inTempDir(t)
	putFile(t, "a.txt", "x\n")
	putFile(t, "b.txt", "y\r\n")

	cfg := parseApp(t, "  metadata:\n    authorization: '${LT_PREFIX} ${file:a.txt}-${file:b.txt}'\n")

	got, files, err := cfg.App.ReadMetadata()
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if got["authorization"] != "Bearer x-y" {
		t.Errorf("authorization = %q, want %q", got["authorization"], "Bearer x-y")
	}
	if want := []config.FileValue{{Path: "a.txt", Value: "x"}, {Path: "b.txt", Value: "y"}}; !slices.Equal(files, want) {
		t.Errorf("files = %v, want %v, in the order of the value", files, want)
	}
}

// Ground: contract (spec §1.4) — user-agent is set when the connection is made and
// cannot be read again: a reference in it is refused with any metadata_refresh,
// or none (one rule, not one that depends on a neighbouring key), whatever the
// case of the key. The file need not exist: it is a config error, not a read.
func TestMetadataFile_UserAgentCannotComeFromAFile(t *testing.T) {
	const want = `invalid metadata: key "user-agent": cannot come from a file: it is set when the connection is made`

	for _, tc := range []struct{ name, refresh, key, value string }{
		{"lowercase", "", "user-agent", "${file:ua.txt}"},
		{"mixed case", "", "User-Agent", "${file:ua.txt}"},
		{"upper case", "", "USER-AGENT", "${file:ua.txt}"},
		{"with a prefix", "", "user-agent", "lt/${file:ua.txt}"},
		{"with a refresh", "  metadata_refresh: 5s\n", "user-agent", "${file:ua.txt}"},
		{"mixed case with a refresh", "  metadata_refresh: 5s\n", "User-Agent", "${file:ua.txt}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse(withApp(tc.refresh + "  metadata:\n    " + tc.key + ": '" + tc.value + "'\n"))
			if !errors.Is(err, config.ErrInvalidMetadata) {
				t.Fatalf("error = %v, want ErrInvalidMetadata", err)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q, want it to say %q", err, want)
			}
		})
	}
}

// Ground: guard (spec §1.5, green before the code) — the checks of the key run as
// they did, before the references are read: a reserved or malformed key with a
// reference is refused for the key, not for the file. The literal pieces of a
// value are checked for printable ASCII at load, as today.
func TestMetadataFile_OtherRulesOfTheKeyAndTheLiteralsHold(t *testing.T) {
	for _, tc := range []struct{ name, lines, want string }{
		{"content-type", "    content-type: '${file:t}'\n", "set by the gRPC transport"},
		{"grpc prefix", "    grpc-x: '${file:t}'\n", "grpc- is reserved by gRPC"},
		{"binary", "    x-trace-bin: '${file:t}'\n", "binary metadata is not supported"},
		{"space in the key", "    \"x api\": '${file:t}'\n", "only letters, digits"},
		{"twice by case", "    Authorization: '${file:a}'\n    authorization: '${file:b}'\n", `key "authorization" twice`},
		{"literal outside ASCII", "    authorization: 'é ${file:t}'\n", `key "authorization": the value must be printable ASCII`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Parse(withApp("  metadata:\n" + tc.lines))
			if !errors.Is(err, config.ErrInvalidMetadata) {
				t.Fatalf("error = %v, want ErrInvalidMetadata", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q, want it to say %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "needs a path") || strings.Contains(err.Error(), "cannot come from a file") {
				t.Errorf("error %q is about the reference, not the key", err)
			}
		})
	}
}

// Ground: contract (spec §1.2) — LoadFile reads a relative path from the
// directory of the config, as ca, cert and key; an absolute path is left alone;
// FileValue.Path and the messages keep the path as written.
func TestMetadataFile_LoadFileResolvesRelativePathsFromTheConfigDirectory(t *testing.T) {
	cfgDir, otherDir, absDir := t.TempDir(), t.TempDir(), t.TempDir()
	putFile(t, filepath.Join(cfgDir, "token.txt"), "abc\n")
	putFile(t, filepath.Join(absDir, "abs.txt"), "xyz")

	abs := filepath.ToSlash(filepath.Join(absDir, "abs.txt"))
	cfgPath := filepath.Join(cfgDir, "leettest.yaml")
	putFile(t, cfgPath, string(withApp(tokenHeader+"    x-abs: '${file:"+abs+"}'\n    x-gone: '${file:gone.txt}'\n")))

	t.Chdir(otherDir)

	cfg, err := config.LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	_, _, err = cfg.App.ReadMetadata()
	if err == nil {
		t.Fatal("ReadMetadata read x-gone from nowhere")
	}
	if !strings.Contains(err.Error(), `key "x-gone": file gone.txt: `) {
		t.Errorf("error %q, want the path of x-gone as written", err)
	}
	if strings.Contains(err.Error(), cfgDir) || strings.Contains(err.Error(), otherDir) {
		t.Errorf("error %q shows the resolved path, not the one as written", err)
	}

	// Without the missing header the same config reads from the config's directory.
	putFile(t, cfgPath, string(withApp(tokenHeader+"    x-abs: '${file:"+abs+"}'\n")))

	cfg, err = config.LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	got, files, err := cfg.App.ReadMetadata()
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if want := map[string]string{"authorization": "Bearer abc", "x-abs": "xyz"}; !maps.Equal(got, want) {
		t.Errorf("ReadMetadata map = %v, want %v", got, want)
	}

	// Headers are read by ascending key: authorization, then x-abs.
	want := []config.FileValue{{Path: "token.txt", Value: "abc"}, {Path: abs, Value: "xyz"}}
	if !slices.Equal(files, want) {
		t.Errorf("files = %v, want %v (paths as written, in the order of the keys)", files, want)
	}
}

// Ground: contract (spec §1.2) — Parse knows no directory: a relative path stays
// as written and is read from the working directory, as ca is.
func TestMetadataFile_ParseReadsARelativePathFromTheWorkingDirectory(t *testing.T) {
	inTempDir(t)
	putFile(t, "token.txt", "abc")

	_, files, err := parseApp(t, tokenHeader).App.ReadMetadata()
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if want := []config.FileValue{{Path: "token.txt", Value: "abc"}}; !slices.Equal(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}
}

// Ground: contract (spec §1, the surface) — HasMetadata is true for any header,
// one that reads a file included: a token that comes only from a file still has
// to make the credentials guard fire.
func TestMetadataFile_HasMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, lines string
		want        bool
	}{
		{"no metadata", "", false},
		{"an empty map", "  metadata: {}\n", false},
		{"a literal header", "  metadata:\n    x-a: b\n", true},
		{"only a file header", tokenHeader, true},
		{"both", tokenHeader + "    x-a: b\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseApp(t, tc.lines).App.HasMetadata(); got != tc.want {
				t.Errorf("HasMetadata = %v, want %v", got, tc.want)
			}
		})
	}
}

// Ground: contract (spec §1, the surface) — without a file reference ReadMetadata
// is a copy of Metadata, not the map itself: the sender publishes its result and
// never changes it, and a caller changing what it got must not reach the config.
func TestMetadataFile_ReadMetadataWithoutFilesIsACopy(t *testing.T) {
	cfg := parseApp(t, "  metadata:\n    x-a: b\n")

	first, files, err := cfg.App.ReadMetadata()
	if err != nil {
		t.Fatalf("ReadMetadata: %v", err)
	}
	if !maps.Equal(first, map[string]string{"x-a": "b"}) || len(files) != 0 {
		t.Fatalf("ReadMetadata = %v, %v, want a copy of Metadata and no files", first, files)
	}

	first["x-a"] = "changed"
	first["x-new"] = "z"

	if cfg.App.Metadata["x-a"] != "b" || len(cfg.App.Metadata) != 1 {
		t.Errorf("Metadata %v changed through the returned map", cfg.App.Metadata)
	}

	second, _, err := cfg.App.ReadMetadata()
	if err != nil {
		t.Fatalf("second ReadMetadata: %v", err)
	}
	if !maps.Equal(second, map[string]string{"x-a": "b"}) {
		t.Errorf("second ReadMetadata = %v, want a fresh copy", second)
	}

	none, noFiles, err := parseApp(t, "").App.ReadMetadata()
	if err != nil || len(none) != 0 || len(noFiles) != 0 {
		t.Errorf("ReadMetadata without metadata = %v, %v, %v, want nothing and no error", none, noFiles, err)
	}
}

// Ground: contract (spec §1, the surface) — every call reads the files now and
// returns a new map: a file rewritten between two calls shows in the second and
// not in the first map. All files are read each time, and one that fails fails
// the call whichever header it belongs to.
func TestMetadataFile_ReadMetadataReadsAgainAndReadsEveryFile(t *testing.T) {
	inTempDir(t)
	putFile(t, "a.txt", "v1\n")
	putFile(t, "b.txt", "w1")

	cfg := parseApp(t, "  metadata:\n    x-a: 'Bearer ${file:a.txt}'\n    x-b: '${file:b.txt}'\n")

	first, files, err := cfg.App.ReadMetadata()
	if err != nil {
		t.Fatalf("first ReadMetadata: %v", err)
	}
	if want := map[string]string{"x-a": "Bearer v1", "x-b": "w1"}; !maps.Equal(first, want) {
		t.Errorf("first map = %v, want %v", first, want)
	}

	if want := []config.FileValue{{Path: "a.txt", Value: "v1"}, {Path: "b.txt", Value: "w1"}}; !slices.Equal(files, want) {
		t.Errorf("first files = %v, want %v, in the order of the keys", files, want)
	}

	putFile(t, "a.txt", "v2\n")

	second, _, err := cfg.App.ReadMetadata()
	if err != nil {
		t.Fatalf("second ReadMetadata: %v", err)
	}
	if want := map[string]string{"x-a": "Bearer v2", "x-b": "w1"}; !maps.Equal(second, want) {
		t.Errorf("second map = %v, want %v", second, want)
	}
	if first["x-a"] != "Bearer v1" {
		t.Errorf("the first map changed to %q after the file was rewritten", first["x-a"])
	}

	if err = os.Remove("b.txt"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	_, _, err = cfg.App.ReadMetadata()
	if !errors.Is(err, config.ErrInvalidMetadata) || !strings.Contains(err.Error(), `key "x-b": file b.txt: `) {
		t.Errorf("error = %v, want the read of x-b to fail the call", err)
	}
}

// Ground: contract (spec §3.1, round 1 F2) — headers are read by ascending key and
// the first failure is returned, the rest not read: the last_error of a report
// must not change from run to run with the order a map is walked in.
func TestMetadataFile_TheFirstFailureInKeyOrderIsReported(t *testing.T) {
	inTempDir(t)

	cfg := parseApp(t, "  metadata:\n    x-b: '${file:nob.txt}'\n    x-a: '${file:noa.txt}'\n")

	for i := range 20 {
		_, _, err := cfg.App.ReadMetadata()
		if err == nil {
			t.Fatalf("call %d: ReadMetadata read files that are not there", i)
		}
		if !strings.Contains(err.Error(), `key "x-a": file noa.txt: `) || strings.Contains(err.Error(), "nob.txt") {
			t.Fatalf("call %d: error %q, want the failure of x-a alone", i, err)
		}
	}
}
