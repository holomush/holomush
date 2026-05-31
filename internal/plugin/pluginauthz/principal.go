// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz

import (
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
)

// SettingsGameWriteResource is the ABAC resource a GAME-scope SetSetting writes
// to. It is the single source of truth shared by the binary
// (PluginHostService.SetSetting) and Lua (holomush.set_setting) surfaces so the
// two runtimes cannot drift onto different operator-permission resources
// (plugin-runtime-symmetry, INV-8). Only operator subjects are granted "write"
// on it; a non-operator plugin/character is denied.
const SettingsGameWriteResource = "setting:game"

// CheckPrincipalOwnership parses principalID as a ULID and enforces that the
// acting actor owns it (principalID == actor.ID). It is the single
// runtime-neutral ownership gate shared by the binary
// (PluginHostService.GetSetting/SetSetting) and Lua
// (holomush.get_setting/set_setting) settings surfaces, so the trust check
// cannot diverge between runtimes (plugin-runtime-symmetry, INV-8).
//
// Identity recovery legitimately differs per runtime — the binary path
// recovers the actor from the dispatch token, the Lua path from
// core.ActorFromContext — but BOTH feed the recovered actor here so the
// ownership comparison is identical.
//
// Returns:
//   - oops.Code("INVALID_PRINCIPAL_ID") when principalID is empty or not a
//     valid ULID. Callers crossing the gRPC boundary map this to
//     codes.InvalidArgument ("invalid principal_id").
//   - oops.Code("PRINCIPAL_NOT_OWNED") when principalID is well-formed but
//     does not equal the acting actor's ID. Callers map this to
//     codes.PermissionDenied ("permission denied").
//
// For CHARACTER scope this is correct and functional: the recovered actor is
// always an ActorCharacter, so principalID == character ID is the expected
// comparison (holomush-iokti.16).
//
// For PLAYER scope the INTENDED semantics are "the owning player of the acting
// character" (spec §3.3; holomush-iokti.16 decision). The host-side char→player
// resolver is DEFERRED to holomush-iokti.19. Until it lands the gate is
// fail-closed: a player's ULID differs from the acting character's ULID
// (distinct entities), so any real player-principal PLAYER request is denied.
// This is a deliberate interim contract, NOT a bug.
func CheckPrincipalOwnership(principalID string, actor core.Actor) (ulid.ULID, error) {
	pid, err := ulid.Parse(principalID)
	if err != nil {
		return ulid.ULID{}, oops.Code("INVALID_PRINCIPAL_ID").
			With("principal_id", principalID).
			Wrap(err)
	}
	// Compare against the recovered actor ID, never a caller-supplied field.
	if principalID != actor.ID {
		return ulid.ULID{}, oops.Code("PRINCIPAL_NOT_OWNED").
			Errorf("principal not owned by acting actor")
	}
	return pid, nil
}
