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

package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// metadataKey is what HTTP/2 carries as a header name once lowercased.
var metadataKey = regexp.MustCompile(`^[0-9a-z_.-]+$`)

// transportOwned are the headers grpc-go writes on every call; a second one
// from the config would go out next to its own.
var transportOwned = map[string]bool{"content-type": true, "te": true}

// notInHTTP2 are the connection-specific fields RFC 9113 section 8.2.2 forbids.
var notInHTTP2 = map[string]bool{
	"connection": true, "keep-alive": true, "proxy-connection": true,
	"transfer-encoding": true, "upgrade": true,
}

// envRef is ${NAME}; "$${" before it writes a literal "${" instead.
var envRef = regexp.MustCompile(`\$?\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnvRefs replaces every ${NAME} of value by fn(NAME) and every "$${"
// by a literal "${": the one place that knows the grammar.
func expandEnvRefs(value string, fn func(name string) string) string {
	return envRef.ReplaceAllStringFunc(value, func(ref string) string {
		if strings.HasPrefix(ref, "$$") {
			return ref[1:]
		}

		return fn(envRef.FindStringSubmatch(ref)[1])
	})
}

// SubstitutedEnvNames are the variables a metadata value reads, in the order
// they appear: the ${NAME} that metadata resolution replaces, not the
// "$${NAME}" it leaves as text. A caller that must tell what was substituted
// asks here, so that it cannot disagree with the loader.
func SubstitutedEnvNames(value string) []string {
	var names []string

	expandEnvRefs(value, func(name string) string {
		names = append(names, name)

		return ""
	})

	return names
}

// resolveMetadata lowercases keys and fills ${NAME}. Errors name the key and
// the variable, never the value: it may be a secret.
func (a *App) resolveMetadata() error {
	if len(a.RawMetadata) == 0 {
		return nil
	}

	out := make(map[string]string, len(a.RawMetadata))

	var errs []error

	for raw, value := range a.RawMetadata {
		key := strings.ToLower(raw)

		switch {
		case !metadataKey.MatchString(key):
			errs = append(errs, fmt.Errorf("%w: key %q: only letters, digits, '-', '_' and '.'", ErrInvalidMetadata, raw))
			continue
		case strings.HasPrefix(key, "grpc-"):
			errs = append(errs, fmt.Errorf("%w: key %q: grpc- is reserved by gRPC", ErrInvalidMetadata, raw))
			continue
		case transportOwned[key]:
			errs = append(errs, fmt.Errorf("%w: key %q: set by the gRPC transport", ErrInvalidMetadata, key))
			continue
		case notInHTTP2[key]:
			errs = append(errs, fmt.Errorf("%w: key %q: not allowed in HTTP/2", ErrInvalidMetadata, key))
			continue
		case strings.HasSuffix(key, "-bin"):
			errs = append(errs, fmt.Errorf("%w: key %q: binary metadata is not supported", ErrInvalidMetadata, raw))
			continue
		}

		if _, dup := out[key]; dup {
			errs = append(errs, fmt.Errorf("%w: key %q twice: keys are case-insensitive", ErrInvalidMetadata, key))
			continue
		}

		var problems []error

		resolved := expandEnvRefs(value, func(name string) string {
			v, ok := os.LookupEnv(name)
			switch {
			case !ok:
				problems = append(problems, fmt.Errorf("%w: key %q uses ${%s}, which is not set; %s", ErrInvalidMetadata, key, name, envHint(name)))
			case v == "":
				problems = append(problems, fmt.Errorf("%w: key %q uses ${%s}, which is set but empty; %s", ErrInvalidMetadata, key, name, envHint(name)))
			}

			return v
		})

		if len(problems) == 0 && !printableASCII(resolved) {
			// Where the value came from is named, not what it holds.
			problems = append(problems, fmt.Errorf("%w: key %q: the value must be printable ASCII (%%x20-%%x7E)", ErrInvalidMetadata, key))
		}

		if len(problems) > 0 {
			errs = append(errs, problems...)
			continue
		}

		out[key] = resolved
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	a.Metadata = out

	return nil
}

// envHint is the command that fixes a missing variable. <value> stays as written: what the
// variable holds is never printed.
func envHint(name string) string {
	return "set it before the run: export " + name + "=<value>"
}

// printableASCII is what gRPC carries as an ASCII header value.
func printableASCII(s string) bool {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}

	return true
}

// resolveCompression reads app.compression: gzip, or left out for none. The
// name is exact; it is the encoding's registered one.
func (a *App) resolveCompression() error {
	if a.RawCompression == nil {
		return nil
	}

	if *a.RawCompression != "gzip" {
		return fmt.Errorf("%w: %q", ErrInvalidCompression, *a.RawCompression)
	}

	a.Compression = *a.RawCompression

	return nil
}

var sizeUnits = map[string]int64{
	"b": 1, "kb": 1e3, "mb": 1e6, "gb": 1e9, "kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30,
}

var sizePattern = regexp.MustCompile(`^(\d+)\s*([A-Za-z]+)$`)

// resolveMaxResponseSize reads a size such as 16MiB. A bare number is refused:
// 16 would be read as bytes by one person and megabytes by another.
func (a *App) resolveMaxResponseSize() error {
	if a.RawMaxResponseSize == nil {
		return nil
	}

	m := sizePattern.FindStringSubmatch(strings.TrimSpace(*a.RawMaxResponseSize))
	if m == nil {
		return fmt.Errorf("%w: %q", ErrInvalidMaxResponseSize, *a.RawMaxResponseSize)
	}

	unit, ok := sizeUnits[strings.ToLower(m[2])]
	n, err := strconv.ParseInt(m[1], 10, 64)

	// grpc-go takes the limit as an int and frames a message with a 32-bit
	// length; a limit it cannot hold is refused rather than wrapped.
	if !ok || err != nil || n <= 0 || n > math.MaxInt32/unit {
		return fmt.Errorf("%w: %q", ErrInvalidMaxResponseSize, *a.RawMaxResponseSize)
	}

	a.MaxResponseBytes = int(n * unit)

	return nil
}

// Connection counts the config accepts.
const (
	minConnections = 1
	maxConnections = 256
)

// resolveConnections reads app.connections, 1 when it is left out. What is not
// a whole number (2.0, 2.5, "two") is refused like one out of range, not
// rounded or read as 1; the message prints the value as written.
func (a *App) resolveConnections() error {
	if a.RawConnections == nil {
		a.Connections = minConnections

		return nil
	}

	text := string(*a.RawConnections)
	n, err := strconv.Atoi(text)
	if err != nil || n < minConnections || n > maxConnections {
		return fmt.Errorf("%w: %s", ErrInvalidConnections, text)
	}

	a.Connections = n

	return nil
}

func (a *App) validateTLSFiles() error {
	if !a.UseTLS && (a.CA != "" || a.Cert != "" || a.Key != "" || a.ServerName != "") {
		return ErrTLSFilesWithoutTLS
	}
	if (a.Cert == "") != (a.Key == "") {
		return ErrCertWithoutKey
	}

	return nil
}

// relativeTo makes the certificate paths relative to dir.
func (a *App) relativeTo(dir string) {
	for _, p := range []*string{&a.CA, &a.Cert, &a.Key} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(dir, *p)
		}
	}
}
