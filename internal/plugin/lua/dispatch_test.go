// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// Verifies: INV-PLUGIN-51
func TestStampDispatchStampsHostVouchedSubjectForCharacterActor(t *testing.T) {
	charID := core.NewULID().String()
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})

	got := stampDispatch(ctx)

	dc, ok := pluginauthz.DispatchForHost(got)
	require.True(t, ok, "dispatch context must be stamped for a character actor")
	assert.Equal(t, access.CharacterSubject(charID), dc.Subject, "host-vouched subject")
	assert.Nil(t, dc.Attributes, "attributes resolved by a follow-up wiring task")
}

func TestStampDispatchLeavesNonCharacterActorUnchanged(t *testing.T) {
	base := context.Background()

	// No actor on ctx.
	_, ok := pluginauthz.DispatchForHost(stampDispatch(base))
	assert.False(t, ok, "missing actor must not stamp a dispatch subject")

	// Plugin actor.
	plug := core.WithActor(base, core.Actor{Kind: core.ActorPlugin, ID: core.NewULID().String()})
	_, ok = pluginauthz.DispatchForHost(stampDispatch(plug))
	assert.False(t, ok, "plugin actor must not stamp a dispatch subject")

	// Character actor with empty ID: fail-closed.
	empty := core.WithActor(base, core.Actor{Kind: core.ActorCharacter, ID: ""})
	_, ok = pluginauthz.DispatchForHost(stampDispatch(empty))
	assert.False(t, ok, "empty-ID character actor must not stamp a dispatch subject")
}
