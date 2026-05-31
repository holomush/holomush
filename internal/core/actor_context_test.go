// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/core"
)

func TestWithActorRoundTripsTheStampedActor(t *testing.T) {
	t.Parallel()
	actor := core.Actor{Kind: core.ActorCharacter, ID: "01HCHAR0000000000000000000"}
	ctx := core.WithActor(context.Background(), actor)

	got, ok := core.ActorFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, actor, got)
}

func TestActorFromContextReturnsFalseWhenAbsent(t *testing.T) {
	t.Parallel()
	_, ok := core.ActorFromContext(context.Background())
	assert.False(t, ok)
}

func TestWithOwningPlayerRoundTripsTheStampedPlayerID(t *testing.T) {
	t.Parallel()
	playerID := core.NewULID().String()
	ctx := core.WithOwningPlayer(context.Background(), playerID)

	got, ok := core.OwningPlayerFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, playerID, got)
}

func TestOwningPlayerFromContextReturnsFalseWhenAbsent(t *testing.T) {
	t.Parallel()
	got, ok := core.OwningPlayerFromContext(context.Background())
	assert.False(t, ok)
	assert.Empty(t, got)
}
