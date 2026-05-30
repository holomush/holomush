// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package pluginauthz holds the runtime-neutral per-action authorization
// core shared by the binary (PluginHostService.Evaluate) and Lua
// (holomush.evaluate) surfaces. Both delegate here so policy/trust
// behavior cannot diverge between runtimes (INV-5).
package pluginauthz

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/samber/oops"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/pkg/errutil"
)

// otelInstruments holds the OTel evaluation counter acquired lazily on first
// call to Evaluate. Acquiring at init() time binds the instrument to the
// no-op default provider because package init runs before main configures the
// global SDK. sync.Once defers acquisition until after SDK setup.
var (
	counterOnce        sync.Once
	evaluationsCounter metric.Int64Counter // nil until first Evaluate call
)

// initCounter acquires the evaluations counter from the global MeterProvider.
// Called once via counterOnce on the first Evaluate invocation, by which time
// main has configured the global OTel SDK. Evaluation proceeds even if counter
// creation fails (observability failure MUST NOT block authz).
func initCounter() {
	meter := otel.GetMeterProvider().Meter("holomush.plugin")
	var err error
	evaluationsCounter, err = meter.Int64Counter(
		"holomush_pluginauthz_evaluations_total",
		metric.WithDescription("Total plugin authorization evaluations by pluginauthz.Evaluate"),
	)
	if err != nil {
		// No context available at this call site; use bare slog (MAY carve-out).
		slog.Warn("pluginauthz: failed to create evaluations counter; metric will not be recorded",
			"error", err)
		evaluationsCounter = nil
	}
}

// ActorSubject maps a host-stamped Actor to its ABAC subject string. It is
// the single mapping shared by the binary and Lua surfaces (INV-5) so the
// two cannot derive divergent subjects. Returns "" for an unknown/zero
// actor kind, which Evaluate treats as fail-closed.
func ActorSubject(a core.Actor) string {
	switch a.Kind {
	case core.ActorCharacter:
		// Empty ID would bypass the access.CharacterSubject guard (it panics on
		// empty); fail closed instead, matching this function's documented
		// "" contract for unresolvable actors.
		if a.ID == "" {
			return ""
		}
		return access.CharacterSubject(a.ID)
	case core.ActorPlugin:
		// Empty ID would trip access.PluginSubject's panic-on-empty guard; fail
		// closed instead, matching this function's "" contract (and the
		// ActorCharacter case above). Also keeps the colon ABAC-subject literal
		// out of host code (the INV-ROPS-3 boundary).
		if a.ID == "" {
			return ""
		}
		return access.PluginSubject(a.ID)
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
//
// OTel instrumentation: a span ("pluginauthz.evaluate") and a counter
// ("holomush_pluginauthz_evaluations_total") are recorded per call. The
// counter is acquired lazily on first call (via counterOnce) so it binds to
// the real SDK provider after main configures it — not the no-op default that
// exists during package init. The tracer is acquired per call (always fine).
func Evaluate(ctx context.Context, in Input) (Decision, error) {
	// Lazily acquire the counter on first call so it binds to the real SDK
	// provider (configured in main) rather than the no-op default.
	counterOnce.Do(initCounter)

	// Start OTel span — use the global tracer provider (matches otel_middleware.go scope).
	tracer := otel.GetTracerProvider().Tracer("holomush.plugin")
	subjectKind := subjectKindFromSubject(in.Subject)
	ctx, span := tracer.Start(
		ctx, "pluginauthz.evaluate",
		trace.WithAttributes(
			attribute.String("plugin.name", in.PluginName),
			attribute.String("evaluate.action", in.Action),
			attribute.String("evaluate.resource", in.Resource),
			attribute.String("evaluate.subject_kind", subjectKind),
		),
	)
	defer span.End()

	// recordEffect is a helper that emits the counter and sets evaluate.effect on the span.
	recordEffect := func(effect string) {
		span.SetAttributes(attribute.String("evaluate.effect", effect))
		if evaluationsCounter != nil {
			evaluationsCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("plugin", in.PluginName),
				attribute.String("effect", effect),
				attribute.String("subject_kind", subjectKind),
			))
		}
	}
	recordError := func(err error) {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		recordEffect("error")
	}

	if in.Engine == nil {
		err := oops.Code("EVALUATE_NO_ENGINE").
			With("plugin", in.PluginName).Errorf("evaluate called with nil engine")
		recordError(err)
		return Decision{}, err
	}
	if in.Subject == "" {
		// No authenticated actor bound to the call (INV-2).
		err := oops.Code("EVALUATE_NO_SUBJECT").
			With("plugin", in.PluginName).
			Errorf("evaluate called without an authenticated subject")
		recordError(err)
		return Decision{}, err
	}
	if in.Action == "" {
		err := oops.Code("EVALUATE_EMPTY_ACTION").
			With("plugin", in.PluginName).Errorf("action must not be empty")
		recordError(err)
		return Decision{}, err
	}

	resType, resID, ok := splitResourceRef(in.Resource)
	if !ok {
		err := oops.Code("EVALUATE_BAD_RESOURCE").
			With("plugin", in.PluginName).With("resource", in.Resource).
			Errorf("resource must be of the form type:id")
		recordError(err)
		return Decision{}, err
	}

	// Entitlement (INV-3): plugin-owned type or the command carve-out.
	if resType != commandResourceType && !in.OwnedTypes[resType] {
		err := oops.Code("EVALUATE_UNENTITLED_TYPE").
			With("plugin", in.PluginName).With("resource_type", resType).
			Errorf("plugin may not evaluate resource type %q", resType)
		recordError(err)
		return Decision{}, err
	}
	_ = resID // present-and-non-empty already validated by splitResourceRef

	req, reqErr := types.NewAccessRequest(in.Subject, in.Action, in.Resource, nil)
	if reqErr != nil {
		err := oops.With("plugin", in.PluginName).Wrap(reqErr)
		recordError(err)
		return Decision{}, err
	}

	dec, evalErr := in.Engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, "plugin evaluate engine error", evalErr,
			"plugin", in.PluginName, "action", in.Action, "resource", in.Resource)
		// Fail closed: error accompanies a non-allowing decision.
		err := oops.With("plugin", in.PluginName).Wrap(evalErr)
		recordError(err)
		return Decision{}, err
	}

	result := Decision{
		Allowed:       dec.IsAllowed(),
		Reason:        dec.Reason(),
		MatchedPolicy: dec.PolicyID(),
	}
	span.SetAttributes(attribute.String("evaluate.matched_policy", result.MatchedPolicy))

	effect := "deny"
	if result.Allowed {
		effect = "allow"
	}
	recordEffect(effect)

	// Audit (INV-4): exactly one host-stamped event per evaluation.
	if in.Auditor != nil {
		auditEffect := types.EffectDeny
		if dec.IsAllowed() {
			auditEffect = types.EffectAllow
		}
		if logErr := in.Auditor.Log(ctx, audit.Event{
			Name:      "plugin.evaluate",
			Source:    audit.SourcePlugin,
			Component: in.PluginName,
			Subject:   in.Subject,
			Action:    in.Action,
			Resource:  in.Resource,
			Effect:    auditEffect,
			Timestamp: time.Now(),
		}); logErr != nil {
			errutil.LogErrorContext(ctx, "plugin evaluate audit log failed", logErr,
				"plugin", in.PluginName, "action", in.Action)
			// Audit failure does not change the authorization outcome.
		}
	}

	return result, nil
}

// subjectKindFromSubject extracts the kind prefix from an ABAC subject string
// (e.g. "character:01ABC" → "character", "plugin:core-scenes" → "plugin",
// "system" → "system", "" → "unknown"). These are ABAC subject IDs in
// "<kind>:<id>" form — not pub/sub stream subjects, which use dot-style.
// Used as a non-PII span attribute.
func subjectKindFromSubject(subject string) string {
	if subject == "" {
		return "unknown"
	}
	if subject == "system" {
		return "system"
	}
	if i := strings.IndexByte(subject, ':'); i > 0 {
		return subject[:i]
	}
	return "unknown"
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
