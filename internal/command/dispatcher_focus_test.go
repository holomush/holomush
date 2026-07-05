// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/session"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// newFocusExec builds a CommandExecution with the Output + Services that
// Dispatch requires (dispatcher.go returns ErrNilServices when Services is nil,
// before any parse/redirect logic runs). Pass ulid.ULID{} for connID to model
// "no connection context".
func newFocusExec(connID ulid.ULID) *command.CommandExecution {
	var buf bytes.Buffer
	return command.NewTestExecution(command.CommandExecutionConfig{
		CharacterID:  ulid.Make(),
		ConnectionID: connID,
		Output:       &buf,
		Services:     command.NewTestServices(command.ServicesConfig{Engine: policytest.AllowAllEngine()}),
	})
}

type fakeFocusReader struct {
	kind session.FocusKind
	err  error
}

func (f fakeFocusReader) ConnectionFocusKind(_ context.Context, _ ulid.ULID) (session.FocusKind, error) {
	return f.kind, f.err
}

// captureDeliverer records the last CommandRequest that reached a plugin.
type captureDeliverer struct{ last pluginsdk.CommandRequest }

func (c *captureDeliverer) DeliverCommand(_ context.Context, _ string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	c.last = cmd
	return &pluginsdk.CommandResponse{Status: pluginsdk.CommandOK}, nil
}

func (c *captureDeliverer) EmitPluginEvent(_ context.Context, _ string, _ pluginsdk.EmitEvent) error {
	return nil
}

// focusRedirectDispatcher builds a dispatcher with two plugin-backed commands
// ("pose" and "scene") routed to the capture deliverer, the redirect table
// pose→scene for the scene kind, and the given focus reader + optional alias.
func focusRedirectDispatcher(t *testing.T, fr command.FocusReader, alias *command.AliasCache) (*command.Dispatcher, *captureDeliverer) {
	t.Helper()
	reg := command.NewRegistry()
	for _, name := range []string{"pose", "scene"} {
		entry, err := command.NewCommandEntry(command.CommandEntryConfig{
			Name:       name,
			PluginName: "core-fake",
			Source:     "core-fake",
		})
		require.NoError(t, err)
		require.NoError(t, reg.Register(*entry))
	}
	deliverer := &captureDeliverer{}
	table := command.FocusRedirectTable{"pose": {"scene": "scene"}}
	opts := []command.DispatcherOption{
		command.WithPluginDeliverer(deliverer),
		command.WithFocusReader(fr),
		command.WithFocusRedirects(table),
	}
	if alias != nil {
		opts = append(opts, command.WithAliasCache(alias))
	}
	d, err := command.NewDispatcher(reg, policytest.AllowAllEngine(), opts...)
	require.NoError(t, err)
	return d, deliverer
}

// Verifies: INV-SCENE-66
func TestDispatcherRedirectsSceneFocusedVerbToSceneCommand(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, nil)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), "pose bows", exec))
	assert.Equal(t, "scene", deliverer.last.Command, "scene-focused pose must route to the scene command")
	assert.Equal(t, "pose bows", deliverer.last.Args, "the verb is prepended to the scene command's args")
}

// Verifies: INV-SCENE-66
func TestDispatcherDoesNotRedirectWhenGridFocused(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: ""}, nil)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), "pose bows", exec))
	assert.Equal(t, "pose", deliverer.last.Command, "grid focus must route to the location pose handler")
}

func TestDispatcherFailsOpenToLocationOnFocusReadError(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{err: oops.Errorf("focus store down")}, nil)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), "pose bows", exec))
	assert.Equal(t, "pose", deliverer.last.Command, "a focus-read infra error must fail open to location, not drop the command")
}

func TestDispatcherDoesNotRedirectWithoutConnectionID(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, nil)
	exec := newFocusExec(ulid.ULID{}) // no ConnectionID
	require.NoError(t, d.Dispatch(context.Background(), "pose bows", exec))
	assert.Equal(t, "pose", deliverer.last.Command, "no connection context ⇒ no focus ⇒ no redirect")
}

// Verifies: INV-SCENE-66
func TestDispatcherRedirectPreservesInvokedAsForSemiposeSigil(t *testing.T) {
	// ";" is a system prefix alias for "pose". Alias resolution strips the sigil
	// into invokedAs (";"); the redirect must NOT clobber it, so no-space
	// semantics survive into the scene command's CommandRequest.InvokedAs.
	alias := command.NewAliasCache()
	require.NoError(t, alias.SetSystemAlias(";", "pose")) // single-char system alias = prefix alias (alias.go:108)
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, alias)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), ";waves", exec))
	assert.Equal(t, "scene", deliverer.last.Command)
	assert.Equal(t, "pose waves", deliverer.last.Args)
	assert.Equal(t, ";", deliverer.last.InvokedAs, "invokedAs (the semipose sigil) MUST survive the redirect")
}

// Verifies: INV-SCENE-66
func TestDispatcherRedirectPreservesInvokedAsForWithSpacePoseSigil(t *testing.T) {
	// ":" is a system prefix alias for "pose" (with-space pose). Alias resolution
	// strips the sigil into invokedAs (":"); the redirect must NOT clobber it.
	alias := command.NewAliasCache()
	require.NoError(t, alias.SetSystemAlias(":", "pose"))
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, alias)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), ":waves", exec))
	assert.Equal(t, "scene", deliverer.last.Command)
	assert.Equal(t, "pose waves", deliverer.last.Args)
	assert.Equal(t, ":", deliverer.last.InvokedAs, "invokedAs (the with-space pose sigil) MUST survive the redirect")
}
