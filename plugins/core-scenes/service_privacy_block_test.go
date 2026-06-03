// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// These tests pin INV-SCENE-36 / spec §10: the privacy-boundary block fires a
// THREE-part signal (WARN log + metric + span error) and emits ZERO IC events
// (no side channel that leaks the attempt's existence to a non-participant).
// They exercise emitPrivacyBoundaryBlock (publish_service.go, Task B5) directly.

// TestPrivacyBoundaryBlockEmitsWarnLogWithFullAttributeSet — signal 1.
// NOT parallel: swaps the global slog default to capture output.
func TestPrivacyBoundaryBlockEmitsWarnLogWithFullAttributeSet(t *testing.T) {
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	svc := newTestService(t, newFakeStore())
	svc.emitPrivacyBoundaryBlock(context.Background(), "GetPublishedScene", "scene-x", "caller-y", "not_participant")

	out := buf.String()
	assert.Contains(t, out, "scene privacy boundary block")
	assert.Contains(t, out, "operation=GetPublishedScene")
	assert.Contains(t, out, "scene_id=scene-x")
	assert.Contains(t, out, "caller_id=caller-y")
	assert.Contains(t, out, "denial_reason=not_participant")
	assert.Contains(t, out, "code=SCENE_PRIVACY_BOUNDARY_BLOCK")
}

// TestPrivacyBoundaryBlockIncrementsMetric — signal 2.
// NOT parallel: swaps the package-level metric hook.
func TestPrivacyBoundaryBlockIncrementsMetric(t *testing.T) {
	prev := metricScenePublishPrivacyBlock
	var gotOp, gotReason string
	var calls int
	metricScenePublishPrivacyBlock = func(operation, reason string) {
		calls++
		gotOp, gotReason = operation, reason
	}
	t.Cleanup(func() { metricScenePublishPrivacyBlock = prev })

	svc := newTestService(t, newFakeStore())
	svc.emitPrivacyBoundaryBlock(context.Background(), "DownloadPublishedScene", "scene-x", "caller-y", "not_participant")

	assert.Equal(t, 1, calls, "the privacy-block metric MUST be incremented exactly once")
	assert.Equal(t, "DownloadPublishedScene", gotOp)
	assert.Equal(t, "not_participant", gotReason)
}

// TestPrivacyBoundaryBlockEmitsNoICEvent — the NEGATIVE invariant that
// accompanies the three signals (side-channel prevention). A denial MUST NOT
// surface any event on the scene IC stream that could leak the attempt's
// existence. emitPrivacyBoundaryBlock references neither s.eventSink nor
// s.events, so an empty recording sink proves no event fired.
func TestPrivacyBoundaryBlockEmitsNoICEvent(t *testing.T) {
	t.Parallel()
	sink := &recordingEventSink{}
	svc := newTestService(t, newFakeStore())
	svc.SetEventSink(sink)

	svc.emitPrivacyBoundaryBlock(context.Background(), "GetPublishedScene", "scene-x", "caller-y", "not_participant")

	assert.Empty(t, sink.intents, "a privacy-boundary block MUST NOT emit any IC event (no side channel)")
}

// TestPrivacyBoundaryBlockSetsSpanError — signal 3.
// NOT parallel: swaps the global OTel TracerProvider to record spans.
func TestPrivacyBoundaryBlockSetsSpanError(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	svc := newTestService(t, newFakeStore())
	ctx, span := startSpan(context.Background(), "test.privacy_block")
	svc.emitPrivacyBoundaryBlock(ctx, "GetPublishedScene", "scene-x", "caller-y", "not_participant")
	span.End()

	ended := recorder.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, otelcodes.Error, ended[0].Status().Code, "span status MUST be Error on a privacy block")

	var denyReason string
	var found bool
	for _, kv := range ended[0].Attributes() {
		if string(kv.Key) == "deny.reason" {
			found = true
			denyReason = kv.Value.AsString()
		}
	}
	assert.True(t, found, "span MUST carry the deny.reason attribute")
	assert.Equal(t, "not_participant", denyReason)
}
