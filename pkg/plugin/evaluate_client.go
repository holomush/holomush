// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"github.com/samber/oops"
	"google.golang.org/grpc"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
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

// HostEvaluatorAware is the optional interface service providers implement to
// receive a HostEvaluator during Init, parallel to FocusClientAware and
// EventSinkAware. Implement this on the plugin struct to get the host ABAC
// evaluator injected before Init is called.
type HostEvaluatorAware interface {
	SetHostEvaluator(HostEvaluator)
}

// hostEvaluateClient is the concrete HostEvaluator used by binary plugins.
// It wraps the generated EvalServiceClient and forwards Evaluate calls.
type hostEvaluateClient struct {
	client hostv1.EvalServiceClient
}

// newHostEvaluateClient constructs a HostEvaluator from a broker gRPC
// connection. Exposed to the adapter for wiring; test code constructs a
// hostEvaluateClient directly.
func newHostEvaluateClient(conn grpc.ClientConnInterface) HostEvaluator {
	return &hostEvaluateClient{client: hostv1.NewEvalServiceClient(conn)}
}

// Evaluate implements HostEvaluator. A nil client fails closed.
func (c *hostEvaluateClient) Evaluate(ctx context.Context, action, resource string) (EvaluateDecision, error) {
	if c.client == nil {
		return EvaluateDecision{}, oops.New("host evaluate client is not configured")
	}

	// Ferry the host-issued per-dispatch token from the incoming command
	// context to the outgoing Evaluate RPC so the host can recover the acting
	// subject server-side — the identical mechanism EmitEvent uses (see
	// emitTokenHeader in event_sink.go). Without this the host rejects the call
	// with EMIT_TOKEN_MISSING ("plugin evaluated without a host-issued dispatch
	// token"). Evaluate is always command-gated, so the token is present on the
	// incoming context; the self-token fallback used by plugin-initiated
	// EmitEvent is not needed here. Uses the shared withDispatchToken helper
	// (settings_client.go) so the ferry logic lives in one place (sl0ir.16).
	resp, err := c.client.Evaluate(withDispatchToken(ctx), &hostv1.EvaluateRequest{
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
