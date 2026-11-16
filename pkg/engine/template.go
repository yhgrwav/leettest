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

package engine

import "errors"

// ErrSeqOverflow is a run that would hand out a seq the field it goes into
// cannot hold.
var ErrSeqOverflow = errors.New("seq does not fit its field")

// Template makes the body of a call whose request changes with each request.
type Template struct {
	// Placeholders are the ones the call's data and the data of its templated
	// steps use, sorted, each once.
	Placeholders []string
	// Max is the largest seq the narrowest field of the call's body and of the
	// bodies of its templated steps can hold; 0 is no limit.
	Max uint64
	// MaxAt names the step whose field sets Max, as "<method> (steps[k])"; empty
	// when the call's own field does, or there is none.
	MaxAt string
	// Counter is the call's seq: the requests handed out so far, shared by
	// every Call copied from this one.
	Counter *RecordCounter
	// Build is the body of the request number seq (from 1).
	Build func(seq uint64) []byte
}

// nextSeq is the seq of the call's next request: the requests handed out so far
// plus one. The scheduler takes it once and uses it for the body of step 1 and
// for the steps after it.
func (t *Template) nextSeq() uint64 {
	return uint64(t.Counter.Handed()) + 1 //nolint:gosec // Handed counts Add(1) calls from zero, never negative
}

// bodyAt is the body of the request number seq. No Build is a template that
// only counts, with an empty body.
func (t *Template) bodyAt(seq uint64) []byte {
	if t.Build == nil {
		return nil
	}

	return t.Build(seq)
}

// handedOut counts one request of the call as handed to a worker.
func (t *Template) handedOut() { t.Counter.add() }

// NewTemplate is a template with a counter at zero.
func NewTemplate(placeholders []string, limit uint64, build func(seq uint64) []byte) *Template {
	return &Template{Placeholders: placeholders, Max: limit, Counter: NewRecordCounter(), Build: build}
}

// TemplatesReport is how a call used its templates.
type TemplatesReport struct {
	Placeholders []string
	// LastSeq is the requests of the call handed to workers in the process,
	// ramp and warm-up included.
	LastSeq uint64
}
