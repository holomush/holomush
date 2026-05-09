// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// PlayerAttributeProvider exposes player-level attributes for ABAC subject
// resolution. v1 schema: player.id, player.grants. The grant set is captured
// at construction time from the operator allow-list (typically loaded from
// crypto.operators YAML config) and is read-only thereafter — no reload in v1,
// by design (sub-epic B INV-B6).
//
// Players are Subjects in this design, not Resources; ResolveResource always
// returns (nil, nil) per INV-B9.
type PlayerAttributeProvider struct {
	// operators is the set of player ULIDs that hold CapabilityCryptoOperator.
	// Membership is captured at construction; no method on this type writes
	// to this map (INV-B6).
	operators map[string]struct{}
}

// NewPlayerAttributeProvider constructs a provider with the given operator
// allow-list. Input is deduplicated; empty / nil input is valid and produces
// a provider for which every player has no grants (INV-B7).
func NewPlayerAttributeProvider(operatorPlayerIDs []string) *PlayerAttributeProvider {
	set := make(map[string]struct{}, len(operatorPlayerIDs))
	for _, id := range operatorPlayerIDs {
		if id == "" {
			continue
		}
		set[id] = struct{}{}
	}
	return &PlayerAttributeProvider{operators: set}
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

// Schema returns the v1 namespace schema: id (string) + grants (string list).
// The resolver namespaces these keys at merge time as "player.id" and
// "player.grants" — see internal/access/policy/attribute/resolver.go.
func (p *PlayerAttributeProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id":     types.AttrTypeString,
			"grants": types.AttrTypeStringList,
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
func (p *PlayerAttributeProvider) ResolveSubject(_ context.Context, subjectID string) (map[string]any, error) {
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

	return map[string]any{
		"id":     idStr,
		"grants": grants,
	}, nil
}

// operatorCount is a test-only accessor for the operator-set size. Unexported
// on purpose — production code MUST NOT depend on this method, and INV-B6
// (no public mutation API) is enforced by TestPlayerProviderNoMutationAPI.
func (p *PlayerAttributeProvider) operatorCount() int {
	return len(p.operators)
}
