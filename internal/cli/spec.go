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
	"path/filepath"
	"strings"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// CallsFromConfig turns the calls of a config into engine calls. A call with a
// dataset gets one empty payload per record and a counter of its own; its
// records go in later, in AttachData. A config that names a dataset whose
// records were never read (one from config.Parse) is an error here.
// STUB of the red commit: datasets are not looked at yet.
func CallsFromConfig(cfg *config.MasterConfig) ([]engine.Call, error) {
	calls := make([]engine.Call, 0, len(cfg.Load.Calls))

	for _, call := range cfg.Load.Calls {
		calls = append(calls, engine.Call{
			// gRPC sends /pkg.Service/Method; the config writes it without the slash.
			Method:  "/" + call.Method,
			Timeout: call.Timeout,
			Stages: []engine.Stage{{
				StartRPS:  int(call.RPS),
				TargetRPS: int(call.RPS),
				Duration:  call.Duration,
			}},
		})
	}

	return calls, nil
}

// displayMethod shows a method the way the config names it: the engine carries
// the gRPC path, which differs only by the leading slash.
func displayMethod(method string) string {
	return strings.TrimPrefix(method, "/")
}

// ServiceLabel is what the header calls the run: the name the config gives it;
// otherwise the one service all its methods belong to, by its short name;
// otherwise the config file's name, which is the collection name by default.
func ServiceLabel(cfg *config.MasterConfig, configPath string) string {
	if cfg.Name != nil {
		return *cfg.Name
	}

	service := ""

	for _, call := range cfg.Load.Calls {
		full, _, _ := strings.Cut(call.Method, "/")
		if service != "" && full != service {
			return strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))
		}
		service = full
	}

	return service[strings.LastIndex(service, ".")+1:]
}
