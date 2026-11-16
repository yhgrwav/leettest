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

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNoStages      = errors.New("scheduler has no stages")
	ErrStageRPS      = errors.New("stage rps must not be negative, and not 0 at both ends")
	ErrStageDuration = errors.New("stage duration must be positive")
	// ErrOnceNeedsEngine is what Scheduler.Run returns for a call whose dataset
	// has Once set: only Engine counts records across runs.
	ErrOnceNeedsEngine = errors.New("a dataset with Once needs the engine; Scheduler.Run does not honour it")
)

type Request struct {
	Method string
	// Payload is encoded once before the run starts and shared by every
	// request for this method, or by every request that carries the same
	// record of a dataset; senders must treat it as read-only.
	Payload     []byte
	ScheduledAt time.Time
	// Deadline is the absolute moment derived from ScheduledAt plus the
	// method's timeout, so a request stuck in queue does not get a fresh
	// budget. Zero means no deadline.
	Deadline time.Time
	// KeepResponse says whether to keep the response body; needed for the
	// dependency graph, always false in the MVP.
	KeepResponse bool
	// scenario is the call, for a call with steps and nil otherwise: the worker
	// runs its Next one after another inside this request's in-flight slot.
	// One pointer keeps Request small (a field per request is paid by every
	// call), and it points at a Call that outlives the run: a copy made for one
	// request would be an allocation per request.
	scenario *Call
	// Seq is the seq the call's template built this request's body with, for
	// the steps of its iteration; zero for a call without a template.
	Seq uint64
	// StepRows is the row each dataset step of Next takes in this iteration,
	// indexed as Next; nil for a call without dataset steps.
	StepRows []int32
}

type Scheduler struct {
	Call Call
}

func NewScheduler(call Call) *Scheduler {
	return &Scheduler{Call: call}
}

func (s *Scheduler) Run(ctx context.Context, out chan<- Request) error {
	if err := s.validate(); err != nil {
		return err
	}
	if s.Call.Dataset != nil && s.Call.Dataset.Once {
		return fmt.Errorf("%s: %w", s.Call.Method, ErrOnceNeedsEngine)
	}

	// Judged once, as newDispatcher does: a measure of the clock step takes
	// milliseconds and must not delay each request.
	exact := exactSchedule()

	stageStart := time.Now()

	// A dataset call with no counter counts this run from its first record.
	run := &Scheduler{Call: counted(s.Call)}

	for i, stage := range s.Call.Stages {
		if err := run.runStage(ctx, out, stage, stageStart, exact); err != nil {
			return fmt.Errorf("stage %d: %w", i, err)
		}
		stageStart = stageStart.Add(stage.Duration)
	}

	return nil
}

func (s *Scheduler) validate() error {
	if len(s.Call.Stages) == 0 {
		return ErrNoStages
	}
	if err := s.Call.checkStepDatasets(); err != nil {
		return err
	}

	for i, stage := range s.Call.Stages {
		// A ramp may start or end at 0; a stage idle at both ends has no tick.
		if stage.StartRPS < 0 || stage.TargetRPS < 0 || max(stage.StartRPS, stage.TargetRPS) < 1 {
			return fmt.Errorf("stage %d: %w: %d to %d", i, ErrStageRPS, stage.StartRPS, stage.TargetRPS)
		}
		if stage.Duration <= 0 {
			return fmt.Errorf("stage %d: %w: %s", i, ErrStageDuration, stage.Duration)
		}
	}

	return nil
}

func (s *Scheduler) runStage(ctx context.Context, out chan<- Request, stage Stage, stageStart time.Time, exact bool) error {
	for i := 0; ; i++ {
		offset, ok := stage.offset(i)
		if !ok {
			return nil
		}

		scheduledAt := stageStart.Add(offset)

		if err := waitUntil(ctx, scheduledAt, exact); err != nil {
			return err
		}

		if s.Call.Template != nil {
			if err := s.Call.seqOverflow(); err != nil {
				return err
			}
		}

		// Built before the select, so a record is taken from S before the
		// request leaves; S moves only after it has.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- s.Call.newRequest(scheduledAt):
		}
		s.Call.handedOut()

		if s.Call.Template != nil {
			s.Call.Template.handedOut()
		}
	}
}

func (s *Scheduler) newRequest(scheduledAt time.Time) Request {
	return s.Call.newRequest(scheduledAt)
}

// newRequest is on the Call, not on a Scheduler made for the occasion: the
// request keeps a pointer to c, and that must be a Call that lives on.
func (c *Call) newRequest(scheduledAt time.Time) Request {
	req := Request{
		Method:       c.Method,
		ScheduledAt:  scheduledAt,
		KeepResponse: c.KeepResponse,
	}
	if tpl := c.Template; tpl != nil {
		req.Seq = tpl.nextSeq()
		req.Payload = tpl.bodyAt(req.Seq)
	} else {
		req.Payload = c.payload()
	}
	if len(c.Next) > 0 {
		req.scenario = c
		req.StepRows = c.stepRows()
	}

	if c.Timeout > 0 {
		req.Deadline = scheduledAt.Add(c.Timeout)
	}

	return req
}
