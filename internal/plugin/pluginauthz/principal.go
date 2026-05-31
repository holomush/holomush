// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz

import (
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// SettingsGameWriteResource is the ABAC resource a GAME-scope SetSetting writes
// to. It is the single source of truth shared by the binary
// (PluginHostService.SetSetting) and Lua (holomush.set_setting) surfaces so the
// two runtimes cannot drift onto different operator-permission resources
// (plugin-runtime-symmetry, INV-8). Only operator subjects are granted "write"
// on it; a non-operator plugin/character is denied.
const SettingsGameWriteResource = "setting:game"

// CheckPrincipalOwnership parses principalID as a ULID and enforces that it
// equals expectedOwnerID — the host-vouched owner the caller is permitted to
// act on behalf of. It is the single runtime-neutral ownership gate shared by
// the binary (PluginHostService.GetSetting/SetSetting) and Lua
// (holomush.get_setting/set_setting) settings surfaces, so the trust check
// cannot diverge between runtimes (plugin-runtime-symmetry, INV-8).
//
// Identity recovery legitimately differs per runtime — the binary path recovers
// the expected owner from the dispatch token (emit_token_store), the Lua path
// from core.OwningPlayerFromContext / core.ActorFromContext — but BOTH feed the
// host-vouched expected owner here so the comparison is identical. expectedOwnerID
// MUST originate from the host-stamped token/ctx, NEVER from a plugin- or
// Lua-supplied argument.
//
// Per-scope expectedOwnerID (set by the caller):
//   - CHARACTER: the acting character's ID (the dispatch-token actor is always
//     an ActorCharacter, so principalID == character ID is the expected match).
//   - PLAYER: the host-vouched owning player ULID of the acting character. PLAYER
//     scope is now FUNCTIONAL: when the dispatch carried a player context the
//     owning player is the expected owner and a matching principalID succeeds
//     (holomush-iokti.19). When no owning player was vouched (e.g. a pure event
//     dispatch with no player), expectedOwnerID is "" and the request fails
//     closed below.
//
// Returns:
//   - oops.Code("INVALID_PRINCIPAL_ID") when principalID is empty or not a valid
//     ULID. Callers crossing the gRPC boundary map this to codes.InvalidArgument
//     ("invalid principal_id").
//   - oops.Code("PRINCIPAL_NOT_OWNED") when expectedOwnerID is empty (no
//     host-vouched owner — fail closed) or when principalID is well-formed but
//     does not equal expectedOwnerID. Callers map this to codes.PermissionDenied
//     ("permission denied").
func CheckPrincipalOwnership(principalID, expectedOwnerID string) (ulid.ULID, error) {
	pid, err := ulid.Parse(principalID)
	if err != nil {
		return ulid.ULID{}, oops.Code("INVALID_PRINCIPAL_ID").
			With("principal_id", principalID).
			Wrap(err)
	}
	// Fail closed when the host vouched no owner (empty expected owner). This is
	// the PLAYER-from-event case: no authenticated player ⇒ no PLAYER ownership.
	if expectedOwnerID == "" {
		return ulid.ULID{}, oops.Code("PRINCIPAL_NOT_OWNED").
			Errorf("no host-vouched owner for principal")
	}
	// Compare against the host-vouched expected owner, never a caller-supplied field.
	if principalID != expectedOwnerID {
		return ulid.ULID{}, oops.Code("PRINCIPAL_NOT_OWNED").
			Errorf("principal not owned by acting actor")
	}
	return pid, nil
}
