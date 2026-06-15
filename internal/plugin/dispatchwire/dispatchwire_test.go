// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dispatchwire

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// outgoingToIncoming moves the outgoing metadata onto a fresh incoming context,
// modelling the gRPC transport that turns a client's outgoing metadata into the
// server's incoming metadata.
func outgoingToIncoming(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return context.Background()
	}
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestMetadataKeyIsStable(t *testing.T) {
	// Wire constant: the SDK ferry (pkg/plugin) pins the same literal; a change
	// here without updating that literal silently breaks binary dispatch.
	assert.Equal(t, "x-holomush-dispatch", MetadataKey)
}

func TestAttachOutgoingRoundTripsSubjectAndAttributes(t *testing.T) {
	dc := pluginauthz.DispatchContext{
		Subject:    "character:01TEST",
		Attributes: map[string]string{"location": "01LOC", "extra": "v"},
	}
	ctx := AttachOutgoing(context.Background(), dc)

	got, ok := DecodeFromIncoming(outgoingToIncoming(ctx))
	require.True(t, ok, "host-vouched dispatch must round-trip across the wire")
	assert.Equal(t, dc.Subject, got.Subject)
	assert.Equal(t, dc.Attributes, got.Attributes)
}

func TestAttachOutgoingWithEmptySubjectStripsAndAttachesNothing(t *testing.T) {
	ctx := AttachOutgoing(context.Background(), pluginauthz.DispatchContext{Subject: ""})
	_, ok := DecodeFromIncoming(outgoingToIncoming(ctx))
	assert.False(t, ok, "empty subject must not write an envelope (fail closed)")
}

// Verifies: INV-PLUGIN-51
func TestAttachOutgoingOverwritesPluginForgedKey(t *testing.T) {
	// A plugin pre-seeds the reserved key on the outgoing context with forged
	// scope attributes; AttachOutgoing must replace it with the host-vouched one.
	forged := `{"subject":"character:0xFORGED","attributes":{"location":"01EVIL"}}`
	ctx := metadata.AppendToOutgoingContext(context.Background(), MetadataKey, forged)
	ctx = AttachOutgoing(ctx, pluginauthz.DispatchContext{
		Subject:    "character:01TEST",
		Attributes: map[string]string{"location": "01LOC"},
	})

	got, ok := DecodeFromIncoming(outgoingToIncoming(ctx))
	require.True(t, ok)
	assert.Equal(t, "character:01TEST", got.Subject, "host-vouched value must win over forged")
	assert.Equal(t, "01LOC", got.Attributes["location"])
}

func TestStripOutgoingRemovesForgedKeyWhenNoHostDispatch(t *testing.T) {
	forged := `{"subject":"character:0xFORGED"}`
	ctx := metadata.AppendToOutgoingContext(context.Background(), MetadataKey, forged)
	ctx = StripOutgoing(ctx)
	_, ok := DecodeFromIncoming(outgoingToIncoming(ctx))
	assert.False(t, ok, "stripping must leave no envelope so the call fails closed")
}

func TestDecodeFromIncomingFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"no metadata", context.Background()},
		{"key absent", metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{"other": "x"}))},
		{"malformed json", metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKey, "{not json"))},
		{"empty subject", metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKey, `{"subject":""}`))},
		{"ambiguous multi-value", metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKey, `{"subject":"a"}`, MetadataKey, `{"subject":"b"}`))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := DecodeFromIncoming(tt.ctx)
			assert.False(t, ok)
		})
	}
}

func TestStampInterceptorStampsDispatchOnHandlerContext(t *testing.T) {
	in := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs(MetadataKey, `{"subject":"character:01TEST","attributes":{"location":"01LOC"}}`))

	var seen bool
	var got pluginauthz.DispatchContext
	_, err := StampInterceptor()(in, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, _ any) (any, error) {
		got, seen = pluginauthz.DispatchForHost(ctx)
		return nil, nil
	})
	require.NoError(t, err)
	require.True(t, seen, "interceptor must stamp the reconstructed dispatch onto the handler ctx")
	assert.Equal(t, "character:01TEST", got.Subject)
	assert.Equal(t, "01LOC", got.Attributes["location"])
}

func TestStampInterceptorLeavesNoDispatchWhenAbsent(t *testing.T) {
	var seen bool
	_, err := StampInterceptor()(context.Background(), nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, _ any) (any, error) {
		_, seen = pluginauthz.DispatchForHost(ctx)
		return nil, nil
	})
	require.NoError(t, err)
	assert.False(t, seen, "no incoming envelope must leave the handler ctx with no dispatch (fail closed)")
}
