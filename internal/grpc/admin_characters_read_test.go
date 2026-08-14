// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/world"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// fakeAdminCharacterReader records what the handler asked for and replays a
// canned page. Recording the REQUEST is the point: the assertions that matter
// here are about what the handler computed before the repository was reached.
type fakeAdminCharacterReader struct {
	gotTerm  string
	gotOpts  world.AdminCharacterListOptions
	gotID    ulid.ULID
	page     world.AdminCharacterPage
	row      world.AdminCharacterRow
	err      error
	rowErr   error
	getCalls int
}

func (f *fakeAdminCharacterReader) AdminListCharacters(_ context.Context, opts world.AdminCharacterListOptions) (world.AdminCharacterPage, error) {
	f.gotOpts = opts
	return f.page, f.err
}

func (f *fakeAdminCharacterReader) AdminSearchCharacters(_ context.Context, term string, opts world.AdminCharacterListOptions) (world.AdminCharacterPage, error) {
	f.gotTerm = term
	f.gotOpts = opts
	return f.page, f.err
}

func (f *fakeAdminCharacterReader) AdminGetCharacterRow(_ context.Context, id ulid.ULID) (world.AdminCharacterRow, error) {
	f.getCalls++
	f.gotID = id
	return f.row, f.rowErr
}

// fakeAdminProfileReader replays raw property rows — EVERY row on the
// character, exactly as world.PropertyRepository.ListByParent would, so the
// handler's name filter is the only thing between them and the wire.
type fakeAdminProfileReader struct {
	rows []*world.EntityProperty
	err  error
}

func (f *fakeAdminProfileReader) ListByParent(_ context.Context, _ string, _ ulid.ULID) ([]*world.EntityProperty, error) {
	return f.rows, f.err
}

func strPtr(s string) *string { return &s }

func newAdminCharServer(t *testing.T, chars *fakeAdminCharacterReader, profile *fakeAdminProfileReader) *AdminPortalServer {
	t.Helper()
	opts := []AdminPortalServerOption{}
	if chars != nil {
		opts = append(opts, WithAdminCharacterReader(chars))
	}
	if profile != nil {
		opts = append(opts, WithAdminProfileReader(profile))
	}
	// The engine is immaterial here: these three handlers evaluate NO policy —
	// the gate is the interceptor's, upstream. It is supplied only because
	// NewAdminPortalServer refuses a nil one. The wire-level denial and its
	// paired positive control live in test/integration/access, where the real
	// interceptor runs in front of the real handler.
	return NewAdminPortalServer(seedEngineFor(t, ulid.Make()), opts...)
}

func listReq() *adminportalv1.AdminListCharactersRequest {
	return &adminportalv1.AdminListCharactersRequest{
		PlayerSessionToken: "tok",
		SortField:          adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_NAME,
		Page:               1,
	}
}

func searchReq(query string) *adminportalv1.AdminSearchCharactersRequest {
	return &adminportalv1.AdminSearchCharactersRequest{
		PlayerSessionToken: "tok",
		SortField:          adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_NAME,
		Page:               1,
		Query:              query,
	}
}

func statusCodeOf(t *testing.T, err error) codes.Code {
	t.Helper()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "the handler must return a gRPC status error")
	return st.Code()
}

func TestAdminListCharactersProjectsThePageAndItsTotal(t *testing.T) {
	id, playerID := ulid.Make(), ulid.Make()
	created := time.Unix(0, 1700000000000000000).UTC()
	chars := &fakeAdminCharacterReader{page: world.AdminCharacterPage{
		Rows: []world.AdminCharacterRow{{
			Character: &world.Character{
				ID: id, PlayerID: playerID, Name: "Ada", Description: "prose",
				CreatedAt: created, Version: 7, Status: world.StatusActive, LastActiveAt: 99,
			},
			PlayerUsername: "ada_player",
		}},
		TotalCount: 41,
	}}
	srv := newAdminCharServer(t, chars, nil)

	resp, err := srv.AdminListCharacters(context.Background(), listReq())
	require.NoError(t, err)
	require.Len(t, resp.GetCharacters(), 1)

	got := resp.GetCharacters()[0]
	assert.Equal(t, id.String(), got.GetId())
	assert.Equal(t, playerID.String(), got.GetPlayerId())
	assert.Equal(t, "ada_player", got.GetPlayerUsername())
	assert.Equal(t, "Ada", got.GetName())
	assert.Equal(t, "active", got.GetStatus())
	assert.Equal(t, int64(99), got.GetLastActiveAt())
	assert.Equal(t, created.UnixNano(), got.GetCreatedAt())
	assert.Equal(t, int32(7), got.GetVersion())
	assert.Equal(t, int64(41), resp.GetTotalCount())
}

// TestAdminSearchCharactersNormalizesTheTermServerSide uses a term whose
// normalized form DIFFERS from its raw form, so a handler that forwarded the
// raw string fails.
func TestAdminSearchCharactersNormalizesTheTermServerSide(t *testing.T) {
	chars := &fakeAdminCharacterReader{}
	srv := newAdminCharServer(t, chars, nil)

	_, err := srv.AdminSearchCharacters(context.Background(), searchReq("ＫＡＦＫＡ"))
	require.NoError(t, err)
	assert.Equal(t, "kafka", chars.gotTerm,
		"the raw typed string is normalized through the shared charname pipeline, not forwarded")
}

// TestAdminSearchCharactersHandlesTheBlankAndEmptyNormalFormTerms is the wire
// half of the empty-term contract — the branch a normalize-first handler cannot
// reach, because charname.Normalize REJECTS blank input.
func TestAdminSearchCharactersHandlesTheBlankAndEmptyNormalFormTerms(t *testing.T) {
	t.Run("an empty query bypasses the predicate and returns the unfiltered page", func(t *testing.T) {
		chars := &fakeAdminCharacterReader{page: world.AdminCharacterPage{TotalCount: 3}}
		srv := newAdminCharServer(t, chars, nil)

		resp, err := srv.AdminSearchCharacters(context.Background(), searchReq(""))
		require.NoError(t, err)
		assert.Equal(t, "", chars.gotTerm, "an empty term reaches the repository as the no-filter sentinel")
		assert.Equal(t, int64(3), resp.GetTotalCount(), "the UNFILTERED page is returned")
	})

	t.Run("a whitespace-only query does the same", func(t *testing.T) {
		chars := &fakeAdminCharacterReader{page: world.AdminCharacterPage{TotalCount: 3}}
		srv := newAdminCharServer(t, chars, nil)

		resp, err := srv.AdminSearchCharacters(context.Background(), searchReq("   "))
		require.NoError(t, err)
		assert.Equal(t, "", chars.gotTerm)
		assert.Equal(t, int64(3), resp.GetTotalCount())
	})

	t.Run("a query whose normal form is empty returns an empty page, not an error", func(t *testing.T) {
		chars := &fakeAdminCharacterReader{page: world.AdminCharacterPage{TotalCount: 3}}
		srv := newAdminCharServer(t, chars, nil)

		// U+200D ZERO WIDTH JOINER is category Cf: non-blank, but nothing
		// survives normalization.
		resp, err := srv.AdminSearchCharacters(context.Background(), searchReq("\u200d"))
		require.NoError(t, err, "no error surface the list page does not have")
		assert.Empty(t, resp.GetCharacters())
		assert.Equal(t, int64(0), resp.GetTotalCount())
		assert.Equal(t, 0, len(chars.gotTerm), "the repository is not consulted at all")
	})
}

func TestAdminGetCharacterRefusesAnUnknownIDWithoutNamingIt(t *testing.T) {
	t.Run("an id that names no row", func(t *testing.T) {
		chars := &fakeAdminCharacterReader{
			rowErr: oops.Code("CHARACTER_NOT_FOUND").Wrap(world.ErrNotFound),
		}
		srv := newAdminCharServer(t, chars, &fakeAdminProfileReader{})

		id := ulid.Make()
		_, err := srv.AdminGetCharacter(context.Background(), &adminportalv1.AdminGetCharacterRequest{
			PlayerSessionToken: "tok", CharacterId: id.String(),
		})
		require.Equal(t, codes.NotFound, statusCodeOf(t, err))
		st, _ := status.FromError(err)
		assert.Equal(t, adminCharacterNotFoundMessage, st.Message())
		assert.NotContains(t, st.Message(), id.String(), "the message carries no id")
	})

	t.Run("an id that does not parse answers identically", func(t *testing.T) {
		chars := &fakeAdminCharacterReader{}
		srv := newAdminCharServer(t, chars, &fakeAdminProfileReader{})

		_, err := srv.AdminGetCharacter(context.Background(), &adminportalv1.AdminGetCharacterRequest{
			PlayerSessionToken: "tok", CharacterId: "not-a-ulid",
		})
		require.Equal(t, codes.NotFound, statusCodeOf(t, err))
		st, _ := status.FromError(err)
		assert.Equal(t, adminCharacterNotFoundMessage, st.Message(),
			"an unparseable id and an absent row are indistinguishable, so this is not an existence oracle")
		assert.Equal(t, 0, chars.getCalls, "an unparseable id never reaches the repository")
	})
}

func TestAdminCharacterSortFieldOutsideTheClosedSetIsRejected(t *testing.T) {
	t.Run("the unspecified value is refused rather than defaulted", func(t *testing.T) {
		chars := &fakeAdminCharacterReader{}
		srv := newAdminCharServer(t, chars, nil)

		req := listReq()
		req.SortField = adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_UNSPECIFIED
		_, err := srv.AdminListCharacters(context.Background(), req)
		assert.Equal(t, codes.InvalidArgument, statusCodeOf(t, err))
		assert.Equal(t, world.AdminCharacterSortField(""), chars.gotOpts.SortField,
			"the repository is never reached, so no ordering was silently chosen")
	})

	t.Run("a value with no handler arm is refused", func(t *testing.T) {
		chars := &fakeAdminCharacterReader{}
		srv := newAdminCharServer(t, chars, nil)

		req := listReq()
		req.SortField = adminportalv1.AdminCharacterSortField(9999)
		_, err := srv.AdminListCharacters(context.Background(), req)
		assert.Equal(t, codes.InvalidArgument, statusCodeOf(t, err))
	})
}

// TestAdminCharacterSortFieldEnumCarriesExactlyTheFiveSortableFields asserts
// SET EQUALITY in BOTH directions.
//
// player_id and version are unsortable STRUCTURALLY: no enum value exists to
// express them, so a client cannot make the request at all. Adding
// ADMIN_CHARACTER_SORT_FIELD_PLAYER_ID to the proto and regenerating fails the
// EXTRA direction here.
func TestAdminCharacterSortFieldEnumCarriesExactlyTheFiveSortableFields(t *testing.T) {
	want := map[string]struct{}{
		"ADMIN_CHARACTER_SORT_FIELD_UNSPECIFIED":     {},
		"ADMIN_CHARACTER_SORT_FIELD_NAME":            {},
		"ADMIN_CHARACTER_SORT_FIELD_CREATED_AT":      {},
		"ADMIN_CHARACTER_SORT_FIELD_STATUS":          {},
		"ADMIN_CHARACTER_SORT_FIELD_LAST_ACTIVE_AT":  {},
		"ADMIN_CHARACTER_SORT_FIELD_PLAYER_USERNAME": {},
	}
	got := map[string]struct{}{}
	for _, name := range adminportalv1.AdminCharacterSortField_name {
		got[name] = struct{}{}
	}
	require.NotEmpty(t, got, "an empty enum would satisfy one direction vacuously")

	for name := range want {
		assert.Contains(t, got, name, "MISSING from the shipped enum")
	}
	for name := range got {
		assert.Contains(t, want, name, "EXTRA value on the shipped enum — §11.3 permits five sortable fields")
	}
}

// TestAdminCharacterStatusFilterEnumCarriesExactlyTheThreeLifecycleValues pins
// the filter vocabulary the way the sort vocabulary is pinned. A free-text
// filter would re-introduce §9.3's lifecycle words on the wire as a string and
// would answer a typo with a silent zero-row page.
func TestAdminCharacterStatusFilterEnumCarriesExactlyTheThreeLifecycleValues(t *testing.T) {
	want := map[string]struct{}{
		"ADMIN_CHARACTER_STATUS_FILTER_UNSPECIFIED": {},
		"ADMIN_CHARACTER_STATUS_FILTER_ACTIVE":      {},
		"ADMIN_CHARACTER_STATUS_FILTER_IDLE":        {},
		"ADMIN_CHARACTER_STATUS_FILTER_RETIRED":     {},
	}
	got := map[string]struct{}{}
	for _, name := range adminportalv1.AdminCharacterStatusFilter_name {
		got[name] = struct{}{}
	}
	require.NotEmpty(t, got)

	for name := range want {
		assert.Contains(t, got, name, "MISSING")
	}
	for name := range got {
		assert.Contains(t, want, name, "EXTRA")
	}

	t.Run("unspecified means no filter, not a rejection", func(t *testing.T) {
		chars := &fakeAdminCharacterReader{}
		srv := newAdminCharServer(t, chars, nil)
		_, err := srv.AdminListCharacters(context.Background(), listReq())
		require.NoError(t, err)
		assert.Nil(t, chars.gotOpts.StatusFilter)
	})

	t.Run("each lifecycle value maps to its world.Status counterpart", func(t *testing.T) {
		for value, want := range map[adminportalv1.AdminCharacterStatusFilter]world.Status{
			adminportalv1.AdminCharacterStatusFilter_ADMIN_CHARACTER_STATUS_FILTER_ACTIVE:  world.StatusActive,
			adminportalv1.AdminCharacterStatusFilter_ADMIN_CHARACTER_STATUS_FILTER_IDLE:    world.StatusIdle,
			adminportalv1.AdminCharacterStatusFilter_ADMIN_CHARACTER_STATUS_FILTER_RETIRED: world.StatusRetired,
		} {
			chars := &fakeAdminCharacterReader{}
			srv := newAdminCharServer(t, chars, nil)
			req := listReq()
			req.StatusFilter = value
			_, err := srv.AdminListCharacters(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, chars.gotOpts.StatusFilter)
			assert.Equal(t, want, *chars.gotOpts.StatusFilter)
		}
	})

	t.Run("a value with no handler arm is refused", func(t *testing.T) {
		chars := &fakeAdminCharacterReader{}
		srv := newAdminCharServer(t, chars, nil)
		req := listReq()
		req.StatusFilter = adminportalv1.AdminCharacterStatusFilter(9999)
		_, err := srv.AdminListCharacters(context.Background(), req)
		assert.Equal(t, codes.InvalidArgument, statusCodeOf(t, err))
	})
}

// TestAdminListCharactersFiltersByPlayerIDWithoutOrderingOnIt keeps §11.3's two
// columns from being collapsed: player_id is Filter=Yes and Sort=No, and this
// pairs with the sort-enum set equality above.
func TestAdminListCharactersFiltersByPlayerIDWithoutOrderingOnIt(t *testing.T) {
	playerID := ulid.Make()
	chars := &fakeAdminCharacterReader{}
	srv := newAdminCharServer(t, chars, nil)

	req := listReq()
	req.PlayerId = playerID.String()
	_, err := srv.AdminListCharacters(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, chars.gotOpts.PlayerID)
	assert.Equal(t, playerID, *chars.gotOpts.PlayerID)

	t.Run("an unparseable player_id is refused", func(t *testing.T) {
		req := listReq()
		req.PlayerId = "nope"
		_, err := srv.AdminListCharacters(context.Background(), req)
		assert.Equal(t, codes.InvalidArgument, statusCodeOf(t, err))
	})
}

// TestAdminListCharactersClampsThePageSizeAndRefusesAPageBelowOne is what
// discharges T-06-24. Without it a page_size of 2^31-1 reaches the repository
// as a LIMIT and the mitigation has nothing behind it.
func TestAdminListCharactersClampsThePageSizeAndRefusesAPageBelowOne(t *testing.T) {
	tests := []struct {
		name       string
		page       int32
		pageSize   int32
		wantLimit  int
		wantOffset int
		wantCode   codes.Code
	}{
		{name: "an absent page size defaults to fifty", page: 1, pageSize: 0, wantLimit: 50, wantOffset: 0},
		{name: "a page size above the ceiling is clamped DOWN, not honoured", page: 1, pageSize: 2147483647, wantLimit: 50, wantOffset: 0},
		{name: "a page size below the ceiling is honoured", page: 3, pageSize: 10, wantLimit: 10, wantOffset: 20},
		{name: "page zero is refused", page: 0, pageSize: 10, wantCode: codes.InvalidArgument},
		{name: "a negative page is refused", page: -1, pageSize: 10, wantCode: codes.InvalidArgument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chars := &fakeAdminCharacterReader{}
			srv := newAdminCharServer(t, chars, nil)
			req := listReq()
			req.Page, req.PageSize = tt.page, tt.pageSize

			_, err := srv.AdminListCharacters(context.Background(), req)
			if tt.wantCode != codes.OK {
				assert.Equal(t, tt.wantCode, statusCodeOf(t, err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLimit, chars.gotOpts.Limit)
			assert.Equal(t, tt.wantOffset, chars.gotOpts.Offset)
		})
	}
}

func TestAdminListCharactersReturnsThePageBeyondTheEndWithItsTrueTotal(t *testing.T) {
	chars := &fakeAdminCharacterReader{page: world.AdminCharacterPage{TotalCount: 3}}
	srv := newAdminCharServer(t, chars, nil)

	req := listReq()
	req.Page, req.PageSize = 6, 2
	resp, err := srv.AdminListCharacters(context.Background(), req)
	require.NoError(t, err, "a page beyond the end is an empty page, never an error")
	assert.Empty(t, resp.GetCharacters())
	assert.Equal(t, int64(3), resp.GetTotalCount())
}

// governedProfileNames is the twelve, read back out of the shipped map rather
// than re-spelled — a fourth spelling of the list could drift from the edit
// surface, which is the exact property this test is asserting.
func governedProfileNames() map[string]struct{} {
	out := make(map[string]struct{}, len(updateCharacterProfileMaskablePaths))
	for name := range updateCharacterProfileMaskablePaths {
		out[name] = struct{}{}
	}
	return out
}

// TestAdminGetCharacterProjectsExactlyTheTwelveGovernedProfileValues asserts
// set equality in BOTH directions over a fixture that CAN contain the failing
// case.
//
// The fixture seeds the twelve governed rows plus TWO discriminators:
//
//   - `internal.scratch` — an arbitrary non-governed row. It discriminates
//     NOTHING on its own: any plausible filter rejects it.
//   - `profile.image.gallery.00` — a GALLERY SLOT. It is rejected by
//     updateCharacterProfileMaskablePaths and ACCEPTED by isGovernedProfileName,
//     so it is the only seeded row that tells the two filters apart. Without it
//     this test passes identically under the correct twelve-name filter and
//     under the twenty-three-name one, and reads as coverage for a property it
//     cannot observe.
func TestAdminGetCharacterProjectsExactlyTheTwelveGovernedProfileValues(t *testing.T) {
	id, playerID := ulid.Make(), ulid.Make()

	rows := make([]*world.EntityProperty, 0, 14)
	for name := range governedProfileNames() {
		rows = append(rows, &world.EntityProperty{Name: name, Value: strPtr("value of " + name)})
	}
	rows = append(
		rows,
		&world.EntityProperty{Name: "internal.scratch", Value: strPtr("plugin scribble")},
		&world.EntityProperty{Name: "profile.image.gallery.00", Value: strPtr("media-01")},
	)

	chars := &fakeAdminCharacterReader{row: world.AdminCharacterRow{
		Character: &world.Character{
			ID: id, PlayerID: playerID, Name: "Ada", Description: "the in-world look text",
			CreatedAt: time.Unix(0, 1).UTC(), Version: 2, Status: world.StatusActive,
		},
		PlayerUsername: "ada_player",
	}}
	srv := newAdminCharServer(t, chars, &fakeAdminProfileReader{rows: rows})

	resp, err := srv.AdminGetCharacter(context.Background(), &adminportalv1.AdminGetCharacterRequest{
		PlayerSessionToken: "tok", CharacterId: id.String(),
	})
	require.NoError(t, err)

	detail := resp.GetCharacter()
	assert.Equal(t, "the in-world look text", detail.GetDescription(),
		"description is a characters COLUMN, which is why the writable set is thirteen and the map is twelve")
	assert.Equal(t, "ada_player", detail.GetCharacter().GetPlayerUsername(),
		"the detail embeds the SAME §11.3 projection the list carries, username included")

	want := governedProfileNames()
	got := map[string]struct{}{}
	for name := range detail.GetProfile() {
		got[name] = struct{}{}
	}
	require.NotEmpty(t, want, "an empty expectation would satisfy the EXTRA direction vacuously")
	require.NotEmpty(t, got, "an empty response would satisfy the MISSING direction vacuously")

	for name := range want {
		assert.Contains(t, got, name, "MISSING governed value %q", name)
	}
	for name := range got {
		assert.Contains(t, want, name, "LEAKED non-governed value %q", name)
	}
	assert.Len(t, detail.GetProfile(), len(want))
}

// TestAdminGetCharacterOmitsAGovernedRowWhoseValueIsNull matches
// ownedProfileAttributes (characteraccess_owner.go:176-179): a flag-style row
// carries no value to write back, and emitting it as present-and-empty would
// round-trip through the edit surface as a blank the operator never authored.
func TestAdminGetCharacterOmitsAGovernedRowWhoseValueIsNull(t *testing.T) {
	id := ulid.Make()
	chars := &fakeAdminCharacterReader{row: world.AdminCharacterRow{
		Character:      &world.Character{ID: id, PlayerID: ulid.Make(), CreatedAt: time.Unix(0, 1).UTC(), Status: world.StatusActive},
		PlayerUsername: "p",
	}}
	srv := newAdminCharServer(t, chars, &fakeAdminProfileReader{rows: []*world.EntityProperty{
		{Name: "profile.pronouns", Value: strPtr("they/them")},
		{Name: "profile.concept", Value: nil},
	}})

	resp, err := srv.AdminGetCharacter(context.Background(), &adminportalv1.AdminGetCharacterRequest{
		PlayerSessionToken: "tok", CharacterId: id.String(),
	})
	require.NoError(t, err)

	profile := resp.GetCharacter().GetProfile()
	assert.Equal(t, "they/them", profile["profile.pronouns"])
	assert.NotContains(t, profile, "profile.concept",
		"a NULL value is OMITTED, not sent as present-and-empty")
}

// TestAdminCharacterHandlersRefuseWhenAReaderIsMissing pins the two failure
// shapes that must never be an empty answer.
func TestAdminCharacterHandlersRefuseWhenAReaderIsMissing(t *testing.T) {
	t.Run("no character reader is Internal, never an empty page", func(t *testing.T) {
		srv := newAdminCharServer(t, nil, &fakeAdminProfileReader{})
		_, err := srv.AdminListCharacters(context.Background(), listReq())
		assert.Equal(t, codes.Internal, statusCodeOf(t, err),
			"an empty page would read as 'no characters exist', a claim this server cannot make")
	})

	t.Run("no profile reader is Internal, never twelve empty prose fields", func(t *testing.T) {
		id := ulid.Make()
		chars := &fakeAdminCharacterReader{row: world.AdminCharacterRow{
			Character: &world.Character{ID: id, PlayerID: ulid.Make(), CreatedAt: time.Unix(0, 1).UTC(), Status: world.StatusActive},
		}}
		srv := newAdminCharServer(t, chars, nil)
		_, err := srv.AdminGetCharacter(context.Background(), &adminportalv1.AdminGetCharacterRequest{
			PlayerSessionToken: "tok", CharacterId: id.String(),
		})
		assert.Equal(t, codes.Internal, statusCodeOf(t, err),
			"twelve empty prose fields is the exact input that makes the edit sheet overwrite on save")
	})
}

// TestAdminCharacterErrorsCarryNoInnerText pins the .claude/rules/grpc-errors.md
// rule at this boundary: an inner error never reaches the client's status
// message.
func TestAdminCharacterErrorsCarryNoInnerText(t *testing.T) {
	chars := &fakeAdminCharacterReader{
		err: oops.Code("CHARACTER_ADMIN_LIST_FAILED").Errorf("relation \"characters\" does not exist"),
	}
	srv := newAdminCharServer(t, chars, nil)

	_, err := srv.AdminListCharacters(context.Background(), listReq())
	require.Equal(t, codes.Internal, statusCodeOf(t, err))
	st, _ := status.FromError(err)
	assert.Equal(t, "internal error", st.Message())
	assert.NotContains(t, st.Message(), "relation")
}

// TestAdminCharacterSortFieldUnsupportedFromTheRepositoryIsInvalidArgument
// covers the second, defence-in-depth rejection: the repository refuses an
// unknown key too, and that refusal is a caller fault rather than an outage.
func TestAdminCharacterSortFieldUnsupportedFromTheRepositoryIsInvalidArgument(t *testing.T) {
	chars := &fakeAdminCharacterReader{
		err: oops.Code("CHARACTER_ADMIN_SORT_FIELD_UNSUPPORTED").Errorf("nope"),
	}
	srv := newAdminCharServer(t, chars, nil)

	_, err := srv.AdminListCharacters(context.Background(), listReq())
	assert.Equal(t, codes.InvalidArgument, statusCodeOf(t, err))
}
