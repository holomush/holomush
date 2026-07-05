// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package eventbus_test

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
)

// TestContentValidationRejectsNonConformingCommunicationPayload is INV-COMM-1
// (pending binding — this gate is built but not yet wired live per Slice 1).
// A category:communication event whose payload does not decode to a valid
// CommunicationContent (missing required `text`) MUST be rejected with
// EMIT_CONTENT_INVALID and MUST NOT be forwarded to the inner publisher.
func TestContentValidationRejectsNonConformingCommunicationPayload(t *testing.T) {
	inner := &fakePublisher{}
	p := eventbus.NewContentValidationPublisher(inner)
	ev := eventbus.Event{
		Type:      eventbus.Type("core-communication:say"),
		Rendering: &eventbus.RenderingMetadata{Category: "communication"},
		Payload:   []byte(`{"sender_name":"X","message":"hi"}`), // no text -> invalid
	}
	err := p.Publish(context.Background(), ev)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected an oops error")
	require.Equal(t, "EMIT_CONTENT_INVALID", oopsErr.Code())
	require.Zero(t, len(inner.published), "must not forward on rejection")
}

// TestContentValidationPassesConformingPayload verifies a payload that
// validates as CommunicationContent forwards unchanged to the inner publisher.
func TestContentValidationPassesConformingPayload(t *testing.T) {
	inner := &fakePublisher{}
	p := eventbus.NewContentValidationPublisher(inner)
	ev := eventbus.Event{
		Type:      eventbus.Type("core-communication:say"),
		Rendering: &eventbus.RenderingMetadata{Category: "communication"},
		Payload:   []byte(`{"actor_id":"01H","actor_display_name":"Alaric","text":"hi"}`),
	}
	require.NoError(t, p.Publish(context.Background(), ev))
	require.Len(t, inner.published, 1)
}

// TestContentValidationIgnoresNonCommunicationCategory verifies events whose
// rendering category is not "communication" pass through without decoding or
// validating the payload.
func TestContentValidationIgnoresNonCommunicationCategory(t *testing.T) {
	inner := &fakePublisher{}
	p := eventbus.NewContentValidationPublisher(inner)
	ev := eventbus.Event{
		Type:      eventbus.Type("plugin_integrity_violation"),
		Rendering: &eventbus.RenderingMetadata{Category: "system"},
		Payload:   []byte(`{"anything":true}`),
	}
	require.NoError(t, p.Publish(context.Background(), ev)) // pass-through, no validation
	require.Len(t, inner.published, 1)
}

// TestContentValidationRejectsMalformedJSON exercises the protojson.Unmarshal
// error branch directly (structurally-broken untrusted JSON, distinct from a
// well-formed-but-invalid payload): it MUST reject with EMIT_CONTENT_INVALID
// and MUST NOT forward.
func TestContentValidationRejectsMalformedJSON(t *testing.T) {
	inner := &fakePublisher{}
	p := eventbus.NewContentValidationPublisher(inner)
	ev := eventbus.Event{
		Type:      eventbus.Type("core-communication:say"),
		Rendering: &eventbus.RenderingMetadata{Category: "communication"},
		Payload:   []byte("{"), // truncated -> protojson.Unmarshal fails
	}
	err := p.Publish(context.Background(), ev)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected an oops error")
	require.Equal(t, "EMIT_CONTENT_INVALID", oopsErr.Code())
	require.Zero(t, len(inner.published), "must not forward on malformed payload")
}

// TestContentValidationWrapsInnerPublishFailure verifies a downstream publish
// failure on a conforming communication payload is wrapped with the distinct
// EMIT_PUBLISH_FAILED code (not EMIT_CONTENT_INVALID) at the top level.
func TestContentValidationWrapsInnerPublishFailure(t *testing.T) {
	inner := &fakePublisher{err: oops.Errorf("simulated downstream failure")}
	p := eventbus.NewContentValidationPublisher(inner)
	ev := eventbus.Event{
		Type:      eventbus.Type("core-communication:say"),
		Rendering: &eventbus.RenderingMetadata{Category: "communication"},
		Payload:   []byte(`{"actor_id":"01H","actor_display_name":"Alaric","text":"hi"}`),
	}
	err := p.Publish(context.Background(), ev)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "expected an oops error")
	require.Equal(t, "EMIT_PUBLISH_FAILED", oopsErr.Code())
}

// TestContentValidationPassesThroughWhenNoRenderingMetadata verifies an event
// with nil Rendering bypasses validation and forwards unchanged (the gate only
// validates events explicitly tagged category:communication).
func TestContentValidationPassesThroughWhenNoRenderingMetadata(t *testing.T) {
	inner := &fakePublisher{}
	p := eventbus.NewContentValidationPublisher(inner)
	ev := eventbus.Event{
		Type:    eventbus.Type("core-communication:say"),
		Payload: []byte(`{"anything":true}`),
	}
	require.NoError(t, p.Publish(context.Background(), ev))
	require.Len(t, inner.published, 1)
}
