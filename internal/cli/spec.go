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
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
)

// CallsFromConfig turns the calls of a config into engine calls. A call with a
// dataset gets one empty payload per record and a counter of its own, made
// here once for the process: every run of a search goes on with it. The
// records go in later, in AttachData. A config that names a dataset whose
// records were never read (one from config.Parse) is an error here, never a
// call that sends empty messages under the file's name.
func CallsFromConfig(cfg *config.MasterConfig) ([]engine.Call, error) {
	calls := make([]engine.Call, 0, len(cfg.Load.Calls))

	var errs []error

	for i := range cfg.Load.Calls {
		call := &cfg.Load.Calls[i]
		built := engine.Call{
			// gRPC sends /pkg.Service/Method; the config writes it without the slash.
			Method:  "/" + call.Method,
			Timeout: call.Timeout,
			Stages: []engine.Stage{{
				StartRPS:  int(call.RPS),
				TargetRPS: int(call.RPS),
				Duration:  call.Duration,
			}},
		}
		if cfg.Load.Ramp > 0 {
			// The ramp comes before the call's duration: it does not shorten it.
			built.Stages = append([]engine.Stage{{
				StartRPS:  0,
				TargetRPS: int(call.RPS),
				Duration:  cfg.Load.Ramp,
			}}, built.Stages...)
		}

		if call.Dataset != "" {
			if len(call.Records) == 0 {
				errs = append(errs, fmt.Errorf("%s: dataset %s: not read; load the config with LoadFile",
					call.Method, call.Dataset))

				continue
			}

			built.Payloads = make([][]byte, len(call.Records))
			counter, err := datasetCounter(call)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", call.Method, err))

				continue
			}
			built.Dataset = &engine.DatasetRef{File: call.Dataset, SHA256: call.DatasetSHA256, Counter: counter, Once: call.DatasetReuse != nil && !*call.DatasetReuse}
		}

		calls = append(calls, built)
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	return calls, nil
}

// ExcludedWindow is the time from the start of a run that its statistics leave
// out: the ramp and then the warm-up. It is the engine's Warmup option and
// the live view's, never the report's echo of load.warmup.
func ExcludedWindow(cfg *config.MasterConfig) time.Duration {
	return cfg.Load.Ramp + cfg.Load.Warmup
}

// datasetCounter is the counter of a dataset call: the file's order, or a
// random one. A random order without a seed in the config draws one here, once
// per call, so that the report can name it.
func datasetCounter(call *config.Call) (*engine.RecordCounter, error) {
	if call.DatasetOrder == nil || *call.DatasetOrder != config.DatasetOrderRandom {
		return engine.NewRecordCounter(), nil
	}

	if call.DatasetSeed != nil {
		return engine.NewShuffledCounter(len(call.Records), uint64(*call.DatasetSeed)), nil
	}

	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("draw a seed: %w", err)
	}

	return engine.NewShuffledCounter(len(call.Records), binary.LittleEndian.Uint64(raw[:])&engine.MaxShuffleSeed), nil
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

	for i := range cfg.Load.Calls {
		full, _, _ := strings.Cut(cfg.Load.Calls[i].Method, "/")
		if service != "" && full != service {
			base := filepath.Base(configPath)
			if label := strings.TrimSuffix(base, filepath.Ext(base)); label != "" {
				return label
			}

			return base
		}
		service = full
	}

	return service[strings.LastIndex(service, ".")+1:]
}
