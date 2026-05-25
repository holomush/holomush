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

func TestGatedSubcommand_DenyShortCircuitsBeforeHandler(t *testing.T) {
	ev := &fakeEvaluator{allow: false}
	handlerRan := false
	gs := GatedSubcommand{
		Name:        "extend",
		Action:      "extend_publish_attempts",
		ResourceRef: func(args string) (string, error) { return "scene:" + args, nil },
		Handler: func(context.Context, CommandRequest, string) (*CommandResponse, error) {
			handlerRan = true
			return OK(""), nil
		},
	}

	resp, err := gs.Run(context.Background(), ev, CommandRequest{Command: "scene", Args: "extend 01SCENE"}, "01SCENE")
	require.NoError(t, err)
	assert.False(t, handlerRan, "handler MUST NOT run when the gate denies")
	assert.Equal(t, CommandError, resp.Status)
	assert.Equal(t, "extend_publish_attempts", ev.gotAction)
	assert.Equal(t, "scene:01SCENE", ev.gotResrc)
}

func TestGatedSubcommand_AllowRunsHandler(t *testing.T) {
	ev := &fakeEvaluator{allow: true}
	gs := GatedSubcommand{
		Name: "extend", Action: "extend_publish_attempts",
		ResourceRef: func(args string) (string, error) { return "scene:" + args, nil },
		Handler: func(context.Context, CommandRequest, string) (*CommandResponse, error) {
			return OK("extended"), nil
		},
	}
	resp, err := gs.Run(context.Background(), ev, CommandRequest{Command: "scene", Args: "extend 01SCENE"}, "01SCENE")
	require.NoError(t, err)
	assert.Equal(t, CommandOK, resp.Status)
	assert.Equal(t, "extended", resp.Output)
}

func TestGatedSubcommand_ResourceRefErrorSkipsGateAndHandler(t *testing.T) {
	ev := &fakeEvaluator{allow: true}
	handlerRan := false
	gs := GatedSubcommand{
		Name: "extend", Action: "extend_publish_attempts",
		ResourceRef: func(string) (string, error) { return "", assertRefErr() },
		Handler: func(context.Context, CommandRequest, string) (*CommandResponse, error) {
			handlerRan = true
			return OK(""), nil
		},
	}
	resp, err := gs.Run(context.Background(), ev, CommandRequest{Command: "scene", Args: "extend"}, "")
	require.NoError(t, err)
	assert.Equal(t, CommandError, resp.Status)
	assert.False(t, handlerRan)
	assert.Equal(t, 0, ev.calls, "gate MUST NOT be consulted when the resource ref can't be derived")
}

func TestGatedSubcommand_EngineErrorReturnsFailure(t *testing.T) {
	ev := &fakeEvaluator{err: errors.New("engine unavailable")}
	handlerRan := false
	gs := GatedSubcommand{
		Name: "extend", Action: "extend_publish_attempts",
		ResourceRef: func(args string) (string, error) { return "scene:" + args, nil },
		Handler: func(context.Context, CommandRequest, string) (*CommandResponse, error) {
			handlerRan = true
			return OK(""), nil
		},
	}
	resp, err := gs.Run(context.Background(), ev, CommandRequest{Command: "scene", Args: "extend 01SCENE"}, "01SCENE")
	require.NoError(t, err)
	assert.Equal(t, CommandFailure, resp.Status)
	assert.False(t, handlerRan, "handler MUST NOT run when the engine errors")
}

func TestGatedSubcommand_NilEvaluatorFailsClosed(t *testing.T) {
	handlerRan := false
	gs := GatedSubcommand{
		Name:        "extend",
		Action:      "extend_publish_attempts",
		ResourceRef: func(args string) (string, error) { return "scene:" + args, nil },
		Handler: func(context.Context, CommandRequest, string) (*CommandResponse, error) {
			handlerRan = true
			return OK(""), nil
		},
	}

	resp, err := gs.Run(context.Background(), nil, CommandRequest{}, "01SCENE")
	require.NoError(t, err)
	assert.Equal(t, CommandFailure, resp.Status, "nil evaluator MUST return CommandFailure")
	assert.False(t, handlerRan, "handler MUST NOT run when evaluator is nil")
}

func TestGatedSubcommand_NilResourceRefFailsClosed(t *testing.T) {
	ev := &fakeEvaluator{allow: true}
	handlerRan := false
	gs := GatedSubcommand{
		Name:        "extend",
		Action:      "extend_publish_attempts",
		ResourceRef: nil,
		Handler: func(context.Context, CommandRequest, string) (*CommandResponse, error) {
			handlerRan = true
			return OK(""), nil
		},
	}

	resp, err := gs.Run(context.Background(), ev, CommandRequest{}, "01SCENE")
	require.NoError(t, err)
	assert.Equal(t, CommandFailure, resp.Status, "nil ResourceRef MUST return CommandFailure")
	assert.False(t, handlerRan, "handler MUST NOT run when ResourceRef is nil")
	assert.Equal(t, 0, ev.calls, "evaluator MUST NOT be called when ResourceRef is nil")
}

func TestGatedSubcommand_NilHandlerFailsClosed(t *testing.T) {
	ev := &fakeEvaluator{allow: true}
	gs := GatedSubcommand{
		Name:        "extend",
		Action:      "extend_publish_attempts",
		ResourceRef: func(args string) (string, error) { return "scene:" + args, nil },
		Handler:     nil,
	}

	resp, err := gs.Run(context.Background(), ev, CommandRequest{}, "01SCENE")
	require.NoError(t, err)
	assert.Equal(t, CommandFailure, resp.Status, "nil Handler MUST return CommandFailure")
}

func assertRefErr() error { return context.Canceled }
