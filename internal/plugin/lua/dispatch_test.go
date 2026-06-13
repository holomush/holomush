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

	got := NewHost().stampDispatch(ctx)

	dc, ok := pluginauthz.DispatchForHost(got)
	require.True(t, ok, "dispatch context must be stamped for a character actor")
	assert.Equal(t, access.CharacterSubject(charID), dc.Subject, "host-vouched subject")
	assert.Nil(t, dc.Attributes, "no resolver wired ⇒ Attributes nil")
}

func TestStampDispatchLeavesNonCharacterActorUnchanged(t *testing.T) {
	base := context.Background()
	h := NewHost()

	// No actor on ctx.
	_, ok := pluginauthz.DispatchForHost(h.stampDispatch(base))
	assert.False(t, ok, "missing actor must not stamp a dispatch subject")

	// Plugin actor.
	plug := core.WithActor(base, core.Actor{Kind: core.ActorPlugin, ID: core.NewULID().String()})
	_, ok = pluginauthz.DispatchForHost(h.stampDispatch(plug))
	assert.False(t, ok, "plugin actor must not stamp a dispatch subject")

	// Character actor with empty ID: fail-closed.
	empty := core.WithActor(base, core.Actor{Kind: core.ActorCharacter, ID: ""})
	_, ok = pluginauthz.DispatchForHost(h.stampDispatch(empty))
	assert.False(t, ok, "empty-ID character actor must not stamp a dispatch subject")
}

// fakeAttrResolver is a test double for pluginauthz.AttributeResolver.
type fakeAttrResolver struct {
	attrs map[string]any
	err   error
}

func (f fakeAttrResolver) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return f.attrs, f.err
}

// Verifies: INV-PLUGIN-51
func TestStampDispatchResolvesCharacterAttributesWhenResolverWired(t *testing.T) {
	charID := core.NewULID().String()
	ctx := core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: charID})

	t.Run("projects string-valued attributes and drops non-strings", func(t *testing.T) {
		h := NewHost(WithDispatchAttributeResolver(fakeAttrResolver{
			attrs: map[string]any{
				"location":     "01LOC",
				"has_location": true,
				"roles":        []string{"player"},
			},
		}))
		dc, ok := pluginauthz.DispatchForHost(h.stampDispatch(ctx))
		require.True(t, ok, "character actor must stamp a dispatch subject")
		assert.Equal(t, access.CharacterSubject(charID), dc.Subject, "host-vouched subject")
		require.NotNil(t, dc.Attributes, "string attributes must be projected")
		assert.Equal(t, "01LOC", dc.Attributes["location"], "location attribute projected")
		_, hasBool := dc.Attributes["has_location"]
		assert.False(t, hasBool, "non-string attribute (bool) must be dropped")
		_, hasSlice := dc.Attributes["roles"]
		assert.False(t, hasSlice, "non-string attribute (slice) must be dropped")
	})

	t.Run("resolver error is fail-closed with nil attributes", func(t *testing.T) {
		h := NewHost(WithDispatchAttributeResolver(fakeAttrResolver{err: assert.AnError}))
		dc, ok := pluginauthz.DispatchForHost(h.stampDispatch(ctx))
		require.True(t, ok, "subject is still stamped on resolver error")
		assert.Equal(t, access.CharacterSubject(charID), dc.Subject, "host-vouched subject")
		assert.Nil(t, dc.Attributes, "resolver error leaves Attributes nil (fail-closed)")
	})

	t.Run("nil resolver leaves attributes nil", func(t *testing.T) {
		dc, ok := pluginauthz.DispatchForHost(NewHost().stampDispatch(ctx))
		require.True(t, ok, "character actor must stamp a dispatch subject")
		assert.Nil(t, dc.Attributes, "no resolver wired ⇒ Attributes nil (current behavior)")
	})

	t.Run("resolver returning only non-string attributes yields nil", func(t *testing.T) {
		h := NewHost(WithDispatchAttributeResolver(fakeAttrResolver{
			attrs: map[string]any{"has_location": false},
		}))
		dc, ok := pluginauthz.DispatchForHost(h.stampDispatch(ctx))
		require.True(t, ok, "character actor must stamp a dispatch subject")
		assert.Nil(t, dc.Attributes, "no string-valued attributes ⇒ nil, not empty map")
	})
}
