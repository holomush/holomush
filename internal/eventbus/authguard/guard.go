// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package authguard

import (
	"context"
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	accesstypes "github.com/holomush/holomush/internal/access/policy/types"
)

// Guard is the production AuthGuard impl. Stateless; safe for concurrent use.
type Guard struct {
	parts    ParticipantLookup
	manifest ManifestLookup
	abac     ABACEngine
	bp       BackpressureChecker
}

// New constructs a Guard. All four dependencies are required; nil returns
// AUTHGUARD_DEPENDENCY_NIL.
func New(p ParticipantLookup, m ManifestLookup, a ABACEngine, b BackpressureChecker) (*Guard, error) {
	switch {
	case p == nil:
		return nil, oops.Code("AUTHGUARD_DEPENDENCY_NIL").With("dependency", "ParticipantLookup").Errorf("nil ParticipantLookup")
	case m == nil:
		return nil, oops.Code("AUTHGUARD_DEPENDENCY_NIL").With("dependency", "ManifestLookup").Errorf("nil ManifestLookup")
	case a == nil:
		return nil, oops.Code("AUTHGUARD_DEPENDENCY_NIL").With("dependency", "ABACEngine").Errorf("nil ABACEngine")
	case b == nil:
		return nil, oops.Code("AUTHGUARD_DEPENDENCY_NIL").With("dependency", "BackpressureChecker").Errorf("nil BackpressureChecker")
	}
	return &Guard{parts: p, manifest: m, abac: a, bp: b}, nil
}

// Check evaluates the CheckRequest against the §7.2 four-branch decision tree.
func (g *Guard) Check(ctx context.Context, req CheckRequest) (Decision, error) {
	switch req.Identity.Kind {
	case IdentityKindCharacter:
		return g.checkCharacter(ctx, req)
	case IdentityKindPlayer:
		return g.checkPlayer(ctx, req)
	case IdentityKindPlugin:
		if req.ReadBack {
			return g.checkPluginReadback(ctx, req)
		}
		return g.checkPlugin(ctx, req)
	case IdentityKindOperator:
		return Decision{
			Permit: false,
			Code:   DenyOperatorUseAdminRPC,
			Reason: "operator reads go through AdminReadStream (§7.5)",
		}, nil
	default:
		return Decision{
			Permit: false,
			Code:   DenyUnknownIdentityKind,
			Reason: fmt.Sprintf("unknown identity kind: %d", req.Identity.Kind),
		}, nil
	}
}

// checkCharacter — Branch 1: binding_id match against participant set.
func (g *Guard) checkCharacter(ctx context.Context, req CheckRequest) (Decision, error) {
	parts, err := g.parts.Participants(ctx, req.KeyID, req.KeyVersion)
	if err != nil {
		return Decision{}, oops.Code("AUTHGUARD_PARTICIPANTS_FAILED").Wrap(err)
	}
	for _, p := range parts {
		if p.BindingID != "" && p.BindingID == req.Identity.BindingID {
			return Decision{Permit: true, Code: PermitParticipant, Reason: "character is current participant by binding_id"}, nil
		}
	}
	return Decision{Permit: false, Code: DenyNotParticipant, Reason: "character not in DEK participant set"}, nil
}

// checkPlayer — Branch 2: player_id match + ABAC permit for read_own_history.
func (g *Guard) checkPlayer(ctx context.Context, req CheckRequest) (Decision, error) {
	parts, err := g.parts.Participants(ctx, req.KeyID, req.KeyVersion)
	if err != nil {
		return Decision{}, oops.Code("AUTHGUARD_PARTICIPANTS_FAILED").Wrap(err)
	}
	var matched bool
	for _, p := range parts {
		if p.PlayerID != "" && p.PlayerID == req.Identity.PlayerID {
			matched = true
			break
		}
	}
	if !matched {
		return Decision{Permit: false, Code: DenyPlayerNeverParticipated, Reason: "player never participated"}, nil
	}
	abacReq, err := accesstypes.NewAccessRequest(
		"player:"+req.Identity.PlayerID,
		"read_own_history",
		fmt.Sprintf("dek:%d:%d", req.KeyID, req.KeyVersion),
		nil,
	)
	if err != nil {
		return Decision{}, oops.Code("AUTHGUARD_ABAC_REQUEST_FAILED").Wrap(err)
	}
	abacDec, err := g.abac.Evaluate(ctx, abacReq)
	if err != nil {
		return Decision{}, oops.Code("AUTHGUARD_ABAC_EVAL_FAILED").Wrap(err)
	}
	if !abacDec.IsAllowed() {
		return Decision{Permit: false, Code: DenyPlayerNoABACGrant, Reason: "ABAC denied", ABACDecision: &abacDec}, nil
	}
	return Decision{
		Permit:       true,
		Code:         PermitPlayerHistory,
		GrantID:      mustParseULID(abacDec.PolicyID()),
		ABACDecision: &abacDec,
	}, nil
}

// checkPlugin — Branch 3: backpressure pre-check → manifest declaration → ABAC permit.
func (g *Guard) checkPlugin(ctx context.Context, req CheckRequest) (Decision, error) {
	if g.bp.ShouldThrottle(req.Identity.PluginName) {
		return Decision{Permit: false, Code: DenyAuditBackpressure, Reason: "audit-emit queue throttled"}, nil
	}
	if !g.manifest.PluginRequestsDecryption(req.Identity.PluginName, req.EventType) {
		return Decision{Permit: false, Code: DenyManifestDeclarationMissing, Reason: "manifest does not declare requests_decryption"}, nil
	}
	abacReq, err := accesstypes.NewAccessRequest(
		access.PluginSubject(req.Identity.PluginName),
		"decrypt",
		fmt.Sprintf("dek:%d:%d", req.KeyID, req.KeyVersion),
		map[string]any{
			"event_type":  req.EventType,
			"plugin_name": req.Identity.PluginName,
			"plugin_inst": req.Identity.InstanceID,
		},
	)
	if err != nil {
		return Decision{}, oops.Code("AUTHGUARD_ABAC_REQUEST_FAILED").Wrap(err)
	}
	abacDec, err := g.abac.Evaluate(ctx, abacReq)
	if err != nil {
		return Decision{}, oops.Code("AUTHGUARD_ABAC_EVAL_FAILED").Wrap(err)
	}
	if !abacDec.IsAllowed() {
		return Decision{Permit: false, Code: DenyNoABACGrant, Reason: "ABAC denied", ABACDecision: &abacDec}, nil
	}
	return Decision{
		Permit:       true,
		Code:         PermitPluginGrant,
		GrantID:      mustParseULID(abacDec.PolicyID()),
		ABACDecision: &abacDec,
	}, nil
}

// checkPluginReadback — read-back path: backpressure pre-check → readback
// manifest declaration → permit. INV-CRYPTO-27 gate g2 (manifest); gate g1
// (OwnerMap subject ownership) is enforced upstream at the primitive entry.
// NO ABAC gate — gate 3 was dropped (that plumbing is unbuilt; spec §7.5).
// ctx is unused (kept for signature parity with checkPlugin / future ABAC).
func (g *Guard) checkPluginReadback(_ context.Context, req CheckRequest) (Decision, error) {
	if g.bp.ShouldThrottle(req.Identity.PluginName) {
		return Decision{Permit: false, Code: DenyAuditBackpressure, Reason: "audit-emit queue throttled"}, nil
	}
	if !g.manifest.PluginCanReadBack(req.Identity.PluginName, req.EventType) {
		return Decision{Permit: false, Code: DenyReadbackManifestMissing, Reason: "manifest does not declare crypto.emits[].readback"}, nil
	}
	return Decision{Permit: true, Code: PermitPluginReadbackGrant}, nil
}

// mustParseULID parses a ULID-format string into ulid.ULID. Falls back
// to zero ULID if the input doesn't parse (which can happen for
// non-ULID PolicyIDs in legacy seed policies). The caller logs the
// parse failure separately.
func mustParseULID(s string) ulid.ULID {
	if s == "" {
		return ulid.ULID{}
	}
	parsed, err := ulid.Parse(s)
	if err != nil {
		return ulid.ULID{}
	}
	return parsed
}
