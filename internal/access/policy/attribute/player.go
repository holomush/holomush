// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"log/slog"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// PlayerKindLookup resolves whether a player is an ephemeral guest by player
// ID string (ULID). Returning (false, nil) means registered player; (true, nil)
// means guest; any non-nil error triggers the omit path (ADR holomush-ti1b).
//
// The lookup is a func type rather than an interface that returns *auth.Player
// to avoid importing internal/auth from this package, which would create an
// import-cycle via the access → auth dependency edge.
type PlayerKindLookup func(ctx context.Context, playerID string) (isGuest bool, err error)

// PlayerAttributeProviderOption configures optional behaviour on
// PlayerAttributeProvider at construction time.
type PlayerAttributeProviderOption func(*PlayerAttributeProvider)

// WithPlayerKindLookup supplies an optional guest-lookup. Without it the
// provider omits is_guest (has_is_guest=false) per the omit-don't-sentinel
// rule (ADR holomush-ti1b).
func WithPlayerKindLookup(fn PlayerKindLookup) PlayerAttributeProviderOption {
	return func(p *PlayerAttributeProvider) {
		p.kindLookup = fn
	}
}

// PlayerAttributeProvider exposes player-level attributes for ABAC subject
// resolution. v1 schema: player.id, player.grants, player.is_guest,
// player.has_is_guest. The grant set is captured at construction time from the
// operator allow-list (typically loaded from crypto.operators YAML config) and
// is read-only thereafter — no reload in v1, by design (sub-epic B INV-B6).
//
// Players are Subjects in this design, not Resources; ResolveResource always
// returns (nil, nil) per INV-B9.
type PlayerAttributeProvider struct {
	// operators is the set of player ULIDs that hold CapabilityCryptoOperator.
	// Membership is captured at construction; no method on this type writes
	// to this map (INV-B6).
	operators map[string]struct{}
	// kindLookup is an optional func that resolves is_guest for a player ID.
	// When nil the provider omits the is_guest key (has_is_guest=false) per
	// the omit-don't-sentinel invariant (ADR holomush-ti1b).
	kindLookup PlayerKindLookup
}

// NewPlayerAttributeProvider constructs a provider with the given operator
// allow-list. Input is deduplicated; empty / nil input is valid and produces
// a provider for which every player has no grants (INV-B7). Optional
// PlayerAttributeProviderOption values configure additional behaviour such as
// the is_guest lookup (WithPlayerKindLookup).
func NewPlayerAttributeProvider(operatorPlayerIDs []string, opts ...PlayerAttributeProviderOption) *PlayerAttributeProvider {
	set := make(map[string]struct{}, len(operatorPlayerIDs))
	for _, id := range operatorPlayerIDs {
		if id == "" {
			continue
		}
		set[id] = struct{}{}
	}
	p := &PlayerAttributeProvider{operators: set}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Namespace returns "player". The bare type name (without trailing colon) is
// the convention for AttributeProvider.Namespace; access.SubjectPlayer
// ("player:") is the colon-suffixed Subject-ID prefix used at construction
// time by access.PlayerSubject.
func (p *PlayerAttributeProvider) Namespace() string {
	return "player"
}

// ResolveResource always returns (nil, nil): players are Subjects in this
// design, not Resources (INV-B9). No resource attributes are exposed under
// the player namespace.
func (p *PlayerAttributeProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// Schema returns the namespace schema: id (string), grants (string list),
// is_guest (bool), has_is_guest (bool). The resolver namespaces these keys at
// merge time as "player.id", "player.grants", etc. — see resolver.go.
// is_guest and has_is_guest follow ADR holomush-ti1b: has_is_guest is always
// present; is_guest is present only when a PlayerKindLookup resolves it.
func (p *PlayerAttributeProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id":           types.AttrTypeString,
			"grants":       types.AttrTypeStringList,
			"is_guest":     types.AttrTypeBool,
			"has_is_guest": types.AttrTypeBool,
		},
	}
}

// ResolveSubject resolves player attributes for a "player:<ulid>" subject.
// Returns (nil, nil) for any other namespace (provider declines, resolver
// moves on). Returns un-namespaced attribute keys ("id", "grants") per the
// AttributeProvider contract; the resolver namespaces them at merge time.
//
// Error codes:
//   - INVALID_ENTITY_ID: malformed subject shape (no colon, empty post-colon).
//   - INVALID_PLAYER_ID: non-ULID identifier under "player:" namespace.
func (p *PlayerAttributeProvider) ResolveSubject(ctx context.Context, subjectID string) (map[string]any, error) {
	parts := strings.SplitN(subjectID, ":", 2)
	if len(parts) != 2 {
		return nil, oops.
			Code("INVALID_ENTITY_ID").
			With("entity_id", subjectID).
			Errorf("invalid subject ID format: expected 'type:id'")
	}

	entityType, idStr := parts[0], parts[1]
	if entityType != "player" {
		return nil, nil
	}
	if idStr == "" {
		return nil, oops.
			Code("INVALID_ENTITY_ID").
			With("entity_id", subjectID).
			Errorf("invalid subject ID format: empty ID under 'player:' namespace")
	}

	if _, err := ulid.Parse(idStr); err != nil {
		return nil, oops.
			Code("INVALID_PLAYER_ID").
			With("entity_id", subjectID).
			With("id_part", idStr).
			Wrapf(err, "invalid player ULID")
	}

	grants := []string{}
	if _, ok := p.operators[idStr]; ok {
		grants = []string{access.CapabilityCryptoOperator}
	}

	attrs := map[string]any{
		"id":     idStr,
		"grants": grants,
	}

	// Resolve is_guest per ADR holomush-ti1b (motivating bug holomush-9gtl):
	// when has_is_guest=false the `is_guest` key MUST be OMITTED from the bag
	// (not emitted as a false or empty-string sentinel). The DSL evaluator's
	// missing-attr→false semantics preserve default-deny on seeds that gate on
	// player.is_guest when the lookup is unconfigured or errors. Emitting a
	// sentinel false would satisfy `false == false` and create a fail-open
	// match against any other unresolved peer attribute.
	if p.kindLookup != nil {
		isGuest, err := p.kindLookup(ctx, idStr)
		if err != nil {
			// Lookup failure → omit is_guest, emit witness false (fail-safe).
			slog.WarnContext(
				ctx, "player kind lookup failed — omitting is_guest attribute (fail-safe)",
				"player_id", idStr,
				"err", err,
			)
			attrs["has_is_guest"] = false
		} else {
			attrs["is_guest"] = isGuest
			attrs["has_is_guest"] = true
		}
	} else {
		// No lookup configured → omit is_guest key entirely (ADR holomush-ti1b).
		attrs["has_is_guest"] = false
	}

	return attrs, nil
}

// operatorCount is a test-only accessor for the operator-set size. Unexported
// on purpose — production code MUST NOT depend on this method, and INV-B6
// (no public mutation API) is enforced by TestPlayerProviderNoMutationAPI.
func (p *PlayerAttributeProvider) operatorCount() int {
	return len(p.operators)
}
