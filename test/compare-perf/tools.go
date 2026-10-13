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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	method    = "grpc.health.v1.Health/Check"
	standPort = 50051
	// callTimeout is every tool's per-call deadline.
	callTimeout = 5 * time.Second
	// readyLine is what the stand prints once it listens, on stderr.
	readyLine = "stand on "
)

// Tool names, as -tools and the tables spell them.
const (
	leettest = "leettest"
	ghz      = "ghz"
	k6       = "k6"
)

// setup is everything one run is told: the same for every tool, except what a
// tool needs to be open-model, which toolCommand and the generated inputs
// spell out.
type setup struct {
	Rate     int
	Duration time.Duration
	// Delay is how long the stand holds every answer.
	Delay time.Duration

	ToolCPUs, StandCPUs int
	// K6VUs is k6's pre-allocated and maximum virtual users: k6 opens one
	// connection per VU, the others one in all.
	K6VUs int

	Image   string
	K6Image string
}

// toolResult is what a tool says about its own run.
type toolResult struct {
	Sent, Failed int
	P50, P99     time.Duration
	// Note carries what the tool called its own outcome, if it does.
	Note string
}

// leettestConfig is the YAML of an open-model run at the stand.
func leettestConfig(s setup, ip string) string {
	return fmt.Sprintf("app:\n  target:\n    ip: %s\n    port: %d\n  tls: false\n  connections: 1\n\nload:\n  calls:\n"+
		"    - method: %s\n      rps: %d\n      duration: %s\n      timeout: %s\n",
		ip, standPort, method, s.Rate, s.Duration, callTimeout)
}

// ghzArgs: --async with --rps is ghz's open model; one connection, as the
// existing ghz comparison. -c is ghz's own default, written out.
func ghzArgs(s setup, ip string) []string {
	return []string{
		"--insecure", "--call", method, "-d", "{}",
		"--rps", strconv.Itoa(s.Rate), "-z", s.Duration.String(), "-t", callTimeout.String(),
		"--duration-stop", "wait", "--connections", "1", "-c", "50", "--async",
		"-O", "json", "-o", "/data/ghz.json",
		fmt.Sprintf("%s:%d", ip, standPort),
	}
}

// k6Script is constant-arrival-rate, k6's open model, over k6/net/grpc with
// reflection, like the other two.
func k6Script(s setup, ip string) string {
	return fmt.Sprintf(`import grpc from 'k6/net/grpc';
import { Counter } from 'k6/metrics';

const client = new grpc.Client();
const errors = new Counter('perf_errors');

export const options = {
  scenarios: {
    open: {
      executor: 'constant-arrival-rate',
      rate: %d,
      timeUnit: '1s',
      duration: '%s',
      preAllocatedVUs: %d,
      maxVUs: %d,
    },
  },
  summaryTrendStats: ['count', 'min', 'p(50)', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  if (__ITER === 0) {
    client.connect('%s:%d', { plaintext: true, reflect: true, timeout: '%s' });
  }
  const res = client.invoke('%s', {}, { timeout: '%s' });
  if (!res || res.status !== grpc.StatusOK) {
    errors.add(1);
  }
}

export function handleSummary(data) {
  return { '/data/k6-summary.json': JSON.stringify(data) };
}
`, s.Rate, s.Duration, s.K6VUs, s.K6VUs, ip, standPort, callTimeout, method, callTimeout)
}

// leettestInFlight is the in-flight cap LeetTest is given. Its default (5000)
// refuses a run whose rate × timeout plus the release margin (100 ms of
// calls) and one edge slot would not fit, which every rate above ~980 with a
// 5 s timeout does; the cap is raised to fit, with two slots to spare. Nothing
// is held that long against a stand answering at once, so the cap is not
// reached; ghz and k6 have no such check.
func leettestInFlight(s setup) int {
	const releaseMargin = 100 * time.Millisecond

	return int(math.Ceil(float64(s.Rate)*(callTimeout+releaseMargin).Seconds())) + 2
}

// toolCommand is the container image and the command inside it.
func toolCommand(tool string, s setup, ip string) (image string, cmd []string, err error) {
	switch tool {
	case leettest:
		return s.Image, []string{
			"/perf/leettest", "-c", "/data/leettest.yaml", "-output", "json",
			"-max-in-flight", strconv.Itoa(leettestInFlight(s)),
		}, nil
	case ghz:
		return s.Image, append([]string{"/perf/ghz"}, ghzArgs(s, ip)...), nil
	case k6:
		return s.K6Image, []string{"run", "--quiet", "--no-color", "/data/k6.js"}, nil
	}

	return "", nil, fmt.Errorf("unknown tool %q", tool)
}

// toolRunArgs is the docker run line of a tool: one core, the network of the
// stand, the same Go scheduler width for all three.
func toolRunArgs(name, network, tool string, s setup, ip, bin, data string) ([]string, error) {
	image, cmd, err := toolCommand(tool, s, ip)
	if err != nil {
		return nil, err
	}

	args := []string{
		"run", "--rm", "--name", name, "--network", network,
		fmt.Sprintf("--cpus=%d", s.ToolCPUs), "-e", fmt.Sprintf("GOMAXPROCS=%d", s.ToolCPUs),
	}
	if tool == k6 {
		args = append(args, "-e", "K6_NO_USAGE_REPORT=true")
	}
	args = append(args, "-v", bin+":/perf:ro", "-v", data+":/data", image)

	return append(args, cmd...), nil
}

// standRunArgs is the docker run line of the stand, detached.
func standRunArgs(name, network string, s setup, bin, data string) []string {
	return []string{
		"run", "-d", "--name", name, "--network", network, fmt.Sprintf("--cpus=%d", s.StandCPUs),
		"-v", bin + ":/perf:ro", "-v", data + ":/data", s.Image,
		"/perf/stand", "-addr", fmt.Sprintf("0.0.0.0:%d", standPort), "-delay", s.Delay.String(),
		"-arrivals", "/data/arrivals.txt",
	}
}

// parseLeetTestJSON reads the JSON report of one method.
func parseLeetTestJSON(data []byte) (toolResult, error) {
	var rep struct {
		Outcome string `json:"outcome"`
		Methods []struct {
			Method  string `json:"method"`
			Sent    int    `json:"sent"`
			Failed  int    `json:"failed"`
			Latency struct {
				P50 *struct {
					US int64 `json:"us"`
				} `json:"p50"`
				P99 *struct {
					US int64 `json:"us"`
				} `json:"p99"`
			} `json:"latency"`
		} `json:"methods"`
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		return toolResult{}, fmt.Errorf("leettest json: %w", err)
	}

	for _, m := range rep.Methods {
		if strings.TrimPrefix(m.Method, "/") != method { // the report writes "/grpc.health.v1.Health/Check"
			continue
		}
		if m.Latency.P50 == nil || m.Latency.P99 == nil {
			return toolResult{}, fmt.Errorf("leettest reported no latency for %s (outcome %s)", method, rep.Outcome)
		}

		return toolResult{
			Sent: m.Sent, Failed: m.Failed, Note: rep.Outcome,
			P50: time.Duration(m.Latency.P50.US) * time.Microsecond,
			P99: time.Duration(m.Latency.P99.US) * time.Microsecond,
		}, nil
	}

	return toolResult{}, fmt.Errorf("no %s in the leettest report (outcome %s)", method, rep.Outcome)
}

// parseGhzJSON reads ghz's report; its percentiles are nearest rank over the
// successful calls' exact latencies.
func parseGhzJSON(data []byte) (toolResult, error) {
	var rep struct {
		Count               int            `json:"count"`
		ErrorDistribution   map[string]int `json:"errorDistribution"`
		LatencyDistribution []struct {
			Percentage int           `json:"percentage"`
			Latency    time.Duration `json:"latency"`
		} `json:"latencyDistribution"`
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		return toolResult{}, fmt.Errorf("ghz json: %w", err)
	}

	r := toolResult{Sent: rep.Count}
	for _, n := range rep.ErrorDistribution {
		r.Failed += n
	}
	found := 0
	for _, l := range rep.LatencyDistribution {
		switch l.Percentage {
		case 50:
			r.P50, found = l.Latency, found+1
		case 99:
			r.P99, found = l.Latency, found+1
		}
	}
	if found != 2 {
		return r, errors.New("ghz reported no p50 and p99")
	}

	return r, nil
}

// parseK6Summary reads handleSummary's data: k6's trend percentiles are exact
// values with linear interpolation between neighbours, in milliseconds. k6
// v2.3.0 has no grpc_reqs metric; the calls it made are the count of
// grpc_req_duration, which the script's summaryTrendStats asks for. The
// handleSummary data has the old form, metrics.<name>.{type,contains,values}
// (--new-machine-readable-summary is off by default); values beside the
// metric's other fields, a form k6 does not produce, are read too.
func parseK6Summary(data []byte) (toolResult, error) {
	var rep struct {
		Metrics map[string]map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		return toolResult{}, fmt.Errorf("k6 json: %w", err)
	}

	value := func(metric, key string) (float64, bool) {
		m, ok := rep.Metrics[metric]
		if !ok {
			return 0, false
		}
		if inner, nested := m["values"].(map[string]any); nested {
			m = inner
		}
		v, found := m[key].(float64)

		return v, found
	}

	var r toolResult
	p50, ok50 := value("grpc_req_duration", "p(50)")
	p99, ok99 := value("grpc_req_duration", "p(99)")
	if !ok50 || !ok99 {
		return r, errors.New("k6 reported no p(50) and p(99) of grpc_req_duration")
	}
	r.P50 = time.Duration(p50 * float64(time.Millisecond))
	r.P99 = time.Duration(p99 * float64(time.Millisecond))

	reqs, ok := value("grpc_req_duration", "count")
	if !ok {
		return r, errors.New("k6 reported no count of grpc_req_duration")
	}
	r.Sent = int(reqs)
	if errs, ok := value("perf_errors", "count"); ok {
		r.Failed = int(errs)
	}
	if dropped, ok := value("dropped_iterations", "count"); ok && dropped > 0 {
		r.Note = fmt.Sprintf("dropped %d iterations", int(dropped))
	}

	return r, nil
}
