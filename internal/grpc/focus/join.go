// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// JoinFocus adds a new focus membership to the session and applies the
// kind-specific join policy.
func (c *defaultCoordinator) JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error {
	info, err := c.sessionOrError(ctx, sessionID)
	if err != nil {
		return err
	}

	policy, err := c.policyFor(target.Kind)
	if err != nil {
		return err
	}

	pctx := c.buildPolicyContext(ctx, info, target)

	joinStreams, err := policy.OnJoin(pctx)
	if err != nil {
		return oops.Code("FOCUS_POLICY_FAILED").With("kind", string(target.Kind)).Wrap(err)
	}

	now := time.Now().UTC()
	mutErr := c.sessionStore.UpdateFocusMemberships(ctx, sessionID, session.NewFocusMutator(
		func(current []session.FocusMembership, presenting *session.FocusKey) ([]session.FocusMembership, *session.FocusKey, error) {
			for _, m := range current {
				if m.Kind == target.Kind && m.TargetID == target.TargetID {
					return nil, nil, oops.Code("FOCUS_ALREADY_MEMBER").
						Errorf("session %s already has membership for %s:%s", sessionID, target.Kind, target.TargetID)
				}
			}
			next := append(current, session.FocusMembership{Kind: target.Kind, TargetID: target.TargetID, JoinedAt: now})
			return next, presenting, nil
		},
	))
	if mutErr != nil {
		return mutErr
	}

	if c.streamSender != nil {
		for _, sw := range joinStreams {
			_ = c.streamSender.Send(sessionID, sw.Stream, true, sw.Mode)
		}
	}

	return nil
}
