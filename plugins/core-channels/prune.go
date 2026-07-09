// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
)

// channelPruneInfo is the projection ListChannelsForPrune returns for retention
// computation: a channel's id, type, and per-channel retention override
// (nullable — NULL defers to the plugin config default, or is unlimited for an
// admin channel).
type channelPruneInfo struct {
	ID            string
	Type          string
	RetentionDays *int
}

// channelPruneStore is the persistence surface the retention sweep needs:
// enumerate channels for retention computation, and delete channel_log rows
// older than a per-channel cutoff.
type channelPruneStore interface {
	ListChannelsForPrune(ctx context.Context) ([]channelPruneInfo, error)
	DeleteChannelLogOlderThan(ctx context.Context, subject string, cutoff time.Time) (int64, error)
}

// channelPruner is the background retention sweep (D-07): it periodically
// deletes channel_log rows older than each channel's effective retention window.
// It mirrors core-scenes' publishScheduler — a ticker loop keyed off interval
// with an injectable clock (now) for deterministic boundary tests.
type channelPruner struct {
	store         channelPruneStore
	gameID        string
	defaultWindow time.Duration // config retention_window; applied when retention_days is NULL (non-admin)
	interval      time.Duration // config prune_interval; the ticker period
	now           func() time.Time
}

// Run starts the sweep loop. It ticks at p.interval and runs one sweep per tick;
// the loop exits when ctx is cancelled (plugin shutdown / process exit). Mirrors
// publishScheduler.Run.
func (p *channelPruner) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.sweep(ctx); err != nil {
				errutil.LogErrorContext(ctx, "channel retention prune sweep failed", err)
			}
		}
	}
}

// sweep is one retention pass over all channels. For each channel it computes
// the effective retention window and deletes channel_log rows older than
// now - window. A channel with unlimited retention (an admin channel whose
// retention_days is NULL) is skipped. A per-channel delete failure is
// WARN-logged and the sweep continues — one channel's error MUST NOT abort the
// batch (mirrors publishScheduler.sweep). Only the initial channel enumeration
// failure is returned.
func (p *channelPruner) sweep(ctx context.Context) error {
	infos, err := p.store.ListChannelsForPrune(ctx)
	if err != nil {
		return oops.Code("CHANNEL_PRUNE_LIST_FAILED").Wrap(err)
	}

	now := p.now().UTC()
	for _, info := range infos {
		window, prune := effectiveRetention(info, p.defaultWindow)
		if !prune {
			continue // unlimited retention — never pruned
		}
		cutoff := now.Add(-window)
		subject := dotStyleChannelSubject(p.gameID, info.ID)
		deleted, delErr := p.store.DeleteChannelLogOlderThan(ctx, subject, cutoff)
		if delErr != nil {
			slog.WarnContext(ctx, "channel prune: delete failed",
				"channel_id", info.ID, "subject", subject, "err", delErr)
			continue
		}
		if deleted > 0 {
			slog.InfoContext(ctx, "channel prune: swept expired history",
				"channel_id", info.ID, "deleted", deleted)
		}
	}
	return nil
}

// effectiveRetention computes a channel's retention window and whether it should
// be pruned at all. A positive per-channel retention_days override always wins
// (window = days). When retention_days is NULL, an admin channel is treated as
// unlimited (never pruned — staff-oversight history is retained), while every
// other channel type falls back to the plugin config default window. Reconciles
// the schema comment ("NULL = config default; admin channels MAY be unlimited")
// with D-07.
func effectiveRetention(info channelPruneInfo, defaultWindow time.Duration) (window time.Duration, prune bool) {
	if info.RetentionDays != nil && *info.RetentionDays > 0 {
		return time.Duration(*info.RetentionDays) * 24 * time.Hour, true
	}
	if info.Type == string(channelTypeAdmin) {
		return 0, false // admin default: unlimited retention
	}
	return defaultWindow, true
}
