// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package pluginauthz holds the runtime-neutral per-action authorization
// core shared by the binary (PluginHostService.Evaluate) and Lua
// (holomush.evaluate) surfaces. Both delegate here so policy/trust
// behavior cannot diverge between runtimes (INV-5).
package pluginauthz

import (
	"context"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/pkg/errutil"
)

// ActorSubject maps a host-stamped Actor to its ABAC subject string. It is
// the single mapping shared by the binary and Lua surfaces (INV-5) so the
// two cannot derive divergent subjects. Returns "" for an unknown/zero
// actor kind, which Evaluate treats as fail-closed.
func ActorSubject(a core.Actor) string {
	switch a.Kind {
	case core.ActorCharacter:
		return "character:" + a.ID
	case core.ActorPlugin:
		return "plugin:" + a.ID
	case core.ActorSystem:
		// ID is intentionally dropped: all system actors collapse to the bare
		// "system" subject regardless of their specific sentinel ULID.
		return "system"
	default:
		return ""
	}
}

// Auditor is the minimal audit sink pluginauthz needs. *audit.Logger
// satisfies it.
type Auditor interface {
	Log(ctx context.Context, event audit.Event) error
}

// Input carries everything the shared core needs. Subject is HOST-DERIVED
// and MUST NOT originate from plugin-supplied data (INV-1).
type Input struct {
	Engine     types.AccessPolicyEngine
	Auditor    Auditor
	PluginName string
	OwnedTypes map[string]bool // plugin's resource_types; empty for Lua
	Subject    string
	Action     string
	Resource   string // "type:id"
}

// Decision is the runtime-neutral result returned to both surfaces.
type Decision struct {
	Allowed       bool
	Reason        string
	MatchedPolicy string
}

// commandResourceType is the carve-out type any plugin may evaluate for its
// own commands (see spec §3).
const commandResourceType = "command"

// Evaluate runs entitlement → engine → audit and returns a runtime-neutral
// Decision. It fails closed on every error path: a non-nil error always
// accompanies a non-allowing Decision.
func Evaluate(ctx context.Context, in Input) (Decision, error) {
	if in.Engine == nil {
		return Decision{}, oops.Code("EVALUATE_NO_ENGINE").
			With("plugin", in.PluginName).Errorf("evaluate called with nil engine")
	}
	if in.Subject == "" {
		// No authenticated actor bound to the call (INV-2).
		return Decision{}, oops.Code("EVALUATE_NO_SUBJECT").
			With("plugin", in.PluginName).
			Errorf("evaluate called without an authenticated subject")
	}
	if in.Action == "" {
		return Decision{}, oops.Code("EVALUATE_EMPTY_ACTION").
			With("plugin", in.PluginName).Errorf("action must not be empty")
	}

	resType, resID, ok := splitResourceRef(in.Resource)
	if !ok {
		return Decision{}, oops.Code("EVALUATE_BAD_RESOURCE").
			With("plugin", in.PluginName).With("resource", in.Resource).
			Errorf("resource must be of the form type:id")
	}

	// Entitlement (INV-3): plugin-owned type or the command carve-out.
	if resType != commandResourceType && !in.OwnedTypes[resType] {
		return Decision{}, oops.Code("EVALUATE_UNENTITLED_TYPE").
			With("plugin", in.PluginName).With("resource_type", resType).
			Errorf("plugin may not evaluate resource type %q", resType)
	}
	_ = resID // present-and-non-empty already validated by splitResourceRef

	req, reqErr := types.NewAccessRequest(in.Subject, in.Action, in.Resource, nil)
	if reqErr != nil {
		return Decision{}, oops.With("plugin", in.PluginName).Wrap(reqErr)
	}

	dec, evalErr := in.Engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, "plugin evaluate engine error", evalErr,
			"plugin", in.PluginName, "action", in.Action, "resource", in.Resource)
		// Fail closed: error accompanies a non-allowing decision.
		return Decision{}, oops.With("plugin", in.PluginName).Wrap(evalErr)
	}

	result := Decision{
		Allowed:       dec.IsAllowed(),
		Reason:        dec.Reason(),
		MatchedPolicy: dec.PolicyID(),
	}

	// Audit (INV-4): exactly one host-stamped event per evaluation.
	if in.Auditor != nil {
		effect := types.EffectDeny
		if dec.IsAllowed() {
			effect = types.EffectAllow
		}
		if logErr := in.Auditor.Log(ctx, audit.Event{
			Name:      "plugin.evaluate",
			Source:    audit.SourcePlugin,
			Component: in.PluginName,
			Subject:   in.Subject,
			Action:    in.Action,
			Resource:  in.Resource,
			Effect:    effect,
			Timestamp: time.Now(),
		}); logErr != nil {
			errutil.LogErrorContext(ctx, "plugin evaluate audit log failed", logErr,
				"plugin", in.PluginName, "action", in.Action)
			// Audit failure does not change the authorization outcome.
		}
	}

	return result, nil
}

// splitResourceRef parses "type:id" and requires both halves non-empty.
// Per spec §3 the resource format is exactly "type:id" (single colon); a
// resource whose id half still contains a colon is rejected (ok=false).
func splitResourceRef(ref string) (resType, resID string, ok bool) {
	i := strings.IndexByte(ref, ':')
	if i <= 0 || i >= len(ref)-1 {
		return "", "", false
	}
	resID = ref[i+1:]
	if strings.Contains(resID, ":") {
		return "", "", false
	}
	return ref[:i], resID, true
}
