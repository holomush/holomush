// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/session"
)

// dotStyleLocation returns a NATS dot-style location subject for testing,
// using the fixed game ID "test".
func dotStyleLocation(locID string) string {
	return "events.test.location." + locID
}

func TestStreamScopeFloor(t *testing.T) {
	locID := ulid.MustParse("01H000000000000000000000A1")
	charID := ulid.MustParse("01H000000000000000000000C1")
	sceneID := ulid.MustParse("01H000000000000000000000S1")
	arrival := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	sceneJoin := time.Date(2026, 5, 17, 11, 0, 0, 0, time.UTC)
	guestCreated := time.Date(2026, 5, 17, 9, 30, 0, 0, time.UTC)

	cases := []struct {
		name   string
		info   *session.Info
		stream string
		want   time.Time
	}{
		{
			"location current — non-guest", &session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: arrival},
			dotStyleLocation(locID.String()), arrival,
		},
		{
			"location current — guest, arrival later than guest_created",
			&session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: arrival, IsGuest: true, GuestCharacterCreatedAt: guestCreated},
			dotStyleLocation(locID.String()), arrival,
		},
		{
			"location current — guest, guest_created later than arrival (shouldn't happen but MAX wins)",
			&session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: time.Time{}, IsGuest: true, GuestCharacterCreatedAt: guestCreated},
			dotStyleLocation(locID.String()), guestCreated,
		},
		{
			"scene member — non-guest, uses JoinedAt (dot-style IC subject)",
			&session.Info{CharacterID: charID, FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: sceneJoin},
			}}, dotStyleSceneIC(sceneID.String()), sceneJoin,
		},
		{"character own stream — no floor", &session.Info{CharacterID: charID}, dotStyleCharacter(charID.String()), time.Time{}},
		{"legacy colon location — no longer matched, falls to zero", &session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: arrival}, "location:" + locID.String(), time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := streamScopeFloor(tc.info, tc.stream)
			assert.True(t, got.Equal(tc.want), "got %v want %v", got, tc.want)
		})
	}
}

// TestMaxStreamScopeFloor covers the Subscribe-path MAX aggregation used by
// CoreServer.Subscribe to compute the minFloor it forwards to OpenSession.
// All subjects use NATS dot-style: scene per Phase 4 / INV-P4-1 / ADR
// holomush-s9nu, location and character per holomush-rops (the legacy colon
// forms are no longer recognized).
func TestMaxStreamScopeFloor(t *testing.T) {
	locID := ulid.MustParse("01H000000000000000000000A1")
	sceneID := ulid.MustParse("01H000000000000000000000S1")
	charID := ulid.MustParse("01H000000000000000000000C1")
	arrival := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	sceneJoin := time.Date(2026, 5, 17, 11, 0, 0, 0, time.UTC)

	info := &session.Info{
		CharacterID:       charID,
		LocationID:        locID,
		LocationArrivedAt: arrival,
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: sceneJoin},
		},
	}
	locStream := dotStyleLocation(locID.String())
	sceneStream := dotStyleSceneIC(sceneID.String())
	charStream := dotStyleCharacter(charID.String())

	cases := []struct {
		name    string
		filters []string
		want    time.Time
	}{
		{"empty filter set returns zero", nil, time.Time{}},
		{"only character — no floor — returns zero", []string{charStream}, time.Time{}},
		{"only location — returns LocationArrivedAt", []string{locStream}, arrival},
		{"only scene — returns FocusMembership.JoinedAt", []string{sceneStream}, sceneJoin},
		{"location + character — MAX is location", []string{locStream, charStream}, arrival},
		{"location + scene — MAX is scene (later)", []string{locStream, sceneStream}, sceneJoin},
		{"scene + location — order-independent — MAX is scene", []string{sceneStream, locStream}, sceneJoin},
		{
			"unknown subjects fall through to zero — MAX with known still wins",
			[]string{"global", "events.gid.unknowndomain.x", locStream},
			arrival,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maxStreamScopeFloor(info, tc.filters)
			assert.True(t, got.Equal(tc.want), "got %v want %v", got, tc.want)
		})
	}
}

func TestIsLocationStream(t *testing.T) {
	assert.True(t, isLocationStream(dotStyleLocation("01H000000000000000000000A1")))
	assert.False(t, isLocationStream("location:01H000000000000000000000A1"))
	assert.False(t, isLocationStream(dotStyleSceneIC("01H000000000000000000000S1")))
	assert.False(t, isLocationStream(dotStyleCharacter("01H000000000000000000000C1")))
	assert.False(t, isLocationStream("global"))
	assert.False(t, isLocationStream("events.test.location."))
}

// TestStreamScopeFloor_SceneSubjects_INV_P4_9 pins the bug-fix moment for
// the scope_floor.go format mismatch (spec §3.3). Pre-Phase-4, the scene
// branch matched the colon-style "scene:<id>:ic" form, but production
// callers pass NATS dot-style ("events.<gid>.scene.<id>.{ic,ooc}"), so
// streamScopeFloor returned time.Time{} for every real-world scene subject
// and the iwzt §6.1 temporal floor for scene streams was silently inactive.
//
// Phase 4 / T13 migrated the scene branch to dot-style. Post-migration:
//   - legacy colon-style is no longer recognized (returns time.Time{} via the
//     default branch — same fail-closed semantics as any unknown subject)
//   - dot-style IC and OOC subjects floor to FocusMembership.JoinedAt
//
// The diff between this test and the previous TestStreamScopeFloor scene
// fixture IS the audit artifact for INV-P4-9 per spec §3.3.
func TestStreamScopeFloor_SceneSubjects_INV_P4_9(t *testing.T) {
	t.Parallel()
	sceneID := ulid.Make()
	// Post-gfo6: pgnanos preserves nanosecond precision end-to-end; no truncation needed.
	joinedAt := time.Now().UTC()

	info := &session.Info{
		FocusMemberships: []session.FocusMembership{{
			Kind:     session.FocusKindScene,
			TargetID: sceneID,
			JoinedAt: joinedAt,
		}},
	}

	cases := []struct {
		name      string
		stream    string
		wantFloor time.Time
		rationale string
	}{
		{
			name:      "legacy colon-style (no longer matched after migration)",
			stream:    "scene:" + sceneID.String() + ":ic",
			wantFloor: time.Time{},
			rationale: "Phase 4 removed the colon-style scene branch — legacy form falls to time.Time{} default.",
		},
		{
			name:      "NATS dot-style IC (newly matched after migration)",
			stream:    dotStyleSceneIC(sceneID.String()),
			wantFloor: joinedAt,
			rationale: "Phase 4 added the dot-style scene branch — production-shape subjects now floor to FocusMembership.JoinedAt.",
		},
		{
			name:      "NATS dot-style OOC facet",
			stream:    dotStyleSceneOOC(sceneID.String()),
			wantFloor: joinedAt,
			rationale: "OOC facet floors to same JoinedAt as IC.",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := streamScopeFloor(info, tc.stream)
			assert.Equal(t, tc.wantFloor.UTC(), got.UTC(), tc.rationale)
		})
	}
}
