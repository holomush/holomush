// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package eventbus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// TestRenderingMetadataGoProtoParity is INV-GW-14. The Go struct and
// proto message MUST stay in sync — round-tripping through both
// conversion helpers MUST produce equal values.
func TestRenderingMetadataGoProtoParity(t *testing.T) {
	src := &eventbus.RenderingMetadata{
		Category:            "communication",
		Format:              "speech",
		Label:               "says",
		DisplayTarget:       eventbus.EventChannelTerminal,
		SourcePlugin:        "core-communication",
		SourcePluginVersion: "0.1.0",
	}

	proto := eventbus.RenderingToProto(src)
	require.NotNil(t, proto)
	assert.Equal(t, "communication", proto.GetCategory())
	assert.Equal(t, "speech", proto.GetFormat())
	assert.Equal(t, "says", proto.GetLabel())
	assert.Equal(t, corev1.EventChannel_EVENT_CHANNEL_TERMINAL, proto.GetDisplayTarget())
	assert.Equal(t, "core-communication", proto.GetSourcePlugin())
	assert.Equal(t, "0.1.0", proto.GetSourcePluginVersion())

	roundTrip := eventbus.RenderingFromProto(proto)
	assert.Equal(t, src, roundTrip)
}

func TestRenderingMetadataNilRoundTrip(t *testing.T) {
	assert.Nil(t, eventbus.RenderingToProto(nil))
	assert.Nil(t, eventbus.RenderingFromProto(nil))
}

// TestEventChannelEnumParity asserts the Go-side mirror values match
// the proto enum values. INV-GW-14 (the parity dimension covering
// the EventChannel mirror specifically).
func TestEventChannelEnumParity(t *testing.T) {
	cases := []struct {
		goVal    eventbus.EventChannel
		protoVal corev1.EventChannel
	}{
		{eventbus.EventChannelUnspecified, corev1.EventChannel_EVENT_CHANNEL_UNSPECIFIED},
		{eventbus.EventChannelTerminal, corev1.EventChannel_EVENT_CHANNEL_TERMINAL},
		{eventbus.EventChannelState, corev1.EventChannel_EVENT_CHANNEL_STATE},
		{eventbus.EventChannelBoth, corev1.EventChannel_EVENT_CHANNEL_BOTH},
		{eventbus.EventChannelAuditOnly, corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY},
	}
	for _, c := range cases {
		assert.Equal(t, int32(c.goVal), int32(c.protoVal))
	}
}

// TestNoPlaintextReasonEnumParity asserts Go-side NoPlaintextReason mirror
// values match proto enum values (INV-GW-14 — holomush-ojw1.6).
//
// The length check catches one-sided enum extension: if a new value is added
// to the proto without a Go-side mirror (or vice versa), the test fails
// before the per-value comparison would silently miss the new entry.
func TestNoPlaintextReasonEnumParity(t *testing.T) {
	cases := []struct {
		goVal    eventbus.NoPlaintextReason
		protoVal corev1.NoPlaintextReason
	}{
		{eventbus.NoPlaintextReasonUnspecified, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_UNSPECIFIED},
		{eventbus.NoPlaintextReasonAuthGuardDeny, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_AUTHGUARD_DENY},
		{eventbus.NoPlaintextReasonStaleDEK, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_STALE_DEK},
		{eventbus.NoPlaintextReasonAuditQueueFull, corev1.NoPlaintextReason_NO_PLAINTEXT_REASON_AUDIT_QUEUE_FULL},
	}
	assert.Len(t, cases, len(corev1.NoPlaintextReason_name),
		"every proto NoPlaintextReason value MUST have a Go-side mirror in cases above; "+
			"if a new value was added on one side only, mirror it on the other side")
	for _, c := range cases {
		assert.Equal(t, int32(c.goVal), int32(c.protoVal),
			"Go NoPlaintextReason and proto NoPlaintextReason must have equal numeric values")
	}
}
