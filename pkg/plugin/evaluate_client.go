// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"github.com/samber/oops"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// EvaluateDecision holds the result of an access-control evaluation
// performed by the host on behalf of a plugin.
type EvaluateDecision struct {
	// Allowed is true when the host granted the requested action.
	Allowed bool
	// Reason is a human-readable explanation from the host.
	Reason string
	// MatchedPolicy is the policy identifier that produced the decision, if any.
	MatchedPolicy string
}

// HostEvaluator is the interface exposed to binary plugins for performing
// access-control checks against the host. The host derives the subject
// server-side; the plugin supplies only the action and resource.
type HostEvaluator interface {
	// Evaluate asks the host whether the current plugin subject may perform
	// action on resource. A nil client fails closed (returns an error and
	// EvaluateDecision{Allowed: false}).
	Evaluate(ctx context.Context, action, resource string) (EvaluateDecision, error)
}

// hostEvaluateClient is the concrete HostEvaluator used by binary plugins.
// It wraps the generated PluginHostServiceClient and forwards Evaluate calls.
type hostEvaluateClient struct {
	client pluginv1.PluginHostServiceClient
}

// Evaluate implements HostEvaluator. A nil client fails closed.
func (c *hostEvaluateClient) Evaluate(ctx context.Context, action, resource string) (EvaluateDecision, error) {
	if c.client == nil {
		return EvaluateDecision{}, oops.New("host evaluate client is not configured")
	}

	resp, err := c.client.Evaluate(ctx, &pluginv1.PluginHostServiceEvaluateRequest{
		Action:   action,
		Resource: resource,
	})
	if err != nil {
		return EvaluateDecision{}, oops.With("action", action, "resource", resource).Wrap(err)
	}

	return EvaluateDecision{
		Allowed:       resp.GetAllowed(),
		Reason:        resp.GetReason(),
		MatchedPolicy: resp.GetMatchedPolicy(),
	}, nil
}
