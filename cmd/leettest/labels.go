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
	"maps"
	"strings"

	"github.com/yhgrwav/leettest/pkg/config"
)

// labelFlags collects every -label as given; parseLabels checks them once the
// flags are parsed, so that the error is the command's and not the flag
// package's.
type labelFlags []string

func (l *labelFlags) String() string { return strings.Join(*l, ",") }

func (l *labelFlags) Set(value string) error {
	*l = append(*l, value)

	return nil
}

// parseLabels reads the -label arguments, key=value each, the first "=" ends
// the key. A key given twice is an error.
func parseLabels(args labelFlags) (map[string]string, error) {
	labels := make(map[string]string, len(args))

	for _, arg := range args {
		key, value, found := strings.Cut(arg, "=")
		if !found {
			return nil, fmt.Errorf("-label %.64q: want key=value", arg)
		}
		if err := config.CheckLabel(key, value); err != nil {
			return nil, fmt.Errorf("-label %.100q: %w", key, err)
		}
		if _, twice := labels[key]; twice {
			return nil, fmt.Errorf("-label %q is given twice", key)
		}
		labels[key] = value
	}

	return labels, nil
}

// mergeLabels is the config's labels with the flags' on top: a key in both
// takes the flag's value.
func mergeLabels(fromConfig, fromFlags map[string]string) map[string]string {
	merged := make(map[string]string, len(fromConfig)+len(fromFlags))
	maps.Copy(merged, fromConfig)
	maps.Copy(merged, fromFlags)

	return merged
}
