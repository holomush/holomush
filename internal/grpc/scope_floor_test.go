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
			"location:" + locID.String(), arrival,
		},
		{
			"location current — guest, arrival later than guest_created",
			&session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: arrival, IsGuest: true, GuestCharacterCreatedAt: guestCreated},
			"location:" + locID.String(), arrival,
		},
		{
			"location current — guest, guest_created later than arrival (shouldn't happen but MAX wins)",
			&session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: time.Time{}, IsGuest: true, GuestCharacterCreatedAt: guestCreated},
			"location:" + locID.String(), guestCreated,
		},
		{
			"scene member — non-guest, uses JoinedAt",
			&session.Info{CharacterID: charID, FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: sceneJoin},
			}}, "scene:" + sceneID.String() + ":ic", sceneJoin,
		},
		{"character own stream — no floor", &session.Info{CharacterID: charID}, "character:" + charID.String(), time.Time{}},
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
// The helper takes legacy stream-name format ("location:X", "scene:Y:ic",
// "character:Z"); production callers pass NATS subjects today, so these tests
// exercise the function as designed independent of that pending format-gap.
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
	locStream := "location:" + locID.String()
	sceneStream := "scene:" + sceneID.String() + ":ic"
	charStream := "character:" + charID.String()

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
			[]string{"global", "events.gid.location.unknown", locStream},
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
	assert.True(t, isLocationStream("location:01H000000000000000000000A1"))
	assert.False(t, isLocationStream("scene:01H000000000000000000000S1:ic"))
	assert.False(t, isLocationStream("character:01H000000000000000000000C1"))
	assert.False(t, isLocationStream("global"))
}
