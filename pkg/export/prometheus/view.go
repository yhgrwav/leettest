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

package prometheus

import "time"

// Bounds are the upper edges of the latency histogram's buckets, in seconds:
// 1-2.5-5 per decade from 100 µs to 10 s, then 25 s, 50 s and 60 s. They are a
// contract: a change is incompatible with every stored series.
var Bounds = [19]float64{
	0.0001, 0.00025, 0.0005,
	0.001, 0.0025, 0.005,
	0.01, 0.025, 0.05,
	0.1, 0.25, 0.5,
	1, 2.5, 5,
	10, 25, 50, 60,
}

// Categories are the outcome categories of leettest_calls_total, in the order
// of Method.Calls: the report's contract names.
var Categories = [11]string{
	"success", "request_error", "overload", "failure", "timed_out", "cut_off",
	"unreachable", "client_error", "bad_response", "aborted", "unclassified",
}

// Causes are the client-side reasons of leettest_not_sent_total, in the order
// of Method.NotSent.
var Causes = [3]string{"generator", "connection", "stream"}

// MinRenderInterval is how often the handler of a running exporter renders
// at most: any scrape in between gets the text of the last render.
const MinRenderInterval = time.Second

// Label is one constant label of every series, set by the user.
type Label struct{ Name, Value string }

// Histogram is the latency of the successful calls of a method as cumulative
// buckets. Cumulative[i] counts what is at or below Bounds[i]; Count is the
// total, what +Inf and _count hold, a value above the last bound included.
type Histogram struct {
	Cumulative [19]int64
	Count      int64
	SumSeconds float64
}

// Method is the counters of one method of the plan.
type Method struct {
	Name       string
	PlannedRPS float64
	// Calls is indexed like Categories.
	Calls   [11]int64
	Latency Histogram
	// NotSent is indexed like Causes.
	NotSent    [3]int64
	WarmupSent int64
}

// View is everything one scrape shows.
type View struct {
	// Labels are the user's constant labels, in any order; Render sorts them.
	Labels []Label
	// Started is false before the run has reserved its methods: the scrape
	// then shows the metadata of every series and no sample.
	Started  bool
	InFlight int64
	Warmup   bool
	Methods  []Method
}
