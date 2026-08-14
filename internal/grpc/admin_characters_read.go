// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// The three admin CHARACTER READS, and the authorization argument behind them.
// Written for `abac-reviewer`, because one of these paths deliberately bypasses
// world.Service's per-row ABAC and the bound on that bypass is the thing worth
// checking.
//
// # These handlers evaluate NO policy, deliberately
//
// The gate is the admin-portal interceptor's `admin_section:characters` READ
// decision (D-99), made from the fail-closed section.AdminDescriptors table
// before any handler here runs. A handler that re-derived authority would be
// the pre-D-99 model: a check a future method can forget. This is the same
// gate-then-read shape characteraccess_directory.go already uses, and it is NOT
// the write path — the admin WRITE (plan 06-05) does evaluate
// world.Service.checkAccess on `character:<id>`.
//
// # What bounds the list projection
//
// 01-SPEC §11.3's field list. The list message carries no profile prose at all:
// it is a bulk cross-player projection, and prose in it would be a bulk export
// of player-authored text.
//
// # What bounds the DETAIL projection, and why it is not an ABAC decision
//
// Membership in updateCharacterProfileMaskablePaths
// (characteraccess_write.go:142) — the twelve §7.2 names, which are exactly the
// `profile.*` half of the thirteen paths §10.6 lets an admin WRITE. Read and
// write are therefore symmetric AGAINST ONE SHIPPED LIST rather than against
// two lists that happen to agree: an admin can read exactly the fields an admin
// can edit, and widening either means editing that one map, which is a reviewed
// change.
//
// The filter is NOT isGovernedProfileName. That helper
// (characteraccess_projection.go:238-243) admits TWENTY-THREE names — the
// twelve, plus profileImagePrimaryName, plus the ten gallery slots — which is
// correct for the OWNER read, whose message carries the image and gallery rows,
// and wrong here, because AdminCharacterDetail carries neither and because the
// whole argument above rests on the filter being EXACTLY §10.6's write set.
// Using it would make this paragraph false by eleven names, in the doc block
// written for the one gate that exists to check it.
//
// # The rejected alternative, named so a later reader does not "restore" it
//
// world.Service.ListPropertiesByParent evaluates a `property:<id>` / `read`
// ABAC decision PER ROW and, per its own doc comment (service.go:1774-1800),
// a deny means the "property [is] filtered out SILENTLY". Under the D-104
// player-flavoured admin caller EVERY row is denied: every shipped
// property-read permit is `principal is character` (seed.go:111-143),
// `principal is viewer` (seed.go:684-815) or `principal is plugin`
// (seed.go:394-396), the only player-principal policy in the corpus is
// seed:admin-section-access scoped `resource is admin_section`
// (seed.go:985-987), and 06-05's seed:admin-character-administration is scoped
// `resource is character`. So that path returns an EMPTY slice today and a
// silently-PARTIAL one under any future policy — and an edit form cannot
// distinguish a silently-partial read from an empty field, so it would
// overwrite existing content on save.
//
// Widening the policy corpus instead was considered and rejected: a
// `permit(principal is player, …, resource is property)` grant would let an
// admin player subject read EVERY property row in the world through any code
// path taking a player caller, far wider than the twelve fields the edit
// surface needs, and it would leave the silent-drop behaviour in place.

package grpc

import (
	"context"
	"errors"
	"strings"

	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// adminCharacterPageSizeMax is the server-enforced ceiling on rows per page,
// and adminCharacterPageSizeDefault is what an absent or zero page_size means.
//
// The clamp is what discharges T-06-24's "mandatory pagination with a
// server-enforced maximum page size". Without it a page_size of 2^31-1 reaches
// the repository as a LIMIT and the mitigation is a claim with nothing behind
// it. A value ABOVE the ceiling is clamped DOWN rather than refused: the
// operator asked for more rows, and answering with the most the server will
// give is more useful than an error, whereas a page BELOW 1 is refused because
// there is no charitable reading of it.
const (
	adminCharacterPageSizeMax     = 50
	adminCharacterPageSizeDefault = 50
)

// adminCharacterNotFoundMessage is the ONE message both "that id does not
// parse" and "that id names no row" carry.
//
// It names no id and carries no body, so a caller the gate already permitted
// cannot use this RPC to probe which ULIDs exist.
const adminCharacterNotFoundMessage = "character not found"

// adminCharacterParentType is the entity_properties.parent_type discriminator
// for a character's rows.
const adminCharacterParentType = "character"

// AdminListCharacters returns one page of the cross-player character list.
func (s *AdminPortalServer) AdminListCharacters(
	ctx context.Context,
	req *adminportalv1.AdminListCharactersRequest,
) (*adminportalv1.AdminListCharactersResponse, error) {
	opts, err := adminCharacterOptions(
		req.GetSortField(), req.GetDescending(), req.GetStatusFilter(),
		req.GetPlayerId(), req.GetPage(), req.GetPageSize(),
	)
	if err != nil {
		return nil, err
	}
	if s.characters == nil {
		return nil, adminCharacterReaderMissing(ctx, "character")
	}

	page, err := s.characters.AdminListCharacters(ctx, opts)
	if err != nil {
		return nil, mapAdminCharacterError(ctx, err, "operation", "admin_list_characters")
	}
	return &adminportalv1.AdminListCharactersResponse{
		Characters: adminCharacterMessages(page.Rows),
		TotalCount: page.TotalCount,
	}, nil
}

// AdminSearchCharacters returns one page filtered by a substring of the stored
// normalized name or of the joined player username.
func (s *AdminPortalServer) AdminSearchCharacters(
	ctx context.Context,
	req *adminportalv1.AdminSearchCharactersRequest,
) (*adminportalv1.AdminSearchCharactersResponse, error) {
	opts, err := adminCharacterOptions(
		req.GetSortField(), req.GetDescending(), req.GetStatusFilter(),
		req.GetPlayerId(), req.GetPage(), req.GetPageSize(),
	)
	if err != nil {
		return nil, err
	}
	if s.characters == nil {
		return nil, adminCharacterReaderMissing(ctx, "character")
	}

	term, emptyNormalForm, err := adminNormalizeSearchTerm(ctx, req.GetQuery())
	if err != nil {
		return nil, err
	}
	if emptyNormalForm {
		// The operator typed something that normalizes to nothing. "No matches"
		// is the truthful answer: it needs no error surface the list page does
		// not have, and it discloses nothing about which rows exist.
		return &adminportalv1.AdminSearchCharactersResponse{
			Characters: []*adminportalv1.AdminCharacter{},
			TotalCount: 0,
		}, nil
	}

	page, err := s.characters.AdminSearchCharacters(ctx, term, opts)
	if err != nil {
		return nil, mapAdminCharacterError(ctx, err, "operation", "admin_search_characters")
	}
	return &adminportalv1.AdminSearchCharactersResponse{
		Characters: adminCharacterMessages(page.Rows),
		TotalCount: page.TotalCount,
	}, nil
}

// AdminGetCharacter returns the one character the admin edit sheet reads when
// it opens, with the thirteen values §10.6 lets an admin write.
func (s *AdminPortalServer) AdminGetCharacter(
	ctx context.Context,
	req *adminportalv1.AdminGetCharacterRequest,
) (*adminportalv1.AdminGetCharacterResponse, error) {
	if s.characters == nil {
		return nil, adminCharacterReaderMissing(ctx, "character")
	}
	if s.profileReader == nil {
		// NEVER a detail response with twelve empty prose fields: that reads as
		// "this character has written nothing" and is what makes the edit sheet
		// overwrite on save.
		return nil, adminCharacterReaderMissing(ctx, "profile")
	}

	id, parseErr := ulid.Parse(req.GetCharacterId())
	if parseErr != nil {
		// An unparseable id and an absent row answer IDENTICALLY. Splitting them
		// would tell a caller which of their guesses was well-formed.
		return nil, status.Errorf(codes.NotFound, adminCharacterNotFoundMessage)
	}

	// AdminGetCharacterRow, not the bare Get. The detail message embeds the SAME
	// §11.3 projection the list carries, and player_username is one of its
	// fields — Get does not join players, so composing from it would leave that
	// field silently empty on the one read the edit sheet renders from.
	row, err := s.characters.AdminGetCharacterRow(ctx, id)
	if err != nil {
		if errors.Is(err, world.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, adminCharacterNotFoundMessage)
		}
		return nil, mapAdminCharacterError(ctx, err, "operation", "admin_get_character")
	}

	rows, err := s.profileReader.ListByParent(ctx, adminCharacterParentType, id)
	if err != nil {
		return nil, mapAdminCharacterError(ctx, err, "operation", "admin_get_character_profile")
	}

	return &adminportalv1.AdminGetCharacterResponse{
		Character: &adminportalv1.AdminCharacterDetail{
			Character:   adminCharacterMessage(row),
			Description: row.Description,
			Profile:     adminGovernedProfile(rows),
		},
	}, nil
}

// adminGovernedProfile projects property rows into the detail message's closed
// profile map.
//
// The loop is transposed from ownedProfileAttributes
// (characteraccess_owner.go:159-190) — nil-Value skip first, then the name
// filter. Its SUBJECT choice and its FILTER choice are deliberately not
// transposed; the file doc block above says why for each.
func adminGovernedProfile(rows []*world.EntityProperty) map[string]string {
	profile := make(map[string]string, len(updateCharacterProfileMaskablePaths))
	for _, row := range rows {
		if row == nil || row.Value == nil {
			// A flag-style row carries no value to write back. Emitting it as
			// present-and-empty would round-trip through the edit surface as a
			// blank the operator never authored.
			continue
		}
		// THE NAME FILTER. A raw ListByParent returns EVERY property row on the
		// character — including anything a plugin, the media pipeline or a
		// future subsystem wrote — so without this the detail message would
		// export arbitrary rows to the admin wire alongside the twelve.
		if _, governed := updateCharacterProfileMaskablePaths[row.Name]; !governed {
			continue
		}
		profile[row.Name] = *row.Value
	}
	return profile
}

// adminCharacterOptions validates and clamps the page request, and maps both
// closed enums through denying switches.
func adminCharacterOptions(
	sortField adminportalv1.AdminCharacterSortField,
	descending bool,
	statusFilter adminportalv1.AdminCharacterStatusFilter,
	playerID string,
	page, pageSize int32,
) (world.AdminCharacterListOptions, error) {
	sort, err := adminSortField(sortField)
	if err != nil {
		return world.AdminCharacterListOptions{}, err
	}
	lifecycle, err := adminStatusFilter(statusFilter)
	if err != nil {
		return world.AdminCharacterListOptions{}, err
	}

	if page < 1 {
		// There is no charitable reading of page 0 or a negative page, and
		// computing an OFFSET from one would silently return the first page.
		return world.AdminCharacterListOptions{}, invalidArgument("page must be 1 or greater")
	}

	size := pageSize
	if size <= 0 {
		size = adminCharacterPageSizeDefault
	}
	if size > adminCharacterPageSizeMax {
		size = adminCharacterPageSizeMax
	}

	opts := world.AdminCharacterListOptions{
		SortField:    sort,
		Descending:   descending,
		StatusFilter: lifecycle,
		Limit:        int(size),
		Offset:       int(page-1) * int(size),
	}

	if playerID != "" {
		parsed, parseErr := ulid.Parse(playerID)
		if parseErr != nil {
			return world.AdminCharacterListOptions{}, invalidArgument("player_id is not a valid identifier")
		}
		opts.PlayerID = &parsed
	}
	return opts, nil
}

// adminSortField maps the proto enum to the repository's closed vocabulary.
//
// The default arm DENIES, so a value added to the proto without an arm here is
// refused rather than silently ordered by whatever the repository defaults to.
// UNSPECIFIED is refused for the same reason: a silently-defaulted ordering is
// indistinguishable from an honoured one at the call site.
func adminSortField(f adminportalv1.AdminCharacterSortField) (world.AdminCharacterSortField, error) {
	switch f {
	case adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_NAME:
		return world.AdminSortName, nil
	case adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_CREATED_AT:
		return world.AdminSortCreatedAt, nil
	case adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_STATUS:
		return world.AdminSortStatus, nil
	case adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_LAST_ACTIVE_AT:
		return world.AdminSortLastActiveAt, nil
	case adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_PLAYER_USERNAME:
		return world.AdminSortPlayerUsername, nil
	case adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_UNSPECIFIED:
		return "", invalidArgument("sort_field must be set")
	default:
		return "", invalidArgument("sort_field is not a supported ordering")
	}
}

// adminStatusFilter maps the proto enum to an optional world.Status.
//
// UNSPECIFIED is the one enum zero value on this surface that is NOT a
// rejection: it means "no filter", because the unfiltered list has to be
// requestable.
func adminStatusFilter(f adminportalv1.AdminCharacterStatusFilter) (*world.Status, error) {
	var s world.Status
	switch f {
	case adminportalv1.AdminCharacterStatusFilter_ADMIN_CHARACTER_STATUS_FILTER_UNSPECIFIED:
		return nil, nil //nolint:nilnil // a nil filter with no error IS the "no filter" answer
	case adminportalv1.AdminCharacterStatusFilter_ADMIN_CHARACTER_STATUS_FILTER_ACTIVE:
		s = world.StatusActive
	case adminportalv1.AdminCharacterStatusFilter_ADMIN_CHARACTER_STATUS_FILTER_IDLE:
		s = world.StatusIdle
	case adminportalv1.AdminCharacterStatusFilter_ADMIN_CHARACTER_STATUS_FILTER_RETIRED:
		s = world.StatusRetired
	default:
		return nil, invalidArgument("status_filter is not a supported lifecycle value")
	}
	return &s, nil
}

// adminNormalizeSearchTerm applies the two mandatory exceptions to normalizing
// the operator's raw query.
//
// charname.Normalize REJECTS input with no normal form
// (NAME_EMPTY_NORMAL_FORM, pipeline.go:118-130), so a handler that normalized
// FIRST could not implement the no-minimum-length contract at all:
//
//   - Blank after TrimSpace bypasses normalization AND the predicate entirely,
//     returning the unfiltered page. This is the branch a normalize-first
//     handler cannot reach.
//   - Non-blank but with an empty normal form (a lone zero-width joiner, say)
//     yields an empty page rather than an error.
//
// The returned bool is the second case. Both branches are asserted at the WIRE,
// because the repository receives the term already normalized and so never
// exercises the path where the bug lives.
func adminNormalizeSearchTerm(ctx context.Context, raw string) (term string, emptyNormalForm bool, err error) {
	if strings.TrimSpace(raw) == "" {
		return "", false, nil
	}
	normalized, normErr := charname.Normalize(raw)
	if normErr != nil {
		if adminSectionCode(normErr) == "NAME_EMPTY_NORMAL_FORM" {
			return "", true, nil
		}
		errutil.LogErrorContext(ctx, "admin characters: search term normalization failed", normErr)
		return "", false, invalidArgument("query could not be interpreted")
	}
	return normalized.Key, false, nil
}

// adminCharacterMessages projects a page of rows.
func adminCharacterMessages(rows []world.AdminCharacterRow) []*adminportalv1.AdminCharacter {
	out := make([]*adminportalv1.AdminCharacter, 0, len(rows))
	for _, row := range rows {
		out = append(out, adminCharacterMessage(row))
	}
	return out
}

// adminCharacterMessage projects ONE row into §11.3's field list.
func adminCharacterMessage(row world.AdminCharacterRow) *adminportalv1.AdminCharacter {
	return &adminportalv1.AdminCharacter{
		Id:             row.ID.String(),
		PlayerId:       row.PlayerID.String(),
		PlayerUsername: row.PlayerUsername,
		Name:           row.Name,
		Status:         string(row.Status),
		LastActiveAt:   row.LastActiveAt,
		CreatedAt:      row.CreatedAt.UnixNano(),
		Version:        int32(row.Version), //nolint:gosec // characters.version is a monotonic row counter, not attacker-sized
	}
}

// invalidArgument is the one shape a caller-fault refusal takes here: a static
// message with no format verb, so nothing about the request is echoed back.
func invalidArgument(msg string) error {
	return status.Error(codes.InvalidArgument, msg) //nolint:wrapcheck // gRPC status error at the handler boundary
}

// adminCharacterReaderMissing refuses a call this server was not wired to
// answer.
//
// It is codes.Internal and NEVER an empty page: an empty page would render as
// "no characters exist", a factual claim a server missing its repository is in
// no position to make.
func adminCharacterReaderMissing(ctx context.Context, which string) error {
	errutil.LogErrorContext(ctx, "admin characters: reader not configured", nil, "reader", which)
	return status.Errorf(codes.Internal, "internal error")
}

// mapAdminCharacterError translates a repository error at THIS one layer.
//
// Translating at exactly one layer is not tidiness: a double translation breaks
// status.FromError chain-walking, because the inner conversion produces a fresh
// error carrying no GRPCStatus method. No inner error is ever formatted into a
// status message — the id and the driver error go to the log only.
func mapAdminCharacterError(ctx context.Context, err error, logAttrs ...any) error {
	if adminSectionCode(err) == "CHARACTER_ADMIN_SORT_FIELD_UNSUPPORTED" {
		return invalidArgument("sort_field is not a supported ordering")
	}
	errutil.LogErrorContext(ctx, "admin characters: read failed", err, logAttrs...)
	return status.Errorf(codes.Internal, "internal error")
}
