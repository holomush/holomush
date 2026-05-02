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

// CheckCommandExecution evaluates Layer 1: can the subject execute this command?
func CheckCommandExecution(ctx context.Context, engine types.AccessPolicyEngine, subject, cmdName string) error {
	req, reqErr := types.NewAccessRequest(subject, "execute", "command:"+cmdName, nil)
	if reqErr != nil {
		errutil.LogErrorContext(ctx, cmdName+" command access request failed",
			reqErr, "subject", subject, "command", cmdName)
		observability.RecordEngineFailure(cmdName + "_command_access")
		return oops.Code(CodeAccessEvaluationFailed).
			With("command", cmdName).
			Wrap(errors.Join(ErrCapabilityCheckFailed, reqErr))
	}

	decision, evalErr := engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, cmdName+" command access evaluation failed",
			evalErr, "subject", subject, "command", cmdName)
		observability.RecordEngineFailure(cmdName + "_command_access")
		return oops.Code(CodeAccessEvaluationFailed).
			With("command", cmdName).
			Wrap(errors.Join(ErrCapabilityCheckFailed, evalErr))
	}

	if !decision.IsAllowed() {
		if decision.IsInfraFailure() {
			slog.ErrorContext(ctx, cmdName+" command access infra failure",
				"subject", subject,
				"reason", decision.Reason(),
				"policy_id", decision.PolicyID())
			observability.RecordEngineFailure(cmdName + "_command_access")
			return oops.Code(CodeAccessEvaluationFailed).
				With("command", cmdName).
				With("reason", decision.Reason()).
				With("policy_id", decision.PolicyID()).
				Wrap(ErrCapabilityCheckFailed)
		}
		slog.DebugContext(ctx, cmdName+" command execution denied",
			"subject", subject,
			"reason", decision.Reason(),
			"policy_id", decision.PolicyID())
		return ErrPermissionDenied(cmdName, "execute")
	}
	return nil
}

// CheckCapabilityPreFlight evaluates Layer 2: does the subject have the class of permissions?
func CheckCapabilityPreFlight(ctx context.Context, engine types.AccessPolicyEngine, subject, cmdName string, caps []Capability) error {
	for _, capability := range caps {
		allowed, err := engine.CanPerformAction(ctx, subject, capability.Action, capability.Resource, capability.EffectiveScope())
		if err != nil {
			errutil.LogErrorContext(ctx, cmdName+" capability pre-flight error",
				err, "subject", subject, "action", capability.Action, "resource", capability.Resource)
			return oops.Code(CodeAccessEvaluationFailed).
				With("command", cmdName).
				With("action", capability.Action).
				With("resource", capability.Resource).
				Wrap(err)
		}
		if !allowed {
			slog.DebugContext(ctx, cmdName+" capability pre-flight denied",
				"subject", subject,
				"action", capability.Action,
				"resource", capability.Resource,
				"scope", capability.EffectiveScope())
			return ErrInsufficientCapability(cmdName, capability)
		}
	}
	return nil
}
