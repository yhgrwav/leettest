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
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/engine"
)

const maxSeed = 9007199254740991

func randomOf(file string, records, used, usedMax int, seed uint64) *engine.DatasetReport {
	d := datasetOf(file, records, used, usedMax)
	d.Random, d.Seed = true, seed

	return d
}

// The line of a random call ends with its order and seed, exact; file order
// prints as it did, with no word of order.
// Ground: contract — the seed in the text is what repeats the run.
func TestReport_DatasetLineRandomOrder(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    *engine.DatasetReport
		want string
	}{
		{"random", randomOf("data/users.jsonl", 3, 3, 5, 42),
			"  data: 3 of 3 requests from users.jsonl, each used up to 5 times, random order, seed 42"},
		{"seed 0", randomOf("data/users.jsonl", 3, 3, 1, 0),
			"  data: 3 of 3 requests from users.jsonl, each used up to 1 time, random order, seed 0"},
		{"the largest seed, exact", randomOf("data/users.jsonl", 3, 3, 1, maxSeed),
			"  data: 3 of 3 requests from users.jsonl, each used up to 1 time, random order, seed 9007199254740991"},
		{"nothing used still names the seed", randomOf("data/users.jsonl", 3, 0, 0, 7),
			"  data: none of 3 requests from users.jsonl used, random order, seed 7"},
		{"file order has no order words", datasetOf("data/users.jsonl", 3, 3, 5),
			"  data: 3 of 3 requests from users.jsonl, each used up to 5 times"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out strings.Builder
			PrintReport(&out, "localhost:50051", reportWith(engine.MethodReport{Method: "/pkg.S/M", Dataset: tc.d}))

			if got := lineAfter(t, out.String(), "pkg.S/M "); got != tc.want {
				t.Errorf("line under the method = %q, want %q", got, tc.want)
			}
		})
	}
}

// The search prints the same line, the last run's.
// Ground: contract — the one place the search's text says what data it ran on.
func TestBreakpoint_DatasetLineRandomOrder(t *testing.T) {
	res := withDatasets(outcomes["held_all"].res, func(int) *engine.DatasetReport {
		return randomOf("data/users.jsonl", 3, 3, 7, maxSeed)
	})

	const want = "  data: 3 of 3 requests from users.jsonl, each used up to 7 times, random order, seed 9007199254740991\n"
	if out := printedSearch(res); !strings.Contains(out, want) {
		t.Errorf("no %q in:\n%s", want, out)
	}
}

// exactNumber is the value at path of out as the exact text of a JSON number.
func exactNumber(t *testing.T, raw []byte, path ...string) any {
	t.Helper()

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("json: %v", err)
	}

	return field(t, v, path...)
}

// Ground: contract — the JSON names are an interface (schema_v1.txt): order is
// "file" or "random", seed an integer or null, and the seed is exact.
func TestJSON_DatasetOrderAndSeed(t *testing.T) {
	var buf bytes.Buffer
	run := jsonRun(engine.Report{Methods: []engine.MethodReport{
		{Method: "/pkg.S/Random", Sent: 1, Dataset: randomOf("data/users.jsonl", 3, 3, 5, maxSeed)},
		{Method: "/pkg.S/File", Sent: 1, Dataset: datasetOf("data/users.jsonl", 3, 3, 5)},
		{Method: "/pkg.S/Zero", Sent: 1, Dataset: randomOf("data/users.jsonl", 3, 3, 5, 0)},
	}})
	if err := WriteJSON(&buf, run); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	methods := exactNumber(t, buf.Bytes(), "methods").([]any)

	random := field(t, methods[0], "dataset").(map[string]any)
	if random["order"] != "random" {
		t.Errorf("random: order = %v, want \"random\"", random["order"])
	}
	if n, ok := random["seed"].(json.Number); !ok || n.String() != "9007199254740991" {
		t.Errorf("random: seed = %v (%T), want the integer 9007199254740991 exactly", random["seed"], random["seed"])
	}

	file := field(t, methods[1], "dataset").(map[string]any)
	if v, ok := file["order"]; !ok || v != "file" {
		t.Errorf("file: order = %v, want \"file\"", v)
	}
	if v, ok := file["seed"]; !ok || v != nil {
		t.Errorf("file: seed = %v, want null (the key present)", v)
	}

	zero := field(t, methods[2], "dataset").(map[string]any)
	if n, ok := zero["seed"].(json.Number); !ok || n.String() != "0" {
		t.Errorf("seed 0: seed = %v, want the integer 0, not null", zero["seed"])
	}

	for i, obj := range []map[string]any{random, file, zero} {
		if len(obj) != 8 {
			t.Errorf("method %d: dataset has %d keys (%v), want 8: file, sha256, records, used, used_max, order, seed, reuse", i, len(obj), obj)
		}
	}
}

// In a search every run's method object carries the order and seed.
// Ground: contract — the JSON of D3.
func TestJSON_DatasetOrderAndSeedInASearch(t *testing.T) {
	res := withDatasets(outcomes["held_all"].res, func(int) *engine.DatasetReport {
		return randomOf("data/users.jsonl", 3, 3, 2, 123)
	})

	out := searchJSON(t, res)

	for i, r := range field(t, out, "breakpoint", "runs").([]any) {
		methods := field(t, r, "report", "methods").([]any)
		if got := field(t, methods[0], "dataset", "order"); got != "random" {
			t.Errorf("run %d: order = %v, want random", i, got)
		}
		if got := field(t, methods[0], "dataset", "seed"); got != 123.0 {
			t.Errorf("run %d: seed = %v, want 123", i, got)
		}
	}
}

// payloadSender answers every request and keeps the record it carried (its
// first byte), per method, in the order the requests were scheduled.
type payloadSender struct {
	mu   sync.Mutex
	seen []struct {
		at     time.Time
		method string
		record int
	}
}

func (s *payloadSender) Send(_ context.Context, req engine.Request) (engine.Outcome, error) {
	s.mu.Lock()
	s.seen = append(s.seen, struct {
		at     time.Time
		method string
		record int
	}{req.ScheduledAt, req.Method, int(req.Payload[0])})
	s.mu.Unlock()

	now := time.Now()

	return engine.Outcome{Category: engine.CategorySuccess, SentAt: now, DoneAt: now}, nil
}

func (s *payloadSender) recordsOf(method string) []int {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := slices.Clone(s.seen)
	sort.SliceStable(seen, func(i, j int) bool { return seen[i].at.Before(seen[j].at) })

	var out []int
	for _, r := range seen {
		if r.method == method {
			out = append(out, r.record)
		}
	}

	return out
}

// randomConfig is a config of random calls on the given methods, each of five
// records and 15 requests (100 rps for 150 ms); seeds[i] is nil for no seed.
func randomConfig(methods []string, seeds []*int64) *config.MasterConfig {
	random := "random"
	cfg := &config.MasterConfig{}
	for i, m := range methods {
		records := make([]config.DatasetRecord, 5)
		for j := range records {
			records[j] = rec(j+1, `{}`)
		}
		cfg.Load.Calls = append(cfg.Load.Calls, config.Call{
			Method: m, RPS: 100, Duration: 150 * time.Millisecond, Timeout: 100 * time.Millisecond,
			Dataset: "data/" + strings.ReplaceAll(m, "/", "_") + ".jsonl", Records: records,
			DatasetOrder: &random, DatasetSeed: seeds[i],
		})
	}

	return cfg
}

// runOf runs the calls CallsFromConfig builds from cfg, each record carrying
// its own number, and returns what each method sent and what the report says.
func runOf(t *testing.T, cfg *config.MasterConfig) (sent map[string][]int, reports map[string]*engine.DatasetReport) {
	t.Helper()

	calls, err := CallsFromConfig(cfg)
	if err != nil {
		t.Fatalf("calls: %v", err)
	}
	for _, c := range calls {
		for i := range c.Payloads {
			c.Payloads[i] = []byte{byte(i)}
		}
	}

	sender := &payloadSender{}
	eng, err := engine.New(engine.Options{Calls: calls, Sender: sender, MaxInFlight: 64})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := eng.Run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}

	sent = map[string][]int{}
	reports = map[string]*engine.DatasetReport{}
	report := eng.Report()
	for i := range report.Methods {
		m := &report.Methods[i]
		sent[m.Method] = sender.recordsOf(m.Method)
		reports[m.Method] = m.Dataset
	}

	return sent, reports
}

// No seed in the config: the run draws one per call, reports it, and the same
// config with that seed hands out the same sequence. Two calls draw two seeds.
// Ground: contract — "any run can be repeated" from its report (P28).
func TestRandomDataset_DrawnSeedIsReportedAndRepeatsTheRun(t *testing.T) {
	methods := []string{"a.B/One", "a.B/Two"}

	sent, reports := runOf(t, randomConfig(methods, []*int64{nil, nil}))

	seeds := make([]int64, len(methods))
	for i, m := range methods {
		r := reports["/"+m]
		if r == nil || !r.Random {
			t.Fatalf("%s: report %+v, want a random order", m, r)
		}
		if r.Seed > maxSeed {
			t.Errorf("%s: drawn seed %d is above 2^53-1: a JSON reader on float64 would repeat another run", m, r.Seed)
		}
		seeds[i] = int64(r.Seed)

		first := sent["/"+m]
		if len(first) != 15 {
			t.Fatalf("%s: %d requests, want 15", m, len(first))
		}
		identity := true
		for pass := 0; pass < 3; pass++ {
			seen := slices.Clone(first[pass*5 : pass*5+5])
			slices.Sort(seen)
			if !slices.Equal(seen, []int{0, 1, 2, 3, 4}) {
				t.Errorf("%s: pass %d = %v, want each record once", m, pass, first[pass*5:pass*5+5])
			}
			identity = identity && slices.Equal(first[pass*5:pass*5+5], []int{0, 1, 2, 3, 4})
		}
		if identity {
			t.Errorf("%s: %v is the file's order three times over, not a random one", m, first)
		}
	}
	if seeds[0] == seeds[1] {
		t.Errorf("both calls drew seed %d, want a seed of its own per call", seeds[0])
	}

	again, againReports := runOf(t, randomConfig(methods, []*int64{&seeds[0], &seeds[1]}))
	for i, m := range methods {
		if !slices.Equal(again["/"+m], sent["/"+m]) {
			t.Errorf("%s: with seed %d again: %v, want %v", m, seeds[i], again["/"+m], sent["/"+m])
		}
		if r := againReports["/"+m]; r == nil || !r.Random || int64(r.Seed) != seeds[i] {
			t.Errorf("%s: report %+v, want the seed %d from the config", m, r, seeds[i])
		}
	}
}

// A seed the config gives is the one reported, even the largest.
// Ground: contract — the report names the seed that was used.
func TestRandomDataset_ConfigSeedIsReported(t *testing.T) {
	seed := int64(maxSeed)

	_, reports := runOf(t, randomConfig([]string{"a.B/One"}, []*int64{&seed}))

	if r := reports["/a.B/One"]; r == nil || !r.Random || r.Seed != maxSeed {
		t.Errorf("report %+v, want random order, seed %d", r, maxSeed)
	}
}

// Ground: contract — file order, or none written, stays file order: not random,
// no seed drawn.
func TestRandomDataset_FileOrderDrawsNoSeed(t *testing.T) {
	file := "file"
	// nil is what the config gives for a key that is not written.
	for name, order := range map[string]*string{"not written": nil, "file": &file} {
		cfg := randomConfig([]string{"a.B/One"}, []*int64{nil})
		cfg.Load.Calls[0].DatasetOrder = order

		sent, reports := runOf(t, cfg)

		if r := reports["/a.B/One"]; r == nil || r.Random || r.Seed != 0 {
			t.Errorf("order %s: report %+v, want file order and no seed", name, r)
		}
		want := []int{0, 1, 2, 3, 4, 0, 1, 2, 3, 4, 0, 1, 2, 3, 4}
		if got := sent["/a.B/One"]; !slices.Equal(got, want) {
			t.Errorf("order %s: sent %v, want the file's order %v", name, got, want)
		}
	}
}
