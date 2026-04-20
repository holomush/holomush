// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus"
)

// TestClassifySubjectExactReturnedForLiteral verifies the happy path where
// no wildcards appear: exact is the input, pattern is empty.
func TestClassifySubjectExactReturnedForLiteral(t *testing.T) {
	t.Parallel()
	exact, pattern := classifySubject("events.main.character.01HYXYZCHAR0000000000000CH")
	assert.Equal(t, "events.main.character.01HYXYZCHAR0000000000000CH", exact)
	assert.Empty(t, pattern)
}

func TestClassifySubjectEmptyInputReturnsEmpty(t *testing.T) {
	t.Parallel()
	exact, pattern := classifySubject("")
	assert.Empty(t, exact)
	assert.Empty(t, pattern)
}

func TestClassifySubjectWildcardProducesPattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		pattern string
	}{
		{"single token wildcard", "events.main.character.*", "events.main.character.%"},
		{"terminal gt wildcard", "events.main.>", "events.main.%"},
		{"both wildcards", "events.*.>", "events.%.%"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			exact, pattern := classifySubject(tc.input)
			assert.Empty(t, exact, "wildcards never produce an exact")
			assert.Equal(t, tc.pattern, pattern)
		})
	}
}

func TestClassifySubjectEscapesLikeMetacharacters(t *testing.T) {
	t.Parallel()
	// The % and _ characters are LIKE metacharacters; they must be
	// backslash-escaped so the resulting pattern treats them as literal.
	exact, pattern := classifySubject("events.main.channel.foo_%bar.*")
	assert.Empty(t, exact)
	assert.Contains(t, pattern, `\_`)
	assert.Contains(t, pattern, `\%`)
	// The trailing * becomes %.
	assert.True(t, pattern[len(pattern)-1] == '%')
}

// TestActorFromAuditRowEveryKind exercises every known kind.
func TestActorFromAuditRowEveryKind(t *testing.T) {
	t.Parallel()
	id := ulid.MustNew(1, nil)
	idBytes := id.Bytes()

	tests := []struct {
		name     string
		kindStr  string
		wantKind eventbus.ActorKind
	}{
		{"character", "character", eventbus.ActorKindCharacter},
		{"player", "player", eventbus.ActorKindPlayer},
		{"system", "system", eventbus.ActorKindSystem},
		{"plugin", "plugin", eventbus.ActorKindPlugin},
		{"unknown falls through to unknown", "WEIRD", eventbus.ActorKindUnknown},
		{"empty string is unknown", "", eventbus.ActorKindUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := actorFromAuditRow(tc.kindStr, idBytes)
			assert.Equal(t, tc.wantKind, a.Kind)
			assert.Equal(t, id, a.ID)
		})
	}
}

func TestActorFromAuditRowIgnoresWrongSizeID(t *testing.T) {
	t.Parallel()
	a := actorFromAuditRow("character", []byte{1, 2, 3})
	assert.Equal(t, eventbus.ActorKindCharacter, a.Kind)
	var zero ulid.ULID
	assert.Equal(t, zero, a.ID, "non-16-byte id is ignored")
}

func TestActorFromAuditRowEmptyIDLeavesZeroULID(t *testing.T) {
	t.Parallel()
	a := actorFromAuditRow("system", nil)
	assert.Equal(t, eventbus.ActorKindSystem, a.Kind)
	var zero ulid.ULID
	assert.Equal(t, zero, a.ID)
}
