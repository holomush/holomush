// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEvaluator struct {
	allow     bool
	err       error
	gotAction string
	gotResrc  string
	calls     int
}

func (f *fakeEvaluator) Evaluate(_ context.Context, action, resource string) (EvaluateDecision, error) {
	f.calls++
	f.gotAction, f.gotResrc = action, resource
	return EvaluateDecision{Allowed: f.allow, Reason: "nope"}, f.err
}

func TestGatedSubcommand_Run(t *testing.T) {
	type tc struct {
		name string
		// evaluator configuration — if nilEval is true, nil is passed as ev.
		nilEval   bool
		evalAllow bool
		evalErr   error
		// GatedSubcommand field overrides — nil means use the default real func.
		nilResourceRef bool
		resourceRefErr error
		nilHandler     bool
		// expected outcome
		wantStatus     CommandStatus
		wantHandlerRan bool
		// wantEvalCalls is the expected value of ev.calls (-1 means skip the check).
		wantEvalCalls int
		wantAction    string
		wantResource  string
		wantOutput    string
	}

	tests := []tc{
		{
			name:           "deny short-circuits before handler",
			evalAllow:      false,
			wantStatus:     CommandError,
			wantHandlerRan: false,
			wantEvalCalls:  1,
			wantAction:     "extend_publish_attempts",
			wantResource:   "scene:01SCENE",
		},
		{
			name:           "allow runs handler",
			evalAllow:      true,
			wantStatus:     CommandOK,
			wantHandlerRan: true,
			wantEvalCalls:  1,
			wantOutput:     "extended",
		},
		{
			name:           "resource ref error skips gate and handler",
			evalAllow:      true,
			resourceRefErr: assertRefErr(),
			wantStatus:     CommandError,
			wantHandlerRan: false,
			wantEvalCalls:  0,
		},
		{
			name:           "engine error returns CommandFailure",
			evalErr:        errors.New("engine unavailable"),
			wantStatus:     CommandFailure,
			wantHandlerRan: false,
			wantEvalCalls:  1,
		},
		{
			name:           "nil evaluator fails closed",
			nilEval:        true,
			wantStatus:     CommandFailure,
			wantHandlerRan: false,
			wantEvalCalls:  -1, // no fakeEvaluator to count on
		},
		{
			name:           "nil ResourceRef fails closed",
			evalAllow:      true,
			nilResourceRef: true,
			wantStatus:     CommandFailure,
			wantHandlerRan: false,
			wantEvalCalls:  0,
		},
		{
			name:           "nil Handler fails closed",
			evalAllow:      true,
			nilHandler:     true,
			wantStatus:     CommandFailure,
			wantHandlerRan: false,
			wantEvalCalls:  -1, // guard fires before eval is called
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := &fakeEvaluator{allow: tt.evalAllow, err: tt.evalErr}

			handlerRan := false
			handler := func(context.Context, CommandRequest, string) (*CommandResponse, error) {
				handlerRan = true
				return OK("extended"), nil
			}
			if tt.nilHandler {
				handler = nil
			}

			resourceRef := func(args string) (string, error) {
				if tt.resourceRefErr != nil {
					return "", tt.resourceRefErr
				}
				return "scene:" + args, nil
			}
			var resourceRefFn func(string) (string, error)
			if !tt.nilResourceRef {
				resourceRefFn = resourceRef
			}

			gs := GatedSubcommand{
				Name:        "extend",
				Action:      "extend_publish_attempts",
				ResourceRef: resourceRefFn,
				Handler:     handler,
			}

			var evArg HostEvaluator = ev
			if tt.nilEval {
				evArg = nil
			}

			resp, err := gs.Run(context.Background(), evArg, CommandRequest{Command: "scene", Args: "extend 01SCENE"}, "01SCENE")
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.Status)
			assert.Equal(t, tt.wantHandlerRan, handlerRan)

			if tt.wantEvalCalls >= 0 {
				assert.Equal(t, tt.wantEvalCalls, ev.calls)
			}
			if tt.wantAction != "" {
				assert.Equal(t, tt.wantAction, ev.gotAction)
			}
			if tt.wantResource != "" {
				assert.Equal(t, tt.wantResource, ev.gotResrc)
			}
			if tt.wantOutput != "" {
				assert.Equal(t, tt.wantOutput, resp.Output)
			}
		})
	}
}

func assertRefErr() error { return context.Canceled }
