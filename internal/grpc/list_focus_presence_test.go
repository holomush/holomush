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

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// ---------- T7 test helpers ----------

// stubNameResolver is an in-process characterNameResolver for T7 tests.
type stubNameResolver struct {
	names map[ulid.ULID]string
}

func (s *stubNameResolver) Names(_ context.Context, ids []ulid.ULID) (map[ulid.ULID]string, error) {
	out := make(map[ulid.ULID]string, len(ids))
	for _, id := range ids {
		if n, ok := s.names[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

// mkActiveAt returns a session.Info with Status=Active, non-zero ExpiresAt,
// and the given characterID / locationID. PlayerID matches ownedPlayerID so
// that newFakePlayerSessionRepo(ownedPlayerID) passes ownership validation.
func mkActiveAt(id string, characterID, locationID ulid.ULID) *session.Info {
	future := time.Now().Add(time.Hour)
	return &session.Info{
		ID:          id,
		Status:      session.StatusActive,
		ExpiresAt:   &future,
		CharacterID: characterID,
		LocationID:  locationID,
		PlayerID:    ownedPlayerID,
	}
}

// entryNames extracts CharacterName from each PresenceEntry, for compact assertions.
func entryNames(entries []*corev1.PresenceEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.CharacterName)
	}
	return out
}

func TestListFocusPresenceReturnsInvalidArgumentOnNilRequest(t *testing.T) {
	// Defends against direct-call misuse (test or wrapping code passing nil).
	// ConnectRPC's wire path never delivers a nil request, but a direct nil
	// would panic on req.Meta access without this guard.
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	_, err := s.ListFocusPresence(context.Background(), nil)
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT", o.Code())
}

func TestListFocusPresenceReturnsInvalidArgumentOnEmptySessionID(t *testing.T) {
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	_, err := s.ListFocusPresence(context.Background(),
		&corev1.ListFocusPresenceRequest{SessionId: ""})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT", o.Code())
}

func TestListFocusPresenceReturnsSessionNotFoundOnUnknownSession(t *testing.T) {
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	_, err := s.ListFocusPresence(context.Background(),
		&corev1.ListFocusPresenceRequest{
			SessionId:          "missing",
			PlayerSessionToken: testPlayerSessionToken,
		})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

func TestListFocusPresenceCollapsesOwnershipMismatchToNotFound(t *testing.T) {
	// Caller's player_session_token resolves to a player session with a
	// different PlayerID than the target session's PlayerID — ownership
	// mismatch MUST collapse to SESSION_NOT_FOUND (enumeration-safe).
	future := time.Now().Add(time.Hour)
	foreignSess := &session.Info{
		ID:        "foreign-sess",
		PlayerID:  ulid.MustParse("01HYXPLAYER000000000000XYZ"),
		ExpiresAt: &future,
	}
	s := &CoreServer{
		sessionStore: newTestSessionStore(t,
			map[string]*session.Info{"foreign-sess": foreignSess}),
		// newFakePlayerSessionRepo binds testPlayerSessionToken to the given
		// player_id; passing a DIFFERENT ID below forces mismatch.
		playerSessionRepo: newFakePlayerSessionRepo(
			ulid.MustParse("01HYXPLAYER111111111111ABC"),
		),
	}
	_, err := s.ListFocusPresence(context.Background(),
		&corev1.ListFocusPresenceRequest{
			SessionId:          "foreign-sess",
			PlayerSessionToken: testPlayerSessionToken,
		})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

// ownedPlayerID is the player ID that newFakePlayerSessionRepo will return
// when called with this value, and the session.Info.PlayerID that mkOwnedSession
// seeds — ensuring ownership validation succeeds.
var ownedPlayerID = ulid.MustParse("01HYXOWN0000000000000000PS")

// mkOwnedSession returns a session.Info whose PlayerID matches ownedPlayerID,
// so that newFakePlayerSessionRepo(ownedPlayerID) passes ownership validation.
func mkOwnedSession(id string, opts ...func(*session.Info)) *session.Info {
	future := time.Now().Add(time.Hour)
	s := &session.Info{
		ID:        id,
		Status:    session.StatusActive,
		ExpiresAt: &future,
		PlayerID:  ownedPlayerID,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func TestListFocusPresenceReturnsSessionExpiredForExpiredSession(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	sess := mkOwnedSession("sess-expired")
	sess.ExpiresAt = &past
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-expired": sess}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
	}
	_, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId:          "sess-expired",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_EXPIRED", o.Code())
}

func TestListFocusPresenceReturnsUnimplementedForSceneFocus(t *testing.T) {
	sess := mkOwnedSession("sess-1", func(s *session.Info) {
		s.CharacterID = ulid.MustParse("01HYXCHAR0000000000000000C")
		s.LocationID = ulid.MustParse("01HYXLOC00000000000000000L")
		s.FocusMemberships = []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: ulid.MustParse("01HYXSCENE0000000000000000")},
		}
	})
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-1": sess}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
	}
	_, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId:          "sess-1",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "UNIMPLEMENTED", o.Code())
}

func TestListFocusPresenceReturnsEmptyEntriesWhenLocationUnset(t *testing.T) {
	sess := mkOwnedSession("sess-1", func(s *session.Info) {
		s.CharacterID = ulid.MustParse("01HYXCHAR0000000000000000C")
		// LocationID intentionally zero
	})
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-1": sess}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
	}
	resp, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId:          "sess-1",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Equal(t, corev1.PresenceContext_PRESENCE_CONTEXT_LOCATION, resp.Context)
	assert.Equal(t, "", resp.ContextId)
	assert.Empty(t, resp.Entries)
}

// ---------- T7 tests ----------

// I-PRES-4: ABAC denial → PERMISSION_DENIED.
func TestListFocusPresenceReturnsPermissionDeniedWhenABACDenies(t *testing.T) {
	char := ulid.MustParse("01HYXCHARALICE0000000000AA")
	loc := ulid.MustParse("01HYXLOCATION0000000000001")
	sess := mkActiveAt("sess-1", char, loc)
	s := &CoreServer{
		sessionStore:          newTestSessionStore(t, map[string]*session.Info{"sess-1": sess}),
		playerSessionRepo:     newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:          policytest.DenyAllEngine(),
		characterNameResolver: &stubNameResolver{},
	}
	_, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-1", PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "PERMISSION_DENIED", o.Code())
}

// I-PRES-1 + I-PRES-6: two Active sessions at same location, both returned
// with state=ACTIVE; caller is included in the result.
func TestListFocusPresenceReturnsCallerAndOtherSessions(t *testing.T) {
	char1 := ulid.MustParse("01HYXCHARALICE0000000000AA")
	char2 := ulid.MustParse("01HYXCHARBOB000000000000BB")
	loc := ulid.MustParse("01HYXLOCATION0000000000001")
	caller := mkActiveAt("sess-1", char1, loc)
	other := mkActiveAt("sess-2", char2, loc)
	grant := policytest.NewGrantEngine()
	grant.Grant(access.CharacterSubject(char1.String()), "list_presence", access.LocationResource(loc.String()))

	s := &CoreServer{
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			"sess-1": caller, "sess-2": other,
		}),
		playerSessionRepo:     newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:          grant,
		characterNameResolver: &stubNameResolver{names: map[ulid.ULID]string{char1: "alice", char2: "bob"}},
	}
	resp, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-1", PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Equal(t, corev1.PresenceContext_PRESENCE_CONTEXT_LOCATION, resp.Context)
	assert.Equal(t, loc.String(), resp.ContextId)
	require.Len(t, resp.Entries, 2)
	for _, e := range resp.Entries {
		assert.Equal(t, corev1.PresenceState_PRESENCE_STATE_ACTIVE, e.State)
	}
	names := entryNames(resp.Entries)
	assert.Contains(t, names, "alice")
	assert.Contains(t, names, "bob")
}

// Name resolution missing → entry skipped; other entries still returned.
func TestListFocusPresenceSkipsEntryWhenNameUnresolved(t *testing.T) {
	char1 := ulid.MustParse("01HYXCHARALICE0000000000AA")
	char2 := ulid.MustParse("01HYXCHARBOB000000000000BB")
	loc := ulid.MustParse("01HYXLOCATION0000000000001")
	caller := mkActiveAt("sess-1", char1, loc)
	other := mkActiveAt("sess-2", char2, loc)
	grant := policytest.NewGrantEngine()
	grant.Grant(access.CharacterSubject(char1.String()), "list_presence", access.LocationResource(loc.String()))

	s := &CoreServer{
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			"sess-1": caller, "sess-2": other,
		}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:      grant,
		// char2 absent — its entry must be skipped.
		characterNameResolver: &stubNameResolver{names: map[ulid.ULID]string{char1: "alice"}},
	}
	resp, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-1", PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, "alice", resp.Entries[0].CharacterName)
}

// I-PRES-9: defense-in-depth dedup — two sessions for the same character_id
// must collapse to a single presence entry.
func TestListFocusPresenceDeduplicatesByCharacterID(t *testing.T) {
	char := ulid.MustParse("01HYXCHARALICE0000000000AA")
	loc := ulid.MustParse("01HYXLOCATION0000000000001")
	caller := mkActiveAt("sess-1", char, loc)
	dup := mkActiveAt("sess-2", char, loc)
	grant := policytest.NewGrantEngine()
	grant.Grant(access.CharacterSubject(char.String()), "list_presence", access.LocationResource(loc.String()))

	s := &CoreServer{
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			"sess-1": caller, "sess-2": dup,
		}),
		playerSessionRepo:     newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:          grant,
		characterNameResolver: &stubNameResolver{names: map[ulid.ULID]string{char: "alice"}},
	}
	resp, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-1", PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Entries, 1, "duplicate character_id must collapse to single entry")
}

// I-PRES-1 cross-location: the handler trusts ListActiveByLocation to filter;
// sessions at a different location must not appear in the response.
func TestListFocusPresenceDoesNotLeakSessionsFromOtherLocations(t *testing.T) {
	char1 := ulid.MustParse("01HYXCHARALICE0000000000AA")
	char2 := ulid.MustParse("01HYXCHARBOB000000000000BB")
	loc1 := ulid.MustParse("01HYXLOCATION0000000000001")
	loc2 := ulid.MustParse("01HYXLOCATION0000000000002")
	caller := mkActiveAt("sess-1", char1, loc1)
	elsewhere := mkActiveAt("sess-2", char2, loc2) // different location
	grant := policytest.NewGrantEngine()
	grant.Grant(access.CharacterSubject(char1.String()), "list_presence", access.LocationResource(loc1.String()))

	s := &CoreServer{
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			"sess-1": caller, "sess-2": elsewhere,
		}),
		playerSessionRepo:     newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:          grant,
		characterNameResolver: &stubNameResolver{names: map[ulid.ULID]string{char1: "alice"}},
	}
	resp, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-1", PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, "alice", resp.Entries[0].CharacterName)
}

// Missing accessEngine → PERMISSION_DENIED (fail-closed; defense against
// misconfigured construction paths that omit WithAccessEngine).
func TestListFocusPresenceReturnsPermissionDeniedWhenAccessEngineIsNil(t *testing.T) {
	char := ulid.MustParse("01HYXCHARALICE0000000000AA")
	loc := ulid.MustParse("01HYXLOCATION0000000000001")
	sess := mkActiveAt("sess-1", char, loc)
	s := &CoreServer{
		sessionStore:          newTestSessionStore(t, map[string]*session.Info{"sess-1": sess}),
		playerSessionRepo:     newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:          nil, // explicit
		characterNameResolver: &stubNameResolver{},
	}
	_, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-1", PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "PERMISSION_DENIED", o.Code())
}

// Missing characterNameResolver → INTERNAL (misconfiguration; not a security
// boundary, so we don't fail-closed with PERMISSION_DENIED).
func TestListFocusPresenceReturnsInternalWhenNameResolverIsNil(t *testing.T) {
	char := ulid.MustParse("01HYXCHARALICE0000000000AA")
	loc := ulid.MustParse("01HYXLOCATION0000000000001")
	sess := mkActiveAt("sess-1", char, loc)
	grant := policytest.NewGrantEngine()
	grant.Grant(access.CharacterSubject(char.String()), "list_presence", access.LocationResource(loc.String()))
	s := &CoreServer{
		sessionStore:          newTestSessionStore(t, map[string]*session.Info{"sess-1": sess}),
		playerSessionRepo:     newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:          grant,
		characterNameResolver: nil, // explicit
	}
	_, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-1", PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INTERNAL", o.Code())
}
