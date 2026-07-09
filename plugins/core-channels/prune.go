// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"time"
)

// channelPruneInfo is the projection ListChannelsForPrune returns for retention
// computation: a channel's id, type, and per-channel retention override.
type channelPruneInfo struct {
	ID            string
	Type          string
	RetentionDays *int
}

// channelPruneStore is the persistence surface the retention sweep needs.
type channelPruneStore interface {
	ListChannelsForPrune(ctx context.Context) ([]channelPruneInfo, error)
	DeleteChannelLogOlderThan(ctx context.Context, subject string, cutoff time.Time) (int64, error)
}

// channelPruner is the background retention sweep. SKELETON — filled in the
// GREEN commit.
type channelPruner struct {
	store         channelPruneStore
	gameID        string
	defaultWindow time.Duration
	interval      time.Duration
	now           func() time.Time
}

// Run is the ticker loop. SKELETON.
func (p *channelPruner) Run(_ context.Context) {}

// sweep is one retention pass. SKELETON — does no work yet.
func (p *channelPruner) sweep(_ context.Context) error {
	return nil
}
