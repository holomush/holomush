// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChannelCreatesValidPublicChannel(t *testing.T) {
	ch, err := newChannel("General", channelTypePublic, "General discussion", "owner-1")
	require.NoError(t, err)
	assert.Equal(t, "General", ch.Name)
	assert.Equal(t, channelTypePublic, ch.Type)
	assert.Equal(t, "General discussion", ch.Description)
	assert.Equal(t, "owner-1", ch.OwnerID)
	assert.NotEmpty(t, ch.ID)
	assert.NotZero(t, ch.CreatedAt)
	assert.Nil(t, ch.ArchivedAt)
}

func TestValidateChannel(t *testing.T) {
	t.Run("accepts valid channel names", func(t *testing.T) {
		names := []string{"Public", "a", "test-channel", "RP_General", "a1234567890123456789012345678901"}
		for _, name := range names {
			ch, err := newChannel(name, channelTypePublic, "", "owner-1")
			require.NoError(t, err, "name=%q", name)
			assert.Equal(t, name, ch.Name)
		}
	})

	t.Run("rejects invalid channel names", func(t *testing.T) {
		cases := []struct {
			name  string
			input string
		}{
			{"empty name", ""},
			{"starts with hyphen", "-bad"},
			{"starts with underscore", "_bad"},
			{"contains spaces", "has space"},
			{"too long", "a12345678901234567890123456789012"},
			{"special characters", "bad@name"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := newChannel(tc.input, channelTypePublic, "", "owner-1")
				require.Error(t, err)
			})
		}
	})

	t.Run("rejects invalid channel type", func(t *testing.T) {
		_, err := newChannel("test", channelType("bogus"), "", "owner-1")
		require.Error(t, err)
	})

	t.Run("rejects empty owner ID", func(t *testing.T) {
		_, err := newChannel("test", channelTypePublic, "", "")
		require.Error(t, err)
	})
}

func TestChannelIsArchived(t *testing.T) {
	ch, err := newChannel("test", channelTypePublic, "", "owner-1")
	require.NoError(t, err)
	assert.False(t, ch.isArchived())
}

func TestChannelStreamName(t *testing.T) {
	ch, err := newChannel("test", channelTypePublic, "", "owner-1")
	require.NoError(t, err)
	assert.Equal(t, "channel:"+ch.ID, ch.streamName())
}

func TestMembershipIsMuted(t *testing.T) {
	m := &membershipRow{Role: roleMember}

	t.Run("returns false when not muted", func(t *testing.T) {
		assert.False(t, m.isMuted())
	})

	t.Run("returns true when muted until future", func(t *testing.T) {
		future := time.Now().Add(time.Hour)
		m.MutedUntil = &future
		assert.True(t, m.isMuted())
	})

	t.Run("returns false when mute has expired", func(t *testing.T) {
		past := time.Now().Add(-time.Hour)
		m.MutedUntil = &past
		assert.False(t, m.isMuted())
	})
}
