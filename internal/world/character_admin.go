// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import "github.com/oklog/ulid/v2"

// AdminCharacterSortField is the CLOSED ordering vocabulary of the admin
// character list — 01-SPEC §11.3's five rows marked Sort=Yes and nothing else.
//
// It is a typed string rather than a bare one so the repository's ORDER BY can
// be emitted from a closed switch: the column name is chosen by the switch, not
// interpolated from caller text, which is what keeps T-06-21 (ORDER BY
// injection) closed by construction rather than by escaping.
//
// characters.player_id is DELIBERATELY ABSENT. §11.3 marks it Sort=No /
// Filter=Yes (`01-SPEC.md:2734`) and §14 row 12 re-asserts that an ordering on
// it would leak creation sequence (`:3168`). It stays reachable as
// [AdminCharacterListOptions.PlayerID], the equality filter the click-to-filter
// path uses — filterability and sortability are different §11.3 columns and
// MUST NOT be collapsed.
type AdminCharacterSortField string

const (
	// AdminSortName orders on characters.normalized_name, NOT characters.name.
	// §11.3's name row orders on the stored normal form of §6.1.3, and the two
	// orderings differ observably under case and NFKC folding.
	AdminSortName AdminCharacterSortField = "name"
	// AdminSortCreatedAt orders on characters.created_at.
	AdminSortCreatedAt AdminCharacterSortField = "created_at"
	// AdminSortStatus orders on characters.status.
	AdminSortStatus AdminCharacterSortField = "status"
	// AdminSortLastActiveAt orders on characters.last_active_at, with the 0
	// "never active" sentinel forced LAST in BOTH directions.
	AdminSortLastActiveAt AdminCharacterSortField = "last_active_at"
	// AdminSortPlayerUsername orders on the joined players.username.
	AdminSortPlayerUsername AdminCharacterSortField = "player_username"
)

// AdminCharacterListOptions is the bounded request shape for both admin reads.
//
// Every field that reaches SQL is either a bound parameter or a closed enum;
// nothing here is interpolated as text.
type AdminCharacterListOptions struct {
	// SortField selects the ordering column. The zero value is NOT a default:
	// the repository refuses it with CHARACTER_ADMIN_SORT_FIELD_UNSUPPORTED
	// rather than silently picking an order, because a silently-defaulted
	// ordering is indistinguishable from an honoured one at the call site.
	SortField AdminCharacterSortField
	// Descending reverses the primary sort key only. The trailing
	// normalized_name tiebreak stays ascending in both directions so the order
	// is TOTAL, and the never-active sentinel stays last in both directions.
	Descending bool
	// StatusFilter, when non-nil, restricts to one lifecycle value.
	StatusFilter *Status
	// PlayerID, when non-nil, restricts to one player's characters. This is
	// §11.3's Filter=Yes half of `player_id`; there is no ordering on it.
	PlayerID *ulid.ULID
	// Limit is the page size. It is server-clamped ABOVE this layer; the
	// repository binds whatever it is given.
	Limit int
	// Offset is the zero-based row offset of the requested page.
	Offset int
}

// AdminCharacterRow is one row of the admin list: the FULL character projection
// plus the joined player username.
//
// It embeds *Character rather than re-listing its fields, so the projection is
// defined by reference to the full-entity read and cannot drift into
// ListAll's id+name-only shape — whose zero-valued Status would make any
// lifecycle decision downstream violate INV-WORLD-5.
type AdminCharacterRow struct {
	*Character
	// PlayerUsername is players.username for Character.PlayerID. It is an OOC
	// identity column the admin audience already sees, not profile content.
	PlayerUsername string
}

// AdminCharacterPage is one page plus the total over the SAME filtered set.
//
// TotalCount is computed by its own scalar count in the same read transaction
// as the page, NOT by a window column: once OFFSET has removed every row there
// is no row left to carry a window value, so a page beyond the end would report
// 0 instead of the true total.
type AdminCharacterPage struct {
	Rows       []AdminCharacterRow
	TotalCount int64
}
