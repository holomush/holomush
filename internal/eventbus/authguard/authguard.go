// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package authguard provides the policy-evaluation seam for sensitive
// event delivery. AuthGuard combines DEK participant-set membership,
// plugin manifest declarations, and ABAC grants into a single typed
// Decision per Phase 3b grounding doc Decision 1.
package authguard

import (
	"context"

	"github.com/oklog/ulid/v2"

	accesstypes "github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// IdentityKind identifies the type of principal in a CheckRequest.
type IdentityKind int

// IdentityKind constants enumerate the principals AuthGuard can evaluate.
const (
	IdentityKindUnknown IdentityKind = iota
	IdentityKindCharacter
	IdentityKindPlayer
	IdentityKindPlugin
	IdentityKindOperator
)

// Identity is the typed authenticated principal AuthGuard evaluates.
// Named "Identity" rather than "Subject" because eventbus.Subject already
// exists at internal/eventbus/types.go:16 as the JetStream subject filter
// type. binding_id semantics: see master spec §4.3a — long-lived
// player↔character tenure, NOT session.ID.
type Identity struct {
	Kind        IdentityKind
	PlayerID    string
	CharacterID string
	BindingID   string
	PluginName  string
	InstanceID  string
}

// CheckRequest is the input to Guard.Check.
type CheckRequest struct {
	Identity   Identity
	KeyID      codec.KeyID
	KeyVersion uint32
	EventType  string
	EventID    ulid.ULID
}

// DecisionCode is the typed outcome of a Guard.Check call.
type DecisionCode int

// DecisionCode constants enumerate the outcomes of Guard.Check.
const (
	DecisionCodeUnknown DecisionCode = iota
	PermitParticipant
	PermitPlayerHistory
	PermitPluginGrant
	DenyNotParticipant
	DenyPlayerNeverParticipated
	DenyPlayerNoABACGrant
	DenyManifestDeclarationMissing
	DenyNoABACGrant
	DenyOperatorUseAdminRPC
	DenyAuditBackpressure
	DenyUnknownIdentityKind
)

// Decision is the result of a Guard.Check call.
type Decision struct {
	Permit       bool
	Code         DecisionCode
	GrantID      ulid.ULID
	Reason       string
	ABACDecision *accesstypes.Decision
}

// Permitted returns true if the decision grants access.
func (d Decision) Permitted() bool { return d.Permit }

// AuthGuard is the policy-evaluation seam for sensitive event delivery.
type AuthGuard interface {
	Check(ctx context.Context, req CheckRequest) (Decision, error)
}

// ParticipantLookup retrieves the participant set for a (keyID, version) DEK.
type ParticipantLookup interface {
	Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]dek.Participant, error)
}

// ManifestLookup checks whether a plugin has declared requests_decryption
// for a given event type in its manifest.
type ManifestLookup interface {
	PluginRequestsDecryption(pluginName, eventType string) bool
}

// ABACEngine is the narrow ABAC interface AuthGuard requires.
type ABACEngine interface {
	Evaluate(ctx context.Context, req accesstypes.AccessRequest) (accesstypes.Decision, error)
}

// BackpressureChecker reports whether a plugin's audit-emit queue is throttled.
type BackpressureChecker interface {
	ShouldThrottle(pluginName string) bool
}
