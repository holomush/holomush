// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/session"
)

func TestIsPrivateStreamReturnsTrueForSceneStreams(t *testing.T) {
	assert.True(t, isPrivateStream("scene:01ABC:ic"))
	assert.True(t, isPrivateStream("scene:01ABC:ooc"))
}

func TestIsPrivateStreamReturnsTrueForCharacterStreams(t *testing.T) {
	assert.True(t, isPrivateStream("character:01ABC"))
}

func TestIsPrivateStreamReturnsFalseForLocationStreams(t *testing.T) {
	assert.False(t, isPrivateStream("location:01ABC"))
}

func TestIsPrivateStreamReturnsFalseForUnknownStreams(t *testing.T) {
	assert.False(t, isPrivateStream("global"))
	assert.False(t, isPrivateStream(""))
}

func TestSessionHasMembershipPermitsOwnCharacterStream(t *testing.T) {
	charID := ulid.Make()
	info := &session.Info{CharacterID: charID}
	assert.True(t, sessionHasMembership(info, "character:"+charID.String()))
}

func TestSessionHasMembershipDeniesOtherCharacterStream(t *testing.T) {
	info := &session.Info{CharacterID: ulid.Make()}
	assert.False(t, sessionHasMembership(info, "character:"+ulid.Make().String()))
}

func TestSessionHasMembershipPermitsSceneStreamWithFocusMembership(t *testing.T) {
	targetID := ulid.Make()
	info := &session.Info{
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: targetID},
		},
	}
	assert.True(t, sessionHasMembership(info, "scene:"+targetID.String()+":ic"))
	assert.True(t, sessionHasMembership(info, "scene:"+targetID.String()+":ooc"))
}

func TestSessionHasMembershipDeniesSceneStreamWithoutMembership(t *testing.T) {
	info := &session.Info{
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: ulid.Make()},
		},
	}
	assert.False(t, sessionHasMembership(info, "scene:"+ulid.Make().String()+":ic"))
}

func TestSessionHasMembershipDeniesSceneStreamWithEmptyMemberships(t *testing.T) {
	info := &session.Info{}
	assert.False(t, sessionHasMembership(info, "scene:"+ulid.Make().String()+":ic"))
}

func TestSessionHasMembershipDeniesMalformedSceneStream(t *testing.T) {
	info := &session.Info{
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: ulid.Make()},
		},
	}
	// Malformed ULID in stream name — must not match any membership
	assert.False(t, sessionHasMembership(info, "scene:not-a-ulid:ic"))
}

func TestSessionHasMembershipDeniesNilInfo(t *testing.T) {
	assert.False(t, sessionHasMembership(nil, "character:"+ulid.Make().String()))
	assert.False(t, sessionHasMembership(nil, "scene:"+ulid.Make().String()+":ic"))
}

func TestStreamToFocusKeyParsesSceneIC(t *testing.T) {
	id := ulid.Make()
	fk, err := streamToFocusKey("scene:" + id.String() + ":ic")
	assert.NoError(t, err)
	assert.NotNil(t, fk)
	assert.Equal(t, session.FocusKindScene, fk.Kind)
	assert.Equal(t, id, fk.TargetID)
}

func TestStreamToFocusKeyParsesSceneOOC(t *testing.T) {
	id := ulid.Make()
	fk, err := streamToFocusKey("scene:" + id.String() + ":ooc")
	assert.NoError(t, err)
	assert.NotNil(t, fk)
	assert.Equal(t, session.FocusKindScene, fk.Kind)
	assert.Equal(t, id, fk.TargetID)
}

func TestStreamToFocusKeyRejectsNonSceneStream(t *testing.T) {
	_, err := streamToFocusKey("location:01ABC")
	assert.Error(t, err)
}

func TestStreamToFocusKeyRejectsMalformedULID(t *testing.T) {
	_, err := streamToFocusKey("scene:not-a-ulid:ic")
	assert.Error(t, err)
}

func TestStreamToFocusKeyRejectsMissingSuffix(t *testing.T) {
	id := ulid.Make()
	_, err := streamToFocusKey("scene:" + id.String())
	assert.Error(t, err)
}

func TestSessionHasMembershipDeniesWhenCharacterIDIsZero(t *testing.T) {
	// Session with zero CharacterID must not match any character stream,
	// including "character:00000000000000000000000000".
	info := &session.Info{CharacterID: ulid.ULID{}}
	zeroID := ulid.ULID{}
	assert.False(t, sessionHasMembership(info, "character:"+zeroID.String()))
}

func TestSessionHasMembershipDeniesSceneWhenMembershipTargetIDIsZero(t *testing.T) {
	// FocusMembership with zero TargetID must not match a zero-TargetID scene stream.
	zeroID := ulid.ULID{}
	info := &session.Info{
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: zeroID},
		},
	}
	assert.False(t, sessionHasMembership(info, "scene:"+zeroID.String()+":ic"))
}

func TestStreamToFocusKeyRejectsUnknownSuffix(t *testing.T) {
	id := ulid.Make()
	_, err := streamToFocusKey("scene:" + id.String() + ":evil")
	assert.Error(t, err)
}

func TestStreamToFocusKeyRejectsEmptySuffix(t *testing.T) {
	id := ulid.Make()
	_, err := streamToFocusKey("scene:" + id.String() + ":")
	assert.Error(t, err)
}
