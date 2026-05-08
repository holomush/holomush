// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// CapabilityCryptoOperator is the grant string that gates break-glass
// crypto operations (Rekey, AdminReadStream). Held by players in the
// crypto.operators config allow-list. MUST be combined with RoleAdmin
// to authorize break-glass operations.
const CapabilityCryptoOperator = "crypto.operator"

// PlayerGrantsAttribute is the bag-key under which PlayerAttributeProvider
// publishes a player's grant set into the Subject attribute bag. This
// constant is the contract between HasPlayerGrant (consumer) and the
// PlayerAttributeProvider (producer); both MUST agree on this exact key
// or grant lookups will silently return empty.
const PlayerGrantsAttribute = "player.grants"

// SubjectResolver is the narrow interface HasPlayerGrant requires from
// the ABAC attribute resolver. *attribute.Resolver satisfies this
// implicitly. Defined in the consuming package so test fakes can
// substitute without importing the full resolver internals.
type SubjectResolver interface {
	ResolveSubjectAttributes(ctx context.Context, subjectID string, action string) (*types.AttributeBags, error)
}

// HasPlayerGrant returns true iff the given player holds the named
// grant. The grant set is resolved through the SubjectResolver, which
// MUST be configured with a PlayerAttributeProvider (see
// internal/access/policy/attribute/player.go) for this to return
// non-empty results.
//
// Returns (false, nil) when the player has no matching grant.
// Returns (false, error) on resolver errors or invalid input. Empty
// playerID and empty grant are rejected at the API boundary with typed
// errors, without invoking the resolver.
func HasPlayerGrant(ctx context.Context, resolver SubjectResolver,
	playerID string, grant string,
) (bool, error) {
	if playerID == "" {
		return false, oops.
			Code("PLAYER_ID_EMPTY").
			With("grant", grant).
			Errorf("playerID must be non-empty")
	}
	if grant == "" {
		return false, oops.
			Code("GRANT_EMPTY").
			With("player_id", playerID).
			Errorf("grant must be non-empty")
	}

	bags, err := resolver.ResolveSubjectAttributes(ctx, PlayerSubject(playerID), "")
	if err != nil {
		return false, oops.
			With("player_id", playerID).
			With("grant", grant).
			Wrap(err)
	}
	if bags == nil {
		// Fail-closed: the SubjectResolver interface contract does not
		// require non-nil bags on success. Treat nil as "no grants".
		return false, nil
	}

	raw, ok := bags.Subject[PlayerGrantsAttribute]
	if !ok {
		return false, nil
	}
	grants, ok := raw.([]string)
	if !ok {
		return false, nil
	}
	for _, g := range grants {
		if g == grant {
			return true, nil
		}
	}
	return false, nil
}
