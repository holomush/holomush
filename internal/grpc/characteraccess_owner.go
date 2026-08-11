// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"

	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// The OWNER audience.
//
// # Why it exists at all, and why it constructs no viewer principal
//
// Phase 2's D-27 left the derived owner peer
// (internal/access/policy/attribute/property.go, resolveDerivedPlayerPeers)
// leaning the ALL direction: a `viewer:` principal enters a row's permit only
// when EVERY character of that player appears in the row's character-keyed
// field. entity_properties.owner is a SCALAR column, so for ownership that
// reduces to "the owning character is that player's only character" — a player
// holding two characters cannot read their own private row through a viewer
// principal.
//
// D-69 resolved that by SPLITTING THE AUDIENCES rather than by relaxing the
// peer. The owner reaches their own rows here, through their CHARACTER's
// subject, where the grid-side row-keyed policies (seed:property-private-read
// and its siblings) already say the right thing and have said it since Phase 2.
// Widening the viewer-side peer to the ANY direction would have worked too —
// and would have broadcast alt linkage across every one of that player's
// characters, which is the disclosure the privacy phase exists to prevent.
//
// SO: NO HANDLER IN THIS FILE MAY CONSTRUCT A `viewer:` PRINCIPAL. Not for the
// property enumeration, not as a fallback, not as a convenience view of the
// caller's own public profile. The moment one does, the split stops being a
// split and the ALL/ANY asymmetry starts governing a path someone depends on —
// at which point relaxing it looks like a bug fix. INV-ACCESS-15 pins the other
// half (that the viewer path never widens); this file is the half that makes
// keeping it cheap.
//
// The gate order is the shipped one: resolveAndGate first (session + guest
// gate, INV-SCENE-64), then ownedCharacter for a single-character read
// (INV-SCENE-63). A client-supplied character id is never trusted.

// ListMyCharacters returns the authenticated caller's own roster.
//
// RETIRED CHARACTERS ARE INCLUDED (01-SPEC §4.5 property 1). A roster that hid
// them would put un-retire out of reach of the only surface that can perform
// it, and would make a name reservation the player still holds look released.
//
// THE ROSTER CARRIES NO PROFILE MAP, AND THAT IS THE SHAPE, NOT A STUB. §9.2
// gives the two owner reads distinct jobs: this one is the roster the edit
// surface picks FROM, GetMyCharacter is "one owned character in full, FOR the
// edit surfaces". Enumerating every character's property rows here would make
// GetMyCharacter redundant and turn a roster read into N+1 ABAC-filtered
// enumerations. Identity, lifecycle status and the concurrency token are what a
// picker needs; the rows arrive when a character is opened.
//
// There is deliberately no player-id input: whose roster it is follows from who
// is asking, resolved server-side from the session.
func (s *CharacterAccessServer) ListMyCharacters(ctx context.Context, req *characteraccessv1.ListMyCharactersRequest) (*characteraccessv1.ListMyCharactersResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}

	chars, err := s.charRepo.ListByPlayer(ctx, ps.PlayerID)
	if err != nil {
		// An OUTAGE, not an empty roster. Collapsing the two would render an
		// unreachable repository as a player who owns nothing — a working
		// feature with no rows, which §8.10 forbids.
		errutil.LogErrorContext(ctx, "character access: owner roster lookup failed", err,
			"player_id", ps.PlayerID.String())
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	out := make([]*characteraccessv1.OwnCharacter, 0, len(chars))
	for _, char := range chars {
		if char == nil {
			continue
		}
		out = append(out, projectOwner(char, nil))
	}
	return &characteraccessv1.ListMyCharactersResponse{Characters: out}, nil
}

// GetMyCharacter returns one owned character in the shape the edit forms write
// back, including every stored `profile.*` row.
//
// NO TIER FLOOR IS EVALUATED. A floor governs what a VIEWER may read of someone
// else's character; it has nothing to say about what an owner sees of their
// own. The enumeration is driven with world.HumanCaller over the OWNING
// CHARACTER's subject, so the grid-side row-keyed policies decide — the same
// policies that have governed `look`-side property reads since Phase 2, rather
// than a second rule written for the web.
//
// A CHARACTER THE CALLER DOES NOT OWN AND ONE THAT DOES NOT EXIST TAKE THE SAME
// OUTCOME, because ownedCharacter resolves both against the caller's own list
// and returns one NotFound for either (INV-SCENE-63). A distinguishable
// "exists, but not yours" would let any caller enumerate which character ids
// are real.
func (s *CharacterAccessServer) GetMyCharacter(ctx context.Context, req *characteraccessv1.GetMyCharacterRequest) (*characteraccessv1.GetMyCharacterResponse, error) {
	ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())
	if err != nil {
		return nil, err
	}
	char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())
	if err != nil {
		return nil, err
	}

	profile, err := s.ownedProfileAttributes(ctx, char.ID)
	if err != nil {
		return nil, err
	}
	return &characteraccessv1.GetMyCharacterResponse{Character: projectOwner(char, profile)}, nil
}

// ownedProfileAttributes enumerates one owned character's stored `profile.*`
// rows as (name, value) pairs.
//
// THE SUBJECT IS THE CHARACTER'S OWN (access.CharacterSubject), never a viewer
// subject and never the system bypass. The system bypass would hand the facade
// every row unconditionally and empty the narrow-interface fence of meaning
// (D-79); a viewer subject would re-import the ALL/ANY asymmetry this audience
// exists to route around.
//
// A NULL value column is omitted here for the same reason projectOwner omits an
// empty string: a flag-style row carries no value to write back, and emitting
// it as present-and-empty would round-trip through the edit surface as a blank
// row the owner never authored.
func (s *CharacterAccessServer) ownedProfileAttributes(ctx context.Context, characterID ulid.ULID) (map[string]string, error) {
	caller := world.HumanCaller(access.CharacterSubject(characterID.String()))
	rows, err := s.world.ListPropertiesByParent(ctx, caller, characterParentType, characterID)
	if err != nil {
		errutil.LogErrorContext(ctx, "character access: owner profile property enumeration failed", err,
			"character_id", characterID.String())
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // gRPC status error at handler boundary
	}

	attrs := make(map[string]string, len(rows))
	for _, row := range rows {
		if row == nil || row.Value == nil {
			continue
		}
		// THE READ IS NAME-CLOSED, for the same reason the write is. What the
		// character's own subject may read is broader than this message's
		// contract — the row-keyed grid policies decide on the row, not on its
		// name — so an arbitrary property row on the character would otherwise
		// land verbatim in OwnCharacter.profile and in the edit form's model.
		// projectOwner may then keep holding only what it is contracted to
		// publish, exactly as projectPublic already does.
		if !isGovernedProfileName(row.Name) {
			continue
		}
		attrs[row.Name] = *row.Value
	}
	return attrs, nil
}
