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
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/yhgrwav/leettest/pkg/config"
	"github.com/yhgrwav/leettest/pkg/descriptor"
)

// A refused method check is decided by its code. Unauthenticated with
// metadata set: the credentials sent were refused, and a run would only show
// a config mistake as the target's result — stop. PermissionDenied: the
// credentials were accepted but may not reach reflection while still serving
// calls — the run goes on, as decided in #62. Unauthenticated without
// metadata: the run goes on too, and the note says what is missing. The
// target's message is never repeated: it may echo what was sent.
func TestAttachData_ARefusedCheckIsDecidedByItsCode(t *testing.T) {
	cases := []struct {
		name     string
		code     codes.Code
		metadata map[string]string
		stops    bool
		note     string
	}{
		{"unauthenticated with metadata", codes.Unauthenticated, map[string]string{"authorization": "x"}, true, ""},
		{"permission denied with metadata", codes.PermissionDenied, map[string]string{"authorization": "x"}, false, "PermissionDenied"},
		{"permission denied without metadata", codes.PermissionDenied, nil, false, "PermissionDenied"},
		{"unauthenticated without metadata", codes.Unauthenticated, nil, false, "target requires credentials; app.metadata is not set"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, calls := loadOf(config.Call{Method: "wallet.v1.Wallet/One"})
			cfg.App.Metadata = c.metadata
			resolver := &fakeResolver{err: status.Error(c.code, "echo s3cret-token")}

			unchecked, err := AttachData(t.Context(), resolver, cfg, calls)

			if got := errors.Is(err, ErrCredentialsRejected); got != c.stops {
				t.Fatalf("stopped = %v (err %v), want %v", got, err, c.stops)
			}
			if c.stops {
				if strings.Contains(err.Error(), "s3cret") {
					t.Errorf("the error repeats the target's message: %v", err)
				}
				if len(unchecked) != 0 {
					t.Errorf("unchecked %v alongside a stop", unchecked)
				}

				return
			}

			if err != nil || len(unchecked) != 1 {
				t.Fatalf("err %v, unchecked %v; want the run to go on with the method unchecked", err, unchecked)
			}

			var out strings.Builder
			PrintReport(&out, "localhost:50051", RunReport{Unchecked: unchecked})
			if !strings.Contains(out.String(), c.note) {
				t.Errorf("the note does not say %q:\n%s", c.note, out.String())
			}
		})
	}
}

// stepRefusingResolver knows every method of scenarioConfig and refuses the
// ones in refuse with the given error, so the head call of a scenario passes
// its check and only a step meets the refusal. A fakeResolver fails every
// method alike, which would stop the run at the call and never reach the step.
type stepRefusingResolver struct {
	known  fakeResolver
	refuse map[string]error
}

func (r *stepRefusingResolver) Resolve(ctx context.Context, method string) (*descriptor.Method, error) {
	if err, ok := r.refuse[method]; ok {
		return nil, err
	}

	return r.known.Resolve(ctx, method)
}

func (r *stepRefusingResolver) ResolveAll(ctx context.Context, methods []string) (map[string]*descriptor.Method, error) {
	out := make(map[string]*descriptor.Method, len(methods))
	for _, m := range methods {
		resolved, err := r.Resolve(ctx, m)
		if err != nil {
			return nil, err
		}
		out[m] = resolved
	}

	return out, nil
}

// A step's method check is decided by its code as a call's is, and its error
// names the step. Unauthenticated with metadata set: the credentials sent were
// refused — stop, not an unchecked method. Unauthenticated without metadata,
// and PermissionDenied: the step stays unchecked and the run goes on, with the
// same note a call gets. A stop never repeats the target's message.
// Ground: contract — a step is checked as the call is (data.go, checkStepMethod).
// Mutation "unchecked even with metadata" (the credentials branch dropped for
// steps) dies on the first case, "an error even without metadata" on the second.
func TestAttachMethods_AStepsRefusedCheckIsDecidedByItsCode(t *testing.T) {
	cases := []struct {
		name     string
		code     codes.Code
		metadata map[string]string
		stops    bool
		note     string
	}{
		{"unauthenticated with metadata", codes.Unauthenticated, map[string]string{"authorization": "x"}, true, ""},
		{"unauthenticated without metadata", codes.Unauthenticated, nil, false, "target requires credentials; app.metadata is not set"},
		{"permission denied with metadata", codes.PermissionDenied, map[string]string{"authorization": "x"}, false, "PermissionDenied"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := scenarioConfig()
			cfg.App.Metadata = c.metadata
			calls, err := CallsFromConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			empty := (&emptypb.Empty{}).ProtoReflect().Descriptor()
			resolver := &stepRefusingResolver{
				known: fakeResolver{known: map[string]protoreflect.MessageDescriptor{
					wsFirst: empty, wsSecond: empty, wsThird: empty, wsPing: empty,
				}},
				refuse: map[string]error{wsSecond: status.Error(c.code, "echo s3cret-token")},
			}

			unchecked, _, err := AttachMethods(t.Context(), resolver, cfg, calls)

			if got := errors.Is(err, ErrCredentialsRejected); got != c.stops {
				t.Fatalf("stopped = %v (err %v), want %v", got, err, c.stops)
			}
			if c.stops {
				if !strings.Contains(err.Error(), "load.calls[0].steps[0]") || !strings.Contains(err.Error(), wsSecond) {
					t.Errorf("error = %q, want it to name the step and its method", err)
				}
				if strings.Contains(err.Error(), "s3cret") {
					t.Errorf("the error repeats the target's message: %v", err)
				}
				if len(unchecked) != 0 {
					t.Errorf("unchecked %v alongside a stop", unchecked)
				}

				return
			}

			if err != nil || len(unchecked) != 1 || unchecked[0].Method != wsSecond {
				t.Fatalf("err %v, unchecked %v; want the run to go on with only %s unchecked", err, unchecked, wsSecond)
			}

			var out strings.Builder
			PrintReport(&out, "localhost:50051", RunReport{Unchecked: unchecked})
			if !strings.Contains(out.String(), c.note) {
				t.Errorf("the note does not say %q:\n%s", c.note, out.String())
			}
		})
	}
}
