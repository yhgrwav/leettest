package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func testSetup() setup {
	return setup{
		Rate: 2500, Duration: 10 * time.Second, Delay: 20 * time.Millisecond,
		ToolCPUs: 1, StandCPUs: 4, K6VUs: 100, Image: "debian:stable-slim", K6Image: "grafana/k6:2.3.0",
	}
}

func TestParseLeetTestJSON(t *testing.T) {
	data := `{"outcome":"ok","methods":[{"method":"other/Method","sent":1,"failed":0,"latency":{"p50":{"us":1},"p99":{"us":2}}},
	{"method":"grpc.health.v1.Health/Check","sent":9990,"failed":10,"latency":{"p50":{"us":20500,"lower_bound":false},"p99":{"us":23100}}}]}`

	r, err := parseLeetTestJSON([]byte(data))
	if err != nil {
		t.Fatal(err)
	}

	if r.Sent != 9990 || r.Failed != 10 || r.P50 != 20500*time.Microsecond || r.P99 != 23100*time.Microsecond || r.Note != "ok" {
		t.Errorf("result = %+v", r)
	}
}

func TestParseLeetTestJSON_NoLatencyOrNoMethodIsAnError(t *testing.T) {
	for name, data := range map[string]string{
		"no latency": `{"outcome":"invalid","methods":[{"method":"grpc.health.v1.Health/Check","sent":5,"latency":{}}]}`,
		"no method":  `{"outcome":"ok","methods":[]}`,
		"not json":   `no`,
	} {
		if _, err := parseLeetTestJSON([]byte(data)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestParseGhzJSON(t *testing.T) {
	data := `{"count":10000,"errorDistribution":{"Unavailable":3,"DeadlineExceeded":2},
	"latencyDistribution":[{"percentage":50,"latency":20400000},{"percentage":90,"latency":21000000},{"percentage":99,"latency":25000000}]}`

	r, err := parseGhzJSON([]byte(data))
	if err != nil {
		t.Fatal(err)
	}

	if r.Sent != 10000 || r.Failed != 5 || r.P50 != 20400*time.Microsecond || r.P99 != 25*time.Millisecond {
		t.Errorf("result = %+v", r)
	}
}

func TestParseGhzJSON_NoPercentilesIsAnError(t *testing.T) {
	if _, err := parseGhzJSON([]byte(`{"count":5,"latencyDistribution":[{"percentage":50,"latency":1}]}`)); err == nil {
		t.Error("want an error: p99 is missing, a zero would read as an instant tool")
	}
}

func TestParseK6Summary_ReadsBothShapesOfValues(t *testing.T) {
	nested := `{"metrics":{"grpc_req_duration":{"values":{"p(50)":20.5,"p(99)":23.25}},
	"grpc_reqs":{"values":{"count":9000}},"perf_errors":{"values":{"count":4}},"dropped_iterations":{"values":{"count":1000}}}}`
	flat := `{"metrics":{"grpc_req_duration":{"p(50)":20.5,"p(99)":23.25},"grpc_reqs":{"count":9000}}}`

	for name, data := range map[string]string{"nested": nested, "flat": flat} {
		r, err := parseK6Summary([]byte(data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r.Sent != 9000 || r.P50 != 20500*time.Microsecond || r.P99 != 23250*time.Microsecond {
			t.Errorf("%s: result = %+v", name, r)
		}
	}

	r, _ := parseK6Summary([]byte(nested))
	if r.Failed != 4 || r.Note != "dropped 1000 iterations" {
		t.Errorf("nested: failed %d, note %q, want 4 and the dropped iterations named", r.Failed, r.Note)
	}
}

func TestParseK6Summary_MissingMetricsAreErrors(t *testing.T) {
	for name, data := range map[string]string{
		"no trend":  `{"metrics":{"grpc_reqs":{"count":5}}}`,
		"no count":  `{"metrics":{"grpc_req_duration":{"p(50)":1,"p(99)":2}}}`,
		"no metric": `{}`,
	} {
		if _, err := parseK6Summary([]byte(data)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// Every tool gets the same box: one core, the same Go scheduler width, the
// stand's network, a rate and a duration taken from the same setup.
func TestToolRunArgs_EveryToolGetsTheSameBox(t *testing.T) {
	s := testSetup()
	for _, tool := range []string{leettest, ghz, k6} {
		args, err := toolRunArgs("c", "net", tool, s, "10.0.0.2", "/b", "/d")
		if err != nil {
			t.Fatal(err)
		}
		line := " " + strings.Join(args, " ") + " "

		for _, want := range []string{" --cpus=1 ", " GOMAXPROCS=1 ", " --network net ", " --rm "} {
			if !strings.Contains(line, want) {
				t.Errorf("%s: %q lacks %q", tool, line, want)
			}
		}
	}
}

func TestToolRunArgs_AToolThatIsNotKnownIsAnError(t *testing.T) {
	if _, err := toolRunArgs("c", "net", "wrk", testSetup(), "10.0.0.2", "/b", "/d"); err == nil {
		t.Error("want an error")
	}
}

func TestGhzArgs_AreOpenModelOnOneConnection(t *testing.T) {
	args := ghzArgs(testSetup(), "10.0.0.2")

	for _, want := range [][]string{{"--async"}, {"--rps", "2500"}, {"-z", "10s"}, {"--connections", "1"}, {"-t", "5s"}} {
		if !containsRun(args, want) {
			t.Errorf("args %v lack %v", args, want)
		}
	}
	if args[len(args)-1] != "10.0.0.2:50051" {
		t.Errorf("target = %q, want the stand's IP and port last", args[len(args)-1])
	}
}

func TestK6Script_IsConstantArrivalRateAtTheSetupRate(t *testing.T) {
	script := k6Script(testSetup(), "10.0.0.2")

	for _, want := range []string{
		"'constant-arrival-rate'", "rate: 2500,", "duration: '10s'", "preAllocatedVUs: 100", "maxVUs: 100",
		"client.connect('10.0.0.2:50051'", "reflect: true", "'grpc.health.v1.Health/Check'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
}

func TestLeetTestConfig_IsOneConnectionAtTheSetupRate(t *testing.T) {
	cfg := leettestConfig(testSetup(), "10.0.0.2")

	for _, want := range []string{"ip: 10.0.0.2", "port: 50051", "connections: 1", "rps: 2500", "duration: 10s", "timeout: 5s"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config lacks %q:\n%s", want, cfg)
		}
	}
}

func TestStandRunArgs_BiggerBoxAndTheArrivalsRecord(t *testing.T) {
	args := standRunArgs("s", "net", testSetup(), "/b", "/d")
	line := " " + strings.Join(args, " ") + " "

	for _, want := range []string{" --cpus=4 ", " -arrivals /data/arrivals.txt ", " -delay 20ms ", " --network net "} {
		if !strings.Contains(line, want) {
			t.Errorf("%q lacks %q", line, want)
		}
	}
}

// The line is what the stand prints on stderr once it listens.
func TestStandIsReady(t *testing.T) {
	if !standIsReady("stand on 0.0.0.0:50051 (plaintext): grpc.health.v1.Health/Check, wallet.v1.WalletService/GetBalance and /Transfer\n") {
		t.Error("the stand's ready line was not recognised")
	}
	if standIsReady("") {
		t.Error("an empty log is not ready")
	}
}

func containsRun(args, run []string) bool {
	for i := 0; i+len(run) <= len(args); i++ {
		if slices.Equal(args[i:i+len(run)], run) {
			return true
		}
	}

	return false
}
