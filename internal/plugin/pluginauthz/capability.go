// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz

import (
	"context"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/pkg/errutil"
)

// CapabilityInput is the capability-access decision input. Subject is
// host-derived (plugin:<name>); Declared is the resolver-proven manifest reach.
type CapabilityInput struct {
	Engine     types.AccessPolicyEngine
	Auditor    Auditor
	PluginName string
	Subject    string
	Action     string
	Resource   string
	Declared   bool
	Context    map[string]any // dispatch attributes for scope conditions (M3)
}

// EvaluateCapabilityAccess authorizes a plugin's consumption of a host
// capability. Entitlement is manifest-declaration (Declared), NOT OwnedTypes —
// host-capability resources are not plugin-owned (INV-PLUGIN-50). Shares the
// engine call + single audit event with Evaluate (INV-PLUGIN-26). Fails closed:
// a non-nil error always accompanies a non-allowing Decision.
func EvaluateCapabilityAccess(ctx context.Context, in CapabilityInput) (Decision, error) {
	if in.Engine == nil {
		return Decision{}, oops.Code("EVALUATE_NO_ENGINE").With("plugin", in.PluginName).Errorf("evaluate called with nil engine")
	}
	if in.Subject == "" {
		return Decision{}, oops.Code("EVALUATE_NO_SUBJECT").With("plugin", in.PluginName).Errorf("evaluate called without an authenticated subject")
	}
	if in.Action == "" {
		return Decision{}, oops.Code("EVALUATE_EMPTY_ACTION").With("plugin", in.PluginName).Errorf("action must not be empty")
	}
	if !in.Declared {
		return Decision{}, oops.Code("CAPABILITY_NOT_DECLARED").
			With("plugin", in.PluginName).With("resource", in.Resource).
			Errorf("plugin did not declare this capability")
	}
	req, reqErr := types.NewAccessRequest(in.Subject, in.Action, in.Resource, in.Context)
	if reqErr != nil {
		return Decision{}, oops.With("plugin", in.PluginName).Wrap(reqErr)
	}
	dec, evalErr := in.Engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, "plugin capability-access engine error", evalErr,
			"plugin", in.PluginName, "action", in.Action, "resource", in.Resource)
		return Decision{}, oops.With("plugin", in.PluginName).Wrap(evalErr) // fail closed
	}
	result := Decision{Allowed: dec.IsAllowed(), Reason: dec.Reason(), MatchedPolicy: dec.PolicyID()}
	// Single audit event (mirror Evaluate; Name "plugin.capability_access").
	if in.Auditor != nil {
		auditEffect := types.EffectDeny
		if dec.IsAllowed() {
			auditEffect = types.EffectAllow
		}
		if logErr := in.Auditor.Log(ctx, audit.Event{
			Name: "plugin.capability_access", Source: audit.SourcePlugin, Component: in.PluginName,
			Subject: in.Subject, Action: in.Action, Resource: in.Resource,
			Effect: auditEffect, Timestamp: time.Now(),
		}); logErr != nil {
			errutil.LogErrorContext(ctx, "plugin capability-access audit log failed", logErr, "plugin", in.PluginName, "action", in.Action)
		}
	}
	return result, nil
}
