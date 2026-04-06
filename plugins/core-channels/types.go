// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"time"

	"github.com/oklog/ulid/v2"
)

// channelType represents the access model for a channel.
type channelType string

const (
	channelTypePublic  channelType = "public"
	channelTypePrivate channelType = "private"
	channelTypeAdmin   channelType = "admin"
)

var validChannelTypes = map[channelType]bool{
	channelTypePublic:  true,
	channelTypePrivate: true,
	channelTypeAdmin:   true,
}

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`)

const (
	maxNameLength           = 32
	defaultMaxMessageLength = 4096
	defaultHistoryCount     = 20
	maxHistoryCount         = 500
	maxMemberships          = 50
)

// Member roles.
const (
	roleMember = "member"
	roleOp     = "op"
	roleOwner  = "owner"
)

// channelRow represents a channel record in the database.
type channelRow struct {
	ID          string
	Name        string
	Type        channelType
	Description string
	OwnerID     string
	CreatedAt   time.Time
	ArchivedAt  *time.Time
}

func newChannel(name string, ct channelType, description, ownerID string) (*channelRow, error) {
	ch := &channelRow{
		ID:          ulid.MustNew(ulid.Now(), rand.Reader).String(),
		Name:        name,
		Type:        ct,
		Description: description,
		OwnerID:     ownerID,
		CreatedAt:   time.Now().UTC(),
	}
	if err := ch.validate(); err != nil {
		return nil, err
	}
	return ch, nil
}

func (c *channelRow) validate() error {
	if c.Name == "" {
		return fmt.Errorf("channel name is required")
	}
	if !namePattern.MatchString(c.Name) {
		return fmt.Errorf("channel name must match pattern: starts with alphanumeric, 1-%d chars", maxNameLength)
	}
	if !validChannelTypes[c.Type] {
		return fmt.Errorf("invalid channel type %q", c.Type)
	}
	if c.OwnerID == "" {
		return fmt.Errorf("owner ID is required")
	}
	return nil
}

func (c *channelRow) isArchived() bool {
	return c.ArchivedAt != nil
}

// streamName returns the event stream for this channel.
func (c *channelRow) streamName() string {
	return "channel:" + c.ID
}

// membershipRow represents a player's membership in a channel.
type membershipRow struct {
	ChannelID  string
	PlayerID   string
	Role       string
	JoinedAt   time.Time
	MutedUntil *time.Time
	Banned     bool
}

func (m *membershipRow) isMuted() bool {
	if m.MutedUntil == nil {
		return false
	}
	return m.MutedUntil.After(time.Now())
}

// gagRow represents a per-character channel gag.
type gagRow struct {
	ChannelID   string
	CharacterID string
	Gagged      bool
}

// messageRow represents a stored channel message for history.
type messageRow struct {
	ID         string
	ChannelID  string
	AuthorID   string
	AuthorName string
	Message    string
	EventType  string
	Source     string
	CreatedAt  time.Time
}
