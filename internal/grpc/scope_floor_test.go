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
		{"location current — non-guest", &session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: arrival},
			"location:" + locID.String(), arrival},
		{"location current — guest, arrival later than guest_created",
			&session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: arrival, IsGuest: true, GuestCharacterCreatedAt: guestCreated},
			"location:" + locID.String(), arrival},
		{"location current — guest, guest_created later than arrival (shouldn't happen but MAX wins)",
			&session.Info{CharacterID: charID, LocationID: locID, LocationArrivedAt: time.Time{}, IsGuest: true, GuestCharacterCreatedAt: guestCreated},
			"location:" + locID.String(), guestCreated},
		{"scene member — non-guest, uses JoinedAt",
			&session.Info{CharacterID: charID, FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: sceneJoin},
			}}, "scene:" + sceneID.String() + ":ic", sceneJoin},
		{"character own stream — no floor", &session.Info{CharacterID: charID}, "character:" + charID.String(), time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := streamScopeFloor(tc.info, tc.stream)
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
