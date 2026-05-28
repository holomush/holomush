// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/pkg/errutil"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fakeRenderingInnerPublisher captures Publish calls for assertion.
type fakeRenderingInnerPublisher struct {
	published []eventbus.Event
}

func (f *fakeRenderingInnerPublisher) Publish(_ context.Context, ev eventbus.Event) error {
	f.published = append(f.published, ev)
	return nil
}

// TestViolationEmitterReachesRenderingPublisher is the regression guard for
// the bead-1r0v.5 crypto-review BLOCKING finding: the fence's audit-emit
// path goes through RenderingPublisher, which rejects unregistered event
// types with EMIT_UNKNOWN_VERB. Without `system:plugin_integrity_violation`
// in the builtin verb registry, every fence refusal silently drops the
// documented operator audit signal — INV-P7-7's audit-emit half is dead.
//
// This test wires a production-shaped emitter (newViolationEmitter) over a
// REAL RenderingPublisher backed by core.BootstrapVerbRegistry, then
// invokes EmitViolation. Pre-fix: assert fails with EMIT_UNKNOWN_VERB.
// Post-fix: publishes cleanly and the fake inner publisher captures the
// fully-stamped event.
func TestViolationEmitterReachesRenderingPublisher(t *testing.T) {
	t.Parallel()

	registry, err := core.BootstrapVerbRegistry("test-1r0v.5")
	require.NoError(t, err)

	inner := &fakeRenderingInnerPublisher{}
	emitter := newViolationEmitter(inner, registry, "test-game")

	rowID := ulid.Make()
	rowIDBytes := rowID.Bytes()
	row := &pluginauditpb.AuditRow{
		Id:      rowIDBytes[:],
		Subject: "events.test-game.scene.01ABC.ic",
		Type:    "test-plugin:secret",
		Codec:   "identity",
	}

	err = emitter.EmitViolation(
		context.Background(),
		"test-plugin",
		row,
		"sensitivity:always",
		"AUDIT_ROW_DOWNGRADE_DETECTED",
	)
	require.NoError(t, err,
		"INV-P7-7 audit emit MUST succeed through the production RenderingPublisher; "+
			"if this fails with EMIT_UNKNOWN_VERB, the system:plugin_integrity_violation "+
			"verb is missing from internal/core/builtins.go::registerBuiltinTypes")

	require.Len(t, inner.published, 1, "exactly one violation event MUST reach the inner publisher")
	got := inner.published[0]
	assert.Equal(t, "system:plugin_integrity_violation", string(got.Type))
	assert.Equal(t, "events.test-game.system.plugin_integrity_violation", string(got.Subject))
	require.NotNil(t, got.Rendering, "RenderingPublisher MUST stamp event.Rendering on the violation event")
	assert.Equal(t, "system", got.Rendering.Category)
	assert.Equal(t, "audit", got.Rendering.Format)
	assert.Equal(t, eventbus.EventChannelAuditOnly, got.Rendering.DisplayTarget)
}

// errPublisher is the simplest fake publisher that always returns a
// sentinel error — used to drive the violation-emitter's
// PLUGIN_INTEGRITY_VIOLATION_EMIT_FAILED wrap path.
type errPublisher struct {
	err error
}

func (e *errPublisher) Publish(_ context.Context, _ eventbus.Event) error { return e.err }

// validViolationRow builds a syntactically valid AuditRow fixture for the
// violation-emitter tests. Tests that care about specific fields override
// after construction.
func validViolationRow() *pluginauditpb.AuditRow {
	id := ulid.Make()
	idBytes := id.Bytes()
	return &pluginauditpb.AuditRow{
		Id:      idBytes[:],
		Subject: "events.test-game.scene.01ABC.ic",
		Type:    "test-plugin:secret",
		Codec:   "identity",
	}
}

// TestViolationEmitter_NilPublisher_GracefulNoop verifies the documented
// degraded-deployment path: when the publisher is nil, EmitViolation
// returns nil rather than erroring on every fence refusal. The fence
// still refuses the row; this just suppresses the never-going-to-emit
// error log.
func TestViolationEmitter_NilPublisher_GracefulNoop(t *testing.T) {
	t.Parallel()
	emitter := newViolationEmitter(nil, nil, "test-game")

	err := emitter.EmitViolation(
		context.Background(),
		"test-plugin",
		validViolationRow(),
		"sensitivity:always",
		"AUDIT_ROW_DOWNGRADE_DETECTED",
	)
	require.NoError(t, err, "nil publisher MUST be a graceful no-op (degraded deployment)")
}

// TestViolationEmitter_PublishError_PropagatesFromRenderingPublisher
// verifies the error path when the underlying publisher errors. With
// the newViolationEmitter refactor (rawPub + registry → wraps with
// RenderingPublisher internally), the publish path is:
//
//	errPublisher → RenderingPublisher(EMIT_PUBLISH_FAILED) → EmitViolation(PLUGIN_INTEGRITY_VIOLATION_EMIT_FAILED)
//
// samber/oops returns the DEEPEST oops code via OopsError.Code(), so
// AsOops surfaces EMIT_PUBLISH_FAILED. The outer
// PLUGIN_INTEGRITY_VIOLATION_EMIT_FAILED layer still contributes
// context (plugin_name, subject) for structured logging — the fence's
// log line at plugin_downgrade_fence.go:292-295 logs the full error
// chain, not just the code — but the canonical machine-readable code
// is the publisher's. errors.Is still unwraps to the sentinel so
// callers retain root-cause-matching semantics.
func TestViolationEmitter_PublishError_PropagatesFromRenderingPublisher(t *testing.T) {
	t.Parallel()
	registry, err := core.BootstrapVerbRegistry("test-publish-err")
	require.NoError(t, err)

	sentinel := errors.New("nats unavailable")
	emitter := newViolationEmitter(&errPublisher{err: sentinel}, registry, "test-game")

	err = emitter.EmitViolation(
		context.Background(),
		"test-plugin",
		validViolationRow(),
		"sensitivity:always",
		"AUDIT_ROW_DOWNGRADE_DETECTED",
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EMIT_PUBLISH_FAILED")
	require.ErrorIs(t, err, sentinel, "underlying publisher error MUST wrap, not swallow")
}

// TestViolationEmitter_InvalidGameID_RejectsSubject verifies the subject
// validation path. eventbus.NewSubject rejects subjects with spaces; an
// invalid gameID surfaces via PLUGIN_INTEGRITY_VIOLATION_INVALID_SUBJECT
// rather than a downstream NATS reject.
func TestViolationEmitter_InvalidGameID_RejectsSubject(t *testing.T) {
	t.Parallel()
	registry, err := core.BootstrapVerbRegistry("test-bad-gameid")
	require.NoError(t, err)

	// gameID with embedded space — produces "events.bad game.system.…"
	// which fails eventbus.NewSubject's tokenization. The publisher
	// path is never invoked because the subject validation fails first,
	// so the inner publisher's behavior doesn't matter here.
	emitter := newViolationEmitter(&errPublisher{err: nil}, registry, "bad game")

	err = emitter.EmitViolation(
		context.Background(),
		"test-plugin",
		validViolationRow(),
		"sensitivity:always",
		"AUDIT_ROW_DOWNGRADE_DETECTED",
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLUGIN_INTEGRITY_VIOLATION_INVALID_SUBJECT")
}

// TestViolationEmitter_BadEventID_StillEmits verifies that a malformed
// row.Id (not 16 bytes) is silently rendered as the zero ULID rather
// than failing the emit. The fence already refused the row before
// invoking the emitter, so logging "best effort" identifying info is
// sufficient even if the offending row's id is malformed.
func TestViolationEmitter_BadEventID_StillEmits(t *testing.T) {
	t.Parallel()
	registry, err := core.BootstrapVerbRegistry("test-bad-id")
	require.NoError(t, err)
	inner := &fakeRenderingInnerPublisher{}
	emitter := newViolationEmitter(inner, registry, "test-game")

	row := validViolationRow()
	row.Id = []byte("8-bytes!") // not 16

	err = emitter.EmitViolation(
		context.Background(),
		"test-plugin",
		row,
		"sensitivity:always",
		"AUDIT_ROW_DOWNGRADE_DETECTED",
	)
	require.NoError(t, err, "malformed row.Id MUST NOT block the emit; zero ULID is acceptable")
	require.Len(t, inner.published, 1)
}
