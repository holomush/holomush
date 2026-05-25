// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// CryptoKeysLookup answers the layer (1) DEK existence pre-check for
// the fence. Production wiring (Task E.3 / bead 1r0v.5) supplies a
// concrete implementation that queries crypto_keys with the
// `destroyed_at IS NULL` filter so destroyed keys read as Exists=false.
//
// The caller MUST treat (false, nil) as "DEK is gone or never existed"
// (per-row refusal) and a non-nil err as infrastructure failure
// (stream-fatal, AUDIT_ROW_DEK_LOOKUP_FAILED).
type CryptoKeysLookup interface {
	Exists(ctx context.Context, dekRef uint64) (bool, error)
}

// ViolationEmitter publishes a host-emit
// `audit.<game>.system.plugin_integrity_violation` event when the
// fence detects a downgrade attempt at INV-P7-7. EmitViolation MUST
// NOT block indefinitely — the fence applies a 100ms bounded timeout
// and proceeds with the row refusal regardless of emit success.
type ViolationEmitter interface {
	EmitViolation(
		ctx context.Context,
		pluginName string,
		row *pluginauditpb.AuditRow,
		expectedSensitivity string,
		refusalCode string,
	) error
}

// PluginDowngradeFenceOption tunes NewPluginDowngradeFence.
type PluginDowngradeFenceOption func(*PluginDowngradeFence)

// WithAlwaysSensitiveTypes installs the manifest-derived always-sensitive
// type set. Built ONCE at boot per INV-P7-8 — no hot-reload. The fence
// copies the input map so callers may not mutate it after construction
// (any mutation would silently shift the refusal surface).
func WithAlwaysSensitiveTypes(set map[string]struct{}) PluginDowngradeFenceOption {
	return func(f *PluginDowngradeFence) {
		// Copy to insulate the fence from caller-side mutation.
		copied := make(map[string]struct{}, len(set))
		for k := range set {
			copied[k] = struct{}{}
		}
		f.alwaysSensitive = copied
	}
}

// WithCryptoKeysLookup wires the layer (1) DEK existence check.
// Required for INV-P7-15; a nil lookup makes the fence treat any
// non-identity codec row as a refusal.
func WithCryptoKeysLookup(lookup CryptoKeysLookup) PluginDowngradeFenceOption {
	return func(f *PluginDowngradeFence) { f.cryptoKeysLookup = lookup }
}

// WithViolationEmitter wires the audit emitter for INV-P7-7 refusals.
// A nil emitter means the fence still refuses the row but does not
// emit the host audit event (the caller decides whether to allow this
// degraded mode in tests).
func WithViolationEmitter(emitter ViolationEmitter) PluginDowngradeFenceOption {
	return func(f *PluginDowngradeFence) { f.emitter = emitter }
}

// WithFenceLogger sets the slog handler used for non-fatal warnings
// (e.g. emitter timeout / error). Defaults to slog.Default() when
// unset.
func WithFenceLogger(log *slog.Logger) PluginDowngradeFenceOption {
	return func(f *PluginDowngradeFence) {
		if log != nil {
			f.log = log
		}
	}
}

// violationEmitTimeout bounds the synchronous emit at INV-P7-7 so a
// backpressured `audit.<game>.system.*` cannot block the read stream
// indefinitely. 100ms is the spec-pinned ceiling (Phase C plan §3
// rule 3); on timeout the row refusal still proceeds — losing the
// audit signal is worse than blocking the stream, but blocking the
// stream is also unacceptable.
const violationEmitTimeout = 100 * time.Millisecond

// PluginDowngradeFence wraps an inner PluginHistoryRouter and applies
// the Phase 7 read-side fence checks before forwarding rows to the
// caller. Implements PluginHistoryRouter for drop-in installation at
// the Reader.QueryHistory plugin branch.
//
// Two-layer fence:
//   - Layer (1) — INV-P7-7 manifest-set heuristic: identity codec
//     for an always-sensitive type is a downgrade attempt; refuse +
//     emit violation audit.
//   - Layer (1) — INV-P7-15 DEK existence: non-identity codec with
//     unknown / absent dek_ref is unrecoverable; refuse silently
//     (indistinguishable from legitimate Rekey-destroyed case).
//
// Refusals surface as per-row metadata_only=true (NOT stream-fatal).
// A malicious plugin that puts a downgrade event first MUST NOT be
// able to DoS subsequent honest rows on the same stream.
type PluginDowngradeFence struct {
	inner            PluginHistoryRouter
	alwaysSensitive  map[string]struct{}
	cryptoKeysLookup CryptoKeysLookup
	emitter          ViolationEmitter
	log              *slog.Logger
}

// NewPluginDowngradeFence builds the fence. The set passed via
// WithAlwaysSensitiveTypes is captured by copy at construction time
// per INV-P7-8 — no hot-reload. Hot-reload infrastructure is filed
// as holomush-kl9w (P3, separate bead).
func NewPluginDowngradeFence(inner PluginHistoryRouter, opts ...PluginDowngradeFenceOption) *PluginDowngradeFence {
	f := &PluginDowngradeFence{
		inner:           inner,
		alwaysSensitive: map[string]struct{}{},
		log:             slog.Default(),
	}
	for _, o := range opts {
		if o != nil {
			o(f)
		}
	}
	return f
}

// QueryHistory implements PluginHistoryRouter — wraps the inner stream.
func (f *PluginDowngradeFence) QueryHistory(
	ctx context.Context,
	pluginName string,
	q eventbus.HistoryQuery,
) (eventbus.HistoryStream, error) {
	inner, err := f.inner.QueryHistory(ctx, pluginName, q)
	if err != nil {
		//nolint:wrapcheck // forwarding inner router error verbatim preserves gRPC status codes
		return nil, err
	}
	return &fencedStream{
		fence:      f,
		inner:      inner,
		pluginName: pluginName,
	}, nil
}

// fenceVerdict is the result of fenceCheckRow — the per-row check that
// enforces INV-P7-7 (downgrade heuristic) and INV-P7-15 (DEK existence).
// Shared in-package so the snapshot read-back path (T5 / INV-RB-5) can
// reuse the check without going through the full fencedStream pipeline.
type fenceVerdict int

const (
	// fenceClean means the row passed both checks and may proceed to decryption.
	fenceClean fenceVerdict = iota
	// fenceRefuseDowngrade means INV-P7-7 fired: identity codec for an
	// always-sensitive type. Caller MUST emit a plugin_integrity_violation
	// audit event (emitViolationBounded) before refusing.
	fenceRefuseDowngrade
	// fenceRefuseDEKMissing means INV-P7-15 fired: non-identity codec with
	// absent or lookup-miss dek_ref. Indistinguishable from legitimate
	// Rekey-destroyed case; no violation emit.
	fenceRefuseDEKMissing
	// fenceRefuseInternal means INV-P7-15 fail-closed: cryptoKeysLookup is
	// nil (configuration failure). Distinct from fenceRefuseDEKMissing so
	// callers can surface the right NoPlaintextReason.
	fenceRefuseInternal
)

// fenceCheckRow applies INV-P7-7 (downgrade) + INV-P7-15 (DEK existence)
// to one plugin audit row. Pure except for the cryptoKeys existence lookup.
// Shared by fencedStream.Next (routed reads, T4) and the snapshot direct
// entry (T5) so INV-RB-5 holds on both paths.
//
// The caller is responsible for mapping the returned fenceVerdict to the
// appropriate refusal reason and emitting the violation audit on
// fenceRefuseDowngrade.
func fenceCheckRow(
	ctx context.Context,
	row *pluginauditpb.AuditRow,
	alwaysSensitive map[string]struct{},
	lookup CryptoKeysLookup,
) (fenceVerdict, error) {
	codec := row.GetCodec()

	// INV-P7-7 — manifest-set heuristic.
	if codec == "identity" {
		if _, sensitive := alwaysSensitive[row.GetType()]; sensitive {
			return fenceRefuseDowngrade, nil
		}
		// identity + non-sensitive: pass through.
		return fenceClean, nil
	}

	// INV-P7-15 — DEK existence pre-check for non-identity codec.
	if row.DekRef == nil {
		// Absent dek_ref for non-identity codec is unrecoverable. No
		// violation emit — indistinguishable from legitimate
		// Rekey-destroyed row. NoPlaintextReasonDEKMissing per the
		// "looks like destroyed-DEK metadata-only" contract; using
		// DowngradeRefused here would mis-attribute a normal Rekey-aged
		// row as a malicious downgrade.
		return fenceRefuseDEKMissing, nil
	}
	if lookup == nil {
		// Without a configured lookup the fence cannot validate. Treat
		// as refusal (fail-closed) — production wiring at E.3 always
		// supplies a non-nil lookup; only test fakes hit this branch
		// when they intentionally omit it. Reason is Internal to
		// distinguish from the legitimate DEK-gone case.
		return fenceRefuseInternal, nil
	}
	exists, lookupErr := lookup.Exists(ctx, *row.DekRef)
	if lookupErr != nil {
		// Infrastructure failure — stream-fatal. Wrap with the
		// AUDIT_ROW_DEK_LOOKUP_FAILED code so callers can distinguish
		// infrastructure failure from the legitimate DEK-gone case.
		return fenceClean, oops.Code("AUDIT_ROW_DEK_LOOKUP_FAILED").
			With("dek_ref", *row.DekRef).
			Wrap(lookupErr)
	}
	if !exists {
		// DEK existed at publish time but is now absent (legitimate
		// Rekey-destroyed) or never existed (malformed publisher). Both
		// surface as fenceRefuseDEKMissing — the read-side cannot
		// distinguish them, and the operator UX should match the
		// legitimate destroyed-DEK case.
		return fenceRefuseDEKMissing, nil
	}

	return fenceClean, nil
}

// fencedStream is the wrapped HistoryStream applied by the fence.
type fencedStream struct {
	fence      *PluginDowngradeFence
	inner      eventbus.HistoryStream
	pluginName string
}

// Next applies the Phase 7 layer (1) checks per the spec contract.
// See PluginDowngradeFence type doc for the rule order.
func (s *fencedStream) Next(ctx context.Context) (eventbus.Event, error) {
	ev, err := s.inner.Next(ctx)
	if err != nil {
		// Forward all errors (including io.EOF) unchanged.
		//nolint:wrapcheck // forward inner stream error verbatim
		return ev, err
	}

	// Recover the plugin's source-of-truth row. nil = host-owned
	// (substrate stamp not applied); pass through unchanged so the
	// fence never penalizes events the host itself produced.
	row := eventbus.AuditRowOf(ev)
	if row == nil {
		return ev, nil
	}

	verdict, fenceErr := fenceCheckRow(ctx, row, s.fence.alwaysSensitive, s.fence.cryptoKeysLookup)
	if fenceErr != nil {
		// Infrastructure failure — stream-fatal. Re-wrap with plugin
		// context so callers can identify which plugin's row triggered
		// the failure. Callers MUST treat this as terminal.
		return ev, oops.Code("AUDIT_ROW_DEK_LOOKUP_FAILED").
			With("plugin", s.pluginName).
			Wrap(fenceErr)
	}

	switch verdict {
	case fenceRefuseDowngrade:
		s.fence.emitViolationBounded(ctx, s.pluginName, row)
		return refuseEvent(ev, eventbus.NoPlaintextReasonDowngradeRefused), nil
	case fenceRefuseDEKMissing:
		return refuseEvent(ev, eventbus.NoPlaintextReasonDEKMissing), nil
	case fenceRefuseInternal:
		return refuseEvent(ev, eventbus.NoPlaintextReasonInternal), nil
	default: // fenceClean
		// Clean row — pass through.
		return ev, nil
	}
}

// Close forwards to the inner stream.
func (s *fencedStream) Close() error {
	//nolint:wrapcheck // forward inner Close verbatim
	return s.inner.Close()
}

// refuseEvent wraps eventbus.Event.Refused with the fence's reason
// taxonomy in one place. Delegates payload + auditRow.Payload clearing
// to eventbus.Event.Refused, which is the canonical refusal semantic
// (master spec INV-26: refused row payload empty — both Event.Payload
// AND the embedded plugin-source-of-truth auditRow's Payload).
//
// The reason MUST distinguish the spec-mandated branches:
//   - INV-P7-7 downgrade detected → NoPlaintextReasonDowngradeRefused
//   - INV-P7-15 DEK absent / DEK lookup-miss → NoPlaintextReasonDEKMissing
//     (so the row reads operationally identical to the legitimate
//     destroyed-DEK metadata-only case per master spec INV-26 — a malicious
//     plugin that omits dek_ref MUST NOT be reported as a "downgrade").
//   - INV-P7-15 nil-lookup fail-closed → NoPlaintextReasonInternal
//     (configuration failure — production wiring at E.3 always supplies a
//     non-nil lookup; only test fakes hit this fail-closed branch).
func refuseEvent(ev eventbus.Event, reason eventbus.NoPlaintextReason) eventbus.Event {
	return ev.Refused(reason)
}

// emitViolationBounded fires the INV-P7-7 audit emit synchronously
// with a 100ms bounded timeout. On timeout / error the row refusal
// still proceeds — the audit signal is best-effort. WARN-level log
// captures the failure for operator visibility.
func (f *PluginDowngradeFence) emitViolationBounded(
	parent context.Context,
	pluginName string,
	row *pluginauditpb.AuditRow,
) {
	if f.emitter == nil {
		// No emitter configured — silent. Tests may intentionally
		// omit; production wiring always supplies one.
		return
	}
	emitCtx, cancel := context.WithTimeout(parent, violationEmitTimeout)
	defer cancel()

	// expected_sensitivity payload value MUST be "always" per the
	// `events.<game>.system.plugin_integrity_violation` schema documented
	// in master spec §4.6 (Phase 7 amendment). "always_sensitive" was a
	// pre-spec wording that produces an off-schema payload.
	err := f.emitter.EmitViolation(emitCtx, pluginName, row, "always", "AUDIT_ROW_DOWNGRADE_DETECTED")
	if err == nil {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		f.log.WarnContext(parent, "plugin downgrade fence: violation emit timed out",
			slog.String("plugin", pluginName),
			slog.String("type", row.GetType()),
			slog.Duration("timeout", violationEmitTimeout))
		return
	}
	f.log.WarnContext(parent, "plugin downgrade fence: violation emit failed",
		slog.String("plugin", pluginName),
		slog.String("type", row.GetType()),
		slog.Any("err", err))
}

// AlwaysSensitiveTypesForTest exposes the captured always-sensitive
// set for the INV-P7-8 boot-built immutability test. Returns a copy
// to prevent the test from mutating the live set.
//
// Build-tagged would be ideal but the corresponding test file is in
// _test.go and may run under either build tag — keeping this exported
// here is the simplest path. Documented as test-only by the For-Test
// suffix.
func (f *PluginDowngradeFence) AlwaysSensitiveTypesForTest() map[string]struct{} {
	out := make(map[string]struct{}, len(f.alwaysSensitive))
	for k := range f.alwaysSensitive {
		out[k] = struct{}{}
	}
	return out
}
