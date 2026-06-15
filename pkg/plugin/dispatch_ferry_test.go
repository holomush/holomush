// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func outgoingDispatch(ctx context.Context) []string {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return nil
	}
	return md.Get(dispatchMetadataHeader)
}

func TestDispatchHeaderMatchesWireConstant(t *testing.T) {
	// Pins the SDK literal to the host's dispatchwire.MetadataKey; a drift here
	// silently breaks binary dispatch propagation (no shared symbol across the
	// internal/SDK boundary, mirroring x-holomush-emit-token).
	assert.Equal(t, "x-holomush-dispatch", dispatchMetadataHeader)
}

func TestFerryDispatchForwardsHostDeliveredEnvelope(t *testing.T) {
	env := `{"subject":"character:01TEST","attributes":{"location":"01LOC"}}`
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(dispatchMetadataHeader, env))

	got := outgoingDispatch(ferryDispatch(ctx))
	require.Len(t, got, 1, "the host-delivered envelope must be ferried onto the outgoing call")
	assert.Equal(t, env, got[0])
}

func TestFerryDispatchDropsPluginForgedOutgoingWhenNoDelivery(t *testing.T) {
	// Plugin forges the reserved key on its outgoing call with no host-delivered
	// dispatch on the incoming side: nothing host-vouched exists, so the forged
	// value MUST be stripped and not forwarded (fail closed on the host).
	forged := `{"subject":"character:0xFORGED","attributes":{"location":"01EVIL"}}`
	ctx := metadata.AppendToOutgoingContext(context.Background(), dispatchMetadataHeader, forged)

	assert.Empty(t, outgoingDispatch(ferryDispatch(ctx)),
		"plugin-forged outgoing dispatch must be stripped, never forwarded")
}

func TestFerryDispatchHostVouchedWinsOverForgedOutgoing(t *testing.T) {
	// Plugin forges an outgoing value AND a host envelope was delivered: only the
	// host-vouched delivered value may be forwarded.
	forged := `{"subject":"character:0xFORGED"}`
	delivered := `{"subject":"character:01TEST","attributes":{"location":"01LOC"}}`
	ctx := metadata.AppendToOutgoingContext(context.Background(), dispatchMetadataHeader, forged)
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(dispatchMetadataHeader, delivered))

	got := outgoingDispatch(ferryDispatch(ctx))
	require.Len(t, got, 1, "exactly one value: the host-delivered envelope")
	assert.Equal(t, delivered, got[0])
}

func TestFerryDispatchForwardsNothingWhenNoDispatchAnywhere(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("other", "x"))
	assert.Empty(t, outgoingDispatch(ferryDispatch(ctx)))
}

func TestFerryDispatchDropsAmbiguousIncoming(t *testing.T) {
	// Two delivered values is ambiguous; the host decoder fails closed on
	// len != 1, so the ferry must not forward either (matching semantics).
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs(dispatchMetadataHeader, `{"subject":"a"}`, dispatchMetadataHeader, `{"subject":"b"}`))
	assert.Empty(t, outgoingDispatch(ferryDispatch(ctx)))
}
