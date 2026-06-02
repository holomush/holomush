// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

// readback.go: the host-side read-back decrypt primitive shared by both
// read-back consumers — the snapshot RPC (T6) and the fence clean-row
// decrypt wiring (T8). It is the single security-critical seam where a
// plugin-stored audit row is decrypted on demand under the read-back
// authorization path (manifest crypto.emits[].readback), distinct from the
// live-delivery path.
//
// INV-RB-1 — one primitive, two consumers: snapshot + fence both call
// decryptPluginRow rather than re-implementing decrypt/authz/audit.
// INV-RB-3 — every plugin read-back decrypt produces an INV-19 audit
// record; absence of an audit emitter fails closed (enforced inside
// decodeAuthorizeAndDispatch).
// INV-RB-4 — clean rows yield plaintext; refused rows yield a typed
// NoPlaintextReason and no plaintext.
// INV-RB-5 — the downgrade/DEK-existence fence (fenceCheckRow, T4) runs
// BEFORE any decrypt, so the read-back path inherits the same layer-(1)
// refusals as the routed read path.
// INV-RB-12 — the read-back authorization discriminator (ReadBack=true) is
// threaded onto the AuthGuard check for plugin principals.

import (
	"context"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/codec"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// maxDecryptBatch is the per-call REJECT cap for DecryptOwnRows, enforced ONCE
// on the common read-back path so both plugin runtimes (binary gRPC and Lua
// hostfunc) inherit the identical bound — the plugin-runtime-symmetry invariant
// (a cap the binary path enforces but Lua bypasses would be a privilege
// gradient). Unlike a clamp, an over-cap batch is REJECTED outright with
// DECRYPT_BATCH_TOO_LARGE: silently truncating a decrypt request would hide
// rows from the caller, the wrong failure mode for a read-back surface.
const maxDecryptBatch = 500

// RowResult is the outcome of decryptPluginRow. Exactly one of the three
// observable states holds:
//
//   - OK (plaintext available): Err == nil && Reason == Unspecified;
//     Plaintext carries the decrypted bytes.
//   - Refused (metadata-only): Err == nil && Reason != Unspecified;
//     Plaintext is nil. Reason is one of the eventbus.NoPlaintextReason
//     refusal values (non-zero).
//   - Errored (infrastructure / fail-closed): Err != nil. The caller MUST
//     NOT surface plaintext; this includes the INV-RB-3 nil-audit-emitter
//     fail-closed case.
type RowResult struct {
	Plaintext []byte
	Reason    eventbus.NoPlaintextReason // zero (Unspecified) == no refusal
	Err       error
}

// OK reports whether the row decrypted to usable plaintext — no error and
// no refusal reason. Zero-value NoPlaintextReason is the canonical "no
// refusal" sentinel (INV-RB-4); adding a new reason constant is forbidden
// (TestNoPlaintextReasonEnumParity pins the count at 8).
func (r RowResult) OK() bool {
	return r.Err == nil && r.Reason == eventbus.NoPlaintextReasonUnspecified
}

// readbackDeps bundles the host-side capabilities the read-back primitive
// needs. Built once at the consumer's boot seam (snapshot RPC / fence) and
// passed by value per call.
type readbackDeps struct {
	// alwaysSensitive is the manifest-derived set of event types that MUST
	// NOT appear with an identity codec (INV-P7-7 downgrade detection).
	alwaysSensitive map[string]struct{}
	// cryptoKeys answers the layer-(1) DEK existence pre-check (INV-P7-15).
	cryptoKeys CryptoKeysLookup
	// guard authorizes the read-back (ReadBack=true path).
	guard eventbus.SessionAuthGuard
	// dek resolves DEK key material for decryption.
	dek eventbus.SessionDEKManager
	// audit records the INV-19 plugin decrypt event (INV-RB-3). A nil audit
	// emitter fails closed for plugin principals inside the dispatcher.
	audit eventbus.SessionAuditEmitter
}

// decryptPluginRow is the reusable host-side read-back decrypt primitive
// (INV-RB-1). It runs the downgrade/DEK-existence fence first (INV-RB-5),
// maps a refusal verdict to a typed RowResult, and otherwise reconstructs
// the AAD envelope from the audit row and delegates decrypt + authorization
// + audit to the shared dispatcher with ReadBack=true for plugin principals
// (INV-RB-12). The dispatcher enforces the INV-19 / INV-RB-3 audit
// fail-closed contract.
func decryptPluginRow(
	ctx context.Context,
	identity eventbus.SessionIdentity,
	row *pluginauditpb.AuditRow,
	d readbackDeps,
) RowResult {
	// INV-RB-5: layer-(1) fence BEFORE any decrypt.
	verdict, err := fenceCheckRow(ctx, row, d.alwaysSensitive, d.cryptoKeys)
	if err != nil {
		return RowResult{Err: err}
	}
	switch verdict {
	case fenceRefuseDowngrade:
		return RowResult{Reason: eventbus.NoPlaintextReasonDowngradeRefused}
	case fenceRefuseDEKMissing:
		return RowResult{Reason: eventbus.NoPlaintextReasonDEKMissing}
	case fenceRefuseInternal:
		return RowResult{Reason: eventbus.NoPlaintextReasonInternal}
	case fenceClean:
		// fall through to decrypt
	}

	// AuditRowToEvent omits Payload (it carries only the AAD-canonical
	// fields, INV-STORE-5). Restore the ciphertext so the dispatcher's Decode
	// has its AEAD input; aad.Build excludes Payload, so the reconstructed
	// AAD is unaffected by this assignment.
	envelope := AuditRowToEvent(row)
	envelope.Payload = row.GetPayload()

	codecName := codec.Name(row.GetCodec())
	keyID := codec.KeyID(row.GetDekRef())
	keyVersion := row.GetDekVersion()

	// INV-RB-12: ReadBack=true selects the manifest crypto.emits[].readback
	// authorization branch — only meaningful for plugin principals.
	readBack := identity.Kind == eventbus.IdentityKindPlugin

	ev, metaOnly, decErr := decodeAuthorizeAndDispatch(
		ctx, envelope, codecName, keyID, keyVersion,
		identity, d.guard, d.dek, d.audit, readBack,
	)
	if decErr != nil {
		return RowResult{Err: decErr}
	}
	if metaOnly {
		return RowResult{Reason: ev.NoPlaintextReason}
	}
	return RowResult{Plaintext: ev.Payload}
}

// Stable snake_case no_plaintext_reason strings surfaced over the wire by
// DecryptOwnAuditRows. These are an API contract: SDKs and clients switch on
// them, so the values MUST NOT drift. They are deliberately decoupled from the
// internal eventbus.NoPlaintextReason enum numbering.
const (
	noPlaintextReasonNotOwner         = "not_owner"
	noPlaintextReasonDowngradeRefused = "downgrade_refused"
	noPlaintextReasonDEKMissing       = "dek_missing"
	noPlaintextReasonAuthGuardDeny    = "auth_guard_deny"
	noPlaintextReasonStaleDEK         = "stale_dek"
	noPlaintextReasonAuditQueueFull   = "audit_queue_full"
	noPlaintextReasonInternal         = "internal"
)

// reasonToWire maps an internal refusal reason to its stable wire string.
// Unspecified (the OK sentinel) MUST never reach this function — callers gate
// on RowResult.OK() first — so an unexpected zero maps to "internal" as a
// fail-safe rather than an empty string that a client would read as "OK".
func reasonToWire(r eventbus.NoPlaintextReason) string {
	switch r {
	case eventbus.NoPlaintextReasonDowngradeRefused:
		return noPlaintextReasonDowngradeRefused
	case eventbus.NoPlaintextReasonDEKMissing:
		return noPlaintextReasonDEKMissing
	case eventbus.NoPlaintextReasonAuthGuardDeny:
		return noPlaintextReasonAuthGuardDeny
	case eventbus.NoPlaintextReasonStaleDEK:
		return noPlaintextReasonStaleDEK
	case eventbus.NoPlaintextReasonAuditQueueFull:
		return noPlaintextReasonAuditQueueFull
	case eventbus.NoPlaintextReasonDEKBadColumns, eventbus.NoPlaintextReasonInternal,
		eventbus.NoPlaintextReasonUnspecified:
		return noPlaintextReasonInternal
	default:
		return noPlaintextReasonInternal
	}
}

// ReadbackDecryptor is the host-side RPC entry to the read-back decrypt
// primitive (decryptPluginRow), gated by OwnerMap subject ownership (g1). It is
// the single seam between the snapshot's PluginHostService.DecryptOwnAuditRows
// handler (package goplugin) and the unexported primitive in this package — the
// host never touches decryptPluginRow directly, and the primitive stays
// unexported (INV-RB-1).
//
// g1 (this type) refuses any row whose subject the OwnerMap attributes to a
// different plugin BEFORE any decrypt; g2 (the manifest crypto.emits[].readback
// flag, INV-RB-2) is enforced inside decryptPluginRow's AuthGuard check via the
// ReadBack=true discriminator.
type ReadbackDecryptor struct {
	owners *audit.OwnerMap
	deps   readbackDeps
}

// NewReadbackDecryptor builds the read-back decryptor from the OwnerMap (g1
// gate) and the host-side crypto capabilities. owners MAY be nil — a nil
// OwnerMap attributes every subject to the host, so EVERY plugin row resolves
// to not_owner (fail-closed: no plugin owns anything without a declared map).
func NewReadbackDecryptor(
	owners *audit.OwnerMap,
	alwaysSensitive map[string]struct{},
	cryptoKeys CryptoKeysLookup,
	guard eventbus.SessionAuthGuard,
	dek eventbus.SessionDEKManager,
	auditEm eventbus.SessionAuditEmitter,
) *ReadbackDecryptor {
	// Copy to insulate the read-back fence from caller-side mutation. The
	// manifest set is shared by reference with the fence dispatcher at the boot
	// seam (cmd/holomush/sub_grpc.go), which copies it for the same reason
	// (see plugin_downgrade_fence.go and tier.go).
	copied := make(map[string]struct{}, len(alwaysSensitive))
	for k := range alwaysSensitive {
		copied[k] = struct{}{}
	}
	return &ReadbackDecryptor{
		owners: owners,
		deps: readbackDeps{
			alwaysSensitive: copied,
			cryptoKeys:      cryptoKeys,
			guard:           guard,
			dek:             dek,
			audit:           auditEm,
		},
	}
}

// DecryptOwnRow decrypts one of pluginName's OWN audit rows, returning the
// per-row proto envelope the host streams back (INV-RB-12: id always echoes
// AuditRow.id for positional correlation).
//
// g1 ownership gate runs FIRST: if the OwnerMap attributes row.Subject to a
// plugin other than pluginName (or to the host), the row is refused with
// no_plaintext_reason="not_owner" and decryptPluginRow is NEVER called — no
// decrypt, no DEK touch, no audit emission. Otherwise the row flows through the
// shared primitive, which runs the downgrade/DEK fence (INV-RB-5), the
// ReadBack=true AuthGuard branch (g2 / INV-RB-2), and the INV-19 audit
// (INV-RB-3). Clean rows yield plaintext; refused rows map their reason to the
// stable wire string; infrastructure errors map to "internal" and NEVER leak
// plaintext (INV-RB-4 fail-closed).
func (d *ReadbackDecryptor) DecryptOwnRow(
	ctx context.Context,
	pluginName, instanceID string,
	row *pluginauditpb.AuditRow,
) *pluginauditpb.RowResult {
	// g1: OwnerMap subject ownership. A nil OwnerMap resolves to the host
	// owner (empty PluginName), so plugin rows fail closed as not_owner.
	owner := d.owners.Resolve(row.GetSubject())
	if owner.PluginName != pluginName {
		return &pluginauditpb.RowResult{
			Id:      row.GetId(),
			Outcome: &pluginauditpb.RowResult_NoPlaintextReason{NoPlaintextReason: noPlaintextReasonNotOwner},
		}
	}

	identity := eventbus.SessionIdentity{
		Kind:       eventbus.IdentityKindPlugin,
		PluginName: pluginName,
		InstanceID: instanceID,
	}

	res := decryptPluginRow(ctx, identity, row, d.deps)
	switch {
	case res.Err != nil:
		// Infrastructure / fail-closed (incl. INV-RB-3 nil-audit-emitter).
		// Surface a generic refusal — NEVER plaintext.
		return &pluginauditpb.RowResult{
			Id:      row.GetId(),
			Outcome: &pluginauditpb.RowResult_NoPlaintextReason{NoPlaintextReason: noPlaintextReasonInternal},
		}
	case !res.OK():
		return &pluginauditpb.RowResult{
			Id:      row.GetId(),
			Outcome: &pluginauditpb.RowResult_NoPlaintextReason{NoPlaintextReason: reasonToWire(res.Reason)},
		}
	default:
		return &pluginauditpb.RowResult{
			Id:      row.GetId(),
			Outcome: &pluginauditpb.RowResult_Plaintext{Plaintext: res.Plaintext},
		}
	}
}

// DecryptOwnRows decrypts a batch of pluginName's OWN audit rows, enforcing the
// maxDecryptBatch cap ONCE on this common read-back path so both plugin
// runtimes (binary gRPC + Lua hostfunc) inherit the identical bound. A batch
// larger than maxDecryptBatch is REJECTED (not clamped) with
// DECRYPT_BATCH_TOO_LARGE and NO row is decrypted. Otherwise each row flows
// through DecryptOwnRow; results are returned 1:1 in request order (INV-RB-12).
func (d *ReadbackDecryptor) DecryptOwnRows(
	ctx context.Context,
	pluginName, instanceID string,
	rows []*pluginauditpb.AuditRow,
) ([]*pluginauditpb.RowResult, error) {
	if len(rows) > maxDecryptBatch {
		return nil, oops.Code("DECRYPT_BATCH_TOO_LARGE").
			With("plugin", pluginName).
			With("count", len(rows)).
			Errorf("decrypt batch exceeds cap %d", maxDecryptBatch)
	}
	results := make([]*pluginauditpb.RowResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, d.DecryptOwnRow(ctx, pluginName, instanceID, row))
	}
	return results, nil
}
