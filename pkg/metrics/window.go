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

package metrics

import (
	"math"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"
)

// windowSignificantFigures is how precisely a window keeps a latency: coarser
// than the run's own, so that a run's worth of windows stays small.
const windowSignificantFigures = 2

// NewWindowLatencies is the distribution of one time window of a run: the
// same measured and censored pair as NewLatencies, but with two significant
// digits instead of three, so that a run's worth of windows stays small. A
// percentile read from it is coarser than the run's own and is not to be
// compared with it by the last digit.
func NewWindowLatencies() *Latencies {
	return &Latencies{
		measured:    newWindowHistogram(),
		censored:    newWindowHistogram(),
		censoredMin: math.MaxInt64,
	}
}

func newWindowHistogram() *hdrhistogram.Histogram {
	return hdrhistogram.New(lowestTrackableNanos, highestTrackableNanos, windowSignificantFigures)
}

// ByteSize is the memory held by the histograms, in bytes. It grows by a
// second histogram if a censored value past the range ever needs one.
func (l *Latencies) ByteSize() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	size := l.measured.ByteSize()
	if l.censored != nil {
		size += l.censored.ByteSize()
	}

	return size
}
