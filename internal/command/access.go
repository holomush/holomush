// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"errors"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/pkg/errutil"
)

// ErrCapabilityCheckFailed is a sentinel for infrastructure failures in capability checks.
// Callers can use errors.Is(err, ErrCapabilityCheckFailed) to detect infra failures
// as distinct from policy denials (CodePermissionDenied).
var ErrCapabilityCheckFailed = errors.New("capability check failed")

// CheckCapability evaluates whether a subject can execute a given capability
// using the ABAC policy engine. It handles request construction errors, engine
// evaluation errors, infrastructure failures, and permission denial with
// consistent logging, metrics, and error codes.
//
// Returned errors carry oops codes and context:
//   - CodeAccessEvaluationFailed with command, capability: request construction
//     failed, engine returned an error, or the engine denied due to infrastructure failure
//   - CodePermissionDenied with command, capability, reason, policy_id: the engine
//     denied access based on policy
func CheckCapability(ctx context.Context, engine types.AccessPolicyEngine, subject, capability, cmdName string) error {
	req, reqErr := types.NewAccessRequest(subject, "execute", capability)
	if reqErr != nil {
		errutil.LogErrorContext(ctx, cmdName+" access request construction failed",
			reqErr,
			"subject", subject,
			"action", "execute",
			"resource", capability,
		)
		observability.RecordEngineFailure(cmdName + "_access_check")
		return oops.Code(CodeAccessEvaluationFailed).
			With("command", cmdName).
			With("capability", capability).
			Wrap(errors.Join(ErrCapabilityCheckFailed, reqErr))
	}

	decision, evalErr := engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, cmdName+" access evaluation failed",
			evalErr,
			"subject", subject,
			"action", "execute",
			"resource", capability,
		)
		observability.RecordEngineFailure(cmdName + "_access_check")
		return oops.Code(CodeAccessEvaluationFailed).
			With("command", cmdName).
			With("capability", capability).
			Wrap(errors.Join(ErrCapabilityCheckFailed, evalErr))
	}

	if !decision.IsAllowed() {
		if decision.IsInfraFailure() {
			slog.ErrorContext(ctx, cmdName+" access check infrastructure failure",
				"subject", subject,
				"action", "execute",
				"resource", capability,
				"reason", decision.Reason(),
				"policy_id", decision.PolicyID(),
			)
			observability.RecordEngineFailure(cmdName + "_access_check")
			return oops.Code(CodeAccessEvaluationFailed).
				With("command", cmdName).
				With("capability", capability).
				With("reason", decision.Reason()).
				With("policy_id", decision.PolicyID()).
				Wrap(ErrCapabilityCheckFailed)
		}
		slog.DebugContext(ctx, cmdName+" permission denied",
			"subject", subject,
			"capability", capability,
			"reason", decision.Reason(),
			"policy_id", decision.PolicyID(),
		)
		return oops.Code(CodePermissionDenied).
			With("command", cmdName).
			With("capability", capability).
			With("reason", decision.Reason()).
			With("policy_id", decision.PolicyID()).
			Errorf("permission denied for command %s", cmdName)
	}

	return nil
}
