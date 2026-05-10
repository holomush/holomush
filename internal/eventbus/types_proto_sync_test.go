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
