// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	// auditheader is a leaf sub-package that owns the JetStream-header
	// parser used by both the host audit projection and SDK Layer 2.
	// Going through the leaf rather than internal/eventbus/audit avoids
	// the test-time cycle that would otherwise form via internal/core's
	// cross-package event-type assertions (event_test.go imports
	// pkg/plugin; the audit package transitively imports internal/core
	// through internal/eventbus.RenderingPublisher).
	//
	// Same-module import of the internal/ leaf is structurally permitted
	// by Go's internal/ rules (both packages live under
	// github.com/holomush/holomush/). SDK consumers (plugin authors)
	// MUST NOT follow this precedent — internal/ is host-only.
	"github.com/holomush/holomush/internal/eventbus/audit/auditheader"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
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
// pluginv1.AuditRow 1:1 (INV-EVENTBUS-26) and is consumed via the two
// helpers below.
//
// Plugin authors typically don't construct AuditRow manually — they use
// StoreFromMessage(msg) at AuditEvent RPC ingest, persist the row
// fields verbatim, then use LoadForQuery(row) to construct the proto
// frame returned on QueryHistory. Round-trip stability is INV-CRYPTO-40.
//
// crypto fields (Codec, Payload, DEKRef, DEKVersion) are OPAQUE to the
// plugin — plugin code MUST store and return them byte-for-byte. The
// host owns interpretation. Plugin Layer 2 is convenience for plugin
// authors; the host's threat model does not rely on Layer 2 correctness
// (INV-CRYPTO-41 and INV-CRYPTO-42 are enforced host-side).

// AuditRow is the Go-side mirror of pluginv1.AuditRow. Field
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
// values — INV-CRYPTO-39 byte-equality across the host-projection branch
// and the per-plugin dispatcher branch is structural.
//
// Mirrors the host dispatcher's buildAuditRow construction so plugin
// authors who choose to use Layer 2 see the same projection-field +
// crypto-header extraction behaviour as the host audit projection.
//
// Returns errors with codes:
//
//	AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED
//	AUDIT_MISSING_HEADER / AUDIT_BAD_SCHEMA_VERSION /
//	AUDIT_DEK_REF_PARSE_FAILED / AUDIT_DEK_VERSION_PARSE_FAILED
//	  (from audit.ParseAuditHeaders).
func StoreFromMessage(msg jetstream.Msg) (AuditRow, error) {
	hdrMeta, err := auditheader.Parse(msg.Headers())
	if err != nil {
		return AuditRow{}, err //nolint:wrapcheck // error already coded by parser
	}

	var ev eventbusv1.Event
	if err := proto.Unmarshal(msg.Data(), &ev); err != nil {
		return AuditRow{}, oops.Code("AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED").Wrap(err)
	}

	row := AuditRow{
		Subject:   ev.GetSubject(),
		Type:      ev.GetType(),
		Actor:     ev.GetActor(),
		Codec:     hdrMeta.Codec,
		Payload:   ev.GetPayload(),
		SchemaVer: hdrMeta.SchemaVer,
	}
	id := ev.GetId()
	if len(id) != 16 {
		return AuditRow{}, oops.Code("AUDIT_PLUGIN_BAD_EVENT_ID").
			With("length", len(id)).
			Errorf("event.id must be 16 bytes (ULID); got %d", len(id))
	}
	copy(row.EventID[:], id)
	if ts := ev.GetTimestamp(); ts != nil {
		row.Timestamp = ts.AsTime()
	}
	if hdrMeta.DEKRef != nil {
		v := uint64(*hdrMeta.DEKRef) //nolint:gosec // dek_ref originates as crypto_keys.id (BIGSERIAL, always >= 0); int64→uint64 widening is safe
		row.DEKRef = &v
	}
	if hdrMeta.DEKVersion != nil {
		v := uint32(*hdrMeta.DEKVersion) //nolint:gosec // dek_version originates as a 1-based counter (always >= 0); int32→uint32 is safe
		row.DEKVersion = &v
	}
	return row, nil
}

// LoadForQuery converts a stored AuditRow into the proto frame returned
// by PluginAuditService.QueryHistory. Round-trip stable with
// StoreFromMessage (INV-CRYPTO-40).
//
// Per-field copy from AuditRow to *pluginv1.AuditRow:
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
func LoadForQuery(row AuditRow) (*pluginv1.AuditRow, error) {
	out := &pluginv1.AuditRow{
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
		out.DekRef = row.DEKRef
	}
	if row.DEKVersion != nil {
		out.DekVersion = row.DEKVersion
	}
	return out, nil
}

// DecryptOwnAuditRows sends a batch of AuditRows to the host's
// AuditService.DecryptOwnAuditRows RPC and returns the per-row
// RowResult slice (one result per input row, echoing row.Id for
// positional correlation per INV-CRYPTO-37).
//
// The host owns all crypto decisions; this function is client transport
// only — it MUST NOT log or cache the returned plaintext bytes. Callers
// MUST discard plaintext once they have finished using it.
//
// gRPC status errors pass through as-is; the host stamps reject codes
// as gRPC status messages. Callers that branch on specific codes use
// google.golang.org/grpc/status.FromError to extract them.
func DecryptOwnAuditRows(ctx context.Context, client hostv1.AuditServiceClient, rows []*pluginv1.AuditRow) ([]*pluginv1.RowResult, error) {
	resp, err := client.DecryptOwnAuditRows(ctx, &hostv1.DecryptOwnAuditRowsRequest{Rows: rows})
	if err != nil {
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is (host stamps codes; callers use status.FromError)
	}
	return resp.GetResults(), nil
}
