// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/profilevis"
	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/world"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
)

// The viewer-identity seam is unit-tested rather than driven through the full
// stack because behaviors 7-9 are branch coverage on ONE function — which rung a
// token resolves to — not stack behavior. The end-to-end proof that a guest
// token actually moves the rung lives in
// test/integration/access/character_profile_read_test.go.

const testCAToken = "test-character-access-token"

// stubCharacterWorldReader returns one fixed description for any character, so a
// spec about viewer rungs is not also a spec about the world repository.
type stubCharacterWorldReader struct {
	desc world.CharacterDescription
	err  error
}

func (s *stubCharacterWorldReader) ListPropertiesByParent(_ context.Context, _ world.Caller, _ string, _ ulid.ULID) ([]*world.EntityProperty, error) {
	return nil, nil
}

func (s *stubCharacterWorldReader) GetCharacterDescription(_ context.Context, _ world.Caller, _ ulid.ULID) (world.CharacterDescription, error) {
	if s.err != nil {
		return world.CharacterDescription{}, s.err
	}
	return s.desc, nil
}

// recordingProfileVisibility permits every profile and RECORDS the viewer
// subject it was handed. Recording is the whole point: the subject string is the
// only externally observable evidence of which rung the facade resolved, so a
// spec that merely asserted "the call succeeded" would pass just as well if all
// three rungs collapsed into anonymous.
type recordingProfileVisibility struct {
	reachable bool
	err       error
	subjects  []string
}

func (r *recordingProfileVisibility) Reachable(_ context.Context, viewerSubject, _ string) (bool, error) {
	r.subjects = append(r.subjects, viewerSubject)
	if r.err != nil {
		return false, r.err
	}
	return r.reachable, nil
}

func (r *recordingProfileVisibility) VisibleAttributes(_ context.Context, viewerSubject, _ string, _ []profilevis.Property) (map[string]profilevis.Property, error) {
	r.subjects = append(r.subjects, viewerSubject)
	return nil, r.err
}

// newCAServerForToken wires a CharacterAccessServer whose only interesting
// dependency is the identity seam: the session repo resolves testCAToken to a
// session for playerID, and the player repo returns player for that id.
func newCAServerForToken(
	t *testing.T,
	playerID ulid.ULID,
	player *auth.Player,
	playerErr error,
) (*CharacterAccessServer, *recordingProfileVisibility) {
	t.Helper()

	ps := &auth.PlayerSession{ID: idgen.New(), PlayerID: playerID, TokenHash: auth.HashSessionToken(testCAToken)}
	sessionRepo := authmocks.NewMockPlayerSessionRepository(t)
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, ps.TokenHash).Return(ps, nil).Maybe()
	sessionRepo.EXPECT().GetByTokenHash(mock.Anything, mock.Anything).
		Return(nil, oops.Code("PLAYER_SESSION_NOT_FOUND").Errorf("no such session")).Maybe()
	sessionRepo.EXPECT().RefreshTTL(mock.Anything, ps.ID, auth.PlayerSessionTTL).Return(nil).Maybe()

	playerRepo := authmocks.NewMockPlayerRepository(t)
	playerRepo.EXPECT().GetByID(mock.Anything, playerID).Return(player, playerErr).Maybe()

	vis := &recordingProfileVisibility{reachable: true}
	srv := NewCharacterAccessServer(
		&stubCharacterWorldReader{desc: world.CharacterDescription{Name: "Ada", Description: "A tinkerer."}},
		// The public read path never mutates: a double that fails on call keeps
		// that continuously enforced.
		&recordingWorldMutator{t: t, failOnCall: true},
		vis,
		&failOnCallDirectoryGate{t: t},
		&failOnCallDirectoryReader{t: t},
		sessionRepo,
		playerRepo,
		// The viewer-identity seam never reaches the owner audience, so a
		// STRICT mock with no expectations is the right double: a regression
		// that routed a public read through the gate fails here loudly rather
		// than nil-panicking.
		authmocks.NewMockCharacterRepository(t),
	)
	return srv, vis
}

// TestViewerIdentityResolvesThreeDistinguishableRungsFromTheSessionToken is
// behavior 7: the three rungs must be DISTINGUISHABLE, not merely successful.
// Each subtest asserts the constructed ABAC subject string, so three rungs
// collapsing into anonymous fails here rather than silently under-privileging
// every authenticated reader.
func TestViewerIdentityResolvesThreeDistinguishableRungsFromTheSessionToken(t *testing.T) {
	t.Parallel()

	playerID := idgen.New()
	charID := idgen.New()

	t.Run("no token yields the anonymous rung with the player id omitted", func(t *testing.T) {
		t.Parallel()
		srv, vis := newCAServerForToken(t, playerID, &auth.Player{ID: playerID}, nil)

		_, err := srv.GetCharacterProfile(context.Background(), &characteraccessv1.GetCharacterProfileRequest{
			CharacterId: charID.String(),
		})
		require.NoError(t, err)
		require.NotEmpty(t, vis.subjects)
		assert.Equal(t, "viewer:anonymous", vis.subjects[0],
			"the anonymous rung carries NO identifier — an empty-string sentinel would be fail-open")
	})

	t.Run("a token resolving to a guest player yields the guest rung carrying that ULID", func(t *testing.T) {
		t.Parallel()
		srv, vis := newCAServerForToken(t, playerID, &auth.Player{ID: playerID, IsGuest: true}, nil)

		_, err := srv.GetCharacterProfile(context.Background(), &characteraccessv1.GetCharacterProfileRequest{
			CharacterId:        charID.String(),
			PlayerSessionToken: testCAToken,
		})
		require.NoError(t, err)
		require.NotEmpty(t, vis.subjects)
		assert.Equal(t, access.ViewerSubject(access.ViewerTierGuest, playerID.String()), vis.subjects[0],
			"a guest reaches a GUEST rung — playerGate.resolveAndGate would have refused this caller outright")
	})

	t.Run("a token resolving to a non-guest player yields the player rung carrying that ULID", func(t *testing.T) {
		t.Parallel()
		srv, vis := newCAServerForToken(t, playerID, &auth.Player{ID: playerID, IsGuest: false}, nil)

		_, err := srv.GetCharacterProfile(context.Background(), &characteraccessv1.GetCharacterProfileRequest{
			CharacterId:        charID.String(),
			PlayerSessionToken: testCAToken,
		})
		require.NoError(t, err)
		require.NotEmpty(t, vis.subjects)
		assert.Equal(t, access.ViewerSubject(access.ViewerTierPlayer, playerID.String()), vis.subjects[0])
	})
}

// TestViewerIdentityDegradesAnUnresolvableTokenToAnonymousRatherThanRefusing is
// behavior 8. A logged-out visitor whose stale cookie is still attached must
// still get the public page, and the degradation is fail-CLOSED because
// anonymous is the least-privileged rung — it can only narrow what the caller
// sees, never widen it.
func TestViewerIdentityDegradesAnUnresolvableTokenToAnonymousRatherThanRefusing(t *testing.T) {
	t.Parallel()

	playerID := idgen.New()
	charID := idgen.New()
	srv, vis := newCAServerForToken(t, playerID, &auth.Player{ID: playerID}, nil)

	_, err := srv.GetCharacterProfile(context.Background(), &characteraccessv1.GetCharacterProfileRequest{
		CharacterId:        charID.String(),
		PlayerSessionToken: "a-well-formed-token-that-resolves-to-nothing",
	})
	require.NoError(t, err, "an expired or unknown token MUST NOT produce Unauthenticated on the public read")
	require.NotEmpty(t, vis.subjects)
	assert.Equal(t, "viewer:anonymous", vis.subjects[0],
		"an unresolvable token degrades to the LEAST-privileged rung, never to a higher one")
}

// TestViewerIdentityReportsAPlayerLookupFailureAsInternalRatherThanAnonymous is
// behavior 9, and it is the one that keeps 01-SPEC §8.10 honest: a rung is a
// policy answer and an outage is not one. Rendering an identity-resolution
// outage as a successful anonymous-rung read would silently downgrade an
// authenticated viewer's profile and look like a working feature.
func TestViewerIdentityReportsAPlayerLookupFailureAsInternalRatherThanAnonymous(t *testing.T) {
	t.Parallel()

	playerID := idgen.New()
	charID := idgen.New()
	srv, vis := newCAServerForToken(t, playerID, nil, oops.Code("PLAYER_GET_FAILED").Errorf("database is on fire"))

	resp, err := srv.GetCharacterProfile(context.Background(), &characteraccessv1.GetCharacterProfileRequest{
		CharacterId:        charID.String(),
		PlayerSessionToken: testCAToken,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Internal, status.Code(err),
		"an identity-resolution OUTAGE is Internal, not an anonymous-rung success")
	assert.Empty(t, vis.subjects,
		"the visibility gate must never be reached with a rung derived from a failed lookup")
	assert.NotContains(t, status.Convert(err).Message(), "database is on fire",
		"the inner error MUST NOT be interpolated into the wire message")
}

// TestResolveViewerTierIsExhaustiveWithADenyingDefault pins the mapping half in
// isolation. The default arm is the load-bearing one: a tier token outside the
// closed set must yield NO subject, so an unrecognized rung becomes a denial
// rather than a subject string no policy matches (which fails closed too, but
// silently and for the wrong reason).
func TestResolveViewerTierIsExhaustiveWithADenyingDefault(t *testing.T) {
	t.Parallel()

	playerID := idgen.New().String()

	tests := []struct {
		name        string
		identity    viewerIdentity
		wantSubject string
		wantOK      bool
	}{
		{"anonymous with no player id yields the bare anonymous subject", viewerIdentity{tier: access.ViewerTierAnonymous}, "viewer:anonymous", true},
		{"guest with a player id yields the guest subject carrying it", viewerIdentity{tier: access.ViewerTierGuest, playerID: playerID}, "viewer:guest:" + playerID, true},
		{"player with a player id yields the player subject carrying it", viewerIdentity{tier: access.ViewerTierPlayer, playerID: playerID}, "viewer:player:" + playerID, true},
		{"guest with no player id is denied rather than emitting a bare prefix", viewerIdentity{tier: access.ViewerTierGuest}, "", false},
		{"an unrecognized tier token is denied by the default arm", viewerIdentity{tier: "spectator", playerID: playerID}, "", false},
		{"the zero identity is denied by the default arm", viewerIdentity{}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			subject, ok := resolveViewerTier(tt.identity)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantSubject, subject)
		})
	}
}
