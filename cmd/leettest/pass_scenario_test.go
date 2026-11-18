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
	"context"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/yhgrwav/leettest/internal/cli"
	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

// limitNames are the names methodLimits gave, in its order.
func limitNames(limits []threshold.MethodLimits) []string {
	names := make([]string, len(limits))
	for i, l := range limits {
		names[i] = l.Name
	}

	return names
}

// Test 8 (reviewer round 0, scenario-pass, unit U2). The limits of a config are
// named by the row key the engine gives each call and step (engine.RowKeys), in
// the order of the rows: a scenario's head, then its steps, then the next
// call. A repeated method is "<method> (<scenario>, step <n>)", the plain call
// of the same method stays bare.
// Ground: contract — thresholds[].method is the key of the judged row.
// Mutations: "/" + Method for the name (the repeated head is not found); the
// steps skipped; the steps before the head.
func TestMethodLimits_AreNamedByTheRowKeysInTheOrderOfTheRows(t *testing.T) {
	const pass = "      pass:\n        p99: 500ms\n        errors: 1%\n"

	for _, c := range []struct {
		name  string
		calls string
		want  []string
	}{
		{
			name: "a repeated method, a step of its own and a plain call",
			calls: "    - method: x.S/Get\n      rps: 10\n      duration: 10s\n      scenario: s\n" + pass +
				"      steps:\n        - method: x.S/Get\n          pass:\n            p99: 400ms\n            errors: 2%\n" +
				"    - method: x.S/List\n      rps: 10\n      duration: 10s\n" + pass,
			want: []string{"/x.S/Get (s, step 1)", "/x.S/Get (s, step 2)", "/x.S/List"},
		},
		{
			name: "methods that are not repeated stay bare, a step without pass has no entry",
			calls: "    - method: x.S/Create\n      rps: 10\n      duration: 10s\n      scenario: s\n" +
				"      steps:\n        - method: x.S/Pay\n          pass:\n            p99: 400ms\n            errors: 2%\n" +
				"        - method: x.S/Check\n",
			want: []string{"/x.S/Pay"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte("app:\n  target:\n    ip: localhost\n    port: 50051\nload:\n  calls:\n" + c.calls))
			if err != nil {
				t.Fatalf("config: %v", err)
			}
			calls, err := cli.CallsFromConfig(cfg)
			if err != nil {
				t.Fatalf("calls: %v", err)
			}

			got := limitNames(methodLimits(cfg, calls))

			if !slices.Equal(got, c.want) {
				t.Errorf("names = %q, want %q", got, c.want)
			}

			keys := engine.RowKeys(calls)
			for _, name := range got {
				found := false
				for _, row := range keys {
					found = found || slices.Contains(row, name)
				}
				if !found {
					t.Errorf("name %q is no row key of the run: %q", name, keys)
				}
			}
		})
	}
}

// passHealth fails a Check whose service is "bad" and answers any other.
type passHealth struct {
	grpc_health_v1.UnimplementedHealthServer
}

func (passHealth) Check(_ context.Context, req *grpc_health_v1.HealthCheckRequest) (
	*grpc_health_v1.HealthCheckResponse, error,
) {
	if req.GetService() == "bad" {
		return nil, status.Error(codes.Internal, "bad")
	}

	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

// passUnary answers UnaryCall, every second one with an error when failHalf.
type passUnary struct {
	testgrpc.UnimplementedTestServiceServer

	failHalf bool
	n        atomic.Int64
}

func (p *passUnary) UnaryCall(context.Context, *testgrpc.SimpleRequest) (*testgrpc.SimpleResponse, error) {
	if p.failHalf && p.n.Add(1)%2 == 0 {
		return nil, status.Error(codes.Internal, "half")
	}

	return &testgrpc.SimpleResponse{}, nil
}

// startPassTarget is a target with reflection whose Check fails for the
// service "bad" and whose UnaryCall fails every second call when failHalf.
func startPassTarget(t *testing.T, failHalf bool) string {
	t.Helper()

	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(srv, passHealth{})
	testgrpc.RegisterTestServiceServer(srv, &passUnary{failHalf: failHalf})
	reflection.Register(srv)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	return lis.Addr().String()
}

// thresholdRows are the thresholds of the JSON report in the report's order.
func thresholdRows(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()

	raw, ok := jsonAt(t, out, "thresholds").([]any)
	if !ok {
		t.Fatalf("thresholds = %v, want an array", out["thresholds"])
	}

	rows := make([]map[string]any, len(raw))
	for i := range raw {
		rows[i] = raw[i].(map[string]any)
	}

	return rows
}

// skipIfARowWasLeftToTheGenerator skips a run where a late generator made a limit
// unknown (reason generator) while another row's miss kept the exit at 4: the
// run could not test that limit, as skipIfTheGeneratorDecided says for exit 2.
func skipIfARowWasLeftToTheGenerator(t *testing.T, out map[string]any, rows []map[string]any) {
	t.Helper()

	for _, row := range rows {
		if row["verdict"] == "unknown" && row["reason"] == "generator" {
			t.Skipf("the generator was late, the run could not test %v: start-lag p99 %s", row["method"], lagP99(out))
		}
	}
}

// methodRow is the methods[] entry with the key.
func methodRow(t *testing.T, out map[string]any, key string) map[string]any {
	t.Helper()

	for _, m := range jsonAt(t, out, "methods").([]any) {
		if row := m.(map[string]any); row["method"] == key {
			return row
		}
	}
	t.Fatalf("no row %q in methods", key)

	return nil
}

// Test 9. Create then Pay: Pay fails every second call, the head never. The
// head's limit holds, the step's does not: exit 4, the miss is on Pay's row
// and judged on Pay's own calls, not on the head's.
// Ground: contract — steps[k].pass judges the step's row; the head's pass the head's.
// Mutations: Step.Pass not filled; the steps skipped in methodLimits.
func TestRun_APassOnAStepJudgesThatStepsRow(t *testing.T) {
	addr := startPassTarget(t, true)
	cfg := dryConfig(t, addr, "", "    - method: "+checkMethod+"\n      rps: 40\n      duration: 1s\n      scenario: pay\n"+
		"      pass:\n        errors: 1%\n"+
		"      steps:\n        - method: "+unaryCallMethod+"\n          pass:\n            errors: 1%\n")

	res := runCLI(t.Context(), t, 30*time.Second, "-output", "json", "-c", cfg)
	out := decodeOnly(t, res.stdout)

	skipIfTheGeneratorDecided(t, res, out)

	rows := thresholdRows(t, out)
	skipIfARowWasLeftToTheGenerator(t, out, rows)
	wantExit(t, res, 4)

	if len(rows) != 2 {
		t.Fatalf("%d thresholds, want 2: the head's errors and the step's", len(rows))
	}
	if rows[0]["method"] != "/"+checkMethod || rows[0]["verdict"] != "pass" {
		t.Errorf("thresholds[0] = %v %v (reason %v, outcome %v, tail %v, lag p99 %s), want the head's row, pass",
			rows[0]["method"], rows[0]["verdict"], rows[0]["reason"], out["outcome"], out["tail_wait_cause"], lagP99(out))
	}
	step := rows[1]
	if step["method"] != "/"+unaryCallMethod || step["verdict"] != "fail" {
		t.Errorf("thresholds[1] = %v %v, want the step's row, fail", step["method"], step["verdict"])
	}
	if sent := methodRow(t, out, "/"+unaryCallMethod)["sent"]; step["total"] != sent {
		t.Errorf("thresholds[1].total = %v, want what the step sent, %v", step["total"], sent)
	}
}

// Test 10. A method that is in the run twice: Check as the head and as the last
// step, the last one fails. The limit's method is the key of the row the report
// has, "… (s, step 3)", and the miss is found: exit 4, the head's row passes.
// Ground: contract — thresholds[].method equals methods[].method of the judged row.
// Mutation: "/" + Method for the name (no row of that name: unknown, exit 2).
func TestRun_APassOnARepeatedMethodIsFoundByTheRowKey(t *testing.T) {
	addr := startPassTarget(t, false)
	cfg := dryConfig(t, addr, "", "    - method: "+checkMethod+"\n      rps: 40\n      duration: 1s\n      scenario: s\n"+
		"      pass:\n        errors: 1%\n"+
		"      steps:\n        - method: "+unaryCallMethod+"\n"+
		"        - method: "+checkMethod+"\n          data:\n            service: bad\n"+
		"          pass:\n            errors: 1%\n")

	res := runCLI(t.Context(), t, 30*time.Second, "-output", "json", "-c", cfg)
	out := decodeOnly(t, res.stdout)

	skipIfTheGeneratorDecided(t, res, out)

	rows := thresholdRows(t, out)
	skipIfARowWasLeftToTheGenerator(t, out, rows)
	wantExit(t, res, 4)

	head, last := "/"+checkMethod+" (s, step 1)", "/"+checkMethod+" (s, step 3)"
	if len(rows) != 2 {
		t.Fatalf("%d thresholds, want 2", len(rows))
	}
	for i, want := range []struct{ method, verdict string }{{head, "pass"}, {last, "fail"}} {
		if rows[i]["method"] != want.method || rows[i]["verdict"] != want.verdict {
			t.Errorf("thresholds[%d] = %v %v (reason %v, outcome %v, tail %v, lag p99 %s), want %s %s",
				i, rows[i]["method"], rows[i]["verdict"], rows[i]["reason"], out["outcome"], out["tail_wait_cause"], lagP99(out),
				want.method, want.verdict)
		}
		methodRow(t, out, want.method)
	}
}

// Test 11. The head fails every call, so no call reaches step 2: its limit has
// nothing to judge. Unknown, nothing_sent, exit 2, and the invalid run says it
// is the limits that were undecided.
// Ground: contract — a step nobody reached is unknown, not pass.
func TestRun_AStepNobodyReachedIsUndecided(t *testing.T) {
	addr := startPassTarget(t, false)
	cfg := dryConfig(t, addr, "", "    - method: "+checkMethod+"\n      rps: 40\n      duration: 1s\n      scenario: s\n"+
		"      data:\n        service: bad\n"+
		"      steps:\n        - method: "+unaryCallMethod+"\n          pass:\n            errors: 1%\n")

	res := runCLI(t.Context(), t, 30*time.Second, "-output", "json", "-c", cfg)
	out := decodeOnly(t, res.stdout)

	skipIfTheGeneratorDecided(t, res, out)
	wantExit(t, res, 2)

	rows := thresholdRows(t, out)
	if len(rows) != 1 || rows[0]["verdict"] != "unknown" || rows[0]["reason"] != "nothing_sent" {
		t.Errorf("thresholds = %v, want one unknown nothing_sent", rows)
	}
	if got, _ := jsonAt(t, out, "invalid_reasons").([]any); !slices.Equal(got, []any{"thresholds_undecided"}) {
		t.Errorf("invalid_reasons = %v, want [thresholds_undecided]", got)
	}
}
