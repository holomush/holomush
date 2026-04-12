// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// PresentFocus updates PresentingFocus to the specified target. The target
// MUST already exist in FocusMemberships. No replay, no subscription change.
func (c *defaultCoordinator) PresentFocus(ctx context.Context, sessionID string, target session.FocusKey) error {
	if _, err := c.sessionOrError(ctx, sessionID); err != nil {
		return err
	}

	return c.sessionStore.UpdateFocusMemberships(ctx, sessionID, session.NewFocusMutator(
		func(current []session.FocusMembership, _ *session.FocusKey) ([]session.FocusMembership, *session.FocusKey, error) {
			found := false
			for _, m := range current {
				if m.Kind == target.Kind && m.TargetID == target.TargetID {
					found = true
					break
				}
			}
			if !found {
				return nil, nil, oops.Code("FOCUS_NOT_MEMBER").
					Errorf("target %s:%s is not in FocusMemberships", target.Kind, target.TargetID)
			}
			return current, &target, nil
		},
	))
}
