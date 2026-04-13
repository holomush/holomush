// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
)

// RestoreFocus derives the ordered stream-plus-mode list for the initial
// replay pass. Pure derivation from session.Info (invariant I-8).
func (c *defaultCoordinator) RestoreFocus(ctx context.Context, sessionID string) (RestorePlan, error) {
	info, err := c.sessionOrError(ctx, sessionID)
	if err != nil {
		return RestorePlan{}, err
	}

	plan := RestorePlan{}

	for _, m := range info.FocusMemberships {
		policy, pErr := c.policyFor(m.Kind)
		if pErr != nil {
			continue
		}
		pctx := c.buildPolicyContext(ctx, info, session.FocusKey{Kind: m.Kind, TargetID: m.TargetID})
		streams, rErr := policy.OnRestore(pctx)
		if rErr != nil {
			continue
		}
		plan.Streams = append(plan.Streams, streams...)
	}

	// Ambient streams: character, location, plugin-contributed.
	// Mode depends on whether this is initial attach (no memberships +
	// all cursors zero) or reconnect (detached or has cursors).
	ambientMode := ReplayModeLiveOnly
	if info.Status == session.StatusDetached || len(info.EventCursors) > 0 {
		ambientMode = ReplayModeFromCursor
	}

	if !info.CharacterID.IsZero() {
		plan.Streams = append(plan.Streams, StreamWithMode{
			Stream: world.CharacterStream(info.CharacterID),
			Mode:   ambientMode,
		})
	}
	if !info.LocationID.IsZero() {
		plan.Streams = append(plan.Streams, StreamWithMode{
			Stream: world.LocationStream(info.LocationID),
			Mode:   ambientMode,
		})
	}

	// Plugin-contributed streams.
	if c.streamContributor != nil {
		playerID := ""
		if !info.PlayerID.IsZero() {
			playerID = info.PlayerID.String()
		}
		pluginStreams := c.streamContributor.QuerySessionStreams(ctx, StreamContributorRequest{
			CharacterID: info.CharacterID.String(),
			PlayerID:    playerID,
			SessionID:   info.ID,
		})
		seen := make(map[string]bool, len(plan.Streams))
		for _, sm := range plan.Streams {
			seen[sm.Stream] = true
		}
		for _, ps := range pluginStreams {
			if seen[ps] {
				continue
			}
			plan.Streams = append(plan.Streams, StreamWithMode{
				Stream: ps,
				Mode:   ambientMode,
			})
			seen[ps] = true
		}
	}

	if info.PresentingFocus != nil {
		if policy, pErr := c.policyFor(info.PresentingFocus.Kind); pErr == nil {
			streams := policy.StreamsFor(*info.PresentingFocus)
			if len(streams) > 0 {
				plan.PresentingStream = streams[0]
			}
		}
	}

	return plan, nil
}
