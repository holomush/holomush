// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
)

func TestWithOutgoingActorMetadataOverwritesExistingValues(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		actorKindHeader, "0",
		actorIDHeader, "char-old",
		"other-header", "keep-me",
	))

	ctx = WithOutgoingActorMetadata(ctx, ActorSystem, "system-core")

	kind, id, ok := ActorMetadataFromOutgoingContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, ActorSystem, kind)
	assert.Equal(t, "system-core", id)

	md, ok := metadata.FromOutgoingContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, []string{"keep-me"}, md.Get("other-header"))
	assert.Equal(t, []string{"1"}, md.Get(actorKindHeader))
	assert.Equal(t, []string{"system-core"}, md.Get(actorIDHeader))
}

func TestActorMetadataFromContextPrefersIncomingMetadataOverOutgoingMetadata(t *testing.T) {
	ctx := WithOutgoingActorMetadata(context.Background(), ActorPlugin, "plugin-fallback")
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		actorKindHeader, "0",
		actorIDHeader, "char-alice",
	))

	kind, id, ok := actorMetadataFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, ActorCharacter, kind)
	assert.Equal(t, "char-alice", id)
}

func TestActorMetadataFromContextReturnsFalseForMalformedIncomingMetadata(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		actorKindHeader, "not-a-number",
		actorIDHeader, "char-alice",
	))

	kind, id, ok := actorMetadataFromContext(ctx)
	assert.False(t, ok)
	assert.Equal(t, ActorKind(0), kind)
	assert.Empty(t, id)
}

func TestContextWithIncomingActorMetadataLeavesContextUnchangedWhenMetadataMissing(t *testing.T) {
	ctx := context.Background()
	updated := contextWithIncomingActorMetadata(ctx)

	kind, id, ok := actorMetadataFromContext(updated)
	assert.False(t, ok)
	assert.Equal(t, ActorKind(0), kind)
	assert.Empty(t, id)
}

func TestActorMetadataFromOutgoingContextReturnsFalseWhenMissing(t *testing.T) {
	kind, id, ok := ActorMetadataFromOutgoingContext(context.Background())
	assert.False(t, ok)
	assert.Equal(t, ActorKind(0), kind)
	assert.Empty(t, id)
}

func TestContextWithIncomingActorMetadataStoresParsedMetadata(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		actorKindHeader, "2",
		actorIDHeader, "core-scenes",
	))

	updated := contextWithIncomingActorMetadata(ctx)

	kind, id, ok := actorMetadataFromContext(updated)
	assert.True(t, ok)
	assert.Equal(t, ActorPlugin, kind)
	assert.Equal(t, "core-scenes", id)
}

func TestActorMetadataFromMetadataReturnsFalseWhenIDMissing(t *testing.T) {
	kind, id, ok := actorMetadataFromMetadata(metadata.Pairs(actorKindHeader, "0"))
	assert.False(t, ok)
	assert.Equal(t, ActorKind(0), kind)
	assert.Empty(t, id)
}
