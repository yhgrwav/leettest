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

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Render writes v in the text exposition format 0.0.4: nine metric families
// in a fixed order, each with its HELP and TYPE lines, and methods in the
// order of v.Methods. A series' own labels (method, category or cause, le)
// come first and the user's labels after them, sorted by name.
func Render(w io.Writer, v View) error {
	r := renderer{
		w:      bufio.NewWriter(w),
		labels: append([]Label(nil), v.Labels...),
	}
	slices.SortFunc(r.labels, func(a, b Label) int { return strings.Compare(a.Name, b.Name) })

	r.family("leettest_planned_rps",
		"Offered load of the plan at this moment, calls per second; 0 after the plan ends.",
		"gauge")
	if v.Started {
		for mi := range v.Methods {
			m := &v.Methods[mi]
			r.sample("leettest_planned_rps", []Label{{"method", m.Name}}, formatFloat(m.PlannedRPS))
		}
	}

	r.family("leettest_calls_total",
		"Measured calls by outcome category, warm-up excluded. Counted when a call completes.",
		"counter")
	if v.Started {
		for mi := range v.Methods {
			m := &v.Methods[mi]
			for i, c := range Categories {
				r.sample("leettest_calls_total", []Label{{"method", m.Name}, {"category", c}}, formatInt(m.Calls[i]))
			}
		}
	}

	r.family("leettest_latency_seconds",
		"Latency from the scheduled time, success only, warm-up excluded. Buckets are HDR sub-buckets by lower value, about 0.1% fine; the sum is read from them.",
		"histogram")
	if v.Started {
		for mi := range v.Methods {
			m := &v.Methods[mi]
			for i, bound := range Bounds {
				r.sample("leettest_latency_seconds_bucket",
					[]Label{{"method", m.Name}, {"le", formatFloat(bound)}}, formatInt(m.Latency.Cumulative[i]))
			}
			r.sample("leettest_latency_seconds_bucket",
				[]Label{{"method", m.Name}, {"le", "+Inf"}}, formatInt(m.Latency.Count))
			r.sample("leettest_latency_seconds_sum",
				[]Label{{"method", m.Name}}, formatFloat(m.Latency.SumSeconds))
			r.sample("leettest_latency_seconds_count",
				[]Label{{"method", m.Name}}, formatInt(m.Latency.Count))
		}
	}

	r.family("leettest_censored_total",
		"Calls that ended without an answer, timed_out and aborted: their latency is only a lower bound and is not in leettest_latency_seconds.",
		"counter")
	if v.Started {
		for mi := range v.Methods {
			m := &v.Methods[mi]
			for _, c := range []string{"timed_out", "aborted"} {
				r.sample("leettest_censored_total",
					[]Label{{"method", m.Name}, {"category", c}}, formatInt(m.Calls[slices.Index(Categories[:], c)]))
			}
		}
	}

	r.family("leettest_excluded_total",
		"Calls with no usable latency: unreachable, cut_off, client_error, unclassified. Not in leettest_latency_seconds.",
		"counter")
	if v.Started {
		for mi := range v.Methods {
			m := &v.Methods[mi]
			for _, c := range []string{"unreachable", "cut_off", "client_error", "unclassified"} {
				r.sample("leettest_excluded_total",
					[]Label{{"method", m.Name}, {"category", c}}, formatInt(m.Calls[slices.Index(Categories[:], c)]))
			}
		}
	}

	r.family("leettest_not_sent_total",
		"Calls that never left the generator, by the client-side cause: generator, connection, stream.",
		"counter")
	if v.Started {
		for mi := range v.Methods {
			m := &v.Methods[mi]
			for i, c := range Causes {
				r.sample("leettest_not_sent_total", []Label{{"method", m.Name}, {"cause", c}}, formatInt(m.NotSent[i]))
			}
		}
	}

	r.family("leettest_in_flight", "Calls in flight now, all methods.", "gauge")
	if v.Started {
		r.sample("leettest_in_flight", nil, formatInt(v.InFlight))
	}

	r.family("leettest_warmup_sent_total",
		"Calls sent during warm-up; they are in no other counter.",
		"counter")
	if v.Started {
		for mi := range v.Methods {
			m := &v.Methods[mi]
			r.sample("leettest_warmup_sent_total", []Label{{"method", m.Name}}, formatInt(m.WarmupSent))
		}
	}

	r.family("leettest_warmup", "1 while the run is in its warm-up, else 0.", "gauge")
	if v.Started {
		warmup := 0
		if v.Warmup {
			warmup = 1
		}
		r.sample("leettest_warmup", nil, formatInt(int64(warmup)))
	}

	return r.w.Flush()
}

type renderer struct {
	w      *bufio.Writer
	labels []Label
}

func (r *renderer) family(name, help, kind string) {
	fmt.Fprintf(r.w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}

func (r *renderer) sample(name string, own []Label, value string) {
	fmt.Fprint(r.w, name)
	if len(own)+len(r.labels) > 0 {
		fmt.Fprint(r.w, "{")
		first := true
		for _, l := range own {
			r.label(l, &first)
		}
		for _, l := range r.labels {
			r.label(l, &first)
		}
		fmt.Fprint(r.w, "}")
	}
	fmt.Fprintf(r.w, " %s\n", value)
}

func (r *renderer) label(l Label, first *bool) {
	if !*first {
		fmt.Fprint(r.w, ",")
	}
	*first = false
	fmt.Fprintf(r.w, "%s=\"%s\"", l.Name, escapeLabelValue(l.Value))
}

func escapeLabelValue(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(c)
		}
	}

	return b.String()
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func formatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}
