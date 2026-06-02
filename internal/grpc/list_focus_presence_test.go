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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
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

// mkActiveAt returns a session.Info with Status=Active, GridPresent=true,
// non-zero ExpiresAt, and the given characterID / locationID. PlayerID matches
// ownedPlayerID so that newFakePlayerSessionRepo(ownedPlayerID) passes
// ownership validation. GridPresent=true reflects the invariant that a session
// returned by ListActiveByLocation must be grid-present (INV-PRESENCE-1).
func mkActiveAt(id string, characterID, locationID ulid.ULID) *session.Info {
	future := time.Now().Add(time.Hour)
	return &session.Info{
		ID:          id,
		Status:      session.StatusActive,
		GridPresent: true,
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

// Verifies: INV-PRESENCE-3
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

// Verifies: INV-PRESENCE-3
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

// Verifies: INV-PRESENCE-5
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

// Verifies: INV-PRESENCE-4
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

// Verifies: INV-PRESENCE-1
// Verifies: INV-PRESENCE-4
// Verifies: INV-PRESENCE-6
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

// Verifies: INV-PRESENCE-9 (expired-exclusion half)
// Drift fix (holomush-9mxr Task 10): The former in-memory store allowed two active sessions for the
// same character; PostgresSessionStore enforces idx_sessions_active_character (a
// partial unique index on character_id WHERE status IN ('active','detached')).
// This means it is impossible in production to store two active/detached sessions
// for the same character. This test verifies that ListActiveByLocation excludes
// expired sessions — seeding one active + one expired row for the same character
// and asserting exactly one presence entry is returned.
func TestListFocusPresenceExcludesExpiredSessions(t *testing.T) {
	char := ulid.MustParse("01HYXCHARALICE0000000000AA")
	loc := ulid.MustParse("01HYXLOCATION0000000000001")
	caller := mkActiveAt("sess-1", char, loc)
	// Expired status — excluded from idx_sessions_active_character so both rows
	// can coexist for the same character. ListActiveByLocation returns status=active
	// only, so this does not appear in the presence result.
	expired := mkActiveAt("sess-2", char, loc)
	expired.Status = session.StatusExpired
	grant := policytest.NewGrantEngine()
	grant.Grant(access.CharacterSubject(char.String()), "list_presence", access.LocationResource(loc.String()))

	s := &CoreServer{
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			"sess-1": caller, "sess-2": expired,
		}),
		playerSessionRepo:     newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:          grant,
		characterNameResolver: &stubNameResolver{names: map[ulid.ULID]string{char: "alice"}},
	}
	resp, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-1", PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Entries, 1, "expired session must be excluded from presence list")
}

// Verifies: INV-PRESENCE-9 (dedup-guard half)
// The Postgres unique index idx_sessions_active_character makes it impossible for
// the real store to return two active/detached sessions for the same character.
// This test drives the defensive dedup guard in list_focus_presence.go directly
// via a mock store whose ListActiveByLocation returns two active *session.Info
// with identical CharacterIDs — the precondition the real store's index forbids.
// Asserts the handler collapses them to a single presence entry.
func TestListFocusPresenceDeduplicatesByCharacterID(t *testing.T) {
	char := ulid.MustParse("01HYXCHARALICE0000000000AA")
	loc := ulid.MustParse("01HYXLOCATION0000000000001")

	// Two active sessions with the same CharacterID — impossible in production
	// due to the partial unique index, but the mock injects this precondition
	// to exercise the dedup guard at list_focus_presence.go:147-156.
	future := time.Now().Add(time.Hour)
	dupA := &session.Info{
		ID: "sess-dup-a", Status: session.StatusActive, ExpiresAt: &future,
		CharacterID: char, LocationID: loc, PlayerID: ownedPlayerID,
	}
	dupB := &session.Info{
		ID: "sess-dup-b", Status: session.StatusActive, ExpiresAt: &future,
		CharacterID: char, LocationID: loc, PlayerID: ownedPlayerID,
	}

	// Caller session — needed for ownership validation and session lookup.
	caller := mkActiveAt("sess-caller", char, loc)

	grant := policytest.NewGrantEngine()
	grant.Grant(access.CharacterSubject(char.String()), "list_presence", access.LocationResource(loc.String()))

	mockSessStore := sessionmocks.NewMockStore(t)
	// Get is called twice: once for ownership validation (ValidateSessionOwnership)
	// and once for the session re-fetch in the handler body.
	mockSessStore.EXPECT().Get(mock.Anything, "sess-caller").Return(caller, nil).Times(2)
	// ListActiveByLocation returns two active sessions with the same CharacterID.
	mockSessStore.EXPECT().ListActiveByLocation(mock.Anything, loc).
		Return([]*session.Info{dupA, dupB}, nil).Once()

	s := &CoreServer{
		sessionStore:          mockSessStore,
		playerSessionRepo:     newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:          grant,
		characterNameResolver: &stubNameResolver{names: map[ulid.ULID]string{char: "alice"}},
	}
	resp, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-caller", PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Entries, 1, "duplicate character_id must collapse to single presence entry")
	assert.Equal(t, "alice", resp.Entries[0].CharacterName)
}

// Verifies: INV-PRESENCE-1
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

// Verifies: INV-PRESENCE-7
// PresenceEntry wire shape must contain exactly 3 fields: character_id,
// character_name, state. No timestamps or duration-of-presence fields.
// Uses proto reflection to assert field count stays exactly 3 — any
// proto addition that would leak timing data fails this test.
func TestPresenceEntryHasExactlyThreeFields(t *testing.T) {
	entry := &corev1.PresenceEntry{}
	fields := entry.ProtoReflect().Descriptor().Fields()
	const wantFields = 3
	if fields.Len() != wantFields {
		names := make([]string, fields.Len())
		for i := range fields.Len() {
			names[i] = string(fields.Get(i).Name())
		}
		t.Errorf("INV-PRESENCE-7: PresenceEntry MUST have exactly %d fields (character_id, character_name, state); got %d: %v",
			wantFields, fields.Len(), names)
	}
}

// Verifies: INV-PRESENCE-1
// Verifies: I-LIVENESS-PRES-1
// A session that is active but has grid_present=false (e.g. only a
// comms_hub connection, no terminal/telnet) MUST NOT appear in the
// location roster.
func TestListFocusPresenceExcludesActiveButNotGridPresent(t *testing.T) {
	char := ulid.Make()
	commsChar := ulid.Make()
	loc := ulid.Make()
	// caller session: active AND grid-present — appears in the roster.
	caller := mkActiveAt("sess-caller", char, loc)
	// comms-only session: active but NOT grid-present — must be excluded.
	// commsChar IS resolvable so the name-resolver skip path does not mask
	// the failure; only the grid_present filter must exclude it.
	commsOnly := mkActiveAt("sess-comms", commsChar, loc)
	commsOnly.GridPresent = false

	grant := policytest.NewGrantEngine()
	grant.Grant(access.CharacterSubject(char.String()), "list_presence", access.LocationResource(loc.String()))

	s := &CoreServer{
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			"sess-caller": caller,
			"sess-comms":  commsOnly,
		}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
		accessEngine:      grant,
		characterNameResolver: &stubNameResolver{names: map[ulid.ULID]string{
			char:      "alice",
			commsChar: "bob-comms", // resolvable: ensures exclusion is by grid_present filter, not name-resolver skip
		}},
	}
	resp, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId: "sess-caller", PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Len(t, resp.Entries, 1, "active-but-not-grid-present session must be excluded from presence roster")
	assert.Equal(t, "alice", resp.Entries[0].CharacterName)
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
