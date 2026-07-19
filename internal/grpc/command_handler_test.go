// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventvocab"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/session"
)

// countingPublisher records every published event. The count is the point:
// a dropped or duplicated command_response publish is observable by every
// subscriber on the bus, so the tests assert an exact publish count rather
// than mere presence (T-8-17).
type countingPublisher struct {
	events []eventbus.Event
	err    error
}

func (p *countingPublisher) Publish(_ context.Context, ev eventbus.Event) error {
	p.events = append(p.events, ev)
	return p.err
}

var _ eventbus.Publisher = (*countingPublisher)(nil)

// newTestDispatcher builds a real dispatcher over an EMPTY command registry.
// Any input is therefore an unknown command — the user-facing-error path
// executeViaDispatcher must translate into a command_error event rather than
// an RPC failure.
func newTestDispatcher(t *testing.T) *command.Dispatcher {
	t.Helper()
	d, err := command.NewDispatcher(command.NewRegistry(), policytest.AllowAllEngine())
	require.NoError(t, err)
	return d
}

// TestNewCommandHandlerIsConstructibleWithOnlyItsOwnCollaborators is the SC1 /
// D-02 proof for this unit: the handler is built from `package grpc_test` with
// no *CoreServer, no integrationtest harness, and no integration build tag.
func TestNewCommandHandlerIsConstructibleWithOnlyItsOwnCollaborators(t *testing.T) {
	t.Parallel()

	h := grpcpkg.NewCommandHandler(grpcpkg.CommandDeps{
		Dispatcher:  newTestDispatcher(t),
		CmdServices: &command.Services{},
		Publisher:   &countingPublisher{},
		GameID:      func() string { return "main" },
	})

	require.NotNil(t, h)
}

// TestCommandHandlerEmitCommandResponsePublishesExactlyOneEvent pins the
// publish contract: one event, on the character's fully-qualified subject,
// stamped with the system actor. An accidental double-publish fails on the
// exact-count assertion (T-8-17).
func TestCommandHandlerEmitCommandResponsePublishesExactlyOneEvent(t *testing.T) {
	t.Parallel()

	pub := &countingPublisher{}
	h := grpcpkg.NewCommandHandler(grpcpkg.CommandDeps{
		Publisher: pub,
		GameID:    func() string { return "main" },
	})

	charID := core.NewULID()
	require.NoError(t, grpcpkg.ExportEmitCommandResponse(
		context.Background(), h, core.CharacterRef{ID: charID, Name: "Alice"}, "hi", false))

	require.Len(t, pub.events, 1, "exactly one command_response publish")
	assert.Equal(t, eventbus.Subject("events.main.character."+charID.String()), pub.events[0].Subject)
	assert.Equal(t, eventbus.Type(eventvocab.EventTypeCommandResponse), pub.events[0].Type)
	assert.Equal(t, eventbus.ActorKindSystem, pub.events[0].Actor.Kind)
	assert.Equal(t, core.SystemActorULID, pub.events[0].Actor.ID)
}

// TestCommandHandlerEmitCommandResponseUsesErrorTypeWhenIsErrorIsSet pins the
// command_response / command_error type selection.
func TestCommandHandlerEmitCommandResponseUsesErrorTypeWhenIsErrorIsSet(t *testing.T) {
	t.Parallel()

	pub := &countingPublisher{}
	h := grpcpkg.NewCommandHandler(grpcpkg.CommandDeps{
		Publisher: pub,
		GameID:    func() string { return "main" },
	})

	require.NoError(t, grpcpkg.ExportEmitCommandResponse(
		context.Background(), h, core.CharacterRef{ID: core.NewULID(), Name: "Alice"}, "boom", true))

	require.Len(t, pub.events, 1)
	assert.Equal(t, eventbus.Type(eventvocab.EventTypeCommandError), pub.events[0].Type)
}

// TestCommandHandlerEmitCommandResponseIsSilentNoOpWhenPublisherIsNil pins the
// nil-collaborator semantics moved with the body: a nil publisher must return
// nil without dereferencing, not panic. Many CoreServer fixtures construct
// without wiring a publisher at all.
func TestCommandHandlerEmitCommandResponseIsSilentNoOpWhenPublisherIsNil(t *testing.T) {
	t.Parallel()

	h := grpcpkg.NewCommandHandler(grpcpkg.CommandDeps{})

	require.NoError(t, grpcpkg.ExportEmitCommandResponse(
		context.Background(), h, core.CharacterRef{ID: core.NewULID(), Name: "Nobody"}, "hi", false))
}

// TestCommandHandlerExecuteCommandDeliversUnknownCommandAsAnEventNotAnRPCError
// exercises the executeCommand -> executeViaDispatcher -> emitCommandResponse
// chain end to end on the unit. An unknown command is a user-facing error: the
// player message is delivered as a command_error event and the call returns
// nil so HandleCommand reports Success=true.
func TestCommandHandlerExecuteCommandDeliversUnknownCommandAsAnEventNotAnRPCError(t *testing.T) {
	t.Parallel()

	pub := &countingPublisher{}
	h := grpcpkg.NewCommandHandler(grpcpkg.CommandDeps{
		Dispatcher:  newTestDispatcher(t),
		CmdServices: &command.Services{},
		Publisher:   pub,
		GameID:      func() string { return "main" },
	})

	charID := core.NewULID()
	info := &session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   charID,
		CharacterName: "Alice",
		LocationID:    core.NewULID(),
		PlayerID:      core.NewULID(),
	}

	err := grpcpkg.ExportExecuteCommand(context.Background(), h, info, "definitelynotacommand", "")

	require.NoError(t, err, "a user-facing error must not surface as an RPC failure")
	require.Len(t, pub.events, 1, "exactly one command_error publish")
	assert.Equal(t, eventbus.Type(eventvocab.EventTypeCommandError), pub.events[0].Type)
}

// TestCommandHandlerExecuteCommandRejectsAMalformedConnectionID pins the
// explicit-error contract moved with the body: an empty connection_id is
// accepted (legacy non-gateway callers), but a NON-EMPTY unparseable value is
// an error rather than a silent fallback to the zero ULID.
func TestCommandHandlerExecuteCommandRejectsAMalformedConnectionID(t *testing.T) {
	t.Parallel()

	pub := &countingPublisher{}
	h := grpcpkg.NewCommandHandler(grpcpkg.CommandDeps{
		Dispatcher:  newTestDispatcher(t),
		CmdServices: &command.Services{},
		Publisher:   pub,
		GameID:      func() string { return "main" },
	})

	info := &session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   core.NewULID(),
		CharacterName: "Alice",
	}

	err := grpcpkg.ExportExecuteCommand(context.Background(), h, info, "look", "not-a-ulid")

	require.Error(t, err)
	assert.Empty(t, pub.events, "a rejected connection_id must not publish anything")
}
