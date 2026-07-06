// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/observability"
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

// focusRedirectVerbs are the plugin-backed commands registered by the focus-
// redirect test dispatchers: the four ambient verbs the design (§4.4/§6)
// requires to redirect (pose/say/ooc/emit), plus their "scene" target.
var focusRedirectVerbs = []string{"pose", "say", "ooc", "emit", "scene"}

// focusRedirectTable redirects every ambient verb to "scene" for the scene
// focus kind, mirroring the core-scenes manifest declaration (§4.1).
var focusRedirectTable = command.FocusRedirectTable{
	"pose": {"scene": "scene"},
	"say":  {"scene": "scene"},
	"ooc":  {"scene": "scene"},
	"emit": {"scene": "scene"},
}

// newFocusRedirectRegistry registers the plugin-backed pose/say/ooc/emit/scene
// commands shared by the focus-redirect test dispatchers.
func newFocusRedirectRegistry(t *testing.T) *command.Registry {
	t.Helper()
	reg := command.NewRegistry()
	for _, name := range focusRedirectVerbs {
		entry, err := command.NewCommandEntry(command.CommandEntryConfig{
			Name:       name,
			PluginName: "core-fake",
			Source:     "core-fake",
		})
		require.NoError(t, err)
		require.NoError(t, reg.Register(*entry))
	}
	return reg
}

// focusRedirectDispatcher builds a dispatcher with plugin-backed pose/say/ooc/
// emit/scene commands routed to the capture deliverer, the redirect table
// (every ambient verb → scene for the scene kind), and the given focus reader
// + optional alias. Uses an allow-all engine — ABAC gating THROUGH the
// redirect is exercised separately by focusRedirectDispatcherWithEngine.
func focusRedirectDispatcher(t *testing.T, fr command.FocusReader, alias *command.AliasCache) (*command.Dispatcher, *captureDeliverer) {
	t.Helper()
	return focusRedirectDispatcherWithEngine(t, fr, alias, policytest.AllowAllEngine())
}

// focusRedirectDispatcherWithEngine mirrors focusRedirectDispatcher but takes
// an explicit AccessPolicyEngine instead of always allowing, so Layer-1/Layer-2
// ABAC gating can be exercised through the redirect (INV-SCENE-66, x1lwf.6):
// the redirect rewrites parsed.Name BEFORE the two-layer check runs, so the
// engine must see the REDIRECTED (effective) command name, not the original
// verb.
func focusRedirectDispatcherWithEngine(t *testing.T, fr command.FocusReader, alias *command.AliasCache, engine types.AccessPolicyEngine) (*command.Dispatcher, *captureDeliverer) {
	t.Helper()
	reg := newFocusRedirectRegistry(t)
	deliverer := &captureDeliverer{}
	opts := []command.DispatcherOption{
		command.WithPluginDeliverer(deliverer),
		command.WithFocusReader(fr),
		command.WithFocusRedirects(focusRedirectTable),
	}
	if alias != nil {
		opts = append(opts, command.WithAliasCache(alias))
	}
	d, err := command.NewDispatcher(reg, engine, opts...)
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
// Verifies: INV-SCENE-67
func TestDispatcherDoesNotRedirectWhenGridFocused(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: ""}, nil)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), "pose bows", exec))
	assert.Equal(t, "pose", deliverer.last.Command, "grid focus must route to the location pose handler")
}

// Verifies: INV-SCENE-67
func TestDispatcherFailsClosedOnFocusReadError(t *testing.T) {
	before := testutil.ToFloat64(observability.EngineFailureCounter("focus_redirect"))

	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{err: oops.Errorf("focus store down")}, nil)
	exec := newFocusExec(ulid.Make())
	err := d.Dispatch(context.Background(), "pose bows", exec)
	require.Error(t, err, "a focus-read infra error must abort dispatch (holomush-uprtc)")

	// The command must reach NO handler. Routing to the location handler on a
	// focus-read error (the pre-uprtc fail-open contract) broadcast a
	// scene-focused user's participant-only encrypted content in plaintext to
	// grid bystanders — an INV-SCENE-3 sensitivity downgrade.
	assert.Empty(t, deliverer.last.Command, "no handler may receive the command on a focus-read error")

	// The abort is player-visible and retryable, never silent: the player must
	// learn the message was NOT delivered anywhere.
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodeFocusReadFailed, oopsErr.Code())
	assert.Contains(t, command.PlayerMessage(err), "not sent")

	// The fail-closed path keeps the engine-failure metric — mirrors
	// RateLimitMiddleware.Enforce's observability (metric + span attribute,
	// not just a WARN log) so a live degradation is visible to Prometheus,
	// not only Loki.
	after := testutil.ToFloat64(observability.EngineFailureCounter("focus_redirect"))
	assert.Equal(t, before+1, after, "focus-read failure must increment the focus_redirect engine-failure counter")
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

// Verifies: INV-SCENE-66
func TestDispatcherRedirectsSceneFocusedSayToSceneCommand(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, nil)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), "say hello there", exec))
	assert.Equal(t, "scene", deliverer.last.Command, "scene-focused say must route to the scene command")
	assert.Equal(t, "say hello there", deliverer.last.Args, "the verb is prepended to the scene command's args")
}

// Verifies: INV-SCENE-66
func TestDispatcherRedirectsSceneFocusedOocToSceneCommand(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, nil)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), "ooc brb", exec))
	assert.Equal(t, "scene", deliverer.last.Command, "scene-focused ooc must route to the scene command")
	assert.Equal(t, "ooc brb", deliverer.last.Args, "the verb is prepended to the scene command's args")
}

// Verifies: INV-SCENE-66
func TestDispatcherRedirectsSceneFocusedEmitToSceneCommand(t *testing.T) {
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, nil)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), "emit The door creaks open.", exec))
	assert.Equal(t, "scene", deliverer.last.Command, "scene-focused emit must route to the scene command")
	assert.Equal(t, "emit The door creaks open.", deliverer.last.Args, "the verb is prepended to the scene command's args")
}

// Verifies: INV-SCENE-66
func TestDispatcherRedirectPreservesInvokedAsForOOCStyleSaySigil(t *testing.T) {
	// `"` is a system prefix alias for "say" (classic MUSH say-shorthand, e.g.
	// `"hello` => `say hello`). Alias resolution strips the sigil into
	// invokedAs (`"`); the redirect must NOT clobber it, so the OOC-style/
	// say-shorthand semantics carried on invokedAs survive into the scene
	// command's CommandRequest, exactly as the pose `;`/`:` sigils do above.
	alias := command.NewAliasCache()
	require.NoError(t, alias.SetSystemAlias(`"`, "say"))
	d, deliverer := focusRedirectDispatcher(t, fakeFocusReader{kind: session.FocusKindScene}, alias)
	exec := newFocusExec(ulid.Make())
	require.NoError(t, d.Dispatch(context.Background(), `"hello there`, exec))
	assert.Equal(t, "scene", deliverer.last.Command)
	assert.Equal(t, "say hello there", deliverer.last.Args)
	assert.Equal(t, `"`, deliverer.last.InvokedAs, "invokedAs (the say-shorthand sigil) MUST survive the redirect")
}

// TestNewDispatcherRequiresBothFocusOptionsOrNeither guards x1lwf.9: passing
// only one of WithFocusReader / WithFocusRedirects silently disables the
// whole focus-redirect feature (maybeRedirectForFocus short-circuits unless
// BOTH are non-nil), regressing INV-SCENE-66 with no signal. NewDispatcher
// MUST reject that half-wired state.
func TestNewDispatcherRequiresBothFocusOptionsOrNeither(t *testing.T) {
	engine := policytest.AllowAllEngine()

	t.Run("errors when only WithFocusReader is set", func(t *testing.T) {
		_, err := command.NewDispatcher(newFocusRedirectRegistry(t), engine,
			command.WithFocusReader(fakeFocusReader{kind: session.FocusKindScene}))
		require.Error(t, err)
		assert.Equal(t, command.ErrFocusRedirectWiringIncomplete, err)
	})

	t.Run("errors when only WithFocusRedirects is set", func(t *testing.T) {
		_, err := command.NewDispatcher(newFocusRedirectRegistry(t), engine,
			command.WithFocusRedirects(focusRedirectTable))
		require.Error(t, err)
		assert.Equal(t, command.ErrFocusRedirectWiringIncomplete, err)
	})

	t.Run("succeeds when both focus options are set", func(t *testing.T) {
		_, err := command.NewDispatcher(newFocusRedirectRegistry(t), engine,
			command.WithFocusReader(fakeFocusReader{kind: session.FocusKindScene}),
			command.WithFocusRedirects(focusRedirectTable))
		require.NoError(t, err)
	})

	t.Run("succeeds when neither focus option is set", func(t *testing.T) {
		_, err := command.NewDispatcher(newFocusRedirectRegistry(t), engine)
		require.NoError(t, err)
	})
}

// TestDispatcherRedirectAppliesABACToRedirectedCommand guards x1lwf.6: the
// redirect rewrites parsed.Name pose→scene BEFORE Layer-1 (`command:scene`)
// and Layer-2 checks run. Granting execution of "scene" but NOT "pose" proves
// the two-layer ABAC gate evaluates the REDIRECTED (effective) command name,
// not the original verb.
//
// Verifies: INV-SCENE-66
func TestDispatcherRedirectAppliesABACToRedirectedCommand(t *testing.T) {
	mockAccess := policytest.NewGrantEngine()
	d, deliverer := focusRedirectDispatcherWithEngine(t, fakeFocusReader{kind: session.FocusKindScene}, nil, mockAccess)
	exec := newFocusExec(ulid.Make())
	subject := access.CharacterSubject(exec.CharacterID().String())
	mockAccess.GrantCommandExecution(subject, "scene") // NOT "pose"

	require.NoError(t, d.Dispatch(context.Background(), "pose bows", exec))
	assert.Equal(t, "scene", deliverer.last.Command, "scene-focused pose redirected to scene must be authorized as scene, not pose")
}

// TestDispatcherRedirectDeniesWhenOnlyOriginalVerbIsGranted is the inverse of
// the above: granting only "pose" (not "scene") must DENY a scene-focused
// pose, because the ABAC gate runs against the redirected name.
//
// Verifies: INV-SCENE-66
func TestDispatcherRedirectDeniesWhenOnlyOriginalVerbIsGranted(t *testing.T) {
	mockAccess := policytest.NewGrantEngine()
	d, _ := focusRedirectDispatcherWithEngine(t, fakeFocusReader{kind: session.FocusKindScene}, nil, mockAccess)
	exec := newFocusExec(ulid.Make())
	subject := access.CharacterSubject(exec.CharacterID().String())
	mockAccess.GrantCommandExecution(subject, "pose") // NOT "scene"

	err := d.Dispatch(context.Background(), "pose bows", exec)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, command.CodePermissionDenied, oopsErr.Code(), "the redirected command's name (scene) must be what gates authorization")
}
