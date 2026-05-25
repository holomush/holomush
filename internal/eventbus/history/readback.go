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

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	pluginauditpb "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

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
	// fields, INV-TS-5). Restore the ciphertext so the dispatcher's Decode
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
