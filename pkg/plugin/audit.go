// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"log/slog"
)

// AuditAttrs is a convenience alias for plugin-provided audit attribute maps.
// Keys SHOULD be namespaced (e.g., "channel.type" rather than "type") to
// avoid collision with host-overlay keys the dispatcher may merge in.
type AuditAttrs map[string]string

// handlerContextKey is the unexported type used as the context.WithValue key
// for the in-process audit hint slice.
type handlerContextKey struct{}

// handlerKey is the sentinel value looked up on contexts carrying an
// in-process audit hint slice.
var handlerKey = handlerContextKey{}

// NewContextForHandler returns a derived context with an empty AuditHint
// slice attached. The plugin SDK adapter calls this before invoking the
// plugin's HandleCommand, so plugin authors do not call it directly in
// most cases. It is exported so tests and plugin authors implementing
// custom dispatch flows can construct a compatible context.
func NewContextForHandler(ctx context.Context) context.Context {
	hints := &[]AuditHint{}
	return context.WithValue(ctx, handlerKey, hints)
}

// HarvestAuditHints returns and clears the hint slice attached to ctx.
// The SDK adapter calls this after the plugin's HandleCommand returns
// to serialize the accumulated hints into the proto response. Plugin
// authors should not call it directly.
//
// Returns nil if no slice was attached (plain context, no handler
// derivation).
func HarvestAuditHints(ctx context.Context) []AuditHint {
	slice, ok := ctx.Value(handlerKey).(*[]AuditHint)
	if !ok {
		return nil
	}
	drained := *slice
	*slice = nil
	return drained
}

// AuditRecorder is the interface plugin handlers use to emit audit hints.
// Obtain one via Audit(ctx). Hints emitted through a recorder accumulate
// on the provided context and are harvested into CommandResponse.AuditHints
// when the SDK adapter serializes the response.
//
// Method naming: Deny and Allow correspond to AuditEffectDeny and
// AuditEffectAllow. The interface is intentionally narrow — other effect
// values are not exposed because plugin handler decisions are always one
// of these two outcomes.
type AuditRecorder interface {
	// Deny records an audit hint with AuditEffectDeny.
	Deny(id, message string, attrs AuditAttrs)

	// Allow records an audit hint with AuditEffectAllow.
	Allow(id, message string, attrs AuditAttrs)
}

// contextRecorder is the no-op-safe implementation returned by Audit().
// If the context has no handler attachment, recorder method calls silently
// drop the hint. This is intentional: plugin code that runs in both
// handler and non-handler contexts can call Audit(ctx).Deny
// unconditionally.
type contextRecorder struct {
	ctx context.Context
}

// Audit returns an AuditRecorder bound to ctx. Call this from plugin
// HandleCommand code. The recorder accumulates hints on the context;
// the SDK adapter serializes them into CommandResponse.audit_hints
// when the handler returns.
//
// Example:
//
//	func (h *handler) HandleCommand(ctx context.Context, req CommandRequest) (*CommandResponse, error) {
//	    isMember, err := h.store.IsMember(channelID, req.PlayerID)
//	    if err != nil {
//	        return nil, err
//	    }
//	    if !isMember {
//	        pluginsdk.Audit(ctx).Deny("not_member",
//	            "player not in channel members",
//	            pluginsdk.AuditAttrs{"channel.type": "public"})
//	        return pluginsdk.Errorf("You must join #%s before speaking there.", channelName), nil
//	    }
//	    // ... happy path ...
//	}
func Audit(ctx context.Context) AuditRecorder {
	return &contextRecorder{ctx: ctx}
}

// Deny records a deny hint on the recorder's context.
func (r *contextRecorder) Deny(id, message string, attrs AuditAttrs) {
	r.record(id, message, AuditEffectDeny, attrs)
}

// Allow records an allow hint on the recorder's context.
func (r *contextRecorder) Allow(id, message string, attrs AuditAttrs) {
	r.record(id, message, AuditEffectAllow, attrs)
}

func (r *contextRecorder) record(id, message string, effect AuditEffect, attrs AuditAttrs) {
	if id == "" {
		// Proto AuditDecisionHint has min_len=1 on the ID field — an
		// empty ID would fail marshal at the response-serialization
		// step and drop the hint with a confusing error. Drop it here
		// so plugin authors see the problem locally and clearly.
		slog.Warn("pluginsdk.Audit: dropping hint with empty ID",
			"message", message, "effect", effect)
		return
	}
	slice, ok := r.ctx.Value(handlerKey).(*[]AuditHint)
	if !ok {
		// No handler context attached — silent no-op.
		return
	}
	// Copy the attribute map so later caller mutations don't corrupt
	// the recorded hint.
	var copied map[string]string
	if len(attrs) > 0 {
		copied = make(map[string]string, len(attrs))
		for k, v := range attrs {
			copied[k] = v
		}
	}
	*slice = append(*slice, AuditHint{
		ID:         id,
		Message:    message,
		Effect:     effect,
		Attributes: copied,
	})
}
