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

// PlayerRoleLookup resolves the roles a player holds, by player ID string
// (ULID). The semantics are deliberately PER PLAYER — the union of roles held
// by any character of that player — matching 01-SPEC §10.5's normative verdict
// and the shipped PostgresRoleStore.PlayerHasRole join ("true iff any character
// of playerID has role"). That makes the web answer to "is this caller an
// admin" identical to the answer the operator socket gives for the same human
// at the same moment; a character-scoped alternative would put two different
// answers over one table.
//
// Returning a non-nil error triggers the omit path (ADR holomush-ti1b): the
// caller omits the roles key and emits has_roles=false rather than substituting
// an empty list.
//
// The lookup is a func type rather than an interface so this package need not
// import internal/store, mirroring PlayerKindLookup. It is shared: both
// ViewerTierProvider (WithViewerRoleLookup) and PlayerAttributeProvider answer
// "what roles does this player hold" through this one signature.
type PlayerRoleLookup func(ctx context.Context, playerID string) ([]string, error)

// ViewerTierProviderOption configures optional behaviour on ViewerTierProvider
// at construction time.
type ViewerTierProviderOption func(*ViewerTierProvider)

// WithViewerRoleLookup supplies an optional per-player role lookup. Without it
// the provider omits roles (has_roles=false) per the omit-don't-sentinel rule
// (ADR holomush-ti1b).
func WithViewerRoleLookup(fn PlayerRoleLookup) ViewerTierProviderOption {
	return func(p *ViewerTierProvider) {
		p.roleLookup = fn
	}
}

// ViewerTierProvider supplies the "viewer" attribute namespace for the portal
// read path's tier ladder (01-SPEC §8.4.1). Schema: viewer.tier,
// viewer.player_id, viewer.has_player_id, viewer.roles, viewer.has_roles.
//
// The viewer namespace is deliberately distinct from both character and player:
// a character: subject activates seed:admin-full-access and
// seed:player-character-colocation, bypassing the tier floor entirely, and a
// player: subject carries the crypto-operator grant axis and has no
// representation for the anonymous rung at all.
//
// Viewers are Subjects, never Resources; ResolveResource always returns
// (nil, nil).
type ViewerTierProvider struct {
	// roleLookup optionally resolves per-player roles. When nil the provider
	// omits the roles key (has_roles=false) per ADR holomush-ti1b.
	roleLookup PlayerRoleLookup
}

// NewViewerTierProvider constructs a viewer-namespace attribute provider.
// Optional ViewerTierProviderOption values configure additional behaviour such
// as the per-player role lookup (WithViewerRoleLookup).
func NewViewerTierProvider(opts ...ViewerTierProviderOption) *ViewerTierProvider {
	p := &ViewerTierProvider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Namespace returns "viewer". The bare type name (without trailing colon) is
// the convention for AttributeProvider.Namespace; access.SubjectViewer
// ("viewer:") is the colon-suffixed Subject-ID prefix used at construction time
// by access.ViewerSubject.
func (p *ViewerTierProvider) Namespace() string {
	return "viewer"
}

// ResolveResource always returns (nil, nil): a viewer is a Subject, never a
// Resource (01-SPEC §8.4.1). Profile reachability has its own resource type
// (access.ResourceProfile) and reads no resource attributes at all.
func (p *ViewerTierProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// Schema returns the namespace schema: tier (string), player_id (string),
// has_player_id (bool), roles (string list), has_roles (bool).
//
// Every key this provider can emit MUST be declared here: the resolver drops
// and counts (abac_rejected_provider_attributes_total) any provider attribute
// whose key is not in the namespace schema, so an undeclared key is silently
// absent rather than loudly wrong — an invisible fail-closed.
//
// SPEC deviation: 01-SPEC §8.4.1's attribute table declares three keys. This
// provider ships five; roles/has_roles are a deliberate amendment, without
// which a viewer-flavored admin read policy could reference no roles attribute
// and so could never permit anyone.
func (p *ViewerTierProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"tier":          types.AttrTypeString,
			"player_id":     types.AttrTypeString,
			"has_player_id": types.AttrTypeBool,
			"roles":         types.AttrTypeStringList,
			"has_roles":     types.AttrTypeBool,
		},
	}
}

// ResolveSubject resolves viewer attributes for the three subject forms of
// 01-SPEC §8.4.1: "viewer:anonymous", "viewer:guest:<ulid>" and
// "viewer:player:<ulid>". Returns (nil, nil) for any other namespace (provider
// declines, resolver moves on). Returns un-namespaced attribute keys per the
// AttributeProvider contract; the resolver namespaces them at merge time.
//
// Error codes:
//   - INVALID_ENTITY_ID: malformed subject shape (no colon, empty id half).
//   - INVALID_VIEWER_TIER: a tier token outside the three rungs. An
//     unrecognized tier is rejected, never carried through as a tier value.
//   - INVALID_VIEWER_PLAYER_ID: non-ULID identifier on the guest/player rung.
func (p *ViewerTierProvider) ResolveSubject(ctx context.Context, subjectID string) (map[string]any, error) {
	parts := strings.SplitN(subjectID, ":", 2)
	if len(parts) != 2 {
		return nil, oops.
			Code("INVALID_ENTITY_ID").
			With("entity_id", subjectID).
			Errorf("invalid subject ID format: expected 'type:id'")
	}

	entityType, remainder := parts[0], parts[1]
	if entityType != "viewer" {
		return nil, nil
	}
	if remainder == "" {
		return nil, oops.
			Code("INVALID_ENTITY_ID").
			With("entity_id", subjectID).
			Errorf("invalid subject ID format: empty ID under 'viewer:' namespace")
	}

	tier, playerID, err := parseViewerRung(subjectID, remainder)
	if err != nil {
		return nil, err
	}

	attrs := map[string]any{"tier": tier}

	// Handle the optional player identity.
	//
	// Per ADR holomush-ti1b (motivating bug holomush-9gtl): on the anonymous
	// rung the `player_id` key MUST be OMITTED from the bag — NOT emitted as an
	// empty-string sentinel. The DSL evaluator's missing-attr-→-false semantics
	// keep every operator false for an absent value, but "" would satisfy
	// `"" == ""` against any other unresolved peer attribute and create a
	// fail-open permit in a default-deny system. The has_player_id witness stays
	// on both branches.
	if playerID != "" {
		attrs["player_id"] = playerID
		attrs["has_player_id"] = true
	} else {
		attrs["has_player_id"] = false
	}

	p.resolveRoles(ctx, playerID, attrs)

	return attrs, nil
}

// parseViewerRung splits the post-"viewer:" remainder into its tier token and
// player identifier. The anonymous rung carries no identifier and returns "".
func parseViewerRung(subjectID, remainder string) (tier, playerID string, err error) {
	if remainder == access.ViewerTierAnonymous {
		return access.ViewerTierAnonymous, "", nil
	}

	rung := strings.SplitN(remainder, ":", 2)
	if len(rung) != 2 {
		return "", "", oops.
			Code("INVALID_VIEWER_TIER").
			With("entity_id", subjectID).
			With("tier", remainder).
			Errorf("unrecognized viewer tier")
	}

	tier, idStr := rung[0], rung[1]
	if tier != access.ViewerTierGuest && tier != access.ViewerTierPlayer {
		// This also catches "viewer:anonymous:<something>": the anonymous rung
		// takes no identifier, so a caller supplying one is on the wrong rung.
		return "", "", oops.
			Code("INVALID_VIEWER_TIER").
			With("entity_id", subjectID).
			With("tier", tier).
			Errorf("unrecognized viewer tier")
	}
	if idStr == "" {
		return "", "", oops.
			Code("INVALID_ENTITY_ID").
			With("entity_id", subjectID).
			Errorf("invalid subject ID format: empty player ID on the %q rung", tier)
	}
	if _, perr := ulid.Parse(idStr); perr != nil {
		return "", "", oops.
			Code("INVALID_VIEWER_PLAYER_ID").
			With("entity_id", subjectID).
			With("id_part", idStr).
			Wrapf(perr, "invalid viewer player ULID")
	}

	return tier, idStr, nil
}

// resolveRoles populates roles/has_roles for a viewer that has a player.
//
// Per ADR holomush-ti1b, roles is OMITTED — never emitted as an empty list — on
// every unresolved path: the anonymous rung (no player to resolve roles for),
// no configured lookup, and a failing lookup. An empty list is the list-flavored
// version of the empty-string sentinel: it is a RESOLVED value that a future
// containsAny-shaped condition would evaluate against. The has_roles witness is
// emitted on every code path.
//
// A lookup failure is fail-safe, not fatal: it logs at warn with the ctx and
// continues, exactly as PlayerAttributeProvider's is_guest branch does.
func (p *ViewerTierProvider) resolveRoles(ctx context.Context, playerID string, attrs map[string]any) {
	if playerID == "" || p.roleLookup == nil {
		attrs["has_roles"] = false
		return
	}

	roles, err := p.roleLookup(ctx, playerID)
	if err != nil {
		slog.WarnContext(
			ctx, "viewer role lookup failed — omitting roles attribute (fail-safe)",
			"player_id", playerID,
			"err", err,
		)
		attrs["has_roles"] = false
		return
	}

	attrs["roles"] = roles
	attrs["has_roles"] = true
}
