// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// LeaveFocus removes a focus membership. Clears PresentingFocus if it
// pointed at the removed membership (I-10). Idempotent on not-a-member.
func (c *defaultCoordinator) LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error {
	if _, err := c.sessionOrError(ctx, sessionID); err != nil {
		return err
	}

	var streamsToRemove []string
	if policy, err := c.policyFor(target.Kind); err == nil {
		streamsToRemove = policy.StreamsFor(target)
	}

	var wasMember bool
	mutErr := c.sessionStore.UpdateFocusMemberships(ctx, sessionID, session.NewFocusMutator(
		func(current []session.FocusMembership, presenting *session.FocusKey) ([]session.FocusMembership, *session.FocusKey, error) {
			next := make([]session.FocusMembership, 0, len(current))
			for _, m := range current {
				if m.Kind == target.Kind && m.TargetID == target.TargetID {
					wasMember = true
					continue
				}
				next = append(next, m)
			}
			nextPresenting := presenting
			if presenting != nil && presenting.Kind == target.Kind && presenting.TargetID == target.TargetID {
				nextPresenting = nil
			}
			return next, nextPresenting, nil
		},
	))
	if mutErr != nil {
		return oops.With("session_id", sessionID).Wrap(mutErr)
	}

	if wasMember && c.streamSender != nil {
		for _, stream := range streamsToRemove {
			_ = c.streamSender.Send(sessionID, stream, false, ReplayModeFromCursor) //nolint:errcheck // best-effort: SESSION_NOT_FOUND means no live subscriber
		}
	}

	return nil
}
