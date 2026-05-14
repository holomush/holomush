// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
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

// -----------------------------------------------------------------------
// Layer 2: plugin-owned audit row mirror.
//
// pluginsdk.AuditRow is the projection-only-plus-crypto-envelope shape
// plugins store in their audit tables (e.g. plugin_core_scenes.scene_log)
// and return on PluginAuditService.QueryHistory. It mirrors
// pluginauditpb.AuditRow 1:1 (INV-P7-4) and is consumed via the two
// helpers below.
//
// Plugin authors typically don't construct AuditRow manually — they use
// StoreFromMessage(msg) at AuditEvent RPC ingest, persist the row
// fields verbatim, then use LoadForQuery(row) to construct the proto
// frame returned on QueryHistory. Round-trip stability is INV-P7-5.
//
// crypto fields (Codec, Payload, DEKRef, DEKVersion) are OPAQUE to the
// plugin — plugin code MUST store and return them byte-for-byte. The
// host owns interpretation. Plugin Layer 2 is convenience for plugin
// authors; the host's threat model does not rely on Layer 2 correctness
// (INV-P7-6 and INV-P7-7 are enforced host-side).

// AuditRow is the Go-side mirror of pluginauditpb.AuditRow. Field
// ordering matches the proto field-numbering for stability across
// proto regenerations.
type AuditRow struct {
	EventID   ulid.ULID
	Subject   string
	Type      string
	Timestamp time.Time
	Actor     *eventbusv1.Actor

	Codec   string
	Payload []byte

	DEKRef     *uint64 // nil ⇔ identity codec ⇔ proto field absent
	DEKVersion *uint32

	SchemaVer int32
}

// StoreFromMessage extracts an AuditRow from a JetStream message.
// Preserves payload bytes byte-equal; uses the shared header parser
// (internal/eventbus/audit/header_parser.go) for typed crypto/schema
// values — INV-P7-2 byte-equality across the host-projection branch
// and the per-plugin dispatcher branch is structural.
func StoreFromMessage(_ jetstream.Msg) (AuditRow, error) {
	// Implementation: parse JS headers via audit.ParseAuditHeaders;
	// unmarshal msg.Data() into eventbusv1.Event ONLY to extract the
	// projection fields (NOT payload — payload stays as msg.Data()
	// byte-equal so ciphertext is preserved). Build AuditRow.
	// Full implementation in Task B.1 once the plugin_consumer.go
	// integration point pins the exact extraction sequence.
	//
	// Returns errors with codes:
	//   AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED
	//   AUDIT_MISSING_HEADER / AUDIT_BAD_SCHEMA_VERSION /
	//   AUDIT_DEK_REF_PARSE_FAILED / AUDIT_DEK_VERSION_PARSE_FAILED
	//   (from audit.ParseAuditHeaders).
	// NOTE: this is intentionally a panic per plan §"Task A.3.4". Full body
	// lands in Task B.1; until then, no internal caller exists. If a plugin
	// author hits this, that's a clear signal they're using a not-yet-shipped
	// API — the panic message is more informative than a silent stub.
	panic("StoreFromMessage: TODO — implemented in Task B.1")
}

// LoadForQuery converts a stored AuditRow into the proto frame returned
// by PluginAuditService.QueryHistory. Round-trip stable with
// StoreFromMessage (INV-P7-5).
//
// Per-field copy from AuditRow to *pluginauditpb.AuditRow:
//   - EventID → Id (raw 16-byte ULID via row.EventID[:])
//   - Subject, Type, Codec, Payload, SchemaVer → same-named proto fields verbatim
//   - Timestamp → timestamppb.New(row.Timestamp)
//   - Actor → row.Actor (proto type matches)
//   - DEKRef / DEKVersion → only set when non-nil (proto optional)
//
// Returns (proto, nil) in v1; the error return is reserved for future
// validation (e.g. enforce codec=identity ⇔ DEKRef==nil). Defer the
// agreement check to host-side code per spec §4.5 (host owns the
// envelope semantics).
func LoadForQuery(row AuditRow) (*pluginauditpb.AuditRow, error) {
	proto := &pluginauditpb.AuditRow{
		Id:        row.EventID[:],
		Subject:   row.Subject,
		Type:      row.Type,
		Timestamp: timestamppb.New(row.Timestamp),
		Actor:     row.Actor,
		Codec:     row.Codec,
		Payload:   row.Payload,
		SchemaVer: row.SchemaVer,
	}
	if row.DEKRef != nil {
		proto.DekRef = row.DEKRef
	}
	if row.DEKVersion != nil {
		proto.DekVersion = row.DEKVersion
	}
	return proto, nil
}
