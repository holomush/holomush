// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
)

// AssertOperatorAdmin re-asserts the two INV-CRYPTO-83 defense-in-depth gates
// for an authenticated operator: (1) the player still holds the
// crypto.operator capability, and (2) at least one of their characters
// still holds the admin role.
//
// Three sites use this exact pair: Authenticate (steps 4-5),
// Approve, and ResetTOTP. Sharing the helper here prevents the three
// sites from drifting (e.g., one of them silently removing a check).
//
// Returns nil if both checks pass. On a denial, returns a typed oops
// error whose code is one of:
//
//   - DENY_NOT_OPERATOR        — capability missing
//   - DENY_NOT_ADMIN_ROLE      — admin role missing
//   - INGAME_GRANT_LOOKUP_FAILED — resolver call failed (infra)
//   - INGAME_ROLE_LOOKUP_FAILED  — role-store call failed (infra)
//
// The DENY_* codes round-trip through MapDenyToConnect to PermissionDenied;
// the infrastructure codes fall through to CodeInternal at handlers that
// unconditionally call MapDenyToConnect, or callers can route them
// explicitly. ingame.Authenticate returns the error verbatim because its
// surrounding contract is to produce a typed oops error, not a Connect
// error.
func AssertOperatorAdmin(
	ctx context.Context,
	resolver access.SubjectResolver,
	roleStore PlayerRoleHasher,
	playerID string,
) error {
	hasCap, err := access.HasPlayerGrant(ctx, resolver, playerID, access.CapabilityCryptoOperator)
	if err != nil {
		return oops.Code("INGAME_GRANT_LOOKUP_FAILED").
			With("player_id", playerID).Wrap(err)
	}
	if !hasCap {
		return oops.Code("DENY_NOT_OPERATOR").
			With("player_id", playerID).
			Errorf("crypto.operator capability absent")
	}
	hasRole, err := roleStore.PlayerHasRole(ctx, playerID, access.RoleAdmin)
	if err != nil {
		return oops.Code("INGAME_ROLE_LOOKUP_FAILED").
			With("player_id", playerID).Wrap(err)
	}
	if !hasRole {
		return oops.Code("DENY_NOT_ADMIN_ROLE").
			With("player_id", playerID).
			Errorf("admin role absent")
	}
	return nil
}
