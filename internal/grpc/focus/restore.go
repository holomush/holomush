// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"

	"github.com/holomush/holomush/internal/session"
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
