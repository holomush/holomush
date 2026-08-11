// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"github.com/holomush/holomush/internal/world"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// profileImagePrimaryName is the single primary-image row name (01-SPEC §7.3).
// The database's UNIQUE(parent_type, parent_id, name) constraint is what makes
// "exactly one primary" true; no application-side check is added beside it.
const profileImagePrimaryName = "profile.image.primary"

// profileGallerySlotNames is the ten gallery slot names, IN THE ORDER THEY ARE
// EMITTED. The slice is the ordering mechanism: iterating it — rather than
// ranging a map and sorting afterwards — is what keeps Go's map-iteration order
// out of a repeated field whose element order is carried on the wire.
//
// The index is two-digit zero-padded, and the names are compared as EXACT BYTES
// (§7.3). `profile.image.gallery.0` and `profile.image.gallery.00` are two
// different rows that coexist happily in storage, and there is no normalization
// step anywhere in the read path that would collapse them — so a single-digit
// or an eleventh slot name is not a gallery entry here, exactly as it is not a
// member of any §8.6 tier-floor list.
var profileGallerySlotNames = [...]string{
	"profile.image.gallery.00",
	"profile.image.gallery.01",
	"profile.image.gallery.02",
	"profile.image.gallery.03",
	"profile.image.gallery.04",
	"profile.image.gallery.05",
	"profile.image.gallery.06",
	"profile.image.gallery.07",
	"profile.image.gallery.08",
	"profile.image.gallery.09",
}

// projectPublic is the SOLE constructor of a PublicCharacter (01-SPEC §2.3). No
// handler may assemble a character-shaped message by struct literal, because the
// failure mode is not "a handler redacts wrongly" but "a handler that builds its
// own message never learns that redaction is a thing that happens" — which is
// exactly how a list surface drifts away from the detail surface it was supposed
// to match.
//
// ABSENCE, NEVER EMPTINESS (§8.9, §7.5). A value this viewer may not see and a
// value the character left blank MUST look identical on the wire, so an empty
// attribute is dropped from the map rather than emitted as a present-and-empty
// string; if they differed, the response shape itself would disclose which
// fields exist but are withheld. `name` and `description` are the two the
// character row always carries and they are set unconditionally: reachability is
// name's only gate (§8.8), and it has already been evaluated by the time this
// function runs.
//
// ITS INPUT IS THE ADMITTED PAIRS, NEVER A ROW SLICE. `profile` carries only
// the (name, value) pairs the §8.5.1 conjunction admitted, so this function has
// no reachable value the conjunction did not permit — it cannot leak by
// forgetting a filter, because it holds nothing to filter.
//
// It emits an image only when a media row was admitted. In practice v0.13 emits
// none: nothing in v0.13 mints a media identifier (§9.7), so no
// `profile.image.*` row exists to admit. The routing ships anyway because the
// schema is what ships early — the shape must not change when upload arrives.
//
// THE OUTPUT IS DETERMINISTIC IN THE ONLY SENSE THIS FUNCTION CAN OWN: the same
// admitted pairs produce the same logical message, with no dependence on Go's
// map-iteration order. Gallery order comes from iterating
// profileGallerySlotNames; the profile map is a proto map, which is an unordered
// set by definition and whose BYTE ordering is the marshaler's business (and is
// only promised under proto.MarshalOptions{Deterministic: true}).
func projectPublic(characterID string, desc world.CharacterDescription, profile map[string]string) *characteraccessv1.PublicCharacter {
	out := &characteraccessv1.PublicCharacter{
		Id:          characterID,
		Name:        desc.Name,
		Description: desc.Description,
	}

	for name, value := range profile {
		if value == "" {
			continue
		}
		// Media rows are routed to their own fields rather than duplicated into
		// the text map: §7.2's twelve prose names and §7.3's eleven media names
		// are disjoint sets, and a media reference in the text map would be
		// rendered as prose by any client walking the map.
		if name == profileImagePrimaryName || isProfileGallerySlotName(name) {
			continue
		}
		if out.Profile == nil {
			out.Profile = make(map[string]string, len(profile))
		}
		out.Profile[name] = value
	}

	if mediaID := profile[profileImagePrimaryName]; mediaID != "" {
		out.PrimaryImage = &characteraccessv1.ProfileImage{MediaId: mediaID}
	}

	for _, slot := range profileGallerySlotNames {
		mediaID := profile[slot]
		if mediaID == "" {
			continue
		}
		out.Gallery = append(out.Gallery, &characteraccessv1.ProfileImage{MediaId: mediaID})
	}

	return out
}

// projectOwner is the SOLE constructor of an OwnCharacter (01-SPEC §2.3), the
// owner-audience counterpart of projectPublic. The two are never interchanged:
// OwnCharacter is a DISTINCT message rather than PublicCharacter plus fields,
// so an owner-only value has no field to land in on the public side and the
// absence is checked by the compiler rather than by a handler remembering to
// clear something.
//
// IT EVALUATES NO TIER FLOOR AND TAKES NO VIEWER SUBJECT. Its `profile`
// argument is every row the owning character's own subject could read, and the
// omission of any filtering here is deliberate: a floor governs what a VIEWER
// may see of someone else's character (§8.6), never what an owner sees of their
// own. The audience split is what makes that safe — see
// characteraccess_owner.go.
//
// It carries the two fields PublicCharacter structurally cannot hold: `status`,
// the closed three-value lifecycle vocabulary (§4.2), and `version`, the
// optimistic-concurrency token the client sends back as expected_version on its
// next mutation.
//
// ABSENCE, NEVER EMPTINESS, exactly as projectPublic (§7.5). An empty-valued
// row is dropped rather than emitted present-and-empty — not to hide it from
// the owner, who authored it, but because the edit surface reads these values
// to write them back, and a present-and-empty key round-trips as a blank row
// nobody authored. `status` is omitted when the row carries none, which a
// full-entity read never produces: the id/name projections that leave Status
// zero (world.Character's own doc comment names them) must not reach here.
//
// A nil `profile` is the ROSTER shape, not a missing profile: ListMyCharacters
// deliberately does not enumerate property rows per character. See its doc.
func projectOwner(char *world.Character, profile map[string]string) *characteraccessv1.OwnCharacter {
	out := &characteraccessv1.OwnCharacter{
		Id:          char.ID.String(),
		Name:        char.Name,
		Description: char.Description,
		// characters.version is a Postgres INTEGER (migration 000049), so the
		// stored value cannot exceed int32 range.
		Version: int32(char.Version), //nolint:gosec // characters.version is a 32-bit INTEGER column
	}
	if char.Status != "" {
		out.Status = string(char.Status)
	}

	for name, value := range profile {
		if value == "" {
			continue
		}
		// Media rows route to their own fields rather than being duplicated
		// into the text map — §7.2's twelve prose names and §7.3's eleven media
		// names are disjoint sets, and a media handle in the text map would be
		// rendered as prose by any client walking it.
		if name == profileImagePrimaryName || isProfileGallerySlotName(name) {
			continue
		}
		if out.Profile == nil {
			out.Profile = make(map[string]string, len(profile))
		}
		out.Profile[name] = value
	}

	if mediaID := profile[profileImagePrimaryName]; mediaID != "" {
		out.PrimaryImage = &characteraccessv1.ProfileImage{MediaId: mediaID}
	}

	// The slice is the ordering mechanism, as in projectPublic: iterating it
	// keeps Go's map-iteration order out of a repeated field whose element
	// order is carried on the wire.
	for _, slot := range profileGallerySlotNames {
		mediaID := profile[slot]
		if mediaID == "" {
			continue
		}
		out.Gallery = append(out.Gallery, &characteraccessv1.ProfileImage{MediaId: mediaID})
	}

	return out
}

// isProfileGallerySlotName reports whether name is one of the ten gallery slot
// names, compared as an exact whole string.
func isProfileGallerySlotName(name string) bool {
	for _, slot := range profileGallerySlotNames {
		if name == slot {
			return true
		}
	}
	return false
}
