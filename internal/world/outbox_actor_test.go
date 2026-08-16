// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// TestWorldCallerSubjectReachesTheOutboxActorByteForByte is the automated
// detector for T-02.1-06 / T-02.1-13: the world-change outbox envelope Actor is
// an AUDIT-bearing field, and the caller-model flip moved the string that feeds
// it behind an opaque Caller value.
//
// Reading the migration diff cannot prove this. A normalization applied INSIDE
// the caller value — lowercasing, trimming, re-prefixing — would leave every
// constructor argument character-for-character unchanged in the diff and leave
// every pre-existing suite green, while silently rewriting persisted audit
// payloads. Only driving buildIntent / buildMoveIntent from a real Caller and
// comparing the resulting wmodel.IntentParams.Actor against the raw input string
// detects it.
//
// This test is in `package world` (not world_test) deliberately: it must read
// Caller's unexported subject field and call the unexported intent builders.
// Assertions are require.Equal on raw Go strings — byte equality, never a
// case-folded or normalized comparison.
func TestWorldCallerSubjectReachesTheOutboxActorByteForByte(t *testing.T) {
	charULID := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")

	tests := []struct {
		name   string
		caller Caller
		// wantActor is the exact byte sequence the envelope Actor must carry.
		wantActor string
	}{
		{
			name:      "character subject survives verbatim",
			caller:    HumanCaller("character:" + charULID.String()),
			wantActor: "character:01ARZ3NDEKTSV4RRFFQ69G5FAV",
		},
		{
			name:      "plugin subject survives verbatim",
			caller:    HumanCaller("plugin:core-scenes"),
			wantActor: "plugin:core-scenes",
		},
		{
			// The bootstrap subject is one character from the S1 bypass literal.
			// If anything normalized it to "system" the four bootstrap writes
			// would become total ABAC bypasses AND the audit trail would lie.
			name:      "bootstrap subject is not normalized toward the bare system literal",
			caller:    HumanCaller("system:bootstrap"),
			wantActor: "system:bootstrap",
		},
		{
			name:      "system caller carries the bare system subject",
			caller:    SystemCaller(),
			wantActor: "system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{gameID: defaultGameID}

			// The caller's subject is what every migrated command hands to the
			// intent builders by direct field access.
			require.Equal(t, tt.wantActor, tt.caller.subject,
				"caller must carry the subject string verbatim")

			intent := svc.buildIntent(
				kindLocationUpdated,
				wmodel.AggregateLocation,
				charULID,
				tt.caller.subject,
				[]byte(`{}`),
			)
			require.Equal(t, tt.wantActor, intent.Actor,
				"buildIntent must write the caller's subject to the envelope Actor byte-for-byte")

			moveIntent, err := svc.buildMoveIntent(
				&Character{ID: charULID},
				tt.caller.subject,
				charULID,
				charULID,
			)
			require.NoError(t, err)
			require.Equal(t, tt.wantActor, moveIntent.Actor,
				"buildMoveIntent must write the caller's subject to the envelope Actor byte-for-byte")
		})
	}
}
