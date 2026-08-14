// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// The three admin CHARACTER READS at the WIRE, over a real gRPC connection to a
// server built by the production factory.
//
// Each of the three carries a denial AND a paired positive control, because
// `err != nil` on its own cannot tell "denied for lack of the admin role" apart
// from "the RPC is broken" or "the reader was never wired" — and this plan
// wires two new readers at two composition roots, so a silently-unwired one is
// exactly the failure a bare denial assertion would hide.
package access_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/testsupport/integrationtest"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// adminCharacterDeniedMessage is the ONE message every admin-portal refusal
// carries, whichever arm produced it.
const adminCharacterDeniedMessage = "admin section access denied"

// requireOpaqueRefusal asserts the wire half only: the mapped code, the exact
// static message, and the absence of every distinguishing substring.
//
// It asserts NO oops code, because none survives a gRPC round trip — an
// assertion that appeared to pass on one here would be reading something else.
func requireOpaqueRefusal(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err, "a player with no admin role MUST be refused")
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	msg := status.Convert(err).Message()
	require.Equal(t, adminCharacterDeniedMessage, msg,
		"every refusal MUST carry the SAME static message, so the arm the caller tripped is invisible")
	for _, leak := range []string{"DENY_", "admin_section", "characters", "player_id", "not found"} {
		require.NotContains(t, msg, leak,
			"the wire message MUST NOT carry %q: a distinguishing field turns a refusal into an oracle", leak)
	}
}

// TestEachAdminCharacterReadDeniesANonAdminAtTheWireWithAPairedControl walks all
// three RPCs through both outcomes in one table, so an RPC added to the service
// without a descriptor entry cannot be covered by omission.
func TestEachAdminCharacterReadDeniesANonAdminAtTheWireWithAPairedControl(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(
		t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())

	nonAdmin := srv.ConnectAuthed(ctx, "Charsplain")
	admin := srv.ConnectAuthedWithRoles(ctx, "Charsadmin", []string{"admin"})

	// The admin's own character is a real row, so the positive controls read
	// something rather than passing on an empty database.
	adminCharacterID := admin.CharacterID.String()

	tests := []struct {
		name string
		call func(token string) error
	}{
		{
			name: "AdminListCharacters",
			call: func(token string) error {
				_, err := client.AdminListCharacters(ctx, &adminportalv1.AdminListCharactersRequest{
					PlayerSessionToken: token,
					SortField:          adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_NAME,
					Page:               1,
				})
				return err
			},
		},
		{
			name: "AdminSearchCharacters",
			call: func(token string) error {
				_, err := client.AdminSearchCharacters(ctx, &adminportalv1.AdminSearchCharactersRequest{
					PlayerSessionToken: token,
					SortField:          adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_NAME,
					Page:               1,
					Query:              "char",
				})
				return err
			},
		},
		{
			name: "AdminGetCharacter",
			call: func(token string) error {
				_, err := client.AdminGetCharacter(ctx, &adminportalv1.AdminGetCharacterRequest{
					PlayerSessionToken: token,
					CharacterId:        adminCharacterID,
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" refuses a non-admin", func(t *testing.T) {
			requireOpaqueRefusal(t, tt.call(nonAdmin.PlayerSessionToken()))
		})
		t.Run(tt.name+" admits an admin (paired positive control)", func(t *testing.T) {
			require.NoError(t, tt.call(admin.PlayerSessionToken()),
				"positive control: an admin MUST pass the same gate on the same RPC")
		})
	}
}

// TestAdminListCharactersAtTheWireCarriesTheJoinedUsernameAndTheTotal is the
// substance behind the positive control: the projection really is the joined
// one, and total_count is really populated.
func TestAdminListCharactersAtTheWireCarriesTheJoinedUsernameAndTheTotal(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(
		t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())
	admin := srv.ConnectAuthedWithRoles(ctx, "Charsjoin", []string{"admin"})

	resp, err := client.AdminListCharacters(ctx, &adminportalv1.AdminListCharactersRequest{
		PlayerSessionToken: admin.PlayerSessionToken(),
		SortField:          adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_NAME,
		Page:               1,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetCharacters(),
		"the harness seeded at least the admin's own character; an empty page here means an unwired reader")
	require.Positive(t, resp.GetTotalCount())

	for _, c := range resp.GetCharacters() {
		assert.NotEmpty(t, c.GetPlayerUsername(), "the joined players.username rides on every row")
		assert.NotEmpty(t, c.GetStatus(), "Status is READ, not left zero by a partial projection")
		assert.NotEmpty(t, c.GetId())
		assert.NotEmpty(t, c.GetPlayerId())
	}
}

// TestAdminCharacterReadsRefuseAnUnsetSortFieldAtTheWire pins the rejection at
// the transport, not only in-process: a silently-defaulted ordering would be
// indistinguishable from an honoured one for a client.
func TestAdminCharacterReadsRefuseAnUnsetSortFieldAtTheWire(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(
		t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())
	admin := srv.ConnectAuthedWithRoles(ctx, "Charssort", []string{"admin"})

	_, err := client.AdminListCharacters(ctx, &adminportalv1.AdminListCharactersRequest{
		PlayerSessionToken: admin.PlayerSessionToken(),
		SortField:          adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_UNSPECIFIED,
		Page:               1,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestAdminSearchCharactersAtTheWireHandlesTheEmptyAndCfOnlyTerms is the
// criterion that CANNOT be asserted at the repository: the term arrives there
// already normalized, so a repository-level test never exercises the branch
// where the bug lives. A handler that normalized FIRST returns
// NAME_EMPTY_NORMAL_FORM for all three cases and fails here.
func TestAdminSearchCharactersAtTheWireHandlesTheEmptyAndCfOnlyTerms(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(
		t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())
	admin := srv.ConnectAuthedWithRoles(ctx, "Charsempty", []string{"admin"})

	search := func(t *testing.T, query string) *adminportalv1.AdminSearchCharactersResponse {
		t.Helper()
		resp, err := client.AdminSearchCharacters(ctx, &adminportalv1.AdminSearchCharactersRequest{
			PlayerSessionToken: admin.PlayerSessionToken(),
			SortField:          adminportalv1.AdminCharacterSortField_ADMIN_CHARACTER_SORT_FIELD_NAME,
			Page:               1,
			Query:              query,
		})
		require.NoError(t, err, "none of these three is an error case")
		return resp
	}

	unfiltered := search(t, "")
	require.NotEmpty(t, unfiltered.GetCharacters(),
		"an empty term returns the UNFILTERED first page — the branch a normalize-first handler cannot reach")

	whitespace := search(t, "   ")
	assert.Equal(t, unfiltered.GetTotalCount(), whitespace.GetTotalCount(),
		"a whitespace-only term is blank after trimming and behaves identically")

	// U+200D ZERO WIDTH JOINER: non-blank, but category Cf, so nothing survives
	// normalization.
	cfOnly := search(t, "\u200d")
	assert.Empty(t, cfOnly.GetCharacters(),
		"a term that normalizes to nothing means 'no matches', which needs no error surface")
	assert.Equal(t, int64(0), cfOnly.GetTotalCount())
}

// TestAdminGetCharacterAtTheWireReturnsTheDetailForTheEditSurface proves the
// bounded trusted projection end to end: the detail read reaches the property
// repository through WithAdminProfileReader at THIS composition root.
//
// Wiring only the production root would leave this returning a detail with an
// Internal error; routing the read through world.Service.ListPropertiesByParent
// instead would return an EMPTY profile map, because every shipped property-read
// permit is character-, viewer- or plugin-principal and the admin caller is a
// PLAYER.
func TestAdminGetCharacterAtTheWireReturnsTheDetailForTheEditSurface(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(
		t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())
	admin := srv.ConnectAuthedWithRoles(ctx, "Charsdetail", []string{"admin"})

	resp, err := client.AdminGetCharacter(ctx, &adminportalv1.AdminGetCharacterRequest{
		PlayerSessionToken: admin.PlayerSessionToken(),
		CharacterId:        admin.CharacterID.String(),
	})
	require.NoError(t, err)

	detail := resp.GetCharacter()
	require.NotNil(t, detail)
	assert.Equal(t, admin.CharacterID.String(), detail.GetCharacter().GetId())
	assert.NotEmpty(t, detail.GetCharacter().GetPlayerUsername(),
		"the detail embeds the SAME joined projection the list carries")

	// The projection really reaches entity_properties, and really filters it.
	// The two seeded rows are the discriminating pair: a GOVERNED name that must
	// come back, and a GALLERY SLOT that must not — the gallery row is what
	// tells updateCharacterProfileMaskablePaths apart from
	// isGovernedProfileName, which admits it.
	assert.Empty(t, detail.GetProfile(),
		"before seeding, a character that has authored nothing carries no profile values")

	seedProperty(ctx, t, srv, admin.CharacterID, "profile.pronouns", "they/them")
	seedProperty(ctx, t, srv, admin.CharacterID, "profile.image.gallery.00", "media-01")

	reread, err := client.AdminGetCharacter(ctx, &adminportalv1.AdminGetCharacterRequest{
		PlayerSessionToken: admin.PlayerSessionToken(),
		CharacterId:        admin.CharacterID.String(),
	})
	require.NoError(t, err)

	profile := reread.GetCharacter().GetProfile()
	assert.Equal(t, "they/them", profile["profile.pronouns"],
		"a GOVERNED value reaches the wire — routing this read through world.Service.ListPropertiesByParent instead returns an EMPTY map, because every shipped property-read permit is character-, viewer- or plugin-principal and this caller is a PLAYER")
	assert.NotContains(t, profile, "profile.image.gallery.00",
		"a gallery slot is NOT in updateCharacterProfileMaskablePaths; a handler filtering on isGovernedProfileName would leak it")
	assert.Len(t, profile, 1)
}

// seedProperty writes one entity_properties row directly, because the admin
// read is the surface under test and routing the fixture through a write API
// would make the test depend on a second gate.
func seedProperty(ctx context.Context, t *testing.T, srv *integrationtest.Server, characterID ulid.ULID, name, value string) {
	t.Helper()
	_, err := srv.Pool().Exec(ctx, `
		INSERT INTO entity_properties (id, parent_type, parent_id, name, value, visibility, created_at, updated_at)
		VALUES ($1, 'character', $2, $3, $4, 'public',
		        (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, ulid.Make().String(), characterID.String(), name, value)
	require.NoError(t, err)
}

// TestAdminGetCharacterAtTheWireRefusesAnUnknownIDWithoutNamingIt keeps the RPC
// from becoming an existence oracle for a caller the gate already permitted.
func TestAdminGetCharacterAtTheWireRefusesAnUnknownIDWithoutNamingIt(t *testing.T) {
	ctx := context.Background()

	srv := integrationtest.Start(
		t,
		integrationtest.WithRealABAC(),
		integrationtest.WithGatedGRPCListener(),
	)
	defer srv.Stop()

	client := adminportalv1.NewAdminPortalServiceClient(srv.GatedGRPCConn())
	admin := srv.ConnectAuthedWithRoles(ctx, "Charsmissing", []string{"admin"})

	const absent = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := client.AdminGetCharacter(ctx, &adminportalv1.AdminGetCharacterRequest{
		PlayerSessionToken: admin.PlayerSessionToken(),
		CharacterId:        absent,
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.NotContains(t, status.Convert(err).Message(), absent,
		"the message names no id")
}
